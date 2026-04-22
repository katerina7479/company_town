// Package prole manages prole agent lifecycle: creating git worktrees, launching
// tmux sessions, resetting idle worktrees, and pruning dead ones.
//
// Directory layout under .company_town/:
//
//	.company_town/
//	├── config.json       — project config
//	├── db/               — Dolt database directory (MUST NOT be touched by worktree ops)
//	├── repo.git/         — bare clone used as the shared git object store
//	└── proles/
//	    ├── iron/         — worktree for prole "iron"  (only safe worktree target)
//	    ├── copper/       — worktree for prole "copper"
//	    └── ...
//
// All git worktree add/remove/reset operations must target a path under
// .company_town/proles/ and must never touch db/ or repo.git/.
package prole

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/repo"
	"github.com/katerina7479/company_town/internal/runner"
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

// doltDir returns the path to the Dolt database directory.
func doltDir(cfg *config.Config) string {
	return filepath.Join(config.CompanyTownDir(cfg.ProjectRoot), "db")
}

// isSafeWorktreePath returns true when path is a valid target for git worktree
// operations: it must be non-empty, sit under ProlesDir, and must not coincide
// with the bare repo or the Dolt database directory.
//
// This prevents a corrupted or malicious WorktreePath DB value from causing
// git worktree remove --force or git clean to operate on critical directories.
func isSafeWorktreePath(cfg *config.Config, path string) bool {
	if path == "" {
		return false
	}

	// Resolve to absolute, clean paths so symlinks and ".." can't escape the check.
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	prolesDir, err := filepath.Abs(ProlesDir(cfg))
	if err != nil {
		return false
	}
	bareRepo, err := filepath.Abs(BareRepoPath(cfg))
	if err != nil {
		return false
	}
	dolt, err := filepath.Abs(doltDir(cfg))
	if err != nil {
		return false
	}

	// Must be strictly under the proles directory (not equal to it).
	rel, err := filepath.Rel(prolesDir, absPath)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}

	// Extra belt-and-suspenders: explicitly reject the bare repo and Dolt dir.
	if absPath == bareRepo || absPath == dolt {
		return false
	}

	return true
}

// InstallPreCommitHook copies scripts/pre-commit from the project root into the
// git hooks directory for the given worktree. This ensures the gofmt pre-commit
// check fires in agent and prole worktrees, which do not inherit the main
// checkout's hook installation.
//
// For git worktrees, .git is a file (not a directory) that points to the
// worktree-specific gitdir inside the bare repo. The hook is installed at
// <gitdir>/hooks/pre-commit. Failures are non-fatal — the hook is a
// quality-of-life guard, not required for correctness.
func InstallPreCommitHook(projectRoot, wtPath string) {
	hookSrc := filepath.Join(projectRoot, "scripts", "pre-commit")
	if _, err := os.Stat(hookSrc); err != nil {
		return // hook script not present; skip silently
	}

	// git rev-parse --git-dir from inside a worktree returns the worktree-specific
	// gitdir (e.g. <bare>/worktrees/<name>/).
	cmd := exec.Command("git", "rev-parse", "--git-dir")
	cmd.Dir = wtPath
	out, err := cmd.Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not find gitdir for worktree at %s: %v\n", wtPath, err)
		return
	}
	gitDir := strings.TrimSpace(string(out))
	if !filepath.IsAbs(gitDir) {
		gitDir = filepath.Join(wtPath, gitDir)
	}

	hooksDir := filepath.Join(gitDir, "hooks")
	if err := os.MkdirAll(hooksDir, 0750); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not create hooks dir %s: %v\n", hooksDir, err)
		return
	}

	data, err := os.ReadFile(hookSrc)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not read pre-commit hook: %v\n", err)
		return
	}

	hookDst := filepath.Join(hooksDir, "pre-commit")
	// gosec G703: hookDst is derived from git rev-parse --git-dir run in a
	// worktree we created; not user-controlled input.
	if err := os.WriteFile(hookDst, data, 0750); err != nil { //nolint:gosec
		fmt.Fprintf(os.Stderr, "warning: could not install pre-commit hook at %s: %v\n", hookDst, err)
	}
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
	gitCfg.Run() //nolint:errcheck // best-effort git config (bare repo fetch refspec)

	// Fetch to populate remote tracking refs
	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Dir = barePath
	fetchCmd.Stdout = os.Stdout
	fetchCmd.Stderr = os.Stderr
	fetchCmd.Run() //nolint:errcheck // best-effort fetch

	return nil
}

