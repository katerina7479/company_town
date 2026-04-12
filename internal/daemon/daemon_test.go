package daemon

import (
	"fmt"
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

	issues := repo.NewIssueRepo(conn, nil)
	agents := repo.NewAgentRepo(conn, nil)

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
		resetWorktree:       func(string) error { return nil },
		runQualityBaseline:  func() error { return nil },
		pruneStaleWorktrees: func() error { return nil },
		lastNudged:          make(map[string]time.Time),
		lastNudgeDigest:     make(map[string]string),
		nudgeCooldown:       0, // disabled by default in tests
		qualityInterval:     0, // disabled by default in tests
		worktreeInterval:    0, // disabled by default in tests
		nowFn:               time.Now,
		lastRestartedAt:     make(map[string]time.Time),
		restartDeadAgents:   false, // disabled by default in tests
	}

	return d, issues, agents, &sent
}

// withResetCapture replaces d.resetWorktree with one that records calls.
// Returns a pointer to the slice of agent names that were reset.
func withResetCapture(d *Daemon) *[]string {
	var resets []string
	d.resetWorktree = func(name string) error {
		resets = append(resets, name)
		return nil
	}
	return &resets
}

func TestHandlePRMerged_closesTicket(t *testing.T) {
	d, issues, _ := newTestDaemon(t)

	id, err := issues.Create("Test ticket", "task", nil, nil, nil)
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

	id, _ := issues.Create("Already closed", "task", nil, nil, nil)
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

	id, _ := issues.Create("Test ticket", "task", nil, nil, nil)
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

	id, _ := issues.Create("Unassigned ticket", "task", nil, nil, nil)
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

func TestHandlePRMerged_resetsProleWorktree(t *testing.T) {
	d, issues, agents := newTestDaemon(t)
	resets := withResetCapture(d)

	if err := agents.Register("quartz", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}

	id, _ := issues.Create("Add feature", "task", nil, nil, nil)
	issues.Assign(id, "quartz", "prole/quartz/NC-42")
	issues.SetPR(id, 42)
	agents.SetCurrentIssue("quartz", &id)

	issue, _ := issues.Get(id)
	d.handlePRMerged(issue)

	if len(*resets) != 1 || (*resets)[0] != "quartz" {
		t.Errorf("expected worktree reset for quartz, got %v", *resets)
	}
}

func TestHandlePRMerged_doesNotResetNonProle(t *testing.T) {
	d, issues, agents := newTestDaemon(t)
	resets := withResetCapture(d)

	if err := agents.Register("conductor", "conductor", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}

	id, _ := issues.Create("Route tickets", "task", nil, nil, nil)
	issues.Assign(id, "conductor", "conductor/branch")
	issues.SetPR(id, 99)
	agents.SetCurrentIssue("conductor", &id)

	issue, _ := issues.Get(id)
	d.handlePRMerged(issue)

	if len(*resets) != 0 {
		t.Errorf("expected no worktree reset for conductor, got %v", *resets)
	}
}

func TestHandlePRMerged_noResetWhenNoAssignee(t *testing.T) {
	d, issues, _ := newTestDaemon(t)
	resets := withResetCapture(d)

	id, _ := issues.Create("Unassigned", "task", nil, nil, nil)
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 7)

	issue, _ := issues.Get(id)
	d.handlePRMerged(issue)

	if len(*resets) != 0 {
		t.Errorf("expected no reset when no assignee, got %v", *resets)
	}
}

func TestHandlePRClosed_doesNotCloseTicket(t *testing.T) {
	d, issues, _ := newTestDaemon(t)

	id, _ := issues.Create("Test ticket", "task", nil, nil, nil)
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

	id, _ := issues.Create("Test ticket", "task", nil, nil, nil)
	issues.UpdateStatus(id, "closed")
	issues.SetPR(id, 88)

	issue, _ := issues.Get(id)
	// Should return early without error
	d.handlePRClosed(issue)
}

// --- Reviewer nudge tests ---

func TestHandleInReviewTickets_nudgesReviewerPerTicket(t *testing.T) {
	reviewerSession := "ct-reviewer"
	d, issues, agents, sent := newTestDaemonWithSessions(t, []string{reviewerSession})
	agents.Register("reviewer", "reviewer", nil)

	id, _ := issues.Create("Add auth", "task", nil, nil, nil)
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
	d, issues, agents, sent := newTestDaemonWithSessions(t, []string{"ct-reviewer"})
	agents.Register("reviewer", "reviewer", nil)

	// Two in_review tickets with PRs — one reviewer → ONE batched message
	id1, _ := issues.Create("Ticket A", "task", nil, nil, nil)
	issues.UpdateStatus(id1, "in_review")
	issues.SetPR(id1, 10)

	id2, _ := issues.Create("Ticket B", "task", nil, nil, nil)
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
	d, issues, agents, sent := newTestDaemonWithSessions(t, nil) // no active sessions
	agents.Register("reviewer", "reviewer", nil)

	id, _ := issues.Create("Add auth", "task", nil, nil, nil)
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 42)

	d.handleInReviewTickets()

	if len(*sent) != 0 {
		t.Errorf("expected 0 nudges (reviewer session not active), got %d", len(*sent))
	}
}

func TestHandleInReviewTickets_skipsTicketsWithoutPR(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t, []string{"ct-reviewer"})
	agents.Register("reviewer", "reviewer", nil)

	id, _ := issues.Create("No PR yet", "task", nil, nil, nil)
	issues.UpdateStatus(id, "in_review")
	// No SetPR — ticket has no PR number

	d.handleInReviewTickets()

	if len(*sent) != 0 {
		t.Errorf("expected 0 nudges (no PR number), got %d: %v", len(*sent), *sent)
	}
}

func TestHandleInReviewTickets_mixedTickets(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t, []string{"ct-reviewer"})
	agents.Register("reviewer", "reviewer", nil)

	// in_review with PR — should nudge
	id1, _ := issues.Create("Ready for review", "task", nil, nil, nil)
	issues.UpdateStatus(id1, "in_review")
	issues.SetPR(id1, 7)

	// in_review without PR — should NOT nudge
	id2, _ := issues.Create("No PR", "task", nil, nil, nil)
	issues.UpdateStatus(id2, "in_review")

	// open ticket — should NOT nudge
	id3, _ := issues.Create("Open ticket", "task", nil, nil, nil)
	issues.UpdateStatus(id3, "open")
	issues.SetPR(id3, 8)

	d.handleInReviewTickets()

	if len(*sent) != 1 {
		t.Errorf("expected 1 nudge, got %d", len(*sent))
	}
}

// --- Multi-reviewer load balancing tests ---

