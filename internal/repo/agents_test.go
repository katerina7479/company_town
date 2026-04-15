package repo

import (
	"errors"
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
	return NewAgentRepo(conn, nil)
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

func TestAgentRepo_SetCurrentIssue(t *testing.T) {
	repo := setupAgentRepo(t)

	repo.Register("worker", "prole", nil)

	issueID := 42
	if err := repo.SetCurrentIssue("worker", &issueID); err != nil {
		t.Fatalf("SetCurrentIssue: %v", err)
	}

	agent, _ := repo.Get("worker")
	if agent.Status != "working" {
		t.Errorf("expected status='working', got %q", agent.Status)
	}
	if !agent.CurrentIssue.Valid || int(agent.CurrentIssue.Int64) != issueID {
		t.Errorf("expected current_issue=%d, got %v", issueID, agent.CurrentIssue)
	}
}

func TestAgentRepo_SetCurrentIssue_nil(t *testing.T) {
	repo := setupAgentRepo(t)

	repo.Register("worker", "prole", nil)
	issueID := 7
	repo.SetCurrentIssue("worker", &issueID)

	// Passing nil clears current_issue
	if err := repo.SetCurrentIssue("worker", nil); err != nil {
		t.Fatalf("SetCurrentIssue(nil): %v", err)
	}

	agent, _ := repo.Get("worker")
	if agent.CurrentIssue.Valid {
		t.Errorf("expected current_issue=NULL after nil set, got %d", agent.CurrentIssue.Int64)
	}
}

func TestAgentRepo_ClearCurrentIssue(t *testing.T) {
	repo := setupAgentRepo(t)

	repo.Register("worker", "prole", nil)
	issueID := 5
	repo.SetCurrentIssue("worker", &issueID)

	if err := repo.ClearCurrentIssue("worker"); err != nil {
		t.Fatalf("ClearCurrentIssue: %v", err)
	}

	agent, _ := repo.Get("worker")
	if agent.Status != "idle" {
		t.Errorf("expected status='idle', got %q", agent.Status)
	}
	if agent.CurrentIssue.Valid {
		t.Errorf("expected current_issue=NULL, got %d", agent.CurrentIssue.Int64)
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

func TestAgentRepo_Get_notFound(t *testing.T) {
	r := setupAgentRepo(t)

	_, err := r.Get("nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent agent, got nil")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestAgentRepo_Register_withSpecialty(t *testing.T) {
	repo := setupAgentRepo(t)

	spec := "backend"
	if err := repo.Register("backend1", "prole", &spec); err != nil {
		t.Fatalf("Register: %v", err)
	}

	agent, err := repo.Get("backend1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !agent.Specialty.Valid || agent.Specialty.String != "backend" {
		t.Errorf("expected specialty='backend', got %v", agent.Specialty)
	}
}

func TestAgentRepo_UpdateStatus_dead_setsTimeEnded(t *testing.T) {
	repo := setupAgentRepo(t)

	repo.Register("worker", "prole", nil)

	if err := repo.UpdateStatus("worker", "dead"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	agent, _ := repo.Get("worker")
	if agent.Status != "dead" {
		t.Errorf("expected status='dead', got %q", agent.Status)
	}
	if !agent.TimeEnded.Valid {
		t.Errorf("expected time_ended to be set when status=dead")
	}
}

func TestAgentRepo_UpdateStatus_notDead_leavesTimeEndedNull(t *testing.T) {
	repo := setupAgentRepo(t)

	repo.Register("worker", "prole", nil)
	repo.UpdateStatus("worker", "working")

	agent, _ := repo.Get("worker")
	if agent.TimeEnded.Valid {
		t.Errorf("expected time_ended NULL for non-dead status, got %v", agent.TimeEnded.Time)
	}
}

func TestAgentRepo_SetWorktree(t *testing.T) {
	repo := setupAgentRepo(t)

	repo.Register("worker", "prole", nil)

	if err := repo.SetWorktree("worker", "/tmp/worktrees/worker"); err != nil {
		t.Fatalf("SetWorktree: %v", err)
	}

	agent, _ := repo.Get("worker")
	if !agent.WorktreePath.Valid || agent.WorktreePath.String != "/tmp/worktrees/worker" {
		t.Errorf("expected worktree_path='/tmp/worktrees/worker', got %v", agent.WorktreePath)
	}
}

func TestAgentRepo_SetTmuxSession(t *testing.T) {
	repo := setupAgentRepo(t)

	repo.Register("worker", "prole", nil)

	if err := repo.SetTmuxSession("worker", "ct-worker"); err != nil {
		t.Fatalf("SetTmuxSession: %v", err)
	}

	agent, _ := repo.Get("worker")
	if !agent.TmuxSession.Valid || agent.TmuxSession.String != "ct-worker" {
		t.Errorf("expected tmux_session='ct-worker', got %v", agent.TmuxSession)
	}
}

func TestAgentRepo_ListAll(t *testing.T) {
	repo := setupAgentRepo(t)

	repo.Register("agent1", "prole", nil)
	repo.Register("agent2", "reviewer", nil)
	repo.UpdateStatus("agent2", "dead")

	all, err := repo.ListAll()
	if err != nil {
		t.Fatalf("ListAll: %v", err)
	}
	// ListAll returns ALL agents regardless of status
	if len(all) != 2 {
		t.Errorf("expected 2 agents, got %d", len(all))
	}
}

func TestAgentRepo_CountByType(t *testing.T) {
	repo := setupAgentRepo(t)

	repo.Register("prole1", "prole", nil)
	repo.Register("prole2", "prole", nil)
	repo.Register("reviewer1", "reviewer", nil)

	// Dead agents are excluded
	repo.Register("prole3", "prole", nil)
	repo.UpdateStatus("prole3", "dead")

	count, err := repo.CountByType("prole")
	if err != nil {
		t.Fatalf("CountByType: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 live proles, got %d", count)
	}

	reviewerCount, err := repo.CountByType("reviewer")
	if err != nil {
		t.Fatalf("CountByType reviewer: %v", err)
	}
	if reviewerCount != 1 {
		t.Errorf("expected 1 reviewer, got %d", reviewerCount)
	}
}
