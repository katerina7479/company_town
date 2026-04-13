package gtcmd

import (
	"encoding/json"
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
		fmt.Fprintln(os.Stderr, "usage: gt pr <create|update|show> ...")
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
	case "show":
		return prShow(issues, cfg, args[1:])
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

	ghPRShowFn = func(prNum int, projectRoot string) (*prShowData, error) {
		return fetchPRShow(prNum, projectRoot)
	}
)

// prShowData holds the PR metadata and recent reviews fetched from GitHub.
type prShowData struct {
	Number         int
	Title          string
	State          string
	HeadRefName    string
	Mergeable      string
	ReviewDecision string
	Checks         []prCheckResult
	Reviews        []prReviewEntry
}

// prCheckResult is a single CI check from statusCheckRollup.
type prCheckResult struct {
	Name       string
	Status     string
	Conclusion string
}

// prReviewEntry is a single PR review (gh calls these "reviews").
type prReviewEntry struct {
	AuthorLogin string
	State       string
	SubmittedAt string
	Body        string
}

// fetchPRShow shells out to gh to retrieve PR metadata and reviews.
func fetchPRShow(prNum int, projectRoot string) (*prShowData, error) {
	// Fetch metadata.
	metaFields := "number,title,state,headRefName,mergeable,reviewDecision,statusCheckRollup"
	metaCmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNum), "--json", metaFields)
	metaCmd.Dir = projectRoot
	metaOut, err := metaCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh pr view: %w", err)
	}

	var meta struct {
		Number         int    `json:"number"`
		Title          string `json:"title"`
		State          string `json:"state"`
		HeadRefName    string `json:"headRefName"`
		Mergeable      string `json:"mergeable"`
		ReviewDecision string `json:"reviewDecision"`
		StatusCheckRollup []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal(metaOut, &meta); err != nil {
		return nil, fmt.Errorf("parsing pr view output: %w", err)
	}

	// Fetch reviews.
	reviewCmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNum), "--json", "reviews")
	reviewCmd.Dir = projectRoot
	reviewOut, err := reviewCmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh pr view --json reviews: %w", err)
	}

	var reviewResp struct {
		Reviews []struct {
			Author      struct{ Login string `json:"login"` } `json:"author"`
			State       string `json:"state"`
			SubmittedAt string `json:"submittedAt"`
			Body        string `json:"body"`
		} `json:"reviews"`
	}
	if err := json.Unmarshal(reviewOut, &reviewResp); err != nil {
		return nil, fmt.Errorf("parsing reviews output: %w", err)
	}

	data := &prShowData{
		Number:         meta.Number,
		Title:          meta.Title,
		State:          meta.State,
		HeadRefName:    meta.HeadRefName,
		Mergeable:      meta.Mergeable,
		ReviewDecision: meta.ReviewDecision,
	}
	for _, c := range meta.StatusCheckRollup {
		data.Checks = append(data.Checks, prCheckResult{
			Name:       c.Name,
			Status:     c.Status,
			Conclusion: c.Conclusion,
		})
	}
	for _, r := range reviewResp.Reviews {
		data.Reviews = append(data.Reviews, prReviewEntry{
			AuthorLogin: r.Author.Login,
			State:       r.State,
			SubmittedAt: r.SubmittedAt,
			Body:        r.Body,
		})
	}
	return data, nil
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

func prShow(issues *repo.IssueRepo, cfg *config.Config, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: gt pr show <ticket_id>")
	}

	id, err := parseTicketID(args[0])
	if err != nil {
		return err
	}

	issue, err := issues.Get(id)
	if err != nil {
		return err
	}

	if !issue.PRNumber.Valid {
		return fmt.Errorf("ticket %s-%d has no PR number set", cfg.TicketPrefix, id)
	}
	prNum := int(issue.PRNumber.Int64)

	data, err := ghPRShowFn(prNum, cfg.ProjectRoot)
	if err != nil {
		return fmt.Errorf("fetching PR #%d: %w", prNum, err)
	}

	// Header line: ticket + PR state.
	fmt.Printf("%s-%d [%s] %s\n", cfg.TicketPrefix, id, issue.Status, issue.Title)

	// PR metadata line.
	mergeable := data.Mergeable
	if mergeable == "" {
		mergeable = "unknown"
	}
	reviewDecision := data.ReviewDecision
	if reviewDecision == "" {
		reviewDecision = "none"
	}
	fmt.Printf("PR #%d · state: %s · branch: %s · mergeable: %s · review: %s\n",
		prNum, data.State, data.HeadRefName, mergeable, reviewDecision)

	// CI checks summary.
	fmt.Printf("\nChecks (%d):\n", len(data.Checks))
	if len(data.Checks) == 0 {
		fmt.Printf("  none\n")
	} else {
		for _, c := range data.Checks {
			conclusion := c.Conclusion
			if conclusion == "" {
				conclusion = c.Status
			}
			fmt.Printf("  %-40s %s\n", c.Name, conclusion)
		}
	}

	// Recent reviews — last 5, newest last.
	const maxReviews = 5
	reviews := data.Reviews
	if len(reviews) > maxReviews {
		reviews = reviews[len(reviews)-maxReviews:]
	}
	fmt.Printf("\nReviews (%d", len(data.Reviews))
	if len(data.Reviews) > maxReviews {
		fmt.Printf(", showing last %d", maxReviews)
	}
	fmt.Printf("):\n")
	if len(reviews) == 0 {
		fmt.Printf("  none\n")
	}
	for i, r := range reviews {
		// Trim submittedAt to date portion for readability.
		submittedAt := r.SubmittedAt
		if len(submittedAt) > 10 {
			submittedAt = submittedAt[:10]
		}
		fmt.Printf("\n  [%d] %s · %s · %s\n", i+1, r.AuthorLogin, r.State, submittedAt)
		// Indent each line of the body.
		for _, line := range strings.Split(strings.TrimRight(r.Body, "\n"), "\n") {
			fmt.Printf("      %s\n", line)
		}
	}

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
