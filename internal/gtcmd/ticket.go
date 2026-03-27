package gtcmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

// Ticket dispatches gt ticket subcommands.
func Ticket(args []string) error {
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

	issue, err := issues.Get(id)
	if err != nil {
		return err
	}

	branch := fmt.Sprintf("prole/%s/%d", agentName, issue.ID)
	if err := issues.Assign(id, agentName, branch); err != nil {
		return err
	}

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
