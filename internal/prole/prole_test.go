package prole

import (
	"bytes"
	"errors"
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

func TestIdleProlesNeedingReset_selection(t *testing.T) {
	mk := func(name, agentType, status string, worktree string, currentIssue *int64) *repo.Agent {
		a := &repo.Agent{Name: name, Type: agentType, Status: status}
		if worktree != "" {
			a.WorktreePath.Valid = true
			a.WorktreePath.String = worktree
		}
		if currentIssue != nil {
			a.CurrentIssue.Valid = true
			a.CurrentIssue.Int64 = *currentIssue
		}
		return a
	}

	issueID := int64(42)
	all := []*repo.Agent{
		mk("idle-prole", "prole", "idle", "/wt/idle-prole", nil),
		mk("working-prole", "prole", "working", "/wt/working-prole", &issueID),
		mk("dead-prole", "prole", "dead", "/wt/dead-prole", nil),
		mk("idle-prole-with-issue", "prole", "idle", "/wt/iwi", &issueID),
		mk("idle-prole-no-worktree", "prole", "idle", "", nil),
		mk("idle-reviewer", "reviewer", "idle", "/wt/reviewer", nil),
	}

	got := idleProlesNeedingReset(all)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 selected prole, got %d: %+v", len(got), got)
	}
	if got[0].Name != "idle-prole" {
		t.Errorf("expected idle-prole selected, got %q", got[0].Name)
	}
}

