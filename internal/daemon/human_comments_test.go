package daemon

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/katerina7479/company_town/internal/repo"
)

// makeCommentDaemon returns a daemon wired with a fixed getReviewCommentsFn and
// real DB repos. The returned IssueRepo and AgentRepo are backed by the same
// in-memory test database used by newTestDaemon.
func makeCommentDaemon(t *testing.T, comments []prComment) *Daemon {
	t.Helper()
	d, _, _ := newTestDaemon(t)
	d.getReviewCommentsFn = func(_ int) ([]prComment, error) {
		return comments, nil
	}
	return d
}

// issueInStatus creates a test issue, advances it to the given status, and returns it.
func issueInStatus(t *testing.T, d *Daemon, status string) *repo.Issue {
	t.Helper()
	id, err := d.issues.Create("test ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if status != "draft" {
		if err := d.issues.UpdateStatus(id, status); err != nil {
			t.Fatalf("UpdateStatus(%q): %v", status, err)
		}
	}
	issue, err := d.issues.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return issue
}

func TestCheckForHumanComments_SkipsUnderReview(t *testing.T) {
	comments := []prComment{{Author: "katerina7479", Body: "LGTM, nice work"}}
	d := makeCommentDaemon(t, comments)

	issue := issueInStatus(t, d, "under_review")
	d.checkForHumanComments(issue, 97)

	got, _ := d.issues.Get(issue.ID)
	if got.Status != "under_review" {
		t.Errorf("status: got %q, want \"under_review\"", got.Status)
	}
}

func TestCheckForHumanComments_SkipsRepairing(t *testing.T) {
	comments := []prComment{{Author: "katerina7479", Body: "please fix X"}}
	d := makeCommentDaemon(t, comments)

	issue := issueInStatus(t, d, "repairing")
	d.checkForHumanComments(issue, 97)

	got, _ := d.issues.Get(issue.ID)
	if got.Status != "repairing" {
		t.Errorf("status: got %q, want \"repairing\"", got.Status)
	}
}

func TestCheckForHumanComments_SkipsClosed(t *testing.T) {
	comments := []prComment{{Author: "katerina7479", Body: "looks good"}}
	d := makeCommentDaemon(t, comments)

	issue := issueInStatus(t, d, "closed")
	d.checkForHumanComments(issue, 97)

	got, _ := d.issues.Get(issue.ID)
	if got.Status != "closed" {
		t.Errorf("status: got %q, want \"closed\"", got.Status)
	}
}

func TestCheckForHumanComments_SkipsSentinelCommentOnPROpen(t *testing.T) {
	comments := []prComment{{Author: "katerina7479", Body: "[ct-reviewer] LGTM at abc123."}}
	d := makeCommentDaemon(t, comments)

	issue := issueInStatus(t, d, "pr_open")
	d.checkForHumanComments(issue, 97)

	got, _ := d.issues.Get(issue.ID)
	if got.Status != "pr_open" {
		t.Errorf("status: got %q, want \"pr_open\"", got.Status)
	}
}

func TestCheckForHumanComments_FiresOnPlainCommentOnPROpen(t *testing.T) {
	comments := []prComment{{Author: "katerina7479", Body: "Looks ok but fix X"}}
	d := makeCommentDaemon(t, comments)

	issue := issueInStatus(t, d, "pr_open")
	d.checkForHumanComments(issue, 97)

	got, _ := d.issues.Get(issue.ID)
	if got.Status != "repairing" {
		t.Errorf("status: got %q, want \"repairing\"", got.Status)
	}
}

func TestCheckForHumanComments_MixedComments(t *testing.T) {
	comments := []prComment{
		{Author: "katerina7479", Body: "[ct-reviewer] LGTM"},
		{Author: "katerina7479", Body: "Actually, please fix the tests"},
	}
	d := makeCommentDaemon(t, comments)

	issue := issueInStatus(t, d, "pr_open")
	d.checkForHumanComments(issue, 97)

	got, _ := d.issues.Get(issue.ID)
	if got.Status != "repairing" {
		t.Errorf("status: got %q, want \"repairing\" (plain comment should fire)", got.Status)
	}
}

