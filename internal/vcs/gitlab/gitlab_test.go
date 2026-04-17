package gitlab

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// loadTestdata reads a file from the testdata directory.
func loadTestdata(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("loadTestdata(%q): %v", name, err)
	}
	return data
}

func newTestProvider(runCmd func(string, string, ...string) ([]byte, error)) *Provider {
	return &Provider{project: "kate/myproj", runCmd: runCmd}
}

// contains reports whether s is in slice.
func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// singleStub returns a runCmd that always returns the given data, recording the last call's args.
func singleStub(data []byte, captured *[]string) func(string, string, ...string) ([]byte, error) {
	return func(dir, name string, args ...string) ([]byte, error) {
		if captured != nil {
			*captured = append([]string{}, args...)
		}
		return data, nil
	}
}

// seqStub returns responses in order (cycling if more calls than responses).
func seqStub(responses [][]byte) func(string, string, ...string) ([]byte, error) {
	var n int
	return func(dir, name string, args ...string) ([]byte, error) {
		r := responses[n%len(responses)]
		n++
		return r, nil
	}
}

// --- CreatePR ---

func TestCreatePR_draft(t *testing.T) {
	golden := loadTestdata(t, "mr_create_response.json")
	var captured []string
	p := newTestProvider(singleStub(golden, &captured))

	url, err := p.CreatePR("Test MR", "body text", true, "/repo")
	if err != nil {
		t.Fatalf("CreatePR: %v", err)
	}
	if url != "https://gitlab.com/kate/myproj/-/merge_requests/43" {
		t.Errorf("URL = %q, want MR URL", url)
	}
	if !contains(captured, "--draft") {
		t.Errorf("expected --draft in args: %v", captured)
	}
	if !contains(captured, "-R") || !contains(captured, "kate/myproj") {
		t.Errorf("expected -R kate/myproj in args: %v", captured)
	}
	if !contains(captured, "--output") || !contains(captured, "json") {
		t.Errorf("expected --output json in args: %v", captured)
	}
}

func TestCreatePR_notDraft(t *testing.T) {
	golden := loadTestdata(t, "mr_create_response.json")
	var captured []string
	p := newTestProvider(singleStub(golden, &captured))

	_, err := p.CreatePR("Test", "body", false, "/repo")
	if err != nil {
		t.Fatalf("CreatePR: %v", err)
	}
	if contains(captured, "--draft") {
		t.Errorf("did not expect --draft in args: %v", captured)
	}
}

// --- GetPRMetadata ---

