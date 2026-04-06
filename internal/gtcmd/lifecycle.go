package gtcmd

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

// Start launches a named agent in a tmux session.
func Start(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: gt start <architect|conductor|reviewer|artisan-SPECIALTY>")
	}

	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	agents := repo.NewAgentRepo(conn)
	name := args[0]

	var agentType, model, agentDir, prompt string
	ctDir := config.CompanyTownDir(cfg.ProjectRoot)

	switch {
	case name == "architect":
		agentType = "architect"
		model = cfg.Agents.Architect.Model
		agentDir = filepath.Join(ctDir, "agents", "architect")
		prompt = fmt.Sprintf(
			"You are the Architect. Ticket prefix: %s. "+
				"Read your CLAUDE.md for instructions. "+
				"Check memory/handoff.md to resume previous work. "+
				"Begin your patrol loop: check for draft tickets and spec them out.",
			cfg.TicketPrefix,
		)

	case name == "conductor":
		agentType = "conductor"
		model = cfg.Agents.Conductor.Model
		agentDir = filepath.Join(ctDir, "agents", "conductor")
		prompt = fmt.Sprintf(
			"You are the Conductor. Ticket prefix: %s. "+
				"Read your CLAUDE.md for instructions. "+
				"Check memory/handoff.md to resume previous work. "+
				"Begin your patrol loop and run it continuously — never stop.",
			cfg.TicketPrefix,
		)

	case name == "reviewer":
		agentType = "reviewer"
		model = cfg.Agents.Conductor.Model // reviewer uses same model class as conductor
		agentDir = filepath.Join(ctDir, "agents", "reviewer")
		prompt = fmt.Sprintf(
			"You are the Reviewer. Ticket prefix: %s. "+
				"Read your CLAUDE.md for instructions. "+
				"Check memory/handoff.md to resume previous work. "+
				"Begin your patrol loop and run it continuously — never stop.",
			cfg.TicketPrefix,
		)

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
			spec := specialty
			if regErr := agents.Register(name, agentType, &spec); regErr != nil {
				return fmt.Errorf("registering %s: %w", name, regErr)
			}
		}

	default:
		return fmt.Errorf("unknown agent: %s", name)
	}

	sessionName := session.SessionName(name)

	if tmuxExists(sessionName) {
		fmt.Printf("%s is already running (session: %s)\n", name, sessionName)
		return nil
	}

	if !strings.HasPrefix(name, "artisan-") {
		if _, err := agents.Get(name); err != nil {
			if regErr := agents.Register(name, agentType, nil); regErr != nil {
				return fmt.Errorf("registering %s: %w", name, regErr)
			}
		}
	}

	if err := agents.UpdateStatus(name, "working"); err != nil {
		return fmt.Errorf("updating %s status: %w", name, err)
	}

	if err := agents.SetTmuxSession(name, sessionName); err != nil {
		return fmt.Errorf("recording tmux session for %s: %w", name, err)
	}

	if err := session.CreateInteractive(session.AgentSessionConfig{
		Name:     sessionName,
		WorkDir:  cfg.ProjectRoot,
		Model:    model,
		AgentDir: agentDir,
		Prompt:   prompt,
	}); err != nil {
		return fmt.Errorf("creating session: %w", err)
	}

	fmt.Printf("Started %s (session: %s)\n", name, sessionName)
	return nil
}

// Stop gracefully signals a named agent to write a handoff and exit.
func Stop(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: gt stop <agent-name>")
	}

	name := args[0]
	sessionName := session.SessionName(name)

	if !tmuxExists(sessionName) {
		fmt.Printf("%s is not running.\n", name)
		return nil
	}

	projectRoot, err := db.FindProjectRoot()
	if err != nil {
		return err
	}
	ctDir := config.CompanyTownDir(projectRoot)

	var signalPath string
	switch {
	case name == "architect":
		signalPath = filepath.Join(ctDir, "agents", "architect", "memory", "handoff_requested")
	case strings.HasPrefix(name, "artisan-"):
		specialty := strings.TrimPrefix(name, "artisan-")
		signalPath = filepath.Join(ctDir, "agents", "artisan", specialty, "memory", "handoff_requested")
	}

	if signalPath != "" {
		os.WriteFile(signalPath, []byte("handoff requested\n"), 0644)
	}

	cmd := exec.Command("tmux", "send-keys", "-t", sessionName, "System is shutting down. Write handoff.md and exit cleanly.", "Enter")
	cmd.Run()

	conn, _, err := db.OpenFromWorkingDir()
	if err != nil {
		fmt.Printf("warning: could not open db to update agent status: %v\n", err)
	} else {
		defer conn.Close()
		agents := repo.NewAgentRepo(conn)
		if err := agents.UpdateStatus(name, "idle"); err != nil {
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
