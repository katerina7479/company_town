package commands

import (
	"testing"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

// TestStartAgent_resetsDeadStatusWhenSessionExists verifies that when a tmux
// session is already live but the DB shows the agent as dead, startAgent resets
// the status to idle before attaching.
func TestStartAgent_resetsDeadStatusWhenSessionExists(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	defer conn.Close()

	cfg := &config.Config{
		ProjectRoot: t.TempDir(),
		Agents: config.AgentsConfig{
			Mayor: config.AgentConfig{Model: "claude-test"},
		},
	}
	agents := repo.NewAgentRepo(conn, nil)

	// Pre-register the agent and mark it dead.
	if err := agents.Register("mayor", "mayor", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := agents.UpdateStatus("mayor", "dead"); err != nil {
		t.Fatalf("UpdateStatus dead: %v", err)
	}

	// Stub session existence: session is live.
	oldExists := sessionExistsFn
	defer func() { sessionExistsFn = oldExists }()
	sessionExistsFn = func(_ string) bool { return true }

	// Stub attach: no-op so no real tmux call is made.
	oldAttach := sessionAttachFn
	defer func() { sessionAttachFn = oldAttach }()
	sessionAttachFn = func(_ string) error { return nil }

	if err := startAgent("mayor", "mayor", "claude-test", cfg, agents, "prompt"); err != nil {
		t.Fatalf("startAgent: %v", err)
	}

	a, err := agents.Get("mayor")
	if err != nil {
		t.Fatalf("agents.Get(mayor): %v", err)
	}
	if a.Status != "idle" {
		t.Errorf("expected status=idle after start with dead DB entry, got %q", a.Status)
	}
}

// TestStartAgent_doesNotOverwriteWorkingStatus verifies that if the agent is
// already working (not dead) and its session is live, startAgent does not
// downgrade the status to idle.
func TestStartAgent_doesNotOverwriteWorkingStatus(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	defer conn.Close()

	cfg := &config.Config{
		ProjectRoot: t.TempDir(),
		Agents: config.AgentsConfig{
			Mayor: config.AgentConfig{Model: "claude-test"},
		},
	}
	agents := repo.NewAgentRepo(conn, nil)

	if err := agents.Register("mayor", "mayor", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := agents.UpdateStatus("mayor", "working"); err != nil {
		t.Fatalf("UpdateStatus working: %v", err)
	}

	oldExists := sessionExistsFn
	defer func() { sessionExistsFn = oldExists }()
	sessionExistsFn = func(_ string) bool { return true }

	oldAttach := sessionAttachFn
	defer func() { sessionAttachFn = oldAttach }()
	sessionAttachFn = func(_ string) error { return nil }

	if err := startAgent("mayor", "mayor", "claude-test", cfg, agents, "prompt"); err != nil {
		t.Fatalf("startAgent: %v", err)
	}

	a, err := agents.Get("mayor")
	if err != nil {
		t.Fatalf("agents.Get(mayor): %v", err)
	}
	if a.Status != "working" {
		t.Errorf("expected status=working to be preserved, got %q", a.Status)
	}
}
