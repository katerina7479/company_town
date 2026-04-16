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
	"github.com/katerina7479/company_town/internal/vcs"
)

// PR dispatches gt pr subcommands.
func PR(args []string) error {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gt pr <create|update|ready|show> ...")
		os.Exit(1)
	}

	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	issues := repo.NewIssueRepo(conn, nil)
	agents := repo.NewAgentRepo(conn, nil)

	workDir := resolveGitWorkDir(cfg, agents)

	switch args[0] {
	case "create":
		return prCreate(issues, cfg, workDir, args[1:])
	case "update":
		return prUpdate(issues, cfg, workDir, args[1:])
	case "ready":
		return prReady(issues, cfg, workDir, args[1:])
	case "show":
		return prShow(issues, cfg, args[1:])
	default:
		return fmt.Errorf("unknown pr command: %s", args[0])
	}
}

// resolveGitWorkDir returns the directory where git operations should run.
// For prole agents (identified by CT_AGENT_NAME env var with a registered
// worktree), this is the prole's own worktree so git commands never touch
// the shared main checkout. For other agents it falls back to cfg.ProjectRoot.
func resolveGitWorkDir(cfg *config.Config, agents *repo.AgentRepo) string {
	name := os.Getenv("CT_AGENT_NAME")
	if name == "" {
		return cfg.ProjectRoot
	}
	agent, err := agents.Get(name)
	if err != nil {
		return cfg.ProjectRoot
	}
	if agent.WorktreePath.Valid && agent.WorktreePath.String != "" {
		return agent.WorktreePath.String
	}
	return cfg.ProjectRoot
}

// formatPRTitle returns the canonical PR title: [PREFIX-ID] Title.
func formatPRTitle(prefix string, id int, title string) string {
	return fmt.Sprintf("[%s-%d] %s", prefix, id, title)
}

// vcsProvider is the VCS platform adapter. Override in tests to inject a mock.
var vcsProvider vcs.Provider = vcs.NewGitHub()

