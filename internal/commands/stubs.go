package commands

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
	"github.com/katerina7479/company_town/internal/session"
)

// startAgent is the shared logic for launching any agent in a tmux session.
func startAgent(name, agentType, model string, cfg *config.Config, agents *repo.AgentRepo, prompt string) error {
	sessionName := session.SessionName(name)

	// If session already exists, just attach
	if session.Exists(sessionName) {
		fmt.Printf("%s is already running, attaching...\n", name)
		return session.Attach(sessionName)
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

	ctDir := config.CompanyTownDir(cfg.ProjectRoot)
	agentDir := filepath.Join(ctDir, "agents", agentType)

	fmt.Printf("Starting %s...\n", name)
	err := session.CreateInteractive(session.AgentSessionConfig{
		Name:     sessionName,
		WorkDir:  cfg.ProjectRoot,
		Model:    model,
		AgentDir: agentDir,
		Prompt:   prompt,
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

	// Start daemon if not already running
	daemonSession := session.SessionName("daemon")
	if !session.Exists(daemonSession) {
		fmt.Println("Starting daemon...")
		if err := startDaemon(cfg); err != nil {
			return fmt.Errorf("starting daemon: %w", err)
		}
	} else {
		fmt.Println("Daemon already running.")
	}

	agents := repo.NewAgentRepo(conn)
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

	return cmd.Run()
}

// Architect implements `ct architect` — starts the Architect agent.
func Architect() error {
	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	agents := repo.NewAgentRepo(conn)
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

	agents := repo.NewAgentRepo(conn)
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

	fmt.Printf("Starting %s...\n", name)
	err = session.CreateInteractive(session.AgentSessionConfig{
		Name:     sessionName,
		WorkDir:  cfg.ProjectRoot,
		Model:    artisanCfg.Model,
		AgentDir: agentDir,
		Prompt:   prompt,
	})
	if err != nil {
		return err
	}

	fmt.Printf("%s started. Attaching...\n", name)
	return session.Attach(sessionName)
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

	session.SendKeys(sessionName, "Check for handoff_requested in your memory directory and write handoff.md, then exit.")

	fmt.Println("Handoff signal sent. Architect will exit after writing handoff.md.")
	return nil
}

// Stop implements `ct stop` — graceful shutdown with handoffs.
func Stop() error {
	sessions, err := session.ListCompanyTown()
	if err != nil {
		return err
	}

	if len(sessions) == 0 {
		fmt.Println("No Company Town sessions running.")
		return nil
	}

	projectRoot, err := db.FindProjectRoot()
	if err != nil {
		return err
	}
	ctDir := config.CompanyTownDir(projectRoot)

	for _, s := range sessions {
		agentName := s[len(session.SessionPrefix):]

		switch {
		case agentName == "architect":
			signalPath := filepath.Join(ctDir, "agents", "architect", "memory", "handoff_requested")
			os.WriteFile(signalPath, []byte("handoff requested\n"), 0644)
			session.SendKeys(s, "System is shutting down. Write handoff.md and exit cleanly.")
		case agentName == "mayor":
			session.SendKeys(s, "System is shutting down. Save any state and exit cleanly.")
		case strings.HasPrefix(agentName, "artisan-"):
			specialty := strings.TrimPrefix(agentName, "artisan-")
			signalPath := filepath.Join(ctDir, "agents", "artisan", specialty, "memory", "handoff_requested")
			os.WriteFile(signalPath, []byte("handoff requested\n"), 0644)
			session.SendKeys(s, "System is shutting down. Write handoff.md and exit cleanly.")
		default:
			session.SendKeys(s, "System is shutting down. Commit and push any work, then exit.")
		}

		fmt.Printf("  signaled: %s\n", s)
	}

	fmt.Println("\nHandoff signals sent. Agents will exit after saving state.")
	fmt.Println("Run `ct nuke` if you need to force-kill all sessions.")
	return nil
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
func Nuke() error {
	sessions, err := session.ListCompanyTown()
	if err != nil {
		return err
	}

	if len(sessions) == 0 {
		fmt.Println("No Company Town sessions running.")
		return nil
	}

	conn, _, connErr := db.OpenFromWorkingDir()

	for _, s := range sessions {
		if err := session.Kill(s); err != nil {
			fmt.Printf("  error killing %s: %v\n", s, err)
		} else {
			fmt.Printf("  killed: %s\n", s)
		}

		if connErr == nil {
			agentName := s[len(session.SessionPrefix):]
			agents := repo.NewAgentRepo(conn)
			agents.UpdateStatus(agentName, "dead")
		}
	}

	if connErr == nil {
		conn.Close()
	}

	fmt.Println("\nAll sessions killed.")
	return nil
}
