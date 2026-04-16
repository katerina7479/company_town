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

func setupPRTestRepo(t *testing.T) *repo.IssueRepo {
	t.Helper()
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("creating test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return repo.NewIssueRepo(conn, nil)
}

func setupAgentTestRepo(t *testing.T) (*repo.AgentRepo, func()) {
	t.Helper()
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("creating test db: %v", err)
	}
	return repo.NewAgentRepo(conn, nil), func() { conn.Close() }
}

func testCfg() *config.Config {
	return &config.Config{TicketPrefix: "nc", ProjectRoot: "/project"}
}

// stubBranchFns replaces the two git branch injection points for the duration
// of the test. Use current="HEAD" to simulate detached HEAD, or current="main"
// to simulate being on the default branch. The default branch is always "main".
func stubBranchFns(t *testing.T, current string) {
	t.Helper()
	origCurrent := gitCurrentBranchFn
	origDefault := gitDefaultBranchFn
	gitCurrentBranchFn = func(string) (string, error) { return current, nil }
	gitDefaultBranchFn = func(string) (string, error) { return "main", nil }
	t.Cleanup(func() {
		gitCurrentBranchFn = origCurrent
		gitDefaultBranchFn = origDefault
	})
}

// stubPRShowFns replaces the three gh injection points for the duration of the test.
func stubPRShowFns(t *testing.T,
	viewOut []byte, viewErr error,
	reviewsOut []byte, reviewsErr error,
	commentsOut []byte, commentsErr error,
) {
	t.Helper()
	origView, origReviews, origComments := ghPRViewFn, ghPRReviewsFn, ghPRCommentsFn
	t.Cleanup(func() {
		ghPRViewFn = origView
		ghPRReviewsFn = origReviews
		ghPRCommentsFn = origComments
	})
	ghPRViewFn = func(_ int, _ string) ([]byte, error) { return viewOut, viewErr }
	ghPRReviewsFn = func(_ int, _ string) ([]byte, error) { return reviewsOut, reviewsErr }
	ghPRCommentsFn = func(_ int, _ string) ([]byte, error) { return commentsOut, commentsErr }
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
	err := prUpdate(issues, testCfg(), "/tmp", []string{})
	if err == nil {
		t.Fatal("expected usage error for 0 args, got nil")
	}
}

func TestPRUpdate_notFound(t *testing.T) {
	issues := setupPRTestRepo(t)
	err := prUpdate(issues, testCfg(), "/tmp", []string{"9999"})
	if err == nil {
		t.Fatal("expected error for non-existent ticket, got nil")
	}
}

func TestPRUpdate_wrongStatus(t *testing.T) {
	issues := setupPRTestRepo(t)

	_, _ = issues.Create("A task", "task", nil, nil, nil)
	issues.UpdateStatus(1, "in_review")

	err := prUpdate(issues, testCfg(), "/tmp", []string{"1"})
	if err == nil {
		t.Fatal("expected error when ticket is not in repairing status, got nil")
	}
	if !errors.Is(err, ErrNotRepairingStatus) {
		t.Errorf("expected ErrNotRepairingStatus, got: %v", err)
	}
}

func TestPRUpdate_wrongStatus_open(t *testing.T) {
	issues := setupPRTestRepo(t)

	_, _ = issues.Create("A task", "task", nil, nil, nil)
	issues.UpdateStatus(1, "open")

	err := prUpdate(issues, testCfg(), "/tmp", []string{"1"})
	if err == nil {
		t.Fatal("expected error for ticket in open status, got nil")
	}
}

func TestPRCreate_SetsCIRunning(t *testing.T) {
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

	origCount := gitCommitCountFn
	origPush := gitPushFn
	origGH := ghPRCreateFn
	t.Cleanup(func() {
		gitCommitCountFn = origCount
		gitPushFn = origPush
		ghPRCreateFn = origGH
	})
	gitCommitCountFn = func(_ string) (int, error) { return 1, nil }
	gitPushFn = func(_ string, args ...string) error { return nil }
	ghPRCreateFn = func(title, body string, draft bool) (string, error) {
		return "https://github.com/x/y/pull/42", nil
	}
	stubBranchFns(t, "prole/iron/1")

	if err := prCreate(issues, testCfg(), "/tmp", []string{"1"}); err != nil {
		t.Fatalf("prCreate: %v", err)
	}

	got, err := issues.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// Prole stays assigned — they are still responsible if CI fails.
	if !got.Assignee.Valid || got.Assignee.String != "iron" {
		t.Errorf("expected assignee=iron (kept through ci_running), got %q", got.Assignee.String)
	}
	if got.Status != "ci_running" {
		t.Errorf("expected status=ci_running, got %q", got.Status)
	}
}

