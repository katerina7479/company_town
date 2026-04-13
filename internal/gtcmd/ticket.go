package gtcmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/katerina7479/company_town/internal/assign"
	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/eventlog"
	"github.com/katerina7479/company_town/internal/repo"
	"github.com/katerina7479/company_town/internal/session"
)

// Overridable in tests. Default to the real tmux-backed implementations.
var (
	assignSessionExists = session.Exists
	assignSendKeys      = session.SendKeys
)

// parseTicketID parses a ticket ID that may be in the form "PREFIX-N" (e.g. "nc-58")
// or a bare number (e.g. "58"). The prefix is stripped before parsing.
func parseTicketID(s string) (int, error) {
	raw := s
	if i := strings.Index(s, "-"); i >= 0 {
		raw = s[i+1:]
	}
	id, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid ticket ID: %s", s)
	}
	return id, nil
}

// Ticket dispatches gt ticket subcommands.
func Ticket(args []string) error {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gt ticket <create|show|list|ready|assign|status|type|priority|close|delete|depend> ...")
		os.Exit(1)
	}

	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	events := eventlog.NewLogger(config.CompanyTownDir(cfg.ProjectRoot))
	issues := repo.NewIssueRepo(conn, events)
	agents := repo.NewAgentRepo(conn, events)

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
		return ticketAssign(cfg, issues, agents, args[1:])
	case "review":
		return ticketReview(issues, args[1:])
	case "status":
		return ticketStatus(issues, args[1:])
	case "close":
		return ticketClose(issues, agents, args[1:])
	case "delete":
		return ticketDelete(issues, args[1:])
	case "depend":
		return ticketDepend(issues, cfg.TicketPrefix, args[1:])
	case "describe":
		return ticketDescribe(issues, args[1:])
	case "prioritize", "priority":
		return ticketPrioritize(issues, args[1:])
	case "type":
		return ticketType(issues, args[1:])
	default:
		return fmt.Errorf("unknown ticket command: %s", args[0])
	}
}

func ticketCreate(issues *repo.IssueRepo, prefix string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: gt ticket create <title> [--parent <id>] [--specialty <s>] [--type <t>] [--description <d>] [--priority <P0|P1|P2|P3>]")
	}

	var parentID *int
	var specialty *string
	var description string
	var priority *string
	issueType := "task"
	var positional []string

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--parent":
			if i+1 >= len(args) {
				return fmt.Errorf("--parent requires a value")
			}
			i++
			v, err := parseTicketID(args[i])
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
			if !isValidType(issueType) {
				return fmt.Errorf("invalid type %q: must be one of %v", issueType, repo.ValidTypes)
			}
		case "--description":
			if i+1 >= len(args) {
				return fmt.Errorf("--description requires a value")
			}
			i++
			description = args[i]
		case "--priority":
			if i+1 >= len(args) {
				return fmt.Errorf("--priority requires a value")
			}
			i++
			p := args[i]
			if !isValidPriority(p) {
				return fmt.Errorf("invalid priority %q: must be one of P0, P1, P2, P3", p)
			}
			priority = &p
		default:
			if strings.HasPrefix(args[i], "--") {
				return fmt.Errorf("unknown flag: %s", args[i])
			}
			positional = append(positional, args[i])
		}
	}

	if len(positional) == 0 {
		return fmt.Errorf("gt ticket create: title is required")
	}
	if len(positional) > 1 {
		return fmt.Errorf("gt ticket create: expected one title, got %d positional args (quote the title if it contains spaces): %v", len(positional), positional)
	}
	title := positional[0]

	id, err := issues.Create(title, issueType, parentID, specialty, priority)
	if err != nil {
		return err
	}

	if description != "" {
		if err := issues.UpdateDescription(id, description); err != nil {
			return err
		}
	}

	fmt.Printf("Created %s-%d: %s\n", prefix, id, title)
	return nil
}

func ticketShow(issues *repo.IssueRepo, prefix string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: gt ticket show <id>")
	}

	id, err := parseTicketID(args[0])
	if err != nil {
		return err
	}

	issue, err := issues.Get(id)
	if err != nil {
		return err
	}

	fmt.Printf("%s-%d  [%s]  %s\n", prefix, issue.ID, issue.Status, issue.Title)
	fmt.Printf("  type:      %s\n", issue.IssueType)
	if issue.Priority.Valid {
		fmt.Printf("  priority:  %s\n", issue.Priority.String)
	}
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
		priority := ""
		if issue.Priority.Valid {
			priority = fmt.Sprintf(" [%s]", issue.Priority.String)
		}
		assignee := ""
		if issue.Assignee.Valid {
			assignee = fmt.Sprintf("  (%s)", issue.Assignee.String)
		}
		fmt.Printf("%-8s %-14s %s%s%s\n",
			fmt.Sprintf("%s-%d", prefix, issue.ID),
			"["+issue.Status+"]",
			issue.Title,
			priority,
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

func ticketAssign(cfg *config.Config, issues *repo.IssueRepo, agents *repo.AgentRepo, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: gt ticket assign <ticket_id> <agent_name>")
	}

	id, err := parseTicketID(args[0])
	if err != nil {
		return err
	}

	agentName := args[1]

	if err := assign.Execute(cfg, issues, agents, id, agentName); err != nil {
		return err
	}

	branch := config.ProleBranchName(cfg.TicketPrefix, agentName, id)
	fmt.Printf("Assigned ticket %d to %s (branch: %s)\n", id, agentName, branch)

	// Nudge the agent's tmux session so it picks the work up immediately.
	// Without this, an agent that polled once and went idle won't notice the
	// new assignment until something else wakes it up.
	agent, err := agents.Get(agentName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not look up agent %s to nudge: %v\n", agentName, err)
		return nil
	}
	if !agent.TmuxSession.Valid || agent.TmuxSession.String == "" {
		fmt.Fprintf(os.Stderr, "warning: agent %s has no tmux session recorded; nudge skipped\n", agentName)
		return nil
	}
	if !assignSessionExists(agent.TmuxSession.String) {
		fmt.Fprintf(os.Stderr, "warning: session %s for %s is not running; nudge skipped\n", agent.TmuxSession.String, agentName)
		return nil
	}
	msg := fmt.Sprintf("You have been assigned ticket %d. Run `gt ticket show %d` and begin work per your CLAUDE.md lifecycle.", id, id)
	if err := assignSendKeys(agent.TmuxSession.String, msg); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to nudge %s: %v\n", agentName, err)
	}
	return nil
}