func TestIsValidWorktreePath(t *testing.T) {
	cases := []struct {
		path  string
		valid bool
	}{
		{"/absolute/path", true},
		{"/wt/prole-foo", true},
		{"relative/path", false},
		{"./relative", false},
		{"../traversal", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isValidWorktreePath(c.path); got != c.valid {
			t.Errorf("isValidWorktreePath(%q) = %v, want %v", c.path, got, c.valid)
		}
	}
}

func TestIdleProlesNeedingReset_skipsCorruptedPath(t *testing.T) {
	mk := func(name, worktree string) *repo.Agent {
		a := &repo.Agent{Name: name, Type: "prole", Status: "idle"}
		a.WorktreePath.Valid = true
		a.WorktreePath.String = worktree
		return a
	}

	all := []*repo.Agent{
		mk("relative-path", "worktrees/prole-foo"),
		mk("dot-relative", "./worktrees/prole-bar"),
		mk("traversal", "../../../etc/passwd"),
		mk("absolute", "/wt/good-prole"),
	}

	got := idleProlesNeedingReset(all)
	if len(got) != 1 {
		t.Fatalf("expected exactly 1 selected (absolute path only), got %d: %+v", len(got), got)
	}
	if got[0].Name != "absolute" {
		t.Errorf("expected 'absolute' selected, got %q", got[0].Name)
	}
}

func TestPruneDeadWorktrees_skipsRelativePath(t *testing.T) {
	cfg, agents, _, _ := setupPruneEnv(t)

	agents.Register("relpath", "prole", nil)
	agents.UpdateStatus("relpath", "dead")
	agents.SetWorktree("relpath", "worktrees/relpath") // relative — corrupted DB value

	pruned, err := PruneDeadWorktrees(cfg, agents, silentLogger())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pruned) != 0 {
		t.Errorf("expected 0 pruned (relative path skipped), got %v", pruned)
	}
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
	if !errors.Is(err, ErrMaxProlesLimitReached) {
		t.Errorf("expected ErrMaxProlesLimitReached, got: %v", err)
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
	if errors.Is(err, ErrMaxProlesLimitReached) {
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
	if errors.Is(err, ErrMaxProlesLimitReached) {
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
	if errors.Is(err, ErrMaxProlesLimitReached) {
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

// runGitOut runs a git command, fails the test on error, and returns trimmed stdout.
func runGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
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
	// Worktrees are placed under ProlesDir(cfg) so they pass the isSafeWorktreePath guard.
	addWorktree = func(name string) string {
		wtPath := WorktreePath(cfg, name)
		if err := os.MkdirAll(filepath.Dir(wtPath), 0750); err != nil {
			t.Fatal(err)
		}
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

// --- addWorktreeForProle tests ---

func TestAddWorktreeForProle_freshBranch(t *testing.T) {
	_, _, bareDir, _ := setupPruneEnv(t)
	wtPath := filepath.Join(t.TempDir(), "fresh-wt")

	err := addWorktreeForProle(bareDir, "prole/fresh/standby", wtPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		t.Error("expected worktree directory to be created")
	}
	// Branch must exist in the bare repo as a real ref.
	runGit(t, bareDir, "show-ref", "--verify", "refs/heads/prole/fresh/standby")
}

func TestAddWorktreeForProle_staleBranchReused(t *testing.T) {
	_, _, bareDir, addWorktree := setupPruneEnv(t)

	// Create an initial worktree (simulates a previous prole incarnation).
	stalePath := addWorktree("reuse")
	// Advance the standby branch to a non-origin/main commit so we can prove
	// it gets reset rather than merely reused at its stale pointer.
	runGit(t, stalePath, "config", "user.email", "test@test.com")
	runGit(t, stalePath, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(stalePath, "stale.txt"), []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	runGit(t, stalePath, "add", ".")
	runGit(t, stalePath, "commit", "-m", "stale commit")
	// Remove the worktree but leave the branch (now one commit ahead) behind.
	runGit(t, bareDir, "worktree", "remove", "--force", stalePath)
	// Confirm the branch still exists and is ahead of origin/main.
	runGit(t, bareDir, "show-ref", "--verify", "refs/heads/prole/reuse/standby")

	originMainSHA := runGitOut(t, bareDir, "rev-parse", "origin/main")

	// Re-create a worktree using the same branch name — should succeed.
	newPath := filepath.Join(t.TempDir(), "new-wt")
	err := addWorktreeForProle(bareDir, "prole/reuse/standby", newPath)
	if err != nil {
		t.Fatalf("expected success reusing stale standby branch, got: %v", err)
	}
	if _, err := os.Stat(newPath); os.IsNotExist(err) {
		t.Error("expected new worktree directory to be created")
	}
	// The standby branch must have been reset to origin/main, not left at the stale commit.
	standbySHA := runGitOut(t, bareDir, "rev-parse", "prole/reuse/standby")
	if standbySHA != originMainSHA {
		t.Errorf("standby branch not reset: got %s, want origin/main %s", standbySHA, originMainSHA)
	}
}

func TestAddWorktreeForProle_leavesFeatureBranchAlone(t *testing.T) {
	_, _, bareDir, addWorktree := setupPruneEnv(t)

	// Simulate a previous prole incarnation: create and remove the standby worktree.
	stalePath := addWorktree("foo")
	runGit(t, bareDir, "worktree", "remove", "--force", stalePath)

	// Seed a feature branch at origin/main (represents real in-flight work).
	runGit(t, bareDir, "branch", "prole/foo/NC-99", "origin/main")
	featureSHABefore := runGitOut(t, bareDir, "rev-parse", "prole/foo/NC-99")

	// Recycle foo — must reset /standby but must never touch /NC-99.
	newPath := filepath.Join(t.TempDir(), "new-wt")
	if err := addWorktreeForProle(bareDir, "prole/foo/standby", newPath); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Feature branch must still exist and point at the same commit.
	featureSHAAfter := runGitOut(t, bareDir, "rev-parse", "prole/foo/NC-99")
	if featureSHAAfter != featureSHABefore {
		t.Errorf("feature branch was modified: before=%s after=%s", featureSHABefore, featureSHAAfter)
	}
}

func TestPruneDeadWorktrees_skipsNonProleAgents(t *testing.T) {
	cfg, agents, _, _ := setupPruneEnv(t)

	agents.Register("reviewer", "reviewer", nil)
	agents.UpdateStatus("reviewer", "dead")
	// Give it a fake path — should be skipped because type != "prole"
	agents.SetWorktree("reviewer", "/tmp/fake-path")

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

// --- isSafeWorktreePath tests ---

// makeSafePathCfg returns a Config whose ProjectRoot is a fresh temp dir
// with the expected .company_town/ structure created on disk (Abs resolves
// symlinks on macOS, so the dirs must actually exist).
func makeSafePathCfg(t *testing.T) *config.Config {
	t.Helper()
	root := t.TempDir()
	ctDir := filepath.Join(root, ".company_town")
	for _, sub := range []string{"proles", "repo.git", "db"} {
		if err := os.MkdirAll(filepath.Join(ctDir, sub), 0750); err != nil {
			t.Fatal(err)
		}
	}
	return &config.Config{ProjectRoot: root, TicketPrefix: "nc"}
}

func TestIsSafeWorktreePath_validProle(t *testing.T) {
	cfg := makeSafePathCfg(t)
	path := filepath.Join(ProlesDir(cfg), "iron")
	if err := os.MkdirAll(path, 0750); err != nil {
		t.Fatal(err)
	}
	if !isSafeWorktreePath(cfg, path) {
		t.Errorf("expected path under proles dir to be safe: %s", path)
	}
}

func TestIsSafeWorktreePath_emptyPath(t *testing.T) {
	cfg := makeSafePathCfg(t)
	if isSafeWorktreePath(cfg, "") {
		t.Error("expected empty path to be unsafe")
	}
}

func TestIsSafeWorktreePath_prolesDir(t *testing.T) {
	// The proles dir itself is not a valid worktree target — must be strictly under it.
	cfg := makeSafePathCfg(t)
	if isSafeWorktreePath(cfg, ProlesDir(cfg)) {
		t.Errorf("expected proles dir itself to be unsafe: %s", ProlesDir(cfg))
	}
}

func TestIsSafeWorktreePath_bareRepo(t *testing.T) {
	cfg := makeSafePathCfg(t)
	if isSafeWorktreePath(cfg, BareRepoPath(cfg)) {
		t.Errorf("expected bare repo path to be unsafe: %s", BareRepoPath(cfg))
	}
}

func TestIsSafeWorktreePath_doltDir(t *testing.T) {
	cfg := makeSafePathCfg(t)
	dolt := doltDir(cfg)
	if isSafeWorktreePath(cfg, dolt) {
		t.Errorf("expected dolt dir to be unsafe: %s", dolt)
	}
}

func TestIsSafeWorktreePath_outsideProlesDir(t *testing.T) {
	cfg := makeSafePathCfg(t)
	outside := t.TempDir() // completely separate temp dir
	if isSafeWorktreePath(cfg, outside) {
		t.Errorf("expected path outside proles dir to be unsafe: %s", outside)
	}
}

func TestIsSafeWorktreePath_dotDotEscape(t *testing.T) {
	cfg := makeSafePathCfg(t)
	// A path that enters proles/ then escapes via ".."
	escape := filepath.Join(ProlesDir(cfg), "..", "repo.git")
	if isSafeWorktreePath(cfg, escape) {
		t.Errorf("expected dot-dot escape path to be unsafe: %s", escape)
	}
}

func TestPruneDeadWorktrees_skipsUnsafeWorktreePath(t *testing.T) {
	// A dead prole whose WorktreePath is not under ProlesDir must be skipped
	// with a warning — not passed to git worktree remove.
	cfg, agents, _, _ := setupPruneEnv(t)

	// Use a real directory (so os.Stat succeeds) that is NOT under proles/.
	unsafeDir := t.TempDir()

	agents.Register("unsafe", "prole", nil)
	agents.UpdateStatus("unsafe", "dead")
	agents.SetWorktree("unsafe", unsafeDir)

	logger, buf := capturingLogger()
	pruned, err := PruneDeadWorktrees(cfg, agents, logger)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pruned) != 0 {
		t.Errorf("expected 0 pruned (unsafe path), got %v", pruned)
	}
	if !strings.Contains(buf.String(), "unsafe") {
		t.Errorf("expected warning log mentioning agent name, got: %q", buf.String())
	}
}

// --- List tests ---

func TestList_returnsOnlyProles(t *testing.T) {
	agents := setupAgentRepo(t)
	agents.Register("prole-a", "prole", nil)
	agents.Register("prole-b", "prole", nil)
	agents.Register("mayor", "mayor", nil)
	agents.Register("reviewer", "reviewer", nil)

	cfg := &config.Config{ProjectRoot: t.TempDir()}
	proles, err := List(agents, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(proles) != 2 {
		t.Errorf("expected 2 proles, got %d", len(proles))
	}
	for _, p := range proles {
		if p.Type != "prole" {
			t.Errorf("expected type=prole, got %q", p.Type)
		}
	}
}

func TestList_emptyWhenNoProles(t *testing.T) {
	agents := setupAgentRepo(t)
	agents.Register("mayor", "mayor", nil)

	cfg := &config.Config{ProjectRoot: t.TempDir()}
	proles, err := List(agents, cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(proles) != 0 {
		t.Errorf("expected 0 proles, got %d", len(proles))
	}
}

// --- deployProleCLAUDEMD tests ---

func TestDeployProleCLAUDEMD_substitutesVars(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{ProjectRoot: root, TicketPrefix: "nc"}
	ctDir := config.CompanyTownDir(root)

	templateDir := filepath.Join(ctDir, "agents", "prole")
	if err := os.MkdirAll(templateDir, 0750); err != nil {
		t.Fatal(err)
	}
	template := "Prole {{NAME}} worktree={{WORKTREE_PATH}} prefix={{TICKET_PREFIX}}"
	if err := os.WriteFile(filepath.Join(templateDir, "CLAUDE.md"), []byte(template), 0644); err != nil {
		t.Fatal(err)
	}

	wtPath := "/tmp/worktrees/iron"
	if err := deployProleCLAUDEMD("iron", wtPath, cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dstPath := filepath.Join(ctDir, "proles", "iron", "CLAUDE.md")
	data, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("reading deployed CLAUDE.md: %v", err)
	}
	got := string(data)
	if strings.Contains(got, "{{NAME}}") || strings.Contains(got, "{{WORKTREE_PATH}}") || strings.Contains(got, "{{TICKET_PREFIX}}") {
		t.Errorf("expected all placeholders substituted, got: %q", got)
	}
	if !strings.Contains(got, "iron") || !strings.Contains(got, wtPath) || !strings.Contains(got, "nc") {
		t.Errorf("expected substituted values in output, got: %q", got)
	}
}

func TestDeployProleCLAUDEMD_errorWhenTemplateMissing(t *testing.T) {
	root := t.TempDir()
	cfg := &config.Config{ProjectRoot: root, TicketPrefix: "nc"}
	// No template file created — expect error
	err := deployProleCLAUDEMD("iron", "/tmp/wt", cfg)
	if err == nil {
		t.Error("expected error when template is missing, got nil")
	}
}

// --- mustGetOriginURL tests ---

func TestMustGetOriginURL_fallsBackToGithubRepo(t *testing.T) {
	// t.TempDir() is not a git repo, so getOriginURL will fail.
	cfg := &config.Config{
		ProjectRoot: t.TempDir(),
		GithubRepo:  "git@github.com:org/repo.git",
	}
	got := mustGetOriginURL(cfg)
	if got != "git@github.com:org/repo.git" {
		t.Errorf("expected GithubRepo fallback, got %q", got)
	}
}

// --- currentBranch tests ---

func TestCurrentBranch_returnsActiveBranch(t *testing.T) {
	_, _, _, addWorktree := setupPruneEnv(t)
	wtPath := addWorktree("branchtest")

	branch, err := currentBranch(wtPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if branch != "prole/branchtest/standby" {
		t.Errorf("expected prole/branchtest/standby, got %q", branch)
	}
}

func TestCurrentBranch_errorForNonRepo(t *testing.T) {
	dir := t.TempDir() // not a git repo
	_, err := currentBranch(dir)
	if err == nil {
		t.Error("expected error for non-repo directory")
	}
}

// --- isWorktreeDirty tests ---

func TestIsWorktreeDirty_cleanWorktree(t *testing.T) {
	_, _, _, addWorktree := setupPruneEnv(t)
	wtPath := addWorktree("cleancheck")

	dirty, err := isWorktreeDirty(wtPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dirty {
		t.Error("expected clean worktree to not be dirty")
	}
}

func TestIsWorktreeDirty_dirtyWorktree(t *testing.T) {
	_, _, _, addWorktree := setupPruneEnv(t)
	wtPath := addWorktree("dirtycheck")

	if err := os.WriteFile(filepath.Join(wtPath, "new.txt"), []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}

	dirty, err := isWorktreeDirty(wtPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dirty {
		t.Error("expected worktree with uncommitted file to be dirty")
	}
}

// --- SwitchToBranch tests ---

func TestSwitchToBranch_checksOutExistingBranch(t *testing.T) {
	_, _, bareDir, addWorktree := setupPruneEnv(t)
	wtPath := addWorktree("switchtest")

	// Create a feature branch in the bare repo from origin/main.
	runGit(t, bareDir, "branch", "prole/switchtest/feature-1", "origin/main")

	if err := SwitchToBranch(wtPath, bareDir, "prole/switchtest/feature-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	branch, err := currentBranch(wtPath)
	if err != nil {
		t.Fatalf("reading branch after switch: %v", err)
	}
	if branch != "prole/switchtest/feature-1" {
		t.Errorf("expected prole/switchtest/feature-1, got %q", branch)
	}
}

func TestSwitchToBranch_errorForNonExistentBranch(t *testing.T) {
	_, _, bareDir, addWorktree := setupPruneEnv(t)
	wtPath := addWorktree("switchfail")

	err := SwitchToBranch(wtPath, bareDir, "prole/nonexistent/branch-xyz")
	if err == nil {
		t.Error("expected error for non-existent branch")
	}
}

// --- Reset tests ---

func TestReset_resetsIdleProle(t *testing.T) {
	cfg, agents, bareDir, addWorktree := setupPruneEnv(t)
	wtPath := addWorktree("resetme")
	agents.Register("resetme", "prole", nil)

	// Switch the worktree to a feature branch to confirm Reset brings it back.
	runGit(t, bareDir, "branch", "prole/resetme/nc-99", "origin/main")
	runGit(t, wtPath, "checkout", "prole/resetme/nc-99")

	if err := Reset("resetme", cfg, agents); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	branch, err := currentBranch(wtPath)
	if err != nil {
		t.Fatalf("reading branch after reset: %v", err)
	}
	if branch != "prole/resetme/standby" {
		t.Errorf("expected standby branch after reset, got %q", branch)
	}
}

func TestReset_failsForNonIdleProle(t *testing.T) {
	cfg, agents, _, _ := setupPruneEnv(t)
	agents.Register("busy", "prole", nil)
	agents.UpdateStatus("busy", "working")

	err := Reset("busy", cfg, agents)
	if err == nil {
		t.Error("expected error for non-idle prole, got nil")
	}
}

func TestReset_failsForUnknownProle(t *testing.T) {
	cfg, agents, _, _ := setupPruneEnv(t)
	err := Reset("nonexistent", cfg, agents)
	if err == nil {
		t.Error("expected error for unknown prole, got nil")
	}
}

// --- ResetIdleWorktrees tests ---

func TestResetIdleWorktrees_resetsWorktreeOnFeatureBranch(t *testing.T) {
	cfg, agents, bareDir, addWorktree := setupPruneEnv(t)
	wtPath := addWorktree("idle-feature")
	agents.Register("idle-feature", "prole", nil)
	agents.SetWorktree("idle-feature", wtPath)

	// Put worktree on a feature branch.
	runGit(t, bareDir, "branch", "prole/idle-feature/nc-100", "origin/main")
	runGit(t, wtPath, "checkout", "prole/idle-feature/nc-100")

	if err := ResetIdleWorktrees(cfg, agents, silentLogger()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	branch, err := currentBranch(wtPath)
	if err != nil {
		t.Fatalf("reading branch after ResetIdleWorktrees: %v", err)
	}
	if branch != "prole/idle-feature/standby" {
		t.Errorf("expected standby branch after idle reset, got %q", branch)
	}
}

func TestResetIdleWorktrees_skipsCleanStandbyWorktree(t *testing.T) {
	cfg, agents, _, addWorktree := setupPruneEnv(t)
	wtPath := addWorktree("idle-clean")
	agents.Register("idle-clean", "prole", nil)
	agents.SetWorktree("idle-clean", wtPath)
	// Worktree is already on standby and clean — nothing to do.

	if err := ResetIdleWorktrees(cfg, agents, silentLogger()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Branch should still be standby (no reset happened).
	branch, _ := currentBranch(wtPath)
	if branch != "prole/idle-clean/standby" {
		t.Errorf("expected standby branch unchanged, got %q", branch)
	}
}

func TestResetIdleWorktrees_logsWarningForUnsafePath(t *testing.T) {
	cfg, agents, _, _ := setupPruneEnv(t)

	// Register an idle prole with a worktree path outside proles/.
	unsafeDir := t.TempDir()
	agents.Register("unsafe-idle", "prole", nil)
	agents.SetWorktree("unsafe-idle", unsafeDir)

	logger, buf := capturingLogger()
	if err := ResetIdleWorktrees(cfg, agents, logger); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "unsafe-idle") {
		t.Errorf("expected warning log mentioning agent name, got: %q", buf.String())
	}
}

// --- InstallPreCommitHook tests ---

func TestInstallPreCommitHook_installsHook(t *testing.T) {
	cfg, _, _, addWorktree := setupPruneEnv(t)
	wtPath := addWorktree("hooktest")

	// Create scripts/pre-commit in the project root.
	scriptsDir := filepath.Join(cfg.ProjectRoot, "scripts")
	if err := os.MkdirAll(scriptsDir, 0750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scriptsDir, "pre-commit"), []byte("#!/bin/sh\n"), 0750); err != nil {
		t.Fatal(err)
	}

	InstallPreCommitHook(cfg.ProjectRoot, wtPath)

	// Verify the hook was installed in the worktree's gitdir.
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = wtPath
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse --git-dir: %v", err)
	}
	gitDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(wtPath, gitDir)
	}
	hookDst := filepath.Join(gitDir, "hooks", "pre-commit")
	if _, err := os.Stat(hookDst); os.IsNotExist(err) {
		t.Errorf("expected pre-commit hook at %s, not found", hookDst)
	}
}

func TestInstallPreCommitHook_silentWhenSourceAbsent(t *testing.T) {
	_, _, _, addWorktree := setupPruneEnv(t)
	wtPath := addWorktree("nohooktest")
	root := t.TempDir() // no scripts/pre-commit here

	// Must return silently without panicking.
	InstallPreCommitHook(root, wtPath)
}

// --- EnsureBareRepo tests ---

func TestEnsureBareRepo_fetchesWhenBareRepoExists(t *testing.T) {
	cfg, _, _, _ := setupPruneEnv(t)
	// The bare repo already exists (created by setupPruneEnv).
	// EnsureBareRepo should run git fetch origin without error.
	if err := EnsureBareRepo(cfg); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
