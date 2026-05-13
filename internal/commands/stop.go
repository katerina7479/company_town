package commands

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/eventlog"
	"github.com/katerina7479/company_town/internal/repo"
	"github.com/katerina7479/company_town/internal/session"
)

// ArtisanStop implements `ct artisan <specialty> stop` — graceful Artisan shutdown.
func ArtisanStop(specialty string) error {
	projectRoot, err := db.FindProjectRoot()
	if err != nil {
		return err
	}
	if cfg, cfgErr := config.Load(projectRoot); cfgErr == nil {
		applySessionPrefix(cfg)
	}
	ctDir := config.CompanyTownDir(projectRoot)

	name := fmt.Sprintf("artisan-%s", specialty)
	sessionName := session.SessionName(name)

	if !session.Exists(sessionName) {
		fmt.Printf("artisan-%s is not running.\n", specialty)
		return nil
	}

	fmt.Printf("Signaling %s artisan to write handoff and exit...\n", specialty)

	signalPath := filepath.Join(ctDir, "agents", "artisan", specialty, "memory", "handoff_requested")
	if err := os.WriteFile(signalPath, []byte("handoff requested\n"), 0644); err != nil { //nolint:gosec // signalPath is derived from project config, not user input
		return fmt.Errorf("writing handoff signal: %w", err)
	}

	session.SendKeys(sessionName, fmt.Sprintf("Check for handoff_requested in your memory directory, write handoff.md, run `gt agent status %s stopped`, then exit.", name)) //nolint:errcheck // fire-and-forget signal to agent

	fmt.Printf("Handoff signal sent. artisan-%s will exit after writing handoff.md.\n", specialty)
	return nil
}

// ArchitectStop implements `ct architect stop` — graceful Architect shutdown.
func ArchitectStop() error {
	projectRoot, err := db.FindProjectRoot()
	if err != nil {
		return err
	}
	if cfg, cfgErr := config.Load(projectRoot); cfgErr == nil {
		applySessionPrefix(cfg)
	}
	ctDir := config.CompanyTownDir(projectRoot)

	sessionName := session.SessionName("architect")

	if !session.Exists(sessionName) {
		fmt.Println("Architect is not running.")
		return nil
	}

	fmt.Println("Signaling Architect to write handoff and exit...")

	signalPath := filepath.Join(ctDir, "agents", "architect", "memory", "handoff_requested")
	if err := os.WriteFile(signalPath, []byte("handoff requested\n"), 0644); err != nil {
		return fmt.Errorf("writing handoff signal: %w", err)
	}

	session.SendKeys(sessionName, "Check for handoff_requested in your memory directory, write handoff.md, run `gt agent status architect stopped`, then exit.") //nolint:errcheck // fire-and-forget signal to agent

	fmt.Println("Handoff signal sent. Architect will exit after writing handoff.md.")
	return nil
}

// Stop implements `ct stop [target] [--clean]`. When target is "", it stops
// every Company Town session (full shutdown, today's behavior). When target
// names a specific agent ("daemon", "architect", "reviewer",
// "artisan-<specialty>", "prole-<name>"), it stops only that session and
// leaves everything else running.
// When clean is true, worktree directories for stopped prole sessions are removed.
func Stop(target string, clean bool) error {
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

	sessions, err := session.ListCompanyTown()
	if err != nil {
		return err
	}

	if len(sessions) == 0 {
		fmt.Println("No Company Town sessions running.")
		return nil
	}

	if target != "" {
		wanted := session.SessionName(target)
		filtered := filterSessions(sessions, wanted)
		if len(filtered) == 0 {
			return fmt.Errorf("no running session matches target %q (expected %s)", target, wanted)
		}
		sessions = filtered
		fmt.Printf("Targeted stop: %s\n", wanted)
	}

	if clean {
		if target != "" && !strings.HasPrefix(target, "prole-") {
			fmt.Println("note: --clean has no effect on non-prole targets")
		} else {
			fmt.Println("--clean: prole worktrees will be removed immediately after signaling.")
			fmt.Println("         Agents will NOT get time to finish in-flight commits.")
		}
	}

	conn, _, connErr := db.OpenFromWorkingDir()
	var updateStatus func(string, string) error
	var getStatus func(string) (string, error)
	if connErr == nil {
		stopEvents := eventlog.NewLogger(ctDir)
		agentRepo := repo.NewAgentRepo(conn, stopEvents)
		updateStatus = agentRepo.UpdateStatus
		getStatus = func(name string) (string, error) {
			a, err := agentRepo.Get(name)
			if err != nil {
				return "", err
			}
			return a.Status, nil
		}
		defer conn.Close()
	}

	stopCore(sessions, ctDir, clean, session.Kill, session.SendKeys, updateStatus, os.RemoveAll, gitWorktreePrune, getStatus, 60*time.Second)

	if target == "" {
		fmt.Println("\nHandoff signals sent. Agents will exit after saving state.")
		fmt.Println("Run `ct nuke` if you need to force-kill all sessions.")
	} else {
		fmt.Printf("\nHandoff signal sent to %s.\n", target)
	}
	return nil
}

// filterSessions returns only the sessions in all whose name equals wanted.
// Exact match — no prefix matching, since session names like "ct-prole-copper"
// should not match target "prole" by accident.
func filterSessions(all []string, wanted string) []string {
	for _, s := range all {
		if s == wanted {
			return []string{s}
		}
	}
	return nil
}