func TestHandleInReviewTickets_distributesTwoTicketsAcrossTwoReviewers(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t,
		[]string{"ct-reviewer-1", "ct-reviewer-2"})
	agents.Register("reviewer-1", "reviewer", nil)
	agents.Register("reviewer-2", "reviewer", nil)

	id1, _ := issues.Create("Ticket A", "task", nil, nil, nil)
	issues.UpdateStatus(id1, "in_review")
	issues.SetPR(id1, 10)

	id2, _ := issues.Create("Ticket B", "task", nil, nil, nil)
	issues.UpdateStatus(id2, "in_review")
	issues.SetPR(id2, 11)

	d.handleInReviewTickets()

	// Two reviewers → two messages, one ticket each
	if len(*sent) != 2 {
		t.Fatalf("expected 2 nudges (one per reviewer), got %d", len(*sent))
	}

	// Each message should contain exactly one ticket
	msgs := map[string]string{
		(*sent)[0].session: (*sent)[0].msg,
		(*sent)[1].session: (*sent)[1].msg,
	}
	r1msg := msgs["ct-reviewer-1"]
	r2msg := msgs["ct-reviewer-2"]

	if r1msg == "" || r2msg == "" {
		t.Fatalf("expected messages to ct-reviewer-1 and ct-reviewer-2, got sessions: %s, %s",
			(*sent)[0].session, (*sent)[1].session)
	}

	// First ticket (id1/PR#10) goes to reviewer-1, second (id2/PR#11) to reviewer-2
	if !containsAll(r1msg, "NC-"+itoa(id1), "PR #10") {
		t.Errorf("reviewer-1 expected ticket %d PR #10, got: %q", id1, r1msg)
	}
	if !containsAll(r2msg, "NC-"+itoa(id2), "PR #11") {
		t.Errorf("reviewer-2 expected ticket %d PR #11, got: %q", id2, r2msg)
	}
}

func TestHandleInReviewTickets_threeTicketsTwoReviewers(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t,
		[]string{"ct-reviewer-1", "ct-reviewer-2"})
	agents.Register("reviewer-1", "reviewer", nil)
	agents.Register("reviewer-2", "reviewer", nil)

	for i, pr := range []int{10, 11, 12} {
		id, _ := issues.Create(fmt.Sprintf("Ticket %d", i), "task", nil, nil, nil)
		issues.UpdateStatus(id, "in_review")
		issues.SetPR(id, pr)
	}

	d.handleInReviewTickets()

	// reviewer-1 gets tickets 0,2 (indices 0%2=0, 2%2=0); reviewer-2 gets ticket 1
	if len(*sent) != 2 {
		t.Fatalf("expected 2 nudges (one per reviewer), got %d", len(*sent))
	}
}

func TestHandleInReviewTickets_skipsDeadReviewer(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t,
		[]string{"ct-reviewer-1", "ct-reviewer-2"})
	agents.Register("reviewer-1", "reviewer", nil)
	agents.Register("reviewer-2", "reviewer", nil)
	agents.UpdateStatus("reviewer-2", "dead")

	id, _ := issues.Create("Add feature", "task", nil, nil, nil)
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 5)

	d.handleInReviewTickets()

	// Only reviewer-1 is active
	if len(*sent) != 1 {
		t.Fatalf("expected 1 nudge (dead reviewer skipped), got %d", len(*sent))
	}
	if (*sent)[0].session != "ct-reviewer-1" {
		t.Errorf("expected nudge to ct-reviewer-1, got %q", (*sent)[0].session)
	}
}

func TestHandleInReviewTickets_noNudgeWhenNoReviewersRegistered(t *testing.T) {
	// No reviewer agents registered — even if a session named ct-reviewer exists
	d, issues, _, sent := newTestDaemonWithSessions(t, []string{"ct-reviewer"})

	id, _ := issues.Create("Ticket", "task", nil, nil, nil)
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 1)

	d.handleInReviewTickets()

	if len(*sent) != 0 {
		t.Errorf("expected 0 nudges (no reviewer agents registered), got %d", len(*sent))
	}
}

// --- Repairing ticket tests ---

func TestHandleRepairingTickets_nudgesConductor(t *testing.T) {
	conductorSession := "ct-conductor"
	d, issues, _, sent := newTestDaemonWithSessions(t, []string{conductorSession})

	id, _ := issues.Create("Fix auth bug", "task", nil, nil, nil)
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

	id, _ := issues.Create("Fix lint", "task", nil, nil, nil)
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

	id, _ := issues.Create("Fix something", "task", nil, nil, nil)
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

	id, _ := issues.Create("Fix auth bug", "task", nil, nil, nil)
	issues.UpdateStatus(id, "repairing")

	d.handleRepairingTickets()

	if len(*sent) != 0 {
		t.Errorf("expected 0 nudges (conductor not running), got %d", len(*sent))
	}
}

