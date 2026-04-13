package daemon

import (
	"io"
	"log"
	"testing"
	"time"

	"github.com/katerina7479/company_town/internal/assign"
	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

// newAssignmentDaemon creates a minimal daemon for handleAssignments tests.
// It stubs assign.ProleCreator so no real worktrees or sessions are created.
func newAssignmentDaemon(t *testing.T) (*Daemon, *repo.IssueRepo, *repo.AgentRepo) {
	t.Helper()
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	cfg := &config.Config{
		TicketPrefix: "NC",
		ProjectRoot:  t.TempDir(),
		MaxProles:    3,
	}
	issues := repo.NewIssueRepo(conn, nil)
	agents := repo.NewAgentRepo(conn, nil)

	// Stub ProleCreator so tests do not spawn real sessions or worktrees.
	origProleCreator := assign.ProleCreator
	t.Cleanup(func() { assign.ProleCreator = origProleCreator })
	assign.ProleCreator = func(name string, cfg *config.Config, ar *repo.AgentRepo) error {
		return ar.Register(name, "prole", nil)
	}

	d := &Daemon{
		cfg:             cfg,
		issues:          issues,
		agents:          agents,
		logger:          log.New(io.Discard, "", 0),
		stop:            make(chan struct{}),
		sessionExists:   func(string) bool { return false },
		sendKeys:        func(string, string) error { return nil },
		lastNudged:      make(map[string]time.Time),
		lastNudgeDigest: make(map[string]string),
		lastRestartedAt: make(map[string]time.Time),
		nowFn:           time.Now,
	}
	return d, issues, agents
}

// mustOpen creates and opens a ticket, then sets its status to "open".
func mustOpen(t *testing.T, issues *repo.IssueRepo, title string) int {
	t.Helper()
	id, err := issues.Create(title, "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := issues.UpdateStatus(id, "open"); err != nil {
		t.Fatalf("UpdateStatus open: %v", err)
	}
	return id
}

func TestHandleAssignments_noCandidatesNoOp(t *testing.T) {
	d, _, _ := newAssignmentDaemon(t)
	// No tickets — should not panic or error.
	d.handleAssignments()
}

func TestHandleAssignments_oneCandidateOneIdleProle(t *testing.T) {
	d, issues, agents := newAssignmentDaemon(t)

	id := mustOpen(t, issues, "Implement auth")

	// Register an idle prole.
	if err := agents.Register("copper", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}

	d.handleAssignments()

	issue, err := issues.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !issue.Assignee.Valid || issue.Assignee.String != "copper" {
		t.Errorf("expected ticket assigned to copper, got assignee=%v", issue.Assignee)
	}
	// NC-44: Assign preserves ticket status — the ticket stays "open" until
	// the prole itself promotes it to "in_progress" when it picks up the work.
	if issue.Status != "open" {
		t.Errorf("expected status=open (preserved by NC-44), got %q", issue.Status)
	}
}

func TestHandleAssignments_threeCandidatesOneIdlePlusTwoHeadroom(t *testing.T) {
	d, issues, agents := newAssignmentDaemon(t)
	// max_proles=3, no existing proles → all 3 slots are available (copper, iron, tin)

	id1 := mustOpen(t, issues, "Feature A")
	id2 := mustOpen(t, issues, "Feature B")
	id3 := mustOpen(t, issues, "Feature C")

	// One idle prole already registered.
	if err := agents.Register("copper", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}

	d.handleAssignments()

	for _, id := range []int{id1, id2, id3} {
		issue, err := issues.Get(id)
		if err != nil {
			t.Fatalf("Get %d: %v", id, err)
		}
		if !issue.Assignee.Valid || issue.Assignee.String == "" {
			t.Errorf("ticket %d not assigned", id)
		}
	}
}

func TestHandleAssignments_moreCandidatesThanSlots(t *testing.T) {
	d, issues, _ := newAssignmentDaemon(t)
	d.cfg.MaxProles = 1 // only 1 slot total

	id1 := mustOpen(t, issues, "Feature A")
	id2 := mustOpen(t, issues, "Feature B")

	d.handleAssignments()

	// First candidate gets the slot; second stays unassigned.
	issue1, _ := issues.Get(id1)
	if !issue1.Assignee.Valid || issue1.Assignee.String == "" {
		t.Errorf("expected ticket %d assigned, got assignee=%v", id1, issue1.Assignee)
	}

	issue2, _ := issues.Get(id2)
	if issue2.Assignee.Valid && issue2.Assignee.String != "" {
		t.Errorf("expected ticket %d unassigned, got assignee=%q", id2, issue2.Assignee.String)
	}
}

func TestHandleAssignments_maxProlesZeroNoIdleProlesNoOp(t *testing.T) {
	d, issues, _ := newAssignmentDaemon(t)
	d.cfg.MaxProles = 0 // headroom expansion disabled; no idle proles exist

	mustOpen(t, issues, "Feature A")

	d.handleAssignments()

	// All issues should remain unassigned.
	all, err := issues.List("")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	for _, i := range all {
		if i.Assignee.Valid && i.Assignee.String != "" {
			t.Errorf("expected ticket %d unassigned, got %q", i.ID, i.Assignee.String)
		}
	}
}

func TestHandleAssignments_repairingTicketsAssignedFirst(t *testing.T) {
	d, issues, _ := newAssignmentDaemon(t)
	d.cfg.MaxProles = 1 // only 1 slot

	// Open ticket first (lower ID).
	mustOpen(t, issues, "Open feature")

	// Repairing ticket (higher ID but should be assigned first).
	idRepairing, err := issues.Create("Fix bug", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := issues.UpdateStatus(idRepairing, "repairing"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	d.handleAssignments()

	repairingIssue, _ := issues.Get(idRepairing)
	if !repairingIssue.Assignee.Valid || repairingIssue.Assignee.String == "" {
		t.Errorf("expected repairing ticket assigned first, got assignee=%v", repairingIssue.Assignee)
	}
}

// TestHandleAssignments_busyProleSkipped verifies that a prole that already
// holds an active ticket is not given a second one on the next tick, even when
// it still reports as idle in the agents table (the known race condition).
func TestHandleAssignments_busyProleSkipped(t *testing.T) {
	d, issues, agents := newAssignmentDaemon(t)
	d.cfg.MaxProles = 1 // only one slot — copper

	id1 := mustOpen(t, issues, "Feature A")

	if err := agents.Register("copper", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}

	// First tick: copper gets ticket id1.
	d.handleAssignments()

	issue1, err := issues.Get(id1)
	if err != nil {
		t.Fatalf("Get id1: %v", err)
	}
	if !issue1.Assignee.Valid || issue1.Assignee.String != "copper" {
		t.Fatalf("expected id1 assigned to copper, got %v", issue1.Assignee)
	}

	// Copper is still idle in the agents table (prole hasn't picked up yet).
	// A second ticket appears.
	id2 := mustOpen(t, issues, "Feature B")

	// Second tick: copper must NOT receive id2.
	d.handleAssignments()

	issue2, err := issues.Get(id2)
	if err != nil {
		t.Fatalf("Get id2: %v", err)
	}
	if issue2.Assignee.Valid && issue2.Assignee.String != "" {
		t.Errorf("expected id2 unassigned (copper is busy), got assignee=%q", issue2.Assignee.String)
	}
}

func TestHandleAssignments_assignExecuteErrorContinues(t *testing.T) {
	d, issues, agents := newAssignmentDaemon(t)
	d.cfg.MaxProles = 2

	id1 := mustOpen(t, issues, "Feature A")
	id2 := mustOpen(t, issues, "Feature B")

	// Pre-register one idle prole so we have a known first slot.
	if err := agents.Register("copper", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}

	d.handleAssignments()

	// Both tickets should be assigned (2 slots for 2 tickets).
	issue1, _ := issues.Get(id1)
	issue2, _ := issues.Get(id2)

	if !issue1.Assignee.Valid || issue1.Assignee.String == "" {
		t.Errorf("expected ticket %d assigned, got assignee=%v", id1, issue1.Assignee)
	}
	if !issue2.Assignee.Valid || issue2.Assignee.String == "" {
		t.Errorf("expected ticket %d assigned, got assignee=%v", id2, issue2.Assignee)
	}
}