func TestCheckForHumanComments_SentinelWithLeadingWhitespace(t *testing.T) {
	comments := []prComment{{Author: "katerina7479", Body: "  \n[ct-reviewer] LGTM"}}
	d := makeCommentDaemon(t, comments)

	issue := issueInStatus(t, d, "pr_open")
	d.checkForHumanComments(issue, 97)

	got, _ := d.issues.Get(issue.ID)
	if got.Status != "pr_open" {
		t.Errorf("status: got %q, want \"pr_open\" (TrimSpace should strip leading whitespace)", got.Status)
	}
}

func TestCheckForHumanComments_EmptyBody(t *testing.T) {
	// Empty body = not a reviewer comment; treat as human (e.g. GitHub approve button).
	comments := []prComment{{Author: "katerina7479", Body: ""}}
	d := makeCommentDaemon(t, comments)

	issue := issueInStatus(t, d, "pr_open")
	d.checkForHumanComments(issue, 97)

	got, _ := d.issues.Get(issue.ID)
	if got.Status != "repairing" {
		t.Errorf("status: got %q, want \"repairing\" (empty body should fire)", got.Status)
	}
}

func TestCheckForHumanComments_SkipsBotAuthor(t *testing.T) {
	comments := []prComment{{Author: "some-bot", IsBot: true, Body: "automated check passed"}}
	d := makeCommentDaemon(t, comments)

	issue := issueInStatus(t, d, "pr_open")
	d.checkForHumanComments(issue, 97)

	got, _ := d.issues.Get(issue.ID)
	if got.Status != "pr_open" {
		t.Errorf("status: got %q, want \"pr_open\" (bot comment should be skipped)", got.Status)
	}
}

func TestCheckForHumanComments_ExcerptTruncatesAtRuneBoundary(t *testing.T) {
	// Build a body where a multi-byte UTF-8 character straddles the 120-byte
	// mark. "é" is 2 bytes (0xC3 0xA9); placing it so that byte 120 lands
	// inside it would corrupt the string if we sliced by bytes. We want the
	// final excerpt to be valid UTF-8 and end with the ellipsis, not garbage.
	//
	// 119 ASCII 'a' chars + 'é' (2 bytes) = 121 bytes, 120 runes → exactly at
	// the boundary. Appending more ASCII pushes it past 120 runes so truncation
	// fires.
	body := strings.Repeat("a", 119) + "é" + strings.Repeat("b", 10) // 131 bytes, 130 runes
	comments := []prComment{{Author: "human", Body: body}}
	d := makeCommentDaemon(t, comments)

	issue := issueInStatus(t, d, "pr_open")
	d.checkForHumanComments(issue, 97)

	got, _ := d.issues.Get(issue.ID)
	if got.Status != "repairing" {
		t.Fatalf("expected repairing, got %q", got.Status)
	}
	if !got.RepairReason.Valid {
		t.Fatal("expected repair_reason to be set")
	}

	reason := got.RepairReason.String
	// Must be valid UTF-8 — the old byte-slice code could produce an invalid
	// sequence when the cut landed inside "é".
	if !utf8.ValidString(reason) {
		t.Errorf("repair_reason is not valid UTF-8: %q", reason)
	}
	// Must end with the ellipsis indicator (excerpt was truncated).
	if !strings.HasSuffix(reason, "…") {
		t.Errorf("expected truncated reason to end with '…', got: %q", reason)
	}
	// The 'é' must appear in the excerpt (it's at rune 119, within the 120-rune
	// limit).
	if !strings.Contains(reason, "é") {
		t.Errorf("expected 'é' to appear in excerpt (rune 119 is within limit), got: %q", reason)
	}
}

func TestCheckForHumanComments_ExcerptNotTruncatedIfShort(t *testing.T) {
	// A body shorter than 120 runes must not be truncated or have "…" appended.
	body := "please fix the linting errors"
	comments := []prComment{{Author: "human", Body: body}}
	d := makeCommentDaemon(t, comments)

	issue := issueInStatus(t, d, "pr_open")
	d.checkForHumanComments(issue, 97)

	got, _ := d.issues.Get(issue.ID)
	if !got.RepairReason.Valid {
		t.Fatal("expected repair_reason to be set")
	}
	if strings.Contains(got.RepairReason.String, "…") {
		t.Errorf("short body should not be truncated, got: %q", got.RepairReason.String)
	}
	if !strings.Contains(got.RepairReason.String, body) {
		t.Errorf("full body should appear in reason, got: %q", got.RepairReason.String)
	}
}

