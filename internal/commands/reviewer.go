package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/prole"
	"github.com/katerina7479/company_town/internal/vcs"
)

// reviewerVCSProvider is the VCS platform adapter for reviewer commands.
var reviewerVCSProvider vcs.Provider = vcs.NewGitHub()

// Package-level vars for injection in tests.
var ghPRBranchFn func(prNumber int, repoDir string) (string, error) = func(prNumber int, repoDir string) (string, error) {
	return reviewerVCSProvider.GetPRHeadBranch(prNumber, repoDir)
}
var gitFetchFn func(barePath, branch string) error = gitFetch
var gitWorktreeAddFn func(barePath, wtPath, ref string) error = gitWorktreeAdd
var gitWorktreeRemoveFn func(barePath, wtPath string) error = gitWorktreeRemove

// prWorktreePath returns the path to the PR inspection worktree.
func prWorktreePath(cfg *config.Config) string {
	ctDir := config.CompanyTownDir(cfg.ProjectRoot)
	return filepath.Join(ctDir, "agents", "reviewer", "pr-worktree")
}

// ReviewerInspect sets up a dedicated git worktree for PR inspection at
// .company_town/agents/reviewer/pr-worktree/, checked out to origin/<branch>
// where <branch> is the PR's head ref. If the path already exists (e.g. a
// prior review was not cleaned up), it is removed first so the new add is
// clean.
//
// Prints the absolute path to stdout on success so the caller can cd there.
func ReviewerInspect(prNumber int) error {
	_, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	return reviewerInspectCore(cfg, prNumber)
}

// ReviewerInspectClean removes the PR inspection worktree if present.
// Idempotent — absent path is not an error.
func ReviewerInspectClean() error {
	_, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	return reviewerInspectCleanCore(cfg)
}

// reviewerInspectCore is the injectable implementation of ReviewerInspect.
func reviewerInspectCore(cfg *config.Config, prNumber int) error {
	branch, err := ghPRBranchFn(prNumber, cfg.ProjectRoot)
	if err != nil {
		return fmt.Errorf("looking up PR branch: %w", err)
	}

	barePath := prole.BareRepoPath(cfg)
	if _, err := os.Stat(barePath); os.IsNotExist(err) {
		return fmt.Errorf("bare clone not found at %s — run `gt start reviewer` to initialize it", barePath)
	}

	// Fetch the branch from origin so origin/<branch> resolves in the bare
	// clone. Without this, git worktree add fails with a ref-not-found error
	// when the branch was pushed after the last fetch (nc-206).
	if err := gitFetchFn(barePath, branch); err != nil {
		return fmt.Errorf("fetching origin/%s: %w", branch, err)
	}

	wtPath := prWorktreePath(cfg)

	// Remove stale worktree if present so the add is always clean.
	if _, err := os.Stat(wtPath); err == nil {
		if rmErr := gitWorktreeRemoveFn(barePath, wtPath); rmErr != nil {
			// Fallback: force-remove directory then prune the index entry.
			os.RemoveAll(wtPath) //nolint:errcheck // best-effort fallback cleanup
			pruneWorktrees(barePath)
		}
	}

	ref := "origin/" + branch
	if err := gitWorktreeAddFn(barePath, wtPath, ref); err != nil {
		return fmt.Errorf("adding PR worktree for %s: %w", ref, err)
	}

	fmt.Println(wtPath)
	return nil
}

// reviewerInspectCleanCore is the injectable implementation of ReviewerInspectClean.
func reviewerInspectCleanCore(cfg *config.Config) error {
	wtPath := prWorktreePath(cfg)
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		return nil // already gone
	}

	barePath := prole.BareRepoPath(cfg)
	if err := gitWorktreeRemoveFn(barePath, wtPath); err != nil {
		// Fallback: force-remove the directory tree and prune the index entry.
		if removeErr := os.RemoveAll(wtPath); removeErr != nil {
			return fmt.Errorf("removing PR worktree (fallback): %w", removeErr)
		}
		pruneWorktrees(barePath)
	}

	return nil
}

// gitFetch runs `git fetch origin <branch>` from the bare repo to ensure
// origin/<branch> resolves before git worktree add is called.
func gitFetch(barePath, branch string) error {
	cmd := exec.Command("git", "fetch", "origin", branch)
	cmd.Dir = barePath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// gitWorktreeAdd runs `git worktree add --detach <wtPath> <ref>` from the bare repo.
func gitWorktreeAdd(barePath, wtPath, ref string) error {
	cmd := exec.Command("git", "worktree", "add", "--detach", wtPath, ref)
	cmd.Dir = barePath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// gitWorktreeRemove runs `git worktree remove --force <wtPath>` from the bare repo.
func gitWorktreeRemove(barePath, wtPath string) error {
	cmd := exec.Command("git", "worktree", "remove", "--force", wtPath)
	cmd.Dir = barePath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// pruneWorktrees runs `git worktree prune` from the bare repo to clean up stale entries.
func pruneWorktrees(barePath string) {
	cmd := exec.Command("git", "worktree", "prune")
	cmd.Dir = barePath
	cmd.Run() //nolint:errcheck // best-effort prune; non-fatal
}
