package gtcmd

import (
	"fmt"
	"strings"
	"testing"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

func setupTicketTestRepo(t *testing.T) *repo.IssueRepo {
	t.Helper()
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("creating test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return repo.NewIssueRepo(conn, nil)
}

func setupTicketTestRepos(t *testing.T) (*repo.IssueRepo, *repo.AgentRepo) {
	t.Helper()
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("creating test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return repo.NewIssueRepo(conn, nil), repo.NewAgentRepo(conn, nil)
}

// withStubSession swaps the tmux helpers used by ticketAssign for in-memory
// fakes and restores them after the test. Returns a pointer to the slice of
// captured sendKeys calls.
func withStubSession(t *testing.T, liveSessions map[string]bool) *[]struct{ session, msg string } {
	t.Helper()
	origExists := assignSessionExists
	origSend := assignSendKeys
	sent := &[]struct{ session, msg string }{}
	assignSessionExists = func(name string) bool { return liveSessions[name] }
	assignSendKeys = func(name, msg string) error {
		*sent = append(*sent, struct{ session, msg string }{name, msg})
		return nil
	}
	t.Cleanup(func() {
		assignSessionExists = origExists
		assignSendKeys = origSend
	})
	return sent
}

func TestTicketCreate_withDescription(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketCreate(issues, "nc", []string{"My ticket", "--description", "Some details here."})
	if err != nil {
		t.Fatalf("ticketCreate: %v", err)
	}

	issue, err := issues.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !issue.Description.Valid || issue.Description.String != "Some details here." {
		t.Errorf("expected description %q, got %q (valid=%v)",
			"Some details here.", issue.Description.String, issue.Description.Valid)
	}
}

func TestTicketCreate_withoutDescription(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketCreate(issues, "nc", []string{"No desc ticket"})
	if err != nil {
		t.Fatalf("ticketCreate: %v", err)
	}

	issue, err := issues.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if issue.Description.Valid && issue.Description.String != "" {
		t.Errorf("expected description to be empty, got %q", issue.Description.String)
	}
}

func TestTicketCreate_descriptionMissingValue(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketCreate(issues, "nc", []string{"My ticket", "--description"})
	if err == nil {
		t.Fatal("expected error when --description has no value")
	}
	if !strings.Contains(err.Error(), "--description requires a value") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTicketDescribe(t *testing.T) {
	issues := setupTicketTestRepo(t)

	issues.Create("Test ticket", "task", nil, nil, nil)

	err := ticketDescribe(issues, []string{"1", "Updated description."})
	if err != nil {
		t.Fatalf("ticketDescribe: %v", err)
	}

	issue, err := issues.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !issue.Description.Valid || issue.Description.String != "Updated description." {
		t.Errorf("expected %q, got %q (valid=%v)",
			"Updated description.", issue.Description.String, issue.Description.Valid)
	}
}

func TestTicketDescribe_notFound(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketDescribe(issues, []string{"9999", "anything"})
	if err == nil {
		t.Fatal("expected error for non-existent issue")
	}
}

func TestTicketDescribe_missingArgs(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketDescribe(issues, []string{"1"})
	if err == nil {
		t.Fatal("expected error when description argument is missing")
	}
	if !strings.Contains(err.Error(), "usage:") {
		t.Errorf("expected usage error, got: %v", err)
	}
}

func TestTicketDescribe_noArgs(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketDescribe(issues, []string{})
	if err == nil {
		t.Fatal("expected error when no args provided")
	}
}

func TestTicketPrioritize_happyPath(t *testing.T) {
	issues := setupTicketTestRepo(t)

	id, err := issues.Create("A task", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ticketPrioritize(issues, []string{"1", "P1"}); err != nil {
		t.Fatalf("ticketPrioritize: %v", err)
	}

	issue, err := issues.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !issue.Priority.Valid || issue.Priority.String != "P1" {
		t.Errorf("expected priority='P1', got %v", issue.Priority)
	}
}

func TestTicketPrioritize_allValidPriorities(t *testing.T) {
	for _, p := range []string{"P0", "P1", "P2", "P3"} {
		t.Run(p, func(t *testing.T) {
			issues := setupTicketTestRepo(t)

			_, err := issues.Create("A task", "task", nil, nil, nil)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}

			if err := ticketPrioritize(issues, []string{"1", p}); err != nil {
				t.Errorf("ticketPrioritize with %q: unexpected error: %v", p, err)
			}
		})
	}
}

func TestTicketPrioritize_invalidPriority(t *testing.T) {
	issues := setupTicketTestRepo(t)

	_, err := issues.Create("A task", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	err = ticketPrioritize(issues, []string{"1", "P5"})
	if err == nil {
		t.Fatal("expected error for invalid priority 'P5', got nil")
	}
}

func TestTicketPrioritize_notFound(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketPrioritize(issues, []string{"9999", "P0"})
	if err == nil {
		t.Fatal("expected error for non-existent ticket, got nil")
	}
}

func TestTicketReview_ApproveFromUnderReview(t *testing.T) {
	issues := setupTicketTestRepo(t)

	id, err := issues.Create("auth ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := issues.Assign(id, "iron", "prole/iron/1"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if err := issues.UpdateStatus(id, "under_review"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	if err := ticketReview(issues, []string{fmt.Sprintf("%d", id), "approve"}); err != nil {
		t.Fatalf("ticketReview approve: %v", err)
	}

	got, err := issues.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "pr_open" {
		t.Errorf("status: got %q, want pr_open", got.Status)
	}
	if !got.Assignee.Valid || got.Assignee.String != "iron" {
		t.Errorf("assignee: got %v %q, want iron", got.Assignee.Valid, got.Assignee.String)
	}
}

func TestTicketReview_RequestChangesFromUnderReview(t *testing.T) {
	issues := setupTicketTestRepo(t)

	id, err := issues.Create("auth ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := issues.Assign(id, "iron", "prole/iron/1"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if err := issues.UpdateStatus(id, "under_review"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	if err := ticketReview(issues, []string{fmt.Sprintf("%d", id), "request-changes"}); err != nil {
		t.Fatalf("ticketReview request-changes: %v", err)
	}

	got, err := issues.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Status != "repairing" {
		t.Errorf("status: got %q, want repairing", got.Status)
	}
	if !got.Assignee.Valid || got.Assignee.String != "iron" {
		t.Errorf("assignee: got %v %q, want iron", got.Assignee.Valid, got.Assignee.String)
	}
}

func TestTicketReview_RejectsNonUnderReview(t *testing.T) {
	issues := setupTicketTestRepo(t)

	id, err := issues.Create("ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for _, status := range []string{"open", "in_progress", "in_review", "pr_open", "repairing"} {
		if err := issues.UpdateStatus(id, status); err != nil {
			t.Fatalf("UpdateStatus(%s): %v", status, err)
		}
		err := ticketReview(issues, []string{fmt.Sprintf("%d", id), "approve"})
		if err == nil {
			t.Errorf("status %q: expected error, got nil", status)
		} else if !strings.Contains(err.Error(), "not under_review") {
			t.Errorf("status %q: unexpected error: %v", status, err)
		}
	}
}

func TestTicketReview_RejectsUnknownVerdict(t *testing.T) {
	issues := setupTicketTestRepo(t)

	id, err := issues.Create("ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := issues.UpdateStatus(id, "under_review"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	err = ticketReview(issues, []string{fmt.Sprintf("%d", id), "lgtm"})
	if err == nil {
		t.Fatal("expected error for unknown verdict, got nil")
	}
	if !strings.Contains(err.Error(), "unknown verdict") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestTicketStatus_NoLongerClobbersAssignee(t *testing.T) {
	issues := setupTicketTestRepo(t)

	id, err := issues.Create("ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := issues.Assign(id, "iron", "prole/iron/1"); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	// Move to under_review — assignee must remain "iron"
	if err := ticketStatus(issues, []string{fmt.Sprintf("%d", id), "under_review"}); err != nil {
		t.Fatalf("ticketStatus under_review: %v", err)
	}
	got, _ := issues.Get(id)
	if !got.Assignee.Valid || got.Assignee.String != "iron" {
		t.Errorf("after under_review: assignee=%v %q, want iron", got.Assignee.Valid, got.Assignee.String)
	}

	// Move to pr_open — assignee must remain "iron"
	if err := ticketStatus(issues, []string{fmt.Sprintf("%d", id), "pr_open"}); err != nil {
		t.Fatalf("ticketStatus pr_open: %v", err)
	}
	got, _ = issues.Get(id)
	if !got.Assignee.Valid || got.Assignee.String != "iron" {
		t.Errorf("after pr_open: assignee=%v %q, want iron", got.Assignee.Valid, got.Assignee.String)
	}

	// Move to repairing from a fresh under_review — assignee must remain "iron"
	if err := ticketStatus(issues, []string{fmt.Sprintf("%d", id), "under_review"}); err != nil {
		t.Fatalf("ticketStatus under_review (2): %v", err)
	}
	if err := ticketStatus(issues, []string{fmt.Sprintf("%d", id), "repairing"}); err != nil {
		t.Fatalf("ticketStatus repairing: %v", err)
	}
	got, _ = issues.Get(id)
	if !got.Assignee.Valid || got.Assignee.String != "iron" {
		t.Errorf("after repairing: assignee=%v %q, want iron", got.Assignee.Valid, got.Assignee.String)
	}
}

func TestTicketPrioritize_missingArgs(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketPrioritize(issues, []string{})
	if err == nil {
		t.Fatal("expected usage error for 0 args, got nil")
	}

	err = ticketPrioritize(issues, []string{"1"})
	if err == nil {
		t.Fatal("expected usage error for 1 arg, got nil")
	}
}

func TestTicketPrioritize_prefixedID(t *testing.T) {
	issues := setupTicketTestRepo(t)

	id, err := issues.Create("A task", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ticketPrioritize(issues, []string{"nc-1", "P2"}); err != nil {
		t.Fatalf("ticketPrioritize with prefixed id: %v", err)
	}

	issue, err := issues.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !issue.Priority.Valid || issue.Priority.String != "P2" {
		t.Errorf("expected priority='P2', got %v", issue.Priority)
	}
}

func TestTicketAssign_nudgesAgentAndLeavesStatusAlone(t *testing.T) {
	issues, agents := setupTicketTestRepos(t)

	if err := agents.Register("copper", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := agents.SetTmuxSession("copper", "ct-prole-copper"); err != nil {
		t.Fatalf("SetTmuxSession: %v", err)
	}
	// Prole starts idle — ticketAssign must NOT flip it to working.
	id, err := issues.Create("Build thing", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	sent := withStubSession(t, map[string]bool{"ct-prole-copper": true})

	if err := ticketAssign(&config.Config{TicketPrefix: "nc"}, issues, agents, []string{fmt.Sprintf("%d", id), "copper"}); err != nil {
		t.Fatalf("ticketAssign: %v", err)
	}

	agent, err := agents.Get("copper")
	if err != nil {
		t.Fatalf("Get agent: %v", err)
	}
	if agent.Status != "idle" {
		t.Errorf("expected agent status 'idle' (prole owns its own status), got %q", agent.Status)
	}
	if agent.CurrentIssue.Valid {
		t.Errorf("expected current_issue NULL (prole sets it on pickup), got %d", agent.CurrentIssue.Int64)
	}

	issue, _ := issues.Get(id)
	if !issue.Assignee.Valid || issue.Assignee.String != "copper" {
		t.Errorf("expected ticket assignee='copper', got %v", issue.Assignee)
	}
	if issue.Status != "draft" {
		// Assign no longer transitions status; prole acknowledges explicitly.
		t.Errorf("expected ticket status unchanged ('draft'), got %q", issue.Status)
	}

	if len(*sent) != 1 {
		t.Fatalf("expected 1 sendKeys call, got %d", len(*sent))
	}
	if (*sent)[0].session != "ct-prole-copper" {
		t.Errorf("expected nudge to ct-prole-copper, got %q", (*sent)[0].session)
	}
	if !strings.Contains((*sent)[0].msg, fmt.Sprintf("ticket %d", id)) {
		t.Errorf("expected nudge msg to mention ticket %d, got %q", id, (*sent)[0].msg)
	}
}

func TestTicketAssign_skipsNudgeWhenSessionMissing(t *testing.T) {
	issues, agents := setupTicketTestRepos(t)

	if err := agents.Register("copper", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := agents.SetTmuxSession("copper", "ct-prole-copper"); err != nil {
		t.Fatalf("SetTmuxSession: %v", err)
	}
	id, err := issues.Create("Build thing", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Empty live-session map — nudge should be skipped without erroring.
	sent := withStubSession(t, map[string]bool{})

	if err := ticketAssign(&config.Config{TicketPrefix: "nc"}, issues, agents, []string{fmt.Sprintf("%d", id), "copper"}); err != nil {
		t.Fatalf("ticketAssign should not error when session is gone: %v", err)
	}

	if len(*sent) != 0 {
		t.Errorf("expected 0 sendKeys calls, got %d", len(*sent))
	}

	// Ticket should still be properly assigned even though nudge failed.
	issue, _ := issues.Get(id)
	if !issue.Assignee.Valid || issue.Assignee.String != "copper" {
		t.Errorf("expected ticket assignee='copper', got %v", issue.Assignee)
	}
	if issue.Status != "draft" {
		// Assign no longer transitions status; prole acknowledges explicitly.
		t.Errorf("expected ticket status unchanged ('draft'), got %q", issue.Status)
	}
}