func TestGetReviewComments_ParsesBodyField(t *testing.T) {
	// Verify that parseReviewLine populates the Body field correctly.
	line := `{"author":"katerina7479","authorType":"User","state":"COMMENTED","body":"[ct-reviewer] LGTM at abc123."}`
	comment, ok := parseReviewLine([]byte(line))
	if !ok {
		t.Fatal("parseReviewLine returned ok=false")
	}
	if comment.Author != "katerina7479" {
		t.Errorf("Author: got %q, want \"katerina7479\"", comment.Author)
	}
	if comment.IsBot {
		t.Errorf("IsBot: got true, want false")
	}
	if comment.Body != "[ct-reviewer] LGTM at abc123." {
		t.Errorf("Body: got %q, want \"[ct-reviewer] LGTM at abc123.\"", comment.Body)
	}
	if comment.State != "COMMENTED" {
		t.Errorf("State: got %q, want \"COMMENTED\"", comment.State)
	}
}

func TestGetReviewComments_ParsesStateField(t *testing.T) {
	cases := []struct {
		state string
		isBot bool
	}{
		{"APPROVED", false},
		{"CHANGES_REQUESTED", false},
		{"COMMENTED", false},
		{"APPROVED", true},
	}
	for _, tc := range cases {
		authorType := "User"
		if tc.isBot {
			authorType = "Bot"
		}
		line := `{"author":"reviewer","authorType":"` + authorType + `","state":"` + tc.state + `","body":"review body"}`
		comment, ok := parseReviewLine([]byte(line))
		if !ok {
			t.Fatalf("parseReviewLine returned ok=false for state=%q", tc.state)
		}
		if comment.State != tc.state {
			t.Errorf("State: got %q, want %q", comment.State, tc.state)
		}
		if comment.IsBot != tc.isBot {
			t.Errorf("IsBot: got %v, want %v", comment.IsBot, tc.isBot)
		}
	}
}

// --- TDD tests approval detection ---

// tddIssuePROpen creates a tdd_tests issue in pr_open status.
func tddIssuePROpen(t *testing.T, d *Daemon) *repo.Issue {
	t.Helper()
	id, err := d.issues.Create("TDD tests ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := d.issues.UpdateType(id, "tdd_tests"); err != nil {
		t.Fatalf("UpdateType: %v", err)
	}
	if err := d.issues.UpdateStatus(id, "pr_open"); err != nil {
		t.Fatalf("UpdateStatus(pr_open): %v", err)
	}
	issue, err := d.issues.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	return issue
}

func TestCheckForTDDTestsApproval_HumanApproval_ClosesTicket(t *testing.T) {
	comments := []prComment{
		{Author: "katerina7479", IsBot: false, State: "APPROVED", Body: "LGTM, tests look good"},
	}
	d := makeCommentDaemon(t, comments)
	d.obs = &tickObservations{}

	issue := tddIssuePROpen(t, d)
	d.checkForHumanComments(issue, 42)

	got, _ := d.issues.Get(issue.ID)
	if got.Status != "closed" {
		t.Errorf("status: got %q, want \"closed\"", got.Status)
	}
	if d.obs.prEventsTDDApproved != 1 {
		t.Errorf("prEventsTDDApproved: got %d, want 1", d.obs.prEventsTDDApproved)
	}
}

func TestCheckForTDDTestsApproval_BotApproval_DoesNotClose(t *testing.T) {
	// Bot approvals must not close the ticket — only human approvals count.
	comments := []prComment{
		{Author: "github-actions[bot]", IsBot: true, State: "APPROVED", Body: ""},
	}
	d := makeCommentDaemon(t, comments)
	d.obs = &tickObservations{}

	issue := tddIssuePROpen(t, d)
	d.checkForHumanComments(issue, 42)

	got, _ := d.issues.Get(issue.ID)
	if got.Status != "pr_open" {
		t.Errorf("status: got %q, want \"pr_open\" (bot approval must be skipped)", got.Status)
	}
	if d.obs.prEventsTDDApproved != 0 {
		t.Errorf("prEventsTDDApproved: got %d, want 0", d.obs.prEventsTDDApproved)
	}
}

