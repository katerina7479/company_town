package gtcmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/katerina7479/company_town/internal/commands"
	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
	"github.com/katerina7479/company_town/internal/session"
)

// Create dispatches gt create subcommands.
func Create(args []string) error {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gt create <reviewer> <name>")
		os.Exit(1)
	}

	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	agents := repo.NewAgentRepo(conn, nil)

	switch args[0] {
	case "reviewer":
		if len(args) < 2 {
			return fmt.Errorf("usage: gt create reviewer <name>")
		}
		return createReviewerWithDeps(args[1], cfg, agents, session.New())
	default:
		return fmt.Errorf("unknown create noun: %s", args[0])
	}
}

// createReviewerWithDeps creates a named reviewer agent and starts its tmux session.
// It uses the package-level createInteractiveFn var and the injected sess for existence checks.
func createReviewerWithDeps(name string, cfg *config.Config, agents *repo.AgentRepo, sess session.Client) error {
	sessionName := session.SessionName(name)

	if sess.Exists(sessionName) {
		return fmt.Errorf("%w: %s", ErrSessionAlreadyExists, sessionName)
	}

	ctDir := config.CompanyTownDir(cfg.ProjectRoot)
	agentDir := filepath.Join(ctDir, "agents", "reviewer")

	// Ensure agentDir and memory subdirectory exist
	if err := os.MkdirAll(filepath.Join(agentDir, "memory"), 0755); err != nil {
		return fmt.Errorf("creating reviewer agent dir: %w", err)
	}

	// Re-deploy CLAUDE.md from embedded template so the agent always gets latest instructions
	commands.WriteClaudeMD(agentDir, "reviewer")

	// Register in DB if not already present
	if _, err := agents.Get(name); err != nil {
		if regErr := agents.Register(name, "reviewer", nil); regErr != nil {
			return fmt.Errorf("registering reviewer %s: %w", name, regErr)
		}
	}

	prompt := fmt.Sprintf(
		"You are reviewer %s. Ticket prefix: %s. "+
			"Read your CLAUDE.md for instructions. "+
			"Check memory/handoff.md to resume previous work. "+
			"Begin patrol: check for in_review tickets and review their PRs.",
		name, cfg.TicketPrefix,
	)

	if err := createInteractiveFn(session.AgentSessionConfig{
		Name:     sessionName,
		WorkDir:  cfg.ProjectRoot,
		Model:    cfg.Agents.Reviewer.Model,
		AgentDir: agentDir,
		Prompt:   prompt,
		EnvVars:  map[string]string{"CT_AGENT_NAME": name},
	}); err != nil {
		return fmt.Errorf("creating session: %w", err)
	}

	if err := agents.SetTmuxSession(name, sessionName); err != nil {
		return fmt.Errorf("recording tmux session for %s: %w", name, err)
	}

	if err := agents.UpdateStatus(name, "idle"); err != nil {
		return fmt.Errorf("updating status: %w", err)
	}

	fmt.Printf("Reviewer %s created (session: %s)\n", name, sessionName)
	return nil
}
