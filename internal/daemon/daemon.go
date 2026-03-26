package daemon

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/repo"
	"github.com/katerina7479/company_town/internal/session"
)

// Daemon polls for state changes and routes work to agents.
type Daemon struct {
	cfg           *config.Config
	issues        *repo.IssueRepo
	agents        *repo.AgentRepo
	logger        *log.Logger
	stop          chan struct{}
	sendKeys      func(session, msg string) error
	sessionExists func(session string) bool
}

// New creates a new Daemon.
func New(db *sql.DB, cfg *config.Config) (*Daemon, error) {
	ctDir := config.CompanyTownDir(cfg.ProjectRoot)
	logPath := filepath.Join(ctDir, "logs", "daemon.log")

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening daemon log: %w", err)
	}

	return &Daemon{
		cfg:           cfg,
		issues:        repo.NewIssueRepo(db),
		agents:        repo.NewAgentRepo(db),
		logger:        log.New(f, "[DAEMON] ", log.LstdFlags),
		stop:          make(chan struct{}),
		sendKeys:      session.SendKeys,
		sessionExists: session.Exists,
	}, nil
}

// Run starts the polling loop. Blocks until Stop() is called.
func (d *Daemon) Run() {
	interval := time.Duration(d.cfg.PollingIntervalSeconds) * time.Second
	d.logger.Printf("Daemon started (polling every %s)", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Run once immediately
	d.poll()

	for {
		select {
		case <-ticker.C:
			d.poll()
		case <-d.stop:
			d.logger.Println("Daemon stopped")
			return
		}
	}
}

// Stop signals the daemon to stop.
func (d *Daemon) Stop() {
	close(d.stop)
}

func (d *Daemon) poll() {
	d.handleDraftTickets()
	d.handleOpenTickets()
	d.handleInReviewTickets()
	d.handleRepairingTickets()
	d.handlePREvents()
}

// handleOpenTickets nudges the Conductor when ready tickets are waiting for assignment.
func (d *Daemon) handleOpenTickets() {
	ready, err := d.issues.Ready()
	if err != nil {
		d.logger.Printf("error listing ready tickets: %v", err)
		return
	}

	// Filter to unassigned tickets only.
	var unassigned []*repo.Issue
	for _, issue := range ready {
		if !issue.Assignee.Valid || issue.Assignee.String == "" {
			unassigned = append(unassigned, issue)
		}
	}

	if len(unassigned) == 0 {
		return
	}

	conductorSession := session.SessionName("conductor")
	if !session.Exists(conductorSession) {
		return // Conductor not running, nothing to do
	}

	ids := make([]string, len(unassigned))
	for i, issue := range unassigned {
		ids[i] = fmt.Sprintf("%s-%d", d.cfg.TicketPrefix, issue.ID)
	}

	msg := fmt.Sprintf(
		"%d unassigned ticket(s) ready for assignment: %s. "+
			"Run `gt ticket list --status open` and assign them to idle agents.",
		len(unassigned), strings.Join(ids, ", "),
	)

	if err := session.SendKeys(conductorSession, msg); err != nil {
		d.logger.Printf("error nudging conductor: %v", err)
	} else {
		d.logger.Printf("nudged conductor: %d ready ticket(s) unassigned (%s)",
			len(unassigned), strings.Join(ids, ", "))
	}
}

// handleDraftTickets prompts the Architect to pick up draft tickets.
func (d *Daemon) handleDraftTickets() {
	drafts, err := d.issues.List("draft")
	if err != nil {
		d.logger.Printf("error listing draft tickets: %v", err)
		return
	}

	if len(drafts) == 0 {
		return
	}

	architectSession := session.SessionName("architect")
	if !d.sessionExists(architectSession) {
		return // Architect not running, nothing to do
	}

	for _, issue := range drafts {
		msg := fmt.Sprintf("Draft ticket %s-%d needs spec: %s. "+
			"Run `gt ticket show %d` and begin specification.",
			d.cfg.TicketPrefix, issue.ID, issue.Title, issue.ID)

		if err := d.sendKeys(architectSession, msg); err != nil {
			d.logger.Printf("error nudging architect for ticket %d: %v", issue.ID, err)
		} else {
			d.logger.Printf("nudged architect for draft ticket %s-%d", d.cfg.TicketPrefix, issue.ID)
		}
	}
}

// handleInReviewTickets prompts Reviewer to review tickets in in_review status.
func (d *Daemon) handleInReviewTickets() {
	reviews, err := d.issues.List("in_review")
	if err != nil {
		d.logger.Printf("error listing in_review tickets: %v", err)
		return
	}

	if len(reviews) == 0 {
		return
	}

	reviewerSession := session.SessionName("reviewer")
	if !d.sessionExists(reviewerSession) {
		return // Reviewer not running
	}

	for _, issue := range reviews {
		if !issue.PRNumber.Valid {
			continue
		}

		msg := fmt.Sprintf("PR #%d for ticket %s-%d is ready for review: %s. "+
			"Review the PR and file comments.",
			issue.PRNumber.Int64, d.cfg.TicketPrefix, issue.ID, issue.Title)

		if err := d.sendKeys(reviewerSession, msg); err != nil {
			d.logger.Printf("error nudging Reviewer for ticket %d: %v", issue.ID, err)
		} else {
			d.logger.Printf("nudged Reviewer for in_review ticket %s-%d (PR #%d)",
				d.cfg.TicketPrefix, issue.ID, issue.PRNumber.Int64)
		}
	}
}

// handleRepairingTickets prompts the Conductor to assign a prole to fix review comments.
func (d *Daemon) handleRepairingTickets() {
	tickets, err := d.issues.List("repairing")
	if err != nil {
		d.logger.Printf("error listing repairing tickets: %v", err)
		return
	}

	if len(tickets) == 0 {
		return
	}

	conductorSession := session.SessionName("conductor")
	if !d.sessionExists(conductorSession) {
		return // Conductor not running
	}

	for _, issue := range tickets {
		pr := ""
		if issue.PRNumber.Valid {
			pr = fmt.Sprintf(" (PR #%d)", issue.PRNumber.Int64)
		}
		msg := fmt.Sprintf("Ticket %s-%d%s needs repair: %s. "+
			"Assign an available prole to address the review comments.",
			d.cfg.TicketPrefix, issue.ID, pr, issue.Title)

		if err := d.sendKeys(conductorSession, msg); err != nil {
			d.logger.Printf("error nudging Conductor for repairing ticket %d: %v", issue.ID, err)
		} else {
			d.logger.Printf("nudged Conductor for repairing ticket %s-%d", d.cfg.TicketPrefix, issue.ID)
		}
	}
}

// handlePREvents checks GitHub for PR state changes.
func (d *Daemon) handlePREvents() {
	// Find all tickets with PR numbers that aren't closed
	tickets, err := d.issues.ListWithPRs()
	if err != nil {
		d.logger.Printf("error listing tickets with PRs: %v", err)
		return
	}

	for _, issue := range tickets {
		if !issue.PRNumber.Valid {
			continue
		}

		prNum := int(issue.PRNumber.Int64)
		state, merged, err := d.getPRState(prNum)
		if err != nil {
			d.logger.Printf("error checking PR #%d: %v", prNum, err)
			continue
		}

		switch {
		case state == "MERGED" || merged:
			d.handlePRMerged(issue)
		case state == "CLOSED":
			d.handlePRClosed(issue)
		case state == "OPEN":
			d.checkForHumanComments(issue, prNum)
		}
	}
}

func (d *Daemon) handlePRMerged(issue *repo.Issue) {
	if issue.Status == "closed" {
		return // already handled
	}

	d.logger.Printf("PR #%d merged for ticket %s-%d",
		issue.PRNumber.Int64, d.cfg.TicketPrefix, issue.ID)

	if err := d.issues.UpdateStatus(issue.ID, "closed"); err != nil {
		d.logger.Printf("error closing ticket %d: %v", issue.ID, err)
		return
	}

	// Free the assignee agent so it can pick up new work
	if issue.Assignee.Valid {
		if err := d.agents.ClearCurrentIssue(issue.Assignee.String); err != nil {
			d.logger.Printf("error clearing current issue for agent %s: %v",
				issue.Assignee.String, err)
		} else {
			d.logger.Printf("freed agent %s after PR #%d merged",
				issue.Assignee.String, issue.PRNumber.Int64)
		}
	}

	// Notify Mayor
	mayorSession := session.SessionName("mayor")
	if d.sessionExists(mayorSession) {
		msg := fmt.Sprintf("PR #%d merged. Ticket %s-%d (%s) is now closed.",
			issue.PRNumber.Int64, d.cfg.TicketPrefix, issue.ID, issue.Title)
		d.sendKeys(mayorSession, msg)
	}
}

func (d *Daemon) handlePRClosed(issue *repo.Issue) {
	if issue.Status == "closed" {
		return
	}

	d.logger.Printf("PR #%d closed without merge for ticket %s-%d — escalating to Mayor",
		issue.PRNumber.Int64, d.cfg.TicketPrefix, issue.ID)

	// Escalate to Mayor
	mayorSession := session.SessionName("mayor")
	if d.sessionExists(mayorSession) {
		msg := fmt.Sprintf("ESCALATION: PR #%d for ticket %s-%d (%s) was closed without merging. "+
			"Please decide next action.",
			issue.PRNumber.Int64, d.cfg.TicketPrefix, issue.ID, issue.Title)
		d.sendKeys(mayorSession, msg)
	}
}

func (d *Daemon) checkForHumanComments(issue *repo.Issue, prNum int) {
	if issue.Status == "repairing" {
		return // already in repair flow
	}

	comments, err := d.getReviewComments(prNum)
	if err != nil {
		d.logger.Printf("error checking comments on PR #%d: %v", prNum, err)
		return
	}

	// Look for human comments (not from bots or Reviewer agent)
	for _, c := range comments {
		if c.IsBot || c.Author == "reviewer" {
			continue
		}

		d.logger.Printf("human comment on PR #%d by %s — moving ticket %s-%d to repairing",
			prNum, c.Author, d.cfg.TicketPrefix, issue.ID)

		if err := d.issues.UpdateStatus(issue.ID, "repairing"); err != nil {
			d.logger.Printf("error updating ticket %d to repairing: %v", issue.ID, err)
			return
		}

		// Notify Mayor about the repair need
		mayorSession := session.SessionName("mayor")
		if d.sessionExists(mayorSession) {
			msg := fmt.Sprintf("PR #%d for ticket %s-%d has human review comments from %s. "+
				"Ticket moved to repairing. Conductor should assign to an available agent.",
				prNum, d.cfg.TicketPrefix, issue.ID, c.Author)
			d.sendKeys(mayorSession, msg)
		}

		return // only need one human comment to trigger repair
	}
}

// PRState from GitHub API
type prComment struct {
	Author string
	IsBot  bool
}

func (d *Daemon) getPRState(prNum int) (state string, merged bool, err error) {
	cmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNum), "--json", "state,mergedAt")
	cmd.Dir = d.cfg.ProjectRoot
	out, err := cmd.Output()
	if err != nil {
		return "", false, fmt.Errorf("gh pr view: %w", err)
	}

	var result struct {
		State    string  `json:"state"`
		MergedAt *string `json:"mergedAt"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", false, fmt.Errorf("parsing PR state: %w", err)
	}

	return result.State, result.MergedAt != nil, nil
}

func (d *Daemon) getReviewComments(prNum int) ([]prComment, error) {
	cmd := exec.Command("gh", "api",
		fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/reviews", prNum),
		"--jq", ".[] | {author: .user.login, authorType: .user.type, state: .state}")
	cmd.Dir = d.cfg.ProjectRoot
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh api: %w", err)
	}

	if len(strings.TrimSpace(string(out))) == 0 {
		return nil, nil
	}

	var comments []prComment
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		var review struct {
			Author     string `json:"author"`
			AuthorType string `json:"authorType"`
			State      string `json:"state"`
		}
		if err := json.Unmarshal([]byte(line), &review); err != nil {
			continue
		}
		comments = append(comments, prComment{
			Author: review.Author,
			IsBot:  review.AuthorType == "Bot",
		})
	}

	return comments, nil
}