func TestPRUpdate_SetsCIRunning(t *testing.T) {
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
	gitPushFn = func(_ string, args ...string) error { return nil }
	stubBranchFns(t, "prole/iron/1")

	if err := prUpdate(issues, testCfg(), "/tmp", []string{"1"}); err != nil {
		t.Fatalf("prUpdate: %v", err)
	}

	got, err := issues.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// Prole stays assigned — they are still responsible if CI fails again.
	if !got.Assignee.Valid || got.Assignee.String != "iron" {
		t.Errorf("expected assignee=iron (kept through ci_running), got %q", got.Assignee.String)
	}
	if got.Status != "ci_running" {
		t.Errorf("expected status=ci_running, got %q", got.Status)
	}
}

func TestPRUpdate_noBranch(t *testing.T) {
	issues := setupPRTestRepo(t)

	_, _ = issues.Create("A task", "task", nil, nil, nil)
	issues.UpdateStatus(1, "repairing")

	err := prUpdate(issues, testCfg(), "/tmp", []string{"1"})
	if err == nil {
		t.Fatal("expected error for repairing ticket with no branch, got nil")
	}
	if !errors.Is(err, ErrNoBranchSet) {
		t.Errorf("expected ErrNoBranchSet, got: %v", err)
	}
}

// --- gt pr show tests ---

func TestPRShow_missingArg(t *testing.T) {
	issues := setupPRTestRepo(t)
	err := prShow(issues, testCfg(), []string{})
	if err == nil {
		t.Fatal("expected error for missing arg, got nil")
	}
}

func TestPRShow_ticketNotFound(t *testing.T) {
	issues := setupPRTestRepo(t)
	err := prShow(issues, testCfg(), []string{"9999"})
	if err == nil {
		t.Fatal("expected error for non-existent ticket, got nil")
	}
}

func TestPRShow_noPR(t *testing.T) {
	issues := setupPRTestRepo(t)
	var viewCalled bool
	origView := ghPRViewFn
	t.Cleanup(func() { ghPRViewFn = origView })
	ghPRViewFn = func(_ int, _ string) ([]byte, error) {
		viewCalled = true
		return nil, nil
	}

	_, _ = issues.Create("A task", "task", nil, nil, nil)
	err := prShow(issues, testCfg(), []string{"1"})
	if err == nil {
		t.Fatal("expected error for ticket with no PR number, got nil")
	}
	if viewCalled {
		t.Error("ghPRViewFn should not be called when ticket has no PR number")
	}
}

