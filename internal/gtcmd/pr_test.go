package gtcmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

func setupPRTestRepo(t *testing.T) *repo.IssueRepo {
	t.Helper()
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("creating test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return repo.NewIssueRepo(conn, nil)
}

func testCfg() *config.Config {
	return &config.Config{TicketPrefix: "nc"}
}

func TestFormatPRTitle(t *testing.T) {
	cases := []struct {
		prefix string
		id     int
		title  string
		want   string
	}{
		{"nc", 42, "Fix the bug", "[nc-42] Fix the bug"},
		{"CT", 1, "Add new feature", "[CT-1] Add new feature"},
		{"nc", 100, "Refactor auth layer", "[nc-100] Refactor auth layer"},
	}

	for _, tc := range cases {
		got := formatPRTitle(tc.prefix, tc.id, tc.title)
		if got != tc.want {
			t.Errorf("formatPRTitle(%q, %d, %q) = %q, want %q",
				tc.prefix, tc.id, tc.title, got, tc.want)
		}
	}
}

func TestFormatPRTitle_hasBracketPrefix(t *testing.T) {
	title := formatPRTitle("nc", 7, "Some work")
	if !strings.HasPrefix(title, "[nc-7] ") {
		t.Errorf("expected title to start with \"[nc-7] \", got %q", title)
	}
}

func TestFormatPRTitle_prefixCaseSensitive(t *testing.T) {
	lower := formatPRTitle("nc", 1, "title")
	upper := formatPRTitle("NC", 1, "title")
	if lower == upper {
		t.Errorf("expected prefix to be case-sensitive, but %q == %q", lower, upper)
	}
}

func TestParseTicketID(t *testing.T) {
	cases := []struct {
		input   string
		wantID  int
		wantErr bool
	}{
		{"58", 58, false},
		{"nc-58", 58, false},
		{"NC-58", 58, false},
		{"CT-100", 100, false},
		{"1", 1, false},
		{"prefix-42", 42, false},
		{"notanumber", 0, true},
		{"nc-notanumber", 0, true},
		{"nc-", 0, true},
		{"nc-58-2", 0, true},
	}

	for _, tc := range cases {
		id, err := parseTicketID(tc.input)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseTicketID(%q): expected error, got id=%d", tc.input, id)
			}
		} else {
			if err != nil {
				t.Errorf("parseTicketID(%q): unexpected error: %v", tc.input, err)
			} else if id != tc.wantID {
				t.Errorf("parseTicketID(%q) = %d, want %d", tc.input, id, tc.wantID)
			}
		}
	}
}

func TestPRUpdate_missingArgs(t *testing.T) {
	issues := setupPRTestRepo(t)
	err := prUpdate(issues, testCfg(), []string{})
	if err == nil {
		t.Fatal("expected usage error for 0 args, got nil")
	}
}

func TestPRUpdate_notFound(t *testing.T) {
	issues := setupPRTestRepo(t)
	err := prUpdate(issues, testCfg(), []string{"9999"})
	if err == nil {
		t.Fatal("expected error for non-existent ticket, got nil")
	}
}

func TestPRUpdate_wrongStatus(t *testing.T) {
	issues := setupPRTestRepo(t)

	_, _ = issues.Create("A task", "task", nil, nil, nil)
	issues.UpdateStatus(1, "in_review")

	err := prUpdate(issues, testCfg(), []string{"1"})
	if err == nil {
		t.Fatal("expected error when ticket is not in repairing status, got nil")
	}
	if !strings.Contains(err.Error(), "repairing") {
		t.Errorf("expected error to mention 'repairing', got: %v", err)
	}
}

func TestPRUpdate_wrongStatus_open(t *testing.T) {
	issues := setupPRTestRepo(t)

	_, _ = issues.Create("A task", "task", nil, nil, nil)
	issues.UpdateStatus(1, "open")

	err := prUpdate(issues, testCfg(), []string{"1"})
	if err == nil {
		t.Fatal("expected error for ticket in open status, got nil")
	}
}

func TestPRCreate_ClearsAssignee(t *testing.T) {
	issues := setupPRTestRepo(t)

	id, err := issues.Create("A task", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}
	if err := issues.Assign(id, "iron", "prole/iron/1"); err != nil {
		t.Fatalf("assigning: %v", err)
	}
	if err := issues.UpdateStatus(id, "in_progress"); err != nil {
		t.Fatalf("updating status: %v", err)
	}

	origPush := gitPushFn
	origGH := ghPRCreateFn
	t.Cleanup(func() {
		gitPushFn = origPush
		ghPRCreateFn = origGH
	})
	gitPushFn = func(args ...string) error { return nil }
	ghPRCreateFn = func(title, body string) (string, error) {
		return "https://github.com/x/y/pull/42", nil
	}

	if err := prCreate(issues, testCfg(), []string{"1"}); err != nil {
		t.Fatalf("prCreate: %v", err)
	}

	got, err := issues.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Assignee.Valid && got.Assignee.String != "" {
		t.Errorf("expected assignee cleared, got %q", got.Assignee.String)
	}
	if got.Status != "in_review" {
		t.Errorf("expected status=in_review, got %q", got.Status)
	}
}

