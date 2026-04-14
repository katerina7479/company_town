package assign

import (
	"errors"
	"testing"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

func setupRepos(t *testing.T) (*repo.IssueRepo, *repo.AgentRepo) {
	t.Helper()
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("creating test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return repo.NewIssueRepo(conn, nil), repo.NewAgentRepo(conn, nil)
}

func TestExecute_existingProle(t *testing.T) {
	issues, agents := setupRepos(t)

	// Pre-register the prole agent.
	if err := agents.Register("copper", "prole", nil); err != nil {
		t.Fatalf("registering agent: %v", err)
	}

	// Create a ticket to assign.
	ticketID, err := issues.Create("test ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}

	// ProleCreator must NOT be called when the prole already exists.
	orig := ProleCreator
	t.Cleanup(func() { ProleCreator = orig })
	ProleCreator = func(name string, cfg *config.Config, a *repo.AgentRepo) error {
		t.Errorf("ProleCreator called unexpectedly for existing prole %q", name)
		return nil
	}

	cfg := &config.Config{TicketPrefix: "nc"}
	if err := Execute(cfg, issues, agents, ticketID, "copper"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Assert issue has correct assignee and branch.
	issue, err := issues.Get(ticketID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if !issue.Assignee.Valid || issue.Assignee.String != "copper" {
		t.Errorf("expected assignee=copper, got %v", issue.Assignee)
	}
	wantBranch := "prole/copper/nc-1"
	if !issue.Branch.Valid || issue.Branch.String != wantBranch {
		t.Errorf("expected branch=%q, got %v", wantBranch, issue.Branch)
	}

	// Agent status and current_issue are left alone — proles own their own status.
	agent, err := agents.Get("copper")
	if err != nil {
		t.Fatalf("getting agent: %v", err)
	}
	if agent.Status != "idle" {
		t.Errorf("expected status unchanged (idle), got %q", agent.Status)
	}
	if agent.CurrentIssue.Valid {
		t.Errorf("expected current_issue unchanged (NULL), got %d", agent.CurrentIssue.Int64)
	}
}

func TestExecute_newProle(t *testing.T) {
	issues, agents := setupRepos(t)

	ticketID, err := issues.Create("test ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}

	var creatorCalls int
	orig := ProleCreator
	t.Cleanup(func() { ProleCreator = orig })
	ProleCreator = func(name string, cfg *config.Config, a *repo.AgentRepo) error {
		creatorCalls++
		// Simulate what prole.Create does: register the agent.
		return a.Register(name, "prole", nil)
	}

	cfg := &config.Config{TicketPrefix: "nc"}
	if err := Execute(cfg, issues, agents, ticketID, "zinc"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if creatorCalls != 1 {
		t.Errorf("expected ProleCreator called exactly once, got %d", creatorCalls)
	}

	issue, err := issues.Get(ticketID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if !issue.Assignee.Valid || issue.Assignee.String != "zinc" {
		t.Errorf("expected assignee=zinc, got %v", issue.Assignee)
	}

	// Agent status and current_issue are left alone — proles own their own status.
	agent, err := agents.Get("zinc")
	if err != nil {
		t.Fatalf("getting agent: %v", err)
	}
	if agent.Status != "idle" {
		t.Errorf("expected status unchanged (idle), got %q", agent.Status)
	}
	if agent.CurrentIssue.Valid {
		t.Errorf("expected current_issue unchanged (NULL), got %d", agent.CurrentIssue.Int64)
	}
}

func TestExecute_preservesBranchOnReassignment(t *testing.T) {
	// Re-assigning a ticket that already has a branch (e.g. after reviewer
	// sends it back for repairs) must leave the branch column untouched so
	// the existing PR continues to track incoming commits.
	issues, agents := setupRepos(t)

	if err := agents.Register("iron", "prole", nil); err != nil {
		t.Fatalf("registering iron: %v", err)
	}
	if err := agents.Register("tin", "prole", nil); err != nil {
		t.Fatalf("registering tin: %v", err)
	}

	ticketID, err := issues.Create("test ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}

	// First assignment: iron picks up the ticket.
	cfg := &config.Config{TicketPrefix: "nc"}
	if err := Execute(cfg, issues, agents, ticketID, "iron"); err != nil {
		t.Fatalf("first Execute (iron): %v", err)
	}

	issue, err := issues.Get(ticketID)
	if err != nil {
		t.Fatalf("Get after first assign: %v", err)
	}
	firstBranch := issue.Branch.String // "prole/iron/1"

	// Re-assignment: ticket sent back (repairing), now assigned to tin.
	if err := Execute(cfg, issues, agents, ticketID, "tin"); err != nil {
		t.Fatalf("second Execute (tin): %v", err)
	}

	issue, err = issues.Get(ticketID)
	if err != nil {
		t.Fatalf("Get after second assign: %v", err)
	}

	// Assignee must be updated to the new prole.
	if !issue.Assignee.Valid || issue.Assignee.String != "tin" {
		t.Errorf("expected assignee=tin, got %v", issue.Assignee)
	}
	// Branch must be preserved — not overwritten with "prole/tin/1".
	if !issue.Branch.Valid || issue.Branch.String != firstBranch {
		t.Errorf("expected branch=%q (preserved), got %v", firstBranch, issue.Branch)
	}
}

func TestExecute_reassignmentToSameProleNoop(t *testing.T) {
	// Assigning a ticket to the same prole twice should be idempotent: the
	// branch must be generated once and left unchanged on the second call.
	issues, agents := setupRepos(t)

	if err := agents.Register("iron", "prole", nil); err != nil {
		t.Fatalf("registering iron: %v", err)
	}

	ticketID, err := issues.Create("test ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}

	cfg := &config.Config{TicketPrefix: "nc"}

	// First assignment.
	if err := Execute(cfg, issues, agents, ticketID, "iron"); err != nil {
		t.Fatalf("first Execute: %v", err)
	}
	issue1, err := issues.Get(ticketID)
	if err != nil {
		t.Fatalf("Get after first assign: %v", err)
	}
	firstBranch := issue1.Branch.String

	// Second assignment to the same prole.
	if err := Execute(cfg, issues, agents, ticketID, "iron"); err != nil {
		t.Fatalf("second Execute: %v", err)
	}
	issue2, err := issues.Get(ticketID)
	if err != nil {
		t.Fatalf("Get after second assign: %v", err)
	}

	if issue2.Branch.String != firstBranch {
		t.Errorf("branch changed on idempotent reassign: first=%q second=%q", firstBranch, issue2.Branch.String)
	}
	if !issue2.Assignee.Valid || issue2.Assignee.String != "iron" {
		t.Errorf("expected assignee=iron, got %v", issue2.Assignee)
	}
}

func TestExecute_emptyStringBranchTreatedAsUnset(t *testing.T) {
	// A branch column value of "" (non-NULL, but empty) must be treated the
	// same as NULL and overwritten with a freshly-generated branch name.
	// This defends the explicit `ticket.Branch.String != ""` guard in assign.go.
	issues, agents := setupRepos(t)

	if err := agents.Register("copper", "prole", nil); err != nil {
		t.Fatalf("registering copper: %v", err)
	}

	ticketID, err := issues.Create("test ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}

	// Seed the ticket with an empty-string branch (Valid=true, String="").
	if err := issues.Assign(ticketID, "copper", ""); err != nil {
		t.Fatalf("seeding empty branch: %v", err)
	}
	seeded, err := issues.Get(ticketID)
	if err != nil {
		t.Fatalf("Get after seed: %v", err)
	}
	if !seeded.Branch.Valid || seeded.Branch.String != "" {
		t.Fatalf("seed did not produce empty-string branch: %v", seeded.Branch)
	}

	cfg := &config.Config{TicketPrefix: "nc"}
	if err := Execute(cfg, issues, agents, ticketID, "copper"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	issue, err := issues.Get(ticketID)
	if err != nil {
		t.Fatalf("Get after Execute: %v", err)
	}

	wantBranch := config.ProleBranchName("nc", "copper", ticketID)
	if !issue.Branch.Valid || issue.Branch.String != wantBranch {
		t.Errorf("expected branch=%q, got %v", wantBranch, issue.Branch)
	}
}

func TestExecute_repairWorktreeSwitched(t *testing.T) {
	// When a repair ticket with a pre-existing branch is assigned to a new prole
	// whose worktree path is set, WorktreeSwitcher must be called with the
	// existing branch so the prole starts on the right commits.
	issues, agents := setupRepos(t)

	// Register copper (original prole) and iron (new prole picking up repair).
	if err := agents.Register("copper", "prole", nil); err != nil {
		t.Fatalf("registering copper: %v", err)
	}
	if err := agents.Register("iron", "prole", nil); err != nil {
		t.Fatalf("registering iron: %v", err)
	}
	// Give iron a worktree path so WorktreeSwitcher is triggered.
	if err := agents.SetWorktree("iron", "/fake/worktree/iron"); err != nil {
		t.Fatalf("SetWorktree iron: %v", err)
	}

	ticketID, err := issues.Create("repair ticket", "bug", nil, nil, nil)
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}

	// First assignment: copper sets up the branch.
	cfg := &config.Config{TicketPrefix: "nc"}
	if err := Execute(cfg, issues, agents, ticketID, "copper"); err != nil {
		t.Fatalf("first Execute (copper): %v", err)
	}
	ticket, _ := issues.Get(ticketID)
	existingBranch := ticket.Branch.String // "prole/copper/nc-1"

	// Stub WorktreeSwitcher to capture calls.
	var switcherCalls []struct{ wtPath, barePath, branch string }
	orig := WorktreeSwitcher
	t.Cleanup(func() { WorktreeSwitcher = orig })
	WorktreeSwitcher = func(wtPath, barePath, branch string) error {
		switcherCalls = append(switcherCalls, struct{ wtPath, barePath, branch string }{wtPath, barePath, branch})
		return nil
	}

	// Second assignment: iron picks up the repair.
	if err := Execute(cfg, issues, agents, ticketID, "iron"); err != nil {
		t.Fatalf("second Execute (iron): %v", err)
	}

	// Branch must still be copper's original branch.
	repaired, _ := issues.Get(ticketID)
	if repaired.Branch.String != existingBranch {
		t.Errorf("branch changed: got %q, want %q", repaired.Branch.String, existingBranch)
	}
	if !repaired.Assignee.Valid || repaired.Assignee.String != "iron" {
		t.Errorf("expected assignee=iron, got %v", repaired.Assignee)
	}

	// WorktreeSwitcher must have been called with the existing branch.
	if len(switcherCalls) != 1 {
		t.Fatalf("expected WorktreeSwitcher called once, got %d", len(switcherCalls))
	}
	if switcherCalls[0].branch != existingBranch {
		t.Errorf("WorktreeSwitcher called with branch %q, want %q", switcherCalls[0].branch, existingBranch)
	}
	if switcherCalls[0].wtPath != "/fake/worktree/iron" {
		t.Errorf("WorktreeSwitcher called with wtPath %q, want /fake/worktree/iron", switcherCalls[0].wtPath)
	}
}

func TestExecute_freshTicketNoWorktreeSwitch(t *testing.T) {
	// A brand-new ticket (no existing branch) must NOT trigger WorktreeSwitcher.
	issues, agents := setupRepos(t)

	if err := agents.Register("copper", "prole", nil); err != nil {
		t.Fatalf("registering copper: %v", err)
	}
	if err := agents.SetWorktree("copper", "/fake/worktree/copper"); err != nil {
		t.Fatalf("SetWorktree copper: %v", err)
	}

	ticketID, err := issues.Create("fresh ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}

	var switcherCalled bool
	orig := WorktreeSwitcher
	t.Cleanup(func() { WorktreeSwitcher = orig })
	WorktreeSwitcher = func(wtPath, barePath, branch string) error {
		switcherCalled = true
		return nil
	}

	cfg := &config.Config{TicketPrefix: "nc"}
	if err := Execute(cfg, issues, agents, ticketID, "copper"); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if switcherCalled {
		t.Error("WorktreeSwitcher must not be called for fresh tickets with no existing branch")
	}
}

func TestExecute_proleCreateFails(t *testing.T) {
	issues, agents := setupRepos(t)

	ticketID, err := issues.Create("test ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("creating issue: %v", err)
	}

	orig := ProleCreator
	t.Cleanup(func() { ProleCreator = orig })
	ProleCreator = func(name string, cfg *config.Config, a *repo.AgentRepo) error {
		return errors.New("simulated prole create failure")
	}

	cfg := &config.Config{TicketPrefix: "nc"}
	err = Execute(cfg, issues, agents, ticketID, "zinc")
	if err == nil {
		t.Fatal("expected error when ProleCreator fails, got nil")
	}

	// issues.Assign must not have been called — issue should have no assignee.
	issue, err := issues.Get(ticketID)
	if err != nil {
		t.Fatalf("getting issue: %v", err)
	}
	if issue.Assignee.Valid {
		t.Errorf("expected no assignee after ProleCreator failure, got %q", issue.Assignee.String)
	}
}