func TestHandleRepairingTickets_batchesMultipleTickets(t *testing.T) {
	d, issues, _, sent := newTestDaemonWithSessions(t, []string{"ct-conductor"})

	id1, _ := issues.Create("Fix A", "task", nil, nil, nil)
	issues.UpdateStatus(id1, "repairing")

	id2, _ := issues.Create("Fix B", "task", nil, nil, nil)
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
	id1, _ := issues.Create("Needs fix", "task", nil, nil, nil)
	issues.UpdateStatus(id1, "repairing")

	// in_review — should NOT nudge conductor
	id2, _ := issues.Create("In review", "task", nil, nil, nil)
	issues.UpdateStatus(id2, "in_review")
	issues.SetPR(id2, 20)

	// open — should NOT nudge
	id3, _ := issues.Create("Open ticket", "task", nil, nil, nil)
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
	id1, _ := issues.Create("Open with PR", "task", nil, nil, nil)
	issues.UpdateStatus(id1, "in_review")
	issues.SetPR(id1, 10)

	// Closed ticket with PR — should NOT appear
	id2, _ := issues.Create("Closed with PR", "task", nil, nil, nil)
	issues.UpdateStatus(id2, "closed")
	issues.SetPR(id2, 11)

	// Open ticket without PR — should NOT appear
	id3, _ := issues.Create("Open no PR", "task", nil, nil, nil)
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

func TestHandleDeadSessions_marksCoreAgentDeadWhenSessionGone(t *testing.T) {
	d, _, agents, _ := newTestDaemonWithSessions(t, nil) // no active sessions

	agents.Register("reviewer", "reviewer", nil)
	agents.SetTmuxSession("reviewer", "ct-reviewer")

	d.handleDeadSessions()

	agent, err := agents.Get("reviewer")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if agent.Status != "dead" {
		t.Errorf("expected status='dead', got %q", agent.Status)
	}
}

func TestHandleDeadSessions_deletesProleWhenSessionGone(t *testing.T) {
	d, _, agents, _ := newTestDaemonWithSessions(t, nil)

	agents.Register("worker", "prole", nil)
	agents.SetTmuxSession("worker", "ct-worker")

	d.handleDeadSessions()

	if _, err := agents.Get("worker"); err == nil {
		t.Errorf("expected prole to be deleted, still present")
	}
}

func TestHandleDeadSessions_deletesProleWithNoSession(t *testing.T) {
	d, _, agents, _ := newTestDaemonWithSessions(t, nil)

	// No SetTmuxSession call — prole has no session recorded
	agents.Register("worker", "prole", nil)

	d.handleDeadSessions()

	if _, err := agents.Get("worker"); err == nil {
		t.Errorf("expected prole with no session to be deleted, still present")
	}
}

func TestHandleDeadSessions_keepsAgentAliveWhenSessionExists(t *testing.T) {
	d, _, agents, _ := newTestDaemonWithSessions(t, []string{"ct-worker"})

	agents.Register("worker", "prole", nil)
	agents.SetTmuxSession("worker", "ct-worker")

	d.handleDeadSessions()

	agent, err := agents.Get("worker")
	if err != nil {
		t.Fatalf("expected prole to survive, got error: %v", err)
	}
	if agent.Status != "idle" {
		t.Errorf("expected status='idle' (session alive), got %q", agent.Status)
	}
}

func TestHandleDeadSessions_skipsAlreadyDeadCoreAgents(t *testing.T) {
	d, _, agents, _ := newTestDaemonWithSessions(t, nil)

	agents.Register("reviewer", "reviewer", nil)
	agents.SetTmuxSession("reviewer", "ct-reviewer")
	agents.UpdateStatus("reviewer", "dead")

	// Should not error or double-process
	d.handleDeadSessions()

	agent, _ := agents.Get("reviewer")
	if agent.Status != "dead" {
		t.Errorf("expected status='dead', got %q", agent.Status)
	}
}

func TestHandleDeadSessions_handlesMultipleAgents(t *testing.T) {
	d, _, agents, _ := newTestDaemonWithSessions(t, []string{"ct-alive"})

	agents.Register("alive-prole", "prole", nil)
	agents.SetTmuxSession("alive-prole", "ct-alive")

	agents.Register("dead-prole", "prole", nil)
	agents.SetTmuxSession("dead-prole", "ct-dead")

	d.handleDeadSessions()

	alive, err := agents.Get("alive-prole")
	if err != nil {
		t.Fatalf("alive-prole should survive: %v", err)
	}
	if alive.Status != "idle" {
		t.Errorf("alive-prole: expected 'idle', got %q", alive.Status)
	}

	if _, err := agents.Get("dead-prole"); err == nil {
		t.Errorf("dead-prole: expected to be deleted, still present")
	}
}

// --- Cooldown tests ---

// withCooldown returns a copy of d with the given cooldown duration and a fixed now function.
func withCooldown(d *Daemon, cooldown time.Duration, fixedNow time.Time) {
	d.nudgeCooldown = cooldown
	d.nowFn = func() time.Time { return fixedNow }
}

func TestCooldown_suppressesRepeatNudge(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t, []string{"ct-reviewer"})
	agents.Register("reviewer", "reviewer", nil)

	id, _ := issues.Create("Add feature", "task", nil, nil, nil)
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

func TestCooldown_allowsNudgeAfterExpiry_withNewTickets(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t, []string{"ct-reviewer"})
	agents.Register("reviewer", "reviewer", nil)

	id, _ := issues.Create("Add feature", "task", nil, nil, nil)
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 5)

	base := time.Now()
	withCooldown(d, 5*time.Minute, base)

	// First nudge
	d.handleInReviewTickets()
	if len(*sent) != 1 {
		t.Fatalf("expected 1 nudge on first call, got %d", len(*sent))
	}

	// Same tickets after cooldown — should NOT re-nudge (digest unchanged)
	d.nowFn = func() time.Time { return base.Add(6 * time.Minute) }
	d.handleInReviewTickets()
	if len(*sent) != 1 {
		t.Errorf("expected no re-nudge for unchanged tickets, got %d total", len(*sent))
	}

	// Add a new ticket — digest changes, cooldown already expired → should nudge
	id2, _ := issues.Create("Another feature", "task", nil, nil, nil)
	issues.UpdateStatus(id2, "in_review")
	issues.SetPR(id2, 6)

	d.handleInReviewTickets()
	if len(*sent) != 2 {
		t.Errorf("expected 2 nudges after ticket set changed, got %d", len(*sent))
	}
}

func TestCooldown_independentPerHandler(t *testing.T) {
	conductorSession := "ct-conductor"
	reviewerSession := "ct-reviewer"
	d, issues, agents, sent := newTestDaemonWithSessions(t, []string{conductorSession, reviewerSession})
	agents.Register("reviewer", "reviewer", nil)

	// Repairing ticket for conductor
	id1, _ := issues.Create("Fix bug", "task", nil, nil, nil)
	issues.UpdateStatus(id1, "repairing")

	// In-review ticket for reviewer
	id2, _ := issues.Create("Review feature", "task", nil, nil, nil)
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

// --- Quality baseline tests ---

func TestHandleQualityBaseline_runsWhenIntervalElapsed(t *testing.T) {
	d, _, _ := newTestDaemon(t)

	var calls int
	d.runQualityBaseline = func() error { calls++; return nil }
	d.qualityInterval = 5 * time.Minute

	// lastQualityBaseline is zero — interval has definitely elapsed
	d.handleQualityBaseline()

	if calls != 1 {
		t.Errorf("expected 1 baseline run, got %d", calls)
	}
}

func TestHandleQualityBaseline_skipsWhenTooSoon(t *testing.T) {
	d, _, _ := newTestDaemon(t)

	var calls int
	d.runQualityBaseline = func() error { calls++; return nil }
	d.qualityInterval = 5 * time.Minute

	now := time.Now()
	d.nowFn = func() time.Time { return now }
	d.lastQualityBaseline = now.Add(-1 * time.Minute) // ran 1 minute ago

	d.handleQualityBaseline()

	if calls != 0 {
		t.Errorf("expected 0 baseline runs (interval not elapsed), got %d", calls)
	}
}

func TestHandleQualityBaseline_disabledWhenIntervalZero(t *testing.T) {
	d, _, _ := newTestDaemon(t)

	var calls int
	d.runQualityBaseline = func() error { calls++; return nil }
	d.qualityInterval = 0 // disabled

	d.handleQualityBaseline()

	if calls != 0 {
		t.Errorf("expected 0 baseline runs (interval=0 means disabled), got %d", calls)
	}
}

func TestHandleQualityBaseline_updatesLastRunTime(t *testing.T) {
	d, _, _ := newTestDaemon(t)

	d.runQualityBaseline = func() error { return nil }
	d.qualityInterval = 5 * time.Minute

	now := time.Now()
	d.nowFn = func() time.Time { return now }

	d.handleQualityBaseline()

	if !d.lastQualityBaseline.Equal(now) {
		t.Errorf("expected lastQualityBaseline=%v, got %v", now, d.lastQualityBaseline)
	}
}

func TestHandleQualityBaseline_updatesLastRunOnError(t *testing.T) {
	d, _, _ := newTestDaemon(t)

	d.runQualityBaseline = func() error { return fmt.Errorf("check failed") }
	d.qualityInterval = 5 * time.Minute

	now := time.Now()
	d.nowFn = func() time.Time { return now }

	d.handleQualityBaseline() // should not panic; should still update timestamp

	if !d.lastQualityBaseline.Equal(now) {
		t.Errorf("expected lastQualityBaseline updated even on error")
	}
}

func TestHandleQualityBaseline_runsAgainAfterInterval(t *testing.T) {
	d, _, _ := newTestDaemon(t)

	var calls int
	d.runQualityBaseline = func() error { calls++; return nil }
	d.qualityInterval = 5 * time.Minute

	base := time.Now()
	d.nowFn = func() time.Time { return base }

	// First run
	d.handleQualityBaseline()
	if calls != 1 {
		t.Fatalf("expected 1 call after first run, got %d", calls)
	}

	// Advance by less than interval — should not run
	d.nowFn = func() time.Time { return base.Add(4 * time.Minute) }
	d.handleQualityBaseline()
	if calls != 1 {
		t.Errorf("expected no call within interval, got %d total", calls)
	}

	// Advance past interval — should run again
	d.nowFn = func() time.Time { return base.Add(6 * time.Minute) }
	d.handleQualityBaseline()
	if calls != 2 {
		t.Errorf("expected 2 calls after interval elapsed, got %d", calls)
	}
}

func TestCooldown_disabledWhenZero_dedupsIdenticalTickets(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t, []string{"ct-reviewer"})
	agents.Register("reviewer", "reviewer", nil)
	// nudgeCooldown is 0 (default in test helper) — no time-based suppression

	id, _ := issues.Create("Add feature", "task", nil, nil, nil)
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 5)

	// First call sends, subsequent calls deduped by digest
	d.handleInReviewTickets()
	d.handleInReviewTickets()
	d.handleInReviewTickets()

	if len(*sent) != 1 {
		t.Errorf("expected 1 nudge (digest dedup), got %d", len(*sent))
	}
}

