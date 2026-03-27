package gtcmd

import (
	"fmt"

	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

// Status prints a summary of all agents and tickets.
func Status() error {
	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	agents := repo.NewAgentRepo(conn)
	issues := repo.NewIssueRepo(conn)

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
