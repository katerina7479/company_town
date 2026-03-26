package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/prole"
	"github.com/katerina7479/company_town/internal/repo"
	"github.com/katerina7479/company_town/internal/session"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "ticket":
		err = handleTicket(args)
	case "prole":
		err = handleProle(args)
	case "agent":
		err = handleAgent(args)
	case "pr":
		err = handlePR(args)
	case "start":
		err = handleStart(args)
	case "stop":
		err = handleStop(args)
	case "status":
		err = handleStatus()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func handleTicket(args []string) error {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gt ticket <create|show|list|ready|assign|status|close|delete|depend> ...")
		os.Exit(1)
	}

	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	issues := repo.NewIssueRepo(conn)
	agents := repo.NewAgentRepo(conn)

	switch args[0] {
	case "create":
		return ticketCreate(issues, cfg.TicketPrefix, args[1:])
	case "show":
		return ticketShow(issues, cfg.TicketPrefix, args[1:])
	case "list":
		return ticketList(issues, cfg.TicketPrefix, args[1:])
	case "ready":
		return ticketReady(issues, cfg.TicketPrefix)
	case "assign":
		return ticketAssign(issues, agents, args[1:])
	case "status":
		return ticketStatus(issues, args[1:])
	case "close":
		return ticketClose(issues, agents, args[1:])
	case "delete":
		return ticketDelete(issues, args[1:])
	case "depend":
		return ticketDepend(issues, cfg.TicketPrefix, args[1:])
	default:
		return fmt.Errorf("unknown ticket command: %s", args[0])
	}
}

func ticketCreate(issues *repo.IssueRepo, prefix string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: gt ticket create <title> [--parent <id>] [--specialty <s>] [--type <t>]")
	}

	title := args[0]
	var parentID *int
	var specialty *string
	issueType := "task"

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--parent":
			if i+1 >= len(args) {
				return fmt.Errorf("--parent requires a value")
			}
			i++
			v, err := strconv.Atoi(args[i])
			if err != nil {
				return fmt.Errorf("invalid parent ID: %s", args[i])
			}
			parentID = &v
		case "--specialty":
			if i+1 >= len(args) {
				return fmt.Errorf("--specialty requires a value")
			}
			i++
			s := args[i]
			specialty = &s
		case "--type":
			if i+1 >= len(args) {
				return fmt.Errorf("--type requires a value")
			}
			i++
			issueType = args[i]
		}
	}

	id, err := issues.Create(title, issueType, parentID, specialty)
	if err != nil {
		return err
	}

	fmt.Printf("Created %s-%d: %s\n", prefix, id, title)
	return nil
}

func ticketShow(issues *repo.IssueRepo, prefix string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: gt ticket show <id>")
	}

	id, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid ticket ID: %s", args[0])
	}

	issue, err := issues.Get(id)
	if err != nil {
		return err
	}

	fmt.Printf("%s-%d  [%s]  %s\n", prefix, issue.ID, issue.Status, issue.Title)
	fmt.Printf("  type:      %s\n", issue.IssueType)
	if issue.Assignee.Valid {
		fmt.Printf("  assignee:  %s\n", issue.Assignee.String)
	}
	if issue.Branch.Valid {
		fmt.Printf("  branch:    %s\n", issue.Branch.String)
	}
	if issue.PRNumber.Valid {
		fmt.Printf("  pr:        #%d\n", issue.PRNumber.Int64)
	}
	if issue.Specialty.Valid {
		fmt.Printf("  specialty: %s\n", issue.Specialty.String)
	}
	if issue.ParentID.Valid {
		fmt.Printf("  parent:    %s-%d\n", prefix, issue.ParentID.Int64)
	}
	deps, err := issues.GetDependencies(id)
	if err != nil {
		return err
	}
	if len(deps) > 0 {
		depStrs := make([]string, len(deps))
		for i, d := range deps {
			depStrs[i] = fmt.Sprintf("%s-%d", prefix, d)
		}
		fmt.Printf("  depends:   %s\n", strings.Join(depStrs, ", "))
	}
	if issue.Description.Valid && issue.Description.String != "" {
		fmt.Printf("  ---\n  %s\n", issue.Description.String)
	}

	return nil
}

func ticketList(issues *repo.IssueRepo, prefix string, args []string) error {
	var status string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--status":
			if i+1 >= len(args) {
				return fmt.Errorf("--status requires a value")
			}
			i++
			status = args[i]
		}
	}

	list, err := issues.List(status)
	if err != nil {
		return err
	}

	if len(list) == 0 {
		fmt.Println("No tickets found.")
		return nil
	}

	for _, issue := range list {
		assignee := ""
		if issue.Assignee.Valid {
			assignee = fmt.Sprintf("  (%s)", issue.Assignee.String)
		}
		fmt.Printf("%-8s %-14s %s%s\n",
			fmt.Sprintf("%s-%d", prefix, issue.ID),
			"["+issue.Status+"]",
			issue.Title,
			assignee,
		)
	}
	return nil
}