// --- Working agent suppression tests ---

func TestHandleOpenTickets_skipsWhenConductorWorking(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t, []string{"ct-conductor"})
	agents.Register("conductor", "conductor", nil)
	agents.UpdateStatus("conductor", "working")

	id, _ := issues.Create("Open ticket", "task", nil, nil, nil)
	issues.UpdateStatus(id, "open")

	d.handleOpenTickets()

	if len(*sent) != 0 {
		t.Errorf("expected 0 nudges (conductor is working), got %d", len(*sent))
	}
}

func TestHandleOpenTickets_nudgesWhenConductorIdle(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t, []string{"ct-conductor"})
	agents.Register("conductor", "conductor", nil)
	// status defaults to idle

	id, _ := issues.Create("Open ticket", "task", nil, nil, nil)
	issues.UpdateStatus(id, "open")

	d.handleOpenTickets()

	if len(*sent) != 1 {
		t.Errorf("expected 1 nudge (conductor is idle), got %d", len(*sent))
	}
}

func TestHandleDraftTickets_skipsWhenArchitectWorking(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t, []string{"ct-architect"})
	agents.Register("architect", "architect", nil)
	agents.UpdateStatus("architect", "working")

	id, _ := issues.Create("Draft ticket", "task", nil, nil, nil)
	_ = id // ticket starts as draft

	d.handleDraftTickets()

	if len(*sent) != 0 {
		t.Errorf("expected 0 nudges (architect is working), got %d", len(*sent))
	}
}

func TestHandleRepairingTickets_skipsWhenConductorWorking(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t, []string{"ct-conductor"})
	agents.Register("conductor", "conductor", nil)
	agents.UpdateStatus("conductor", "working")

	id, _ := issues.Create("Fix bug", "task", nil, nil, nil)
	issues.UpdateStatus(id, "repairing")

	d.handleRepairingTickets()

	if len(*sent) != 0 {
		t.Errorf("expected 0 nudges (conductor is working), got %d", len(*sent))
	}
}

func TestHandleInReviewTickets_skipsWorkingReviewer(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t,
		[]string{"ct-reviewer-1", "ct-reviewer-2"})
	agents.Register("reviewer-1", "reviewer", nil)
	agents.Register("reviewer-2", "reviewer", nil)
	agents.UpdateStatus("reviewer-1", "working") // busy

	id, _ := issues.Create("Review me", "task", nil, nil, nil)
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 42)

	d.handleInReviewTickets()

	// Only reviewer-2 should get nudged
	if len(*sent) != 1 {
		t.Fatalf("expected 1 nudge (working reviewer skipped), got %d", len(*sent))
	}
	if (*sent)[0].session != "ct-reviewer-2" {
		t.Errorf("expected nudge to ct-reviewer-2, got %q", (*sent)[0].session)
	}
}

// --- Digest dedup tests ---

func TestDigest_suppressesDuplicateNudge(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t, []string{"ct-conductor"})
	agents.Register("conductor", "conductor", nil)

	now := time.Now()
	withCooldown(d, 5*time.Minute, now)

	id, _ := issues.Create("Fix bug", "task", nil, nil, nil)
	issues.UpdateStatus(id, "repairing")

	// First nudge — should send
	d.handleRepairingTickets()
	if len(*sent) != 1 {
		t.Fatalf("expected 1 nudge on first call, got %d", len(*sent))
	}

	// Advance past cooldown — same tickets → should NOT re-send
	d.nowFn = func() time.Time { return now.Add(10 * time.Minute) }
	d.handleRepairingTickets()
	if len(*sent) != 1 {
		t.Errorf("expected no re-nudge for same ticket set, got %d total", len(*sent))
	}
}

func TestDigest_nudgesWhenTicketSetChanges(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t, []string{"ct-conductor"})
	agents.Register("conductor", "conductor", nil)

	now := time.Now()
	withCooldown(d, 5*time.Minute, now)

	id1, _ := issues.Create("Fix bug A", "task", nil, nil, nil)
	issues.UpdateStatus(id1, "repairing")

	// First nudge
	d.handleRepairingTickets()
	if len(*sent) != 1 {
		t.Fatalf("expected 1 nudge, got %d", len(*sent))
	}

	// Add a new repairing ticket — digest changes
	id2, _ := issues.Create("Fix bug B", "task", nil, nil, nil)
	issues.UpdateStatus(id2, "repairing")

	// Advance past cooldown so the changed digest can fire
	d.nowFn = func() time.Time { return now.Add(10 * time.Minute) }
	d.handleRepairingTickets()
	if len(*sent) != 2 {
		t.Errorf("expected 2 nudges (ticket set changed + cooldown expired), got %d", len(*sent))
	}
}

// --- Stale worktree pruning tests ---

func TestHandleStaleWorktrees_callsPruneFunction(t *testing.T) {
	d, _, _ := newTestDaemon(t)

	var calls int
	d.pruneStaleWorktrees = func() error {
		calls++
		return nil
	}

	d.handleStaleWorktrees()

	if calls != 1 {
		t.Errorf("expected pruneStaleWorktrees called once, got %d", calls)
	}
}

func TestHandleStaleWorktrees_logsErrorWithoutPanicking(t *testing.T) {
	d, _, _ := newTestDaemon(t)

	d.pruneStaleWorktrees = func() error {
		return fmt.Errorf("git worktree remove failed")
	}

	// Should not panic
	d.handleStaleWorktrees()
}

