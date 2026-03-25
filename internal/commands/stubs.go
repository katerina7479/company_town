package commands

import (
	"fmt"
	"os"
	"path/filepath"

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

// Start implements `ct start` — starts the Mayor and attaches to its tmux session.
func Start() error {
	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	agents := repo.NewAgentRepo(conn)
	prompt := fmt.Sprintf(
		"You are the Mayor. Ticket prefix: %s. "+
			"Read your CLAUDE.md for instructions, then run `gt status` to check the system.",
		cfg.TicketPrefix,
	)

	return startAgent("mayor", "mayor", cfg.Agents.Mayor.Model, cfg, agents, prompt)
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

		switch agentName {
		case "architect":
			signalPath := filepath.Join(ctDir, "agents", "architect", "memory", "handoff_requested")
			os.WriteFile(signalPath, []byte("handoff requested\n"), 0644)
			session.SendKeys(s, "System is shutting down. Write handoff.md and exit cleanly.")
		case "mayor":
			session.SendKeys(s, "System is shutting down. Save any state and exit cleanly.")
		default:
			session.SendKeys(s, "System is shutting down. Commit and push any work, then exit.")
		}

		fmt.Printf("  signaled: %s\n", s)
	}

	fmt.Println("\nHandoff signals sent. Agents will exit after saving state.")
	fmt.Println("Run `ct nuke` if you need to force-kill all sessions.")
	return nil
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