func TestGetPRMetadata_openPR(t *testing.T) {
	golden := loadTestdata(t, "mr_view_open.json")
	var captured []string
	p := newTestProvider(singleStub(golden, &captured))

	out, err := p.GetPRMetadata(42, "/repo")
	if err != nil {
		t.Fatalf("GetPRMetadata: %v", err)
	}
	// Verify glab args include view, MR number, -R, and --output json
	for _, want := range []string{"view", "42", "-R", "kate/myproj", "--output", "json"} {
		if !contains(captured, want) {
			t.Errorf("expected %q in args: %v", want, captured)
		}
	}

	var meta struct {
		Number            int    `json:"number"`
		Title             string `json:"title"`
		State             string `json:"state"`
		HeadRefName       string `json:"headRefName"`
		Mergeable         string `json:"mergeable"`
		ReviewDecision    string `json:"reviewDecision"`
		StatusCheckRollup []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal(out, &meta); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if meta.Number != 42 {
		t.Errorf("number = %d, want 42", meta.Number)
	}
	if meta.State != "OPEN" {
		t.Errorf("state = %q, want OPEN", meta.State)
	}
	if meta.HeadRefName != "prole/copper/nc-42" {
		t.Errorf("headRefName = %q, want prole/copper/nc-42", meta.HeadRefName)
	}
	if meta.Mergeable != "MERGEABLE" {
		t.Errorf("mergeable = %q, want MERGEABLE", meta.Mergeable)
	}
	if len(meta.StatusCheckRollup) != 1 {
		t.Fatalf("statusCheckRollup len = %d, want 1", len(meta.StatusCheckRollup))
	}
	if meta.StatusCheckRollup[0].Status != "COMPLETED" || meta.StatusCheckRollup[0].Conclusion != "SUCCESS" {
		t.Errorf("pipeline: status=%q conclusion=%q, want COMPLETED/SUCCESS",
			meta.StatusCheckRollup[0].Status, meta.StatusCheckRollup[0].Conclusion)
	}
}

func TestGetPRMetadata_noPipeline(t *testing.T) {
	golden := loadTestdata(t, "mr_view_no_pipeline.json")
	p := newTestProvider(singleStub(golden, nil))

	out, err := p.GetPRMetadata(42, "/repo")
	if err != nil {
		t.Fatalf("GetPRMetadata: %v", err)
	}
	var meta struct {
		StatusCheckRollup []interface{} `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal(out, &meta); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(meta.StatusCheckRollup) != 0 {
		t.Errorf("expected empty statusCheckRollup for no pipeline, got %v", meta.StatusCheckRollup)
	}
}

// --- GetPRStateJSON ---

func TestGetPRStateJSON_merged(t *testing.T) {
	golden := loadTestdata(t, "mr_view_merged.json")
	p := newTestProvider(singleStub(golden, nil))

	out, err := p.GetPRStateJSON(42, "/repo")
	if err != nil {
		t.Fatalf("GetPRStateJSON: %v", err)
	}
	var state struct {
		State    string  `json:"state"`
		MergedAt *string `json:"mergedAt"`
	}
	if err := json.Unmarshal(out, &state); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if state.State != "MERGED" {
		t.Errorf("state = %q, want MERGED", state.State)
	}
	if state.MergedAt == nil {
		t.Error("mergedAt should not be nil for merged MR")
	}
}

func TestGetPRStateJSON_conflicting(t *testing.T) {
	golden := loadTestdata(t, "mr_view_conflicting.json")
	p := newTestProvider(singleStub(golden, nil))

	out, err := p.GetPRStateJSON(42, "/repo")
	if err != nil {
		t.Fatalf("GetPRStateJSON: %v", err)
	}
	var state struct {
		Mergeable         string `json:"mergeable"`
		StatusCheckRollup []struct {
			Conclusion string `json:"conclusion"`
		} `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal(out, &state); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if state.Mergeable != "CONFLICTING" {
		t.Errorf("mergeable = %q, want CONFLICTING", state.Mergeable)
	}
	if len(state.StatusCheckRollup) == 0 || state.StatusCheckRollup[0].Conclusion != "FAILURE" {
		t.Errorf("expected FAILURE pipeline conclusion, got %+v", state.StatusCheckRollup)
	}
}

func TestGetPRStateJSON_noPipeline(t *testing.T) {
	golden := loadTestdata(t, "mr_view_no_pipeline.json")
	p := newTestProvider(singleStub(golden, nil))

	out, err := p.GetPRStateJSON(42, "/repo")
	if err != nil {
		t.Fatalf("GetPRStateJSON: %v", err)
	}
	var state struct {
		StatusCheckRollup []interface{} `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal(out, &state); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(state.StatusCheckRollup) != 0 {
		t.Errorf("expected nil/empty pipeline rollup, got %v", state.StatusCheckRollup)
	}
}

// --- State translation ---

func TestTranslateState(t *testing.T) {
	cases := []struct{ in, want string }{
		{"opened", "OPEN"},
		{"closed", "CLOSED"},
		{"merged", "MERGED"},
		{"locked", "OPEN"},
		{"unknown_state", "UNKNOWN_STATE"},
	}
	for _, tc := range cases {
		if got := translateState(tc.in); got != tc.want {
			t.Errorf("translateState(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// --- Mergeable translation ---

func TestTranslateMergeable(t *testing.T) {
	cases := []struct{ in, want string }{
		{"can_be_merged", "MERGEABLE"},
		{"cannot_be_merged", "CONFLICTING"},
		{"cannot_be_merged_recheck", "CONFLICTING"},
		{"checking", "UNKNOWN"},
		{"", "UNKNOWN"},
	}
	for _, tc := range cases {
		if got := translateMergeable(tc.in); got != tc.want {
			t.Errorf("translateMergeable(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// --- Pipeline rollup translation ---

func TestPipelineCheckRollup(t *testing.T) {
	cases := []struct {
		status     string
		wantStatus string
		wantConc   string
		wantNil    bool
	}{
		{"success", "COMPLETED", "SUCCESS", false},
		{"failed", "COMPLETED", "FAILURE", false},
		{"canceled", "COMPLETED", "CANCELLED", false},
		{"skipped", "COMPLETED", "SKIPPED", false},
		{"manual", "COMPLETED", "NEUTRAL", false},
		{"running", "IN_PROGRESS", "", false},
		{"pending", "QUEUED", "", false},
		{"created", "QUEUED", "", false},
		{"", "", "", true},
	}
	for _, tc := range cases {
		got := pipelineCheckRollup(tc.status)
		if tc.wantNil {
			if got != nil {
				t.Errorf("pipelineCheckRollup(%q): want nil, got %v", tc.status, got)
			}
			continue
		}
		if len(got) == 0 {
			t.Errorf("pipelineCheckRollup(%q): want non-empty", tc.status)
			continue
		}
		if got[0]["status"] != tc.wantStatus {
			t.Errorf("pipelineCheckRollup(%q) status = %q, want %q", tc.status, got[0]["status"], tc.wantStatus)
		}
		if got[0]["conclusion"] != tc.wantConc {
			t.Errorf("pipelineCheckRollup(%q) conclusion = %q, want %q", tc.status, got[0]["conclusion"], tc.wantConc)
		}
	}
}

// --- GetPRReviews ---

func TestGetPRReviews_withApprovals(t *testing.T) {
	golden := loadTestdata(t, "mr_view_with_approvals.json")
	p := newTestProvider(singleStub(golden, nil))

	out, err := p.GetPRReviews(42, "/repo")
	if err != nil {
		t.Fatalf("GetPRReviews: %v", err)
	}
	var resp struct {
		Reviews []struct {
			Author struct {
				Login string `json:"login"`
			} `json:"author"`
			State string `json:"state"`
		} `json:"reviews"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Reviews) != 1 {
		t.Fatalf("reviews len = %d, want 1", len(resp.Reviews))
	}
	if resp.Reviews[0].Author.Login != "alice" {
		t.Errorf("author = %q, want alice", resp.Reviews[0].Author.Login)
	}
	if resp.Reviews[0].State != "APPROVED" {
		t.Errorf("state = %q, want APPROVED", resp.Reviews[0].State)
	}
}

func TestGetPRReviews_noApprovals(t *testing.T) {
	golden := loadTestdata(t, "mr_view_open.json")
	p := newTestProvider(singleStub(golden, nil))

	out, err := p.GetPRReviews(42, "/repo")
	if err != nil {
		t.Fatalf("GetPRReviews: %v", err)
	}
	var resp struct {
		Reviews []interface{} `json:"reviews"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Reviews) != 0 {
		t.Errorf("reviews len = %d, want 0", len(resp.Reviews))
	}
}

// --- GetPRComments ---

func TestGetPRComments(t *testing.T) {
	golden := loadTestdata(t, "mr_note_list.json")
	var captured []string
	p := newTestProvider(singleStub(golden, &captured))

	out, err := p.GetPRComments(42, "/repo")
	if err != nil {
		t.Fatalf("GetPRComments: %v", err)
	}
	// Verify the glab note list command was used with correct args
	for _, want := range []string{"note", "list", "42", "-R", "kate/myproj", "--output", "json"} {
		if !contains(captured, want) {
			t.Errorf("expected %q in args: %v", want, captured)
		}
	}

	var resp struct {
		Comments []struct {
			Author struct {
				Login string `json:"login"`
			} `json:"author"`
			Body      string `json:"body"`
			CreatedAt string `json:"createdAt"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Comments) != 3 {
		t.Fatalf("comments len = %d, want 3", len(resp.Comments))
	}
	if resp.Comments[0].Author.Login != "alice" {
		t.Errorf("comment[0].author = %q, want alice", resp.Comments[0].Author.Login)
	}
	if resp.Comments[0].CreatedAt == "" {
		t.Error("comment[0].createdAt should not be empty")
	}
}

// --- GetReviewCommentsRaw ---

func TestGetReviewCommentsRaw_synthesizesJSONL(t *testing.T) {
	viewGolden := loadTestdata(t, "mr_view_with_approvals.json")
	noteGolden := loadTestdata(t, "mr_note_list.json")
	// First call is glabView, second is note list
	p := newTestProvider(seqStub([][]byte{viewGolden, noteGolden}))

	out, err := p.GetReviewCommentsRaw(42, "/repo")
	if err != nil {
		t.Fatalf("GetReviewCommentsRaw: %v", err)
	}

	// 1 approval (alice) + 3 notes = 4 lines
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) != 4 {
		t.Fatalf("line count = %d, want 4; output:\n%s", len(lines), out)
	}

	// First line: approval from alice (APPROVED)
	var first struct {
		Author string `json:"author"`
		State  string `json:"state"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("parse line 0: %v", err)
	}
	if first.Author != "alice" || first.State != "APPROVED" {
		t.Errorf("line[0]: author=%q state=%q, want alice/APPROVED", first.Author, first.State)
	}

	// Last line: [changes-requested] note → CHANGES_REQUESTED
	var last struct {
		State string `json:"state"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal([]byte(lines[3]), &last); err != nil {
		t.Fatalf("parse line 3: %v", err)
	}
	if last.State != "CHANGES_REQUESTED" {
		t.Errorf("line[3] state = %q, want CHANGES_REQUESTED", last.State)
	}
	if !strings.HasPrefix(last.Body, "[changes-requested]") {
		t.Errorf("line[3] body should start with [changes-requested]: %q", last.Body)
	}
}

func TestGetReviewCommentsRaw_emptyWhenNoReviews(t *testing.T) {
	viewGolden := loadTestdata(t, "mr_view_open.json") // no approved_by
	p := newTestProvider(seqStub([][]byte{viewGolden, []byte("[]")}))

	out, err := p.GetReviewCommentsRaw(42, "/repo")
	if err != nil {
		t.Fatalf("GetReviewCommentsRaw: %v", err)
	}
	if len(strings.TrimSpace(string(out))) != 0 {
		t.Errorf("expected empty output for no reviews, got: %q", out)
	}
}

// --- MarkPRReady ---

func TestMarkPRReady_callsGlabUpdate(t *testing.T) {
	var captured []string
	p := newTestProvider(singleStub(nil, &captured))

	if err := p.MarkPRReady(42, "/repo"); err != nil {
		t.Fatalf("MarkPRReady: %v", err)
	}
	for _, want := range []string{"update", "42", "--ready", "-R", "kate/myproj"} {
		if !contains(captured, want) {
			t.Errorf("expected %q in args: %v", want, captured)
		}
	}
}

// --- ClosePR ---

func TestClosePR_twoInvocations(t *testing.T) {
	var calls [][]string
	p := newTestProvider(func(dir, name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{}, args...))
		return nil, nil
	})

	if err := p.ClosePR(42, "/repo"); err != nil {
		t.Fatalf("ClosePR: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 glab invocations (close + note create), got %d", len(calls))
	}
	if !contains(calls[0], "close") {
		t.Errorf("first call should include 'close': %v", calls[0])
	}
	if !contains(calls[1], "note") || !contains(calls[1], "create") {
		t.Errorf("second call should include 'note' and 'create': %v", calls[1])
	}
	if !contains(calls[1], "Ticket cancelled.") {
		t.Errorf("second call should include cancellation message: %v", calls[1])
	}
}