func TestHandleStaleWorktrees_calledEachPollCycle(t *testing.T) {
	d, _, _ := newTestDaemon(t)

	var calls int
	d.pruneStaleWorktrees = func() error {
		calls++
		return nil
	}

	d.poll()

	if calls != 1 {
		t.Errorf("expected pruneStaleWorktrees called once per poll, got %d", calls)
	}
}

// --- Stuck agent detection tests ---

func TestHandleStuckAgents_escalatesToMayor(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t, []string{"ct-mayor"})

	agents.Register("flint", "prole", nil)
	id, _ := issues.Create("Implement auth", "task", nil, nil, nil)
	agents.SetCurrentIssue("flint", &id)

	d.stuckAgentThreshold = 30 * time.Minute
	d.nowFn = func() time.Time { return time.Now().Add(2 * time.Hour) }

	d.handleStuckAgents()

	if len(*sent) != 1 {
		t.Fatalf("expected 1 escalation message, got %d", len(*sent))
	}
	if (*sent)[0].session != "ct-mayor" {
		t.Errorf("expected message to ct-mayor, got %q", (*sent)[0].session)
	}
	if !containsAll((*sent)[0].msg, "flint", "ESCALATION") {
		t.Errorf("escalation message missing expected content: %q", (*sent)[0].msg)
	}
}

func TestHandleStuckAgents_includesTicketInfo(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t, []string{"ct-mayor"})

	agents.Register("granite", "prole", nil)
	id, _ := issues.Create("Wire artisan command", "task", nil, nil, nil)
	agents.SetCurrentIssue("granite", &id)

	d.stuckAgentThreshold = 30 * time.Minute
	d.nowFn = func() time.Time { return time.Now().Add(2 * time.Hour) }

	d.handleStuckAgents()

	if len(*sent) != 1 {
		t.Fatalf("expected 1 escalation message, got %d", len(*sent))
	}
	ticketRef := fmt.Sprintf("NC-%d", id)
	if !strings.Contains((*sent)[0].msg, ticketRef) {
		t.Errorf("expected ticket ref %q in message: %q", ticketRef, (*sent)[0].msg)
	}
}

func TestHandleStuckAgents_noTicketInfoWhenUnassigned(t *testing.T) {
	d, _, agents, sent := newTestDaemonWithSessions(t, []string{"ct-mayor"})

	agents.Register("slate", "prole", nil)
	agents.UpdateStatus("slate", "working")

	d.stuckAgentThreshold = 30 * time.Minute
	d.nowFn = func() time.Time { return time.Now().Add(2 * time.Hour) }

	d.handleStuckAgents()

	if len(*sent) != 1 {
		t.Fatalf("expected 1 escalation message, got %d", len(*sent))
	}
	if !strings.Contains((*sent)[0].msg, "no assigned ticket") {
		t.Errorf("expected 'no assigned ticket' in message: %q", (*sent)[0].msg)
	}
}

func TestHandleStuckAgents_noEscalationWhenBelowThreshold(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t, []string{"ct-mayor"})

	agents.Register("obsidian", "prole", nil)
	id, _ := issues.Create("Some task", "task", nil, nil, nil)
	agents.SetCurrentIssue("obsidian", &id)

	// Threshold is 2 hours but we only advance 30 minutes
	d.stuckAgentThreshold = 2 * time.Hour
	d.nowFn = func() time.Time { return time.Now().Add(30 * time.Minute) }

	d.handleStuckAgents()

	if len(*sent) != 0 {
		t.Errorf("expected 0 escalations (below threshold), got %d", len(*sent))
	}
}

func TestHandleStuckAgents_noEscalationWhenMayorNotRunning(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t, nil)

	agents.Register("quartz", "prole", nil)
	id, _ := issues.Create("Some task", "task", nil, nil, nil)
	agents.SetCurrentIssue("quartz", &id)

	d.stuckAgentThreshold = 30 * time.Minute
	d.nowFn = func() time.Time { return time.Now().Add(2 * time.Hour) }

	d.handleStuckAgents()

	if len(*sent) != 0 {
		t.Errorf("expected 0 escalations (Mayor not running), got %d", len(*sent))
	}
}

func TestHandleStuckAgents_skipsIdleAgents(t *testing.T) {
	d, _, agents, sent := newTestDaemonWithSessions(t, []string{"ct-mayor"})

	agents.Register("idle-agent", "prole", nil)

	d.stuckAgentThreshold = 30 * time.Minute
	d.nowFn = func() time.Time { return time.Now().Add(2 * time.Hour) }

	d.handleStuckAgents()

	if len(*sent) != 0 {
		t.Errorf("expected 0 escalations (agent is idle), got %d", len(*sent))
	}
}

func TestHandleStuckAgents_skipsDeadAgents(t *testing.T) {
	d, _, agents, sent := newTestDaemonWithSessions(t, []string{"ct-mayor"})

	agents.Register("dead-agent", "prole", nil)
	agents.UpdateStatus("dead-agent", "dead")

	d.stuckAgentThreshold = 30 * time.Minute
	d.nowFn = func() time.Time { return time.Now().Add(2 * time.Hour) }

	d.handleStuckAgents()

	if len(*sent) != 0 {
		t.Errorf("expected 0 escalations (agent is dead), got %d", len(*sent))
	}
}

func TestHandleStuckAgents_disabledWhenThresholdZero(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t, []string{"ct-mayor"})

	agents.Register("basalt", "prole", nil)
	id, _ := issues.Create("Some task", "task", nil, nil, nil)
	agents.SetCurrentIssue("basalt", &id)

	// stuckAgentThreshold defaults to 0 in test helper — feature disabled
	d.nowFn = func() time.Time { return time.Now().Add(24 * time.Hour) }

	d.handleStuckAgents()

	if len(*sent) != 0 {
		t.Errorf("expected 0 escalations (threshold=0 means disabled), got %d", len(*sent))
	}
}

func TestHandleStuckAgents_cooldownSuppressesRepeatEscalation(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t, []string{"ct-mayor"})

	agents.Register("flint", "prole", nil)
	id, _ := issues.Create("Some task", "task", nil, nil, nil)
	agents.SetCurrentIssue("flint", &id)

	base := time.Now()
	d.stuckAgentThreshold = 30 * time.Minute
	d.nudgeCooldown = 1 * time.Hour
	d.nowFn = func() time.Time { return base.Add(2 * time.Hour) }

	d.handleStuckAgents()
	if len(*sent) != 1 {
		t.Fatalf("expected 1 escalation on first call, got %d", len(*sent))
	}

	// Same time: within cooldown — suppressed
	d.handleStuckAgents()
	if len(*sent) != 1 {
		t.Errorf("expected no repeat escalation within cooldown, got %d total", len(*sent))
	}
}

