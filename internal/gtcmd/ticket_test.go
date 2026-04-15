package gtcmd

import (
	"errors"
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

func TestTicketCreate_flagsBeforeTitle(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketCreate(issues, "nc", []string{"--type", "bug", "--priority", "P0", "My real title"})
	if err != nil {
		t.Fatalf("ticketCreate: %v", err)
	}

	issue, err := issues.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if issue.Title != "My real title" {
		t.Errorf("expected title %q, got %q", "My real title", issue.Title)
	}
	if issue.IssueType != "bug" {
		t.Errorf("expected type %q, got %q", "bug", issue.IssueType)
	}
	if !issue.Priority.Valid || issue.Priority.String != "P0" {
		t.Errorf("expected priority P0, got %q (valid=%v)", issue.Priority.String, issue.Priority.Valid)
	}
}

func TestTicketCreate_flagsInterleaved(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketCreate(issues, "nc", []string{"--type", "bug", "Title in middle", "--priority", "P1"})
	if err != nil {
		t.Fatalf("ticketCreate: %v", err)
	}

	issue, err := issues.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if issue.Title != "Title in middle" {
		t.Errorf("expected title %q, got %q", "Title in middle", issue.Title)
	}
	if issue.IssueType != "bug" {
		t.Errorf("expected type %q, got %q", "bug", issue.IssueType)
	}
	if !issue.Priority.Valid || issue.Priority.String != "P1" {
		t.Errorf("expected priority P1, got %q", issue.Priority.String)
	}
}

func TestTicketCreate_missingTitle(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketCreate(issues, "nc", []string{"--type", "bug", "--priority", "P0"})
	if err == nil {
		t.Fatal("expected error when title is missing")
	}
	if !errors.Is(err, ErrTitleRequired) {
		t.Errorf("expected ErrTitleRequired, got: %v", err)
	}
}

func TestTicketCreate_multiplePositionals(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketCreate(issues, "nc", []string{"first", "second"})
	if err == nil {
		t.Fatal("expected error when multiple positional args are supplied")
	}
	if !errors.Is(err, ErrExpectedOneTitle) {
		t.Errorf("expected ErrExpectedOneTitle, got: %v", err)
	}
}

func TestTicketCreate_unknownFlag(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketCreate(issues, "nc", []string{"--bogus", "x", "title"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
	if !errors.Is(err, ErrUnknownFlag) {
		t.Errorf("expected ErrUnknownFlag, got: %v", err)
	}
}

func TestTicketCreate_descriptionMissingValue(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketCreate(issues, "nc", []string{"My ticket", "--description"})
	if err == nil {
		t.Fatal("expected error when --description has no value")
	}
	if !errors.Is(err, ErrDescriptionRequired) {
		t.Errorf("expected ErrDescriptionRequired, got: %v", err)
	}
}

func TestTicketCreate_invalidType(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketCreate(issues, "nc", []string{"test", "--type", "garbage"})
	if err == nil {
		t.Fatal("expected error for invalid --type, got nil")
	}
	if !errors.Is(err, ErrInvalidType) {
		t.Errorf("expected ErrInvalidType, got: %v", err)
	}
	// No ticket should have been created.
	all, _ := issues.List("open")
	if len(all) != 0 {
		t.Errorf("expected no tickets created, got %d", len(all))
	}
}

func TestTicketCreate_validType(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketCreate(issues, "nc", []string{"test", "--type", "bug"})
	if err != nil {
		t.Fatalf("unexpected error for valid --type: %v", err)
	}
	issue, err := issues.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if issue.IssueType != "bug" {
		t.Errorf("expected issue_type='bug', got %q", issue.IssueType)
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

	err = ticketPrioritize(issues, []string{"1", "P6"})
	if err == nil {
		t.Fatal("expected error for invalid priority 'P6', got nil")
	}
}

func TestTicketPrioritize_p4p5Valid(t *testing.T) {
	issues := setupTicketTestRepo(t)

	id, err := issues.Create("A task", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	for _, p := range []string{"P4", "P5"} {
		if err := ticketPrioritize(issues, []string{fmt.Sprintf("%d", id), p}); err != nil {
			t.Errorf("ticketPrioritize with %s: unexpected error: %v", p, err)
		}
	}
}

func TestTicketCreate_defaultPriorityP3(t *testing.T) {
	issues := setupTicketTestRepo(t)

	if err := ticketCreate(issues, "nc", []string{"A task"}); err != nil {
		t.Fatalf("ticketCreate: %v", err)
	}

	got, err := issues.Get(1)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Priority.Valid || got.Priority.String != "P3" {
		t.Errorf("expected default priority P3, got %v", got.Priority)
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
		} else if !errors.Is(err, ErrNotUnderReview) {
			t.Errorf("status %q: expected ErrNotUnderReview, got: %v", status, err)
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
	if !errors.Is(err, ErrUnknownVerdict) {
		t.Errorf("expected ErrUnknownVerdict, got: %v", err)
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
	if err := ticketStatus(issues, nil, []string{fmt.Sprintf("%d", id), "under_review"}); err != nil {
		t.Fatalf("ticketStatus under_review: %v", err)
	}
	got, _ := issues.Get(id)
	if !got.Assignee.Valid || got.Assignee.String != "iron" {
		t.Errorf("after under_review: assignee=%v %q, want iron", got.Assignee.Valid, got.Assignee.String)
	}

	// Move to pr_open — assignee must remain "iron"
	if err := ticketStatus(issues, nil, []string{fmt.Sprintf("%d", id), "pr_open"}); err != nil {
		t.Fatalf("ticketStatus pr_open: %v", err)
	}
	got, _ = issues.Get(id)
	if !got.Assignee.Valid || got.Assignee.String != "iron" {
		t.Errorf("after pr_open: assignee=%v %q, want iron", got.Assignee.Valid, got.Assignee.String)
	}

	// Move to repairing from a fresh under_review — assignee must remain "iron"
	if err := ticketStatus(issues, nil, []string{fmt.Sprintf("%d", id), "under_review"}); err != nil {
		t.Fatalf("ticketStatus under_review (2): %v", err)
	}
	if err := ticketStatus(issues, nil, []string{fmt.Sprintf("%d", id), "repairing"}); err != nil {
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

func TestTicketType_happyPath(t *testing.T) {
	issues := setupTicketTestRepo(t)

	id, err := issues.Create("A task", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ticketType(issues, []string{"1", "bug"}); err != nil {
		t.Fatalf("ticketType: %v", err)
	}

	issue, err := issues.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if issue.IssueType != "bug" {
		t.Errorf("expected issue_type='bug', got %q", issue.IssueType)
	}
}

func TestTicketType_allValidTypes(t *testing.T) {
	for _, typ := range repo.ValidTypes {
		t.Run(typ, func(t *testing.T) {
			issues := setupTicketTestRepo(t)

			_, err := issues.Create("A task", "task", nil, nil, nil)
			if err != nil {
				t.Fatalf("Create: %v", err)
			}

			if err := ticketType(issues, []string{"1", typ}); err != nil {
				t.Errorf("ticketType with %q: unexpected error: %v", typ, err)
			}
		})
	}
}

func TestTicketType_invalidType(t *testing.T) {
	issues := setupTicketTestRepo(t)

	_, err := issues.Create("A task", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	err = ticketType(issues, []string{"1", "feature"})
	if err == nil {
		t.Fatal("expected error for invalid type 'feature', got nil")
	}
}

func TestTicketType_notFound(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketType(issues, []string{"9999", "bug"})
	if err == nil {
		t.Fatal("expected error for non-existent ticket, got nil")
	}
}

func TestTicketType_missingArgs(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketType(issues, []string{})
	if err == nil {
		t.Fatal("expected usage error for 0 args, got nil")
	}

	err = ticketType(issues, []string{"1"})
	if err == nil {
		t.Fatal("expected usage error for 1 arg, got nil")
	}
}

func TestTicketType_prefixedID(t *testing.T) {
	issues := setupTicketTestRepo(t)

	id, err := issues.Create("A task", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ticketType(issues, []string{"nc-1", "refactor"}); err != nil {
		t.Fatalf("ticketType with prefixed id: %v", err)
	}

	issue, err := issues.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if issue.IssueType != "refactor" {
		t.Errorf("expected issue_type='refactor', got %q", issue.IssueType)
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

// --- NC-60: gt ticket priority alias ---

func TestTicketPrioritize_priorityAlias(t *testing.T) {
	// Route through ticketDispatch (the inner dispatcher) so that the
	// `case "prioritize", "priority":` line is on the critical path.
	// A regression that drops "priority" from the case would leave the
	// old TestTicketPrioritize_* tests passing but break this one.
	issues, agents := setupTicketTestRepos(t)
	cfg := &config.Config{TicketPrefix: "nc"}

	id, err := issues.Create("Some ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := ticketDispatch(issues, agents, cfg, []string{"priority", fmt.Sprintf("%d", id), "P1"}); err != nil {
		t.Fatalf("ticketDispatch priority alias: %v", err)
	}

	got, err := issues.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Priority.Valid || got.Priority.String != "P1" {
		t.Errorf("expected priority=P1, got %v", got.Priority)
	}
}

func TestTicketUnassign_clearsAssignee(t *testing.T) {
	issues := setupTicketTestRepo(t)

	id, err := issues.Create("Some task", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := issues.SetAssignee(id, "iron"); err != nil {
		t.Fatalf("SetAssignee: %v", err)
	}
	if err := issues.UpdateStatus(id, "in_progress"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	if err := ticketUnassign(issues, []string{fmt.Sprintf("%d", id)}); err != nil {
		t.Fatalf("ticketUnassign: %v", err)
	}

	issue, err := issues.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if issue.Assignee.Valid {
		t.Errorf("expected assignee=NULL, got %q", issue.Assignee.String)
	}
	// Status must be unchanged — unassign does not transition status.
	if issue.Status != "in_progress" {
		t.Errorf("expected status unchanged ('in_progress'), got %q", issue.Status)
	}
}

func TestTicketUnassign_notFound(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketUnassign(issues, []string{"9999"})
	if err == nil {
		t.Fatal("expected error for non-existent ticket, got nil")
	}
}

func TestTicketUnassign_missingArg(t *testing.T) {
	issues := setupTicketTestRepo(t)

	err := ticketUnassign(issues, []string{})
	if err == nil {
		t.Fatal("expected usage error, got nil")
	}
}

func TestTicketUnassign_prefixedID(t *testing.T) {
	issues := setupTicketTestRepo(t)

	id, err := issues.Create("Prefixed task", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := issues.SetAssignee(id, "copper"); err != nil {
		t.Fatalf("SetAssignee: %v", err)
	}

	// Use "nc-N" form — parseTicketID should strip the prefix.
	if err := ticketUnassign(issues, []string{fmt.Sprintf("nc-%d", id)}); err != nil {
		t.Fatalf("ticketUnassign with prefixed ID: %v", err)
	}

	issue, _ := issues.Get(id)
	if issue.Assignee.Valid {
		t.Errorf("expected assignee=NULL, got %q", issue.Assignee.String)
	}
}

func TestTicketUndepend_happyPath(t *testing.T) {
	issues := setupTicketTestRepo(t)
	cfg := &config.Config{TicketPrefix: "nc"}

	id1, _ := issues.Create("Ticket 1", "task", nil, nil, nil)
	id2, _ := issues.Create("Ticket 2", "task", nil, nil, nil)

	// Add dependency then remove it via the handler
	if err := ticketDepend(issues, cfg.TicketPrefix, []string{
		fmt.Sprintf("%d", id2), fmt.Sprintf("%d", id1),
	}); err != nil {
		t.Fatalf("ticketDepend: %v", err)
	}

	if err := ticketUndepend(issues, cfg.TicketPrefix, []string{
		fmt.Sprintf("%d", id2), fmt.Sprintf("%d", id1),
	}); err != nil {
		t.Fatalf("ticketUndepend: %v", err)
	}

	deps, _ := issues.GetDependencies(id2)
	if len(deps) != 0 {
		t.Errorf("expected no deps after undepend, got %v", deps)
	}
}

func TestTicketUndepend_idempotent(t *testing.T) {
	issues := setupTicketTestRepo(t)
	cfg := &config.Config{TicketPrefix: "nc"}

	id1, _ := issues.Create("Ticket 1", "task", nil, nil, nil)
	id2, _ := issues.Create("Ticket 2", "task", nil, nil, nil)

	// undepend with no prior depend edge — should succeed silently
	if err := ticketUndepend(issues, cfg.TicketPrefix, []string{
		fmt.Sprintf("%d", id2), fmt.Sprintf("%d", id1),
	}); err != nil {
		t.Errorf("ticketUndepend on non-existent edge should succeed, got %v", err)
	}
}

func TestTicketUndepend_invalidArgs(t *testing.T) {
	issues := setupTicketTestRepo(t)
	cfg := &config.Config{TicketPrefix: "nc"}

	if err := ticketUndepend(issues, cfg.TicketPrefix, []string{"1"}); err == nil {
		t.Error("expected error for missing second arg")
	}
	if err := ticketUndepend(issues, cfg.TicketPrefix, []string{}); err == nil {
		t.Error("expected error for empty args")
	}
}

func TestTicketUndepend_nonexistentTicket(t *testing.T) {
	issues := setupTicketTestRepo(t)
	cfg := &config.Config{TicketPrefix: "nc"}

	id1, _ := issues.Create("Real", "task", nil, nil, nil)

	// Second arg is a ticket ID that doesn't exist.
	err := ticketUndepend(issues, cfg.TicketPrefix, []string{
		fmt.Sprintf("%d", id1), "9999",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent ticket, got nil")
	}
	if !strings.Contains(err.Error(), "9999") {
		t.Errorf("error should mention missing ticket id, got: %v", err)
	}
}

func TestTicketUndepend_prefixedIDs(t *testing.T) {
	issues := setupTicketTestRepo(t)
	cfg := &config.Config{TicketPrefix: "nc"}

	id1, _ := issues.Create("Ticket 1", "task", nil, nil, nil)
	id2, _ := issues.Create("Ticket 2", "task", nil, nil, nil)
	issues.AddDependency(id2, id1)

	// Use "nc-N" form — parseTicketID should strip the prefix
	if err := ticketUndepend(issues, cfg.TicketPrefix, []string{
		fmt.Sprintf("nc-%d", id2), fmt.Sprintf("nc-%d", id1),
	}); err != nil {
		t.Fatalf("ticketUndepend with prefix: %v", err)
	}

	deps, _ := issues.GetDependencies(id2)
	if len(deps) != 0 {
		t.Errorf("expected no deps, got %v", deps)
	}
}

func TestTicketParent_happyPath(t *testing.T) {
	issues := setupTicketTestRepo(t)
	cfg := &config.Config{TicketPrefix: "nc"}

	parent, _ := issues.Create("Epic", "epic", nil, nil, nil)
	child, _ := issues.Create("Task", "task", nil, nil, nil)

	if err := ticketParent(issues, cfg.TicketPrefix, []string{
		fmt.Sprintf("%d", child), fmt.Sprintf("%d", parent),
	}); err != nil {
		t.Fatalf("ticketParent: %v", err)
	}

	got, _ := issues.Get(child)
	if !got.ParentID.Valid || int(got.ParentID.Int64) != parent {
		t.Errorf("ParentID = %v, want %d", got.ParentID, parent)
	}
}

func TestTicketParent_selfParent(t *testing.T) {
	issues := setupTicketTestRepo(t)
	cfg := &config.Config{TicketPrefix: "nc"}

	id, _ := issues.Create("Task", "task", nil, nil, nil)
	err := ticketParent(issues, cfg.TicketPrefix, []string{
		fmt.Sprintf("%d", id), fmt.Sprintf("%d", id),
	})
	if err == nil {
		t.Error("expected error for self-parent")
	}
}

func TestTicketParent_nonexistentTicket(t *testing.T) {
	issues := setupTicketTestRepo(t)
	cfg := &config.Config{TicketPrefix: "nc"}

	id, _ := issues.Create("Task", "task", nil, nil, nil)
	err := ticketParent(issues, cfg.TicketPrefix, []string{
		fmt.Sprintf("%d", id), "9999",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent parent")
	}
	if !strings.Contains(err.Error(), "9999") {
		t.Errorf("error should mention missing id, got: %v", err)
	}
}

func TestTicketParent_prefixedIDs(t *testing.T) {
	issues := setupTicketTestRepo(t)
	cfg := &config.Config{TicketPrefix: "nc"}

	parent, _ := issues.Create("Epic", "epic", nil, nil, nil)
	child, _ := issues.Create("Task", "task", nil, nil, nil)

	if err := ticketParent(issues, cfg.TicketPrefix, []string{
		fmt.Sprintf("nc-%d", child), fmt.Sprintf("nc-%d", parent),
	}); err != nil {
		t.Fatalf("ticketParent with prefix: %v", err)
	}

	got, _ := issues.Get(child)
	if !got.ParentID.Valid || int(got.ParentID.Int64) != parent {
		t.Errorf("ParentID = %v, want %d", got.ParentID, parent)
	}
}

func TestTicketUnparent_happyPath(t *testing.T) {
	issues := setupTicketTestRepo(t)
	cfg := &config.Config{TicketPrefix: "nc"}

	parent, _ := issues.Create("Epic", "epic", nil, nil, nil)
	child, _ := issues.Create("Task", "task", &parent, nil, nil)

	if err := ticketUnparent(issues, cfg.TicketPrefix, []string{
		fmt.Sprintf("%d", child),
	}); err != nil {
		t.Fatalf("ticketUnparent: %v", err)
	}

	got, _ := issues.Get(child)
	if got.ParentID.Valid {
		t.Errorf("expected ParentID NULL after unparent, got %d", got.ParentID.Int64)
	}
}

func TestTicketUnparent_nonexistentTicket(t *testing.T) {
	issues := setupTicketTestRepo(t)
	cfg := &config.Config{TicketPrefix: "nc"}

	err := ticketUnparent(issues, cfg.TicketPrefix, []string{"9999"})
	if err == nil {
		t.Error("expected error for nonexistent ticket")
	}
}

func TestTicketUnparent_invalidArgs(t *testing.T) {
	issues := setupTicketTestRepo(t)
	cfg := &config.Config{TicketPrefix: "nc"}

	if err := ticketUnparent(issues, cfg.TicketPrefix, []string{}); err == nil {
		t.Error("expected error for empty args")
	}
}

func TestTicketParent_rejectsCycleDirect(t *testing.T) {
	issues := setupTicketTestRepo(t)
	cfg := &config.Config{TicketPrefix: "nc"}

	a, _ := issues.Create("A", "task", nil, nil, nil)
	b, _ := issues.Create("B", "task", nil, nil, nil)

	// B parents A
	if err := ticketParent(issues, cfg.TicketPrefix, []string{
		fmt.Sprintf("%d", b), fmt.Sprintf("%d", a),
	}); err != nil {
		t.Fatalf("first parent: %v", err)
	}

	// Try to make A parent B — would create a cycle
	err := ticketParent(issues, cfg.TicketPrefix, []string{
		fmt.Sprintf("%d", a), fmt.Sprintf("%d", b),
	})
	if err == nil {
		t.Fatal("expected error for direct cycle (A→B, B→A), got nil")
	}
}

func TestTicketParent_rejectsCycleIndirect(t *testing.T) {
	issues := setupTicketTestRepo(t)
	cfg := &config.Config{TicketPrefix: "nc"}

	a, _ := issues.Create("A", "task", nil, nil, nil)
	b, _ := issues.Create("B", "task", nil, nil, nil)
	c, _ := issues.Create("C", "task", nil, nil, nil)

	// Build chain: C→B→A (C parents B, B parents A)
	if err := ticketParent(issues, cfg.TicketPrefix, []string{
		fmt.Sprintf("%d", c), fmt.Sprintf("%d", b),
	}); err != nil {
		t.Fatalf("C→B: %v", err)
	}
	if err := ticketParent(issues, cfg.TicketPrefix, []string{
		fmt.Sprintf("%d", b), fmt.Sprintf("%d", a),
	}); err != nil {
		t.Fatalf("B→A: %v", err)
	}

	// Try to make A parent C — A is an ancestor of C, so this would cycle
	err := ticketParent(issues, cfg.TicketPrefix, []string{
		fmt.Sprintf("%d", a), fmt.Sprintf("%d", c),
	})
	if err == nil {
		t.Fatal("expected error for indirect cycle (C→B→A, then A→C), got nil")
	}
}

func TestTicketParent_idempotent(t *testing.T) {
	issues := setupTicketTestRepo(t)
	cfg := &config.Config{TicketPrefix: "nc"}

	parent, _ := issues.Create("Parent", "epic", nil, nil, nil)
	child, _ := issues.Create("Child", "task", nil, nil, nil)

	args := []string{fmt.Sprintf("%d", child), fmt.Sprintf("%d", parent)}
	if err := ticketParent(issues, cfg.TicketPrefix, args); err != nil {
		t.Fatalf("first ticketParent: %v", err)
	}
	// Second call with the same parent should succeed (idempotent)
	if err := ticketParent(issues, cfg.TicketPrefix, args); err != nil {
		t.Errorf("second ticketParent (same parent) should succeed, got: %v", err)
	}
}

func TestTicketUnparent_idempotentWhenNoParent(t *testing.T) {
	issues := setupTicketTestRepo(t)
	cfg := &config.Config{TicketPrefix: "nc"}

	id, _ := issues.Create("Root", "task", nil, nil, nil)

	// Unparent a ticket that has no parent — should succeed silently
	if err := ticketUnparent(issues, cfg.TicketPrefix, []string{fmt.Sprintf("%d", id)}); err != nil {
		t.Errorf("unparent on ticket with no parent should succeed, got: %v", err)
	}
}

func TestTicketStatus_InProgress_SetsAgentWorking(t *testing.T) {
	issues, agents := setupTicketTestRepos(t)

	// Register agent and create + assign a ticket.
	if err := agents.Register("iron", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	id, err := issues.Create("my ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := issues.Assign(id, "iron", "prole/iron/nc-126"); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	// Transition to in_progress — agent should become working.
	if err := ticketStatus(issues, agents, []string{fmt.Sprintf("%d", id), "in_progress"}); err != nil {
		t.Fatalf("ticketStatus in_progress: %v", err)
	}

	agent, err := agents.Get("iron")
	if err != nil {
		t.Fatalf("agents.Get: %v", err)
	}
	if agent.Status != "working" {
		t.Errorf("expected agent status=working after in_progress, got %q", agent.Status)
	}
	if !agent.CurrentIssue.Valid || int(agent.CurrentIssue.Int64) != id {
		t.Errorf("expected current_issue=%d, got valid=%v value=%v", id, agent.CurrentIssue.Valid, agent.CurrentIssue.Int64)
	}
}

func TestTicketStatus_InProgress_NoAssignee_Errors(t *testing.T) {
	issues, agents := setupTicketTestRepos(t)

	id, err := issues.Create("unassigned ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// No assignee — should error.
	err = ticketStatus(issues, agents, []string{fmt.Sprintf("%d", id), "in_progress"})
	if err == nil {
		t.Fatal("expected error for unassigned ticket, got nil")
	}
	if !errors.Is(err, ErrNoAssignee) {
		t.Errorf("expected ErrNoAssignee, got: %v", err)
	}
	// Ticket must remain in its original state — transition must not have applied.
	got, _ := issues.Get(id)
	if got.Status == "in_progress" {
		t.Error("ticket should not transition to in_progress when assignee is missing")
	}
}

func TestTicketStatus_NonInProgress_NoAgentUpdate(t *testing.T) {
	issues, agents := setupTicketTestRepos(t)

	if err := agents.Register("iron", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	id, err := issues.Create("ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := issues.Assign(id, "iron", "prole/iron/nc-126"); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	// Transition to in_review — agent status must NOT change.
	if err := ticketStatus(issues, agents, []string{fmt.Sprintf("%d", id), "in_review"}); err != nil {
		t.Fatalf("ticketStatus in_review: %v", err)
	}

	agent, _ := agents.Get("iron")
	if agent.Status != "idle" {
		t.Errorf("expected agent status unchanged (idle) after in_review, got %q", agent.Status)
	}

	// Multi-step walk: open → in_progress (agent becomes working) → in_review (agent unchanged)
	// Reset to open first.
	if err := issues.UpdateStatus(id, "open"); err != nil {
		t.Fatalf("reset to open: %v", err)
	}
	if err := ticketStatus(issues, agents, []string{fmt.Sprintf("%d", id), "in_progress"}); err != nil {
		t.Fatalf("in_progress: %v", err)
	}
	agent, _ = agents.Get("iron")
	if agent.Status != "working" {
		t.Errorf("expected working after in_progress, got %q", agent.Status)
	}
	if err := ticketStatus(issues, agents, []string{fmt.Sprintf("%d", id), "in_review"}); err != nil {
		t.Fatalf("in_review: %v", err)
	}
	agent, _ = agents.Get("iron")
	if agent.Status != "working" {
		t.Errorf("expected agent status still working after in_review (no agent update), got %q", agent.Status)
	}
}

func TestTicketStatus_InProgress_AlreadyWorkingOnDifferentTicket(t *testing.T) {
	issues, agents := setupTicketTestRepos(t)

	if err := agents.Register("iron", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Create two tickets assigned to the same agent.
	idA, err := issues.Create("ticket A", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create A: %v", err)
	}
	idB, err := issues.Create("ticket B", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create B: %v", err)
	}
	if err := issues.Assign(idA, "iron", "prole/iron/a"); err != nil {
		t.Fatalf("Assign A: %v", err)
	}
	if err := issues.Assign(idB, "iron", "prole/iron/b"); err != nil {
		t.Fatalf("Assign B: %v", err)
	}

	// Start working on ticket A.
	if err := ticketStatus(issues, agents, []string{fmt.Sprintf("%d", idA), "in_progress"}); err != nil {
		t.Fatalf("in_progress A: %v", err)
	}
	agent, _ := agents.Get("iron")
	if !agent.CurrentIssue.Valid || int(agent.CurrentIssue.Int64) != idA {
		t.Errorf("expected current_issue=%d after A, got %v", idA, agent.CurrentIssue.Int64)
	}

	// Switch to ticket B — current_issue must update to B.
	if err := ticketStatus(issues, agents, []string{fmt.Sprintf("%d", idB), "in_progress"}); err != nil {
		t.Fatalf("in_progress B: %v", err)
	}
	agent, _ = agents.Get("iron")
	if !agent.CurrentIssue.Valid || int(agent.CurrentIssue.Int64) != idB {
		t.Errorf("expected current_issue=%d after B, got %v", idB, agent.CurrentIssue.Int64)
	}
}

func TestTicketStatus_InProgress_AgentRowMissing(t *testing.T) {
	issues, agents := setupTicketTestRepos(t)

	// Assign ticket to an agent that has no row in the agents table.
	id, err := issues.Create("ghost ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := issues.Assign(id, "ghost", "prole/ghost/1"); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	err = ticketStatus(issues, agents, []string{fmt.Sprintf("%d", id), "in_progress"})
	if err == nil {
		t.Fatal("expected error when agent row is missing, got nil")
	}

	// Ticket must NOT have transitioned — the agent update is applied first.
	got, _ := issues.Get(id)
	if got.Status == "in_progress" {
		t.Error("ticket should not transition to in_progress when agent row is missing")
	}
}