func TestCheckForTDDTestsApproval_CommentedNotApproved_NoClose(t *testing.T) {
	// A COMMENTED review on a tdd_tests ticket routes to repairing, not close.
	comments := []prComment{
		{Author: "katerina7479", IsBot: false, State: "COMMENTED", Body: "Please fix the test setup"},
	}
	d := makeCommentDaemon(t, comments)
	d.obs = &tickObservations{}

	issue := tddIssuePROpen(t, d)
	d.checkForHumanComments(issue, 42)

	got, _ := d.issues.Get(issue.ID)
	if got.Status != "repairing" {
		t.Errorf("status: got %q, want \"repairing\" (human comment without approval → repairing)", got.Status)
	}
	if d.obs.prEventsTDDApproved != 0 {
		t.Errorf("prEventsTDDApproved: got %d, want 0", d.obs.prEventsTDDApproved)
	}
}

func TestCheckForTDDTestsApproval_NoComments_NoClose(t *testing.T) {
	// Empty comment list → no approval → no close.
	d := makeCommentDaemon(t, nil)
	d.obs = &tickObservations{}

	issue := tddIssuePROpen(t, d)
	d.checkForHumanComments(issue, 42)

	got, _ := d.issues.Get(issue.ID)
	if got.Status != "pr_open" {
		t.Errorf("status: got %q, want \"pr_open\"", got.Status)
	}
}

func TestCheckForTDDTestsApproval_ApprovalTakesPrecedenceOverComment(t *testing.T) {
	// When a tdd_tests PR has both an APPROVED review and a human comment,
	// the APPROVED review wins and closes the ticket (not repairs it).
	comments := []prComment{
		{Author: "reviewer", IsBot: false, State: "APPROVED", Body: "Looks great"},
		{Author: "reviewer", IsBot: false, State: "COMMENTED", Body: "minor nit below"},
	}
	d := makeCommentDaemon(t, comments)
	d.obs = &tickObservations{}

	issue := tddIssuePROpen(t, d)
	d.checkForHumanComments(issue, 42)

	got, _ := d.issues.Get(issue.ID)
	if got.Status != "closed" {
		t.Errorf("status: got %q, want \"closed\" (APPROVED takes precedence over comment)", got.Status)
	}
}

func TestCheckForTDDTestsApproval_NonTDDTicket_ApprovalRoutesToRepairing(t *testing.T) {
	// For a regular (non-tdd_tests) ticket, an APPROVED review should route to
	// repairing via checkForHumanComments just like any other comment, because the
	// body is non-empty and non-sentinel.
	comments := []prComment{
		{Author: "katerina7479", IsBot: false, State: "APPROVED", Body: "LGTM"},
	}
	d := makeCommentDaemon(t, comments)

	issue := issueInStatus(t, d, "pr_open")
	d.checkForHumanComments(issue, 42)

	got, _ := d.issues.Get(issue.ID)
	// "task" type: APPROVED review is just a comment — routes to repairing.
	if got.Status != "repairing" {
		t.Errorf("status: got %q, want \"repairing\" (non-tdd_tests ticket should not be closed on approval)", got.Status)
	}
}

func TestCheckForTDDTestsApproval_ViaHandlePREvents(t *testing.T) {
	// Integration test: handlePREvents correctly closes a tdd_tests ticket in pr_open
	// when the PR has a human APPROVED review.
	d, issues, _, _ := newTestDaemonWithSessions(t, nil)

	id, _ := issues.Create("Test design for feature X", "task", nil, nil, nil)
	issues.UpdateType(id, "tdd_tests")
	issues.UpdateStatus(id, "pr_open")
	issues.SetPR(id, 99)

	d.getPRStateFn = newPRStateFn("MERGEABLE", "passing", nil)
	d.getReviewCommentsFn = func(_ int) ([]prComment, error) {
		return []prComment{
			{Author: "katerina7479", IsBot: false, State: "APPROVED", Body: "Tests look solid"},
		}, nil
	}

	d.handlePREvents()

	updated, _ := issues.Get(id)
	if updated.Status != "closed" {
		t.Errorf("expected tdd_tests ticket to be closed on human approval; got %q", updated.Status)
	}
}
