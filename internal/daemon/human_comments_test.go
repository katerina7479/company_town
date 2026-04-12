package daemon

import (
	"testing"

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
}
