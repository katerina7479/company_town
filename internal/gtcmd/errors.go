// Package gtcmd implements the gt agent CLI subcommands.
package gtcmd

import "errors"

// Sentinel errors for gtcmd commands. Use errors.Is to test for these.
var (
	// ErrSessionAlreadyExists is returned when a session with the requested name
	// already exists in tmux.
	ErrSessionAlreadyExists = errors.New("session already exists")

	// ErrTitleRequired is returned by ticket create when no title is provided.
	ErrTitleRequired = errors.New("title is required")

	// ErrExpectedOneTitle is returned by ticket create when multiple positional
	// arguments are given where exactly one (the title) is expected.
	ErrExpectedOneTitle = errors.New("expected one title")

	// ErrUnknownFlag is returned when an unrecognised flag is passed to a command.
	ErrUnknownFlag = errors.New("unknown flag")

	// ErrDescriptionRequired is returned when --description is given with no value.
	ErrDescriptionRequired = errors.New("--description requires a value")

	// ErrInvalidType is returned when an unrecognised issue type is provided.
	ErrInvalidType = errors.New("invalid type")

	// ErrNotUnderReview is returned by ticket review when the ticket is not in
	// under_review status.
	ErrNotUnderReview = errors.New("not under_review")

	// ErrUnknownVerdict is returned by ticket review when an unrecognised verdict
	// string is given.
	ErrUnknownVerdict = errors.New("unknown verdict")

	// ErrNoAssignee is returned when a status transition requires an assignee but
	// the ticket has none.
	ErrNoAssignee = errors.New("no assignee")

	// ErrHeadDetached is returned by pr create/update when git HEAD is detached.
	ErrHeadDetached = errors.New("HEAD is detached")

	// ErrDefaultBranch is returned by pr create when the current branch is the
	// repository default branch.
	ErrDefaultBranch = errors.New("default branch")

	// ErrNoCommitsYet is returned by pr create when the ticket branch has no
	// commits.
	ErrNoCommitsYet = errors.New("branch has no commits yet")

	// ErrNotRepairingStatus is returned by pr update when the ticket is not in
	// repairing status.
	ErrNotRepairingStatus = errors.New("ticket is not in repairing status")

	// ErrNoBranchSet is returned when a ticket has no branch recorded.
	ErrNoBranchSet = errors.New("ticket has no branch set")

	// ErrNoPRSet is returned by pr ready when the ticket has no PR number
	// recorded (i.e. no draft PR has been created yet).
	ErrNoPRSet = errors.New("ticket has no PR set")
)
