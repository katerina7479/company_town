package gtcmd

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/katerina7479/company_town/internal/agentworktree"
	"github.com/katerina7479/company_town/internal/commands"
	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/eventlog"
	"github.com/katerina7479/company_town/internal/repo"
	"github.com/katerina7479/company_town/internal/session"
)

// Package-level vars for injection in tests.
var createInteractiveFn func(session.AgentSessionConfig) error = session.CreateInteractive
var tmuxExistsFn func(string) bool = tmuxExists
var ensureAgentWorktreeFn func(cfg *config.Config, agentDir string) (string, error) = agentworktree.Ensure
var killSessionFn func(string) error = session.Kill

// startDaemonFn launches the daemon in a detached tmux session. Injected in tests.
var startDaemonFn = func(cfg *config.Config) error {
	sessionName := session.SessionName("daemon")
	cmd := exec.Command("tmux", "new-session",
		"-d",
		"-s", sessionName,
		"-c", cfg.ProjectRoot,
		"ct daemon",
	)
	if err := cmd.Run(); err != nil {
		return err
	}
	_ = session.ApplyStatusBar(sessionName, "daemon")
	return nil
}

// Start launches a named agent in a tmux session.
func Start(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: gt start <architect|reviewer|artisan-SPECIALTY>")
	}

	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	session.SessionPrefix = cfg.SessionPrefix
	ctDir := config.CompanyTownDir(cfg.ProjectRoot)
	events := eventlog.NewLogger(ctDir)
	agents := repo.NewAgentRepo(conn, events)
	return startAgentWithDeps(cfg, agents, args[0])
}

// startAgentWithDeps is the injectable core of Start. Extracted for testability.
func startAgentWithDeps(cfg *config.Config, agents *repo.AgentRepo, name string) error {
	ctDir := config.CompanyTownDir(cfg.ProjectRoot)

	var agentType, templateType, model, agentDir, prompt string

	switch {
	case agentRegistry[name].agentSubDir != "":
		// Registered startable agent (architect, reviewer). Mayor is also in the
		// registry but has an empty agentSubDir so it falls through to the default.
		spec := agentRegistry[name]
		agentType = spec.agentType
		templateType = spec.templateType
		ac := spec.configFor(cfg)
		model = ac.Model
		agentDir = filepath.Join(ctDir, "agents", spec.agentSubDir)
		prompt = spec.promptFn(cfg)

	case strings.HasPrefix(name, "artisan-"):
		specialty := strings.TrimPrefix(name, "artisan-")
		artisanCfg, ok := cfg.Agents.Artisan[specialty]
		if !ok {
			var available []string
			for k := range cfg.Agents.Artisan {
				available = append(available, k)
			}
			return fmt.Errorf("unknown specialty %q (available in config: %v)", specialty, available)
		}
		agentType = "artisan"
		templateType = "artisan-" + specialty
		model = artisanCfg.Model
		agentDir = filepath.Join(ctDir, "agents", "artisan", specialty)
		prompt = fmt.Sprintf(
			"You are a %s Artisan. Ticket prefix: %s. "+
				"Read your CLAUDE.md for instructions. "+
				"Check memory/handoff.md to resume previous work. "+
				"Then check for assigned tickets with `gt ticket list --status in_progress`.",
			specialty, cfg.TicketPrefix,
		)

		if err := os.MkdirAll(filepath.Join(agentDir, "memory"), 0755); err != nil {
			return fmt.Errorf("creating artisan directory: %w", err)
		}

		if _, err := agents.Get(name); err != nil {
			if !errors.Is(err, repo.ErrNotFound) {
				return fmt.Errorf("checking artisan agent %s: %w", name, err)
			}
			spec := specialty
			if regErr := agents.Register(name, agentType, &spec); regErr != nil {
				return fmt.Errorf("registering %s: %w", name, regErr)
			}
		}

	case name == "daemon":
		sessionName := session.SessionName("daemon")

		if tmuxExistsFn(sessionName) {
			fmt.Printf("daemon is already running (session: %s)\n", sessionName)
			return nil
		}

		// Register or upsert the daemon agent row.
		if _, err := agents.Get("daemon"); err != nil {
			if !errors.Is(err, repo.ErrNotFound) {
				return fmt.Errorf("checking daemon agent: %w", err)
			}
			if regErr := agents.Register("daemon", "daemon", nil); regErr != nil {
				return fmt.Errorf("registering daemon: %w", regErr)
			}
		}

		if err := agents.SetTmuxSession("daemon", sessionName); err != nil {
			return fmt.Errorf("recording tmux session for daemon: %w", err)
		}

		if err := startDaemonFn(cfg); err != nil {
			return fmt.Errorf("starting daemon: %w", err)
		}

		if err := agents.UpdateStatus("daemon", repo.StatusWorking); err != nil {
			return fmt.Errorf("updating daemon status: %w", err)
		}

		fmt.Printf("Started daemon (session: %s)\n", sessionName)
		return nil

	default:
		return fmt.Errorf("unknown agent: %s", name)
	}

	// Ensure the agent directory exists before writing CLAUDE.md. Artisan dirs are
	// created above; architect and reviewer dirs may not exist yet in a fresh setup
	// or a test temp dir.
	if err := os.MkdirAll(agentDir, 0755); err != nil {
		return fmt.Errorf("creating agent dir %s: %w", agentDir, err)
	}

	// Re-deploy CLAUDE.md from embedded template on every start so agents always
	// get the latest instructions after a binary upgrade.
	commands.WriteClaudeMD(agentDir, templateType)

	sessionName := session.SessionName(name)

	if tmuxExistsFn(sessionName) {
		// Session is live. If the DB shows dead (e.g. daemon marked it dead after a
		// crash and the user started a new session), reset to idle so gt status and
		// the dashboard reflect reality.
		if existing, getErr := agents.Get(name); getErr == nil && existing.Status == repo.StatusDead {
			if updateErr := agents.UpdateStatus(name, repo.StatusIdle); updateErr != nil {
				fmt.Printf("warning: could not reset %s status from dead to idle: %v\n", name, updateErr)
			}
		}
		fmt.Printf("%s is already running (session: %s)\n", name, sessionName)
		return nil
	}

	if !strings.HasPrefix(name, "artisan-") {
		if _, err := agents.Get(name); err != nil {
			if !errors.Is(err, repo.ErrNotFound) {
				return fmt.Errorf("checking agent %s: %w", name, err)
			}
			if regErr := agents.Register(name, agentType, nil); regErr != nil {
				return fmt.Errorf("registering %s: %w", name, regErr)
			}
		}
	}

	if err := agents.UpdateStatus(name, repo.StatusIdle); err != nil {
		return fmt.Errorf("updating %s status: %w", name, err)
	}

	if err := agents.SetTmuxSession(name, sessionName); err != nil {
		return fmt.Errorf("recording tmux session for %s: %w", name, err)
	}

	// Provision an isolated git worktree so this agent's branch checkouts
	// cannot affect other agents' views of HEAD.
	wtPath, err := ensureAgentWorktreeFn(cfg, agentDir)
	if err != nil {
		return fmt.Errorf("setting up worktree for %s: %w", name, err)
	}

	if err := createInteractiveFn(session.AgentSessionConfig{
		Name:     sessionName,
		WorkDir:  wtPath,
		Model:    model,
		AgentDir: agentDir,
		Prompt:   prompt,
		EnvVars:  map[string]string{"CT_AGENT_NAME": name},
	}); err != nil {
		return fmt.Errorf("creating session: %w", err)
	}

	fmt.Printf("Started %s (session: %s)\n", name, sessionName)
	return nil
}