func ticketStatus(issues *repo.IssueRepo, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: gt ticket status <id> <status>")
	}

	id, err := parseTicketID(args[0])
	if err != nil {
		return err
	}

	status := args[1]

	if err := issues.UpdateStatus(id, status); err != nil {
		return err
	}

	fmt.Printf("Ticket %d → %s\n", id, status)
	return nil
}

func ticketReview(issues *repo.IssueRepo, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: gt ticket review <id> <approve|request-changes>")
	}

	id, err := parseTicketID(args[0])
	if err != nil {
		return err
	}
	verdict := args[1]

	issue, err := issues.Get(id)
	if err != nil {
		return err
	}
	if issue.Status != "under_review" {
		return fmt.Errorf("ticket %d is in %q, not under_review — cannot submit review verdict", id, issue.Status)
	}

	var newStatus string
	switch verdict {
	case "approve":
		newStatus = "pr_open"
	case "request-changes":
		newStatus = "repairing"
	default:
		return fmt.Errorf("unknown verdict %q (expected: approve | request-changes)", verdict)
	}

	if err := issues.UpdateStatus(id, newStatus); err != nil {
		return err
	}
	fmt.Printf("Ticket %d reviewed: %s → %s\n", id, verdict, newStatus)
	return nil
}

func ticketClose(issues *repo.IssueRepo, agents *repo.AgentRepo, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: gt ticket close <id>")
	}

	id, err := parseTicketID(args[0])
	if err != nil {
		return err
	}

	issue, err := issues.Get(id)
	if err != nil {
		return err
	}

	if err := issues.Close(id); err != nil {
		return err
	}

	if issue.Assignee.Valid && issue.Assignee.String != "" {
		if err := agents.ClearCurrentIssue(issue.Assignee.String); err != nil {
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

	id, err := parseTicketID(args[0])
	if err != nil {
		return err
	}

	if err := issues.Delete(id); err != nil {
		return err
	}

	fmt.Printf("Ticket %d deleted.\n", id)
	return nil
}

func ticketDescribe(issues *repo.IssueRepo, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: gt ticket describe <id> <description>")
	}

	id, err := parseTicketID(args[0])
	if err != nil {
		return err
	}

	description := args[1]

	if err := issues.UpdateDescription(id, description); err != nil {
		return err
	}

	fmt.Printf("Ticket %d description updated.\n", id)
	return nil
}

func isValidType(t string) bool {
	for _, v := range repo.ValidTypes {
		if t == v {
			return true
		}
	}
	return false
}

func isValidPriority(p string) bool {
	for _, v := range repo.ValidPriorities {
		if p == v {
			return true
		}
	}
	return false
}

func ticketPrioritize(issues *repo.IssueRepo, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: gt ticket prioritize <id> <P0|P1|P2|P3>")
	}

	id, err := parseTicketID(args[0])
	if err != nil {
		return err
	}

	priority := args[1]
	if !isValidPriority(priority) {
		return fmt.Errorf("invalid priority %q: must be one of P0, P1, P2, P3", priority)
	}

	if err := issues.SetPriority(id, priority); err != nil {
		return err
	}

	fmt.Printf("Ticket %d priority → %s\n", id, priority)
	return nil
}

func ticketType(issues *repo.IssueRepo, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: gt ticket type <id> <task|epic|bug|refactor>")
	}

	id, err := parseTicketID(args[0])
	if err != nil {
		return err
	}

	issueType := args[1]
	if !isValidType(issueType) {
		return fmt.Errorf("invalid type %q: must be one of %v", issueType, repo.ValidTypes)
	}

	if err := issues.UpdateType(id, issueType); err != nil {
		return err
	}

	fmt.Printf("Ticket %d type → %s\n", id, issueType)
	return nil
}

func ticketDepend(issues *repo.IssueRepo, prefix string, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: gt ticket depend <id> <depends-on-id>")
	}

	id, err := parseTicketID(args[0])
	if err != nil {
		return err
	}

	dependsOnID, err := parseTicketID(args[1])
	if err != nil {
		return err
	}

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