func TestPRUpdate_ClearsAssignee(t *testing.T) {
	issues := setupPRTestRepo(t)

	id, err := issues.Create("A task", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}
	if err := issues.Assign(id, "iron", "prole/iron/1"); err != nil {
		t.Fatalf("assigning: %v", err)
	}
	if err := issues.UpdateStatus(id, "repairing"); err != nil {
		t.Fatalf("updating status: %v", err)
	}

	origPush := gitPushFn
	t.Cleanup(func() { gitPushFn = origPush })
	gitPushFn = func(args ...string) error { return nil }

	if err := prUpdate(issues, testCfg(), []string{"1"}); err != nil {
		t.Fatalf("prUpdate: %v", err)
	}

	got, err := issues.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Assignee.Valid && got.Assignee.String != "" {
		t.Errorf("expected assignee cleared, got %q", got.Assignee.String)
	}
	if got.Status != "in_review" {
		t.Errorf("expected status=in_review, got %q", got.Status)
	}
}

func TestPRUpdate_noBranch(t *testing.T) {
	issues := setupPRTestRepo(t)

	_, _ = issues.Create("A task", "task", nil, nil, nil)
	issues.UpdateStatus(1, "repairing")

	err := prUpdate(issues, testCfg(), []string{"1"})
	if err == nil {
		t.Fatal("expected error for repairing ticket with no branch, got nil")
	}
	if !strings.Contains(err.Error(), "no branch") {
		t.Errorf("expected error to mention 'no branch', got: %v", err)
	}
}

// captureStdout redirects os.Stdout for the duration of f() and returns what
// was written.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = orig })

	f()

	w.Close()
	var buf bytes.Buffer
	io.Copy(&buf, r)
	return buf.String()
}

// withStubPRShow swaps ghPRShowFn for the duration of the test.
func withStubPRShow(t *testing.T, data *prShowData, err error) {
	t.Helper()
	orig := ghPRShowFn
	ghPRShowFn = func(prNum int, projectRoot string) (*prShowData, error) {
		return data, err
	}
	t.Cleanup(func() { ghPRShowFn = orig })
}

func TestPRShow_happyPath(t *testing.T) {
	issues := setupPRTestRepo(t)

	id, _ := issues.Create("Add feature", "task", nil, nil, nil)
	issues.UpdateStatus(id, "in_review")
	issues.SetPR(id, 42)

	withStubPRShow(t, &prShowData{
		Number:         42,
		Title:          "[nc-1] Add feature",
		State:          "OPEN",
		HeadRefName:    "prole/tin/1",
		Mergeable:      "MERGEABLE",
		ReviewDecision: "APPROVED",
		Checks: []prCheckResult{
			{Name: "build", Status: "COMPLETED", Conclusion: "SUCCESS"},
		},
		Reviews: []prReviewEntry{
			{AuthorLogin: "alice", State: "APPROVED", SubmittedAt: "2026-04-13T10:00:00Z", Body: "LGTM"},
		},
	}, nil)

	out := captureStdout(t, func() {
		if err := prShow(issues, testCfg(), []string{fmt.Sprintf("%d", id)}); err != nil {
			t.Fatalf("prShow: %v", err)
		}
	})

	if !strings.Contains(out, "nc-1") {
		t.Errorf("expected ticket ID in output, got:\n%s", out)
	}
	if !strings.Contains(out, "PR #42") {
		t.Errorf("expected PR number in output, got:\n%s", out)
	}
	if !strings.Contains(out, "OPEN") {
		t.Errorf("expected PR state in output, got:\n%s", out)
	}
	if !strings.Contains(out, "prole/tin/1") {
		t.Errorf("expected branch in output, got:\n%s", out)
	}
	if !strings.Contains(out, "build") {
		t.Errorf("expected check name in output, got:\n%s", out)
	}
	if !strings.Contains(out, "alice") {
		t.Errorf("expected reviewer login in output, got:\n%s", out)
	}
	if !strings.Contains(out, "LGTM") {
		t.Errorf("expected review body in output, got:\n%s", out)
	}
}

