package gtcmd

import (
	"testing"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
	"github.com/katerina7479/company_town/internal/session"
)

// TestStart_SpawnsAgentIdle verifies that startAgentWithDeps sets agent status
// to "idle" (not "working") after spawning the session, guarding against regression.
func TestStart_SpawnsAgentIdle(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	defer conn.Close()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		ProjectRoot:  tmpDir,
		TicketPrefix: "test",
		Agents: config.AgentsConfig{
			Architect: config.AgentConfig{Model: "claude-test"},
		},
	}
	agents := repo.NewAgentRepo(conn, nil)

	// Stub out session creation so no real tmux session is attempted.
	oldCreate := createInteractiveFn
	defer func() { createInteractiveFn = oldCreate }()
	createInteractiveFn = func(_ session.AgentSessionConfig) error { return nil }

	// Stub out tmux existence check so agent is not considered already running.
	oldTmux := tmuxExistsFn
	defer func() { tmuxExistsFn = oldTmux }()
	tmuxExistsFn = func(_ string) bool { return false }

	// Stub out worktree provisioning — no real git repo in test env.
	oldEnsure := ensureAgentWorktreeFn
	defer func() { ensureAgentWorktreeFn = oldEnsure }()
	ensureAgentWorktreeFn = func(_ *config.Config, agentDir string) (string, error) {
		return agentDir + "/worktree", nil
	}

	if err := startAgentWithDeps(cfg, agents, "architect"); err != nil {
		t.Fatalf("startAgentWithDeps: %v", err)
	}

	a, err := agents.Get("architect")
	if err != nil {
		t.Fatalf("agents.Get(architect): %v", err)
	}
	if a.Status != "idle" {
		t.Errorf("expected status=idle after Start, got %q", a.Status)
	}
}

// TestStart_WorkDirIsWorktree verifies that the session is created with the
// worktree path as WorkDir, not cfg.ProjectRoot.
func TestStart_WorkDirIsWorktree(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	defer conn.Close()

	tmpDir := t.TempDir()
	cfg := &config.Config{
		ProjectRoot:  tmpDir,
		TicketPrefix: "test",
		Agents: config.AgentsConfig{
			Reviewer: config.AgentConfig{Model: "claude-test"},
		},
	}
	agents := repo.NewAgentRepo(conn, nil)

	const fakeWorktree = "/fake/reviewer/worktree"

	var capturedWorkDir string
	oldCreate := createInteractiveFn
	defer func() { createInteractiveFn = oldCreate }()
	createInteractiveFn = func(c session.AgentSessionConfig) error {
		capturedWorkDir = c.WorkDir
		return nil
	}

	oldTmux := tmuxExistsFn
	defer func() { tmuxExistsFn = oldTmux }()
	tmuxExistsFn = func(_ string) bool { return false }

	oldEnsure := ensureAgentWorktreeFn
	defer func() { ensureAgentWorktreeFn = oldEnsure }()
	ensureAgentWorktreeFn = func(_ *config.Config, _ string) (string, error) {
		return fakeWorktree, nil
	}

	if err := startAgentWithDeps(cfg, agents, "reviewer"); err != nil {
		t.Fatalf("startAgentWithDeps: %v", err)
	}

	if capturedWorkDir != fakeWorktree {
		t.Errorf("session WorkDir = %q, want worktree path %q", capturedWorkDir, fakeWorktree)
	}
	if capturedWorkDir == tmpDir {
		t.Errorf("session WorkDir must not be cfg.ProjectRoot after nc-128 fix")
	}
}
