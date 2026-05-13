package repo

import "testing"

// TestValidStatusesMatchConsts ensures every Status* const appears in
// ValidStatuses and every ValidStatuses entry has a matching Status* const.
// This is a cheap compile-time + runtime drift detector: add a new status and
// forget to update either side, and this test tells you.
func TestValidStatusesMatchConsts(t *testing.T) {
	allConsts := map[string]bool{
		StatusIdeating:      true,
		StatusDraft:         true,
		StatusOpen:          true,
		StatusInProgress:    true,
		StatusCIRunning:     true,
		StatusInReview:      true,
		StatusPROpen:        true,
		StatusRepairing:     true,
		StatusMergeConflict: true,
		StatusOnHold:        true,
		StatusClosed:        true,
		StatusCancelled:     true,
	}

	// Every entry in ValidStatuses must have a matching const.
	for _, s := range ValidStatuses {
		if !allConsts[s] {
			t.Errorf("ValidStatuses has %q but no matching Status* const", s)
		}
		delete(allConsts, s)
	}

	// Every const must appear in ValidStatuses.
	for s := range allConsts {
		t.Errorf("Status* const %q is missing from ValidStatuses", s)
	}
}

// TestDisplayStatusOrderIncludesAllNonTerminal asserts that every non-terminal
// status in ValidStatuses appears in DisplayStatusOrder, and that terminal
// statuses (closed/cancelled) are NOT included. This is the drift guard: add
// a new status to ValidStatuses without updating DisplayStatusOrder and this
// test fails with a clear message naming the missing status.
func TestDisplayStatusOrderIncludesAllNonTerminal(t *testing.T) {
	inDisplay := make(map[string]bool, len(DisplayStatusOrder))
	for _, s := range DisplayStatusOrder {
		inDisplay[s] = true
	}

	for _, s := range ValidStatuses {
		if IsTerminalStatus(s) {
			if inDisplay[s] {
				t.Errorf("DisplayStatusOrder must NOT include terminal status %q", s)
			}
			continue
		}
		if !inDisplay[s] {
			t.Errorf("DisplayStatusOrder is missing non-terminal status %q (added to ValidStatuses without updating DisplayStatusOrder)", s)
		}
	}
}
