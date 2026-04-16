// Package vcs provides a platform-agnostic interface for VCS operations
// (pull request lifecycle, branch queries) so callers are not tied to
// a specific platform. The GitHub adapter wraps the gh CLI.
package vcs

// Provider abstracts VCS platform operations so callers can be
// platform-agnostic (GitHub today, GitLab in future tickets).
type Provider interface {
	// CreatePR creates a pull request and returns the PR URL.
	CreatePR(title, body string, draft bool, repoDir string) (string, error)
	// GetPRMetadata returns raw JSON for PR metadata fields:
	// number, title, state, headRefName, mergeable, reviewDecision, statusCheckRollup.
	GetPRMetadata(prNum int, repoDir string) ([]byte, error)
	// GetPRReviews returns raw JSON for PR reviews.
	GetPRReviews(prNum int, repoDir string) ([]byte, error)
	// GetPRComments returns raw JSON for PR issue comments.
	GetPRComments(prNum int, repoDir string) ([]byte, error)
	// MarkPRReady converts a draft PR to ready-for-review.
	MarkPRReady(prNum int, repoDir string) error
	// ClosePR closes a PR with a standard cancellation comment.
	ClosePR(prNum int, repoDir string) error
	// OpenPRInBrowser opens a PR page in the default web browser.
	OpenPRInBrowser(prNum int, repoDir string) error
	// GetPRHeadBranch returns the head branch name for the given PR number.
	GetPRHeadBranch(prNum int, repoDir string) (string, error)
	// GetPRStateJSON returns raw JSON for PR state:
	// state, mergedAt, mergeable, statusCheckRollup.
	GetPRStateJSON(prNum int, repoDir string) ([]byte, error)
	// GetReviewCommentsRaw returns JSONL-formatted review records for a PR.
	// Each line is a JSON object with author, authorType, state, body fields.
	GetReviewCommentsRaw(prNum int, repoDir string) ([]byte, error)
	// FindPRByBranch finds a PR for the given head branch.
	// Returns (prNumber, found, error). found is false when no matching PR exists.
	FindPRByBranch(branch, repoDir string) (int, bool, error)
}