func TestHandleStuckAgents_cooldownIsPerAgent(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t, []string{"ct-mayor"})

	agents.Register("agent-a", "prole", nil)
	agents.Register("agent-b", "prole", nil)

	idA, _ := issues.Create("Task A", "task", nil, nil, nil)
	idB, _ := issues.Create("Task B", "task", nil, nil, nil)
	agents.SetCurrentIssue("agent-a", &idA)
	agents.SetCurrentIssue("agent-b", &idB)

	base := time.Now()
	d.stuckAgentThreshold = 30 * time.Minute
	d.nudgeCooldown = 1 * time.Hour
	d.nowFn = func() time.Time { return base.Add(2 * time.Hour) }

	// Both agents escalated on first call
	d.handleStuckAgents()
	if len(*sent) != 2 {
		t.Fatalf("expected 2 escalations (one per agent), got %d", len(*sent))
	}

	// Both suppressed by cooldown
	d.handleStuckAgents()
	if len(*sent) != 2 {
		t.Errorf("expected no additional escalations within cooldown, got %d total", len(*sent))
	}

	// Advance past cooldown — both escalate again
	d.nowFn = func() time.Time { return base.Add(4 * time.Hour) }
	d.handleStuckAgents()
	if len(*sent) != 4 {
		t.Errorf("expected 4 total escalations after cooldown expiry, got %d", len(*sent))
	}
}

func TestHandleStuckAgents_skipsNullStatusChangedAt(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("creating test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	cfg := &config.Config{
		TicketPrefix: "NC",
		ProjectRoot:  t.TempDir(),
	}

	agents := repo.NewAgentRepo(conn, nil)
	agents.Register("null-ts-agent", "prole", nil)
	// Set status=working directly without setting status_changed_at, simulating
	// an agent created before the status_changed_at column was added.
	conn.Exec(`UPDATE agents SET status = 'working', status_changed_at = NULL WHERE name = 'null-ts-agent'`)

	sessions := map[string]bool{"ct-mayor": true}
	var sent []sentMessage

	d := &Daemon{
		cfg:             cfg,
		issues:          repo.NewIssueRepo(conn, nil),
		agents:          agents,
		logger:          log.New(io.Discard, "", 0),
		stop:            make(chan struct{}),
		sessionExists:   func(s string) bool { return sessions[s] },
		sendKeys:        func(s, msg string) error { sent = append(sent, sentMessage{session: s, msg: msg}); return nil },
		resetWorktree:   func(string) error { return nil },
		lastNudged:      make(map[string]time.Time),
		lastNudgeDigest: make(map[string]string),
		stuckAgentThreshold: 30 * time.Minute,
		nowFn:           func() time.Time { return time.Now().Add(24 * time.Hour) },
	}

	d.handleStuckAgents()

	if len(sent) != 0 {
		t.Errorf("expected 0 escalations for agent with NULL status_changed_at, got %d", len(sent))
	}
}

func TestHandleStuckAgents_skipsMayor(t *testing.T) {
	d, issues, agents, sent := newTestDaemonWithSessions(t, []string{"ct-mayor"})

	// Mayor itself is working past the threshold
	agents.Register("mayor", "mayor", nil)
	id, _ := issues.Create("Some mayor task", "task", nil, nil, nil)
	agents.SetCurrentIssue("mayor", &id)

	d.stuckAgentThreshold = 30 * time.Minute
	d.nowFn = func() time.Time { return time.Now().Add(2 * time.Hour) }

	d.handleStuckAgents()

	if len(*sent) != 0 {
		t.Errorf("expected 0 escalations (Mayor must not escalate itself), got %d", len(*sent))
	}
}

func TestHandleStaleWorktrees_respectsInterval(t *testing.T) {
	d, _, _ := newTestDaemon(t)
	base := time.Now()
	d.nowFn = func() time.Time { return base }
	d.worktreeInterval = 5 * time.Minute

	var calls int
	d.pruneStaleWorktrees = func() error {
		calls++
		return nil
	}

	// First call: interval not elapsed — should run (zero time → always run first time)
	d.handleStaleWorktrees()
	if calls != 1 {
		t.Fatalf("expected 1 call on first invocation, got %d", calls)
	}

	// Second call immediately: interval not elapsed — should NOT run
	d.handleStaleWorktrees()
	if calls != 1 {
		t.Errorf("expected no call within interval, got %d", calls)
	}

	// Advance past interval
	d.nowFn = func() time.Time { return base.Add(6 * time.Minute) }
	d.handleStaleWorktrees()
	if calls != 2 {
		t.Errorf("expected 2 calls after interval elapsed, got %d", calls)
	}
}

func TestHandleEpicAutoClose_closesEpicWhenAllChildrenClosed(t *testing.T) {
	d, issues, _ := newTestDaemon(t)

	epicID, _ := issues.Create("Epic A", "epic", nil, nil, nil)
	issues.UpdateStatus(epicID, "open")
	child1, _ := issues.Create("Task 1", "task", &epicID, nil, nil)
	issues.UpdateStatus(child1, "closed")
	child2, _ := issues.Create("Task 2", "task", &epicID, nil, nil)
	issues.UpdateStatus(child2, "closed")

	d.handleEpicAutoClose()

	epic, err := issues.Get(epicID)
	if err != nil {
		t.Fatalf("Get epic: %v", err)
	}
	if epic.Status != "closed" {
		t.Errorf("expected epic status=closed, got %q", epic.Status)
	}
}

func TestHandleEpicAutoClose_noopWhenOpenChildExists(t *testing.T) {
	d, issues, _ := newTestDaemon(t)

	epicID, _ := issues.Create("Epic B", "epic", nil, nil, nil)
	issues.UpdateStatus(epicID, "open")
	child1, _ := issues.Create("Task 1", "task", &epicID, nil, nil)
	issues.UpdateStatus(child1, "closed")
	child2, _ := issues.Create("Task 2", "task", &epicID, nil, nil)
	issues.UpdateStatus(child2, "open")

	d.handleEpicAutoClose()

	epic, _ := issues.Get(epicID)
	if epic.Status != "open" {
		t.Errorf("expected epic status=open, got %q", epic.Status)
	}
}

func TestHandleEpicAutoClose_noopWhenNoChildren(t *testing.T) {
	d, issues, _ := newTestDaemon(t)

	epicID, _ := issues.Create("Epic C", "epic", nil, nil, nil)
	issues.UpdateStatus(epicID, "open")

	d.handleEpicAutoClose()

	epic, _ := issues.Get(epicID)
	if epic.Status != "open" {
		t.Errorf("expected epic status=open, got %q", epic.Status)
	}
}

func TestHandleEpicAutoClose_noopWhenAlreadyClosed(t *testing.T) {
	d, issues, _ := newTestDaemon(t)

	epicID, _ := issues.Create("Epic D", "epic", nil, nil, nil)
	issues.UpdateStatus(epicID, "open")
	child1, _ := issues.Create("Task 1", "task", &epicID, nil, nil)
	issues.UpdateStatus(child1, "closed")
	issues.UpdateStatus(epicID, "closed")

	d.handleEpicAutoClose()

	epic, _ := issues.Get(epicID)
	if epic.Status != "closed" {
		t.Errorf("expected epic status=closed, got %q", epic.Status)
	}
}