// --- OpenPRInBrowser ---

func TestOpenPRInBrowser_passesWebFlag(t *testing.T) {
	var captured []string
	p := newTestProvider(singleStub(nil, &captured))

	if err := p.OpenPRInBrowser(42, "/repo"); err != nil {
		t.Fatalf("OpenPRInBrowser: %v", err)
	}
	if !contains(captured, "--web") {
		t.Errorf("expected --web in args: %v", captured)
	}
	if !contains(captured, "42") {
		t.Errorf("expected MR number 42 in args: %v", captured)
	}
}

// --- GetPRHeadBranch ---

func TestGetPRHeadBranch(t *testing.T) {
	golden := loadTestdata(t, "mr_view_open.json")
	p := newTestProvider(singleStub(golden, nil))

	branch, err := p.GetPRHeadBranch(42, "/repo")
	if err != nil {
		t.Fatalf("GetPRHeadBranch: %v", err)
	}
	if branch != "prole/copper/nc-42" {
		t.Errorf("branch = %q, want prole/copper/nc-42", branch)
	}
}

// --- FindPRByBranch ---

func TestFindPRByBranch_found(t *testing.T) {
	golden := loadTestdata(t, "mr_list.json")
	var captured []string
	p := newTestProvider(singleStub(golden, &captured))

	iid, found, err := p.FindPRByBranch("feature-x", "/repo")
	if err != nil {
		t.Fatalf("FindPRByBranch: %v", err)
	}
	if !found {
		t.Fatal("expected found=true")
	}
	// Golden: iid=42 (opened, newer) and iid=40 (merged, older) → picks 42
	if iid != 42 {
		t.Errorf("iid = %d, want 42 (most recently updated open MR)", iid)
	}
	for _, want := range []string{"--source-branch", "feature-x", "--state", "all", "--output", "json"} {
		if !contains(captured, want) {
			t.Errorf("expected %q in args: %v", want, captured)
		}
	}
}

