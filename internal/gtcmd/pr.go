package gtcmd

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

// PR dispatches gt pr subcommands.
func PR(args []string) error {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gt pr <create|update> ...")
		os.Exit(1)
	}

	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	issues := repo.NewIssueRepo(conn, nil)

	switch args[0] {
	case "create":
		return prCreate(issues, cfg, args[1:])
	case "update":
		return prUpdate(issues, cfg, args[1:])
	default:
		return fmt.Errorf("unknown pr command: %s", args[0])
	}
}

// formatPRTitle returns the canonical PR title: [PREFIX-ID] Title.
func formatPRTitle(prefix string, id int, title string) string {
	return fmt.Sprintf("[%s-%d] %s", prefix, id, title)
}

// Injection points for tests. Production code uses the real git/gh binaries;
// tests replace these with stubs to avoid network/IO.
var (
	gitPushFn = func(args ...string) error {
		cmd := exec.Command("git", append([]string{"push"}, args...)...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	ghPRCreateFn = func(title, body string) (string, error) {
		cmd := exec.Command("gh", "pr", "create", "--title", title, "--body", body)
		cmd.Stderr = os.Stderr
		out, err := cmd.Output()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}
)

func prCreate(issues *repo.IssueRepo, cfg *config.Config, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: gt pr create <ticket_id>")
	}

	id, err := parseTicketID(args[0])
	if err != nil {
		return err
	}

	issue, err := issues.Get(id)
	if err != nil {
		return err
	}

	if !issue.Branch.Valid || issue.Branch.String == "" {
		return fmt.Errorf("ticket %d has no branch set", id)
	}

	// Push the branch first
	if err := gitPushFn("-u", "origin", "HEAD"); err != nil {
		return fmt.Errorf("pushing branch: %w", err)
	}

	// Build PR title and body
	prTitle := formatPRTitle(cfg.TicketPrefix, issue.ID, issue.Title)

	bodyParts := []string{"## Summary\n"}
	if issue.Description.Valid && issue.Description.String != "" {
		bodyParts = append(bodyParts, issue.Description.String)
	} else {
		bodyParts = append(bodyParts, issue.Title)
	}
	bodyParts = append(bodyParts, fmt.Sprintf("\n\nTicket: %s-%d", cfg.TicketPrefix, issue.ID))

	prBody := strings.Join(bodyParts, "\n")

	prURL, err := ghPRCreateFn(prTitle, prBody)
	if err != nil {
		return fmt.Errorf("creating PR: %w", err)
	}
	fmt.Println(prURL)

	// Extract PR number from URL (last path segment)
	parts := strings.Split(prURL, "/")
	if len(parts) > 0 {
		if prNum, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			if err := issues.SetPR(id, prNum); err != nil {
				return fmt.Errorf("recording PR number on ticket: %w", err)
			}
		}
	}

	// Move ticket to in_review and clear the assignee. Clearing lets the
	// daemon's orphan-reconcile recover the ticket if the prole dies while
	// under review, and keeps the dashboard from showing the ticket as owned
	// by an idle/deleted prole. Supersedes the nc-41 "preserve assignee
	// through review" policy — attribution is sacrificed for orphan recovery.
	// See NC-50.
	if err := issues.UpdateStatus(id, "in_review"); err != nil {
		return fmt.Errorf("updating ticket status: %w", err)
	}
	if err := issues.ClearAssignee(id); err != nil {
		return fmt.Errorf("clearing ticket assignee: %w", err)
	}

	fmt.Printf("Ticket %s-%d → in_review\n", cfg.TicketPrefix, id)
	return nil
}

func prUpdate(issues *repo.IssueRepo, cfg *config.Config, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: gt pr update <ticket_id>")
	}

	id, err := parseTicketID(args[0])
	if err != nil {
		return err
	}

	issue, err := issues.Get(id)
	if err != nil {
		return err
	}

	if issue.Status != "repairing" {
		return fmt.Errorf("ticket %d is not in repairing status (current: %s)", id, issue.Status)
	}

	if !issue.Branch.Valid || issue.Branch.String == "" {
		return fmt.Errorf("ticket %d has no branch set", id)
	}

	// Push latest changes
	if err := gitPushFn("origin", "HEAD"); err != nil {
		return fmt.Errorf("pushing branch: %w", err)
	}

	// Move ticket back to in_review
	if err := issues.UpdateStatus(id, "in_review"); err != nil {
		return fmt.Errorf("updating ticket status: %w", err)
	}

	// Clear assignee so the daemon's orphan-reconcile loop can recover the
	// ticket if the prole dies while under review. Mirror of prCreate — see
	// NC-50. Supersedes the nc-41 "preserve assignee through review" policy.
	if err := issues.ClearAssignee(id); err != nil {
		return fmt.Errorf("clearing ticket assignee: %w", err)
	}

	fmt.Printf("Ticket %s-%d → in_review\n", cfg.TicketPrefix, id)
	return nil
}
