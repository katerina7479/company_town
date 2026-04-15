package repo

import "testing"

// TestValidStatusesMatchConsts ensures every Status* const appears in
// ValidStatuses and every ValidStatuses entry has a matching Status* const.
// This is a cheap compile-time + runtime drift detector: add a new status and
// forget to update either side, and this test tells you.
func TestValidStatusesMatchConsts(t *testing.T) {
	allConsts := map[string]bool{
		StatusDraft:         true,
		StatusOpen:          true,
		StatusInProgress:    true,
		StatusCIRunning:     true,
		StatusInReview:      true,
		StatusUnderReview:   true,
		StatusPROpen:        true,
		StatusReviewed:      true,
		StatusRepairing:     true,
		StatusMergeConflict: true,
		StatusOnHold:        true,
		StatusClosed:        true,
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
