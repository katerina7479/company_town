// Package eventlog provides a JSONL-based append-only event log for tracking
// status transitions of tickets and agents. Each event is written as a single
// JSON line. The Reader supports full and filtered reads for metrics queries.
package eventlog

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Kind identifies the type of event.
type Kind string

const (
	KindTicketStatusChanged Kind = "ticket_status_changed"
	KindAgentStatusChanged  Kind = "agent_status_changed"
)

// Event represents a single state-transition event.
type Event struct {
	Timestamp  time.Time `json:"timestamp"`
	Kind       Kind      `json:"kind"`
	EntityID   string    `json:"entity_id"`             // e.g. "nc-16" or "copper"
	EntityName string    `json:"entity_name,omitempty"` // human-readable label
	FromStatus string    `json:"from_status,omitempty"` // empty if status was unknown
	ToStatus   string    `json:"to_status"`
}

// Writer appends events to a JSONL file. It is safe for concurrent use.
type Writer struct {
	mu   sync.Mutex
	path string
}

// NewWriter creates a Writer that appends to path. The file and any missing
// parent directories are created on the first Write call.
func NewWriter(path string) *Writer {
	return &Writer{path: path}
}

// Write serialises e as a single JSON line and appends it to the log file.
func (w *Writer) Write(e Event) error {
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now().UTC()
	}

	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("eventlog: marshal event: %w", err)
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(w.path), 0755); err != nil {
		return fmt.Errorf("eventlog: create log dir: %w", err)
	}

	f, err := os.OpenFile(w.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("eventlog: open log file: %w", err)
	}
	defer f.Close()

	if _, err := fmt.Fprintf(f, "%s\n", line); err != nil {
		return fmt.Errorf("eventlog: write event: %w", err)
	}
	return nil
}

// Reader reads events from a JSONL log file.
type Reader struct {
	path string
}

// NewReader creates a Reader for path.
func NewReader(path string) *Reader {
	return &Reader{path: path}
}

// Filter constrains which events ReadFiltered returns. Zero values mean "no constraint".
type Filter struct {
	Kind     Kind
	EntityID string
	Since    time.Time
	Until    time.Time
}

// ReadAll reads every event from the log file. If the file does not exist, an
// empty slice is returned without error.
func (r *Reader) ReadAll() ([]Event, error) {
	return r.ReadFiltered(Filter{})
}

// ReadFiltered reads events matching f. Fields with zero values are ignored.
// If the file does not exist, an empty slice is returned without error.
func (r *Reader) ReadFiltered(f Filter) ([]Event, error) {
	file, err := os.Open(r.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("eventlog: open log file: %w", err)
	}
	defer file.Close()

	var events []Event
	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			return nil, fmt.Errorf("eventlog: parse line %d: %w", lineNum, err)
		}

		if f.Kind != "" && e.Kind != f.Kind {
			continue
		}
		if f.EntityID != "" && e.EntityID != f.EntityID {
			continue
		}
		if !f.Since.IsZero() && e.Timestamp.Before(f.Since) {
			continue
		}
		if !f.Until.IsZero() && e.Timestamp.After(f.Until) {
			continue
		}

		events = append(events, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("eventlog: scan log file: %w", err)
	}
	return events, nil
}
