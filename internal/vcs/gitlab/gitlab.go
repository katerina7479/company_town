package gitlab

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Provider implements vcs.Provider using the glab CLI.
// All glab invocations pass -R <project> so the adapter is location-independent.
type Provider struct {
	project string // GitLab project path, e.g. "kate/myproj" or "kate/sub/proj"
	runCmd  func(dir, name string, args ...string) ([]byte, error)
}

// New returns a GitLab VCS provider backed by the glab CLI.
func New(project string) *Provider {
	return &Provider{project: project, runCmd: runReal}
}

// runReal executes a CLI command with dir as the working directory.
// Stderr is captured and appended to the error on failure.
func runReal(dir, name string, args ...string) ([]byte, error) {
	var stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		snippet := strings.TrimSpace(stderr.String())
		const snippetLen = 200
		if len(snippet) > snippetLen {
			snippet = snippet[:snippetLen] + "..."
		}
		if snippet != "" {
			return nil, fmt.Errorf("%w: %s", err, snippet)
		}
		return nil, err
	}
	return out, nil
}

// glabMRView is the parsed representation of glab mr view --output json.
type glabMRView struct {
	IID          int     `json:"iid"`
	Title        string  `json:"title"`
	State        string  `json:"state"`
	SourceBranch string  `json:"source_branch"`
	TargetBranch string  `json:"target_branch"`
	WebURL       string  `json:"web_url"`
	MergeStatus  string  `json:"merge_status"`
	MergedAt     *string `json:"merged_at"`
	HeadPipeline *struct {
		ID     int    `json:"id"`
		Status string `json:"status"`
	} `json:"head_pipeline"`
	ApprovedBy []struct {
		User struct {
			Username string `json:"username"`
			Name     string `json:"name"`
		} `json:"user"`
	} `json:"approved_by"`
}

// glabNote is one item from glab mr note list --output json.
type glabNote struct {
	ID        int    `json:"id"`
	Body      string `json:"body"`
	CreatedAt string `json:"created_at"`
	Author    struct {
		Username string `json:"username"`
		Name     string `json:"name"`
	} `json:"author"`
}

// glabMRListEntry is one element from glab mr list --output json.
type glabMRListEntry struct {
	IID       int       `json:"iid"`
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updated_at"`
}

// glabView calls glab mr view for the given MR number and parses the response.
func (p *Provider) glabView(prNum int, repoDir string) (*glabMRView, error) {
	out, err := p.runCmd(repoDir, "glab", "mr", "view",
		strconv.Itoa(prNum), "-R", p.project, "--output", "json")
	if err != nil {
		return nil, err
	}
	var mr glabMRView
	if err := json.Unmarshal(out, &mr); err != nil {
		return nil, fmt.Errorf("parsing glab mr view: %w", err)
	}
	return &mr, nil
}

// translateState converts a GitLab MR state to the GitHub-compatible enum.
// "locked" is collapsed to "OPEN"; see doc.go for rationale.
func translateState(s string) string {
	switch s {
	case "opened", "locked":
		return "OPEN"
	case "merged":
		return "MERGED"
	case "closed":
		return "CLOSED"
	default:
		return strings.ToUpper(s)
	}
}

// translateMergeable converts a GitLab merge_status to GitHub's mergeable enum.
func translateMergeable(s string) string {
	switch s {
	case "can_be_merged":
		return "MERGEABLE"
	case "cannot_be_merged", "cannot_be_merged_recheck":
		return "CONFLICTING"
	default:
		return "UNKNOWN"
	}
}

// pipelineCheckRollup converts a GitLab pipeline status into a synthetic
// statusCheckRollup slice shaped like GitHub's API response.
// Returns nil when there is no pipeline.
func pipelineCheckRollup(status string) []map[string]string {
	if status == "" {
		return nil
	}
	var s, conclusion string
	switch status {
	case "success":
		s, conclusion = "COMPLETED", "SUCCESS"
	case "failed":
		s, conclusion = "COMPLETED", "FAILURE"
	case "canceled":
		s, conclusion = "COMPLETED", "CANCELLED"
	case "skipped":
		s, conclusion = "COMPLETED", "SKIPPED"
	case "manual":
		s, conclusion = "COMPLETED", "NEUTRAL"
	case "running":
		s, conclusion = "IN_PROGRESS", ""
	case "pending", "created":
		s, conclusion = "QUEUED", ""
	default:
		s, conclusion = "COMPLETED", "NEUTRAL"
	}
	return []map[string]string{{"name": "pipeline", "status": s, "conclusion": conclusion}}
}

