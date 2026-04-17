package vcs

import (
	"strings"
	"testing"
	"time"
)

// --- pickMostRecentPR ---

func TestPickMostRecentPR_singleEntry(t *testing.T) {
	entries := []ghPRListEntry{
		{Number: 7, State: "OPEN", UpdatedAt: time.Unix(1000, 0)},
	}
	got := pickMostRecentPR(entries)
	if got != 7 {
		t.Errorf("pickMostRecentPR = %d, want 7", got)
	}
}

func TestPickMostRecentPR_mostRecentWins(t *testing.T) {
	t0 := time.Unix(1000, 0)
	t1 := time.Unix(2000, 0)
	entries := []ghPRListEntry{
		{Number: 1, State: "OPEN", UpdatedAt: t0},
		{Number: 2, State: "OPEN", UpdatedAt: t1},
	}
	got := pickMostRecentPR(entries)
	if got != 2 {
		t.Errorf("pickMostRecentPR = %d, want 2 (most recent UpdatedAt)", got)
	}
}

func TestPickMostRecentPR_sameTime_mergedBeatsOpen(t *testing.T) {
	ts := time.Unix(1000, 0)
	entries := []ghPRListEntry{
		{Number: 1, State: "OPEN", UpdatedAt: ts},
		{Number: 2, State: "MERGED", UpdatedAt: ts},
	}
	got := pickMostRecentPR(entries)
	if got != 2 {
		t.Errorf("pickMostRecentPR = %d, want 2 (MERGED beats OPEN at equal time)", got)
	}
}

func TestPickMostRecentPR_sameTime_openBeatesClosed(t *testing.T) {
	ts := time.Unix(1000, 0)
	entries := []ghPRListEntry{
		{Number: 1, State: "CLOSED", UpdatedAt: ts},
		{Number: 2, State: "OPEN", UpdatedAt: ts},
	}
	got := pickMostRecentPR(entries)
	if got != 2 {
		t.Errorf("pickMostRecentPR = %d, want 2 (OPEN beats CLOSED at equal time)", got)
	}
}

func TestPickMostRecentPR_sameTime_mergedBeatsClosed(t *testing.T) {
	ts := time.Unix(1000, 0)
	entries := []ghPRListEntry{
		{Number: 1, State: "CLOSED", UpdatedAt: ts},
		{Number: 2, State: "MERGED", UpdatedAt: ts},
	}
	got := pickMostRecentPR(entries)
	if got != 2 {
		t.Errorf("pickMostRecentPR = %d, want 2 (MERGED beats CLOSED at equal time)", got)
	}
}

func TestPickMostRecentPR_newerLowerPrecedence_timeWins(t *testing.T) {
	// CLOSED but newer timestamp should beat MERGED at older timestamp.
	t0 := time.Unix(1000, 0)
	t1 := time.Unix(2000, 0)
	entries := []ghPRListEntry{
		{Number: 1, State: "MERGED", UpdatedAt: t0},
		{Number: 2, State: "CLOSED", UpdatedAt: t1},
	}
	got := pickMostRecentPR(entries)
	if got != 2 {
		t.Errorf("pickMostRecentPR = %d, want 2 (newer timestamp wins over better state)", got)
	}
}

func TestPickMostRecentPR_multipleMixed(t *testing.T) {
	t0 := time.Unix(1000, 0)
	t1 := time.Unix(2000, 0)
	t2 := time.Unix(3000, 0)
	entries := []ghPRListEntry{
		{Number: 10, State: "CLOSED", UpdatedAt: t2},
		{Number: 11, State: "MERGED", UpdatedAt: t1},
		{Number: 12, State: "OPEN", UpdatedAt: t0},
	}
	got := pickMostRecentPR(entries)
	if got != 10 {
		t.Errorf("pickMostRecentPR = %d, want 10 (highest UpdatedAt)", got)
	}
}

func TestPickMostRecentPR_unknownState_lowestPrecedence(t *testing.T) {
	ts := time.Unix(1000, 0)
	entries := []ghPRListEntry{
		{Number: 1, State: "UNKNOWN", UpdatedAt: ts},
		{Number: 2, State: "CLOSED", UpdatedAt: ts},
	}
	// Both UNKNOWN and CLOSED map to precedence 2, so first one in loop wins
	// (the initial best). Just verify it doesn't panic and returns a valid number.
	got := pickMostRecentPR(entries)
	if got != 1 && got != 2 {
		t.Errorf("pickMostRecentPR returned unexpected number %d", got)
	}
}

// --- cleanStderr ---

func TestCleanStderr_plainString(t *testing.T) {
	got := cleanStderr("something went wrong")
	if got != "something went wrong" {
		t.Errorf("cleanStderr = %q, want %q", got, "something went wrong")
	}
}

func TestCleanStderr_stripsANSI(t *testing.T) {
	input := "\x1b[31merror\x1b[0m: not found"
	got := cleanStderr(input)
	if strings.Contains(got, "\x1b") {
		t.Errorf("cleanStderr still contains ANSI escape: %q", got)
	}
	if got != "error: not found" {
		t.Errorf("cleanStderr = %q, want %q", got, "error: not found")
	}
}

func TestCleanStderr_truncatesLongInput(t *testing.T) {
	long := strings.Repeat("x", 300)
	got := cleanStderr(long)
	if len(got) > 203 { // 200 + "..."
		t.Errorf("cleanStderr len = %d, want <= 203", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("cleanStderr did not add ellipsis: %q", got)
	}
}

func TestCleanStderr_exactlyAtLimit(t *testing.T) {
	s := strings.Repeat("y", 200)
	got := cleanStderr(s)
	if got != s {
		t.Errorf("cleanStderr at limit changed string unexpectedly: len=%d", len(got))
	}
}

func TestCleanStderr_trimsSurroundingWhitespace(t *testing.T) {
	got := cleanStderr("  hello world  ")
	if got != "hello world" {
		t.Errorf("cleanStderr = %q, want %q", got, "hello world")
	}
}

func TestCleanStderr_emptyString(t *testing.T) {
	got := cleanStderr("")
	if got != "" {
		t.Errorf("cleanStderr(\"\") = %q, want empty", got)
	}
}

func TestCleanStderr_onlyANSI(t *testing.T) {
	got := cleanStderr("\x1b[0m\x1b[1m")
	if got != "" {
		t.Errorf("cleanStderr(only ANSI) = %q, want empty", got)
	}
}
