package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/katerina7479/company_town/internal/agentworktree"
	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/eventlog"
	"github.com/katerina7479/company_town/internal/repo"
	"github.com/katerina7479/company_town/internal/session"
)

// Package-level vars for injection in tests.
var sessionExistsFn func(string) bool = session.Exists
var sessionAttachFn func(string) error = session.Attach

// startAgent is the shared logic for launching any agent in a tmux session.
func startAgent(name, agentType, model string, cfg *config.Config, agents *repo.AgentRepo, prompt string) error {
	sessionName := session.SessionName(name)

	// If session already exists, just attach. Reset dead status if needed so
	// the dashboard reflects the live session.
	if sessionExistsFn(sessionName) {
		if existing, getErr := agents.Get(name); getErr == nil && existing.Status == repo.StatusDead {
			if updateErr := agents.UpdateStatus(name, repo.StatusIdle); updateErr != nil {
				fmt.Printf("warning: could not reset %s status from dead to idle: %v\n", name, updateErr)
			}
		}
		fmt.Printf("%s is already running, attaching...\n", name)
		return sessionAttachFn(sessionName)
	}

	// Register in DB if not already registered
	if _, err := agents.Get(name); err != nil {
		if regErr := agents.Register(name, agentType, nil); regErr != nil {
			return fmt.Errorf("registering %s: %w", name, regErr)
		}
	}
	if err := agents.UpdateStatus(name, "working"); err != nil {
		return fmt.Errorf("updating %s status: %w", name, err)
	}
	if err := agents.SetTmuxSession(name, sessionName); err != nil {
		return fmt.Errorf("recording tmux session for %s: %w", name, err)
	}

	ctDir := config.CompanyTownDir(cfg.ProjectRoot)
	agentDir := filepath.Join(ctDir, "agents", agentType)

	// Provision an isolated git worktree so this agent's branch checkouts
	// cannot affect other agents' views of HEAD.
	wtPath, err := agentworktree.Ensure(cfg, agentDir)
	if err != nil {
		return fmt.Errorf("setting up worktree for %s: %w", name, err)
	}

	fmt.Printf("Starting %s...\n", name)
	err = session.CreateInteractive(session.AgentSessionConfig{
		Name:     sessionName,
		WorkDir:  wtPath,
		Model:    model,
		AgentDir: agentDir,
		Prompt:   prompt,
		EnvVars:  map[string]string{"CT_AGENT_NAME": name},
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s started. Attaching...\n", name)
	return session.Attach(sessionName)
}

// Start implements `ct start` — starts the Daemon and Mayor, attaches to Mayor's tmux session.
func Start() error {
	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	if err := config.ValidateForStart(cfg); err != nil {
		return err
	}

	events := eventlog.NewLogger(config.CompanyTownDir(cfg.ProjectRoot))
	agents := repo.NewAgentRepo(conn, events)

	// Register daemon in DB if not already present.
	if _, err := agents.Get("daemon"); err != nil {
		if regErr := agents.Register("daemon", "daemon", nil); regErr != nil {
			return fmt.Errorf("registering daemon: %w", regErr)
		}
	}

	// Start daemon if not already running.
	daemonSession := session.SessionName("daemon")
	if !session.Exists(daemonSession) {
		fmt.Println("Starting daemon...")
		if err := startDaemon(cfg); err != nil {
			return fmt.Errorf("starting daemon: %w", err)
		}
		if err := agents.UpdateStatus("daemon", "working"); err != nil {
			return fmt.Errorf("updating daemon status: %w", err)
		}
	} else {
		fmt.Println("Daemon already running.")
		if err := agents.UpdateStatus("daemon", "working"); err != nil {
			return fmt.Errorf("updating daemon status: %w", err)
		}
	}
	if err := agents.SetTmuxSession("daemon", daemonSession); err != nil {
		return fmt.Errorf("recording daemon tmux session: %w", err)
	}

	prompt := fmt.Sprintf(
		"You are the Mayor. Ticket prefix: %s. "+
			"Read your CLAUDE.md for instructions, then run `gt status` to check the system.",
		cfg.TicketPrefix,
	)

	return startAgent("mayor", "mayor", cfg.Agents.Mayor.Model, cfg, agents, prompt)
}

// startDaemon launches the daemon in a detached tmux session.
func startDaemon(cfg *config.Config) error {
	name := session.SessionName("daemon")

	cmd := exec.Command("tmux", "new-session",
		"-d",
		"-s", name,
		"-c", cfg.ProjectRoot,
		"ct daemon",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return err
	}
	_ = session.ApplyStatusBar(name, "daemon")
	return nil
}

// Architect implements `ct architect` — starts the Architect agent.
func Architect() error {
	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	events := eventlog.NewLogger(config.CompanyTownDir(cfg.ProjectRoot))
	agents := repo.NewAgentRepo(conn, events)
	prompt := fmt.Sprintf(
		"You are the Architect. Ticket prefix: %s. "+
			"Read your CLAUDE.md for instructions. "+
			"Check memory/handoff.md to resume previous work. "+
			"Begin your patrol loop: check for draft tickets and spec them out.",
		cfg.TicketPrefix,
	)

	return startAgent("architect", "architect", cfg.Agents.Architect.Model, cfg, agents, prompt)
}

// Artisan implements `ct artisan <specialty>` — starts an Artisan agent.
// Specialties are user-defined in config.json under agents.artisan.<specialty>.
func Artisan(specialty string) error {
	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	// Look up specialty in config
	artisanCfg, ok := cfg.Agents.Artisan[specialty]
	if !ok {
		// List available specialties for error message
		var available []string
		for k := range cfg.Agents.Artisan {
			available = append(available, k)
		}
		return fmt.Errorf("unknown specialty %q (available in config: %v)", specialty, available)
	}

	events := eventlog.NewLogger(config.CompanyTownDir(cfg.ProjectRoot))
	agents := repo.NewAgentRepo(conn, events)
	name := fmt.Sprintf("artisan-%s", specialty)

	prompt := fmt.Sprintf(
		"You are a %s Artisan. Ticket prefix: %s. "+
			"Read your CLAUDE.md for instructions. "+
			"Check memory/handoff.md to resume previous work. "+
			"Then check for assigned tickets with `gt ticket list --status in_progress`.",
		specialty, cfg.TicketPrefix,
	)

	// Register with specialty
	if _, err := agents.Get(name); err != nil {
		spec := specialty
		if regErr := agents.Register(name, "artisan", &spec); regErr != nil {
			return fmt.Errorf("registering %s: %w", name, regErr)
		}
	}

	ctDir := config.CompanyTownDir(cfg.ProjectRoot)
	agentDir := filepath.Join(ctDir, "agents", "artisan", specialty)

	// Ensure the artisan directory exists
	if err := os.MkdirAll(filepath.Join(agentDir, "memory"), 0755); err != nil {
		return fmt.Errorf("creating artisan directory: %w", err)
	}

	sessionName := session.SessionName(name)

	if session.Exists(sessionName) {
		fmt.Printf("%s is already running, attaching...\n", name)
		return session.Attach(sessionName)
	}

	if err := agents.UpdateStatus(name, "working"); err != nil {
		return fmt.Errorf("updating %s status: %w", name, err)
	}
	if err := agents.SetTmuxSession(name, sessionName); err != nil {
		return fmt.Errorf("recording tmux session for %s: %w", name, err)
	}

	// Provision an isolated git worktree for the artisan.
	artisanWtPath, wtErr := agentworktree.Ensure(cfg, agentDir)
	if wtErr != nil {
		return fmt.Errorf("setting up worktree for %s: %w", name, wtErr)
	}

	fmt.Printf("Starting %s...\n", name)
	err = session.CreateInteractive(session.AgentSessionConfig{
		Name:     sessionName,
		WorkDir:  artisanWtPath,
		Model:    artisanCfg.Model,
		AgentDir: agentDir,
		Prompt:   prompt,
		EnvVars:  map[string]string{"CT_AGENT_NAME": name},
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s started. Attaching...\n", name)
	return session.Attach(sessionName)
}

// ArtisanStop implements `ct artisan <specialty> stop` — graceful Artisan shutdown.
func ArtisanStop(specialty string) error {
	name := fmt.Sprintf("artisan-%s", specialty)
	sessionName := session.SessionName(name)

	if !session.Exists(sessionName) {
		fmt.Printf("artisan-%s is not running.\n", specialty)
		return nil
	}

	fmt.Printf("Signaling %s artisan to write handoff and exit...\n", specialty)

	projectRoot, err := db.FindProjectRoot()
	if err != nil {
		return err
	}
	ctDir := config.CompanyTownDir(projectRoot)

	signalPath := filepath.Join(ctDir, "agents", "artisan", specialty, "memory", "handoff_requested")
	if err := os.WriteFile(signalPath, []byte("handoff requested\n"), 0644); err != nil {
		return fmt.Errorf("writing handoff signal: %w", err)
	}

	session.SendKeys(sessionName, "Check for handoff_requested in your memory directory and write handoff.md, then exit.") //nolint:errcheck // fire-and-forget signal to agent

	fmt.Printf("Handoff signal sent. artisan-%s will exit after writing handoff.md.\n", specialty)
	return nil
}

// ArchitectStop implements `ct architect stop` — graceful Architect shutdown.
func ArchitectStop() error {
	sessionName := session.SessionName("architect")

	if !session.Exists(sessionName) {
		fmt.Println("Architect is not running.")
		return nil
	}

	fmt.Println("Signaling Architect to write handoff and exit...")

	projectRoot, err := db.FindProjectRoot()
	if err != nil {
		return err
	}
	ctDir := config.CompanyTownDir(projectRoot)

	signalPath := filepath.Join(ctDir, "agents", "architect", "memory", "handoff_requested")
	if err := os.WriteFile(signalPath, []byte("handoff requested\n"), 0644); err != nil {
		return fmt.Errorf("writing handoff signal: %w", err)
	}

	session.SendKeys(sessionName, "Check for handoff_requested in your memory directory and write handoff.md, then exit.") //nolint:errcheck // fire-and-forget signal to agent

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

	projectRoot, err := db.FindProjectRoot()
	if err != nil {
		return err
	}
	ctDir := config.CompanyTownDir(projectRoot)

	conn, _, connErr := db.OpenFromWorkingDir()
	var updateStatus func(string, string) error
	if connErr == nil {
		stopEvents := eventlog.NewLogger(ctDir)
		updateStatus = repo.NewAgentRepo(conn, stopEvents).UpdateStatus
		defer conn.Close()
	}

	stopCore(sessions, ctDir, clean, session.Kill, session.SendKeys, updateStatus, os.RemoveAll, gitWorktreePrune)

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
// updateStatus may be nil when the DB is unavailable.
// When clean is true, worktree directories for prole sessions are removed after signaling.
func stopCore(sessions []string, ctDir string, clean bool, killFn func(string) error, sendKeysFn func(string, string) error, updateStatus func(string, string) error, removeAll func(string) error, worktreePrune func(string) error) {
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
			os.WriteFile(signalPath, []byte("handoff requested\n"), 0644)                //nolint:errcheck // best-effort signal file write
			sendKeysFn(s, "System is shutting down. Write handoff.md and exit cleanly.") //nolint:errcheck // fire-and-forget shutdown signal
		case agentName == "mayor":
			sendKeysFn(s, "System is shutting down. Save any state and exit cleanly.") //nolint:errcheck // fire-and-forget shutdown signal
		case strings.HasPrefix(agentName, "artisan-"):
			specialty := strings.TrimPrefix(agentName, "artisan-")
			signalPath := filepath.Join(ctDir, "agents", "artisan", specialty, "memory", "handoff_requested")
			os.WriteFile(signalPath, []byte("handoff requested\n"), 0644)                //nolint:errcheck // best-effort signal file write
			sendKeysFn(s, "System is shutting down. Write handoff.md and exit cleanly.") //nolint:errcheck // fire-and-forget shutdown signal
		default:
			sendKeysFn(s, "System is shutting down. Commit and push any work, then exit.") //nolint:errcheck // fire-and-forget shutdown signal
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
		if updateStatus != nil {
			updateStatus(agentName, "idle") //nolint:errcheck // best-effort status update during shutdown
		}
	}

	if clean && cleanedAny {
		repoGit := filepath.Join(ctDir, "repo.git")
		if err := worktreePrune(repoGit); err != nil {
			fmt.Printf("  error pruning worktrees: %v\n", err)
		}
	}
}

// Attach implements `ct attach <name>` — attach to an existing agent session.
func Attach(name string) error {
	sessionName := session.SessionName(name)

	if !session.Exists(sessionName) {
		return fmt.Errorf("session %q is not running", name)
	}

	return session.Attach(sessionName)
}

// Nuke implements `ct nuke` — immediate shutdown, no handoffs.
// target restricts the operation to a single component (see nukeCore).
// An empty target performs the default full teardown.
func Nuke(target string) error {
	projectRoot, err := db.FindProjectRoot()
	if err != nil {
		return err
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