// stopDaemonWithDeps is the injectable core of the daemon stop path. Extracted for testability.
func stopDaemonWithDeps(agents *repo.AgentRepo, sessionName string) error {
	if err := agents.UpdateStatus("daemon", repo.StatusIdle); err != nil {
		fmt.Printf("warning: could not update daemon status: %v\n", err)
	}

	if err := killSessionFn(sessionName); err != nil {
		return fmt.Errorf("killing daemon session: %w", err)
	}

	fmt.Printf("Stopped daemon (session %s killed).\n", sessionName)
	return nil
}

// Stop gracefully signals a named agent to write a handoff and exit.
// For the daemon, which runs a Go binary (not a Claude session), the tmux
// session is killed directly after the DB status flip.
func Stop(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: gt stop <agent-name>")
	}

	// Load config to set session.SessionPrefix before building any session name.
	if projectRoot, findErr := db.FindProjectRoot(); findErr == nil {
		if cfg, cfgErr := config.Load(projectRoot); cfgErr == nil {
			session.SessionPrefix = cfg.SessionPrefix
		}
	}

	name := args[0]
	sessionName := session.SessionName(name)

	if !tmuxExistsFn(sessionName) {
		fmt.Printf("%s is not running.\n", name)
		return nil
	}

	// Daemon has no Claude session to write a handoff — kill the tmux session directly.
	if name == "daemon" {
		conn, stopCfg, err := db.OpenFromWorkingDir()
		if err != nil {
			return fmt.Errorf("opening db to stop daemon: %w", err)
		}
		defer func() { _ = conn.Close() }()
		stopEvents := eventlog.NewLogger(config.CompanyTownDir(stopCfg.ProjectRoot))
		agents := repo.NewAgentRepo(conn, stopEvents)
		return stopDaemonWithDeps(agents, sessionName)
	}

	projectRoot, err := db.FindProjectRoot()
	if err != nil {
		return err
	}
	ctDir := config.CompanyTownDir(projectRoot)

	var signalPath string
	switch {
	case agentRegistry[name].hasSignalFile:
		spec := agentRegistry[name]
		signalPath = filepath.Join(ctDir, "agents", spec.agentSubDir, "memory", "handoff_requested")
	case strings.HasPrefix(name, "artisan-"):
		specialty := strings.TrimPrefix(name, "artisan-")
		signalPath = filepath.Join(ctDir, "agents", "artisan", specialty, "memory", "handoff_requested")
	}

	if signalPath != "" {
		os.WriteFile(signalPath, []byte("handoff requested\n"), 0644) //nolint:errcheck // best-effort signal file write
	}

	cmd := exec.Command("tmux", "send-keys", "-t", sessionName, "System is shutting down. Write handoff.md and exit cleanly.", "Enter")
	cmd.Run() //nolint:errcheck // best-effort tmux send-keys

	conn, stopCfg, err := db.OpenFromWorkingDir()
	if err != nil {
		fmt.Printf("warning: could not open db to update agent status: %v\n", err)
	} else {
		defer conn.Close()
		stopEvents := eventlog.NewLogger(config.CompanyTownDir(stopCfg.ProjectRoot))
		agents := repo.NewAgentRepo(conn, stopEvents)
		if err := agents.UpdateStatus(name, repo.StatusIdle); err != nil {
			fmt.Printf("warning: could not update agent status: %v\n", err)
		}
	}

	fmt.Printf("Signaled %s to shutdown. Check session %s for handoff.\n", name, sessionName)
	return nil
}

func tmuxExists(sessionName string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", sessionName)
	return cmd.Run() == nil
}
