package commands

import (
	"database/sql"
	"testing"
	"time"

	"github.com/katerina7479/company_town/internal/repo"
)

// makeNode builds an IssueNode with the given status and optional ClosedAt.
func makeNode(status string, closedAt *time.Time, children ...*repo.IssueNode) *repo.IssueNode {
	issue := &repo.Issue{Status: status}
	if closedAt != nil {
		issue.ClosedAt = sql.NullTime{Time: *closedAt, Valid: true}
	}
	return &repo.IssueNode{Issue: issue, Children: children}
}

func TestFilterNode(t *testing.T) {
	now := time.Now()
	cutoff := now.Add(-4 * time.Hour)
	staleTime := now.Add(-5 * time.Hour) // before cutoff → stale
	freshTime := now.Add(-1 * time.Hour) // after cutoff → not stale

	t.Run("stale leaf is removed", func(t *testing.T) {
		node := makeNode("closed", &staleTime)
		if got := filterNode(node, cutoff); got != nil {
			t.Errorf("expected nil for stale leaf, got %+v", got)
		}
	})

	t.Run("non-stale leaf is kept", func(t *testing.T) {
		node := makeNode("open", nil)
		got := filterNode(node, cutoff)
		if got == nil {
			t.Fatal("expected non-nil for non-stale leaf")
		}
		if len(got.Children) != 0 {
			t.Errorf("expected no children, got %d", len(got.Children))
		}
	})

	t.Run("recently closed leaf is kept", func(t *testing.T) {
		node := makeNode("closed", &freshTime)
		got := filterNode(node, cutoff)
		if got == nil {
			t.Fatal("expected non-nil for recently closed leaf")
		}
	})

	t.Run("stale node with live child is kept", func(t *testing.T) {
		child := makeNode("open", nil)
		parent := makeNode("closed", &staleTime, child)
		got := filterNode(parent, cutoff)
		if got == nil {
			t.Fatal("expected stale parent with live child to be kept")
		}
		if len(got.Children) != 1 {
			t.Errorf("expected 1 surviving child, got %d", len(got.Children))
		}
	})

	t.Run("non-stale node with stale child has child removed", func(t *testing.T) {
		staleChild := makeNode("closed", &staleTime)
		parent := makeNode("open", nil, staleChild)
		got := filterNode(parent, cutoff)
		if got == nil {
			t.Fatal("expected non-stale parent to be kept")
		}
		if len(got.Children) != 0 {
			t.Errorf("expected stale child removed, got %d children", len(got.Children))
		}
	})

	t.Run("original node is not mutated", func(t *testing.T) {
		staleChild := makeNode("closed", &staleTime)
		liveChild := makeNode("open", nil)
		parent := makeNode("open", nil, staleChild, liveChild)
		_ = filterNode(parent, cutoff)
		if len(parent.Children) != 2 {
			t.Errorf("original node mutated: expected 2 children, got %d", len(parent.Children))
		}
	})
}

func TestFlattenTree(t *testing.T) {
	t.Run("empty input returns nil", func(t *testing.T) {
		result := flattenTree(nil, 0)
		if result != nil {
			t.Errorf("expected nil for empty input, got %v", result)
		}
	})

	t.Run("flat list returns same order at depth 0", func(t *testing.T) {
		n1 := makeNode("open", nil)
		n2 := makeNode("in_progress", nil)
		n3 := makeNode("closed", nil)
		result := flattenTree([]*repo.IssueNode{n1, n2, n3}, 0)
		if len(result) != 3 {
			t.Fatalf("expected 3 nodes, got %d", len(result))
		}
		for i, fn := range result {
			if fn.depth != 0 {
				t.Errorf("node %d: expected depth 0, got %d", i, fn.depth)
			}
		}
		if result[0].node != n1 || result[1].node != n2 || result[2].node != n3 {
			t.Error("flat list order not preserved")
		}
	})

	t.Run("nested tree returns pre-order depth-annotated slice", func(t *testing.T) {
		child1 := makeNode("open", nil)
		child2 := makeNode("open", nil)
		grandchild := makeNode("open", nil)
		// child2 has grandchild
		child2.Children = []*repo.IssueNode{grandchild}
		root := makeNode("open", nil, child1, child2)

		result := flattenTree([]*repo.IssueNode{root}, 0)
		// Expected pre-order: root(0), child1(1), child2(1), grandchild(2)
		if len(result) != 4 {
			t.Fatalf("expected 4 nodes, got %d", len(result))
		}
		expected := []struct {
			node  *repo.IssueNode
			depth int
		}{
			{root, 0},
			{child1, 1},
			{child2, 1},
			{grandchild, 2},
		}
		for i, e := range expected {
			if result[i].node != e.node {
				t.Errorf("index %d: wrong node", i)
			}
			if result[i].depth != e.depth {
				t.Errorf("index %d: expected depth %d, got %d", i, e.depth, result[i].depth)
			}
		}
	})
}

func TestFilterStaleClosedNodes(t *testing.T) {
	now := time.Now()
	cutoff := now.Add(-4 * time.Hour)
	staleTime := now.Add(-5 * time.Hour)

	t.Run("empty input returns nil", func(t *testing.T) {
		result := filterStaleClosedNodes(nil, cutoff)
		if result != nil {
			t.Errorf("expected nil for empty input, got %v", result)
		}
	})

	t.Run("all stale roots removed", func(t *testing.T) {
		roots := []*repo.IssueNode{
			makeNode("closed", &staleTime),
			makeNode("closed", &staleTime),
		}
		result := filterStaleClosedNodes(roots, cutoff)
		if len(result) != 0 {
			t.Errorf("expected 0 roots after filtering all stale, got %d", len(result))
		}
	})

	t.Run("live roots kept, stale roots removed", func(t *testing.T) {
		roots := []*repo.IssueNode{
			makeNode("open", nil),
			makeNode("closed", &staleTime),
			makeNode("in_progress", nil),
		}
		result := filterStaleClosedNodes(roots, cutoff)
		if len(result) != 2 {
			t.Errorf("expected 2 roots, got %d", len(result))
		}
	})
}
