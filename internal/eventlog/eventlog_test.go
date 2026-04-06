package eventlog_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/katerina7479/company_town/internal/eventlog"
)

// logPath returns a temp file path for a test log.
func logPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "events.jsonl")
}

func TestWriteAndReadAll(t *testing.T) {
	path := logPath(t)
	w := eventlog.NewWriter(path)

	events := []eventlog.Event{
		{
			Kind:       eventlog.KindTicketStatusChanged,
			EntityID:   "nc-16",
			EntityName: "Event log package",
			FromStatus: "open",
			ToStatus:   "in_progress",
		},
		{
			Kind:       eventlog.KindAgentStatusChanged,
			EntityID:   "copper",
			EntityName: "copper",
			FromStatus: "idle",
			ToStatus:   "working",
		},
	}

	for _, e := range events {
		if err := w.Write(e); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}

	r := eventlog.NewReader(path)
	got, err := r.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != len(events) {
		t.Fatalf("got %d events, want %d", len(got), len(events))
	}
	for i, e := range got {
		if e.Kind != events[i].Kind {
			t.Errorf("[%d] Kind: got %q, want %q", i, e.Kind, events[i].Kind)
		}
		if e.EntityID != events[i].EntityID {
			t.Errorf("[%d] EntityID: got %q, want %q", i, e.EntityID, events[i].EntityID)
		}
		if e.FromStatus != events[i].FromStatus {
			t.Errorf("[%d] FromStatus: got %q, want %q", i, e.FromStatus, events[i].FromStatus)
		}
		if e.ToStatus != events[i].ToStatus {
			t.Errorf("[%d] ToStatus: got %q, want %q", i, e.ToStatus, events[i].ToStatus)
		}
		if e.Timestamp.IsZero() {
			t.Errorf("[%d] Timestamp should not be zero", i)
		}
	}
}

func TestReadAll_FileNotExist(t *testing.T) {
	r := eventlog.NewReader("/nonexistent/path/events.jsonl")
	events, err := r.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll on missing file should not error, got: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("expected empty slice, got %d events", len(events))
	}
}

func TestReadFiltered_ByKind(t *testing.T) {
	path := logPath(t)
	w := eventlog.NewWriter(path)

	_ = w.Write(eventlog.Event{Kind: eventlog.KindTicketStatusChanged, EntityID: "nc-1", ToStatus: "open"})
	_ = w.Write(eventlog.Event{Kind: eventlog.KindAgentStatusChanged, EntityID: "copper", ToStatus: "working"})
	_ = w.Write(eventlog.Event{Kind: eventlog.KindTicketStatusChanged, EntityID: "nc-2", ToStatus: "in_progress"})

	r := eventlog.NewReader(path)
	got, err := r.ReadFiltered(eventlog.Filter{Kind: eventlog.KindTicketStatusChanged})
	if err != nil {
		t.Fatalf("ReadFiltered: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2", len(got))
	}
	for _, e := range got {
		if e.Kind != eventlog.KindTicketStatusChanged {
			t.Errorf("unexpected kind %q", e.Kind)
		}
	}
}

func TestReadFiltered_ByEntityID(t *testing.T) {
	path := logPath(t)
	w := eventlog.NewWriter(path)

	_ = w.Write(eventlog.Event{Kind: eventlog.KindTicketStatusChanged, EntityID: "nc-1", ToStatus: "open"})
	_ = w.Write(eventlog.Event{Kind: eventlog.KindTicketStatusChanged, EntityID: "nc-2", ToStatus: "open"})
	_ = w.Write(eventlog.Event{Kind: eventlog.KindTicketStatusChanged, EntityID: "nc-1", ToStatus: "in_progress"})

	r := eventlog.NewReader(path)
	got, err := r.ReadFiltered(eventlog.Filter{EntityID: "nc-1"})
	if err != nil {
		t.Fatalf("ReadFiltered: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d events for nc-1, want 2", len(got))
	}
}

func TestReadFiltered_BySince(t *testing.T) {
	path := logPath(t)
	w := eventlog.NewWriter(path)

	past := time.Now().UTC().Add(-2 * time.Hour)
	future := time.Now().UTC().Add(2 * time.Hour)
	cutoff := time.Now().UTC()

	_ = w.Write(eventlog.Event{Timestamp: past, Kind: eventlog.KindTicketStatusChanged, EntityID: "nc-1", ToStatus: "open"})
	_ = w.Write(eventlog.Event{Timestamp: future, Kind: eventlog.KindTicketStatusChanged, EntityID: "nc-2", ToStatus: "open"})

	r := eventlog.NewReader(path)
	got, err := r.ReadFiltered(eventlog.Filter{Since: cutoff})
	if err != nil {
		t.Fatalf("ReadFiltered: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events since cutoff, want 1", len(got))
	}
	if got[0].EntityID != "nc-2" {
		t.Errorf("expected nc-2, got %q", got[0].EntityID)
	}
}

func TestReadFiltered_ByUntil(t *testing.T) {
	path := logPath(t)
	w := eventlog.NewWriter(path)

	past := time.Now().UTC().Add(-2 * time.Hour)
	future := time.Now().UTC().Add(2 * time.Hour)
	cutoff := time.Now().UTC()

	_ = w.Write(eventlog.Event{Timestamp: past, Kind: eventlog.KindTicketStatusChanged, EntityID: "nc-1", ToStatus: "open"})
	_ = w.Write(eventlog.Event{Timestamp: future, Kind: eventlog.KindTicketStatusChanged, EntityID: "nc-2", ToStatus: "open"})

	r := eventlog.NewReader(path)
	got, err := r.ReadFiltered(eventlog.Filter{Until: cutoff})
	if err != nil {
		t.Fatalf("ReadFiltered: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d events until cutoff, want 1", len(got))
	}
	if got[0].EntityID != "nc-1" {
		t.Errorf("expected nc-1, got %q", got[0].EntityID)
	}
}

func TestWrite_CreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "dir", "events.jsonl")
	w := eventlog.NewWriter(path)

	if err := w.Write(eventlog.Event{Kind: eventlog.KindAgentStatusChanged, EntityID: "copper", ToStatus: "idle"}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Errorf("log file not created: %v", err)
	}
}

func TestWrite_TimestampAutoSet(t *testing.T) {
	path := logPath(t)
	w := eventlog.NewWriter(path)

	before := time.Now().UTC().Add(-time.Second)
	if err := w.Write(eventlog.Event{Kind: eventlog.KindAgentStatusChanged, EntityID: "copper", ToStatus: "idle"}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	after := time.Now().UTC().Add(time.Second)

	r := eventlog.NewReader(path)
	events, _ := r.ReadAll()
	if len(events) != 1 {
		t.Fatal("expected 1 event")
	}
	ts := events[0].Timestamp
	if ts.Before(before) || ts.After(after) {
		t.Errorf("timestamp %v outside expected range [%v, %v]", ts, before, after)
	}
}
