// Package repo provides database access for issues, agents, and dependencies.
package repo

// Status constants for issue statuses. Use these instead of raw string
// literals so typos become compile errors and renames are mechanical.
const (
	StatusDraft         = "draft"
	StatusOpen          = "open"
	StatusInProgress    = "in_progress"
	StatusCIRunning     = "ci_running"
	StatusInReview      = "in_review"
	StatusUnderReview   = "under_review"
	StatusPROpen        = "pr_open"
	StatusReviewed      = "reviewed"
	StatusRepairing     = "repairing"
	StatusMergeConflict = "merge_conflict"
	StatusOnHold        = "on_hold"
	StatusClosed        = "closed"
	StatusCancelled     = "cancelled"
)

// Agent status constants. Use these instead of raw string literals.
const (
	StatusWorking = "working"
	StatusIdle    = "idle"
	StatusDead    = "dead"
)

// IsTerminalStatus returns true for statuses that represent final, immutable
// outcomes — work that landed (closed) or was abandoned (cancelled). A ticket
// in a terminal status will never be re-opened, re-assigned, or block other work.
func IsTerminalStatus(s string) bool {
	return s == StatusClosed || s == StatusCancelled
}
