package gtcmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
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

	// gitCommitCountFn counts commits on HEAD not reachable from origin/main.
	// Falls back to counting all HEAD commits if origin/main is unavailable,
	// and returns 0 if even that fails (unborn branch).
	gitCommitCountFn = func() (int, error) {
		cmd := exec.Command("git", "rev-list", "--count", "origin/main..HEAD")
		out, err := cmd.Output()
		if err != nil {
			cmd2 := exec.Command("git", "rev-list", "--count", "HEAD")
			out, err = cmd2.Output()
			if err != nil {
				return 0, nil
			}
		}
		n, err := strconv.Atoi(strings.TrimSpace(string(out)))
		if err != nil {
			return 0, fmt.Errorf("parsing commit count: %w", err)
		}
		return n, nil
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

	// ghPRViewFn fetches PR metadata (title, state, branch, checks, etc.).
	// Hard-errors on failure — the metadata is load-bearing.
	ghPRViewFn = func(prNum int, projectRoot string) ([]byte, error) {
		fields := "number,title,state,headRefName,mergeable,reviewDecision,statusCheckRollup"
		cmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNum), "--json", fields)
		cmd.Dir = projectRoot
		return cmd.Output()
	}

	// ghPRReviewsFn fetches structured PR reviews (APPROVED / CHANGES_REQUESTED / COMMENTED).
	// Soft-fails on error — see fetchPRShow.
	ghPRReviewsFn = func(prNum int, projectRoot string) ([]byte, error) {
		cmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNum), "--json", "reviews")
		cmd.Dir = projectRoot
		return cmd.Output()
	}

	// ghPRCommentsFn fetches free-form issue comments on the PR.
	// Soft-fails on error — see fetchPRShow.
	ghPRCommentsFn = func(prNum int, projectRoot string) ([]byte, error) {
		cmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNum), "--json", "comments")
		cmd.Dir = projectRoot
		return cmd.Output()
	}
)

// prCheckResult is a single CI check from statusCheckRollup.
type prCheckResult struct {
	Name       string
	Status     string
	Conclusion string
}

// prEntry is a unified activity entry — either a PR review or an issue comment.
// Reviews carry a verdict state (APPROVED, CHANGES_REQUESTED, COMMENTED);
// issue comments are free-form remarks. Both are surfaced together sorted by time.
type prEntry struct {
	Kind        string // "review:APPROVED", "review:CHANGES_REQUESTED", "review:COMMENTED", "comment"
	AuthorLogin string
	CreatedAt   string // ISO8601; sorts lexicographically
	Body        string
}

// prShowData holds the PR metadata and recent activity fetched from GitHub.
type prShowData struct {
	Number         int
	Title          string
	State          string
	HeadRefName    string
	Mergeable      string
	ReviewDecision string
	Checks         []prCheckResult
	Activity       []prEntry // unified reviews + issue comments, sorted asc, last activityLimit entries
}

const activityLimit = 5