// Injection points for tests. Production code uses the real git binaries;
// tests replace these with stubs to avoid IO.
var (
	// gitPushFn pushes the current branch. workDir must be the prole's worktree
	// (or project root for non-prole agents) so the command runs in the right
	// git checkout rather than relying on process CWD.
	gitPushFn = func(workDir string, args ...string) error {
		cmd := exec.Command("git", append([]string{"push"}, args...)...)
		cmd.Dir = workDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		return cmd.Run()
	}

	// gitCommitCountFn counts commits on HEAD not reachable from origin/main.
	// Falls back to counting all HEAD commits if origin/main is unavailable,
	// and returns 0 if even that fails (unborn branch).
	// workDir must be the prole's worktree (or project root for non-prole agents).
	gitCommitCountFn = func(workDir string) (int, error) {
		cmd := exec.Command("git", "rev-list", "--count", "origin/main..HEAD")
		cmd.Dir = workDir
		out, err := cmd.Output()
		if err != nil {
			cmd2 := exec.Command("git", "rev-list", "--count", "HEAD")
			cmd2.Dir = workDir
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

	// gitCurrentBranchFn returns the name of the currently checked-out branch,
	// or the literal string "HEAD" if HEAD is detached. Matches the behavior of
	// `git rev-parse --abbrev-ref HEAD`.
	gitCurrentBranchFn = func(dir string) (string, error) {
		out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").Output()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(out)), nil
	}

	// gitDefaultBranchFn returns the name of the repository's default branch
	// (typically "main") by consulting origin/HEAD. Falls back to "main" if
	// origin/HEAD is not set — cheap and safe for fresh worktrees.
	gitDefaultBranchFn = func(dir string) (string, error) {
		out, err := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD").Output()
		if err != nil {
			return "main", nil
		}
		s := strings.TrimSpace(string(out))
		return strings.TrimPrefix(s, "origin/"), nil
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
	metaOut, err := vcsProvider.GetPRMetadata(prNum, projectRoot)
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
	reviewOut, err := vcsProvider.GetPRReviews(prNum, projectRoot)
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
	commentOut, err := vcsProvider.GetPRComments(prNum, projectRoot)
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

// assertBranchReadyForPR verifies that the current worktree is on a branch
// that can legally be the head of a PR against the repo's default branch.
// Returns an error with an actionable message if not. Always runs before any
// push or gh invocation so no remote state is mutated on failure.
func assertBranchReadyForPR(dir, ticketBranch string) error {
	current, err := gitCurrentBranchFn(dir)
	if err != nil {
		return fmt.Errorf("determining current branch: %w", err)
	}
	if current == "HEAD" {
		return fmt.Errorf("%w in %s; check out the ticket branch %q before running gt pr create", ErrHeadDetached, dir, ticketBranch)
	}
	defaultBranch, err := gitDefaultBranchFn(dir)
	if err != nil {
		return fmt.Errorf("determining default branch: %w", err)
	}
	if current == defaultBranch {
		return fmt.Errorf("current branch %q is the repository %w; nothing to PR", current, ErrDefaultBranch)
	}
	if ticketBranch != "" && current != ticketBranch {
		return fmt.Errorf("current branch %q does not match the ticket's recorded branch %q; check out the correct branch before running gt pr create", current, ticketBranch)
	}
	return nil
}

func prCreate(issues *repo.IssueRepo, cfg *config.Config, workDir string, args []string) error {
	var draft bool
	var filtered []string
	for _, a := range args {
		if a == "--draft" {
			draft = true
		} else {
			filtered = append(filtered, a)
		}
	}
	if len(filtered) < 1 {
		return fmt.Errorf("usage: gt pr create <ticket_id> [--draft]")
	}

	id, err := parseTicketID(filtered[0])
	if err != nil {
		return err
	}

	issue, err := issues.Get(id)
	if err != nil {
		return err
	}

	if !issue.Branch.Valid || issue.Branch.String == "" {
		return fmt.Errorf("ticket %d: %w", id, ErrNoBranchSet)
	}

	// Verify the branch has commits before attempting a push.
	commitCount, err := gitCommitCountFn(workDir)
	if err != nil {
		return fmt.Errorf("counting commits on branch: %w", err)
	}
	if commitCount == 0 {
		return fmt.Errorf("ticket %s-%d branch %s: %w — make at least one commit before running `gt pr create`",
			cfg.TicketPrefix, id, issue.Branch.String, ErrNoCommitsYet)
	}

	if err := assertBranchReadyForPR(workDir, issue.Branch.String); err != nil {
		return err
	}

	// Push the branch
	if err := gitPushFn(workDir, "-u", "origin", "HEAD"); err != nil {
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

	prURL, err := vcsProvider.CreatePR(prTitle, prBody, draft, workDir)
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

	// TDD tests tickets filed as draft PRs skip ci_running and go straight to
	// pr_open — CI failure is expected until a prole makes the tests green.
	// All other PRs (including non-TDD drafts) go through the normal ci_running path.
	if draft && issue.IssueType == "tdd_tests" {
		if err := issues.UpdateStatus(id, repo.StatusPROpen); err != nil {
			return fmt.Errorf("updating ticket status: %w", err)
		}
		fmt.Printf("Ticket %s-%d → pr_open (draft, tdd_tests)\n", cfg.TicketPrefix, id)
	} else {
		if err := issues.UpdateStatus(id, repo.StatusCIRunning); err != nil {
			return fmt.Errorf("updating ticket status: %w", err)
		}
		fmt.Printf("Ticket %s-%d → ci_running\n", cfg.TicketPrefix, id)
	}
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

// prReady implements `gt pr ready <ticket_id>`.
// It pushes latest commits, marks the draft PR as ready for review, and
// transitions the ticket to ci_running. Used by proles after TDD implementation
// work — the QA artisan filed a draft PR with failing tests; once the prole
// makes the tests pass, prReady converts the draft to a real PR and enters the
// normal CI → review → merge flow.
func prReady(issues *repo.IssueRepo, cfg *config.Config, workDir string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: gt pr ready <ticket_id>")
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
		return fmt.Errorf("ticket %s-%d: %w — run `gt pr create` first to file a draft PR",
			cfg.TicketPrefix, id, ErrNoPRSet)
	}
	prNum := int(issue.PRNumber.Int64)

	if !issue.Branch.Valid || issue.Branch.String == "" {
		return fmt.Errorf("ticket %d: %w", id, ErrNoBranchSet)
	}

	if err := assertBranchReadyForPR(workDir, issue.Branch.String); err != nil {
		return err
	}

	// Push latest commits so the ready PR contains the passing implementation.
	if err := gitPushFn(workDir, "origin", "HEAD"); err != nil {
		return fmt.Errorf("pushing branch: %w", err)
	}

	// Convert the draft PR to ready-for-review.
	if err := vcsProvider.MarkPRReady(prNum, cfg.ProjectRoot); err != nil {
		return fmt.Errorf("marking PR #%d ready: %w", prNum, err)
	}

	// Transition ticket to ci_running — same as after gt pr create.
	if err := issues.UpdateStatus(id, repo.StatusCIRunning); err != nil {
		return fmt.Errorf("updating ticket status: %w", err)
	}

	fmt.Printf("PR #%d marked ready\n", prNum)
	fmt.Printf("Ticket %s-%d → ci_running\n", cfg.TicketPrefix, id)
	return nil
}

func prUpdate(issues *repo.IssueRepo, cfg *config.Config, workDir string, args []string) error {
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

	if issue.Status != repo.StatusRepairing {
		return fmt.Errorf("ticket %d: %w (current: %s)", id, ErrNotRepairingStatus, issue.Status)
	}

	if !issue.Branch.Valid || issue.Branch.String == "" {
		return fmt.Errorf("ticket %d: %w", id, ErrNoBranchSet)
	}

	if err := assertBranchReadyForPR(workDir, issue.Branch.String); err != nil {
		return err
	}

	// Push latest changes
	if err := gitPushFn(workDir, "origin", "HEAD"); err != nil {
		return fmt.Errorf("pushing branch: %w", err)
	}

	// Move ticket back to ci_running. The prole stays assigned; the daemon
	// will promote to in_review once CI passes or route back to repairing if
	// CI fails again.
	if err := issues.UpdateStatus(id, repo.StatusCIRunning); err != nil {
		return fmt.Errorf("updating ticket status: %w", err)
	}

	fmt.Printf("Ticket %s-%d → ci_running\n", cfg.TicketPrefix, id)
	return nil
}