func TestFindPRByBranch_notFound(t *testing.T) {
	p := newTestProvider(singleStub([]byte("[]"), nil))

	_, found, err := p.FindPRByBranch("no-such-branch", "/repo")
	if err != nil {
		t.Fatalf("FindPRByBranch: %v", err)
	}
	if found {
		t.Error("expected found=false for empty list")
	}
}

// --- pickMostRecentMR ---

func TestPickMostRecentMR_prefersMergedOnTie(t *testing.T) {
	ts := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	entries := []glabMRListEntry{
		{IID: 1, State: "opened", UpdatedAt: ts},
		{IID: 2, State: "merged", UpdatedAt: ts},
	}
	if got := pickMostRecentMR(entries); got != 2 {
		t.Errorf("expected iid=2 (merged wins tie), got %d", got)
	}
}

func TestPickMostRecentMR_prefersNewerUpdated(t *testing.T) {
	older := time.Date(2026, 4, 14, 10, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 4, 16, 10, 0, 0, 0, time.UTC)
	entries := []glabMRListEntry{
		{IID: 1, State: "merged", UpdatedAt: older},
		{IID: 2, State: "opened", UpdatedAt: newer},
	}
	if got := pickMostRecentMR(entries); got != 2 {
		t.Errorf("expected iid=2 (newer wins), got %d", got)
	}
}

// --- Subgroup repo path ---

func TestSubgroupRepoPath_passedVerbatim(t *testing.T) {
	p := &Provider{
		project: "kate/sub1/sub2/myproj",
		runCmd: func(dir, name string, args ...string) ([]byte, error) {
			for i, a := range args {
				if a == "-R" && i+1 < len(args) && args[i+1] == "kate/sub1/sub2/myproj" {
					return []byte(`{"iid":1,"web_url":"https://gitlab.example.com/kate/sub1/sub2/myproj/-/merge_requests/1"}`), nil
				}
			}
			return nil, fmt.Errorf("expected -R kate/sub1/sub2/myproj in %v", args)
		},
	}

	url, err := p.CreatePR("T", "B", false, "/repo")
	if err != nil {
		t.Fatalf("CreatePR with subgroup path: %v", err)
	}
	if !strings.Contains(url, "sub1/sub2") {
		t.Errorf("URL = %q, expected subgroup path preserved", url)
	}
}
