package prole

import (
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
	return repo.NewAgentRepo(conn)
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
