// Package repo provides database access for issues, agents, and dependencies.
package repo

// Status constants for issue statuses. Use these instead of raw string
// literals so typos become compile errors and renames are mechanical.
const (
	StatusIdeating      = "ideating"
	StatusDraft         = "draft"
	StatusOpen          = "open"
	StatusInProgress    = "in_progress"
	StatusCIRunning     = "ci_running"
	StatusInReview      = "in_review"
	StatusUnderReview   = "under_review"
	StatusPROpen        = "pr_open"
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
	// StatusStopped means the agent has finished its shutdown protocol
	// (committed in-flight work, wrote handoff if applicable) and is safe
	// to kill. Distinct from idle (ready for new work) and dead (already gone).
	StatusStopped = "stopped"
)

// ValidAgentStatuses is the complete set of valid agent status values.
var ValidAgentStatuses = []string{StatusIdle, StatusWorking, StatusDead, StatusStopped}

// DisplayStatusOrder is the canonical order for human-facing status enumerations
// (gt status, ct metrics). Terminal statuses (closed, cancelled) are intentionally
// omitted — callers that want them displayed include them separately.
//
// Every non-terminal status in ValidStatuses must appear here.
// TestDisplayStatusOrderIncludesAllNonTerminal enforces this invariant:
// adding a status to ValidStatuses without updating this slice causes a test failure.
var DisplayStatusOrder = []string{
	StatusIdeating, StatusDraft, StatusOpen, StatusInProgress,
	StatusCIRunning,
	StatusInReview, StatusUnderReview, StatusPROpen,
	StatusRepairing, StatusOnHold, StatusMergeConflict,
}

// IsTerminalStatus returns true for statuses that represent final, immutable
// outcomes — work that landed (closed) or was abandoned (cancelled). A ticket
// in a terminal status will never be re-opened, re-assigned, or block other work.
func IsTerminalStatus(s string) bool {
	return s == StatusClosed || s == StatusCancelled
}

// IsBlockingAncestorStatus returns true for ancestor statuses that should
// prevent descendants from being selected for work:
//   - on_hold: explicitly paused; children should pause too.
//   - cancelled: work abandoned; no point landing children under a dead branch.
//   - draft: scope not yet stable; children may change before the parent is finalised.
//   - ideating: pre-draft; mayor-CEO iteration in progress; nothing downstream runs.
func IsBlockingAncestorStatus(s string) bool {
	return s == StatusOnHold || s == StatusCancelled || s == StatusDraft || s == StatusIdeating
}
