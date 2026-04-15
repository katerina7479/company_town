package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/katerina7479/company_town/internal/config"
)

// reviewerTestCfg returns a config pointing at a temp dir with a fake bare
// repo directory so reviewerInspectCore/reviewerInspectCleanCore can locate it.
func reviewerTestCfg(t *testing.T) *config.Config {
	t.Helper()
	root := t.TempDir()
	bareDir := filepath.Join(root, ".company_town", "repo.git")
	if err := os.MkdirAll(bareDir, 0750); err != nil {
		t.Fatalf("creating fake bare dir: %v", err)
	}
	return &config.Config{ProjectRoot: root}
}

// TestReviewerInspect_createsWorktree verifies that reviewerInspectCore calls
// gitWorktreeAddFn with the correct path and ref.
func TestReviewerInspect_createsWorktree(t *testing.T) {
	cfg := reviewerTestCfg(t)

	oldGH := ghPRBranchFn
	t.Cleanup(func() { ghPRBranchFn = oldGH })
	ghPRBranchFn = func(prNumber int) (string, error) {
		if prNumber != 42 {
			t.Errorf("expected prNumber=42, got %d", prNumber)
		}
		return "feat/test", nil
	}

	var addBarePath, addWtPath, addRef string
	oldAdd := gitWorktreeAddFn
	t.Cleanup(func() { gitWorktreeAddFn = oldAdd })
	gitWorktreeAddFn = func(barePath, wtPath, ref string) error {
		addBarePath, addWtPath, addRef = barePath, wtPath, ref
		return nil
	}

	oldRemove := gitWorktreeRemoveFn
	t.Cleanup(func() { gitWorktreeRemoveFn = oldRemove })
	gitWorktreeRemoveFn = func(_, _ string) error { return nil }

	if err := reviewerInspectCore(cfg, 42); err != nil {
		t.Fatalf("reviewerInspectCore: %v", err)
	}

	wantWtPath := prWorktreePath(cfg)
	wantRef := "origin/feat/test"

	if addWtPath != wantWtPath {
		t.Errorf("worktree path = %q, want %q", addWtPath, wantWtPath)
	}
	if addRef != wantRef {
		t.Errorf("ref = %q, want %q", addRef, wantRef)
	}
	if addBarePath == "" {
		t.Error("barePath passed to gitWorktreeAddFn should not be empty")
	}
}

// TestReviewerInspect_removesExistingPathFirst verifies that if pr-worktree
// already exists, gitWorktreeRemoveFn is called before gitWorktreeAddFn.
func TestReviewerInspect_removesExistingPathFirst(t *testing.T) {
	cfg := reviewerTestCfg(t)

	// Pre-create the worktree path to simulate a stale prior review.
	wtPath := prWorktreePath(cfg)
	if err := os.MkdirAll(wtPath, 0750); err != nil {
		t.Fatalf("pre-creating worktree path: %v", err)
	}

	oldGH := ghPRBranchFn
	t.Cleanup(func() { ghPRBranchFn = oldGH })
	ghPRBranchFn = func(_ int) (string, error) { return "main", nil }

	var callOrder []string

	oldRemove := gitWorktreeRemoveFn
	t.Cleanup(func() { gitWorktreeRemoveFn = oldRemove })
	gitWorktreeRemoveFn = func(_, _ string) error {
		callOrder = append(callOrder, "remove")
		return nil
	}

	oldAdd := gitWorktreeAddFn
	t.Cleanup(func() { gitWorktreeAddFn = oldAdd })
	gitWorktreeAddFn = func(_, _, _ string) error {
		callOrder = append(callOrder, "add")
		return nil
	}

	if err := reviewerInspectCore(cfg, 1); err != nil {
		t.Fatalf("reviewerInspectCore: %v", err)
	}

	if len(callOrder) < 2 || callOrder[0] != "remove" || callOrder[1] != "add" {
		t.Errorf("expected [remove add], got %v", callOrder)
	}
}

