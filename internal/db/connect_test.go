package db

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFindProjectRoot_fromAgentWorktreeLayout verifies that FindProjectRoot
// works correctly when called from inside an agent worktree directory.
//
// After nc-128, agents run in isolated git worktrees placed at:
//
//	<project>/.company_town/agents/<role>/worktree/
//
// FindProjectRoot must walk up from this CWD and discover the .company_town/
// directory at the project root, not confuse the worktree-local path for the
// root. This test guards against regressions where the walker stops too early
// or overshoots.
func TestFindProjectRoot_fromAgentWorktreeLayout(t *testing.T) {
	// Build a temp directory tree that mirrors the per-agent worktree layout:
	//   <root>/
	//     .company_town/
	//       agents/
	//         architect/
	//           worktree/   ← simulated CWD inside the worktree
	root := t.TempDir()
	ctDir := filepath.Join(root, ".company_town")
	worktreeDir := filepath.Join(ctDir, "agents", "architect", "worktree")

	if err := os.MkdirAll(worktreeDir, 0750); err != nil {
		t.Fatalf("creating worktree dir: %v", err)
	}

	// Change into the simulated worktree CWD.
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(origDir) //nolint:errcheck // best-effort restore in test cleanup
	if err := os.Chdir(worktreeDir); err != nil {
		t.Fatalf("chdir to worktree: %v", err)
	}

	got, err := FindProjectRoot()
	if err != nil {
		t.Fatalf("FindProjectRoot() from agent worktree: %v", err)
	}

	// Resolve symlinks on both sides: macOS /var → /private/var means TempDir
	// and os.Getwd() can return different forms of the same path.
	wantReal, _ := filepath.EvalSymlinks(root)
	gotReal, _ := filepath.EvalSymlinks(got)
	if gotReal != wantReal {
		t.Errorf("FindProjectRoot() = %q (real: %q), want %q (project root)", got, gotReal, wantReal)
	}
}

// TestFindProjectRoot_fromProjectRoot verifies the base case: running from the
// project root itself.
func TestFindProjectRoot_fromProjectRoot(t *testing.T) {
	root := t.TempDir()
	ctDir := filepath.Join(root, ".company_town")
	if err := os.MkdirAll(ctDir, 0750); err != nil {
		t.Fatalf("creating .company_town: %v", err)
	}

	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(origDir) //nolint:errcheck // best-effort restore in test cleanup
	if err := os.Chdir(root); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	got, err := FindProjectRoot()
	if err != nil {
		t.Fatalf("FindProjectRoot() from project root: %v", err)
	}
	wantReal, _ := filepath.EvalSymlinks(root)
	gotReal, _ := filepath.EvalSymlinks(got)
	if gotReal != wantReal {
		t.Errorf("FindProjectRoot() = %q, want %q", gotReal, wantReal)
	}
}

// TestFindProjectRoot_noCompanyTown verifies that FindProjectRoot returns an
// error when no .company_town/ directory exists anywhere in the ancestor chain.
func TestFindProjectRoot_noCompanyTown(t *testing.T) {
	// Use a temp dir with no .company_town/ anywhere in its hierarchy.
	bare := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(origDir) //nolint:errcheck // best-effort restore in test cleanup
	if err := os.Chdir(bare); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	_, err = FindProjectRoot()
	if err == nil {
		t.Errorf("FindProjectRoot() expected error outside a company town project, got nil")
	}
}
