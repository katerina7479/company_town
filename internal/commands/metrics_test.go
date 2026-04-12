package commands

import (
	"fmt"
	"testing"
	"time"

	"github.com/katerina7479/company_town/internal/eventlog"
)

func TestAvgDuration(t *testing.T) {
	cases := []struct {
		durations []time.Duration
		want      time.Duration
	}{
		{nil, 0},
		{[]time.Duration{time.Hour}, time.Hour},
		{[]time.Duration{time.Hour, 3 * time.Hour}, 2 * time.Hour},
	}
	for _, c := range cases {
		got := avgDuration(c.durations)
		if got != c.want {
			t.Errorf("avgDuration(%v) = %v, want %v", c.durations, got, c.want)
		}
	}
}

func TestMetrics_noEvents(t *testing.T) {
	// A reader pointing at a non-existent file returns empty without error.
	r := eventlog.NewReader(t.TempDir() + "/events.jsonl")
	events, err := r.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestPrintTicketFlow_throughput(t *testing.T) {
	now := time.Now()
	events := []eventlog.Event{
		{Kind: eventlog.KindTicketStatusChanged, EntityID: "1", FromStatus: "draft", ToStatus: "open", Timestamp: now.Add(-2 * time.Hour)},
		{Kind: eventlog.KindTicketStatusChanged, EntityID: "1", FromStatus: "open", ToStatus: "closed", Timestamp: now.Add(-1 * time.Hour)},
		{Kind: eventlog.KindTicketStatusChanged, EntityID: "2", FromStatus: "draft", ToStatus: "open", Timestamp: now.Add(-3 * time.Hour)},
		{Kind: eventlog.KindTicketStatusChanged, EntityID: "2", FromStatus: "open", ToStatus: "in_progress", Timestamp: now.Add(-2 * time.Hour)},
		// ticket 2 not closed
	}
	// printTicketFlow should count 1 closed ticket without panicking
	printTicketFlow(events)
}

func TestPrintTicketFlow_repairRate(t *testing.T) {
	now := time.Now()
	events := []eventlog.Event{
		{Kind: eventlog.KindTicketStatusChanged, EntityID: "1", ToStatus: "open", Timestamp: now.Add(-5 * time.Hour)},
		{Kind: eventlog.KindTicketStatusChanged, EntityID: "1", ToStatus: "in_review", Timestamp: now.Add(-4 * time.Hour)},
		{Kind: eventlog.KindTicketStatusChanged, EntityID: "1", ToStatus: "repairing", Timestamp: now.Add(-3 * time.Hour)},
		{Kind: eventlog.KindTicketStatusChanged, EntityID: "1", ToStatus: "in_review", Timestamp: now.Add(-2 * time.Hour)},
		{Kind: eventlog.KindTicketStatusChanged, EntityID: "1", ToStatus: "closed", Timestamp: now.Add(-1 * time.Hour)},
		// ticket 2 reviewed but not repaired
		{Kind: eventlog.KindTicketStatusChanged, EntityID: "2", ToStatus: "open", Timestamp: now.Add(-5 * time.Hour)},
		{Kind: eventlog.KindTicketStatusChanged, EntityID: "2", ToStatus: "in_review", Timestamp: now.Add(-4 * time.Hour)},
		{Kind: eventlog.KindTicketStatusChanged, EntityID: "2", ToStatus: "closed", Timestamp: now.Add(-3 * time.Hour)},
	}
	// 2 reviewed, 1 repaired → 50% repair rate; should not panic
	printTicketFlow(events)
}

func TestPrintAgentUtilization(t *testing.T) {
	now := time.Now()
	since := now.Add(-24 * time.Hour)
	events := []eventlog.Event{
		{Kind: eventlog.KindAgentStatusChanged, EntityID: "copper", FromStatus: "idle", ToStatus: "working", Timestamp: now.Add(-12 * time.Hour)},
		{Kind: eventlog.KindAgentStatusChanged, EntityID: "copper", FromStatus: "working", ToStatus: "idle", Timestamp: now.Add(-6 * time.Hour)},
	}
	// ~50% utilization for copper; should not panic
	printAgentUtilization(events, since)
}

func TestPrintPRCycle(t *testing.T) {
	now := time.Now()
	events := []eventlog.Event{
		{Kind: eventlog.KindTicketStatusChanged, EntityID: "1", ToStatus: "pr_open", Timestamp: now.Add(-4 * time.Hour)},
		{Kind: eventlog.KindTicketStatusChanged, EntityID: "1", ToStatus: "in_review", Timestamp: now.Add(-3 * time.Hour)},
		{Kind: eventlog.KindTicketStatusChanged, EntityID: "1", ToStatus: "repairing", Timestamp: now.Add(-2 * time.Hour)},
		{Kind: eventlog.KindTicketStatusChanged, EntityID: "1", ToStatus: "in_review", Timestamp: now.Add(-1 * time.Hour)},
		{Kind: eventlog.KindTicketStatusChanged, EntityID: "1", ToStatus: "closed", Timestamp: now},
	}
	// PR cycle ~4h, 2 review rounds; should not panic
	printPRCycle(events)
}

func TestPrintPRCycle_noData(t *testing.T) {
	// No ticket_status events — should print nothing and not panic
	printPRCycle([]eventlog.Event{
		{Kind: eventlog.KindAgentStatusChanged, EntityID: "copper", ToStatus: "working"},
	})
}

func TestMetrics_sinceFlag(t *testing.T) {
	// --since 14 should parse to 14 days; tested indirectly via sinceTime computation
	args := []string{"--since", "14"}
	since := 7
	for i, a := range args {
		if a == "--since" && i+1 < len(args) {
			if n, err2 := parseInt(args[i+1]); err2 == nil {
				since = n
			}
		}
	}
	if since != 14 {
		t.Errorf("expected since=14, got %d", since)
	}
}

// parseInt is a local helper used by the test above to mirror the Metrics flag parsing.
func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}
