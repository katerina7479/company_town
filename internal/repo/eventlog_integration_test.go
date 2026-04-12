package repo

import (
	"path/filepath"
	"testing"

	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/eventlog"
)

func setupRepoWithLogger(t *testing.T) (*IssueRepo, *AgentRepo, *eventlog.Reader) {
	t.Helper()
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("creating test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	ctDir := t.TempDir()
	logger := eventlog.NewLogger(ctDir)
	logPath := filepath.Join(ctDir, "logs", "events.jsonl")
	issues := NewIssueRepo(conn, logger)
	agents := NewAgentRepo(conn, logger)
	reader := eventlog.NewReader(logPath)
	return issues, agents, reader
}

func TestIssueRepo_EmitsTicketCreated(t *testing.T) {
	issues, _, reader := setupRepoWithLogger(t)

	id, err := issues.Create("test ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	events, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event after Create, got %d", len(events))
	}
	e := events[0]
	if e.Kind != eventlog.KindTicketStatusChanged {
		t.Errorf("kind: got %q, want %q", e.Kind, eventlog.KindTicketStatusChanged)
	}
	if e.ToStatus != "open" {
		t.Errorf("to_status: got %q, want \"open\"", e.ToStatus)
	}
	if e.EntityName != "test ticket" {
		t.Errorf("entity_name: got %q, want \"test ticket\"", e.EntityName)
	}
	_ = id
}

func TestIssueRepo_EmitsTicketStatus(t *testing.T) {
	issues, _, reader := setupRepoWithLogger(t)

	id, err := issues.Create("status ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := issues.UpdateStatus(id, "in_progress"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	events, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	// Expect: 1 created + 1 status change = 2
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	e := events[1]
	if e.Kind != eventlog.KindTicketStatusChanged {
		t.Errorf("kind: got %q, want %q", e.Kind, eventlog.KindTicketStatusChanged)
	}
	if e.FromStatus != "draft" {
		t.Errorf("from_status: got %q, want \"draft\"", e.FromStatus)
	}
	if e.ToStatus != "in_progress" {
		t.Errorf("to_status: got %q, want \"in_progress\"", e.ToStatus)
	}
}

func TestIssueRepo_EmitsAssign(t *testing.T) {
	issues, _, reader := setupRepoWithLogger(t)

	id, err := issues.Create("assign ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := issues.Assign(id, "copper", "prole/copper/NC-17"); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	events, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	// 1 created + 1 assign transition
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	e := events[1]
	if e.ToStatus != "in_progress" {
		t.Errorf("assign event to_status: got %q, want \"in_progress\"", e.ToStatus)
	}
}

func TestIssueRepo_NoDoubleLog_Close(t *testing.T) {
	issues, _, reader := setupRepoWithLogger(t)

	id, err := issues.Create("close ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Close calls UpdateStatus internally — should emit exactly one event
	if err := issues.Close(id); err != nil {
		t.Fatalf("Close: %v", err)
	}

	events, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	// 1 created + 1 closed = 2 (not 3)
	if len(events) != 2 {
		t.Fatalf("expected 2 events (no double-log), got %d", len(events))
	}
	if events[1].ToStatus != "closed" {
		t.Errorf("last event to_status: got %q, want \"closed\"", events[1].ToStatus)
	}
}

func TestAgentRepo_EmitsRegistered(t *testing.T) {
	_, agents, reader := setupRepoWithLogger(t)

	if err := agents.Register("test-agent", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}

	events, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event after Register, got %d", len(events))
	}
	e := events[0]
	if e.Kind != eventlog.KindAgentStatusChanged {
		t.Errorf("kind: got %q, want %q", e.Kind, eventlog.KindAgentStatusChanged)
	}
	if e.ToStatus != "idle" {
		t.Errorf("to_status: got %q, want \"idle\"", e.ToStatus)
	}
	if e.EntityID != "test-agent" {
		t.Errorf("entity_id: got %q, want \"test-agent\"", e.EntityID)
	}
}

func TestAgentRepo_EmitsUpdateStatus(t *testing.T) {
	_, agents, reader := setupRepoWithLogger(t)

	agents.Register("my-agent", "prole", nil)

	if err := agents.UpdateStatus("my-agent", "working"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	events, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	// 1 registered + 1 status
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	e := events[1]
	if e.FromStatus != "idle" {
		t.Errorf("from_status: got %q, want \"idle\"", e.FromStatus)
	}
	if e.ToStatus != "working" {
		t.Errorf("to_status: got %q, want \"working\"", e.ToStatus)
	}
}

func TestAgentRepo_EmitsSetAndClearCurrentIssue(t *testing.T) {
	_, agents, reader := setupRepoWithLogger(t)

	agents.Register("issue-agent", "prole", nil)

	issueID := 42
	if err := agents.SetCurrentIssue("issue-agent", &issueID); err != nil {
		t.Fatalf("SetCurrentIssue: %v", err)
	}

	if err := agents.ClearCurrentIssue("issue-agent"); err != nil {
		t.Fatalf("ClearCurrentIssue: %v", err)
	}

	events, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	// 1 registered + 1 set (working) + 1 clear (idle) = 3
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	if events[1].ToStatus != "working" {
		t.Errorf("SetCurrentIssue event to_status: got %q, want \"working\"", events[1].ToStatus)
	}
	if events[2].ToStatus != "idle" {
		t.Errorf("ClearCurrentIssue event to_status: got %q, want \"idle\"", events[2].ToStatus)
	}
}
