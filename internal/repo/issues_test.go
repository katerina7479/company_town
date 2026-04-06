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
	return NewIssueRepo(conn)
}

func TestIssueRepo_Create(t *testing.T) {
	repo := setupTestRepo(t)

	id, err := repo.Create("Test ticket", "task", nil, nil)
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

	id, _ := repo.Create("Test ticket", "task", nil, nil)

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

	id1, _ := repo.Create("Ticket 1", "task", nil, nil)
	id2, _ := repo.Create("Ticket 2", "task", nil, nil)

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

func TestIssueRepo_Ready(t *testing.T) {
	repo := setupTestRepo(t)

	// Create three tickets
	id1, _ := repo.Create("Ticket 1", "task", nil, nil)
	id2, _ := repo.Create("Ticket 2", "task", nil, nil)
	id3, _ := repo.Create("Ticket 3", "task", nil, nil)

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

	taskID, _ := repo.Create("A task", "task", nil, nil)
	epicID, _ := repo.Create("An epic", "epic", nil, nil)

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
	parentID, _ := repo.Create("Epic 1", "epic", nil, nil)
	child1ID, _ := repo.Create("Task 1", "task", &parentID, nil)
	child2ID, _ := repo.Create("Task 2", "task", &parentID, nil)
	rootID, _ := repo.Create("Root Task", "task", nil, nil)

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
	id, _ := repo.Create("Orphan", "task", nil, nil)
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

	repo.Create("Draft 1", "task", nil, nil)
	repo.Create("Draft 2", "task", nil, nil)
	id3, _ := repo.Create("Open 1", "task", nil, nil)
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

	parentID, _ := repo.Create("Epic", "epic", nil, nil)
	spec := "backend"
	childID, err := repo.Create("Child task", "task", &parentID, &spec)
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

	id, _ := repo.Create("To close", "task", nil, nil)

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

	id, _ := repo.Create("Work item", "task", nil, nil)
	repo.UpdateStatus(id, "open")

	if err := repo.Assign(id, "obsidian", "prole/obsidian/NC-24"); err != nil {
		t.Fatalf("Assign: %v", err)
	}

	issue, _ := repo.Get(id)
	if issue.Status != "in_progress" {
		t.Errorf("expected status='in_progress', got %q", issue.Status)
	}
	if !issue.Assignee.Valid || issue.Assignee.String != "obsidian" {
		t.Errorf("expected assignee='obsidian', got %v", issue.Assignee)
	}
	if !issue.Branch.Valid || issue.Branch.String != "prole/obsidian/NC-24" {
		t.Errorf("expected branch='prole/obsidian/NC-24', got %v", issue.Branch)
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

	id, _ := repo.Create("PR ticket", "task", nil, nil)

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
	id1, _ := repo.Create("In review", "task", nil, nil)
	repo.UpdateStatus(id1, "in_review")
	repo.SetPR(id1, 10)

	// closed with PR — should NOT appear
	id2, _ := repo.Create("Closed", "task", nil, nil)
	repo.UpdateStatus(id2, "closed")
	repo.SetPR(id2, 11)

	// open without PR — should NOT appear
	id3, _ := repo.Create("No PR", "task", nil, nil)
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
	id1, _ := repo.Create("In review", "task", nil, nil)
	repo.UpdateStatus(id1, "in_review")
	repo.SetPR(id1, 1)

	id2, _ := repo.Create("Repairing", "task", nil, nil)
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

	id, _ := repo.Create("To close", "task", nil, nil)

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

	id, _ := repo.Create("To delete", "task", nil, nil)

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

	id1, _ := repo.Create("Ticket 1", "task", nil, nil)
	id2, _ := repo.Create("Ticket 2", "task", nil, nil)
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

	id, _ := repo.Create("Claim ticket", "task", nil, nil)
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

	id, _ := repo.Create("Claimed ticket", "task", nil, nil)
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

func TestIssueRepo_UpdateDescription(t *testing.T) {
	repo := setupTestRepo(t)

	id, _ := repo.Create("My ticket", "task", nil, nil)

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
	epicID, _ := r.Create("Epic A", "epic", nil, nil)
	r.UpdateStatus(epicID, "open")
	child1, _ := r.Create("Task 1", "task", &epicID, nil)
	r.UpdateStatus(child1, "closed")
	child2, _ := r.Create("Task 2", "task", &epicID, nil)
	r.UpdateStatus(child2, "closed")

	// Epic with one open child → should NOT be returned.
	epic2ID, _ := r.Create("Epic B", "epic", nil, nil)
	r.UpdateStatus(epic2ID, "open")
	child3, _ := r.Create("Task 3", "task", &epic2ID, nil)
	r.UpdateStatus(child3, "closed")
	child4, _ := r.Create("Task 4", "task", &epic2ID, nil)
	r.UpdateStatus(child4, "open")

	// Epic with no children → should NOT be returned.
	epic3ID, _ := r.Create("Epic C", "epic", nil, nil)
	r.UpdateStatus(epic3ID, "open")

	// Already-closed epic with all children closed → should NOT be returned.
	epic4ID, _ := r.Create("Epic D", "epic", nil, nil)
	r.UpdateStatus(epic4ID, "open")
	child5, _ := r.Create("Task 5", "task", &epic4ID, nil)
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
