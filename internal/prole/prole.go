package prole

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/repo"
	"github.com/katerina7479/company_town/internal/session"
)

// ProlesDir returns the path to the proles directory.
func ProlesDir(cfg *config.Config) string {
	return filepath.Join(config.CompanyTownDir(cfg.ProjectRoot), "proles")
}

// WorktreePath returns the worktree path for a named prole.
func WorktreePath(cfg *config.Config, name string) string {
	return filepath.Join(ProlesDir(cfg), name)
}

// Create sets up a new prole: git worktree, DB registration, tmux session.
func Create(name string, cfg *config.Config, agents *repo.AgentRepo) error {
	wtPath := WorktreePath(cfg, name)

	// Create worktree directory
	prolesDir := ProlesDir(cfg)
	if err := os.MkdirAll(prolesDir, 0755); err != nil {
		return fmt.Errorf("creating proles dir: %w", err)
	}

	// Check if worktree already exists
	if _, err := os.Stat(wtPath); err == nil {
		return fmt.Errorf("worktree already exists: %s", wtPath)
	}

	// Create git worktree from main
	cmd := exec.Command("git", "worktree", "add", wtPath, "main")
	cmd.Dir = cfg.ProjectRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("creating worktree: %w", err)
	}

	// Register agent in DB
	if _, err := agents.Get(name); err != nil {
		if err := agents.Register(name, "prole", nil); err != nil {
			return fmt.Errorf("registering prole: %w", err)
		}
	}

	// Set worktree path in DB
	if err := agents.SetWorktree(name, wtPath); err != nil {
		return fmt.Errorf("setting worktree path: %w", err)
	}

	// Deploy prole CLAUDE.md with template vars filled in
	if err := deployProleCLAUDEMD(name, wtPath, cfg); err != nil {
		return fmt.Errorf("deploying CLAUDE.md: %w", err)
	}

	// Launch tmux session
	sessionName := session.SessionName("prole-" + name)
	prompt := fmt.Sprintf(
		"You are prole %s. Ticket prefix: %s. "+
			"Read your CLAUDE.md for instructions. "+
			"Check your assigned ticket and begin work.",
		name, cfg.TicketPrefix,
	)

	ctDir := config.CompanyTownDir(cfg.ProjectRoot)
	agentDir := filepath.Join(ctDir, "proles", name)

	err := session.CreateInteractive(session.AgentSessionConfig{
		Name:     sessionName,
		WorkDir:  wtPath,
		Model:    cfg.Agents.Prole.Model,
		AgentDir: agentDir,
		Prompt:   prompt,
	})
	if err != nil {
		return fmt.Errorf("creating session: %w", err)
	}

	if err := agents.UpdateStatus(name, "idle"); err != nil {
		return fmt.Errorf("updating status: %w", err)
	}

	fmt.Printf("Prole %s created (worktree: %s, session: %s)\n", name, wtPath, sessionName)
	return nil
}

// Reset resets an idle prole's worktree to fresh main.
func Reset(name string, cfg *config.Config, agents *repo.AgentRepo) error {
	agent, err := agents.Get(name)
	if err != nil {
		return err
	}

	if agent.Status != "idle" {
		return fmt.Errorf("prole %s is %s, not idle (cannot reset)", name, agent.Status)
	}

	wtPath := WorktreePath(cfg, name)

	// Checkout main and pull
	cmds := [][]string{
		{"git", "checkout", "main"},
		{"git", "pull", "origin", "main"},
		{"git", "clean", "-fd"},
	}

	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = wtPath
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("running %s: %w", strings.Join(args, " "), err)
		}
	}

	// Clear current issue
	if err := agents.ClearCurrentIssue(name); err != nil {
		return fmt.Errorf("clearing issue: %w", err)
	}

	fmt.Printf("Prole %s reset to main.\n", name)
	return nil
}

// List returns all prole agents.
func List(agents *repo.AgentRepo, cfg *config.Config) ([]*repo.Agent, error) {
	all, err := agents.ListAll()
	if err != nil {
		return nil, err
	}

	var proles []*repo.Agent
	for _, a := range all {
		if a.Type == "prole" {
			proles = append(proles, a)
		}
	}
	return proles, nil
}

// deployProleCLAUDEMD writes a CLAUDE.md to the prole's worktree directory
// with template variables filled in.
func deployProleCLAUDEMD(name, wtPath string, cfg *config.Config) error {
	ctDir := config.CompanyTownDir(cfg.ProjectRoot)

	// Read the prole template from the deployed version
	templatePath := filepath.Join(ctDir, "agents", "prole", "CLAUDE.md")
	data, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("reading prole template: %w", err)
	}

	content := string(data)
	content = strings.ReplaceAll(content, "{{NAME}}", name)
	content = strings.ReplaceAll(content, "{{WORKTREE_PATH}}", wtPath)
	content = strings.ReplaceAll(content, "{{TICKET_PREFIX}}", cfg.TicketPrefix)

	// Write to the prole's directory under .company_town/proles/<name>/
	proleDir := filepath.Join(ctDir, "proles", name)
	if err := os.MkdirAll(proleDir, 0755); err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(proleDir, "CLAUDE.md"), []byte(content), 0644)
}