func TestPRShow_noPRNumber(t *testing.T) {
	issues := setupPRTestRepo(t)

	id, _ := issues.Create("Unsubmitted task", "task", nil, nil, nil)

	err := prShow(issues, testCfg(), []string{fmt.Sprintf("%d", id)})
	if err == nil {
		t.Fatal("expected error for ticket with no PR number, got nil")
	}
	if !strings.Contains(err.Error(), "no PR number") {
		t.Errorf("expected 'no PR number' in error, got: %v", err)
	}
}

func TestPRShow_ticketNotFound(t *testing.T) {
	issues := setupPRTestRepo(t)

	err := prShow(issues, testCfg(), []string{"9999"})
	if err == nil {
		t.Fatal("expected error for non-existent ticket, got nil")
	}
}

func TestPRShow_missingArg(t *testing.T) {
	issues := setupPRTestRepo(t)

	err := prShow(issues, testCfg(), []string{})
	if err == nil {
		t.Fatal("expected usage error, got nil")
	}
}

func TestPRShow_ghError(t *testing.T) {
	issues := setupPRTestRepo(t)

	id, _ := issues.Create("Some task", "task", nil, nil, nil)
	issues.SetPR(id, 99)

	withStubPRShow(t, nil, fmt.Errorf("gh: not found"))

	err := prShow(issues, testCfg(), []string{fmt.Sprintf("%d", id)})
	if err == nil {
		t.Fatal("expected error from gh, got nil")
	}
	if !strings.Contains(err.Error(), "fetching PR") {
		t.Errorf("expected 'fetching PR' in error, got: %v", err)
	}
}

func TestPRShow_emptyReviews(t *testing.T) {
	issues := setupPRTestRepo(t)

	id, _ := issues.Create("New task", "task", nil, nil, nil)
	issues.SetPR(id, 7)

	withStubPRShow(t, &prShowData{
		Number:      7,
		Title:       "[nc-1] New task",
		State:       "OPEN",
		HeadRefName: "prole/tin/1",
		Mergeable:   "UNKNOWN",
	}, nil)

	out := captureStdout(t, func() {
		if err := prShow(issues, testCfg(), []string{fmt.Sprintf("%d", id)}); err != nil {
			t.Fatalf("prShow: %v", err)
		}
	})

	if !strings.Contains(out, "Checks (0)") {
		t.Errorf("expected 'Checks (0)' in output, got:\n%s", out)
	}
	if !strings.Contains(out, "Reviews (0)") {
		t.Errorf("expected 'Reviews (0)' in output, got:\n%s", out)
	}
}

func TestPRShow_limitsReviewsToFive(t *testing.T) {
	issues := setupPRTestRepo(t)

	id, _ := issues.Create("Busy PR", "task", nil, nil, nil)
	issues.SetPR(id, 55)

	reviews := make([]prReviewEntry, 8)
	for i := range reviews {
		reviews[i] = prReviewEntry{
			AuthorLogin: fmt.Sprintf("user%d", i+1),
			State:       "COMMENTED",
			SubmittedAt: "2026-04-13T00:00:00Z",
			Body:        fmt.Sprintf("comment %d", i+1),
		}
	}
	withStubPRShow(t, &prShowData{
		Number:      55,
		State:       "OPEN",
		HeadRefName: "prole/tin/1",
		Reviews:     reviews,
	}, nil)

	out := captureStdout(t, func() {
		if err := prShow(issues, testCfg(), []string{fmt.Sprintf("%d", id)}); err != nil {
			t.Fatalf("prShow: %v", err)
		}
	})

	// Should show "Reviews (8, showing last 5)".
	if !strings.Contains(out, "showing last 5") {
		t.Errorf("expected 'showing last 5' in output, got:\n%s", out)
	}
	// Last 5 reviews are user4–user8; user1–user3 should be absent.
	if strings.Contains(out, "user1") || strings.Contains(out, "user3") {
		t.Errorf("expected early reviews to be truncated, got:\n%s", out)
	}
	if !strings.Contains(out, "user8") {
		t.Errorf("expected last reviewer in output, got:\n%s", out)
	}
}

func TestPRShow_prefixedTicketID(t *testing.T) {
	issues := setupPRTestRepo(t)

	id, _ := issues.Create("Prefixed lookup", "task", nil, nil, nil)
	issues.SetPR(id, 33)

	withStubPRShow(t, &prShowData{Number: 33, State: "MERGED", HeadRefName: "prole/tin/1"}, nil)

	out := captureStdout(t, func() {
		if err := prShow(issues, testCfg(), []string{fmt.Sprintf("nc-%d", id)}); err != nil {
			t.Fatalf("prShow with prefixed ID: %v", err)
		}
	})

	if !strings.Contains(out, "PR #33") {
		t.Errorf("expected PR #33 in output, got:\n%s", out)
	}
}
