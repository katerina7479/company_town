package daemon

import (
	"io"
	"log"
	"testing"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

func newTestDaemon(t *testing.T) (*Daemon, *repo.IssueRepo, *repo.AgentRepo) {
	t.Helper()
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("creating test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	cfg := &config.Config{
		TicketPrefix: "NC",
		ProjectRoot:  t.TempDir(),
	}

	issues := repo.NewIssueRepo(conn)
	agents := repo.NewAgentRepo(conn)

	d := &Daemon{
		cfg:    cfg,
		issues: issues,
		agents: agents,
		logger: log.New(io.Discard, "", 0),
		stop:   make(chan struct{}),
	}

	return d, issues, agents
}

func TestHandlePRMerged_closesTicket(t *testing.T) {
	d, issues, _ := newTestDaemon(t)

	id, err := issues.Create("Test ticket", "task", nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := issues.UpdateStatus(id, "in_review"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if err := issues.SetPR(id, 42); err != nil {
		t.Fatalf("SetPR: %v", err)
	}

	issue, err := issues.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	d.handlePRMerged(issue)

	updated, err := issues.Get(id)
	if err != nil {
		t.Fatalf("Get after merge: %v", err)
	}
	if updated.Status != "closed" {
		t.Errorf("expected status=closed, got %q", updated.Status)
	}
}

func TestHandlePRMerged_noopIfAlreadyClosed(t *testing.T) {
	d, issues, _ := newTestDaemon(t)

	id, _ := issues.Create("Already closed", "task", nil, nil)
	issues.UpdateStatus(id, "closed")
	issues.SetPR(id, 99)

	issue, _ := issues.Get(id)
	d.handlePRMerged(issue)

	updated, _ := issues.Get(id)
	if updated.Status != "closed" {
		t.Errorf("expected status=closed, got %q", updated.Status)
	}
}

func TestHandlePRMerged_freesAssigneeAgent(t *testing.T) {
	d, issues, agents := newTestDaemon(t)

	if err := agents.Register("obsidian", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}

	id, _ := issues.Create("Test ticket", "task", nil, nil)
	if err := issues.Assign(id, "obsidian", "prole/obsidian/NC-11"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	issues.SetPR(id, 42)
	if err := agents.SetCurrentIssue("obsidian", &id); err != nil {
		t.Fatalf("SetCurrentIssue: %v", err)
	}

	issue, _ := issues.Get(id)
	d.handlePRMerged(issue)

	agent, err := agents.Get("obsidian")
	if err != nil {
		t.Fatalf("Get agent: %v", err)
	}
	if agent.Status != "idle" {
		t.Errorf("expected agent status=idle, got %q", agent.Status)
	}
	if agent.CurrentIssue.Valid {
		t.Errorf("expected current_issue=NULL, got %d", agent.CurrentIssue.Int64)
	}
}

func TestHandlePRMerged_noAssigneeIsOk(t *testing.T) {
	d, issues, _ := newTestDaemon(t)

	id, _ := issues.Create("Unassigned ticket", "task", nil, nil)
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 55)

	issue, _ := issues.Get(id)
	// Should not panic or error when no assignee
	d.handlePRMerged(issue)

	updated, _ := issues.Get(id)
	if updated.Status != "closed" {
		t.Errorf("expected status=closed, got %q", updated.Status)
	}
}

func TestHandlePRClosed_doesNotCloseTicket(t *testing.T) {
	d, issues, _ := newTestDaemon(t)

	id, _ := issues.Create("Test ticket", "task", nil, nil)
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 77)

	issue, _ := issues.Get(id)
	d.handlePRClosed(issue)

	// Ticket should remain in_review — daemon escalates to Mayor but doesn't change status
	updated, _ := issues.Get(id)
	if updated.Status != "in_review" {
		t.Errorf("expected status=in_review, got %q", updated.Status)
	}
}

func TestHandlePRClosed_noopIfAlreadyClosed(t *testing.T) {
	d, issues, _ := newTestDaemon(t)

	id, _ := issues.Create("Test ticket", "task", nil, nil)
	issues.UpdateStatus(id, "closed")
	issues.SetPR(id, 88)

	issue, _ := issues.Get(id)
	// Should return early without error
	d.handlePRClosed(issue)
}

func TestListWithPRs_onlyReturnsNonClosed(t *testing.T) {
	_, issues, _ := newTestDaemon(t)

	// Open ticket with PR
	id1, _ := issues.Create("Open with PR", "task", nil, nil)
	issues.UpdateStatus(id1, "in_review")
	issues.SetPR(id1, 10)

	// Closed ticket with PR — should NOT appear
	id2, _ := issues.Create("Closed with PR", "task", nil, nil)
	issues.UpdateStatus(id2, "closed")
	issues.SetPR(id2, 11)

	// Open ticket without PR — should NOT appear
	id3, _ := issues.Create("Open no PR", "task", nil, nil)
	issues.UpdateStatus(id3, "open")

	result, err := issues.ListWithPRs()
	if err != nil {
		t.Fatalf("ListWithPRs: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 ticket, got %d", len(result))
	}
	if result[0].ID != id1 {
		t.Errorf("expected ticket %d, got %d", id1, result[0].ID)
	}
}