func ticketReady(issues *repo.IssueRepo, prefix string) error {
	list, err := issues.Ready()
	if err != nil {
		return err
	}

	if len(list) == 0 {
		fmt.Println("No ready tickets.")
		return nil
	}

	for _, issue := range list {
		fmt.Printf("%-8s %s\n",
			fmt.Sprintf("%s-%d", prefix, issue.ID),
			issue.Title,
		)
	}
	return nil
}

func ticketAssign(issues *repo.IssueRepo, agents *repo.AgentRepo, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: gt ticket assign <ticket_id> <agent_name>")
	}

	id, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid ticket ID: %s", args[0])
	}

	agentName := args[1]

	// Get the issue to verify it exists
	issue, err := issues.Get(id)
	if err != nil {
		return err
	}

	branch := fmt.Sprintf("prole/%s/%d", agentName, issue.ID)
	if err := issues.Assign(id, agentName, branch); err != nil {
		return err
	}

	// Update agent's current issue
	if err := agents.SetCurrentIssue(agentName, &id); err != nil {
		return fmt.Errorf("setting agent current issue: %w", err)
	}

	fmt.Printf("Assigned ticket %d to %s (branch: %s)\n", id, agentName, branch)
	return nil
}

func ticketStatus(issues *repo.IssueRepo, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: gt ticket status <id> <status>")
	}

	id, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid ticket ID: %s", args[0])
	}

	status := args[1]
	if err := issues.UpdateStatus(id, status); err != nil {
		return err
	}

	fmt.Printf("Ticket %d → %s\n", id, status)
	return nil
}

func ticketClose(issues *repo.IssueRepo, agents *repo.AgentRepo, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: gt ticket close <id>")
	}

	id, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid ticket ID: %s", args[0])
	}

	// Get the issue to find assignee before closing
	issue, err := issues.Get(id)
	if err != nil {
		return err
	}

	if err := issues.Close(id); err != nil {
		return err
	}

	// Clear agent's current issue if there was an assignee
	if issue.Assignee.Valid && issue.Assignee.String != "" {
		if err := agents.ClearCurrentIssue(issue.Assignee.String); err != nil {
			// Log but don't fail — ticket is already closed
			fmt.Printf("Warning: could not clear agent current issue: %v\n", err)
		}
	}

	fmt.Printf("Ticket %d closed.\n", id)
	return nil
}

func ticketDelete(issues *repo.IssueRepo, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: gt ticket delete <id>")
	}

	id, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid ticket ID: %s", args[0])
	}

	if err := issues.Delete(id); err != nil {
		return err
	}

	fmt.Printf("Ticket %d deleted.\n", id)
	return nil
}

func ticketDepend(issues *repo.IssueRepo, prefix string, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: gt ticket depend <id> <depends-on-id>")
	}

	id, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid ticket ID: %s", args[0])
	}

	dependsOnID, err := strconv.Atoi(args[1])
	if err != nil {
		return fmt.Errorf("invalid depends-on ID: %s", args[1])
	}

	// Verify both tickets exist
	if _, err := issues.Get(id); err != nil {
		return fmt.Errorf("ticket %d: %w", id, err)
	}
	if _, err := issues.Get(dependsOnID); err != nil {
		return fmt.Errorf("ticket %d: %w", dependsOnID, err)
	}

	if err := issues.AddDependency(id, dependsOnID); err != nil {
		return err
	}

	fmt.Printf("%s-%d now depends on %s-%d\n", prefix, id, prefix, dependsOnID)
	return nil
}

func handleAgent(args []string) error {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gt agent <register|status> ...")
		os.Exit(1)
	}

	conn, _, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	agents := repo.NewAgentRepo(conn)

	switch args[0] {
	case "register":
		return agentRegister(agents, args[1:])
	case "status":
		return agentStatus(agents, args[1:])
	default:
		return fmt.Errorf("unknown agent command: %s", args[0])
	}
}

func agentRegister(agents *repo.AgentRepo, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: gt agent register <name> <type> [--specialty <s>]")
	}

	name := args[0]
	agentType := args[1]
	var specialty *string

	for i := 2; i < len(args); i++ {
		if args[i] == "--specialty" && i+1 < len(args) {
			i++
			s := args[i]
			specialty = &s
		}
	}

	if err := agents.Register(name, agentType, specialty); err != nil {
		return err
	}

	fmt.Printf("Registered agent %s (type=%s)\n", name, agentType)
	return nil
}

