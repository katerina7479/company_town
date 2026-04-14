package agentworktree_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/katerina7479/company_town/internal/agentworktree"
	"github.com/katerina7479/company_town/internal/config"
)

// runGit runs a git command and fails the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
}

// setupTestRepo creates a minimal local git repo that acts as both the "remote"
// origin and the project root, with a single commit on main so that
// `git worktree add origin/main` has something to check out.
// Returns the config pointing at the project root.
func setupTestRepo(t *testing.T) *config.Config {
	t.Helper()

	root := t.TempDir()

	// Create a bare repo to act as origin.
	remoteDir := filepath.Join(root, "remote.git")
	runGit(t, root, "init", "--bare", remoteDir)

	// Clone it as the "project" checkout, make a commit, push.
	projectDir := filepath.Join(root, "project")
	runGit(t, root, "clone", remoteDir, projectDir)
	runGit(t, projectDir, "config", "user.email", "test@test.com")
	runGit(t, projectDir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(projectDir, "README"), []byte("hello"), 0644); err != nil {
		t.Fatalf("writing README: %v", err)
	}
	runGit(t, projectDir, "add", ".")
	runGit(t, projectDir, "commit", "-m", "init")
	runGit(t, projectDir, "push", "origin", "main")

	// Create .company_town dir so config can find the project root.
	ctDir := filepath.Join(projectDir, ".company_town")
	if err := os.MkdirAll(ctDir, 0755); err != nil {
		t.Fatalf("creating .company_town: %v", err)
	}

	return &config.Config{ProjectRoot: projectDir}
}

func TestPath(t *testing.T) {
	agentDir := "/project/.company_town/agents/architect"
	got := agentworktree.Path(agentDir)
	want := "/project/.company_town/agents/architect/worktree"
	if got != want {
		t.Errorf("Path(%q) = %q, want %q", agentDir, got, want)
	}
}

func TestEnsure_CreatesWorktree(t *testing.T) {
	cfg := setupTestRepo(t)

	agentDir := filepath.Join(cfg.ProjectRoot, ".company_town", "agents", "architect")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("creating agentDir: %v", err)
	}

	wtPath, err := agentworktree.Ensure(cfg, agentDir)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	// Worktree should exist and be a valid git checkout.
	if _, err := os.Stat(wtPath); err != nil {
		t.Errorf("worktree path %q does not exist: %v", wtPath, err)
	}

	// Verify it's a detached HEAD worktree.
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = wtPath
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git rev-parse in worktree: %v", err)
	}
	// Detached HEAD reports "HEAD" rather than a branch name.
	head := string(out)
	if head != "HEAD\n" {
		t.Errorf("expected detached HEAD, got %q", head)
	}

	// Path helper should return the same path.
	if agentworktree.Path(agentDir) != wtPath {
		t.Errorf("Path(%q) = %q, Ensure returned %q", agentDir, agentworktree.Path(agentDir), wtPath)
	}
}

func TestEnsure_IdempotentOnSecondCall(t *testing.T) {
	cfg := setupTestRepo(t)

	agentDir := filepath.Join(cfg.ProjectRoot, ".company_town", "agents", "reviewer")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("creating agentDir: %v", err)
	}

	// First call — creates the worktree.
	if _, err := agentworktree.Ensure(cfg, agentDir); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}

	// Second call — should not fail (worktree already exists, just fetches).
	// The fetch will fail in the test env (no real remote), but the worktree
	// already exists so the overall call should succeed.
	if _, err := agentworktree.Ensure(cfg, agentDir); err != nil {
		t.Fatalf("second Ensure (idempotency): %v", err)
	}
}

func TestEnsure_WorktreeInsideAgentDir(t *testing.T) {
	cfg := setupTestRepo(t)

	agentDir := filepath.Join(cfg.ProjectRoot, ".company_town", "agents", "mayor")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("creating agentDir: %v", err)
	}

	wtPath, err := agentworktree.Ensure(cfg, agentDir)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	// Worktree must be a direct sub-directory of agentDir.
	parent := filepath.Dir(wtPath)
	if parent != agentDir {
		t.Errorf("worktree parent = %q, want agentDir %q", parent, agentDir)
	}
}

func TestEnsure_CLAUDEMDInParentDiscoverableFromWorktree(t *testing.T) {
	cfg := setupTestRepo(t)

	agentDir := filepath.Join(cfg.ProjectRoot, ".company_town", "agents", "architect")
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		t.Fatalf("creating agentDir: %v", err)
	}

	// Place a CLAUDE.md in agentDir (simulating the deployed template).
	claudeMD := filepath.Join(agentDir, "CLAUDE.md")
	if err := os.WriteFile(claudeMD, []byte("# Agent"), 0644); err != nil {
		t.Fatalf("writing CLAUDE.md: %v", err)
	}

	wtPath, err := agentworktree.Ensure(cfg, agentDir)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	// CLAUDE.md is one directory above the worktree. Walking up from wtPath
	// should find it, which is how Claude Code discovers project instructions.
	found := filepath.Join(filepath.Dir(wtPath), "CLAUDE.md")
	if _, err := os.Stat(found); err != nil {
		t.Errorf("CLAUDE.md not reachable from worktree parent: %v", err)
	}
}
