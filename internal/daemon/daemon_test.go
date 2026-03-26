package daemon

import (
	"fmt"
	"io"
	"log"
	"testing"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

type sentMessage struct {
	session string
	msg     string
}

// newTestDaemon creates a daemon with no active sessions and a discarding sendKeys.
func newTestDaemon(t *testing.T) (*Daemon, *repo.IssueRepo, *repo.AgentRepo) {
	t.Helper()
	d, issues, agents, _ := newTestDaemonWithSessions(t, nil)
	return d, issues, agents
}

// newTestDaemonWithSessions creates a daemon where the given sessions appear active.
// Returned *[]sentMessage accumulates all sendKeys calls made during the test.
func newTestDaemonWithSessions(t *testing.T, activeSessions []string) (*Daemon, *repo.IssueRepo, *repo.AgentRepo, *[]sentMessage) {
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

	sessions := make(map[string]bool, len(activeSessions))
	for _, s := range activeSessions {
		sessions[s] = true
	}

	var sent []sentMessage

	d := &Daemon{
		cfg:           cfg,
		issues:        issues,
		agents:        agents,
		logger:        log.New(io.Discard, "", 0),
		stop:          make(chan struct{}),
		sessionExists: func(s string) bool { return sessions[s] },
		sendKeys: func(s, msg string) error {
			sent = append(sent, sentMessage{session: s, msg: msg})
			return nil
		},
	}

	return d, issues, agents, &sent
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

// --- Reviewer nudge tests ---

func TestHandleInReviewTickets_nudgesReviewerPerTicket(t *testing.T) {
	reviewerSession := "ct-reviewer"
	d, issues, _, sent := newTestDaemonWithSessions(t, []string{reviewerSession})

	id, _ := issues.Create("Add auth", "task", nil, nil)
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 42)

	d.handleInReviewTickets()

	if len(*sent) != 1 {
		t.Fatalf("expected 1 nudge, got %d", len(*sent))
	}
	if (*sent)[0].session != reviewerSession {
		t.Errorf("expected message to %q, got %q", reviewerSession, (*sent)[0].session)
	}
	if !containsAll((*sent)[0].msg, "PR #42", "NC-"+itoa(id)) {
		t.Errorf("nudge message missing ticket/PR info: %q", (*sent)[0].msg)
	}
}

func TestHandleInReviewTickets_nudgesOncePerTicket(t *testing.T) {
	d, issues, _, sent := newTestDaemonWithSessions(t, []string{"ct-reviewer"})

	// Two in_review tickets with PRs
	id1, _ := issues.Create("Ticket A", "task", nil, nil)
	issues.UpdateStatus(id1, "in_review")
	issues.SetPR(id1, 10)

	id2, _ := issues.Create("Ticket B", "task", nil, nil)
	issues.UpdateStatus(id2, "in_review")
	issues.SetPR(id2, 11)

	d.handleInReviewTickets()

	if len(*sent) != 2 {
		t.Errorf("expected 2 nudges (one per ticket), got %d", len(*sent))
	}
}

func TestHandleInReviewTickets_noNudgeWhenEmpty(t *testing.T) {
	d, _, _, sent := newTestDaemonWithSessions(t, []string{"ct-reviewer"})

	d.handleInReviewTickets()

	if len(*sent) != 0 {
		t.Errorf("expected 0 nudges (no tickets), got %d", len(*sent))
	}
}

func TestHandleInReviewTickets_noNudgeWhenReviewerNotRunning(t *testing.T) {
	d, issues, _, sent := newTestDaemonWithSessions(t, nil) // no active sessions

	id, _ := issues.Create("Add auth", "task", nil, nil)
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 42)

	d.handleInReviewTickets()

	if len(*sent) != 0 {
		t.Errorf("expected 0 nudges (reviewer not running), got %d", len(*sent))
	}
}

func TestHandleInReviewTickets_skipsTicketsWithoutPR(t *testing.T) {
	d, issues, _, sent := newTestDaemonWithSessions(t, []string{"ct-reviewer"})

	id, _ := issues.Create("No PR yet", "task", nil, nil)
	issues.UpdateStatus(id, "in_review")
	// No SetPR — ticket has no PR number

	d.handleInReviewTickets()

	if len(*sent) != 0 {
		t.Errorf("expected 0 nudges (no PR number), got %d: %v", len(*sent), *sent)
	}
}

func TestHandleInReviewTickets_mixedTickets(t *testing.T) {
	d, issues, _, sent := newTestDaemonWithSessions(t, []string{"ct-reviewer"})

	// in_review with PR — should nudge
	id1, _ := issues.Create("Ready for review", "task", nil, nil)
	issues.UpdateStatus(id1, "in_review")
	issues.SetPR(id1, 7)

	// in_review without PR — should NOT nudge
	id2, _ := issues.Create("No PR", "task", nil, nil)
	issues.UpdateStatus(id2, "in_review")

	// open ticket — should NOT nudge
	id3, _ := issues.Create("Open ticket", "task", nil, nil)
	issues.UpdateStatus(id3, "open")
	issues.SetPR(id3, 8)

	d.handleInReviewTickets()

	if len(*sent) != 1 {
		t.Errorf("expected 1 nudge, got %d", len(*sent))
	}
}

// helpers

func containsAll(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if !containsStr(s, sub) {
			return false
		}
	}
	return true
}

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && findStr(s, sub))
}

func findStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
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