func TestPRShow_prefixedTicketID(t *testing.T) {
	issues := setupPRTestRepo(t)
	id, _ := issues.Create("My task", "task", nil, nil, nil)
	_ = issues.SetPR(id, 42)

	metaJSON := `{"number":42,"title":"[nc-1] My task","state":"OPEN","headRefName":"prole/tin/1","mergeable":"MERGEABLE","reviewDecision":"","statusCheckRollup":[]}`
	reviewsJSON := `{"reviews":[]}`
	commentsJSON := `{"comments":[]}`
	stubPRShowFns(t, []byte(metaJSON), nil, []byte(reviewsJSON), nil, []byte(commentsJSON), nil)

	outStr, _ := captureLogOutput(func() {
		err := prShow(issues, testCfg(), []string{"nc-1"})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(outStr, "PR #42") {
		t.Errorf("expected output to contain 'PR #42', got: %s", outStr)
	}
}

func TestPRShow_softFailOnReviews(t *testing.T) {
	issues := setupPRTestRepo(t)
	id, _ := issues.Create("My task", "task", nil, nil, nil)
	_ = issues.SetPR(id, 42)

	metaJSON := `{"number":42,"title":"[nc-1] My task","state":"OPEN","headRefName":"prole/tin/1","mergeable":"MERGEABLE","reviewDecision":"","statusCheckRollup":[]}`
	commentsJSON := `{"comments":[{"author":{"login":"ceo"},"body":"looks good to me","createdAt":"2024-01-01T01:00:00Z"}]}`
	stubPRShowFns(t, []byte(metaJSON), nil, nil, fmt.Errorf("permission denied"), []byte(commentsJSON), nil)

	var outStr, errStr string
	outStr, errStr = captureLogOutput(func() {
		err := prShow(issues, testCfg(), []string{"1"})
		if err != nil {
			t.Errorf("expected nil (soft-fail), got %v", err)
		}
	})
	if !strings.Contains(outStr, "looks good to me") {
		t.Errorf("expected output to contain comment body, got: %s", outStr)
	}
	if !strings.Contains(errStr, "reviews") {
		t.Errorf("expected stderr to mention 'reviews', got: %s", errStr)
	}
}

func TestPRShow_softFailOnComments(t *testing.T) {
	issues := setupPRTestRepo(t)
	id, _ := issues.Create("My task", "task", nil, nil, nil)
	_ = issues.SetPR(id, 42)

	metaJSON := `{"number":42,"title":"[nc-1] My task","state":"OPEN","headRefName":"prole/tin/1","mergeable":"MERGEABLE","reviewDecision":"APPROVED","statusCheckRollup":[]}`
	reviewsJSON := `{"reviews":[{"author":{"login":"reviewer"},"state":"APPROVED","submittedAt":"2024-01-01T00:00:00Z","body":"LGTM"}]}`
	stubPRShowFns(t, []byte(metaJSON), nil, []byte(reviewsJSON), nil, nil, fmt.Errorf("not found"))

	var outStr, errStr string
	outStr, errStr = captureLogOutput(func() {
		err := prShow(issues, testCfg(), []string{"1"})
		if err != nil {
			t.Errorf("expected nil (soft-fail), got %v", err)
		}
	})
	if !strings.Contains(outStr, "LGTM") {
		t.Errorf("expected output to contain review body, got: %s", outStr)
	}
	if !strings.Contains(errStr, "comments") {
		t.Errorf("expected stderr to mention 'comments', got: %s", errStr)
	}
}

func TestPRShow_activityLimit(t *testing.T) {
	issues := setupPRTestRepo(t)
	id, _ := issues.Create("My task", "task", nil, nil, nil)
	_ = issues.SetPR(id, 42)

	metaJSON := `{"number":42,"title":"[nc-1] My task","state":"OPEN","headRefName":"prole/tin/1","mergeable":"MERGEABLE","reviewDecision":"","statusCheckRollup":[]}`
	reviewsJSON := `{"reviews":[]}`
	// 7 comments — only last 5 should appear
	commentsJSON := `{"comments":[
		{"author":{"login":"u1"},"body":"comment 1","createdAt":"2024-01-01T00:00:00Z"},
		{"author":{"login":"u2"},"body":"comment 2","createdAt":"2024-01-01T01:00:00Z"},
		{"author":{"login":"u3"},"body":"comment 3","createdAt":"2024-01-01T02:00:00Z"},
		{"author":{"login":"u4"},"body":"comment 4","createdAt":"2024-01-01T03:00:00Z"},
		{"author":{"login":"u5"},"body":"comment 5","createdAt":"2024-01-01T04:00:00Z"},
		{"author":{"login":"u6"},"body":"comment 6","createdAt":"2024-01-01T05:00:00Z"},
		{"author":{"login":"u7"},"body":"comment 7","createdAt":"2024-01-01T06:00:00Z"}
	]}`
	stubPRShowFns(t, []byte(metaJSON), nil, []byte(reviewsJSON), nil, []byte(commentsJSON), nil)

	outStr, _ := captureLogOutput(func() {
		err := prShow(issues, testCfg(), []string{"1"})
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	if strings.Contains(outStr, "comment 1") || strings.Contains(outStr, "comment 2") {
		t.Errorf("expected old comments trimmed, but found them in output: %s", outStr)
	}
	if !strings.Contains(outStr, "comment 7") {
		t.Errorf("expected most recent comment in output, got: %s", outStr)
	}
}

func TestPRCreate_ErrorsOnEmptyBranch(t *testing.T) {
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

	origCount := gitCommitCountFn
	t.Cleanup(func() { gitCommitCountFn = origCount })
	gitCommitCountFn = func(_ string) (int, error) { return 0, nil }

	pushCalled := false
	origPush := gitPushFn
	t.Cleanup(func() { gitPushFn = origPush })
	gitPushFn = func(_ string, args ...string) error { pushCalled = true; return nil }

	err = prCreate(issues, testCfg(), "/tmp", []string{"1"})
	if err == nil {
		t.Fatal("expected error for empty branch, got nil")
	}
	if !errors.Is(err, ErrNoCommitsYet) {
		t.Errorf("expected ErrNoCommitsYet, got: %v", err)
	}
	if pushCalled {
		t.Error("gitPushFn should not be called when branch has no commits")
	}
	got, _ := issues.Get(id)
	if got.Status != "in_progress" {
		t.Errorf("expected status unchanged (in_progress), got %q", got.Status)
	}
}

func TestPRCreate_PushProceedsWhenCommitsExist(t *testing.T) {
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

	origCount := gitCommitCountFn
	origPush := gitPushFn
	origGH := ghPRCreateFn
	t.Cleanup(func() {
		gitCommitCountFn = origCount
		gitPushFn = origPush
		ghPRCreateFn = origGH
	})
	gitCommitCountFn = func(_ string) (int, error) { return 3, nil }
	pushCalled := false
	gitPushFn = func(_ string, args ...string) error { pushCalled = true; return nil }
	ghPRCreateFn = func(title, body string, draft bool) (string, error) {
		return "https://github.com/x/y/pull/99", nil
	}
	stubBranchFns(t, "prole/iron/1")

	if err := prCreate(issues, testCfg(), "/tmp", []string{"1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !pushCalled {
		t.Error("expected gitPushFn to be called when commits exist")
	}
	got, _ := issues.Get(id)
	if got.Status != "ci_running" {
		t.Errorf("expected status=ci_running, got %q", got.Status)
	}
}

// --- resolveGitWorkDir tests ---

func TestResolveGitWorkDir_NoAgentName_UsesProjectRoot(t *testing.T) {
	agents, cleanup := setupAgentTestRepo(t)
	defer cleanup()

	t.Setenv("CT_AGENT_NAME", "")

	cfg := &config.Config{ProjectRoot: "/main/checkout"}
	got := resolveGitWorkDir(cfg, agents)
	if got != "/main/checkout" {
		t.Errorf("expected project root %q, got %q", "/main/checkout", got)
	}
}

func TestResolveGitWorkDir_ProleWithWorktree_UsesWorktree(t *testing.T) {
	agents, cleanup := setupAgentTestRepo(t)
	defer cleanup()

	t.Setenv("CT_AGENT_NAME", "tin")
	if err := agents.Register("tin", "prole", nil); err != nil {
		t.Fatalf("registering agent: %v", err)
	}
	if err := agents.SetWorktree("tin", "/project/.company_town/proles/tin"); err != nil {
		t.Fatalf("setting worktree: %v", err)
	}

	cfg := &config.Config{ProjectRoot: "/project"}
	got := resolveGitWorkDir(cfg, agents)
	if got != "/project/.company_town/proles/tin" {
		t.Errorf("expected worktree path, got %q", got)
	}
}

func TestResolveGitWorkDir_UnknownAgent_FallsBackToProjectRoot(t *testing.T) {
	agents, cleanup := setupAgentTestRepo(t)
	defer cleanup()

	t.Setenv("CT_AGENT_NAME", "ghost")

	cfg := &config.Config{ProjectRoot: "/project"}
	got := resolveGitWorkDir(cfg, agents)
	if got != "/project" {
		t.Errorf("expected project root fallback, got %q", got)
	}
}

func TestResolveGitWorkDir_ProleNoWorktreePath_FallsBackToProjectRoot(t *testing.T) {
	agents, cleanup := setupAgentTestRepo(t)
	defer cleanup()

	t.Setenv("CT_AGENT_NAME", "copper")
	if err := agents.Register("copper", "prole", nil); err != nil {
		t.Fatalf("registering agent: %v", err)
	}
	// worktree path deliberately not set

	cfg := &config.Config{ProjectRoot: "/project"}
	got := resolveGitWorkDir(cfg, agents)
	if got != "/project" {
		t.Errorf("expected project root fallback when no worktree path set, got %q", got)
	}
}

// gitPushFn and gitCommitCountFn must receive the workDir from resolveGitWorkDir.
func TestPRCreate_WorkDirPassedToGitFns(t *testing.T) {
	issues := setupPRTestRepo(t)
	id, err := issues.Create("task", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}
	if err := issues.Assign(id, "tin", "prole/tin/1"); err != nil {
		t.Fatalf("assigning: %v", err)
	}
	if err := issues.UpdateStatus(id, "in_progress"); err != nil {
		t.Fatalf("updating status: %v", err)
	}

	const wantDir = "/proles/tin/worktree"

	origCount := gitCommitCountFn
	origPush := gitPushFn
	origGH := ghPRCreateFn
	t.Cleanup(func() {
		gitCommitCountFn = origCount
		gitPushFn = origPush
		ghPRCreateFn = origGH
	})

	var gotCountDir, gotPushDir string
	gitCommitCountFn = func(workDir string) (int, error) {
		gotCountDir = workDir
		return 1, nil
	}
	gitPushFn = func(workDir string, args ...string) error {
		gotPushDir = workDir
		return nil
	}
	ghPRCreateFn = func(title, body string, draft bool) (string, error) {
		return "https://github.com/x/y/pull/7", nil
	}
	stubBranchFns(t, "prole/tin/1")

	if err := prCreate(issues, testCfg(), wantDir, []string{"1"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotCountDir != wantDir {
		t.Errorf("gitCommitCountFn received workDir=%q, want %q", gotCountDir, wantDir)
	}
	if gotPushDir != wantDir {
		t.Errorf("gitPushFn received workDir=%q, want %q", gotPushDir, wantDir)
	}
}

// --- assertBranchReadyForPR / pre-flight check tests ---

// setupBranchTestIssue creates a ticket with a branch, stubs commit count to
// non-zero, and returns the string ticket ID. Callers must still stub branch
// and gh/push fns as needed.
func setupBranchTestIssue(t *testing.T, issues *repo.IssueRepo, branch string) string {
	t.Helper()
	id, err := issues.Create("Branch test task", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}
	if err := issues.Assign(id, "copper", branch); err != nil {
		t.Fatalf("assigning: %v", err)
	}
	if err := issues.UpdateStatus(id, "in_progress"); err != nil {
		t.Fatalf("updating status: %v", err)
	}
	origCount := gitCommitCountFn
	t.Cleanup(func() { gitCommitCountFn = origCount })
	gitCommitCountFn = func(_ string) (int, error) { return 1, nil }
	return fmt.Sprintf("%d", id)
}

func TestPRCreate_refusesDetachedHEAD(t *testing.T) {
	issues := setupPRTestRepo(t)
	ticketID := setupBranchTestIssue(t, issues, "prole/copper/nc-1")

	stubBranchFns(t, "HEAD")

	pushCalled := false
	ghCalled := false
	origPush, origGH := gitPushFn, ghPRCreateFn
	t.Cleanup(func() { gitPushFn = origPush; ghPRCreateFn = origGH })
	gitPushFn = func(_ string, _ ...string) error { pushCalled = true; return nil }
	ghPRCreateFn = func(_, _ string, _ bool) (string, error) { ghCalled = true; return "", nil }

	err := prCreate(issues, testCfg(), "/tmp", []string{ticketID})
	if err == nil {
		t.Fatal("expected error for detached HEAD, got nil")
	}
	if !errors.Is(err, ErrHeadDetached) {
		t.Errorf("expected ErrHeadDetached, got: %v", err)
	}
	if pushCalled {
		t.Error("gitPushFn must not be called when HEAD is detached")
	}
	if ghCalled {
		t.Error("ghPRCreateFn must not be called when HEAD is detached")
	}
}

func TestPRCreate_refusesOnDefaultBranch(t *testing.T) {
	issues := setupPRTestRepo(t)
	ticketID := setupBranchTestIssue(t, issues, "prole/copper/nc-2")

	stubBranchFns(t, "main")

	pushCalled := false
	origPush := gitPushFn
	t.Cleanup(func() { gitPushFn = origPush })
	gitPushFn = func(_ string, _ ...string) error { pushCalled = true; return nil }

	err := prCreate(issues, testCfg(), "/tmp", []string{ticketID})
	if err == nil {
		t.Fatal("expected error when on default branch, got nil")
	}
	if !errors.Is(err, ErrDefaultBranch) {
		t.Errorf("expected ErrDefaultBranch, got: %v", err)
	}
	if pushCalled {
		t.Error("gitPushFn must not be called when on the default branch")
	}
}

func TestPRCreate_refusesBranchMismatch(t *testing.T) {
	issues := setupPRTestRepo(t)
	ticketID := setupBranchTestIssue(t, issues, "prole/copper/right")

	stubBranchFns(t, "prole/copper/wrong")

	pushCalled := false
	origPush := gitPushFn
	t.Cleanup(func() { gitPushFn = origPush })
	gitPushFn = func(_ string, _ ...string) error { pushCalled = true; return nil }

	err := prCreate(issues, testCfg(), "/tmp", []string{ticketID})
	if err == nil {
		t.Fatal("expected error for branch mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "prole/copper/wrong") || !strings.Contains(err.Error(), "prole/copper/right") {
		t.Errorf("expected both branch names in error, got: %v", err)
	}
	if pushCalled {
		t.Error("gitPushFn must not be called on branch mismatch")
	}
}

func TestPRCreate_allowsMatchingBranch(t *testing.T) {
	issues := setupPRTestRepo(t)
	ticketID := setupBranchTestIssue(t, issues, "prole/copper/nc-1")

	stubBranchFns(t, "prole/copper/nc-1")

	ghCalled := false
	origPush, origGH := gitPushFn, ghPRCreateFn
	t.Cleanup(func() { gitPushFn = origPush; ghPRCreateFn = origGH })
	gitPushFn = func(_ string, _ ...string) error { return nil }
	ghPRCreateFn = func(_, _ string, _ bool) (string, error) {
		ghCalled = true
		return "https://github.com/x/y/pull/10", nil
	}

	if err := prCreate(issues, testCfg(), "/tmp", []string{ticketID}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !ghCalled {
		t.Error("expected ghPRCreateFn to be reached on matching branch")
	}
}

func TestPRCreate_allowsEmptyTicketBranch(t *testing.T) {
	issues := setupPRTestRepo(t)

	// Create a ticket without a branch set via Assign — just set branch directly
	// by assigning then using a ticket where branch is empty (no Assign call).
	id, _ := issues.Create("No branch ticket", "task", nil, nil, nil)
	issues.UpdateStatus(id, "in_progress")

	origCount := gitCommitCountFn
	t.Cleanup(func() { gitCommitCountFn = origCount })
	gitCommitCountFn = func(_ string) (int, error) { return 1, nil }

	// Verify that the mismatch check is skipped when ticketBranch is empty.
	stubBranchFns(t, "feature/x")
	err := assertBranchReadyForPR("/tmp", "")
	if err != nil {
		t.Errorf("expected no error when ticketBranch is empty and branch is not default/detached, got: %v", err)
	}
}

func TestPRUpdate_refusesDetachedHEAD(t *testing.T) {
	issues := setupPRTestRepo(t)
	id, _ := issues.Create("Repair task", "task", nil, nil, nil)
	issues.Assign(id, "copper", "prole/copper/nc-5")
	issues.UpdateStatus(id, "repairing")

	stubBranchFns(t, "HEAD")

	pushCalled := false
	origPush := gitPushFn
	t.Cleanup(func() { gitPushFn = origPush })
	gitPushFn = func(_ string, _ ...string) error { pushCalled = true; return nil }

	err := prUpdate(issues, testCfg(), "/tmp", []string{fmt.Sprintf("%d", id)})
	if err == nil {
		t.Fatal("expected error for detached HEAD in prUpdate, got nil")
	}
	if !errors.Is(err, ErrHeadDetached) {
		t.Errorf("expected ErrHeadDetached, got: %v", err)
	}
	if pushCalled {
		t.Error("gitPushFn must not be called when HEAD is detached")
	}
}

// --- additional prCreate coverage ---

func TestPRCreate_missingArgs(t *testing.T) {
	issues := setupPRTestRepo(t)
	err := prCreate(issues, testCfg(), "/tmp", []string{})
	if err == nil {
		t.Fatal("expected usage error for 0 args, got nil")
	}
}

func TestPRCreate_noBranchSet(t *testing.T) {
	issues := setupPRTestRepo(t)
	// Create a ticket without calling Assign (so Branch.Valid == false).
	id, _ := issues.Create("No branch task", "task", nil, nil, nil)
	issues.UpdateStatus(id, "in_progress")

	err := prCreate(issues, testCfg(), "/tmp", []string{fmt.Sprintf("%d", id)})
	if err == nil {
		t.Fatal("expected ErrNoBranchSet, got nil")
	}
	if !errors.Is(err, ErrNoBranchSet) {
		t.Errorf("expected ErrNoBranchSet, got: %v", err)
	}
}

func TestPRCreate_commitCountError(t *testing.T) {
	issues := setupPRTestRepo(t)
	id, _ := issues.Create("Task", "task", nil, nil, nil)
	issues.Assign(id, "iron", "prole/iron/1")
	issues.UpdateStatus(id, "in_progress")

	origCount := gitCommitCountFn
	t.Cleanup(func() { gitCommitCountFn = origCount })
	gitCommitCountFn = func(_ string) (int, error) {
		return 0, fmt.Errorf("git error: not a repo")
	}

	err := prCreate(issues, testCfg(), "/tmp", []string{fmt.Sprintf("%d", id)})
	if err == nil {
		t.Fatal("expected error from gitCommitCountFn, got nil")
	}
	if !strings.Contains(err.Error(), "counting commits") {
		t.Errorf("expected 'counting commits' in error, got: %v", err)
	}
}

func TestPRCreate_pushError(t *testing.T) {
	issues := setupPRTestRepo(t)
	id, _ := issues.Create("Task", "task", nil, nil, nil)
	issues.Assign(id, "iron", "prole/iron/1")
	issues.UpdateStatus(id, "in_progress")

	origCount := gitCommitCountFn
	origPush := gitPushFn
	t.Cleanup(func() {
		gitCommitCountFn = origCount
		gitPushFn = origPush
	})
	gitCommitCountFn = func(_ string) (int, error) { return 3, nil }
	gitPushFn = func(_ string, _ ...string) error {
		return fmt.Errorf("simulated push failure")
	}
	stubBranchFns(t, "prole/iron/1")

	err := prCreate(issues, testCfg(), "/tmp", []string{fmt.Sprintf("%d", id)})
	if err == nil {
		t.Fatal("expected error from gitPushFn, got nil")
	}
	if !strings.Contains(err.Error(), "pushing branch") {
		t.Errorf("expected 'pushing branch' in error, got: %v", err)
	}
}

func TestPRCreate_ghCreateError(t *testing.T) {
	issues := setupPRTestRepo(t)
	id, _ := issues.Create("Task", "task", nil, nil, nil)
	issues.Assign(id, "iron", "prole/iron/1")
	issues.UpdateStatus(id, "in_progress")

	origCount := gitCommitCountFn
	origPush := gitPushFn
	origGH := ghPRCreateFn
	t.Cleanup(func() {
		gitCommitCountFn = origCount
		gitPushFn = origPush
		ghPRCreateFn = origGH
	})
	gitCommitCountFn = func(_ string) (int, error) { return 2, nil }
	gitPushFn = func(_ string, _ ...string) error { return nil }
	ghPRCreateFn = func(_, _ string, _ bool) (string, error) {
		return "", fmt.Errorf("gh: rate limited")
	}
	stubBranchFns(t, "prole/iron/1")

	err := prCreate(issues, testCfg(), "/tmp", []string{fmt.Sprintf("%d", id)})
	if err == nil {
		t.Fatal("expected error from ghPRCreateFn, got nil")
	}
	if !strings.Contains(err.Error(), "creating PR") {
		t.Errorf("expected 'creating PR' in error, got: %v", err)
	}
}

// TestPRCreate_DraftFlag_TDDTests verifies that a tdd_tests ticket filed with
// --draft goes directly to pr_open (skipping ci_running).
func TestPRCreate_DraftFlag_TDDTests(t *testing.T) {
	issues := setupPRTestRepo(t)
	id, err := issues.Create("Failing auth tests", "tdd_tests", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create tdd_tests ticket: %v", err)
	}
	branch := fmt.Sprintf("prole/qa/%d", id)
	if err := issues.Assign(id, "qa", branch); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	origCount := gitCommitCountFn
	origPush := gitPushFn
	origGH := ghPRCreateFn
	t.Cleanup(func() {
		gitCommitCountFn = origCount
		gitPushFn = origPush
		ghPRCreateFn = origGH
	})
	gitCommitCountFn = func(_ string) (int, error) { return 1, nil }
	gitPushFn = func(_ string, _ ...string) error { return nil }

	var gotDraft bool
	ghPRCreateFn = func(_, _ string, draft bool) (string, error) {
		gotDraft = draft
		return "https://github.com/x/y/pull/55", nil
	}
	stubBranchFns(t, branch)

	if err := prCreate(issues, testCfg(), "/tmp", []string{fmt.Sprintf("%d", id), "--draft"}); err != nil {
		t.Fatalf("prCreate with --draft on tdd_tests: %v", err)
	}
	if !gotDraft {
		t.Error("expected ghPRCreateFn to receive draft=true")
	}
	got, _ := issues.Get(id)
	if got.Status != "pr_open" {
		t.Errorf("expected status=pr_open for tdd_tests draft, got %q", got.Status)
	}
}

// TestPRCreate_DraftFlag_RegularTask verifies that a regular task filed with
// --draft still transitions to ci_running (not pr_open).
func TestPRCreate_DraftFlag_RegularTask(t *testing.T) {
	issues := setupPRTestRepo(t)
	id, err := issues.Create("Add config flag", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create task ticket: %v", err)
	}
	branch := fmt.Sprintf("prole/copper/%d", id)
	if err := issues.Assign(id, "copper", branch); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	origCount := gitCommitCountFn
	origPush := gitPushFn
	origGH := ghPRCreateFn
	t.Cleanup(func() {
		gitCommitCountFn = origCount
		gitPushFn = origPush
		ghPRCreateFn = origGH
	})
	gitCommitCountFn = func(_ string) (int, error) { return 1, nil }
	gitPushFn = func(_ string, _ ...string) error { return nil }
	ghPRCreateFn = func(_, _ string, _ bool) (string, error) {
		return "https://github.com/x/y/pull/56", nil
	}
	stubBranchFns(t, branch)

	if err := prCreate(issues, testCfg(), "/tmp", []string{fmt.Sprintf("%d", id), "--draft"}); err != nil {
		t.Fatalf("prCreate with --draft on task: %v", err)
	}
	got, _ := issues.Get(id)
	if got.Status != "ci_running" {
		t.Errorf("expected status=ci_running for task draft, got %q", got.Status)
	}
}

// TestPRShow_withChecks verifies that prShow renders the checks table including
// the pass/fail/running summary line when statusCheckRollup is non-empty.
func TestPRShow_withChecks(t *testing.T) {
	issues := setupPRTestRepo(t)
	id, _ := issues.Create("My task", "task", nil, nil, nil)
	_ = issues.SetPR(id, 42)

	metaJSON := `{
		"number":42,"title":"[nc-1] My task","state":"OPEN",
		"headRefName":"prole/iron/1","mergeable":"MERGEABLE",
		"reviewDecision":"APPROVED",
		"statusCheckRollup":[
			{"name":"ci/test","status":"COMPLETED","conclusion":"SUCCESS"},
			{"name":"ci/lint","status":"COMPLETED","conclusion":"FAILURE"},
			{"name":"ci/build","status":"IN_PROGRESS","conclusion":""}
		]
	}`
	reviewsJSON := `{"reviews":[]}`
	commentsJSON := `{"comments":[]}`
	stubPRShowFns(t, []byte(metaJSON), nil, []byte(reviewsJSON), nil, []byte(commentsJSON), nil)

	outStr, _ := captureLogOutput(func() {
		if err := prShow(issues, testCfg(), []string{"1"}); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
	if !strings.Contains(outStr, "summary: 1 pass / 1 fail / 1 running") {
		t.Errorf("expected check summary in output, got: %s", outStr)
	}
}

// TestPRShow_fetchError verifies that prShow returns an error when ghPRViewFn fails.
func TestPRShow_fetchError(t *testing.T) {
	issues := setupPRTestRepo(t)
	id, _ := issues.Create("My task", "task", nil, nil, nil)
	_ = issues.SetPR(id, 42)

	stubPRShowFns(t, nil, fmt.Errorf("gh api error"), nil, nil, nil, nil)

	err := prShow(issues, testCfg(), []string{"1"})
	if err == nil {
		t.Fatal("expected error when gh pr view fails, got nil")
	}
	if !strings.Contains(err.Error(), "fetching PR") {
		t.Errorf("expected 'fetching PR' in error, got: %v", err)
	}
}

// TestPRShow_invalidMetaJSON verifies that prShow returns an error when the
// gh pr view response is not valid JSON.
func TestPRShow_invalidMetaJSON(t *testing.T) {
	issues := setupPRTestRepo(t)
	id, _ := issues.Create("My task", "task", nil, nil, nil)
	_ = issues.SetPR(id, 42)

	stubPRShowFns(t, []byte("not-valid-json"), nil, nil, nil, nil, nil)

	err := prShow(issues, testCfg(), []string{"1"})
	if err == nil {
		t.Fatal("expected error for invalid meta JSON, got nil")
	}
}

// TestPRUpdate_badTicketID covers the parseTicketID error path in prUpdate.
func TestPRUpdate_badTicketID(t *testing.T) {
	issues := setupPRTestRepo(t)
	err := prUpdate(issues, testCfg(), "/tmp", []string{"not-a-number"})
	if err == nil {
		t.Fatal("expected error for non-numeric ticket ID in prUpdate")
	}
}