func TestHandleEpicAutoClose_notifiesMayor(t *testing.T) {
	d, issues, _, sent := newTestDaemonWithSessions(t, []string{"ct-mayor"})

	epicID, _ := issues.Create("Big Feature", "epic", nil, nil, nil)
	issues.UpdateStatus(epicID, "open")
	child1, _ := issues.Create("Task 1", "task", &epicID, nil, nil)
	issues.UpdateStatus(child1, "closed")

	d.handleEpicAutoClose()

	if len(*sent) != 1 {
		t.Fatalf("expected 1 mayor message, got %d", len(*sent))
	}
	if !strings.Contains((*sent)[0].msg, "Big Feature") {
		t.Errorf("expected message to mention epic title, got %q", (*sent)[0].msg)
	}
	if (*sent)[0].session != "ct-mayor" {
		t.Errorf("expected message to mayor session, got %q", (*sent)[0].session)
	}
}

func TestHandleBackfillPRNumbers_backfillsMatchingBranch(t *testing.T) {
	d, issues, _ := newTestDaemon(t)

	id, err := issues.Create("My ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := issues.Assign(id, "copper", "prole/copper/NC-1"); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	issues.UpdateStatus(id, "in_review")

	d.lookupPRForBranch = func(branch string) (int, bool, error) {
		if branch == "prole/copper/NC-1" {
			return 42, true, nil
		}
		return 0, false, nil
	}

	d.handleBackfillPRNumbers()

	updated, err := issues.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !updated.PRNumber.Valid || updated.PRNumber.Int64 != 42 {
		t.Errorf("expected pr_number=42, got %v", updated.PRNumber)
	}
}

func TestHandleBackfillPRNumbers_skipsTicketsWithExistingPR(t *testing.T) {
	d, issues, _ := newTestDaemon(t)

	id, _ := issues.Create("Already has PR", "task", nil, nil, nil)
	issues.Assign(id, "copper", "prole/copper/NC-2")
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 99)

	var lookupCalls int
	d.lookupPRForBranch = func(branch string) (int, bool, error) {
		lookupCalls++
		return 0, false, nil
	}

	d.handleBackfillPRNumbers()

	if lookupCalls != 0 {
		t.Errorf("expected no GitHub lookups for tickets with existing PR, got %d", lookupCalls)
	}
}

func TestHandleBackfillPRNumbers_skipsTicketsWithNullBranch(t *testing.T) {
	d, issues, _ := newTestDaemon(t)

	id, _ := issues.Create("No branch", "task", nil, nil, nil)
	issues.UpdateStatus(id, "open")
	// No Assign call — branch remains NULL

	var lookupCalls int
	d.lookupPRForBranch = func(branch string) (int, bool, error) {
		lookupCalls++
		return 0, false, nil
	}

	d.handleBackfillPRNumbers()

	if lookupCalls != 0 {
		t.Errorf("expected no GitHub lookups for tickets with null branch, got %d", lookupCalls)
	}
}

func TestHandleBackfillPRNumbers_respectsInterval(t *testing.T) {
	d, issues, _ := newTestDaemon(t)

	id, _ := issues.Create("Interval test", "task", nil, nil, nil)
	issues.Assign(id, "copper", "prole/copper/NC-3")
	issues.UpdateStatus(id, "in_review")

	// Return not-found so the ticket is never backfilled and remains available
	// for the next interval — this lets us count handler invocations cleanly.
	var lookupCalls int
	d.lookupPRForBranch = func(branch string) (int, bool, error) {
		lookupCalls++
		return 0, false, nil
	}

	base := time.Now()
	d.nowFn = func() time.Time { return base }
	d.prBackfillInterval = 5 * time.Minute

	// First call: lastPRBackfill is zero, should run
	d.handleBackfillPRNumbers()
	if lookupCalls != 1 {
		t.Fatalf("expected 1 lookup on first call, got %d", lookupCalls)
	}

	// Second call immediately: interval not elapsed — should NOT run
	d.handleBackfillPRNumbers()
	if lookupCalls != 1 {
		t.Errorf("expected no lookup within interval, got %d", lookupCalls)
	}

	// Advance past interval
	d.nowFn = func() time.Time { return base.Add(6 * time.Minute) }
	d.handleBackfillPRNumbers()
	if lookupCalls != 2 {
		t.Errorf("expected 2 lookups after interval elapsed, got %d", lookupCalls)
	}
}

// --- Agent restart tests ---

// withRestartCapture replaces d.restartAgent with a stub that records calls.
// Returns a pointer to the slice of agent names that were restarted.
func withRestartCapture(d *Daemon) *[]string {
	var restarted []string
	d.restartAgent = func(agent *repo.Agent) error {
		restarted = append(restarted, agent.Name)
		return nil
	}
	return &restarted
}

func TestHandleOpenTickets_restartsConductorWhenDeadAndTicketsReady(t *testing.T) {
	d, issues, agents, _ := newTestDaemonWithSessions(t, nil) // no active sessions
	restarted := withRestartCapture(d)
	d.restartDeadAgents = true

	agents.Register("conductor", "conductor", nil)
	agents.UpdateStatus("conductor", "dead")

	id, _ := issues.Create("Open ticket", "task", nil, nil, nil)
	issues.UpdateStatus(id, "open")

	d.handleOpenTickets()

	if len(*restarted) != 1 || (*restarted)[0] != "conductor" {
		t.Errorf("expected conductor restart, got %v", *restarted)
	}
}

func TestHandleOpenTickets_restartsConductorWhenIdleNoSession(t *testing.T) {
	d, issues, agents, _ := newTestDaemonWithSessions(t, nil) // no active sessions
	restarted := withRestartCapture(d)
	d.restartDeadAgents = true

	agents.Register("conductor", "conductor", nil)
	// status is idle (default), no session

	id, _ := issues.Create("Open ticket", "task", nil, nil, nil)
	issues.UpdateStatus(id, "open")

	d.handleOpenTickets()

	if len(*restarted) != 1 || (*restarted)[0] != "conductor" {
		t.Errorf("expected conductor restart for idle conductor with no session, got %v", *restarted)
	}
}

func TestHandleOpenTickets_noRestartWhenDisabled(t *testing.T) {
	d, issues, agents, _ := newTestDaemonWithSessions(t, nil)
	restarted := withRestartCapture(d)
	d.restartDeadAgents = false // disabled

	agents.Register("conductor", "conductor", nil)
	agents.UpdateStatus("conductor", "dead")

	id, _ := issues.Create("Open ticket", "task", nil, nil, nil)
	issues.UpdateStatus(id, "open")

	d.handleOpenTickets()

	if len(*restarted) != 0 {
		t.Errorf("expected no restart when restartDeadAgents=false, got %v", *restarted)
	}
}

func TestHandleOpenTickets_noRestartWhenNoTickets(t *testing.T) {
	d, _, agents, _ := newTestDaemonWithSessions(t, nil)
	restarted := withRestartCapture(d)
	d.restartDeadAgents = true

	agents.Register("conductor", "conductor", nil)
	agents.UpdateStatus("conductor", "dead")

	// no open tickets
	d.handleOpenTickets()

	if len(*restarted) != 0 {
		t.Errorf("expected no restart when no open tickets, got %v", *restarted)
	}
}

