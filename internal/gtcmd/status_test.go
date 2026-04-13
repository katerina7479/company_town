package gtcmd

import (
	"testing"

	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

func TestReconcileDeadAgents(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	defer conn.Close()

	agents := repo.NewAgentRepo(conn, nil)

	// alive prole: tmux session exists, currently working — should stay
	agents.Register("alive", "prole", nil)
	agents.SetTmuxSession("alive", "ct-alive")
	issueID := 1
	agents.SetCurrentIssue("alive", &issueID)

	// ghost prole: tmux session is gone — should be deleted
	agents.Register("ghost", "prole", nil)
	agents.SetTmuxSession("ghost", "ct-ghost")
	issueID2 := 2
	agents.SetCurrentIssue("ghost", &issueID2)

	// no-session prole: never had a tmux session — should be deleted
	agents.Register("no-session", "prole", nil)

	// dead-reviewer: core agent whose session is gone — should be marked dead, not deleted
	agents.Register("reviewer", "reviewer", nil)
	agents.SetTmuxSession("reviewer", "ct-reviewer")
	issueID3 := 3
	agents.SetCurrentIssue("reviewer", &issueID3)

	// already-dead core agent: should be left alone (not re-processed)
	agents.Register("architect", "architect", nil)
	agents.SetTmuxSession("architect", "ct-architect")
	agents.UpdateStatus("architect", "dead")

	sessionExists := func(name string) bool {
		return name == "ct-alive"
	}

	if err := reconcileDeadAgents(agents, sessionExists); err != nil {
		t.Fatalf("reconcileDeadAgents: %v", err)
	}

	alive, err := agents.Get("alive")
	if err != nil {
		t.Fatalf("alive prole should still exist: %v", err)
	}
	if alive.Status != "working" {
		t.Errorf("alive: expected status='working', got %q", alive.Status)
	}
	if !alive.CurrentIssue.Valid || alive.CurrentIssue.Int64 != 1 {
		t.Errorf("alive: expected current_issue=1, got %v", alive.CurrentIssue)
	}

	if _, err := agents.Get("ghost"); err == nil {
		t.Errorf("ghost prole: expected to be deleted, still present")
	}

	if _, err := agents.Get("no-session"); err == nil {
		t.Errorf("no-session prole: expected to be deleted, still present")
	}

	reviewer, err := agents.Get("reviewer")
	if err != nil {
		t.Fatalf("reviewer should still exist (core agents marked dead, not deleted): %v", err)
	}
	if reviewer.Status != "dead" {
		t.Errorf("reviewer: expected status='dead', got %q", reviewer.Status)
	}
	if reviewer.CurrentIssue.Valid {
		t.Errorf("reviewer: expected current_issue=NULL, got %d", reviewer.CurrentIssue.Int64)
	}

	architect, err := agents.Get("architect")
	if err != nil {
		t.Fatalf("architect should still exist: %v", err)
	}
	if architect.Status != "dead" {
		t.Errorf("architect: expected status='dead', got %q", architect.Status)
	}
}
