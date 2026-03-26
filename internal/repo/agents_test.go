package repo

import (
	"testing"

	"github.com/katerina7479/company_town/internal/db"
)

func setupAgentRepo(t *testing.T) *AgentRepo {
	t.Helper()
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("creating test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return NewAgentRepo(conn)
}

func TestAgentRepo_Register(t *testing.T) {
	repo := setupAgentRepo(t)

	if err := repo.Register("test-agent", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}

	agent, err := repo.Get("test-agent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if agent.Name != "test-agent" {
		t.Errorf("expected name='test-agent', got %q", agent.Name)
	}
	if agent.Type != "prole" {
		t.Errorf("expected type='prole', got %q", agent.Type)
	}
	if agent.Status != "idle" {
		t.Errorf("expected status='idle', got %q", agent.Status)
	}
}

func TestAgentRepo_UpdateStatus(t *testing.T) {
	repo := setupAgentRepo(t)

	repo.Register("test-agent", "prole", nil)

	if err := repo.UpdateStatus("test-agent", "working"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	agent, _ := repo.Get("test-agent")
	if agent.Status != "working" {
		t.Errorf("expected status='working', got %q", agent.Status)
	}
}

func TestAgentRepo_ListByStatus(t *testing.T) {
	repo := setupAgentRepo(t)

	repo.Register("agent1", "prole", nil)
	repo.Register("agent2", "prole", nil)
	repo.Register("agent3", "prole", nil)

	repo.UpdateStatus("agent2", "working")

	idle, err := repo.ListByStatus("idle")
	if err != nil {
		t.Fatalf("ListByStatus: %v", err)
	}
	if len(idle) != 2 {
		t.Errorf("expected 2 idle agents, got %d", len(idle))
	}

	working, _ := repo.ListByStatus("working")
	if len(working) != 1 {
		t.Errorf("expected 1 working agent, got %d", len(working))
	}
}

func TestAgentRepo_FindIdle(t *testing.T) {
	repo := setupAgentRepo(t)

	backend := "backend"
	frontend := "frontend"

	repo.Register("backend1", "prole", &backend)
	repo.Register("backend2", "prole", &backend)
	repo.Register("frontend1", "prole", &frontend)

	repo.UpdateStatus("backend1", "working")

	// Find all idle
	all, err := repo.FindIdle(nil)
	if err != nil {
		t.Fatalf("FindIdle: %v", err)
	}
	if len(all) != 2 {
		t.Errorf("expected 2 idle agents, got %d", len(all))
	}

	// Find idle backend
	backends, _ := repo.FindIdle(&backend)
	if len(backends) != 1 {
		t.Errorf("expected 1 idle backend, got %d", len(backends))
	}
}
