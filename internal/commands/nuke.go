package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/eventlog"
	"github.com/katerina7479/company_town/internal/repo"
	"github.com/katerina7479/company_town/internal/session"
)

// Nuke implements `ct nuke` — immediate shutdown, no handoffs.
// target restricts the operation to a single component (see nukeCore).
// An empty target performs the default full teardown.
func Nuke(target string) error {
	// Load config first so session.SessionPrefix is set before ListCompanyTown.
	projectRoot, err := db.FindProjectRoot()
	if err != nil {
		return err
	}
	cfg, cfgErr := config.Load(projectRoot)
	if cfgErr == nil {
		applySessionPrefix(cfg)
	}
	ctDir := config.CompanyTownDir(projectRoot)

	// "bare" target removes only the bare clone — no sessions involved.
	if target == "bare" {
		nukeCore(nil, ctDir, target, session.Kill, nil, os.RemoveAll, gitWorktreePrune)
		fmt.Println("\nBare clone removed.")
		return nil
	}

	sessions, err := session.ListCompanyTown()
	if err != nil {
		return err
	}

	if len(sessions) == 0 && target == "" {
		fmt.Println("No Company Town sessions running.")
		return nil
	}

	conn, _, connErr := db.OpenFromWorkingDir()

	var updateStatus func(string, string) error
	if connErr == nil {
		nukeEvents := eventlog.NewLogger(ctDir)
		agents := repo.NewAgentRepo(conn, nukeEvents)
		updateStatus = agents.UpdateStatus
		defer conn.Close()
	}

	killed := nukeCore(sessions, ctDir, target, session.Kill, updateStatus, os.RemoveAll, gitWorktreePrune)

	switch {
	case target == "":
		if killed > 0 {
			fmt.Println("\nAll sessions killed.")
		}
	case killed == 0:
		// nukeCore already printed "no running session for target %q" — don't
		// add a misleading follow-up.
	case killed == 1:
		fmt.Printf("\n%s killed.\n", target)
	default:
		// Unreachable for named targets (nukeCore filters to at most one session
		// matching the exact name), but print a safe summary instead of a lie.
		fmt.Printf("\n%d sessions matching %q killed.\n", killed, target)
	}
	return nil
}

// nukeCore is the testable kill logic used by Nuke.
// updateStatus may be nil when the DB is unavailable.
//
// target controls scope:
//   - "" (empty): full teardown — kill all sessions, remove all worktrees,
//     prune bare worktrees, then remove the bare clone.
//   - "bare": remove the bare clone only; sessions slice is ignored.
//   - any other value (e.g. "prole-copper", "architect"): kill only the session
//     whose name is ct-<target>, remove its worktree, and run git worktree prune.
//     The bare clone is NOT removed in single-target mode (it is shared by all
//     proles; removing it while other proles are running would break them).
//
// nukeCore returns the number of sessions actually killed. The caller uses
// this to decide whether to print a "killed" summary line.
func nukeCore(sessions []string, ctDir string, target string, killFn func(string) error, updateStatus func(string, string) error, removeAll func(string) error, worktreePrune func(string) error) int {
	// "bare" target: remove only the bare clone, no sessions to kill.
	if target == "bare" {
		repoGit := filepath.Join(ctDir, "repo.git")
		if err := removeAll(repoGit); err != nil {
			fmt.Printf("  error removing bare clone: %v\n", err)
		} else {
			fmt.Printf("  removed bare clone: %s\n", repoGit)
		}
		return 0
	}

	// Named target: restrict to the single matching session.
	if target != "" {
		sessionName := session.SessionPrefix + target
		var filtered []string
		for _, s := range sessions {
			if s == sessionName {
				filtered = append(filtered, s)
			}
		}
		if len(filtered) == 0 {
			fmt.Printf("  no running session for target %q\n", target)
		}
		sessions = filtered
	}

	fullTeardown := target == ""

	var cleanedAny bool
	killedCount := 0
	for _, s := range sessions {
		agentName := s[len(session.SessionPrefix):]
		if err := killFn(s); err != nil {
			fmt.Printf("  error killing %s: %v\n", s, err)
		} else {
			fmt.Printf("  killed: %s\n", s)
			killedCount++

			var worktreeDir string
			switch {
			case strings.HasPrefix(agentName, "prole-"):
				proleName := strings.TrimPrefix(agentName, "prole-")
				worktreeDir = filepath.Join(ctDir, "proles", proleName)
			default:
				worktreeDir = agentWorktreePath(ctDir, agentName)
			}

			if worktreeDir != "" {
				if err := removeAll(worktreeDir); err != nil {
					fmt.Printf("  error removing worktree for %s: %v\n", agentName, err)
				} else {
					fmt.Printf("  removed worktree: %s\n", worktreeDir)
					cleanedAny = true
				}
			}
		}

		if updateStatus != nil {
			updateStatus(agentName, "dead") //nolint:errcheck // best-effort status update during shutdown
		}
	}

	if cleanedAny {
		repoGit := filepath.Join(ctDir, "repo.git")
		if err := worktreePrune(repoGit); err != nil {
			fmt.Printf("  error pruning worktrees: %v\n", err)
		}
		// Remove the bare clone only on full teardown — targeted nukes leave it
		// intact because other proles may still be using it.
		if fullTeardown {
			if err := removeAll(repoGit); err != nil {
				fmt.Printf("  error removing bare clone: %v\n", err)
			} else {
				fmt.Printf("  removed bare clone: %s\n", repoGit)
			}
		}
	}
	return killedCount
}

// agentWorktreePath returns the worktree directory for a named non-prole agent,
// or "" if the agent has no worktree (e.g. daemon).
func agentWorktreePath(ctDir, agentName string) string {
	switch {
	case agentName == "architect":
		return filepath.Join(ctDir, "agents", "architect", "worktree")
	case agentName == "mayor":
		return filepath.Join(ctDir, "agents", "mayor", "worktree")
	case agentName == "reviewer":
		return filepath.Join(ctDir, "agents", "reviewer", "worktree")
	case strings.HasPrefix(agentName, "artisan-"):
		specialty := strings.TrimPrefix(agentName, "artisan-")
		return filepath.Join(ctDir, "agents", "artisan", specialty, "worktree")
	default:
		return ""
	}
}

// gitWorktreePrune runs `git worktree prune` against the given bare repo path.
func gitWorktreePrune(repoGitPath string) error {
	cmd := exec.Command("git", "-C", repoGitPath, "worktree", "prune")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
