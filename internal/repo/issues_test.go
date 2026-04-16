package repo

import (
	"testing"

	"github.com/katerina7479/company_town/internal/db"
)

func setupTestRepo(t *testing.T) *IssueRepo {
	t.Helper()
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("creating test db: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return NewIssueRepo(conn, nil)
}

func TestIssueRepo_Create(t *testing.T) {
	repo := setupTestRepo(t)

	id, err := repo.Create("Test ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if id != 1 {
		t.Errorf("expected id=1, got %d", id)
	}

	issue, err := repo.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if issue.Title != "Test ticket" {
		t.Errorf("expected title='Test ticket', got %q", issue.Title)
	}
	if issue.Status != "draft" {
		t.Errorf("expected status='draft', got %q", issue.Status)
	}
}

func TestIssueRepo_UpdateStatus(t *testing.T) {
	repo := setupTestRepo(t)

	id, _ := repo.Create("Test ticket", "task", nil, nil, nil)

	if err := repo.UpdateStatus(id, "open"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	issue, _ := repo.Get(id)
	if issue.Status != "open" {
		t.Errorf("expected status='open', got %q", issue.Status)
	}
}

func TestIssueRepo_Dependencies(t *testing.T) {
	repo := setupTestRepo(t)

	id1, _ := repo.Create("Ticket 1", "task", nil, nil, nil)
	id2, _ := repo.Create("Ticket 2", "task", nil, nil, nil)

	if err := repo.AddDependency(id2, id1); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}

	deps, err := repo.GetDependencies(id2)
	if err != nil {
		t.Fatalf("GetDependencies: %v", err)
	}
	if len(deps) != 1 || deps[0] != id1 {
		t.Errorf("expected deps=[%d], got %v", id1, deps)
	}
}

func TestIssueRepo_RemoveDependency(t *testing.T) {
	repo := setupTestRepo(t)

	id1, _ := repo.Create("Ticket 1", "task", nil, nil, nil)
	id2, _ := repo.Create("Ticket 2", "task", nil, nil, nil)

	if err := repo.AddDependency(id2, id1); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}

	// Remove the edge
	if err := repo.RemoveDependency(id2, id1); err != nil {
		t.Fatalf("RemoveDependency: %v", err)
	}

	deps, err := repo.GetDependencies(id2)
	if err != nil {
		t.Fatalf("GetDependencies after remove: %v", err)
	}
	if len(deps) != 0 {
		t.Errorf("expected no deps after remove, got %v", deps)
	}
}

func TestIssueRepo_RemoveDependency_idempotent(t *testing.T) {
	repo := setupTestRepo(t)

	id1, _ := repo.Create("Ticket 1", "task", nil, nil, nil)
	id2, _ := repo.Create("Ticket 2", "task", nil, nil, nil)

	// Remove an edge that does not exist — should succeed silently
	if err := repo.RemoveDependency(id2, id1); err != nil {
		t.Errorf("RemoveDependency on non-existent edge should succeed, got %v", err)
	}
}

func TestIssueRepo_RemoveDependency_OnlyRemovesSpecifiedEdge(t *testing.T) {
	repo := setupTestRepo(t)

	a, _ := repo.Create("A", "task", nil, nil, nil)
	b, _ := repo.Create("B", "task", nil, nil, nil)
	c, _ := repo.Create("C", "task", nil, nil, nil)

	repo.AddDependency(a, b)
	repo.AddDependency(a, c)

	if err := repo.RemoveDependency(a, b); err != nil {
		t.Fatalf("RemoveDependency(a,b): %v", err)
	}

	deps, err := repo.GetDependencies(a)
	if err != nil {
		t.Fatalf("GetDependencies: %v", err)
	}
	if len(deps) != 1 || deps[0] != c {
		t.Errorf("expected deps=[%d], got %v", c, deps)
	}
}

func TestIssueRepo_RemoveDependency_restoresSelectability(t *testing.T) {
	r := setupTestRepo(t)

	id1, _ := r.Create("Blocker", "task", nil, nil, nil)
	id2, _ := r.Create("Blocked", "task", nil, nil, nil)
	r.UpdateStatus(id1, "open")
	r.UpdateStatus(id2, "open")

	if err := r.AddDependency(id2, id1); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}

	// id2 should not be selectable while id1 is open
	tickets, err := r.Selectable()
	if err != nil {
		t.Fatalf("Selectable: %v", err)
	}
	for _, tk := range tickets {
		if tk.ID == id2 {
			t.Error("id2 should not be selectable while dependency unresolved")
		}
	}

	// Remove dependency — id2 should now be selectable
	if err := r.RemoveDependency(id2, id1); err != nil {
		t.Fatalf("RemoveDependency: %v", err)
	}

	tickets, err = r.Selectable()
	if err != nil {
		t.Fatalf("Selectable after remove: %v", err)
	}
	found := false
	for _, tk := range tickets {
		if tk.ID == id2 {
			found = true
		}
	}
	if !found {
		t.Error("id2 should be selectable after dependency removed")
	}
}

func TestIssueRepo_SetBranch(t *testing.T) {
	r := setupTestRepo(t)
	id, _ := r.Create("Branch ticket", "task", nil, nil, nil)

	if err := r.SetBranch(id, "prole/tin/nc-42"); err != nil {
		t.Fatalf("SetBranch: %v", err)
	}

	got, err := r.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.Branch.Valid || got.Branch.String != "prole/tin/nc-42" {
		t.Errorf("Branch: got valid=%v value=%q, want valid=true value=%q",
			got.Branch.Valid, got.Branch.String, "prole/tin/nc-42")
	}
}

func TestIssueRepo_SetBranch_overwritesExisting(t *testing.T) {
	r := setupTestRepo(t)
	id, _ := r.Create("Branch ticket", "task", nil, nil, nil)

	r.SetBranch(id, "old-branch") //nolint:errcheck
	if err := r.SetBranch(id, "new-branch"); err != nil {
		t.Fatalf("SetBranch overwrite: %v", err)
	}

	got, _ := r.Get(id)
	if got.Branch.String != "new-branch" {
		t.Errorf("Branch: got %q, want %q", got.Branch.String, "new-branch")
	}
}

func TestIssueRepo_GetDependents_basic(t *testing.T) {
	r := setupTestRepo(t)

	// tdd: tests ticket (the blocker)
	tests, _ := r.Create("TDD tests", "tdd_tests", nil, nil, nil)
	// impl: implementation ticket that depends on tests
	impl, _ := r.Create("Implementation", "task", nil, nil, nil)

	r.AddDependency(impl, tests) //nolint:errcheck

	dependents, err := r.GetDependents(tests)
	if err != nil {
		t.Fatalf("GetDependents: %v", err)
	}
	if len(dependents) != 1 {
		t.Fatalf("expected 1 dependent, got %d", len(dependents))
	}
	if dependents[0].ID != impl {
		t.Errorf("expected dependent ID=%d, got %d", impl, dependents[0].ID)
	}
}

func TestIssueRepo_GetDependents_closedExcluded(t *testing.T) {
	r := setupTestRepo(t)

	tests, _ := r.Create("TDD tests", "tdd_tests", nil, nil, nil)
	impl, _ := r.Create("Implementation", "task", nil, nil, nil)
	other, _ := r.Create("Closed impl", "task", nil, nil, nil)

	r.AddDependency(impl, tests)  //nolint:errcheck
	r.AddDependency(other, tests) //nolint:errcheck
	r.UpdateStatus(other, "closed")

	dependents, err := r.GetDependents(tests)
	if err != nil {
		t.Fatalf("GetDependents: %v", err)
	}
	if len(dependents) != 1 {
		t.Fatalf("expected 1 open dependent (closed excluded), got %d", len(dependents))
	}
	if dependents[0].ID != impl {
		t.Errorf("expected dependent ID=%d, got %d", impl, dependents[0].ID)
	}
}