// stopCore is the testable shutdown logic used by Stop.
// updateStatus and getStatus may be nil when the DB is unavailable.
// When clean is true, worktree directories for prole sessions are removed after signaling.
// When getStatus is non-nil and clean is false, stopCore polls each signaled agent until
// it sets its own status to "stopped" (graceful) or stopTimeout expires (warning, no kill).
func stopCore(sessions []string, ctDir string, clean bool, killFn func(string) error, sendKeysFn func(string, string) error, updateStatus func(string, string) error, removeAll func(string) error, worktreePrune func(string) error, getStatus func(string) (string, error), stopTimeout time.Duration) {
	var cleanedAny bool
	for _, s := range sessions {
		agentName := s[len(session.SessionPrefix):]

		switch {
		case agentName == "daemon":
			if err := killFn(s); err != nil {
				fmt.Printf("  error stopping daemon: %v\n", err)
			} else {
				fmt.Printf("  stopped: %s\n", s)
				if updateStatus != nil {
					updateStatus("daemon", "dead") //nolint:errcheck // best-effort status update during shutdown
				}
			}
			continue
		case agentName == "architect":
			signalPath := filepath.Join(ctDir, "agents", "architect", "memory", "handoff_requested")
			os.WriteFile(signalPath, []byte("handoff requested\n"), 0644)                                                                            //nolint:errcheck // best-effort signal file write
			sendKeysFn(s, fmt.Sprintf("System is shutting down. Write handoff.md, run `gt agent status %s stopped`, then exit cleanly.", agentName)) //nolint:errcheck // fire-and-forget shutdown signal
		case agentName == "mayor":
			mayorSignalPath := filepath.Join(ctDir, "agents", "mayor", "memory", "stop_requested")
			os.WriteFile(mayorSignalPath, []byte("stop requested\n"), 0644)                                                                        //nolint:errcheck // best-effort signal file write
			sendKeysFn(s, fmt.Sprintf("System is shutting down. Save any state, run `gt agent status %s stopped`, then exit cleanly.", agentName)) //nolint:errcheck // fire-and-forget shutdown signal
		case strings.HasPrefix(agentName, "artisan-"):
			specialty := strings.TrimPrefix(agentName, "artisan-")
			signalPath := filepath.Join(ctDir, "agents", "artisan", specialty, "memory", "handoff_requested")
			os.WriteFile(signalPath, []byte("handoff requested\n"), 0644)                                                                            //nolint:errcheck // best-effort signal file write
			sendKeysFn(s, fmt.Sprintf("System is shutting down. Write handoff.md, run `gt agent status %s stopped`, then exit cleanly.", agentName)) //nolint:errcheck // fire-and-forget shutdown signal
		default:
			if strings.HasPrefix(agentName, "prole-") {
				proleName := strings.TrimPrefix(agentName, "prole-")
				proleSignalPath := filepath.Join(ctDir, "proles", proleName, "stop_requested")
				os.WriteFile(proleSignalPath, []byte("stop requested\n"), 0644) //nolint:errcheck // best-effort signal file write
			}
			sendKeysFn(s, fmt.Sprintf("System is shutting down. Commit and push any work, run `gt agent status %s stopped`, then exit.", agentName)) //nolint:errcheck // fire-and-forget shutdown signal
			if clean && strings.HasPrefix(agentName, "prole-") {
				proleName := strings.TrimPrefix(agentName, "prole-")
				worktreeDir := filepath.Join(ctDir, "proles", proleName)
				if err := removeAll(worktreeDir); err != nil {
					fmt.Printf("  error removing worktree for %s: %v\n", proleName, err)
				} else {
					fmt.Printf("  removed worktree: %s\n", worktreeDir)
					cleanedAny = true
				}
			}
		}

		fmt.Printf("  signaled: %s\n", s)

		// When DB is unavailable or --clean removes the workspace, fall back to
		// marking idle immediately (old behavior). Otherwise, poll until the agent
		// sets its own status to "stopped", then kill the session cleanly.
		shouldWait := getStatus != nil && !clean
		if !shouldWait {
			if updateStatus != nil {
				updateStatus(agentName, repo.StatusIdle) //nolint:errcheck // best-effort status update during shutdown
			}
			continue
		}

		effectiveTimeout := stopTimeout
		if effectiveTimeout == 0 {
			effectiveTimeout = 60 * time.Second
		}
		deadline := time.Now().Add(effectiveTimeout)
		reached := false
		for {
			status, err := getStatus(agentName)
			if err != nil {
				fmt.Printf("  warning: status read for %s failed: %v\n", agentName, err)
			} else if status == repo.StatusStopped {
				reached = true
				break
			}
			if time.Now().After(deadline) {
				break
			}
			time.Sleep(2 * time.Second)
		}
		if reached {
			if err := killFn(s); err != nil {
				fmt.Printf("  error killing %s after stopped signal: %v\n", agentName, err)
			} else {
				fmt.Printf("  killed (graceful): %s\n", s)
				if updateStatus != nil {
					updateStatus(agentName, repo.StatusDead) //nolint:errcheck // best-effort status update during shutdown
				}
			}
		} else {
			fmt.Printf("  warning: agent %s did not reach 'stopped' within %s — run 'ct nuke %s' to force-kill\n",
				agentName, effectiveTimeout, agentName)
		}
	}

	if clean && cleanedAny {
		repoGit := filepath.Join(ctDir, "repo.git")
		if err := worktreePrune(repoGit); err != nil {
			fmt.Printf("  error pruning worktrees: %v\n", err)
		}
	}
}