// fetchPRShow shells out to gh to retrieve PR metadata, reviews, and issue
// comments. The metadata fetch is load-bearing and hard-errors; reviews and
// comments fetches soft-fail (log a warning to stderr, carry on with empty
// slices) so a stale or restricted endpoint does not abort the whole command.
func fetchPRShow(prNum int, projectRoot string) (*prShowData, error) {
	// --- Metadata (hard error) ---
	metaOut, err := ghPRViewFn(prNum, projectRoot)
	if err != nil {
		return nil, fmt.Errorf("gh pr view: %w", err)
	}

	var meta struct {
		Number            int    `json:"number"`
		Title             string `json:"title"`
		State             string `json:"state"`
		HeadRefName       string `json:"headRefName"`
		Mergeable         string `json:"mergeable"`
		ReviewDecision    string `json:"reviewDecision"`
		StatusCheckRollup []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal(metaOut, &meta); err != nil {
		return nil, fmt.Errorf("parsing pr view output: %w", err)
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

	var entries []prEntry

	// --- Reviews (soft-fail) ---
	reviewOut, err := ghPRReviewsFn(prNum, projectRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: reviews fetch failed: %v\n", err)
	} else {
		var resp struct {
			Reviews []struct {
				Author struct {
					Login string `json:"login"`
				} `json:"author"`
				State       string `json:"state"`
				SubmittedAt string `json:"submittedAt"`
				Body        string `json:"body"`
			} `json:"reviews"`
		}
		if parseErr := json.Unmarshal(reviewOut, &resp); parseErr != nil {
			fmt.Fprintf(os.Stderr, "warning: parsing reviews failed: %v\n", parseErr)
		} else {
			for _, r := range resp.Reviews {
				entries = append(entries, prEntry{
					Kind:        "review:" + r.State,
					AuthorLogin: r.Author.Login,
					CreatedAt:   r.SubmittedAt,
					Body:        r.Body,
				})
			}
		}
	}

	// --- Issue comments (soft-fail) ---
	commentOut, err := ghPRCommentsFn(prNum, projectRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: comments fetch failed: %v\n", err)
	} else {
		var resp struct {
			Comments []struct {
				Author struct {
					Login string `json:"login"`
				} `json:"author"`
				Body      string `json:"body"`
				CreatedAt string `json:"createdAt"`
			} `json:"comments"`
		}
		if parseErr := json.Unmarshal(commentOut, &resp); parseErr != nil {
			fmt.Fprintf(os.Stderr, "warning: parsing comments failed: %v\n", parseErr)
		} else {
			for _, c := range resp.Comments {
				entries = append(entries, prEntry{
					Kind:        "comment",
					AuthorLogin: c.Author.Login,
					CreatedAt:   c.CreatedAt,
					Body:        c.Body,
				})
			}
		}
	}

	// Sort by CreatedAt ascending (ISO8601 sorts lexicographically), then keep last N.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].CreatedAt < entries[j].CreatedAt
	})
	if len(entries) > activityLimit {
		entries = entries[len(entries)-activityLimit:]
	}
	data.Activity = entries
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

	// Verify the branch has commits before attempting a push.
	commitCount, err := gitCommitCountFn()
	if err != nil {
		return fmt.Errorf("counting commits on branch: %w", err)
	}
	if commitCount == 0 {
		return fmt.Errorf("ticket %s-%d branch %s has no commits yet — make at least one commit before running `gt pr create`",
			cfg.TicketPrefix, id, issue.Branch.String)
	}

	// Push the branch
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

	data, err := fetchPRShow(prNum, cfg.ProjectRoot)
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
	fmt.Printf("PR #%d . state: %s . branch: %s . mergeable: %s . review: %s\n",
		prNum, data.State, data.HeadRefName, mergeable, reviewDecision)

	// CI checks.
	fmt.Printf("\nChecks (%d):\n", len(data.Checks))
	if len(data.Checks) == 0 {
		fmt.Printf("  none\n")
	} else {
		pass, fail, running := 0, 0, 0
		for _, c := range data.Checks {
			switch c.Conclusion {
			case "SUCCESS":
				pass++
			case "FAILURE", "ERROR":
				fail++
			default:
				running++
			}
			conclusion := c.Conclusion
			if conclusion == "" {
				conclusion = c.Status
			}
			fmt.Printf("  %-40s %s\n", c.Name, conclusion)
		}
		fmt.Printf("  summary: %d pass / %d fail / %d running\n", pass, fail, running)
	}

	// Recent activity — unified reviews + issue comments, last activityLimit, newest last.
	fmt.Printf("\nActivity (last %d):\n", activityLimit)
	if len(data.Activity) == 0 {
		fmt.Printf("  none\n")
	}
	for i, e := range data.Activity {
		ts := e.CreatedAt
		if len(ts) > 10 {
			ts = ts[:10]
		}
		fmt.Printf("\n  [%d] %s . %s . %s\n", i+1, e.AuthorLogin, e.Kind, ts)
		for _, line := range strings.Split(strings.TrimRight(e.Body, "\n"), "\n") {
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