func TestHandleOpenTickets_noRestartWhenConductorWorking(t *testing.T) {
	d, issues, agents, _ := newTestDaemonWithSessions(t, []string{"ct-conductor"})
	restarted := withRestartCapture(d)
	d.restartDeadAgents = true

	agents.Register("conductor", "conductor", nil)
	agents.UpdateStatus("conductor", "working")
	agents.SetTmuxSession("conductor", "ct-conductor")

	id, _ := issues.Create("Open ticket", "task", nil, nil, nil)
	issues.UpdateStatus(id, "open")

	d.handleOpenTickets()

	// Session exists and conductor is working — no restart, just skip nudge
	if len(*restarted) != 0 {
		t.Errorf("expected no restart when conductor session active, got %v", *restarted)
	}
}

func TestHandleOpenTickets_restartCooldownPreventsSpam(t *testing.T) {
	d, issues, agents, _ := newTestDaemonWithSessions(t, nil)
	restarted := withRestartCapture(d)
	d.restartDeadAgents = true
	d.restartCooldown = 5 * time.Minute

	base := time.Now()
	d.nowFn = func() time.Time { return base }

	agents.Register("conductor", "conductor", nil)
	agents.UpdateStatus("conductor", "dead")

	id, _ := issues.Create("Open ticket", "task", nil, nil, nil)
	issues.UpdateStatus(id, "open")

	// First call: restart happens
	d.handleOpenTickets()
	if len(*restarted) != 1 {
		t.Fatalf("expected 1 restart on first call, got %d", len(*restarted))
	}

	// Second call within cooldown: no restart
	d.handleOpenTickets()
	if len(*restarted) != 1 {
		t.Errorf("expected no restart within cooldown, got %d restarts", len(*restarted))
	}

	// After cooldown: restart again
	d.nowFn = func() time.Time { return base.Add(6 * time.Minute) }
	d.handleOpenTickets()
	if len(*restarted) != 2 {
		t.Errorf("expected 2 restarts after cooldown elapsed, got %d", len(*restarted))
	}
}

func TestHandleInReviewTickets_restartsDeadReviewerWhenTicketsReady(t *testing.T) {
	d, issues, agents, _ := newTestDaemonWithSessions(t, nil) // no active sessions
	restarted := withRestartCapture(d)
	d.restartDeadAgents = true

	agents.Register("reviewer", "reviewer", nil)
	agents.UpdateStatus("reviewer", "dead")

	id, _ := issues.Create("Add auth", "task", nil, nil, nil)
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 42)

	d.handleInReviewTickets()

	if len(*restarted) != 1 || (*restarted)[0] != "reviewer" {
		t.Errorf("expected reviewer restart, got %v", *restarted)
	}
}

func TestHandleInReviewTickets_restartsIdleReviewerWithNoSession(t *testing.T) {
	d, issues, agents, _ := newTestDaemonWithSessions(t, nil)
	restarted := withRestartCapture(d)
	d.restartDeadAgents = true

	agents.Register("reviewer", "reviewer", nil)
	// status is idle (default), no session recorded

	id, _ := issues.Create("Add auth", "task", nil, nil, nil)
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 42)

	d.handleInReviewTickets()

	if len(*restarted) != 1 || (*restarted)[0] != "reviewer" {
		t.Errorf("expected reviewer restart for idle reviewer with no session, got %v", *restarted)
	}
}

func TestHandleInReviewTickets_noRestartWhenDisabled(t *testing.T) {
	d, issues, agents, _ := newTestDaemonWithSessions(t, nil)
	restarted := withRestartCapture(d)
	d.restartDeadAgents = false

	agents.Register("reviewer", "reviewer", nil)
	agents.UpdateStatus("reviewer", "dead")

	id, _ := issues.Create("Add auth", "task", nil, nil, nil)
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 42)

	d.handleInReviewTickets()

	if len(*restarted) != 0 {
		t.Errorf("expected no restart when restartDeadAgents=false, got %v", *restarted)
	}
}

func TestHandleInReviewTickets_restartsMultipleDeadReviewers(t *testing.T) {
	d, issues, agents, _ := newTestDaemonWithSessions(t, nil)
	restarted := withRestartCapture(d)
	d.restartDeadAgents = true

	agents.Register("reviewer-1", "reviewer", nil)
	agents.Register("reviewer-2", "reviewer", nil)
	agents.UpdateStatus("reviewer-1", "dead")
	agents.UpdateStatus("reviewer-2", "dead")

	id, _ := issues.Create("Add auth", "task", nil, nil, nil)
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 42)

	d.handleInReviewTickets()

	if len(*restarted) != 2 {
		t.Errorf("expected 2 reviewer restarts, got %v", *restarted)
	}
}

func TestHandleInReviewTickets_reviewerRestartCooldownPreventsSpam(t *testing.T) {
	d, issues, agents, _ := newTestDaemonWithSessions(t, nil)
	restarted := withRestartCapture(d)
	d.restartDeadAgents = true
	d.restartCooldown = 5 * time.Minute

	base := time.Now()
	d.nowFn = func() time.Time { return base }

	agents.Register("reviewer", "reviewer", nil)
	agents.UpdateStatus("reviewer", "dead")

	id, _ := issues.Create("Add auth", "task", nil, nil, nil)
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 42)

	// First call: restart
	d.handleInReviewTickets()
	if len(*restarted) != 1 {
		t.Fatalf("expected 1 restart on first call, got %d", len(*restarted))
	}

	// Within cooldown: no restart
	d.handleInReviewTickets()
	if len(*restarted) != 1 {
		t.Errorf("expected no restart within cooldown, got %d", len(*restarted))
	}

	// After cooldown: restart again
	d.nowFn = func() time.Time { return base.Add(6 * time.Minute) }
	d.handleInReviewTickets()
	if len(*restarted) != 2 {
		t.Errorf("expected 2 restarts after cooldown elapsed, got %d", len(*restarted))
	}
}

func TestHandleInReviewTickets_skipsReviewerWithActiveSession(t *testing.T) {
	// reviewer-1 is dead with no session → restart; reviewer-2 is alive → no restart
	d, issues, agents, _ := newTestDaemonWithSessions(t, []string{"ct-reviewer-2"})
	restarted := withRestartCapture(d)
	d.restartDeadAgents = true

	agents.Register("reviewer-1", "reviewer", nil)
	agents.Register("reviewer-2", "reviewer", nil)
	agents.UpdateStatus("reviewer-1", "dead")
	// reviewer-2: idle with session → should nudge not restart

	id, _ := issues.Create("Add auth", "task", nil, nil, nil)
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 42)

	d.handleInReviewTickets()

	// reviewer-2 has active session → nudge path taken, no restarts needed
	if len(*restarted) != 0 {
		t.Errorf("expected no restart when active reviewer session exists, got %v", *restarted)
	}
}
