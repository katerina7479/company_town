package gtcmd

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
	"github.com/katerina7479/company_town/internal/session"
)

// newReviewerTestDB returns a test DB connection and a minimal config for reviewer tests.
func newReviewerTestDB(t *testing.T) (*sql.DB, *config.Config) {
	t.Helper()
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	cfg := &config.Config{
		ProjectRoot:  t.TempDir(),
		TicketPrefix: "nc",
		Agents: config.AgentsConfig{
			Reviewer: config.AgentConfig{Model: "claude-sonnet-4-6"},
		},
	}
	return conn, cfg
}

func TestCreateReviewer_sessionAlreadyExists(t *testing.T) {
	origExists := tmuxExistsFn
	defer func() { tmuxExistsFn = origExists }()
	tmuxExistsFn = func(string) bool { return true }

	conn, cfg := newReviewerTestDB(t)
	agents := repo.NewAgentRepo(conn, nil)

	err := createReviewerWithDeps("robin", cfg, agents)
	if err == nil {
		t.Fatal("expected error when session already exists")
	}
	if !errors.Is(err, ErrSessionAlreadyExists) {
		t.Errorf("expected ErrSessionAlreadyExists, got: %v", err)
	}
}

func TestCreateReviewer_success(t *testing.T) {
	origCreate := createInteractiveFn
	origExists := tmuxExistsFn
	defer func() {
		createInteractiveFn = origCreate
		tmuxExistsFn = origExists
	}()

	var capturedCfg session.AgentSessionConfig
	createInteractiveFn = func(cfg session.AgentSessionConfig) error {
		capturedCfg = cfg
		return nil
	}
	tmuxExistsFn = func(string) bool { return false }

	conn, cfg := newReviewerTestDB(t)
	agents := repo.NewAgentRepo(conn, nil)

	err := createReviewerWithDeps("robin", cfg, agents)
	if err != nil {
		t.Fatalf("createReviewerWithDeps: %v", err)
	}

	if capturedCfg.Name != "ct-robin" {
		t.Errorf("session name = %q, want %q", capturedCfg.Name, "ct-robin")
	}
	if capturedCfg.EnvVars["CT_AGENT_NAME"] != "robin" {
		t.Errorf("CT_AGENT_NAME = %q, want %q", capturedCfg.EnvVars["CT_AGENT_NAME"], "robin")
	}

	agent, err := agents.Get("robin")
	if err != nil {
		t.Fatalf("agents.Get(robin): %v", err)
	}
	if agent.Type != "reviewer" {
		t.Errorf("agent type = %q, want %q", agent.Type, "reviewer")
	}
	if agent.Status != "idle" {
		t.Errorf("agent status = %q, want %q", agent.Status, "idle")
	}
	if !agent.TmuxSession.Valid || agent.TmuxSession.String != "ct-robin" {
		t.Errorf("tmux_session = %v, want ct-robin", agent.TmuxSession)
	}
	if !strings.Contains(capturedCfg.Prompt, "robin") {
		t.Errorf("prompt %q does not mention reviewer name", capturedCfg.Prompt)
	}
}

func TestCreateReviewer_sessionCreateFails(t *testing.T) {
	origCreate := createInteractiveFn
	origExists := tmuxExistsFn
	defer func() {
		createInteractiveFn = origCreate
		tmuxExistsFn = origExists
	}()

	createInteractiveFn = func(_ session.AgentSessionConfig) error {
		return fmt.Errorf("simulated session create failure")
	}
	tmuxExistsFn = func(string) bool { return false }

	conn, cfg := newReviewerTestDB(t)
	agents := repo.NewAgentRepo(conn, nil)

	err := createReviewerWithDeps("jay", cfg, agents)
	if err == nil {
		t.Fatal("expected error when session creation fails")
	}
	if !strings.Contains(err.Error(), "creating session") {
		t.Errorf("error should mention 'creating session': %v", err)
	}
}

func TestCreateReviewer_idempotentDBRegistration(t *testing.T) {
	origCreate := createInteractiveFn
	origExists := tmuxExistsFn
	defer func() {
		createInteractiveFn = origCreate
		tmuxExistsFn = origExists
	}()

	createInteractiveFn = func(cfg session.AgentSessionConfig) error { return nil }
	tmuxExistsFn = func(string) bool { return false }

	conn, cfg := newReviewerTestDB(t)
	agents := repo.NewAgentRepo(conn, nil)

	// Pre-register so the Get succeeds
	if err := agents.Register("jay", "reviewer", nil); err != nil {
		t.Fatalf("pre-register: %v", err)
	}

	// Should not error even though agent already exists in DB
	if err := createReviewerWithDeps("jay", cfg, agents); err != nil {
		t.Fatalf("createReviewerWithDeps: %v", err)
	}

	agent, err := agents.Get("jay")
	if err != nil {
		t.Fatalf("agents.Get: %v", err)
	}
	if agent.Status != "idle" {
		t.Errorf("status = %q, want idle", agent.Status)
	}
}
