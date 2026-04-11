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

	issues := repo.NewIssueRepo(conn)

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
	pushCmd := exec.Command("git", "push", "-u", "origin", "HEAD")
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if err := pushCmd.Run(); err != nil {
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

	// Create PR with gh — capture stdout to extract the URL, pipe stderr to terminal.
	ghCmd := exec.Command("gh", "pr", "create",
		"--title", prTitle,
		"--body", prBody,
	)
	ghCmd.Stderr = os.Stderr
	out, err := ghCmd.Output()
	if err != nil {
		return fmt.Errorf("creating PR: %w", err)
	}

	prURL := strings.TrimSpace(string(out))
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

	// Move ticket to in_review
	if err := issues.UpdateStatus(id, "in_review"); err != nil {
		return fmt.Errorf("updating ticket status: %w", err)
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
	pushCmd := exec.Command("git", "push", "origin", "HEAD")
	pushCmd.Stdout = os.Stdout
	pushCmd.Stderr = os.Stderr
	if err := pushCmd.Run(); err != nil {
		return fmt.Errorf("pushing branch: %w", err)
	}

	// Move ticket back to in_review
	if err := issues.UpdateStatus(id, "in_review"); err != nil {
		return fmt.Errorf("updating ticket status: %w", err)
	}

	fmt.Printf("Ticket %s-%d → in_review\n", cfg.TicketPrefix, id)
	return nil
}
