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
