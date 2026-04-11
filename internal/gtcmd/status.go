package gtcmd

import (
	"fmt"

	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
	"github.com/katerina7479/company_town/internal/session"
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

	if err := reconcileDeadAgents(agents, session.Exists); err != nil {
		return err
	}

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
	for _, status := range []string{"draft", "open", "in_progress", "in_review", "under_review", "pr_open", "reviewed", "repairing", "on_hold"} {
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

// reconcileDeadAgents marks agents as dead when their tmux session no longer
// exists, keeping the DB in sync with reality. Mirrors the daemon's
// handleDeadSessions so `gt status` is accurate even if the daemon is down.
func reconcileDeadAgents(agents *repo.AgentRepo, sessionExists func(string) bool) error {
	all, err := agents.ListAll()
	if err != nil {
		return err
	}
	for _, a := range all {
		if a.Status == "dead" {
			continue
		}
		if !a.TmuxSession.Valid || a.TmuxSession.String == "" {
			continue
		}
		if sessionExists(a.TmuxSession.String) {
			continue
		}
		if a.CurrentIssue.Valid {
			if err := agents.ClearCurrentIssue(a.Name); err != nil {
				return fmt.Errorf("clearing current issue for %s: %w", a.Name, err)
			}
		}
		if err := agents.UpdateStatus(a.Name, "dead"); err != nil {
			return fmt.Errorf("marking agent %s dead: %w", a.Name, err)
		}
	}
	return nil
}
