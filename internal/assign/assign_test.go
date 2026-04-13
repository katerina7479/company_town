package assign

import (
	"errors"
	"testing"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

func setupRepos(t *testing.T) (*repo.IssueRepo, *repo.AgentRepo) {
	t.Helper()
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("creating test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return repo.NewIssueRepo(conn, nil), repo.NewAgentRepo(conn, nil)
}

func TestExecute_existingProle(t *testing.T) {
	issues, agents := setupRepos(t)

	// Pre-register the prole agent.
	if err := agents.Register("copper", "prole", nil); err != nil {
		t.Fatalf("registering agent: %v", err)
	}

	// Create a ticket to assign.
	ticketID, err := issues.Create("test ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}

	// ProleCreator must NOT be called when the prole already exists.
	orig := ProleCreator
	t.Cleanup(func() { ProleCreator = orig })
	ProleCreator = func(name string, cfg *config.Config, a *repo.AgentRepo) error {
		t.Errorf("ProleCreator called unexpectedly for existing prole %q", name)
		return nil
	}

	cfg := &config.Config{TicketPrefix: "nc"}
	if err := Execute(cfg, issues, agents, ticketID, "copper"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Assert issue has correct assignee and branch.
	issue, err := issues.Get(ticketID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if !issue.Assignee.Valid || issue.Assignee.String != "copper" {
		t.Errorf("expected assignee=copper, got %v", issue.Assignee)
	}
	wantBranch := "prole/copper/nc-1"
	if !issue.Branch.Valid || issue.Branch.String != wantBranch {
		t.Errorf("expected branch=%q, got %v", wantBranch, issue.Branch)
	}

	// Agent status and current_issue are left alone — proles own their own status.
	agent, err := agents.Get("copper")
	if err != nil {
		t.Fatalf("getting agent: %v", err)
	}
	if agent.Status != "idle" {
		t.Errorf("expected status unchanged (idle), got %q", agent.Status)
	}
	if agent.CurrentIssue.Valid {
		t.Errorf("expected current_issue unchanged (NULL), got %d", agent.CurrentIssue.Int64)
	}
}

func TestExecute_newProle(t *testing.T) {
	issues, agents := setupRepos(t)

	ticketID, err := issues.Create("test ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}

	var creatorCalls int
	orig := ProleCreator
	t.Cleanup(func() { ProleCreator = orig })
	ProleCreator = func(name string, cfg *config.Config, a *repo.AgentRepo) error {
		creatorCalls++
		// Simulate what prole.Create does: register the agent.
		return a.Register(name, "prole", nil)
	}

	cfg := &config.Config{TicketPrefix: "nc"}
	if err := Execute(cfg, issues, agents, ticketID, "zinc"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if creatorCalls != 1 {
		t.Errorf("expected ProleCreator called exactly once, got %d", creatorCalls)
	}

	issue, err := issues.Get(ticketID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if !issue.Assignee.Valid || issue.Assignee.String != "zinc" {
		t.Errorf("expected assignee=zinc, got %v", issue.Assignee)
	}

	// Agent status and current_issue are left alone — proles own their own status.
	agent, err := agents.Get("zinc")
	if err != nil {
		t.Fatalf("getting agent: %v", err)
	}
	if agent.Status != "idle" {
		t.Errorf("expected status unchanged (idle), got %q", agent.Status)
	}
	if agent.CurrentIssue.Valid {
		t.Errorf("expected current_issue unchanged (NULL), got %d", agent.CurrentIssue.Int64)
	}
}

func TestExecute_proleCreateFails(t *testing.T) {
	issues, agents := setupRepos(t)

	ticketID, err := issues.Create("test ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}

	orig := ProleCreator
	t.Cleanup(func() { ProleCreator = orig })
	ProleCreator = func(name string, cfg *config.Config, a *repo.AgentRepo) error {
		return errors.New("simulated prole create failure")
	}

	cfg := &config.Config{TicketPrefix: "nc"}
	err = Execute(cfg, issues, agents, ticketID, "zinc")
	if err == nil {
		t.Fatal("expected error when ProleCreator fails, got nil")
	}

	// issues.Assign must not have been called — issue should have no assignee.
	issue, err := issues.Get(ticketID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if issue.Assignee.Valid {
		t.Errorf("expected no assignee after ProleCreator failure, got %q", issue.Assignee.String)
	}
}
