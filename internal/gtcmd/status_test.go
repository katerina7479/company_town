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

	agents := repo.NewAgentRepo(conn)

	// alive: tmux session exists, currently working on an issue — should stay
	agents.Register("alive", "prole", nil)
	agents.SetTmuxSession("alive", "ct-alive")
	issueID := 1
	agents.SetCurrentIssue("alive", &issueID)

	// ghost: tmux session is gone, was working on an issue — should flip to dead
	agents.Register("ghost", "prole", nil)
	agents.SetTmuxSession("ghost", "ct-ghost")
	issueID2 := 2
	agents.SetCurrentIssue("ghost", &issueID2)

	// no-session: never had a tmux session — should be left alone
	agents.Register("no-session", "prole", nil)

	// already-dead: already dead, should be skipped entirely
	agents.Register("already-dead", "prole", nil)
	agents.SetTmuxSession("already-dead", "ct-already-dead")
	agents.UpdateStatus("already-dead", "dead")

	sessionExists := func(name string) bool {
		return name == "ct-alive"
	}

	if err := reconcileDeadAgents(agents, sessionExists); err != nil {
		t.Fatalf("reconcileDeadAgents: %v", err)
	}

	alive, _ := agents.Get("alive")
	if alive.Status != "working" {
		t.Errorf("alive: expected status='working', got %q", alive.Status)
	}
	if !alive.CurrentIssue.Valid || alive.CurrentIssue.Int64 != 1 {
		t.Errorf("alive: expected current_issue=1, got %v", alive.CurrentIssue)
	}

	ghost, _ := agents.Get("ghost")
	if ghost.Status != "dead" {
		t.Errorf("ghost: expected status='dead', got %q", ghost.Status)
	}
	if ghost.CurrentIssue.Valid {
		t.Errorf("ghost: expected current_issue=NULL, got %d", ghost.CurrentIssue.Int64)
	}

	noSession, _ := agents.Get("no-session")
	if noSession.Status != "idle" {
		t.Errorf("no-session: expected status='idle', got %q", noSession.Status)
	}

	alreadyDead, _ := agents.Get("already-dead")
	if alreadyDead.Status != "dead" {
		t.Errorf("already-dead: expected status='dead', got %q", alreadyDead.Status)
	}
}
