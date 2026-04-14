package repo_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

func newDriftDB(t *testing.T) (*repo.AgentRepo, *repo.IssueRepo) {
	t.Helper()
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return repo.NewAgentRepo(conn, nil), repo.NewIssueRepo(conn, nil)
}

func TestCheckDrift_noDrift(t *testing.T) {
	agents, issues := newDriftDB(t)
	agents.Register("copper", "prole", nil)

	id, _ := issues.Create("test", "task", nil, nil, nil)
	issues.UpdateStatus(id, "in_progress")
	issues.SetAssignee(id, "copper")
	agents.SetCurrentIssue("copper", &id)

	entries, err := repo.CheckDrift(agents, issues, "nc")
	if err != nil {
		t.Fatalf("CheckDrift: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no drift, got: %v", entries)
	}
}

func TestCheckDrift_idleWithPointer(t *testing.T) {
	agents, issues := newDriftDB(t)
	agents.Register("copper", "prole", nil)

	id, _ := issues.Create("test", "task", nil, nil, nil)
	issues.UpdateStatus(id, "open")
	issues.SetAssignee(id, "copper")
	agents.SetCurrentIssue("copper", &id)
	agents.UpdateStatus("copper", "idle") // set idle but current_issue still set

	entries, err := repo.CheckDrift(agents, issues, "nc")
	if err != nil {
		t.Fatalf("CheckDrift: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 drift entry, got %d: %v", len(entries), entries)
	}
	if entries[0].AgentName != "copper" {
		t.Errorf("expected AgentName=copper, got %q", entries[0].AgentName)
	}
	if !strings.Contains(entries[0].Reason, "idle") {
		t.Errorf("expected reason to mention idle, got %q", entries[0].Reason)
	}
	if !strings.Contains(entries[0].Reason, "nc-"+intStr(id)) {
		t.Errorf("expected reason to mention ticket id, got %q", entries[0].Reason)
	}
}

func TestCheckDrift_workingOnClosedTicket(t *testing.T) {
	agents, issues := newDriftDB(t)
	agents.Register("iron", "prole", nil)

	id, _ := issues.Create("done", "task", nil, nil, nil)
	issues.UpdateStatus(id, "closed")
	agents.SetCurrentIssue("iron", &id)
	// agent is working (SetCurrentIssue sets working)

	entries, err := repo.CheckDrift(agents, issues, "nc")
	if err != nil {
		t.Fatalf("CheckDrift: %v", err)
	}
	found := false
	for _, e := range entries {
		if e.AgentName == "iron" && strings.Contains(e.Reason, "closed") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected drift entry for iron working on closed ticket, got: %v", entries)
	}
}

func TestCheckDrift_pointingAtOtherAgentsTicket(t *testing.T) {
	agents, issues := newDriftDB(t)
	agents.Register("copper", "prole", nil)
	agents.Register("tin", "prole", nil)

	id, _ := issues.Create("test", "task", nil, nil, nil)
	issues.UpdateStatus(id, "in_progress")
	issues.SetAssignee(id, "tin")         // assigned to tin
	agents.SetCurrentIssue("copper", &id) // copper's current_issue points at it

	entries, err := repo.CheckDrift(agents, issues, "nc")
	if err != nil {
		t.Fatalf("CheckDrift: %v", err)
	}

	var found *repo.DriftEntry
	for i := range entries {
		if entries[i].AgentName == "copper" {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected drift entry for copper, got: %v", entries)
	}
	// Pin the category by asserting on key fragments of the Reason string.
	// This catches the wrong category firing (e.g. IdleWithPointer instead of
	// PointingAtOtherAssignee) because each category produces a distinct sentence.
	if !strings.Contains(found.Reason, "assigned to tin") {
		t.Errorf("expected reason to indicate assignment to tin, got: %q", found.Reason)
	}
	if !strings.Contains(found.Reason, "nc-"+intStr(id)) {
		t.Errorf("expected reason to include ticket ref, got: %q", found.Reason)
	}
}

func TestCheckDrift_noPointerSkipped(t *testing.T) {
	agents, issues := newDriftDB(t)
	agents.Register("copper", "prole", nil)
	// No SetCurrentIssue — current_issue is NULL.

	entries, err := repo.CheckDrift(agents, issues, "nc")
	if err != nil {
		t.Fatalf("CheckDrift: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected no drift for agent with no current issue, got: %v", entries)
	}
}

func intStr(id int) string {
	return strconv.Itoa(id)
}