// TestReviewerInspect_ghFailurePropagates verifies that a gh CLI error surfaces
// with a wrapped "looking up PR branch" context.
func TestReviewerInspect_ghFailurePropagates(t *testing.T) {
	cfg := reviewerTestCfg(t)

	oldGH := ghPRBranchFn
	t.Cleanup(func() { ghPRBranchFn = oldGH })
	ghPRBranchFn = func(_ int) (string, error) {
		return "", fmt.Errorf("gh: not found")
	}

	err := reviewerInspectCore(cfg, 99)
	if err == nil {
		t.Fatal("expected error from gh failure, got nil")
	}
	if !strings.Contains(err.Error(), "looking up PR branch") {
		t.Errorf("expected wrapped context 'looking up PR branch', got: %v", err)
	}
}

// TestReviewerInspect_gitAddFailurePropagates verifies that a git worktree add
// error reaches the caller.
func TestReviewerInspect_gitAddFailurePropagates(t *testing.T) {
	cfg := reviewerTestCfg(t)

	oldGH := ghPRBranchFn
	t.Cleanup(func() { ghPRBranchFn = oldGH })
	ghPRBranchFn = func(_ int) (string, error) { return "feat/test", nil }

	oldRemove := gitWorktreeRemoveFn
	t.Cleanup(func() { gitWorktreeRemoveFn = oldRemove })
	gitWorktreeRemoveFn = func(_, _ string) error { return nil }

	oldAdd := gitWorktreeAddFn
	t.Cleanup(func() { gitWorktreeAddFn = oldAdd })
	gitWorktreeAddFn = func(_, _, _ string) error {
		return fmt.Errorf("git: failed to add worktree")
	}

	err := reviewerInspectCore(cfg, 42)
	if err == nil {
		t.Fatal("expected error from git worktree add failure, got nil")
	}
}

// TestReviewerInspectClean_absent_isNoop verifies that Clean returns nil and
// does NOT call gitWorktreeRemoveFn when the pr-worktree directory is absent.
func TestReviewerInspectClean_absent_isNoop(t *testing.T) {
	cfg := reviewerTestCfg(t)

	removed := false
	oldRemove := gitWorktreeRemoveFn
	t.Cleanup(func() { gitWorktreeRemoveFn = oldRemove })
	gitWorktreeRemoveFn = func(_, _ string) error {
		removed = true
		return nil
	}

	if err := reviewerInspectCleanCore(cfg); err != nil {
		t.Fatalf("reviewerInspectCleanCore (absent): %v", err)
	}
	if removed {
		t.Error("expected gitWorktreeRemoveFn NOT called when path absent")
	}
}

// TestReviewerInspectClean_presentWorktree_removes verifies that Clean calls
// gitWorktreeRemoveFn with the correct path when the worktree exists.
func TestReviewerInspectClean_presentWorktree_removes(t *testing.T) {
	cfg := reviewerTestCfg(t)

	wtPath := prWorktreePath(cfg)
	if err := os.MkdirAll(wtPath, 0750); err != nil {
		t.Fatalf("pre-creating worktree: %v", err)
	}

	var removedPath string
	oldRemove := gitWorktreeRemoveFn
	t.Cleanup(func() { gitWorktreeRemoveFn = oldRemove })
	gitWorktreeRemoveFn = func(_, path string) error {
		removedPath = path
		return nil
	}

	if err := reviewerInspectCleanCore(cfg); err != nil {
		t.Fatalf("reviewerInspectCleanCore: %v", err)
	}
	if removedPath != wtPath {
		t.Errorf("expected gitWorktreeRemoveFn called with %q, got %q", wtPath, removedPath)
	}
}

// TestReviewerInspectClean_gitRemoveFails_fallsBackToRemoveAll verifies that
// when gitWorktreeRemoveFn errors, the directory is cleaned up via os.RemoveAll.
func TestReviewerInspectClean_gitRemoveFails_fallsBackToRemoveAll(t *testing.T) {
	cfg := reviewerTestCfg(t)

	wtPath := prWorktreePath(cfg)
	if err := os.MkdirAll(wtPath, 0750); err != nil {
		t.Fatalf("pre-creating worktree: %v", err)
	}

	oldRemove := gitWorktreeRemoveFn
	t.Cleanup(func() { gitWorktreeRemoveFn = oldRemove })
	gitWorktreeRemoveFn = func(_, _ string) error {
		return fmt.Errorf("git: cannot remove dirty worktree")
	}

	if err := reviewerInspectCleanCore(cfg); err != nil {
		t.Fatalf("reviewerInspectCleanCore (fallback): %v", err)
	}

	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("expected pr-worktree dir to be gone after fallback RemoveAll, but it still exists")
	}
}