// CreatePR creates a merge request and returns the MR web URL.
func (p *Provider) CreatePR(title, body string, draft bool, repoDir string) (string, error) {
	args := []string{
		"mr", "create", "-R", p.project,
		"--title", title, "--description", body,
		"--output", "json",
	}
	if draft {
		args = append(args, "--draft")
	}
	out, err := p.runCmd(repoDir, "glab", args...)
	if err != nil {
		return "", err
	}
	var resp struct {
		WebURL string `json:"web_url"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return "", fmt.Errorf("parsing glab mr create response: %w", err)
	}
	return resp.WebURL, nil
}

// GetPRMetadata returns MR metadata in GitHub-compatible JSON shape with fields:
// number, title, state, headRefName, mergeable, reviewDecision, statusCheckRollup.
func (p *Provider) GetPRMetadata(prNum int, repoDir string) ([]byte, error) {
	mr, err := p.glabView(prNum, repoDir)
	if err != nil {
		return nil, err
	}
	var pipelineStatus string
	if mr.HeadPipeline != nil {
		pipelineStatus = mr.HeadPipeline.Status
	}
	result := map[string]interface{}{
		"number":            mr.IID,
		"title":             mr.Title,
		"state":             translateState(mr.State),
		"headRefName":       mr.SourceBranch,
		"mergeable":         translateMergeable(mr.MergeStatus),
		"reviewDecision":    "",
		"statusCheckRollup": pipelineCheckRollup(pipelineStatus),
	}
	return json.Marshal(result)
}

// GetPRReviews returns approval records in GitHub-compatible JSON shape:
// {"reviews": [{"author": {"login": "..."}, "state": "APPROVED", "submittedAt": "", "body": ""}]}.
func (p *Provider) GetPRReviews(prNum int, repoDir string) ([]byte, error) {
	mr, err := p.glabView(prNum, repoDir)
	if err != nil {
		return nil, err
	}
	type ghAuthor struct {
		Login string `json:"login"`
	}
	type ghReview struct {
		Author      ghAuthor `json:"author"`
		State       string   `json:"state"`
		SubmittedAt string   `json:"submittedAt"`
		Body        string   `json:"body"`
	}
	reviews := make([]ghReview, 0, len(mr.ApprovedBy))
	for _, a := range mr.ApprovedBy {
		reviews = append(reviews, ghReview{
			Author: ghAuthor{Login: a.User.Username},
			State:  "APPROVED",
		})
	}
	return json.Marshal(map[string]interface{}{"reviews": reviews})
}

// GetPRComments returns MR notes in GitHub-compatible JSON shape:
// {"comments": [{"author": {"login": "..."}, "body": "...", "createdAt": "..."}]}.
func (p *Provider) GetPRComments(prNum int, repoDir string) ([]byte, error) {
	out, err := p.runCmd(repoDir, "glab", "mr", "note", "list",
		strconv.Itoa(prNum), "-R", p.project, "--output", "json")
	if err != nil {
		return nil, err
	}
	var notes []glabNote
	if err := json.Unmarshal(out, &notes); err != nil {
		return nil, fmt.Errorf("parsing glab mr note list: %w", err)
	}
	type ghAuthor struct {
		Login string `json:"login"`
	}
	type ghComment struct {
		Author    ghAuthor `json:"author"`
		Body      string   `json:"body"`
		CreatedAt string   `json:"createdAt"`
	}
	comments := make([]ghComment, 0, len(notes))
	for _, n := range notes {
		comments = append(comments, ghComment{
			Author:    ghAuthor{Login: n.Author.Username},
			Body:      n.Body,
			CreatedAt: n.CreatedAt,
		})
	}
	return json.Marshal(map[string]interface{}{"comments": comments})
}

// MarkPRReady converts a draft MR to ready-for-review.
func (p *Provider) MarkPRReady(prNum int, repoDir string) error {
	_, err := p.runCmd(repoDir, "glab", "mr", "update",
		strconv.Itoa(prNum), "-R", p.project, "--ready")
	return err
}

// ClosePR closes a MR with a standard cancellation comment.
func (p *Provider) ClosePR(prNum int, repoDir string) error {
	if _, err := p.runCmd(repoDir, "glab", "mr", "close",
		strconv.Itoa(prNum), "-R", p.project); err != nil {
		return err
	}
	_, err := p.runCmd(repoDir, "glab", "mr", "note", "create",
		strconv.Itoa(prNum), "-R", p.project, "--message", "Ticket cancelled.")
	return err
}

// OpenPRInBrowser opens the MR page in the default web browser.
func (p *Provider) OpenPRInBrowser(prNum int, repoDir string) error {
	_, err := p.runCmd(repoDir, "glab", "mr", "view",
		strconv.Itoa(prNum), "-R", p.project, "--web")
	return err
}

// GetPRHeadBranch returns the source branch name for the given MR.
func (p *Provider) GetPRHeadBranch(prNum int, repoDir string) (string, error) {
	mr, err := p.glabView(prNum, repoDir)
	if err != nil {
		return "", fmt.Errorf("glab mr view %d: %w", prNum, err)
	}
	return mr.SourceBranch, nil
}

// GetPRStateJSON returns MR state in GitHub-compatible JSON shape with fields:
// state, mergedAt, mergeable, statusCheckRollup.
func (p *Provider) GetPRStateJSON(prNum int, repoDir string) ([]byte, error) {
	mr, err := p.glabView(prNum, repoDir)
	if err != nil {
		return nil, err
	}
	var pipelineStatus string
	if mr.HeadPipeline != nil {
		pipelineStatus = mr.HeadPipeline.Status
	}
	result := map[string]interface{}{
		"state":             translateState(mr.State),
		"mergedAt":          mr.MergedAt,
		"mergeable":         translateMergeable(mr.MergeStatus),
		"statusCheckRollup": pipelineCheckRollup(pipelineStatus),
	}
	return json.Marshal(result)
}

// GetReviewCommentsRaw returns JSONL review records for the given MR.
// Each line is a JSON object with author, authorType, state, body fields.
// Approvals produce APPROVED state; notes whose body contains
// "[changes-requested]" (as a leading sentinel, optionally preceded by
// "[ct-reviewer]") produce CHANGES_REQUESTED; all other notes produce
// COMMENTED. See doc.go for the convention details.
func (p *Provider) GetReviewCommentsRaw(prNum int, repoDir string) ([]byte, error) {
	mr, err := p.glabView(prNum, repoDir)
	if err != nil {
		return nil, err
	}

	notesOut, err := p.runCmd(repoDir, "glab", "mr", "note", "list",
		strconv.Itoa(prNum), "-R", p.project, "--output", "json")
	if err != nil {
		return nil, err
	}
	var notes []glabNote
	if err := json.Unmarshal(notesOut, &notes); err != nil {
		return nil, fmt.Errorf("parsing glab mr note list: %w", err)
	}

	type reviewLine struct {
		Author     string `json:"author"`
		AuthorType string `json:"authorType"`
		State      string `json:"state"`
		Body       string `json:"body"`
	}

	var buf bytes.Buffer
	for _, a := range mr.ApprovedBy {
		b, _ := json.Marshal(reviewLine{
			Author:     a.User.Username,
			AuthorType: "User",
			State:      "APPROVED",
		})
		buf.Write(b)
		buf.WriteByte('\n')
	}
	for _, n := range notes {
		state := "COMMENTED"
		if hasChangesRequestedSentinel(n.Body) {
			state = "CHANGES_REQUESTED"
		}
		b, _ := json.Marshal(reviewLine{
			Author:     n.Author.Username,
			AuthorType: "User",
			State:      state,
			Body:       n.Body,
		})
		buf.Write(b)
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

// FindPRByBranch finds an MR for the given source branch.
// Returns (mrIID, found, error). found is false when no matching MR exists.
func (p *Provider) FindPRByBranch(branch, repoDir string) (int, bool, error) {
	out, err := p.runCmd(repoDir, "glab", "mr", "list",
		"-R", p.project,
		"--source-branch", branch,
		"--state", "all",
		"--output", "json",
	)
	if err != nil {
		return 0, false, fmt.Errorf("glab mr list: %w", err)
	}
	var results []glabMRListEntry
	if err := json.Unmarshal(out, &results); err != nil {
		return 0, false, fmt.Errorf("parsing glab mr list: %w", err)
	}
	if len(results) == 0 {
		return 0, false, nil
	}
	return pickMostRecentMR(results), true, nil
}

// hasChangesRequestedSentinel reports whether body carries the [changes-requested]
// sentinel. The reviewer may prepend [ct-reviewer] first (the canonical format
// is "[ct-reviewer][changes-requested] ..."), so the check scans the first
// two tokens rather than requiring an exact prefix.
func hasChangesRequestedSentinel(body string) bool {
	const sentinel = "[changes-requested]"
	trimmed := strings.TrimSpace(body)
	if strings.HasPrefix(trimmed, sentinel) {
		return true
	}
	// Allow [ct-reviewer][changes-requested] prefix.
	const ctPrefix = "[ct-reviewer]"
	if strings.HasPrefix(trimmed, ctPrefix) {
		return strings.HasPrefix(strings.TrimSpace(trimmed[len(ctPrefix):]), sentinel)
	}
	return false
}

// pickMostRecentMR selects the most authoritative MR from a list.
// Sorts by UpdatedAt descending; ties broken by state precedence: merged > opened > closed.
func pickMostRecentMR(entries []glabMRListEntry) int {
	if len(entries) == 0 {
		return 0
	}
	statePrecedence := func(s string) int {
		switch s {
		case "merged":
			return 0
		case "opened":
			return 1
		default:
			return 2
		}
	}
	best := entries[0]
	for _, e := range entries[1:] {
		if e.UpdatedAt.After(best.UpdatedAt) {
			best = e
		} else if e.UpdatedAt.Equal(best.UpdatedAt) && statePrecedence(e.State) < statePrecedence(best.State) {
			best = e
		}
	}
	return best.IID
}
