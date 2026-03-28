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

// BareRepoPath returns the path to the bare clone used for prole worktrees.
func BareRepoPath(cfg *config.Config) string {
	return filepath.Join(config.CompanyTownDir(cfg.ProjectRoot), "repo.git")
}

// ProlesDir returns the path to the proles directory.
func ProlesDir(cfg *config.Config) string {
	return filepath.Join(config.CompanyTownDir(cfg.ProjectRoot), "proles")
}

// WorktreePath returns the worktree path for a named prole.
func WorktreePath(cfg *config.Config, name string) string {
	return filepath.Join(ProlesDir(cfg), name)
}

// EnsureBareRepo creates the bare clone if it doesn't exist, or fetches if it does.
func EnsureBareRepo(cfg *config.Config) error {
	barePath := BareRepoPath(cfg)

	if _, err := os.Stat(barePath); err == nil {
		// Already exists — fetch latest
		cmd := exec.Command("git", "fetch", "origin")
		cmd.Dir = barePath
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// Get the real remote URL from the project repo
	originURL, err := getOriginURL(cfg.ProjectRoot)
	if err != nil || originURL == "" {
		return fmt.Errorf("could not determine origin URL: %w", err)
	}

	// Clone bare directly from the remote, then set up fetch refspec
	// so `git fetch origin` updates remote tracking refs
	cmd := exec.Command("git", "clone", "--bare", originURL, barePath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("creating bare clone: %w", err)
	}

	// Configure fetch refspec (bare clones don't set this by default)
	gitCfg := exec.Command("git", "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	gitCfg.Dir = barePath
	gitCfg.Run()

	// Fetch to populate remote tracking refs
	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Dir = barePath
	fetchCmd.Stdout = os.Stdout
	fetchCmd.Stderr = os.Stderr
	fetchCmd.Run()

	return nil
}

// Create sets up a new prole: bare repo worktree, DB registration, tmux session.
func Create(name string, cfg *config.Config, agents *repo.AgentRepo) error {
	// Enforce max_proles limit before creating a new prole.
	if cfg.MaxProles > 0 {
		count, err := agents.CountByType("prole")
		if err != nil {
			return fmt.Errorf("counting proles: %w", err)
		}
		if count >= cfg.MaxProles {
			return fmt.Errorf("max_proles limit reached (%d/%d): cannot create prole %q", count, cfg.MaxProles, name)
		}
	}

	wtPath := WorktreePath(cfg, name)

	// Ensure proles directory exists
	if err := os.MkdirAll(ProlesDir(cfg), 0755); err != nil {
		return fmt.Errorf("creating proles dir: %w", err)
	}

	// Check if worktree already exists
	if _, err := os.Stat(wtPath); err == nil {
		return fmt.Errorf("worktree already exists: %s", wtPath)
	}

	// Ensure bare repo is set up and current
	if err := EnsureBareRepo(cfg); err != nil {
		return fmt.Errorf("setting up bare repo: %w", err)
	}

	// Create worktree on a new branch from origin/main
	branch := fmt.Sprintf("prole/%s/standby", name)
	cmd := exec.Command("git", "worktree", "add", "-b", branch, wtPath, "origin/main")
	cmd.Dir = BareRepoPath(cfg)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("creating worktree: %w", err)
	}

	// Set push remote to origin so proles push to GitHub
	pushCmd := exec.Command("git", "remote", "set-url", "--push", "origin",
		mustGetOriginURL(cfg))
	pushCmd.Dir = wtPath
	pushCmd.Run()

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

// Reset resets an idle prole's worktree to latest origin/main.
func Reset(name string, cfg *config.Config, agents *repo.AgentRepo) error {
	agent, err := agents.Get(name)
	if err != nil {
		return err
	}

	if agent.Status != "idle" {
		return fmt.Errorf("prole %s is %s, not idle (cannot reset)", name, agent.Status)
	}

	wtPath := WorktreePath(cfg, name)

	// Fetch latest in the bare repo
	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Dir = BareRepoPath(cfg)
	fetchCmd.Stdout = os.Stdout
	fetchCmd.Stderr = os.Stderr
	fetchCmd.Run()

	// Reset worktree to latest main
	branch := fmt.Sprintf("prole/%s/standby", name)
	cmds := [][]string{
		{"git", "checkout", branch},
		{"git", "reset", "--hard", "origin/main"},
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

// deployProleCLAUDEMD writes a CLAUDE.md to the prole's agent directory
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

func getOriginURL(projectRoot string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = projectRoot
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func mustGetOriginURL(cfg *config.Config) string {
	url, err := getOriginURL(cfg.ProjectRoot)
	if err != nil {
		return cfg.GithubRepo
	}
	return url
}

// PruneDeadWorktrees removes worktrees belonging to dead prole agents when they
// are git-clean (no uncommitted changes, no unpushed commits). After processing
// individual worktrees, it runs git worktree prune on the bare repo to clear any
// stale metadata. Returns the names of agents whose worktrees were removed.
func PruneDeadWorktrees(cfg *config.Config, agents *repo.AgentRepo) ([]string, error) {
	all, err := agents.ListAll()
	if err != nil {
		return nil, fmt.Errorf("listing agents: %w", err)
	}

	var pruned []string
	for _, a := range all {
		if a.Type != "prole" || a.Status != "dead" {
			continue
		}
		if !a.WorktreePath.Valid || a.WorktreePath.String == "" {
			continue
		}
		wtPath := a.WorktreePath.String
		if _, err := os.Stat(wtPath); os.IsNotExist(err) {
			continue // already removed from disk
		}

		// Check for uncommitted changes — preserve worktree if dirty.
		statusCmd := exec.Command("git", "status", "--porcelain")
		statusCmd.Dir = wtPath
		statusOut, err := statusCmd.Output()
		if err != nil || len(strings.TrimSpace(string(statusOut))) > 0 {
			continue
		}

		// Check for unpushed commits — preserve worktree if work hasn't been pushed.
		unpushedCmd := exec.Command("git", "log", "@{u}..", "--oneline")
		unpushedCmd.Dir = wtPath
		unpushedOut, upErr := unpushedCmd.Output()
		if upErr != nil || len(strings.TrimSpace(string(unpushedOut))) > 0 {
			continue
		}

		// Worktree is clean — remove it via git so the worktree list stays consistent.
		removeCmd := exec.Command("git", "worktree", "remove", "--force", wtPath)
		removeCmd.Dir = BareRepoPath(cfg)
		if err := removeCmd.Run(); err != nil {
			continue // leave in place if removal fails
		}

		pruned = append(pruned, a.Name)
	}

	// Prune stale worktree metadata from the bare repo (best effort).
	barePath := BareRepoPath(cfg)
	if _, err := os.Stat(barePath); err == nil {
		pruneCmd := exec.Command("git", "worktree", "prune")
		pruneCmd.Dir = barePath
		pruneCmd.Run()
	}

	return pruned, nil
}