func agentStatus(agents *repo.AgentRepo, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: gt agent status <name> <idle|working|dead> [--issue <id>]")
	}

	name := args[0]
	status := args[1]
	var issueID *int

	for i := 2; i < len(args); i++ {
		if args[i] == "--issue" && i+1 < len(args) {
			i++
			v, err := strconv.Atoi(args[i])
			if err != nil {
				return fmt.Errorf("invalid issue ID: %s", args[i])
			}
			issueID = &v
		}
	}

	switch {
	case issueID != nil:
		if status != "working" {
			return fmt.Errorf("--issue requires status \"working\", got %q", status)
		}
		if err := agents.SetCurrentIssue(name, issueID); err != nil {
			return err
		}
		fmt.Printf("Agent %s → working (issue %d)\n", name, *issueID)
	case status == "idle":
		// Clear current issue and set idle
		if err := agents.ClearCurrentIssue(name); err != nil {
			return err
		}
		fmt.Printf("Agent %s → idle\n", name)
	default:
		if err := agents.UpdateStatus(name, status); err != nil {
			return err
		}
		fmt.Printf("Agent %s → %s\n", name, status)
	}

	return nil
}

func handleProle(args []string) error {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gt prole <create|reset|list> ...")
		os.Exit(1)
	}

	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	agents := repo.NewAgentRepo(conn)

	switch args[0] {
	case "create":
		if len(args) < 2 {
			return fmt.Errorf("usage: gt prole create <name>")
		}
		return prole.Create(args[1], cfg, agents)
	case "reset":
		if len(args) < 2 {
			return fmt.Errorf("usage: gt prole reset <name>")
		}
		return prole.Reset(args[1], cfg, agents)
	case "list":
		return proleList(agents, cfg)
	default:
		return fmt.Errorf("unknown prole command: %s", args[0])
	}
}

func proleList(agents *repo.AgentRepo, cfg *config.Config) error {
	proles, err := prole.List(agents, cfg)
	if err != nil {
		return err
	}

	if len(proles) == 0 {
		fmt.Println("No proles.")
		return nil
	}

	for _, p := range proles {
		issue := ""
		if p.CurrentIssue.Valid {
			issue = fmt.Sprintf("  → %s-%d", cfg.TicketPrefix, p.CurrentIssue.Int64)
		}
		session := ""
		if p.TmuxSession.Valid {
			session = fmt.Sprintf("  [%s]", p.TmuxSession.String)
		}
		fmt.Printf("  %-15s %-8s%s%s\n", p.Name, p.Status, issue, session)
	}
	return nil
}

func handlePR(args []string) error {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gt pr <create> ...")
		os.Exit(1)
	}

	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	issues := repo.NewIssueRepo(conn)

	switch args[0] {
	case "create":
		return prCreate(issues, cfg, args[1:])
	default:
		return fmt.Errorf("unknown pr command: %s", args[0])
	}
}

func prCreate(issues *repo.IssueRepo, cfg *config.Config, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: gt pr create <ticket_id>")
	}

	id, err := strconv.Atoi(args[0])
	if err != nil {
		return fmt.Errorf("invalid ticket ID: %s", args[0])
	}

	issue, err := issues.Get(id)
	if err != nil {
		return err
	}

	if !issue.Branch.Valid || issue.Branch.String == "" {
		return fmt.Errorf("ticket %d has no branch set", id)
	}

	// Push the branch first
	pushCmd := exec.Command("git", "push", "-u", "origin", "HEAD")
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if err := pushCmd.Run(); err != nil {
		return fmt.Errorf("pushing branch: %w", err)
	}

	// Build PR title and body
	prTitle := fmt.Sprintf("[%s-%d] %s", cfg.TicketPrefix, issue.ID, issue.Title)

	bodyParts := []string{"## Summary\n"}
	if issue.Description.Valid && issue.Description.String != "" {
		bodyParts = append(bodyParts, issue.Description.String)
	} else {
		bodyParts = append(bodyParts, issue.Title)
	}
	bodyParts = append(bodyParts, fmt.Sprintf("\n\nTicket: %s-%d", cfg.TicketPrefix, issue.ID))

	prBody := strings.Join(bodyParts, "\n")

	// Create PR with gh
	ghCmd := exec.Command("gh", "pr", "create",
		"--title", prTitle,
		"--body", prBody,
	)
	ghCmd.Stdout = os.Stdout
	ghCmd.Stderr = os.Stderr
	out, err := ghCmd.Output()
	if err != nil {
		return fmt.Errorf("creating PR: %w", err)
	}

	prURL := strings.TrimSpace(string(out))
	fmt.Printf("PR created: %s\n", prURL)

	// Extract PR number from URL (last path segment)
	parts := strings.Split(prURL, "/")
	if len(parts) > 0 {
		if prNum, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			issues.SetPR(id, prNum)
		}
	}

	// Move ticket to in_review
	if err := issues.UpdateStatus(id, "in_review"); err != nil {
		return fmt.Errorf("updating ticket status: %w", err)
	}

	fmt.Printf("Ticket %s-%d → in_review\n", cfg.TicketPrefix, id)
	return nil
}