func TestIssueRepo_GetDependents_noDependents(t *testing.T) {
	r := setupTestRepo(t)

	id, _ := r.Create("Standalone", "task", nil, nil, nil)
	dependents, err := r.GetDependents(id)
	if err != nil {
		t.Fatalf("GetDependents: %v", err)
	}
	if len(dependents) != 0 {
		t.Errorf("expected 0 dependents, got %d", len(dependents))
	}
}

func TestIssueRepo_Ready(t *testing.T) {
	repo := setupTestRepo(t)

	// Create three tickets
	id1, _ := repo.Create("Ticket 1", "task", nil, nil, nil)
	id2, _ := repo.Create("Ticket 2", "task", nil, nil, nil)
	id3, _ := repo.Create("Ticket 3", "task", nil, nil, nil)

	// Set all to open
	repo.UpdateStatus(id1, "open")
	repo.UpdateStatus(id2, "open")
	repo.UpdateStatus(id3, "open")

	// id3 depends on id1
	repo.AddDependency(id3, id1)

	ready, err := repo.Ready()
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}

	// Should return id1 and id2 (id3 blocked by id1)
	if len(ready) != 2 {
		t.Errorf("expected 2 ready tickets, got %d", len(ready))
	}

	// Close id1
	repo.UpdateStatus(id1, "closed")

	ready, _ = repo.Ready()
	// Now should return id2 and id3
	if len(ready) != 2 {
		t.Errorf("expected 2 ready tickets after closing dep, got %d", len(ready))
	}
}

func TestIssueRepo_Ready_excludesEpics(t *testing.T) {
	repo := setupTestRepo(t)

	taskID, _ := repo.Create("A task", "task", nil, nil, nil)
	epicID, _ := repo.Create("An epic", "epic", nil, nil, nil)

	repo.UpdateStatus(taskID, "open")
	repo.UpdateStatus(epicID, "open")

	ready, err := repo.Ready()
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}

	if len(ready) != 1 {
		t.Fatalf("expected 1 ready ticket (epic excluded), got %d", len(ready))
	}
	if ready[0].ID != taskID {
		t.Errorf("expected task id=%d, got %d", taskID, ready[0].ID)
	}
	for _, r := range ready {
		if r.IssueType == "epic" {
			t.Errorf("Ready returned an epic (id=%d)", r.ID)
		}
	}
}

func TestIssueRepo_ListHierarchy(t *testing.T) {
	repo := setupTestRepo(t)

	// Create a parent (epic) and two children (tasks), plus an orphan root
	parentID, _ := repo.Create("Epic 1", "epic", nil, nil, nil)
	child1ID, _ := repo.Create("Task 1", "task", &parentID, nil, nil)
	child2ID, _ := repo.Create("Task 2", "task", &parentID, nil, nil)
	rootID, _ := repo.Create("Root Task", "task", nil, nil, nil)

	roots, err := repo.ListHierarchy()
	if err != nil {
		t.Fatalf("ListHierarchy: %v", err)
	}

	// Should have two roots: the epic and the root task
	if len(roots) != 2 {
		t.Fatalf("expected 2 root nodes, got %d", len(roots))
	}

	// First root is the epic (created first, id=parentID)
	epicNode := roots[0]
	if epicNode.ID != parentID {
		t.Errorf("expected first root id=%d, got %d", parentID, epicNode.ID)
	}
	if len(epicNode.Children) != 2 {
		t.Fatalf("expected 2 children on epic, got %d", len(epicNode.Children))
	}

	childIDs := []int{epicNode.Children[0].ID, epicNode.Children[1].ID}
	if childIDs[0] != child1ID || childIDs[1] != child2ID {
		t.Errorf("unexpected child ids: %v, want [%d %d]", childIDs, child1ID, child2ID)
	}

	// Second root is the standalone task
	if roots[1].ID != rootID {
		t.Errorf("expected second root id=%d, got %d", rootID, roots[1].ID)
	}
	if len(roots[1].Children) != 0 {
		t.Errorf("expected 0 children on root task, got %d", len(roots[1].Children))
	}
}

func TestIssueRepo_ListHierarchy_DanglingParent(t *testing.T) {
	repo := setupTestRepo(t)

	// Create a ticket with a parent_id that references a non-existent issue
	// by manually inserting via the db (simulate orphan)
	id, _ := repo.Create("Orphan", "task", nil, nil, nil)
	// Set parent_id to a non-existent id by updating directly
	repo.db.Exec(`UPDATE issues SET parent_id = 9999 WHERE id = ?`, id)

	roots, err := repo.ListHierarchy()
	if err != nil {
		t.Fatalf("ListHierarchy: %v", err)
	}

	// Orphan should be treated as a root
	if len(roots) != 1 || roots[0].ID != id {
		t.Errorf("expected orphan to be root, got %+v", roots)
	}
}

func TestIssueRepo_List(t *testing.T) {
	repo := setupTestRepo(t)

	repo.Create("Draft 1", "task", nil, nil, nil)
	repo.Create("Draft 2", "task", nil, nil, nil)
	id3, _ := repo.Create("Open 1", "task", nil, nil, nil)
	repo.UpdateStatus(id3, "open")

	// List all
	all, err := repo.List("")
	if err != nil {
		t.Fatalf("List all: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 tickets, got %d", len(all))
	}

	// List draft only
	drafts, err := repo.List("draft")
	if err != nil {
		t.Fatalf("List draft: %v", err)
	}
	if len(drafts) != 2 {
		t.Errorf("expected 2 draft tickets, got %d", len(drafts))
	}
}

