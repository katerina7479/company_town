package daemon

import (
	"io"
	"log"
	"strconv"
	"strings"
	"testing"
	"time"

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
		lastNudged:    make(map[string]time.Time),
		nudgeCooldown: 0, // disabled by default in tests
		nowFn:         time.Now,
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

func TestHandleInReviewTickets_batchesMultipleTickets(t *testing.T) {
	d, issues, _, sent := newTestDaemonWithSessions(t, []string{"ct-reviewer"})

	// Two in_review tickets with PRs — should produce ONE batched message
	id1, _ := issues.Create("Ticket A", "task", nil, nil)
	issues.UpdateStatus(id1, "in_review")
	issues.SetPR(id1, 10)

	id2, _ := issues.Create("Ticket B", "task", nil, nil)
	issues.UpdateStatus(id2, "in_review")
	issues.SetPR(id2, 11)

	d.handleInReviewTickets()

	if len(*sent) != 1 {
		t.Fatalf("expected 1 batched nudge, got %d", len(*sent))
	}
	if !containsAll((*sent)[0].msg, "NC-"+itoa(id1), "PR #10", "NC-"+itoa(id2), "PR #11") {
		t.Errorf("batched nudge missing ticket info: %q", (*sent)[0].msg)
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

// --- Repairing ticket tests ---

func TestHandleRepairingTickets_nudgesConductor(t *testing.T) {
	conductorSession := "ct-conductor"
	d, issues, _, sent := newTestDaemonWithSessions(t, []string{conductorSession})

	id, _ := issues.Create("Fix auth bug", "task", nil, nil)
	issues.UpdateStatus(id, "repairing")
	issues.SetPR(id, 55)

	d.handleRepairingTickets()

	if len(*sent) != 1 {
		t.Fatalf("expected 1 nudge, got %d", len(*sent))
	}
	if (*sent)[0].session != conductorSession {
		t.Errorf("expected message to %q, got %q", conductorSession, (*sent)[0].session)
	}
	if !containsAll((*sent)[0].msg, "NC-"+itoa(id), "PR #55") {
		t.Errorf("nudge message missing ticket/PR info: %q", (*sent)[0].msg)
	}
}

func TestHandleRepairingTickets_includesPRNumberWhenPresent(t *testing.T) {
	d, issues, _, sent := newTestDaemonWithSessions(t, []string{"ct-conductor"})

	id, _ := issues.Create("Fix lint", "task", nil, nil)
	issues.UpdateStatus(id, "repairing")
	issues.SetPR(id, 99)

	d.handleRepairingTickets()

	if len(*sent) != 1 {
		t.Fatalf("expected 1 nudge, got %d", len(*sent))
	}
	if !containsAll((*sent)[0].msg, "PR #99") {
		t.Errorf("expected PR number in nudge: %q", (*sent)[0].msg)
	}
}

func TestHandleRepairingTickets_worksWithoutPRNumber(t *testing.T) {
	d, issues, _, sent := newTestDaemonWithSessions(t, []string{"ct-conductor"})

	id, _ := issues.Create("Fix something", "task", nil, nil)
	issues.UpdateStatus(id, "repairing")
	// No SetPR — ticket has no PR

	d.handleRepairingTickets()

	if len(*sent) != 1 {
		t.Fatalf("expected 1 nudge even without PR, got %d", len(*sent))
	}
	if !containsAll((*sent)[0].msg, "NC-"+itoa(id)) {
		t.Errorf("nudge message missing ticket info: %q", (*sent)[0].msg)
	}
}

func TestHandleRepairingTickets_noNudgeWhenEmpty(t *testing.T) {
	d, _, _, sent := newTestDaemonWithSessions(t, []string{"ct-conductor"})

	d.handleRepairingTickets()

	if len(*sent) != 0 {
		t.Errorf("expected 0 nudges (no repairing tickets), got %d", len(*sent))
	}
}

func TestHandleRepairingTickets_noNudgeWhenConductorNotRunning(t *testing.T) {
	d, issues, _, sent := newTestDaemonWithSessions(t, nil) // no active sessions

	id, _ := issues.Create("Fix auth bug", "task", nil, nil)
	issues.UpdateStatus(id, "repairing")

	d.handleRepairingTickets()

	if len(*sent) != 0 {
		t.Errorf("expected 0 nudges (conductor not running), got %d", len(*sent))
	}
}

func TestHandleRepairingTickets_batchesMultipleTickets(t *testing.T) {
	d, issues, _, sent := newTestDaemonWithSessions(t, []string{"ct-conductor"})

	id1, _ := issues.Create("Fix A", "task", nil, nil)
	issues.UpdateStatus(id1, "repairing")

	id2, _ := issues.Create("Fix B", "task", nil, nil)
	issues.UpdateStatus(id2, "repairing")
	issues.SetPR(id2, 12)

	d.handleRepairingTickets()

	if len(*sent) != 1 {
		t.Fatalf("expected 1 batched nudge, got %d", len(*sent))
	}
	if !containsAll((*sent)[0].msg, "NC-"+itoa(id1), "NC-"+itoa(id2)) {
		t.Errorf("batched nudge missing ticket info: %q", (*sent)[0].msg)
	}
}

func TestHandleRepairingTickets_ignoresNonRepairingTickets(t *testing.T) {
	d, issues, _, sent := newTestDaemonWithSessions(t, []string{"ct-conductor"})

	// repairing — should nudge
	id1, _ := issues.Create("Needs fix", "task", nil, nil)
	issues.UpdateStatus(id1, "repairing")

	// in_review — should NOT nudge conductor
	id2, _ := issues.Create("In review", "task", nil, nil)
	issues.UpdateStatus(id2, "in_review")
	issues.SetPR(id2, 20)

	// open — should NOT nudge
	id3, _ := issues.Create("Open ticket", "task", nil, nil)
	issues.UpdateStatus(id3, "open")

	d.handleRepairingTickets()

	if len(*sent) != 1 {
		t.Errorf("expected 1 nudge, got %d", len(*sent))
	}
}

// helpers

func containsAll(s string, substrings ...string) bool {
	for _, sub := range substrings {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}

func itoa(n int) string {
	return strconv.Itoa(n)
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

// --- Dead session detection tests ---

func TestHandleDeadSessions_marksAgentDeadWhenSessionGone(t *testing.T) {
	d, _, agents, _ := newTestDaemonWithSessions(t, nil) // no active sessions

	agents.Register("worker", "prole", nil)
	agents.SetTmuxSession("worker", "ct-worker")

	d.handleDeadSessions()

	agent, err := agents.Get("worker")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if agent.Status != "dead" {
		t.Errorf("expected status='dead', got %q", agent.Status)
	}
}

func TestHandleDeadSessions_keepsAgentAliveWhenSessionExists(t *testing.T) {
	d, _, agents, _ := newTestDaemonWithSessions(t, []string{"ct-worker"})

	agents.Register("worker", "prole", nil)
	agents.SetTmuxSession("worker", "ct-worker")

	d.handleDeadSessions()

	agent, _ := agents.Get("worker")
	if agent.Status != "idle" {
		t.Errorf("expected status='idle' (session alive), got %q", agent.Status)
	}
}

func TestHandleDeadSessions_skipsAgentWithNoSession(t *testing.T) {
	d, _, agents, _ := newTestDaemonWithSessions(t, nil)

	// No SetTmuxSession call — agent has no session recorded
	agents.Register("worker", "prole", nil)

	d.handleDeadSessions()

	agent, _ := agents.Get("worker")
	if agent.Status != "idle" {
		t.Errorf("expected status='idle' (no session to check), got %q", agent.Status)
	}
}

func TestHandleDeadSessions_skipsAlreadyDeadAgents(t *testing.T) {
	d, _, agents, _ := newTestDaemonWithSessions(t, nil)

	agents.Register("worker", "prole", nil)
	agents.SetTmuxSession("worker", "ct-worker")
	agents.UpdateStatus("worker", "dead")

	// Should not error or double-process
	d.handleDeadSessions()

	agent, _ := agents.Get("worker")
	if agent.Status != "dead" {
		t.Errorf("expected status='dead', got %q", agent.Status)
	}
}

func TestHandleDeadSessions_handlesMultipleAgents(t *testing.T) {
	d, _, agents, _ := newTestDaemonWithSessions(t, []string{"ct-alive"})

	agents.Register("alive-agent", "prole", nil)
	agents.SetTmuxSession("alive-agent", "ct-alive")

	agents.Register("dead-agent", "prole", nil)
	agents.SetTmuxSession("dead-agent", "ct-dead")

	d.handleDeadSessions()

	alive, _ := agents.Get("alive-agent")
	if alive.Status != "idle" {
		t.Errorf("alive-agent: expected 'idle', got %q", alive.Status)
	}

	dead, _ := agents.Get("dead-agent")
	if dead.Status != "dead" {
		t.Errorf("dead-agent: expected 'dead', got %q", dead.Status)
	}
}

// --- Cooldown tests ---

// withCooldown returns a copy of d with the given cooldown duration and a fixed now function.
func withCooldown(d *Daemon, cooldown time.Duration, fixedNow time.Time) {
	d.nudgeCooldown = cooldown
	d.nowFn = func() time.Time { return fixedNow }
}

func TestCooldown_suppressesRepeatNudge(t *testing.T) {
	d, issues, _, sent := newTestDaemonWithSessions(t, []string{"ct-reviewer"})

	id, _ := issues.Create("Add feature", "task", nil, nil)
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 5)

	now := time.Now()
	withCooldown(d, 5*time.Minute, now)

	// First call: should send nudge
	d.handleInReviewTickets()
	if len(*sent) != 1 {
		t.Fatalf("expected 1 nudge on first call, got %d", len(*sent))
	}

	// Second call at same time: still within cooldown — no nudge
	d.handleInReviewTickets()
	if len(*sent) != 1 {
		t.Errorf("expected no nudge within cooldown, got %d total", len(*sent))
	}
}

func TestCooldown_allowsNudgeAfterExpiry(t *testing.T) {
	d, issues, _, sent := newTestDaemonWithSessions(t, []string{"ct-reviewer"})

	id, _ := issues.Create("Add feature", "task", nil, nil)
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 5)

	base := time.Now()
	withCooldown(d, 5*time.Minute, base)

	// First nudge
	d.handleInReviewTickets()
	if len(*sent) != 1 {
		t.Fatalf("expected 1 nudge on first call, got %d", len(*sent))
	}

	// Advance clock past cooldown
	d.nowFn = func() time.Time { return base.Add(6 * time.Minute) }

	// Should nudge again
	d.handleInReviewTickets()
	if len(*sent) != 2 {
		t.Errorf("expected 2 nudges after cooldown expiry, got %d", len(*sent))
	}
}

func TestCooldown_independentPerHandler(t *testing.T) {
	conductorSession := "ct-conductor"
	reviewerSession := "ct-reviewer"
	d, issues, _, sent := newTestDaemonWithSessions(t, []string{conductorSession, reviewerSession})

	// Repairing ticket for conductor
	id1, _ := issues.Create("Fix bug", "task", nil, nil)
	issues.UpdateStatus(id1, "repairing")

	// In-review ticket for reviewer
	id2, _ := issues.Create("Review feature", "task", nil, nil)
	issues.UpdateStatus(id2, "in_review")
	issues.SetPR(id2, 9)

	now := time.Now()
	withCooldown(d, 5*time.Minute, now)

	// Both fire on first call
	d.handleInReviewTickets()
	d.handleRepairingTickets()
	if len(*sent) != 2 {
		t.Fatalf("expected 2 nudges on first call, got %d", len(*sent))
	}

	// Second call — both suppressed independently
	d.handleInReviewTickets()
	d.handleRepairingTickets()
	if len(*sent) != 2 {
		t.Errorf("expected no additional nudges within cooldown, got %d total", len(*sent))
	}
}

func TestCooldown_disabledWhenZero(t *testing.T) {
	d, issues, _, sent := newTestDaemonWithSessions(t, []string{"ct-reviewer"})
	// nudgeCooldown is 0 (default in test helper) — should always nudge

	id, _ := issues.Create("Add feature", "task", nil, nil)
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 5)

	d.handleInReviewTickets()
	d.handleInReviewTickets()
	d.handleInReviewTickets()

	if len(*sent) != 3 {
		t.Errorf("expected 3 nudges with cooldown disabled, got %d", len(*sent))
	}
}