func handleStatus() error {
	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	agents := repo.NewAgentRepo(conn)
	issues := repo.NewIssueRepo(conn)

	// Agents summary
	allAgents, err := agents.ListAll()
	if err != nil {
		return err
	}

	fmt.Println("=== Agents ===")
	if len(allAgents) == 0 {
		fmt.Println("  (none registered)")
	}
	for _, a := range allAgents {
		issue := ""
		if a.CurrentIssue.Valid {
			issue = fmt.Sprintf("  → %s-%d", cfg.TicketPrefix, a.CurrentIssue.Int64)
		}
		fmt.Printf("  %-20s %-10s %s%s\n", a.Name, a.Type, a.Status, issue)
	}

	// Ticket summary by status
	fmt.Println("\n=== Tickets ===")
	for _, status := range []string{"draft", "open", "in_progress", "in_review", "reviewed", "repairing"} {
		list, err := issues.List(status)
		if err != nil {
			return err
		}
		if len(list) > 0 {
			fmt.Printf("  %s: %d\n", status, len(list))
		}
	}

	return nil
}

func handleStart(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: gt start <architect|conductor|reviewer|janitor|artisan-SPECIALTY>")
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
				"Begin coordinating proles: check ready tickets and assign them.",
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
				"Begin patrol: check for in_review tickets and review their PRs.",
			cfg.TicketPrefix,
		)

	case name == "janitor":
		agentType = "janitor"
		model = cfg.Agents.Janitor.Model
		agentDir = filepath.Join(ctDir, "agents", "janitor")
		prompt = fmt.Sprintf(
			"You are the Janitor. Ticket prefix: %s. "+
				"Read your CLAUDE.md for instructions. "+
				"Check memory/handoff.md to resume previous work. "+
				"Begin patrol: clean up stale worktrees, prune dead sessions.",
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

		// Ensure artisan directory exists
		if err := os.MkdirAll(filepath.Join(agentDir, "memory"), 0755); err != nil {
			return fmt.Errorf("creating artisan directory: %w", err)
		}

		// Register with specialty if not already registered
		if _, err := agents.Get(name); err != nil {
			spec := specialty
			if regErr := agents.Register(name, agentType, &spec); regErr != nil {
				return fmt.Errorf("registering %s: %w", name, regErr)
			}
		}

	default:
		return fmt.Errorf("unknown agent: %s", name)
	}

	sessionName := "ct-" + name

	// If already running, just report
	if tmuxExists(sessionName) {
		fmt.Printf("%s is already running (session: %s)\n", name, sessionName)
		return nil
	}

	// Register in DB if needed (for non-artisans)
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

	// Create detached tmux session with claude using session.CreateInteractive
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

func handleStop(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: gt stop <agent-name>")
	}

	name := args[0]
	sessionName := "ct-" + name

	if !tmuxExists(sessionName) {
		fmt.Printf("%s is not running.\n", name)
		return nil
	}

	projectRoot, err := db.FindProjectRoot()
	if err != nil {
		return err
	}
	ctDir := config.CompanyTownDir(projectRoot)

	// Signal handoff for agents that support it
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

	// Send shutdown message
	cmd := exec.Command("tmux", "send-keys", "-t", sessionName, "System is shutting down. Write handoff.md and exit cleanly.", "Enter")
	cmd.Run()

	fmt.Printf("Signaled %s to shutdown. Check session %s for handoff.\n", name, sessionName)
	return nil
}

func tmuxExists(sessionName string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", sessionName)
	return cmd.Run() == nil
}

func printUsage() {
	fmt.Println(`Usage: gt <command>

Commands:
  ticket <create|show|list|ready|assign|status|close|depend>   Manage tickets
  prole <create|reset|list>                                     Manage proles
  agent <register|status>                                        Manage agents
  pr <create>                                                    File PRs
  start <agent>                                                  Start an agent
  stop <agent>                                                   Stop an agent (graceful)
  status                                                         Print system status`)
}
