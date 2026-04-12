package prole

import (
	"bytes"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

func setupAgentRepo(t *testing.T) *repo.AgentRepo {
	t.Helper()
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("creating test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return repo.NewAgentRepo(conn, nil)
}

func TestCreate_MaxProlesEnforced(t *testing.T) {
	agents := setupAgentRepo(t)

	// Register two existing proles
	agents.Register("prole-a", "prole", nil)
	agents.Register("prole-b", "prole", nil)

	cfg := &config.Config{
		MaxProles:    2,
		TicketPrefix: "nc",
		ProjectRoot:  t.TempDir(),
	}

	err := Create("prole-c", cfg, agents)
	if err == nil {
		t.Fatal("expected error when max_proles limit is reached, got nil")
	}
	if !strings.Contains(err.Error(), "max_proles limit reached") {
		t.Errorf("expected 'max_proles limit reached' error, got: %v", err)
	}
}

func TestCreate_MaxProlesNotEnforced_WhenZero(t *testing.T) {
	agents := setupAgentRepo(t)

	// Register many proles
	for i := 0; i < 5; i++ {
		agents.Register("prole-existing", "prole", nil)
	}

	cfg := &config.Config{
		MaxProles:    0, // 0 means unlimited
		TicketPrefix: "nc",
		ProjectRoot:  t.TempDir(),
	}

	// Should not return a limit error; will fail later on git ops, not limit check
	err := Create("new-prole", cfg, agents)
	if err != nil && strings.Contains(err.Error(), "max_proles limit reached") {
		t.Errorf("max_proles=0 should disable the limit, but got: %v", err)
	}
}

func TestCreate_MaxProlesAllowsCreate_WhenUnderLimit(t *testing.T) {
	agents := setupAgentRepo(t)

	// Register one existing prole, limit is 2
	agents.Register("prole-a", "prole", nil)

	cfg := &config.Config{
		MaxProles:    2,
		TicketPrefix: "nc",
		ProjectRoot:  t.TempDir(),
	}

	// Should not return a limit error; will fail later on git ops
	err := Create("prole-b", cfg, agents)
	if err != nil && strings.Contains(err.Error(), "max_proles limit reached") {
		t.Errorf("should be allowed when under limit, got: %v", err)
	}
}

func TestCreate_DeadProlesNotCounted(t *testing.T) {
	agents := setupAgentRepo(t)

	// Register two proles, mark both dead
	agents.Register("prole-dead-1", "prole", nil)
	agents.UpdateStatus("prole-dead-1", "dead")
	agents.Register("prole-dead-2", "prole", nil)
	agents.UpdateStatus("prole-dead-2", "dead")

	cfg := &config.Config{
		MaxProles:    2,
		TicketPrefix: "nc",
		ProjectRoot:  t.TempDir(),
	}

	// Dead proles don't count — should not return a limit error
	err := Create("prole-new", cfg, agents)
	if err != nil && strings.Contains(err.Error(), "max_proles limit reached") {
		t.Errorf("dead proles should not count toward limit, got: %v", err)
	}
}

// --- PruneDeadWorktrees tests ---

// runGit is a test helper that runs a git command and fails the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// setupPruneEnv creates a bare repo + worktree environment for PruneDeadWorktrees tests.
// Returns:
//   - cfg: config pointing at tempRoot (BareRepoPath = tempRoot/.company_town/repo.git)
//   - agents: an AgentRepo backed by a test DB
//   - bareDir: path to the bare repo (BareRepoPath)
//   - addWorktree: helper to add a named worktree from the bare repo
func setupPruneEnv(t *testing.T) (cfg *config.Config, agents *repo.AgentRepo, bareDir string, addWorktree func(name string) string) {
	t.Helper()

	tempRoot := t.TempDir()

	// Create a "remote" bare repo to act as origin.
	remoteDir := filepath.Join(tempRoot, "remote.git")
	runGit(t, tempRoot, "init", "--bare", remoteDir)

	// Create a temporary clone to make the initial commit.
	initDir := filepath.Join(tempRoot, "init")
	runGit(t, tempRoot, "clone", remoteDir, initDir)
	runGit(t, initDir, "config", "user.email", "test@test.com")
	runGit(t, initDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(initDir, "README"), []byte("init"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, initDir, "add", ".")
	runGit(t, initDir, "commit", "-m", "init")
	runGit(t, initDir, "push", "origin", "HEAD:main")

	// Create the bare clone that BareRepoPath points to.
	ctDir := filepath.Join(tempRoot, ".company_town")
	if err := os.MkdirAll(ctDir, 0755); err != nil {
		t.Fatal(err)
	}
	bareDir = filepath.Join(ctDir, "repo.git")
	runGit(t, tempRoot, "clone", "--bare", remoteDir, bareDir)
	runGit(t, bareDir, "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	runGit(t, bareDir, "fetch", "origin")

	cfg = &config.Config{
		ProjectRoot:  tempRoot,
		TicketPrefix: "nc",
	}

	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	agents = repo.NewAgentRepo(conn, nil)

	// addWorktree creates a new worktree from the bare repo with a tracking branch.
	addWorktree = func(name string) string {
		wtPath := filepath.Join(tempRoot, "worktrees", name)
		branch := "prole/" + name + "/standby"
		runGit(t, bareDir, "worktree", "add", "-b", branch, wtPath, "origin/main")
		// Set the upstream so @{u}.. works.
		runGit(t, wtPath, "branch", "--set-upstream-to=origin/main", branch)
		return wtPath
	}

	return cfg, agents, bareDir, addWorktree
}

// silentLogger returns a logger that discards output.
func silentLogger() *log.Logger {
	return log.New(io.Discard, "", 0)
}

// capturingLogger returns a logger that captures output into buf.
func capturingLogger() (*log.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return log.New(&buf, "", 0), &buf
}

func TestPruneDeadWorktrees_skipsNonProleAgents(t *testing.T) {
	cfg, agents, _, _ := setupPruneEnv(t)

	agents.Register("conductor", "conductor", nil)
	agents.UpdateStatus("conductor", "dead")
	// Give it a fake path — should be skipped because type != "prole"
	agents.SetWorktree("conductor", "/tmp/fake-path")

	pruned, err := PruneDeadWorktrees(cfg, agents, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pruned) != 0 {
		t.Errorf("expected 0 pruned, got %v", pruned)
	}
}

func TestPruneDeadWorktrees_skipsLiveProles(t *testing.T) {
	cfg, agents, _, addWorktree := setupPruneEnv(t)

	wtPath := addWorktree("live-prole")
	agents.Register("live-prole", "prole", nil)
	// Status stays "idle" (not dead)
	agents.SetWorktree("live-prole", wtPath)

	pruned, err := PruneDeadWorktrees(cfg, agents, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pruned) != 0 {
		t.Errorf("expected 0 pruned (prole is live), got %v", pruned)
	}
}

func TestPruneDeadWorktrees_skipsAgentWithNoWorktreePath(t *testing.T) {
	cfg, agents, _, _ := setupPruneEnv(t)

	agents.Register("pathless", "prole", nil)
	agents.UpdateStatus("pathless", "dead")
	// No SetWorktree call — WorktreePath is null

	pruned, err := PruneDeadWorktrees(cfg, agents, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pruned) != 0 {
		t.Errorf("expected 0 pruned, got %v", pruned)
	}
}

func TestPruneDeadWorktrees_skipsNonExistentPath(t *testing.T) {
	cfg, agents, _, _ := setupPruneEnv(t)

	agents.Register("gone", "prole", nil)
	agents.UpdateStatus("gone", "dead")
	agents.SetWorktree("gone", "/tmp/this-path-does-not-exist-nc58-test")

	pruned, err := PruneDeadWorktrees(cfg, agents, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pruned) != 0 {
		t.Errorf("expected 0 pruned (path absent), got %v", pruned)
	}
}

func TestPruneDeadWorktrees_skipsDirtyWorktree(t *testing.T) {
	cfg, agents, _, addWorktree := setupPruneEnv(t)

	wtPath := addWorktree("dirty")
	agents.Register("dirty", "prole", nil)
	agents.UpdateStatus("dirty", "dead")
	agents.SetWorktree("dirty", wtPath)

	// Add an uncommitted file.
	if err := os.WriteFile(filepath.Join(wtPath, "uncommitted.txt"), []byte("work"), 0644); err != nil {
		t.Fatal(err)
	}

	pruned, err := PruneDeadWorktrees(cfg, agents, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pruned) != 0 {
		t.Errorf("expected 0 pruned (dirty worktree), got %v", pruned)
	}
	// Worktree dir should still exist.
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		t.Error("expected dirty worktree to remain on disk")
	}
}

func TestPruneDeadWorktrees_logsWarningForNoUpstream(t *testing.T) {
	cfg, agents, bareDir, _ := setupPruneEnv(t)

	// Create a worktree WITHOUT setting an upstream tracking branch.
	wtPath := filepath.Join(t.TempDir(), "no-upstream")
	runGit(t, bareDir, "worktree", "add", "--no-track", "-b", "prole/no-upstream/standby", wtPath, "origin/main")

	agents.Register("no-upstream", "prole", nil)
	agents.UpdateStatus("no-upstream", "dead")
	agents.SetWorktree("no-upstream", wtPath)

	logger, buf := capturingLogger()
	pruned, err := PruneDeadWorktrees(cfg, agents, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pruned) != 0 {
		t.Errorf("expected 0 pruned (no upstream), got %v", pruned)
	}
	if !strings.Contains(buf.String(), "no-upstream") {
		t.Errorf("expected warning log mentioning agent name, got: %q", buf.String())
	}
}

func TestPruneDeadWorktrees_skipsWorktreeWithUnpushedCommits(t *testing.T) {
	cfg, agents, _, addWorktree := setupPruneEnv(t)

	wtPath := addWorktree("unpushed")
	agents.Register("unpushed", "prole", nil)
	agents.UpdateStatus("unpushed", "dead")
	agents.SetWorktree("unpushed", wtPath)

	// Make an extra commit in the worktree (not pushed to origin).
	runGit(t, wtPath, "config", "user.email", "test@test.com")
	runGit(t, wtPath, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(wtPath, "work.txt"), []byte("work"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, wtPath, "add", ".")
	runGit(t, wtPath, "commit", "-m", "unpushed work")

	pruned, err := PruneDeadWorktrees(cfg, agents, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pruned) != 0 {
		t.Errorf("expected 0 pruned (unpushed commits), got %v", pruned)
	}
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		t.Error("expected worktree with unpushed commits to remain on disk")
	}
}

func TestPruneDeadWorktrees_prunesCleanWorktree(t *testing.T) {
	cfg, agents, _, addWorktree := setupPruneEnv(t)

	wtPath := addWorktree("clean")
	agents.Register("clean", "prole", nil)
	agents.UpdateStatus("clean", "dead")
	agents.SetWorktree("clean", wtPath)

	pruned, err := PruneDeadWorktrees(cfg, agents, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pruned) != 1 || pruned[0] != "clean" {
		t.Errorf("expected [clean] pruned, got %v", pruned)
	}
	// Worktree directory should be gone.
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("expected clean worktree to be removed from disk")
	}
}

func TestPruneDeadWorktrees_clearsDBPathAfterRemoval(t *testing.T) {
	cfg, agents, _, addWorktree := setupPruneEnv(t)

	wtPath := addWorktree("cleardb")
	agents.Register("cleardb", "prole", nil)
	agents.UpdateStatus("cleardb", "dead")
	agents.SetWorktree("cleardb", wtPath)

	_, err := PruneDeadWorktrees(cfg, agents, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	agent, err := agents.Get("cleardb")
	if err != nil {
		t.Fatalf("getting agent: %v", err)
	}
	if agent.WorktreePath.Valid && agent.WorktreePath.String != "" {
		t.Errorf("expected WorktreePath cleared in DB, got %q", agent.WorktreePath.String)
	}
}

func TestPruneDeadWorktrees_logsRemovalFailure(t *testing.T) {
	cfg, agents, _, _ := setupPruneEnv(t)

	// Register a dead prole with a path that exists but isn't a real git worktree.
	// This will cause git status to succeed (empty output from a non-repo dir with no
	// files) but git worktree remove to fail.
	// We use a plain directory that looks clean to git status but can't be worktree-removed.
	plainDir := t.TempDir()

	// Initialize a git repo in plainDir so git status works, with tracking configured.
	runGit(t, plainDir, "init")
	runGit(t, plainDir, "config", "user.email", "test@test.com")
	runGit(t, plainDir, "config", "user.name", "Test")
	// Set a fake upstream so @{u}.. check succeeds (no unpushed commits).
	// We do this by making a commit and a fake upstream ref.
	if err := os.WriteFile(filepath.Join(plainDir, "f"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, plainDir, "add", ".")
	runGit(t, plainDir, "commit", "-m", "init")
	// Create a local "origin/main" ref pointing at HEAD so @{u} resolves.
	runGit(t, plainDir, "update-ref", "refs/remotes/origin/main", "HEAD")
	runGit(t, plainDir, "config", "branch.main.remote", "origin")
	runGit(t, plainDir, "config", "branch.main.merge", "refs/heads/main")

	agents.Register("removefail", "prole", nil)
	agents.UpdateStatus("removefail", "dead")
	agents.SetWorktree("removefail", plainDir)

	logger, buf := capturingLogger()
	pruned, err := PruneDeadWorktrees(cfg, agents, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should NOT be in pruned (removal failed).
	if len(pruned) != 0 {
		t.Errorf("expected 0 pruned (removal should fail), got %v", pruned)
	}
	// Should log a warning about the failure.
	if !strings.Contains(buf.String(), "removefail") {
		t.Errorf("expected warning log mentioning agent name, got: %q", buf.String())
	}
}
