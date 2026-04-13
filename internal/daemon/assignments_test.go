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
		resetWorktree:   func(string) error { return nil },
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
	if issue.Status != "open" {
		t.Errorf("expected status=open (NC-44: assign preserves status), got %q", issue.Status)
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

// NC-64: a prole that is already holding a non-closed ticket must not be
// handed a second ticket in the same (or any subsequent) tick until the first
// one is closed or cleared.
func TestHandleAssignments_skipsBusyIdleProle(t *testing.T) {
	d, issues, agents := newAssignmentDaemon(t)
	d.cfg.MaxProles = 0 // disable headroom so we test only the idle-slot path

	// Copper is idle per the agents table but already holds an open ticket.
	if err := agents.Register("copper", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	held, _ := issues.Create("Already held", "task", nil, nil, nil)
	if err := issues.UpdateStatus(held, "open"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if err := issues.Assign(held, "copper", "prole/copper/held"); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	// A fresh candidate appears.
	fresh := mustOpen(t, issues, "New work")

	d.handleAssignments()

	freshIssue, _ := issues.Get(fresh)
	if freshIssue.Assignee.Valid && freshIssue.Assignee.String != "" {
		t.Errorf("expected fresh ticket %d to stay unassigned (copper is busy), got assignee=%q",
			fresh, freshIssue.Assignee.String)
	}
}

func TestHandleAssignments_busyProleIgnoredOnlyOneSlotAvailable(t *testing.T) {
	d, issues, agents := newAssignmentDaemon(t)
	d.cfg.MaxProles = 0 // only the explicitly registered idle slots count

	// Two idle proles; copper is already busy, iron is free.
	if err := agents.Register("copper", "prole", nil); err != nil {
		t.Fatalf("Register copper: %v", err)
	}
	if err := agents.Register("iron", "prole", nil); err != nil {
		t.Fatalf("Register iron: %v", err)
	}
	held, _ := issues.Create("Copper holds", "task", nil, nil, nil)
	issues.UpdateStatus(held, "open")
	issues.Assign(held, "copper", "prole/copper/held")

	// Two fresh candidates. Only iron can take one; the other must stay free.
	c1 := mustOpen(t, issues, "Work A")
	c2 := mustOpen(t, issues, "Work B")

	d.handleAssignments()

	// Exactly one of the two candidates is assigned, and it must be to iron.
	i1, _ := issues.Get(c1)
	i2, _ := issues.Get(c2)
	var assigned, unassigned *repo.Issue
	switch {
	case i1.Assignee.Valid && !i2.Assignee.Valid:
		assigned, unassigned = i1, i2
	case i2.Assignee.Valid && !i1.Assignee.Valid:
		assigned, unassigned = i2, i1
	default:
		t.Fatalf("expected exactly one candidate assigned, got c1=%v c2=%v",
			i1.Assignee, i2.Assignee)
	}
	if assigned.Assignee.String != "iron" {
		t.Errorf("expected assignment to iron, got %q", assigned.Assignee.String)
	}
	if unassigned.Assignee.Valid && unassigned.Assignee.String != "" {
		t.Errorf("expected second candidate unassigned, got %q", unassigned.Assignee.String)
	}
}

// Belt-and-braces: a net-new metal name that (somehow) already appears as a
// busy assignee in the issues table must not be handed a second ticket.
func TestHandleAssignments_busyFilterAppliesToMetalNames(t *testing.T) {
	d, issues, _ := newAssignmentDaemon(t)
	d.cfg.MaxProles = 1 // one headroom slot — first metal name is "copper"

	// Pre-seed: copper holds a ticket but is not registered as an agent yet.
	held, _ := issues.Create("Held by phantom copper", "task", nil, nil, nil)
	issues.UpdateStatus(held, "open")
	issues.Assign(held, "copper", "prole/copper/held")

	fresh := mustOpen(t, issues, "Should not reach copper")

	d.handleAssignments()

	freshIssue, _ := issues.Get(fresh)
	if freshIssue.Assignee.Valid && freshIssue.Assignee.String == "copper" {
		t.Errorf("expected busy filter to skip metal name 'copper', but got assigned to copper")
	}
}

// Replays the 2026-04-12 incident shape: two ticks in a row against the same
// slot state. The second tick must not compound assignments onto an idle-in-
// agents-table prole that already received work on tick 1.
func TestHandleAssignments_secondTickDoesNotCompound(t *testing.T) {
	d, issues, agents := newAssignmentDaemon(t)
	d.cfg.MaxProles = 0

	if err := agents.Register("copper", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}

	c1 := mustOpen(t, issues, "Tick 1 work")
	c2 := mustOpen(t, issues, "Tick 2 candidate")

	// Tick 1: copper picks up c1.
	d.handleAssignments()
	i1, _ := issues.Get(c1)
	if !i1.Assignee.Valid || i1.Assignee.String != "copper" {
		t.Fatalf("tick 1: expected c1 assigned to copper, got %v", i1.Assignee)
	}

	// Tick 2: copper is still idle in the agents table (hasn't acked yet),
	// but BusyAssignees should include it. c2 must remain unassigned.
	d.handleAssignments()
	i2, _ := issues.Get(c2)
	if i2.Assignee.Valid && i2.Assignee.String != "" {
		t.Errorf("tick 2: expected c2 to remain unassigned (copper busy from tick 1), got assignee=%q",
			i2.Assignee.String)
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
