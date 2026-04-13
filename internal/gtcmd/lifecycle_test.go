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