// Create sets up a new prole: bare repo worktree, DB registration, tmux session.
// If the prole already exists in the DB, it re-launches the session without
// checking the max_proles cap (no new prole is being created).
func Create(name string, cfg *config.Config, agents *repo.AgentRepo) error {
	// Enforce max_proles limit only when creating a brand-new prole.
	_, existsErr := agents.Get(name)
	isNew := existsErr != nil
	if isNew && cfg.MaxProles > 0 {
		count, err := agents.CountByType("prole")
		if err != nil {
			return fmt.Errorf("counting proles: %w", err)
		}
		if count >= cfg.MaxProles {
			return fmt.Errorf("%w (%d/%d): cannot create prole %q", ErrMaxProlesLimitReached, count, cfg.MaxProles, name)
		}
	}

	wtPath := WorktreePath(cfg, name)

	// Ensure proles directory exists
	if err := os.MkdirAll(ProlesDir(cfg), 0755); err != nil {
		return fmt.Errorf("creating proles dir: %w", err)
	}

	// Create worktree if it doesn't already exist
	if _, err := os.Stat(wtPath); os.IsNotExist(err) {
		// Ensure bare repo is set up and current
		if err := EnsureBareRepo(cfg); err != nil {
			return fmt.Errorf("setting up bare repo: %w", err)
		}

		// Create worktree on a branch from origin/main.
		branch := fmt.Sprintf("prole/%s/standby", name)
		if err := addWorktreeForProle(BareRepoPath(cfg), branch, wtPath); err != nil {
			return fmt.Errorf("creating worktree: %w", err)
		}

		// Install the pre-commit hook so gofmt checks fire in prole worktrees.
		InstallPreCommitHook(cfg.ProjectRoot, wtPath)

		// Set push remote to origin so proles push to GitHub
		pushCmd := exec.Command("git", "remote", "set-url", "--push", "origin",
			mustGetOriginURL(cfg))
		pushCmd.Dir = wtPath
		pushCmd.Run() //nolint:errcheck // best-effort push remote setup
	} else {
		// Worktree exists — the prole may already be on a feature branch, so
		// we must not switch or pull in the worktree. Just refresh the bare
		// repo's remote tracking refs so future git operations see the latest
		// origin state.
		fetchCmd := exec.Command("git", "fetch", "origin")
		fetchCmd.Dir = BareRepoPath(cfg)
		fetchCmd.Stdout = os.Stdout
		fetchCmd.Stderr = os.Stderr
		if err := fetchCmd.Run(); err != nil {
			// Non-fatal: the prole can still work without fresh remote refs.
			fmt.Fprintf(os.Stderr, "warning: could not fetch origin for prole %s bare repo: %v\n", name, err)
		}
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

	// Deploy .claude/settings.json with language-appropriate Bash allowlist.
	// Must run before CreateInteractive so provisionClaudeSettings finds it present.
	if err := deployProleSettings(name, cfg); err != nil {
		return fmt.Errorf("deploying .claude/settings.json: %w", err)
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
		EnvVars:  map[string]string{"CT_AGENT_NAME": name},
	})
	if err != nil {
		return fmt.Errorf("creating session: %w", err)
	}

	if err := agents.SetTmuxSession(name, sessionName); err != nil {
		return fmt.Errorf("recording tmux session for %s: %w", name, err)
	}

	if err := agents.UpdateStatus(name, repo.StatusIdle); err != nil {
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

	if agent.Status != repo.StatusIdle {
		return fmt.Errorf("prole %s is %s, not idle (cannot reset)", name, agent.Status)
	}

	wtPath := WorktreePath(cfg, name)

	// Fetch latest in the bare repo
	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Dir = BareRepoPath(cfg)
	fetchCmd.Stdout = os.Stdout
	fetchCmd.Stderr = os.Stderr
	fetchCmd.Run() //nolint:errcheck // best-effort fetch

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

	// Redeploy settings so proles created before nc-246 get the Bash allowlist.
	if err := deployProleSettings(name, cfg); err != nil {
		return fmt.Errorf("deploying prole settings: %w", err)
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

// isValidWorktreePath returns true if path is a non-empty absolute path.
// A corrupted DB value (relative path, empty after trim, etc.) must never be
// used as a git working directory — the commands would silently operate on the
// wrong location.
func isValidWorktreePath(path string) bool {
	return path != "" && filepath.IsAbs(path)
}

// idleProlesNeedingReset returns the subset of agents that are candidates for
// worktree reset: idle proles with no current issue and a registered worktree
// path. Exposed as a pure filter so the selection logic is unit-testable
// without touching git.
func idleProlesNeedingReset(all []*repo.Agent) []*repo.Agent {
	var out []*repo.Agent
	for _, a := range all {
		if a.Type != "prole" || a.Status != repo.StatusIdle {
			continue
		}
		if a.CurrentIssue.Valid {
			continue
		}
		if !a.WorktreePath.Valid || !isValidWorktreePath(a.WorktreePath.String) {
			continue
		}
		out = append(out, a)
	}
	return out
}

// currentBranch reads the current git branch for a worktree path.
func currentBranch(wtPath string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = wtPath
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// isWorktreeDirty returns true if the worktree has uncommitted changes.
func isWorktreeDirty(wtPath string) (bool, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = wtPath
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return len(strings.TrimSpace(string(out))) > 0, nil
}

// ResetIdleWorktrees is a reconciler: for every idle prole whose worktree is
// not parked on its standby branch (or is parked on standby but dirty), run
// prole.Reset to bring it back to a clean checkout of origin/main. Idempotent
// — clean, standby-parked proles are skipped. This is the reconciler that
// replaces the per-merge worktree reset in daemon.handlePRMerged; see NC-53.
func ResetIdleWorktrees(cfg *config.Config, agents *repo.AgentRepo, logger *log.Logger) error {
	all, err := agents.ListAll()
	if err != nil {
		return fmt.Errorf("listing agents: %w", err)
	}

	for _, a := range idleProlesNeedingReset(all) {
		wtPath := a.WorktreePath.String
		if !isSafeWorktreePath(cfg, wtPath) {
			logger.Printf("warning: skipping idle prole %s — worktree path %q is not under proles dir", a.Name, wtPath)
			continue
		}
		if _, err := os.Stat(wtPath); os.IsNotExist(err) {
			continue
		}

		standbyBranch := fmt.Sprintf("prole/%s/standby", a.Name)
		branch, err := currentBranch(wtPath)
		if err != nil {
			logger.Printf("warning: could not read branch for prole %s at %s: %v", a.Name, wtPath, err)
			continue
		}

		if branch == standbyBranch {
			dirty, err := isWorktreeDirty(wtPath)
			if err != nil {
				logger.Printf("warning: could not check dirty state for prole %s: %v", a.Name, err)
				continue
			}
			if !dirty {
				continue
			}
		}

		if err := Reset(a.Name, cfg, agents); err != nil {
			logger.Printf("error resetting idle prole %s: %v", a.Name, err)
			continue
		}
		logger.Printf("reset idle prole %s worktree (was on %s)", a.Name, branch)
	}
	return nil
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

// proleSettingsJSON builds the .claude/settings.json content for a prole.
// The base allowlist (gt, git, go, make, ct) is always present. Language-specific
// tools are appended: python adds python/pytest/pip; js adds npm/pnpm/node.
// gh, glab, and dolt are intentionally excluded — those mutations must flow
// through gt (which routes them through vcs.Provider and the SQL connection).
func proleSettingsJSON(language string) ([]byte, error) {
	allow := runner.BaseBashAllowList()
	switch language {
	case "python":
		allow = append(allow, "Bash(python:*)", "Bash(pytest:*)", "Bash(pip:*)")
	case "js", "javascript":
		allow = append(allow, "Bash(npm:*)", "Bash(pnpm:*)", "Bash(node:*)")
	}

	type perms struct {
		Allow []string `json:"allow"`
	}
	type settings struct {
		Permissions perms `json:"permissions"`
	}

	return json.MarshalIndent(settings{Permissions: perms{Allow: allow}}, "", "  ")
}

// deployProleSettings writes .claude/settings.json to the prole's agent directory
// (.company_town/proles/<name>/.claude/settings.json) with a Bash allowlist tuned
// to cfg.Language. Called before CreateInteractive so provisionClaudeSettings finds
// the file already present and skips its generic fallback write.
func deployProleSettings(name string, cfg *config.Config) error {
	ctDir := config.CompanyTownDir(cfg.ProjectRoot)
	proleDir := filepath.Join(ctDir, "proles", name)

	data, err := proleSettingsJSON(cfg.Language)
	if err != nil {
		return fmt.Errorf("generating prole settings: %w", err)
	}

	claudeDir := filepath.Join(proleDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0750); err != nil {
		return fmt.Errorf("creating .claude dir: %w", err)
	}

	return os.WriteFile(filepath.Join(claudeDir, "settings.json"), append(data, '\n'), 0644)
}

// SwitchToBranch fetches branch from origin into the bare repo and checks it
// out in the worktree. Called by assign.Execute when handing a repair ticket
// to a (potentially different) prole so it resumes from the existing commits.
func SwitchToBranch(wtPath, barePath, branch string) error {
	// Fetch branch from origin into the bare repo so the worktree can see it.
	fetchCmd := exec.Command("git", "fetch", "origin", branch)
	fetchCmd.Dir = barePath
	_ = fetchCmd.Run() // best-effort; branch may already be local, or offline — checkout below surfaces real failures

	checkoutCmd := exec.Command("git", "checkout", branch)
	checkoutCmd.Dir = wtPath
	if err := checkoutCmd.Run(); err != nil {
		return fmt.Errorf("checking out %s in worktree %s: %w", branch, wtPath, err)
	}
	return nil
}

// addWorktreeForProle adds a git worktree at wtPath on the given branch.
// If the branch already exists in the bare repo (stale from a previous prole
// incarnation), it is reset to origin/main before the worktree is created.
// This makes prole creation idempotent with respect to stale standby branches.
func addWorktreeForProle(barePath, branch, wtPath string) error {
	// Detect whether the standby branch already exists.
	checkCmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	checkCmd.Dir = barePath
	branchExists := checkCmd.Run() == nil

	if branchExists {
		// Reset the stale branch to origin/main so the new worktree starts clean.
		resetCmd := exec.Command("git", "branch", "-f", branch, "origin/main")
		resetCmd.Dir = barePath
		resetCmd.Stdout = os.Stdout
		resetCmd.Stderr = os.Stderr
		if err := resetCmd.Run(); err != nil {
			return fmt.Errorf("resetting stale standby branch: %w", err)
		}
		addCmd := exec.Command("git", "worktree", "add", wtPath, branch)
		addCmd.Dir = barePath
		addCmd.Stdout = os.Stdout
		addCmd.Stderr = os.Stderr
		return addCmd.Run()
	}

	// Branch does not exist — create it fresh from origin/main.
	addCmd := exec.Command("git", "worktree", "add", "-b", branch, wtPath, "origin/main")
	addCmd.Dir = barePath
	addCmd.Stdout = os.Stdout
	addCmd.Stderr = os.Stderr
	return addCmd.Run()
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
		return cfg.Repo
	}
	return url
}

// PruneDeadWorktrees removes worktrees belonging to dead prole agents when they
// are git-clean (no uncommitted changes, no unpushed commits). After processing
// individual worktrees, it runs git worktree prune on the bare repo to clear any
// stale metadata. Returns the names of agents whose worktrees were removed.
func PruneDeadWorktrees(cfg *config.Config, agents *repo.AgentRepo, logger *log.Logger) ([]string, error) {
	all, err := agents.ListAll()
	if err != nil {
		return nil, fmt.Errorf("listing agents: %w", err)
	}

	var pruned []string
	for _, a := range all {
		if a.Type != "prole" || a.Status != repo.StatusDead {
			continue
		}
		if !a.WorktreePath.Valid || !isValidWorktreePath(a.WorktreePath.String) {
			continue
		}
		wtPath := a.WorktreePath.String
		if !isSafeWorktreePath(cfg, wtPath) {
			logger.Printf("warning: skipping prole %s — worktree path %q is not under proles dir", a.Name, wtPath)
			continue
		}
		if _, err := os.Stat(wtPath); os.IsNotExist(err) {
			continue // already removed from disk
		}

		// Check for uncommitted changes — preserve worktree if dirty.
		statusCmd := exec.Command("git", "status", "--porcelain")
		statusCmd.Dir = wtPath
		statusOut, statusErr := statusCmd.Output()
		if statusErr != nil {
			logger.Printf("warning: could not check git status for prole %s at %s: %v", a.Name, wtPath, statusErr)
			continue
		}
		if len(strings.TrimSpace(string(statusOut))) > 0 {
			continue // dirty — leave in place
		}

		// Check for unpushed commits — preserve worktree if work hasn't been pushed.
		unpushedCmd := exec.Command("git", "log", "@{u}..", "--oneline")
		unpushedCmd.Dir = wtPath
		unpushedOut, upErr := unpushedCmd.Output()
		if upErr != nil {
			logger.Printf("warning: could not check unpushed commits for prole %s at %s (no upstream?): %v", a.Name, wtPath, upErr)
			continue
		}
		if len(strings.TrimSpace(string(unpushedOut))) > 0 {
			continue // unpushed commits — leave in place
		}

		// Worktree is clean — remove it via git so the worktree list stays consistent.
		removeCmd := exec.Command("git", "worktree", "remove", "--force", wtPath)
		removeCmd.Dir = BareRepoPath(cfg)
		if err := removeCmd.Run(); err != nil {
			logger.Printf("warning: could not remove worktree for prole %s at %s: %v", a.Name, wtPath, err)
			continue
		}

		// Clear the stale path from the database.
		if err := agents.SetWorktree(a.Name, ""); err != nil {
			logger.Printf("warning: removed worktree for prole %s but could not clear DB path: %v", a.Name, err)
		}

		pruned = append(pruned, a.Name)
	}

	// Prune stale worktree metadata from the bare repo (best effort).
	barePath := BareRepoPath(cfg)
	if _, err := os.Stat(barePath); err == nil {
		pruneCmd := exec.Command("git", "worktree", "prune")
		pruneCmd.Dir = barePath
		pruneCmd.Run() //nolint:errcheck // best-effort worktree prune
	}

	return pruned, nil
}
