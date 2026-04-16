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

	agents := repo.NewAgentRepo(conn, nil)
	issues := repo.NewIssueRepo(conn, nil)

	if err := reconcileDeadAgents(agents, session.New().Exists); err != nil {
		return err
	}

	allAgents, err := agents.ListAll()
	if err != nil {
		return err
	}

	driftEntries, driftErr := repo.CheckDrift(agents, issues, cfg.TicketPrefix)

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

	// Drift is rendered as a section below the agent table rather than as an
	// inline ⚠ marker on each agent row. The section-at-bottom approach keeps
	// the agent table compact and groups all drift together where an operator
	// scanning for problems will see it all at once. The spec suggested inline
	// markers; this deviation is intentional and should not be reverted.
	if driftErr == nil && len(driftEntries) > 0 {
		fmt.Println("\n=== Drift Warnings ===")
		for _, d := range driftEntries {
			fmt.Printf("  ⚠ DRIFT  %s\n", d.Reason)
		}
	}

	fmt.Println("\n=== Tickets ===")
	for _, status := range []string{repo.StatusDraft, repo.StatusOpen, repo.StatusInProgress, repo.StatusInReview, repo.StatusUnderReview, repo.StatusPROpen, repo.StatusReviewed, repo.StatusRepairing, repo.StatusOnHold} {
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

// reconcileDeadAgents keeps the agents table in sync with tmux reality.
// Proles without a live tmux session are deleted outright (they're ephemeral).
// Core agents (reviewer, architect, etc.) are marked dead so their row is
// retained for restart cooldowns and history.
func reconcileDeadAgents(agents *repo.AgentRepo, sessionExists func(string) bool) error {
	all, err := agents.ListAll()
	if err != nil {
		return err
	}
	for _, a := range all {
		if a.TmuxSession.Valid && a.TmuxSession.String != "" && sessionExists(a.TmuxSession.String) {
			continue
		}
		if a.Type == "prole" {
			if err := agents.Delete(a.Name); err != nil {
				return fmt.Errorf("deleting prole %s: %w", a.Name, err)
			}
			continue
		}
		if a.Status == repo.StatusDead {
			continue
		}
		if a.CurrentIssue.Valid {
			if err := agents.ClearCurrentIssue(a.Name); err != nil {
				return fmt.Errorf("clearing current issue for %s: %w", a.Name, err)
			}
		}
		if err := agents.UpdateStatus(a.Name, repo.StatusDead); err != nil {
			return fmt.Errorf("marking agent %s dead: %w", a.Name, err)
		}
	}
	return nil
}
