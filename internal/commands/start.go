package commands

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

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
var startServerFn func(doltDir, ctDir string, cfg *config.DoltConfig) error = db.StartServer
var connectFn func(cfg *config.DoltConfig) (*sql.DB, error) = db.Connect

// applySessionPrefix sets session.SessionPrefix from cfg, preserving the
// current value when cfg.SessionPrefix is empty (e.g., in unit-test configs
// that don't populate every field). Real configs loaded via config.Load always
// have a non-empty SessionPrefix (the loader defaults "" to "ct-").
func applySessionPrefix(cfg *config.Config) {
	if cfg.SessionPrefix != "" {
		session.SessionPrefix = cfg.SessionPrefix
	}
}

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
	projectRoot, err := db.FindProjectRoot()
	if err != nil {
		return err
	}
	cfg, err := config.Load(projectRoot)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	ctDir := config.CompanyTownDir(projectRoot)
	doltDir := filepath.Join(ctDir, "db")

	fmt.Println("Starting Dolt server...")
	if err := startServerFn(doltDir, ctDir, &cfg.Dolt); err != nil {
		return fmt.Errorf("starting dolt server: %w", err)
	}

	conn, err := connectFn(&cfg.Dolt)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer conn.Close()

	if err := db.RunMigrations(conn); err != nil {
		return fmt.Errorf("running migrations: %w", err)
	}

	if err := config.ValidateForStart(cfg); err != nil {
		return err
	}

	applySessionPrefix(cfg)

	events := eventlog.NewLogger(ctDir)
	agents := repo.NewAgentRepo(conn, events)

	// Register daemon in DB if not already present.
	if _, err := agents.Get("daemon"); err != nil {
		if regErr := agents.Register("daemon", "daemon", nil); regErr != nil {
			return fmt.Errorf("registering daemon: %w", regErr)
		}
	}

	// Start daemon if not already running.
	daemonSession := session.SessionName("daemon")
	if !sessionExistsFn(daemonSession) {
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

	applySessionPrefix(cfg)
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

	applySessionPrefix(cfg)

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
