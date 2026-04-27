// Package agentworktree manages isolated git worktrees for non-prole agents
// (architect, mayor, reviewer, artisan-*).
//
// Proles already have their own worktrees under .company_town/proles/<name>/.
// Non-prole agents share a single checkout (cfg.ProjectRoot) which means a
// `git checkout` in one agent's session silently moves HEAD in every other
// agent's session — the "shared-checkout branch-switch ghost". This package
// ends that by giving each named agent a dedicated worktree.
//
// Layout:
//
//	.company_town/agents/<role>/             ← existing agent dir (CLAUDE.md, memory/)
//	.company_town/agents/<role>/worktree/    ← new per-agent git worktree
//
// The worktree sub-directory is placed INSIDE the agent dir so that Claude
// Code, starting in the worktree, walks up and discovers the CLAUDE.md in the
// parent automatically.
//
// All agent worktrees share the same bare clone as proles
// (.company_town/repo.git). Worktrees are created in detached HEAD state at
// origin/main; agents can then freely `git checkout origin/<branch>` without
// creating local branches that would persist across sessions.
package agentworktree

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/prole"
)

// Path returns the worktree path for an agent whose home directory is agentDir.
// The worktree is always the "worktree" sub-directory of agentDir.
func Path(agentDir string) string {
	return filepath.Join(agentDir, "worktree")
}

// Ensure creates the worktree at agentDir/worktree/ if it does not exist, or
// refreshes remote tracking refs in the shared bare repo if it does. Returns
// the worktree path. Uses the same bare clone (.company_town/repo.git) as
// prole worktrees.
func Ensure(cfg *config.Config, agentDir string) (string, error) {
	// Ensure the shared bare clone exists and has current remote refs.
	if err := prole.EnsureBareRepo(cfg); err != nil {
		return "", fmt.Errorf("ensuring bare repo: %w", err)
	}

	wtPath := Path(agentDir)

	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		if err := createWorktree(prole.BareRepoPath(cfg), wtPath); err != nil {
			return "", fmt.Errorf("creating worktree at %s: %w", wtPath, err)
		}
		// Install the pre-commit hook so gofmt checks fire in agent worktrees.
		prole.InstallPreCommitHook(cfg.ProjectRoot, wtPath)
	} else {
		// Worktree already exists — fetch origin so the agent's session starts
		// with up-to-date remote tracking refs.
		fetchCmd := exec.Command("git", "fetch", "origin")
		fetchCmd.Dir = prole.BareRepoPath(cfg)
		fetchCmd.Stdout = os.Stdout
		fetchCmd.Stderr = os.Stderr
		if err := fetchCmd.Run(); err != nil {
			// Non-fatal: stale remote refs are a nuisance, not a hard failure.
			fmt.Fprintf(os.Stderr, "warning: fetch for agent at %s: %v\n", agentDir, err)
		}
	}

	return wtPath, nil
}

// createWorktree adds a new git worktree at wtPath in detached HEAD state at
// origin/main. The parent directory is created if needed.
func createWorktree(barePath, wtPath string) error {
	if err := os.MkdirAll(filepath.Dir(wtPath), 0750); err != nil {
		return fmt.Errorf("creating parent dir for worktree: %w", err)
	}
	if err := prole.RequireOriginMain(barePath); err != nil {
		return err
	}
	cmd := exec.Command("git", "worktree", "add", "--detach", wtPath, "origin/main")
	cmd.Dir = barePath
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