func TestIssueRepo_Create_withParentAndSpecialty(t *testing.T) {
	repo := setupTestRepo(t)

	parentID, _ := repo.Create("Epic", "epic", nil, nil, nil)
	spec := "backend"
	childID, err := repo.Create("Child task", "task", &parentID, &spec, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	issue, err := repo.Get(childID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !issue.ParentID.Valid || int(issue.ParentID.Int64) != parentID {
		t.Errorf("expected parent_id=%d, got %v", parentID, issue.ParentID)
	}
	if !issue.Specialty.Valid || issue.Specialty.String != "backend" {
		t.Errorf("expected specialty='backend', got %v", issue.Specialty)
	}
}

func TestIssueRepo_Get_notFound(t *testing.T) {
	repo := setupTestRepo(t)

	_, err := repo.Get(9999)
	if err == nil {
		t.Fatal("expected error for non-existent issue, got nil")
	}
}

func TestIssueRepo_UpdateStatus_notFound(t *testing.T) {
	repo := setupTestRepo(t)

	err := repo.UpdateStatus(9999, "open")
	if err == nil {
		t.Fatal("expected error for non-existent issue, got nil")
	}
}

func TestIssueRepo_UpdateStatus_closedSetsClosedAt(t *testing.T) {
	repo := setupTestRepo(t)

	id, _ := repo.Create("To close", "task", nil, nil, nil)

	if err := repo.UpdateStatus(id, "closed"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	issue, _ := repo.Get(id)
	if issue.Status != "closed" {
		t.Errorf("expected status='closed', got %q", issue.Status)
	}
	if !issue.ClosedAt.Valid {
		t.Errorf("expected closed_at to be set after closing")
	}
}

func TestIssueRepo_Assign(t *testing.T) {
	repo := setupTestRepo(t)

	id, _ := repo.Create("Work item", "task", nil, nil, nil)
	repo.UpdateStatus(id, "open")

	if err := repo.Assign(id, "obsidian", "prole/obsidian/NC-24"); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	issue, _ := repo.Get(id)
	if !issue.Assignee.Valid || issue.Assignee.String != "obsidian" {
		t.Errorf("expected assignee='obsidian', got %v", issue.Assignee)
	}
	if !issue.Branch.Valid || issue.Branch.String != "prole/obsidian/NC-24" {
		t.Errorf("expected branch='prole/obsidian/NC-24', got %v", issue.Branch)
	}
}

func TestIssueRepo_Assign_preservesStatus(t *testing.T) {
	repo := setupTestRepo(t)

	id, _ := repo.Create("Work item", "task", nil, nil, nil)
	repo.UpdateStatus(id, "open")

	if err := repo.Assign(id, "iron", "prole/iron/42"); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	issue, _ := repo.Get(id)
	if issue.Status != "open" {
		t.Errorf("Assign must not change status: expected 'open', got %q", issue.Status)
	}
	if !issue.Assignee.Valid || issue.Assignee.String != "iron" {
		t.Errorf("expected assignee='iron', got %v", issue.Assignee)
	}
}

func TestIssueRepo_Assign_notFound(t *testing.T) {
	repo := setupTestRepo(t)

	err := repo.Assign(9999, "obsidian", "some-branch")
	if err == nil {
		t.Fatal("expected error for non-existent issue, got nil")
	}
}

func TestIssueRepo_SetPR(t *testing.T) {
	repo := setupTestRepo(t)

	id, _ := repo.Create("PR ticket", "task", nil, nil, nil)

	if err := repo.SetPR(id, 42); err != nil {
		t.Fatalf("SetPR: %v", err)
	}

	issue, _ := repo.Get(id)
	if !issue.PRNumber.Valid || issue.PRNumber.Int64 != 42 {
		t.Errorf("expected pr_number=42, got %v", issue.PRNumber)
	}
}

func TestIssueRepo_ListWithPRs(t *testing.T) {
	repo := setupTestRepo(t)

	// in_review with PR — should appear
	id1, _ := repo.Create("In review", "task", nil, nil, nil)
	repo.UpdateStatus(id1, "in_review")
	repo.SetPR(id1, 10)

	// closed with PR — should NOT appear
	id2, _ := repo.Create("Closed", "task", nil, nil, nil)
	repo.UpdateStatus(id2, "closed")
	repo.SetPR(id2, 11)

	// open without PR — should NOT appear
	id3, _ := repo.Create("No PR", "task", nil, nil, nil)
	repo.UpdateStatus(id3, "open")

	result, err := repo.ListWithPRs()
	if err != nil {
		t.Fatalf("ListWithPRs: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 ticket, got %d", len(result))
	}
	if result[0].ID != id1 {
		t.Errorf("expected ticket id=%d, got %d", id1, result[0].ID)
	}
}

func TestIssueRepo_ListWithPRs_multipleStatuses(t *testing.T) {
	repo := setupTestRepo(t)

	// Various non-closed statuses with PRs — all should appear
	id1, _ := repo.Create("In review", "task", nil, nil, nil)
	repo.UpdateStatus(id1, "in_review")
	repo.SetPR(id1, 1)

	id2, _ := repo.Create("Repairing", "task", nil, nil, nil)
	repo.UpdateStatus(id2, "repairing")
	repo.SetPR(id2, 2)

	result, err := repo.ListWithPRs()
	if err != nil {
		t.Fatalf("ListWithPRs: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 tickets, got %d", len(result))
	}
}

func TestIssueRepo_Close(t *testing.T) {
	repo := setupTestRepo(t)

	id, _ := repo.Create("To close", "task", nil, nil, nil)

	if err := repo.Close(id); err != nil {
		t.Fatalf("Close: %v", err)
	}

	issue, _ := repo.Get(id)
	if issue.Status != "closed" {
		t.Errorf("expected status='closed', got %q", issue.Status)
	}
	if !issue.ClosedAt.Valid {
		t.Errorf("expected closed_at to be set")
	}
}

func TestIssueRepo_Delete(t *testing.T) {
	repo := setupTestRepo(t)

	id, _ := repo.Create("To delete", "task", nil, nil, nil)

	if err := repo.Delete(id); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err := repo.Get(id)
	if err == nil {
		t.Error("expected error after delete, got nil")
	}
}

func TestIssueRepo_Delete_cascadesDependencies(t *testing.T) {
	repo := setupTestRepo(t)

	id1, _ := repo.Create("Ticket 1", "task", nil, nil, nil)
	id2, _ := repo.Create("Ticket 2", "task", nil, nil, nil)
	repo.AddDependency(id2, id1)

	// Delete id1 — dependency row should be removed
	if err := repo.Delete(id1); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	deps, err := repo.GetDependencies(id2)
	if err != nil {
		t.Fatalf("GetDependencies after delete: %v", err)
	}
	if len(deps) != 0 {
		t.Errorf("expected 0 dependencies after deleting dep target, got %d", len(deps))
	}
}

func TestIssueRepo_Delete_notFound(t *testing.T) {
	repo := setupTestRepo(t)

	err := repo.Delete(9999)
	if err == nil {
		t.Fatal("expected error for non-existent issue, got nil")
	}
}

func TestIssueRepo_SetAssignee(t *testing.T) {
	repo := setupTestRepo(t)

	id, _ := repo.Create("Claim ticket", "task", nil, nil, nil)
	repo.UpdateStatus(id, "in_review")

	if err := repo.SetAssignee(id, "reviewer"); err != nil {
		t.Fatalf("SetAssignee: %v", err)
	}

	issue, _ := repo.Get(id)
	if issue.Status != "in_review" {
		t.Errorf("SetAssignee must not change status: got %q", issue.Status)
	}
	if !issue.Assignee.Valid || issue.Assignee.String != "reviewer" {
		t.Errorf("expected assignee='reviewer', got %v", issue.Assignee)
	}
}

func TestIssueRepo_SetAssignee_notFound(t *testing.T) {
	repo := setupTestRepo(t)

	err := repo.SetAssignee(9999, "reviewer")
	if err == nil {
		t.Fatal("expected error for non-existent issue, got nil")
	}
}

func TestIssueRepo_ClearAssignee(t *testing.T) {
	repo := setupTestRepo(t)

	id, _ := repo.Create("Claimed ticket", "task", nil, nil, nil)
	repo.UpdateStatus(id, "under_review")
	repo.SetAssignee(id, "reviewer")

	if err := repo.ClearAssignee(id); err != nil {
		t.Fatalf("ClearAssignee: %v", err)
	}

	issue, _ := repo.Get(id)
	if issue.Assignee.Valid {
		t.Errorf("expected assignee to be NULL after ClearAssignee, got %q", issue.Assignee.String)
	}
	if issue.Status != "under_review" {
		t.Errorf("ClearAssignee must not change status: got %q", issue.Status)
	}
}

func TestIssueRepo_ClearAssignee_notFound(t *testing.T) {
	repo := setupTestRepo(t)

	err := repo.ClearAssignee(9999)
	if err == nil {
		t.Fatal("expected error for non-existent issue, got nil")
	}
}

func TestIssueRepo_ClearAssigneeByAgent_openTicket(t *testing.T) {
	repo := setupTestRepo(t)

	id, _ := repo.Create("Open ticket", "task", nil, nil, nil)
	repo.UpdateStatus(id, "open")
	repo.Assign(id, "iron", "prole/iron/1")

	n, err := repo.ClearAssigneeByAgent("iron")
	if err != nil {
		t.Fatalf("ClearAssigneeByAgent: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 row affected, got %d", n)
	}

	issue, _ := repo.Get(id)
	if issue.Assignee.Valid {
		t.Errorf("expected assignee=NULL, got %q", issue.Assignee.String)
	}
	if issue.Status != "open" {
		t.Errorf("expected status='open' (unchanged), got %q", issue.Status)
	}
}

func TestIssueRepo_ClearAssigneeByAgent_inProgressTicket(t *testing.T) {
	repo := setupTestRepo(t)

	id, _ := repo.Create("In-progress ticket", "task", nil, nil, nil)
	repo.UpdateStatus(id, "open")
	repo.Assign(id, "iron", "prole/iron/2")
	repo.UpdateStatus(id, "in_progress")

	n, err := repo.ClearAssigneeByAgent("iron")
	if err != nil {
		t.Fatalf("ClearAssigneeByAgent: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 row affected, got %d", n)
	}

	issue, _ := repo.Get(id)
	if issue.Assignee.Valid {
		t.Errorf("expected assignee=NULL, got %q", issue.Assignee.String)
	}
	if issue.Status != "open" {
		t.Errorf("expected status='open' (reverted from in_progress), got %q", issue.Status)
	}
}

func TestIssueRepo_ClearAssigneeByAgent_repairingTicket(t *testing.T) {
	repo := setupTestRepo(t)

	id, _ := repo.Create("Repairing ticket", "task", nil, nil, nil)
	repo.UpdateStatus(id, "repairing")
	repo.SetAssignee(id, "iron")

	n, err := repo.ClearAssigneeByAgent("iron")
	if err != nil {
		t.Fatalf("ClearAssigneeByAgent: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 row affected, got %d", n)
	}

	issue, _ := repo.Get(id)
	if issue.Assignee.Valid {
		t.Errorf("expected assignee=NULL, got %q", issue.Assignee.String)
	}
	// repairing tickets retain their status so the next prole inherits the
	// repair cycle and reads the existing reviewer feedback, rather than
	// treating the ticket as fresh work.
	if issue.Status != "repairing" {
		t.Errorf("expected status='repairing' (retained across assignee clearance), got %q", issue.Status)
	}
}

func TestIssueRepo_ClearAssigneeByAgent_leavesOtherStatuses(t *testing.T) {
	repo := setupTestRepo(t)

	for _, status := range []string{"in_review", "pr_open", "closed"} {
		id, _ := repo.Create("Ticket "+status, "task", nil, nil, nil)
		repo.UpdateStatus(id, status)
		repo.SetAssignee(id, "iron")
	}

	n, err := repo.ClearAssigneeByAgent("iron")
	if err != nil {
		t.Fatalf("ClearAssigneeByAgent: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 rows affected (in_review/pr_open/closed statuses skipped), got %d", n)
	}
}

func TestIssueRepo_ClearAssigneeByAgent_ciRunningTicket(t *testing.T) {
	repo := setupTestRepo(t)

	id, _ := repo.Create("CI running ticket", "task", nil, nil, nil)
	repo.UpdateStatus(id, "ci_running")
	repo.SetAssignee(id, "iron")

	n, err := repo.ClearAssigneeByAgent("iron")
	if err != nil {
		t.Fatalf("ClearAssigneeByAgent: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 row affected, got %d", n)
	}

	issue, _ := repo.Get(id)
	if issue.Assignee.Valid {
		t.Errorf("expected assignee=NULL, got %q", issue.Assignee.String)
	}
	// ci_running tickets retain their status — CI is still in flight and the PR
	// is already open. The daemon's CI reconciler resolves the ticket when the
	// run completes; flipping to open would lose the PR reference.
	if issue.Status != "ci_running" {
		t.Errorf("expected status='ci_running' (retained), got %q", issue.Status)
	}
}

func TestIssueRepo_ClearAssigneeByAgent_returnsCount(t *testing.T) {
	repo := setupTestRepo(t)

	for i := 0; i < 3; i++ {
		id, _ := repo.Create("Ticket", "task", nil, nil, nil)
		repo.UpdateStatus(id, "open")
		repo.Assign(id, "iron", "prole/iron/99")
	}

	n, err := repo.ClearAssigneeByAgent("iron")
	if err != nil {
		t.Fatalf("ClearAssigneeByAgent: %v", err)
	}
	if n != 3 {
		t.Errorf("expected count=3, got %d", n)
	}
}

func TestIssueRepo_ClearAssigneeByAgent_noMatches(t *testing.T) {
	repo := setupTestRepo(t)

	n, err := repo.ClearAssigneeByAgent("nonexistent-agent")
	if err != nil {
		t.Fatalf("ClearAssigneeByAgent: %v", err)
	}
	if n != 0 {
		t.Errorf("expected count=0 for unknown agent, got %d", n)
	}
}

func TestIssueRepo_UpdateDescription(t *testing.T) {
	repo := setupTestRepo(t)

	id, _ := repo.Create("My ticket", "task", nil, nil, nil)

	if err := repo.UpdateDescription(id, "This is a description."); err != nil {
		t.Fatalf("UpdateDescription: %v", err)
	}

	issue, err := repo.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !issue.Description.Valid {
		t.Fatal("expected description to be set, got NULL")
	}
	if issue.Description.String != "This is a description." {
		t.Errorf("expected %q, got %q", "This is a description.", issue.Description.String)
	}
}

func TestIssueRepo_UpdateDescription_notFound(t *testing.T) {
	repo := setupTestRepo(t)

	err := repo.UpdateDescription(9999, "irrelevant")
	if err == nil {
		t.Fatal("expected error for non-existent issue, got nil")
	}
}

func TestIssueRepo_ListEpicsWithAllChildrenClosed(t *testing.T) {
	r := setupTestRepo(t)

	// Epic with all children closed → should be returned.
	epicID, _ := r.Create("Epic A", "epic", nil, nil, nil)
	r.UpdateStatus(epicID, "open")
	child1, _ := r.Create("Task 1", "task", &epicID, nil, nil)
	r.UpdateStatus(child1, "closed")
	child2, _ := r.Create("Task 2", "task", &epicID, nil, nil)
	r.UpdateStatus(child2, "closed")

	// Epic with one open child → should NOT be returned.
	epic2ID, _ := r.Create("Epic B", "epic", nil, nil, nil)
	r.UpdateStatus(epic2ID, "open")
	child3, _ := r.Create("Task 3", "task", &epic2ID, nil, nil)
	r.UpdateStatus(child3, "closed")
	child4, _ := r.Create("Task 4", "task", &epic2ID, nil, nil)
	r.UpdateStatus(child4, "open")

	// Epic with no children → should NOT be returned.
	epic3ID, _ := r.Create("Epic C", "epic", nil, nil, nil)
	r.UpdateStatus(epic3ID, "open")

	// Already-closed epic with all children closed → should NOT be returned.
	epic4ID, _ := r.Create("Epic D", "epic", nil, nil, nil)
	r.UpdateStatus(epic4ID, "open")
	child5, _ := r.Create("Task 5", "task", &epic4ID, nil, nil)
	r.UpdateStatus(child5, "closed")
	r.UpdateStatus(epic4ID, "closed")

	epics, err := r.ListEpicsWithAllChildrenClosed()
	if err != nil {
		t.Fatalf("ListEpicsWithAllChildrenClosed: %v", err)
	}
	if len(epics) != 1 {
		t.Fatalf("expected 1 epic, got %d", len(epics))
	}
	if epics[0].ID != epicID {
		t.Errorf("expected epic ID %d, got %d", epicID, epics[0].ID)
	}
}

func TestIssueRepo_Create_withPriority(t *testing.T) {
	repo := setupTestRepo(t)

	p := "P1"
	id, err := repo.Create("High priority task", "task", nil, nil, &p)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	issue, err := repo.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !issue.Priority.Valid || issue.Priority.String != "P1" {
		t.Errorf("expected priority='P1', got %v", issue.Priority)
	}
}

func TestIssueRepo_SetPriority(t *testing.T) {
	repo := setupTestRepo(t)

	id, _ := repo.Create("Task", "task", nil, nil, nil)

	if err := repo.SetPriority(id, "P0"); err != nil {
		t.Fatalf("SetPriority: %v", err)
	}

	issue, _ := repo.Get(id)
	if !issue.Priority.Valid || issue.Priority.String != "P0" {
		t.Errorf("expected priority='P0', got %v", issue.Priority)
	}
}

func TestIssueRepo_SetPriority_notFound(t *testing.T) {
	repo := setupTestRepo(t)

	err := repo.SetPriority(9999, "P1")
	if err == nil {
		t.Fatal("expected error for non-existent issue, got nil")
	}
}

func TestIssueRepo_Ready_ordersByPriority(t *testing.T) {
	repo := setupTestRepo(t)

	// Create tickets with various priorities
	id1, _ := repo.Create("No priority", "task", nil, nil, nil)
	repo.UpdateStatus(id1, "open")

	p0 := "P0"
	id2, _ := repo.Create("P0 task", "task", nil, nil, &p0)
	repo.UpdateStatus(id2, "open")

	p2 := "P2"
	id3, _ := repo.Create("P2 task", "task", nil, nil, &p2)
	repo.UpdateStatus(id3, "open")

	p1 := "P1"
	id4, _ := repo.Create("P1 task", "task", nil, nil, &p1)
	repo.UpdateStatus(id4, "open")

	ready, err := repo.Ready()
	if err != nil {
		t.Fatalf("Ready: %v", err)
	}

	if len(ready) != 4 {
		t.Fatalf("expected 4 ready tickets, got %d", len(ready))
	}

	// P0 first, then P1, then P2, then nil
	if ready[0].ID != id2 {
		t.Errorf("expected P0 ticket first, got id=%d", ready[0].ID)
	}
	if ready[1].ID != id4 {
		t.Errorf("expected P1 ticket second, got id=%d", ready[1].ID)
	}
	if ready[2].ID != id3 {
		t.Errorf("expected P2 ticket third, got id=%d", ready[2].ID)
	}
	if ready[3].ID != id1 {
		t.Errorf("expected no-priority ticket last, got id=%d", ready[3].ID)
	}
}

// --- Selectable() tests ---

func TestSelectable_OrderStatusRepairingFirst(t *testing.T) {
	r := setupTestRepo(t)

	// open P1 task (lower id)
	openID, _ := r.Create("Open task", "task", nil, nil, p1Ptr())
	r.UpdateStatus(openID, "open")

	// repairing P1 task (higher id, but should come first)
	repairID, _ := r.Create("Repairing task", "task", nil, nil, p1Ptr())
	r.UpdateStatus(repairID, "repairing")

	result, err := r.Selectable()
	if err != nil {
		t.Fatalf("Selectable: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if result[0].ID != repairID {
		t.Errorf("expected repairing ticket first (id=%d), got id=%d", repairID, result[0].ID)
	}
}

func TestSelectable_OrderBugBeforeTask(t *testing.T) {
	r := setupTestRepo(t)

	taskID, _ := r.Create("P1 task", "task", nil, nil, p1Ptr())
	r.UpdateStatus(taskID, "open")

	bugID, _ := r.Create("P1 bug", "bug", nil, nil, p1Ptr())
	r.UpdateStatus(bugID, "open")

	result, err := r.Selectable()
	if err != nil {
		t.Fatalf("Selectable: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if result[0].ID != bugID {
		t.Errorf("expected bug first (id=%d), got id=%d", bugID, result[0].ID)
	}
}

func TestSelectable_OrderPriority(t *testing.T) {
	r := setupTestRepo(t)

	p3 := "P3"
	p0 := "P0"
	p2 := "P2"
	p1 := "P1"
	p4 := "P4"
	p5 := "P5"

	id3, _ := r.Create("P3 task", "task", nil, nil, &p3)
	r.UpdateStatus(id3, "open")
	id0, _ := r.Create("P0 task", "task", nil, nil, &p0)
	r.UpdateStatus(id0, "open")
	id2, _ := r.Create("P2 task", "task", nil, nil, &p2)
	r.UpdateStatus(id2, "open")
	id1, _ := r.Create("P1 task", "task", nil, nil, &p1)
	r.UpdateStatus(id1, "open")
	id4, _ := r.Create("P4 task", "task", nil, nil, &p4)
	r.UpdateStatus(id4, "open")
	id5, _ := r.Create("P5 task", "task", nil, nil, &p5)
	r.UpdateStatus(id5, "open")
	idNull, _ := r.Create("null priority task", "task", nil, nil, nil)
	r.UpdateStatus(idNull, "open")

	result, err := r.Selectable()
	if err != nil {
		t.Fatalf("Selectable: %v", err)
	}
	if len(result) != 7 {
		t.Fatalf("expected 7 results, got %d", len(result))
	}
	order := []int{id0, id1, id2, id3, id4, id5, idNull}
	for i, expected := range order {
		if result[i].ID != expected {
			t.Errorf("position %d: expected id=%d, got id=%d", i, expected, result[i].ID)
		}
	}
}

func TestSelectable_OrderIDTiebreaker(t *testing.T) {
	r := setupTestRepo(t)

	id1, _ := r.Create("First P1 task", "task", nil, nil, p1Ptr())
	r.UpdateStatus(id1, "open")
	id2, _ := r.Create("Second P1 task", "task", nil, nil, p1Ptr())
	r.UpdateStatus(id2, "open")

	result, err := r.Selectable()
	if err != nil {
		t.Fatalf("Selectable: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if result[0].ID != id1 {
		t.Errorf("expected lower id first (id=%d), got id=%d", id1, result[0].ID)
	}
}

func TestSelectable_SkipsEpics(t *testing.T) {
	r := setupTestRepo(t)

	epicID, _ := r.Create("Big epic", "epic", nil, nil, nil)
	r.UpdateStatus(epicID, "open")

	result, err := r.Selectable()
	if err != nil {
		t.Fatalf("Selectable: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 results (epic excluded), got %d", len(result))
	}
}

func TestSelectable_SkipsBlockedOpen(t *testing.T) {
	r := setupTestRepo(t)

	blockerID, _ := r.Create("Blocker", "task", nil, nil, nil)
	r.UpdateStatus(blockerID, "open")

	blockedID, _ := r.Create("Blocked task", "task", nil, nil, nil)
	r.UpdateStatus(blockedID, "open")
	r.AddDependency(blockedID, blockerID) // blockedID depends on blockerID (still open)

	result, err := r.Selectable()
	if err != nil {
		t.Fatalf("Selectable: %v", err)
	}
	// Only the blocker (no deps) should be selectable; blocked is excluded.
	if len(result) != 1 {
		t.Fatalf("expected 1 result (blocked ticket excluded), got %d", len(result))
	}
	if result[0].ID != blockerID {
		t.Errorf("expected blocker id=%d, got id=%d", blockerID, result[0].ID)
	}
}

func TestSelectable_IncludesRepairingWithOpenDependency(t *testing.T) {
	r := setupTestRepo(t)

	blockerID, _ := r.Create("Open blocker", "task", nil, nil, nil)
	r.UpdateStatus(blockerID, "open")

	repairID, _ := r.Create("Repairing with dep", "task", nil, nil, nil)
	r.UpdateStatus(repairID, "repairing")
	r.AddDependency(repairID, blockerID) // repairing ticket depends on an open ticket

	result, err := r.Selectable()
	if err != nil {
		t.Fatalf("Selectable: %v", err)
	}
	// repairing ticket bypasses dependency filter; both should be selectable.
	// blocker is open+unblocked, repairing is in repair flow.
	ids := make(map[int]bool)
	for _, i := range result {
		ids[i.ID] = true
	}
	if !ids[repairID] {
		t.Errorf("expected repairing ticket (id=%d) included despite open dependency", repairID)
	}
}

func TestSelectable_SkipsAssigned(t *testing.T) {
	r := setupTestRepo(t)

	id, _ := r.Create("Already assigned", "task", nil, nil, nil)
	r.UpdateStatus(id, "open")
	r.Assign(id, "iron", "prole/iron/1")

	result, err := r.Selectable()
	if err != nil {
		t.Fatalf("Selectable: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 results (assigned ticket excluded), got %d", len(result))
	}
}

func TestSelectable_SkipsAssignedRepairing(t *testing.T) {
	r := setupTestRepo(t)

	id, _ := r.Create("Assigned repairing", "task", nil, nil, nil)
	r.UpdateStatus(id, "repairing")
	r.SetAssignee(id, "iron")

	result, err := r.Selectable()
	if err != nil {
		t.Fatalf("Selectable: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 results (assigned repairing excluded), got %d", len(result))
	}
}

func TestSelectable_FullOrderingLex(t *testing.T) {
	r := setupTestRepo(t)

	p0 := "P0"
	p1s := "P1"

	// Six tickets covering every tiebreaker dimension.
	// Ordering: repairing < open, then bug < task, then P0 < P1, then lower id.
	// So all repairing tickets come first (sorted by type then priority),
	// then all open tickets (sorted by type then priority).
	// Expected: repairBugP0, repairTaskP0, openBugP0, openBugP1, openTaskP0, openTaskP1
	repairBugP0, _ := r.Create("Repair bug P0", "bug", nil, nil, &p0)
	r.UpdateStatus(repairBugP0, "repairing")

	repairTaskP0, _ := r.Create("Repair task P0", "task", nil, nil, &p0)
	r.UpdateStatus(repairTaskP0, "repairing")

	openBugP0, _ := r.Create("Open bug P0", "bug", nil, nil, &p0)
	r.UpdateStatus(openBugP0, "open")

	openTaskP0, _ := r.Create("Open task P0", "task", nil, nil, &p0)
	r.UpdateStatus(openTaskP0, "open")

	openBugP1, _ := r.Create("Open bug P1", "bug", nil, nil, &p1s)
	r.UpdateStatus(openBugP1, "open")

	openTaskP1, _ := r.Create("Open task P1", "task", nil, nil, &p1s)
	r.UpdateStatus(openTaskP1, "open")

	// type takes priority over priority level: all bugs come before all tasks
	expected := []int{repairBugP0, repairTaskP0, openBugP0, openBugP1, openTaskP0, openTaskP1}

	result, err := r.Selectable()
	if err != nil {
		t.Fatalf("Selectable: %v", err)
	}
	if len(result) != len(expected) {
		t.Fatalf("expected %d results, got %d", len(expected), len(result))
	}
	for i, exp := range expected {
		if result[i].ID != exp {
			t.Errorf("position %d: expected id=%d (ordering %v), got id=%d %q",
				i, exp, expected, result[i].ID, result[i].Title)
		}
	}
}

// p1Ptr returns a pointer to "P1" for use in Create calls.
func p1Ptr() *string {
	p := "P1"
	return &p
}

func TestIssueRepo_BusyAssignees_empty(t *testing.T) {
	repo := setupTestRepo(t)

	busy, err := repo.BusyAssignees()
	if err != nil {
		t.Fatalf("BusyAssignees: %v", err)
	}
	if len(busy) != 0 {
		t.Errorf("expected empty set, got %v", busy)
	}
}

func TestIssueRepo_BusyAssignees_oneBusy(t *testing.T) {
	repo := setupTestRepo(t)

	id, _ := repo.Create("Held ticket", "task", nil, nil, nil)
	repo.UpdateStatus(id, "open")
	repo.Assign(id, "copper", "prole/copper/1")

	busy, err := repo.BusyAssignees()
	if err != nil {
		t.Fatalf("BusyAssignees: %v", err)
	}
	if !busy["copper"] {
		t.Errorf("expected copper in busy set, got %v", busy)
	}
	if len(busy) != 1 {
		t.Errorf("expected 1 entry, got %d: %v", len(busy), busy)
	}
}

func TestIssueRepo_BusyAssignees_mixedStatuses(t *testing.T) {
	repo := setupTestRepo(t)

	// Active statuses — prole is still doing work, must stay in busy set.
	for _, tc := range []struct {
		agent  string
		status string
	}{
		{"copper", "open"},
		{"iron", "in_progress"},
		{"zinc", "repairing"},
	} {
		id, _ := repo.Create("T", "task", nil, nil, nil)
		repo.UpdateStatus(id, tc.status)
		repo.Assign(id, tc.agent, "prole/"+tc.agent+"/x")
	}

	// Handoff statuses — prole handed off to reviewer, slot is free.
	// ci_running is included: prole filed the PR and is idle waiting for CI.
	for _, tc := range []struct {
		agent  string
		status string
	}{
		{"ruby", "ci_running"},
		{"tin", "in_review"},
		{"lead", "under_review"},
		{"brass", "pr_open"},
		{"gold", "merge_conflict"},
	} {
		id, _ := repo.Create("T", "task", nil, nil, nil)
		repo.UpdateStatus(id, tc.status)
		repo.Assign(id, tc.agent, "prole/"+tc.agent+"/x")
	}

	// Closed ticket — assignee should be excluded.
	closedID, _ := repo.Create("Closed", "task", nil, nil, nil)
	repo.Assign(closedID, "silver", "prole/silver/x")
	repo.UpdateStatus(closedID, "closed")

	busy, err := repo.BusyAssignees()
	if err != nil {
		t.Fatalf("BusyAssignees: %v", err)
	}

	// Active statuses must be in the busy set.
	for _, name := range []string{"copper", "iron", "zinc"} {
		if !busy[name] {
			t.Errorf("expected %s (active status) in busy set, got %v", name, busy)
		}
	}
	// Handoff and closed statuses must NOT be in the busy set.
	for _, name := range []string{"ruby", "tin", "lead", "brass", "gold", "silver"} {
		if busy[name] {
			t.Errorf("expected %s (handoff/closed status) NOT in busy set, got %v", name, busy)
		}
	}
}

func TestIssueRepo_BusyAssignees_handoffStatusesExcluded(t *testing.T) {
	repo := setupTestRepo(t)

	// All handoff statuses should free the prole slot.
	// ci_running: prole filed the PR and is idle; daemon will reassign via
	// repairing if CI fails, or promote to in_review when it passes.
	for _, tc := range []struct {
		agent  string
		status string
	}{
		{"ruby", "ci_running"},
		{"tin", "in_review"},
		{"lead", "under_review"},
		{"brass", "pr_open"},
		{"gold", "merge_conflict"},
	} {
		id, _ := repo.Create("T", "task", nil, nil, nil)
		repo.UpdateStatus(id, tc.status)
		repo.Assign(id, tc.agent, "prole/"+tc.agent+"/x")
	}

	busy, err := repo.BusyAssignees()
	if err != nil {
		t.Fatalf("BusyAssignees: %v", err)
	}
	if len(busy) != 0 {
		t.Errorf("all handoff statuses should be excluded from busy set, got %v", busy)
	}
}

func TestIssueRepo_BusyAssignees_activeStatusesIncluded(t *testing.T) {
	repo := setupTestRepo(t)

	// in_progress and repairing are still active — prole must be held busy.
	for _, tc := range []struct {
		agent  string
		status string
	}{
		{"iron", "in_progress"},
		{"zinc", "repairing"},
	} {
		id, _ := repo.Create("T", "task", nil, nil, nil)
		repo.UpdateStatus(id, tc.status)
		repo.Assign(id, tc.agent, "prole/"+tc.agent+"/x")
	}

	busy, err := repo.BusyAssignees()
	if err != nil {
		t.Fatalf("BusyAssignees: %v", err)
	}
	for _, name := range []string{"iron", "zinc"} {
		if !busy[name] {
			t.Errorf("expected %s (active status) in busy set, got %v", name, busy)
		}
	}
}

func TestIssueRepo_BusyAssignees_excludesNullAndEmpty(t *testing.T) {
	repo := setupTestRepo(t)

	// NULL assignee — a fresh open ticket with no one assigned.
	nullID, _ := repo.Create("Unassigned", "task", nil, nil, nil)
	repo.UpdateStatus(nullID, "open")

	// Empty-string assignee — set via raw SQL since SetAssignee passes "" through.
	emptyID, _ := repo.Create("Empty", "task", nil, nil, nil)
	repo.UpdateStatus(emptyID, "open")
	if err := repo.SetAssignee(emptyID, ""); err != nil {
		t.Fatalf("SetAssignee empty: %v", err)
	}

	busy, err := repo.BusyAssignees()
	if err != nil {
		t.Fatalf("BusyAssignees: %v", err)
	}
	if len(busy) != 0 {
		t.Errorf("expected empty set (NULL and '' both excluded), got %v", busy)
	}
}

func TestIssueRepo_BusyAssignees_distinctAcrossMultipleTickets(t *testing.T) {
	repo := setupTestRepo(t)

	// Same agent holding two tickets — should appear once.
	for i := 0; i < 2; i++ {
		id, _ := repo.Create("T", "task", nil, nil, nil)
		repo.UpdateStatus(id, "open")
		repo.Assign(id, "copper", "prole/copper/x")
	}

	busy, err := repo.BusyAssignees()
	if err != nil {
		t.Fatalf("BusyAssignees: %v", err)
	}
	if !busy["copper"] {
		t.Errorf("expected copper in busy set, got %v", busy)
	}
	if len(busy) != 1 {
		t.Errorf("expected 1 entry (DISTINCT), got %d: %v", len(busy), busy)
	}
}

func TestValidStatuses_includesMergeConflict(t *testing.T) {
	found := false
	for _, s := range ValidStatuses {
		if s == "merge_conflict" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ValidStatuses to contain %q; got %v", "merge_conflict", ValidStatuses)
	}
}

func TestValidStatuses_includesCIRunning(t *testing.T) {
	found := false
	for _, s := range ValidStatuses {
		if s == "ci_running" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected ValidStatuses to contain %q; got %v", "ci_running", ValidStatuses)
	}
}

func TestUpdateStatus_acceptsCIRunning(t *testing.T) {
	repo := setupTestRepo(t)
	id, err := repo.Create("test ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.UpdateStatus(id, "in_progress"); err != nil {
		t.Fatalf("UpdateStatus in_progress: %v", err)
	}
	if err := repo.UpdateStatus(id, "ci_running"); err != nil {
		t.Fatalf("UpdateStatus ci_running: %v", err)
	}
	issue, err := repo.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if issue.Status != "ci_running" {
		t.Errorf("expected status=ci_running, got %q", issue.Status)
	}
}

func TestUpdateStatus_acceptsMergeConflict(t *testing.T) {
	repo := setupTestRepo(t)
	id, err := repo.Create("test ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.UpdateStatus(id, "pr_open"); err != nil {
		t.Fatalf("UpdateStatus pr_open: %v", err)
	}
	if err := repo.UpdateStatus(id, "merge_conflict"); err != nil {
		t.Fatalf("UpdateStatus merge_conflict: %v", err)
	}
	issue, err := repo.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if issue.Status != "merge_conflict" {
		t.Errorf("expected status=merge_conflict, got %q", issue.Status)
	}
}

func TestIssueRepo_SetParent(t *testing.T) {
	r := setupTestRepo(t)

	child, _ := r.Create("Child", "task", nil, nil, nil)
	parent, _ := r.Create("Parent", "epic", nil, nil, nil)

	if err := r.SetParent(child, parent); err != nil {
		t.Fatalf("SetParent: %v", err)
	}

	got, err := r.Get(child)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.ParentID.Valid || int(got.ParentID.Int64) != parent {
		t.Errorf("ParentID = %v, want %d", got.ParentID, parent)
	}
}

func TestIssueRepo_SetParent_notFound(t *testing.T) {
	r := setupTestRepo(t)
	if err := r.SetParent(9999, 1); err == nil {
		t.Error("expected error for nonexistent ticket")
	}
}

func TestIssueRepo_ClearParent(t *testing.T) {
	r := setupTestRepo(t)

	parent, _ := r.Create("Parent", "epic", nil, nil, nil)
	child, _ := r.Create("Child", "task", &parent, nil, nil)

	got, _ := r.Get(child)
	if !got.ParentID.Valid {
		t.Fatalf("expected ParentID to be set initially")
	}

	if err := r.ClearParent(child); err != nil {
		t.Fatalf("ClearParent: %v", err)
	}

	got, err := r.Get(child)
	if err != nil {
		t.Fatalf("Get after clear: %v", err)
	}
	if got.ParentID.Valid {
		t.Errorf("expected ParentID NULL after clear, got %d", got.ParentID.Int64)
	}
}

func TestIssueRepo_ClearParent_notFound(t *testing.T) {
	r := setupTestRepo(t)
	if err := r.ClearParent(9999); err == nil {
		t.Error("expected error for nonexistent ticket")
	}
}

func TestIssueRepo_SetRepairReason_roundTrip(t *testing.T) {
	r := setupTestRepo(t)
	id, _ := r.Create("Some task", "task", nil, nil, nil)
	r.UpdateStatus(id, "repairing")

	if err := r.SetRepairReason(id, "CI: lint, test"); err != nil {
		t.Fatalf("SetRepairReason: %v", err)
	}

	got, err := r.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.RepairReason.Valid || got.RepairReason.String != "CI: lint, test" {
		t.Errorf("expected repair_reason=%q, got valid=%v value=%q",
			"CI: lint, test", got.RepairReason.Valid, got.RepairReason.String)
	}
}

func TestIssueRepo_SetRepairReason_notFound(t *testing.T) {
	r := setupTestRepo(t)
	if err := r.SetRepairReason(9999, "nope"); err == nil {
		t.Error("expected error for nonexistent ticket")
	}
}

func TestIssueRepo_SetRepairReason_emptyStringStoresNULL(t *testing.T) {
	r := setupTestRepo(t)
	id, _ := r.Create("Task", "task", nil, nil, nil)
	r.UpdateStatus(id, "repairing")
	r.SetRepairReason(id, "CI: lint")

	if err := r.SetRepairReason(id, ""); err != nil {
		t.Fatalf("SetRepairReason: %v", err)
	}

	got, _ := r.Get(id)
	if got.RepairReason.Valid {
		t.Errorf("expected NULL repair_reason after empty string, got %q", got.RepairReason.String)
	}
}

func TestIssueRepo_UpdateStatus_clearsRepairReasonOnRecovery(t *testing.T) {
	r := setupTestRepo(t)
	id, _ := r.Create("Task", "task", nil, nil, nil)
	r.UpdateStatus(id, "repairing")
	r.SetRepairReason(id, "CI: lint")

	// Transition to in_progress — repair_reason should be cleared.
	if err := r.UpdateStatus(id, "in_progress"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, _ := r.Get(id)
	if got.RepairReason.Valid {
		t.Errorf("expected repair_reason cleared after in_progress transition, got %q", got.RepairReason.String)
	}
}

func TestIssueRepo_UpdateStatus_clearsRepairReasonOnCIRunning(t *testing.T) {
	r := setupTestRepo(t)
	id, _ := r.Create("Task", "task", nil, nil, nil)
	r.Assign(id, "copper", "prole/copper/nc-1")
	r.UpdateStatus(id, "repairing")
	r.SetRepairReason(id, "merge conflict with main")

	if err := r.UpdateStatus(id, "ci_running"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, _ := r.Get(id)
	if got.RepairReason.Valid {
		t.Errorf("expected repair_reason cleared on ci_running, got %q", got.RepairReason.String)
	}
}

func TestIssueRepo_UpdateStatus_preservesRepairReasonOnRepairingTransition(t *testing.T) {
	r := setupTestRepo(t)
	id, _ := r.Create("Task", "task", nil, nil, nil)
	r.UpdateStatus(id, "repairing")
	r.SetRepairReason(id, "CI: test")

	// A second repairing transition (re-bounce) should NOT clear the reason —
	// the daemon will overwrite it with the new reason immediately after.
	if err := r.UpdateStatus(id, "repairing"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	got, _ := r.Get(id)
	if !got.RepairReason.Valid || got.RepairReason.String != "CI: test" {
		t.Errorf("expected repair_reason preserved through repairing re-bounce, got valid=%v value=%q",
			got.RepairReason.Valid, got.RepairReason.String)
	}
}

// TestUpdateStatus_openResetsRepairCycleCount verifies that transitioning to
// "open" resets repair_cycle_count to 0. This is the human-unblock path:
// when a human moves an on_hold ticket back to open, the bounce counter is
// cleared so the ticket gets a fresh slate.
func TestUpdateStatus_openResetsRepairCycleCount(t *testing.T) {
	r := setupTestRepo(t)

	id, _ := r.Create("Bouncy ticket", "task", nil, nil, nil)

	// Accumulate some repair cycles.
	r.UpdateStatus(id, "repairing") //nolint:errcheck
	r.UpdateStatus(id, "repairing") //nolint:errcheck
	r.UpdateStatus(id, "on_hold")   //nolint:errcheck

	got, _ := r.Get(id)
	if got.RepairCycleCount != 2 {
		t.Fatalf("precondition: expected repair_cycle_count=2, got %d", got.RepairCycleCount)
	}

	// Human unblocks by moving back to open — count must reset.
	if err := r.UpdateStatus(id, "open"); err != nil {
		t.Fatalf("UpdateStatus to open: %v", err)
	}

	got, _ = r.Get(id)
	if got.RepairCycleCount != 0 {
		t.Errorf("expected repair_cycle_count=0 after open, got %d", got.RepairCycleCount)
	}
	if got.Status != "open" {
		t.Errorf("expected status=open, got %q", got.Status)
	}
}

// TestUpdateStatus_openClearsRepairReason verifies that transitioning to "open"
// clears a stale repair_reason (e.g. one set by the daemon after on_hold).
func TestUpdateStatus_openClearsRepairReason(t *testing.T) {
	r := setupTestRepo(t)

	id, _ := r.Create("Reason ticket", "task", nil, nil, nil)
	r.UpdateStatus(id, "repairing") //nolint:errcheck
	r.UpdateStatus(id, "on_hold")   //nolint:errcheck
	// Simulate the daemon or human annotating the on_hold ticket after the
	// status transition (on_hold clears repair_reason on entry, but it can be
	// set again afterwards).
	r.SetRepairReason(id, "escalated: too many bounces") //nolint:errcheck

	// Confirm repair_reason is set before the open transition.
	got, _ := r.Get(id)
	if !got.RepairReason.Valid {
		t.Fatal("precondition: expected repair_reason to be set after SetRepairReason")
	}

	r.UpdateStatus(id, "open") //nolint:errcheck

	got, _ = r.Get(id)
	if got.RepairReason.Valid {
		t.Errorf("expected repair_reason cleared after open, got %q", got.RepairReason.String)
	}
}

// TestUpdateStatus_repairingAfterOpenIncrements verifies that after a human
// unblock (open reset), a new repairing transition increments from 0 (not
// from the prior accumulated value).
func TestUpdateStatus_repairingAfterOpenIncrements(t *testing.T) {
	r := setupTestRepo(t)

	id, _ := r.Create("Fresh start ticket", "task", nil, nil, nil)

	r.UpdateStatus(id, "repairing") //nolint:errcheck
	r.UpdateStatus(id, "repairing") //nolint:errcheck
	r.UpdateStatus(id, "on_hold")   //nolint:errcheck
	r.UpdateStatus(id, "open")      //nolint:errcheck // resets to 0

	r.UpdateStatus(id, "repairing") //nolint:errcheck

	got, _ := r.Get(id)
	if got.RepairCycleCount != 1 {
		t.Errorf("expected repair_cycle_count=1 after fresh repairing post-open, got %d", got.RepairCycleCount)
	}
}

// TestUpdateStatus_draftClearsRepairReason verifies that transitioning to "draft"
// also clears repair_reason — same stale-reason leak path.
func TestUpdateStatus_draftClearsRepairReason(t *testing.T) {
	r := setupTestRepo(t)

	id, _ := r.Create("Stale reason draft ticket", "task", nil, nil, nil)
	r.UpdateStatus(id, "repairing")      //nolint:errcheck
	r.SetRepairReason(id, "CI: failure") //nolint:errcheck

	r.UpdateStatus(id, "draft") //nolint:errcheck

	got, _ := r.Get(id)
	if got.RepairReason.Valid {
		t.Errorf("expected repair_reason cleared on → draft, got %q", got.RepairReason.String)
	}
}

func TestCreateTicket_TDDTestsType(t *testing.T) {
	r := setupTestRepo(t)
	id, err := r.Create("Failing auth tests", "tdd_tests", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create with tdd_tests type: %v", err)
	}
	issue, err := r.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if issue.IssueType != "tdd_tests" {
		t.Errorf("IssueType = %q, want %q", issue.IssueType, "tdd_tests")
	}
}
