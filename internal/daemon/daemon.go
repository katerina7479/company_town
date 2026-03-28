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
	"github.com/katerina7479/company_town/internal/prole"
	"github.com/katerina7479/company_town/internal/quality"
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
	resetWorktree func(name string) error
	lastNudged          map[string]time.Time
	nudgeCooldown       time.Duration
	stuckAgentThreshold time.Duration
	nowFn               func() time.Time

	// Quality baseline
	runQualityBaseline  func() error
	lastQualityBaseline time.Time
	qualityInterval     time.Duration
}

// New creates a new Daemon.
func New(db *sql.DB, cfg *config.Config) (*Daemon, error) {
	ctDir := config.CompanyTownDir(cfg.ProjectRoot)
	logPath := filepath.Join(ctDir, "logs", "daemon.log")

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("opening daemon log: %w", err)
	}

	metrics := repo.NewQualityMetricRepo(db)
	runner := quality.New(cfg.ProjectRoot)
	logger := log.New(f, "[DAEMON] ", log.LstdFlags)

	return &Daemon{
		cfg:           cfg,
		issues:        repo.NewIssueRepo(db),
		agents:        repo.NewAgentRepo(db),
		logger:        logger,
		stop:          make(chan struct{}),
		sendKeys:      session.SendKeys,
		sessionExists: session.Exists,
		resetWorktree: func(name string) error {
			return prole.Reset(name, cfg, repo.NewAgentRepo(db))
		},
		lastNudged:          make(map[string]time.Time),
		nudgeCooldown:       time.Duration(cfg.NudgeCooldownSeconds) * time.Second,
		stuckAgentThreshold: time.Duration(cfg.StuckAgentThresholdSeconds) * time.Second,
		nowFn:               time.Now,
		runQualityBaseline: func() error {
			return runAndPersistBaseline(runner, cfg.Quality.Checks, metrics, logger)
		},
		qualityInterval: time.Duration(cfg.Quality.BaselineIntervalSeconds) * time.Second,
	}, nil
}

// runAndPersistBaseline executes all quality checks and records each result.
// A Record error for one result is logged and skipped; all results are attempted.
func runAndPersistBaseline(runner *quality.Runner, checks []config.QualityCheckConfig, metrics *repo.QualityMetricRepo, logger *log.Logger) error {
	baseline := runner.Run(checks)
	for _, result := range baseline.Results {
		m := &repo.QualityMetric{
			CheckName: result.CheckName,
			Status:    string(result.Status),
			Output:    result.Output,
			RunAt:     result.RunAt,
			Error:     result.Err,
		}
		if result.Value != nil {
			m.Value = sql.NullFloat64{Float64: *result.Value, Valid: true}
		}
		if err := metrics.Record(m); err != nil {
			logger.Printf("warning: could not persist result for %q: %v", result.CheckName, err)
		}
	}
	return nil
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

// shouldNudge returns true if enough time has passed since the last nudge for key.
// Always returns true if nudgeCooldown is zero (cooldown disabled).
func (d *Daemon) shouldNudge(key string) bool {
	if d.nudgeCooldown == 0 {
		return true
	}
	last, ok := d.lastNudged[key]
	if !ok {
		return true
	}
	return d.nowFn().Sub(last) >= d.nudgeCooldown
}

// recordNudge records the current time as the last nudge for key.
func (d *Daemon) recordNudge(key string) {
	d.lastNudged[key] = d.nowFn()
}

func (d *Daemon) poll() {
	d.handleDeadSessions()
	d.handleDraftTickets()
	d.handleOpenTickets()
	d.handleInReviewTickets()
	d.handleRepairingTickets()
	d.handlePREvents()
	d.handleStuckAgents()
	d.handleQualityBaseline()
}

// handleStuckAgents detects working agents that have not changed status for longer than
// stuckAgentThreshold and escalates each one to the Mayor.
func (d *Daemon) handleStuckAgents() {
	if d.stuckAgentThreshold == 0 {
		return // disabled
	}

	mayorSession := session.SessionName("mayor")
	if !d.sessionExists(mayorSession) {
		return // Mayor not running, nowhere to escalate
	}

	agents, err := d.agents.ListByStatus("working")
	if err != nil {
		d.logger.Printf("error listing working agents: %v", err)
		return
	}

	now := d.nowFn()
	for _, agent := range agents {
		if !agent.StatusChangedAt.Valid {
			continue
		}
		elapsed := now.Sub(agent.StatusChangedAt.Time)
		if elapsed < d.stuckAgentThreshold {
			continue
		}

		nudgeKey := "stuck:" + agent.Name
		if !d.shouldNudge(nudgeKey) {
			continue
		}

		ticketInfo := "no assigned ticket"
		if agent.CurrentIssue.Valid {
			ticketInfo = fmt.Sprintf("%s-%d", d.cfg.TicketPrefix, agent.CurrentIssue.Int64)
		}

		d.logger.Printf("agent %s appears stuck: working for %s (ticket: %s)",
			agent.Name, elapsed.Round(time.Second), ticketInfo)

		msg := fmt.Sprintf("ESCALATION: Agent %s has been working for %s on %s with no status change. "+
			"Please investigate whether the agent is stuck.",
			agent.Name, elapsed.Round(time.Minute), ticketInfo)

		if err := d.sendKeys(mayorSession, msg); err != nil {
			d.logger.Printf("error escalating stuck agent %s to Mayor: %v", agent.Name, err)
		} else {
			d.recordNudge(nudgeKey)
		}
	}
}

// handleQualityBaseline runs quality checks and persists results when the interval has elapsed.
func (d *Daemon) handleQualityBaseline() {
	if d.qualityInterval == 0 {
		return // disabled
	}
	if !d.nowFn().After(d.lastQualityBaseline.Add(d.qualityInterval)) {
		return // not yet time
	}

	d.logger.Printf("running quality baseline")
	if err := d.runQualityBaseline(); err != nil {
		d.logger.Printf("quality baseline error: %v", err)
	} else {
		d.logger.Printf("quality baseline complete")
	}
	d.lastQualityBaseline = d.nowFn()
}

// handleDeadSessions marks agents as dead when their tmux session no longer exists.
func (d *Daemon) handleDeadSessions() {
	agents, err := d.agents.ListAll()
	if err != nil {
		d.logger.Printf("error listing agents: %v", err)
		return
	}

	for _, agent := range agents {
		if agent.Status == "dead" {
			continue
		}
		if !agent.TmuxSession.Valid || agent.TmuxSession.String == "" {
			continue
		}
		if d.sessionExists(agent.TmuxSession.String) {
			continue
		}
		d.logger.Printf("session %s for agent %s not found — marking dead",
			agent.TmuxSession.String, agent.Name)
		if err := d.agents.UpdateStatus(agent.Name, "dead"); err != nil {
			d.logger.Printf("error marking agent %s dead: %v", agent.Name, err)
		}
	}
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
	if !d.sessionExists(conductorSession) {
		return // Conductor not running, nothing to do
	}

	if !d.shouldNudge("open") {
		return
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

	if err := d.sendKeys(conductorSession, msg); err != nil {
		d.logger.Printf("error nudging conductor: %v", err)
	} else {
		d.logger.Printf("nudged conductor: %d ready ticket(s) unassigned (%s)",
			len(unassigned), strings.Join(ids, ", "))
		d.recordNudge("open")
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

	if !d.shouldNudge("draft") {
		return
	}

	ids := make([]string, len(drafts))
	for i, issue := range drafts {
		ids[i] = fmt.Sprintf("%s-%d (%s)", d.cfg.TicketPrefix, issue.ID, issue.Title)
	}

	msg := fmt.Sprintf("%d draft ticket(s) need spec: %s. "+
		"Run `gt ticket show <id>` on each and begin specification.",
		len(drafts), strings.Join(ids, "; "))

	if err := d.sendKeys(architectSession, msg); err != nil {
		d.logger.Printf("error nudging architect: %v", err)
	} else {
		d.logger.Printf("nudged architect: %d draft ticket(s)", len(drafts))
		d.recordNudge("draft")
	}
}

// handleInReviewTickets distributes in_review tickets across all active reviewer agents.
func (d *Daemon) handleInReviewTickets() {
	reviews, err := d.issues.List("in_review")
	if err != nil {
		d.logger.Printf("error listing in_review tickets: %v", err)
		return
	}

	if len(reviews) == 0 {
		return
	}

	// Collect only tickets that have a PR number.
	var withPR []*repo.Issue
	for _, issue := range reviews {
		if issue.PRNumber.Valid {
			withPR = append(withPR, issue)
		}
	}
	if len(withPR) == 0 {
		return
	}

	activeSessions := d.activeReviewerSessions()
	if len(activeSessions) == 0 {
		return
	}

	if !d.shouldNudge("in_review") {
		return
	}

	// Distribute tickets round-robin across active reviewers.
	perReviewer := make([][]string, len(activeSessions))
	for i, issue := range withPR {
		bucket := i % len(activeSessions)
		perReviewer[bucket] = append(perReviewer[bucket],
			fmt.Sprintf("%s-%d (PR #%d)", d.cfg.TicketPrefix, issue.ID, issue.PRNumber.Int64))
	}

	nudged := 0
	for i, reviewerSession := range activeSessions {
		if len(perReviewer[i]) == 0 {
			continue
		}
		msg := fmt.Sprintf("%d ticket(s) ready for review: %s. "+
			"Review each PR and file comments.",
			len(perReviewer[i]), strings.Join(perReviewer[i], ", "))
		if err := d.sendKeys(reviewerSession, msg); err != nil {
			d.logger.Printf("error nudging reviewer %s: %v", reviewerSession, err)
		} else {
			nudged++
		}
	}
	if nudged > 0 {
		d.logger.Printf("nudged %d reviewer(s): %d in_review ticket(s)", nudged, len(withPR))
		d.recordNudge("in_review")
	}
}

// activeReviewerSessions returns session names for all non-dead reviewer agents
// whose tmux sessions are currently active, ordered by agent name.
func (d *Daemon) activeReviewerSessions() []string {
	allAgents, err := d.agents.ListAll()
	if err != nil {
		d.logger.Printf("error listing agents for reviewer sessions: %v", err)
		return nil
	}
	var sessions []string
	for _, a := range allAgents {
		if a.Type != "reviewer" || a.Status == "dead" {
			continue
		}
		s := session.SessionName(a.Name)
		if d.sessionExists(s) {
			sessions = append(sessions, s)
		}
	}
	return sessions
}

// reviewerAgentNames returns a set of all registered reviewer agent names.
func (d *Daemon) reviewerAgentNames() map[string]bool {
	allAgents, err := d.agents.ListAll()
	if err != nil {
		d.logger.Printf("error listing reviewer agent names: %v", err)
		return nil
	}
	names := make(map[string]bool)
	for _, a := range allAgents {
		if a.Type == "reviewer" {
			names[a.Name] = true
		}
	}
	return names
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

	if !d.shouldNudge("repairing") {
		return
	}

	parts := make([]string, len(tickets))
	for i, issue := range tickets {
		entry := fmt.Sprintf("%s-%d (%s)", d.cfg.TicketPrefix, issue.ID, issue.Title)
		if issue.PRNumber.Valid {
			entry += fmt.Sprintf(" PR #%d", issue.PRNumber.Int64)
		}
		parts[i] = entry
	}

	msg := fmt.Sprintf("%d ticket(s) need repair: %s. "+
		"Assign an available prole to address the review comments.",
		len(tickets), strings.Join(parts, "; "))

	if err := d.sendKeys(conductorSession, msg); err != nil {
		d.logger.Printf("error nudging Conductor for repairing tickets: %v", err)
	} else {
		d.logger.Printf("nudged Conductor: %d repairing ticket(s)", len(tickets))
		d.recordNudge("repairing")
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
		assignee := issue.Assignee.String
		if err := d.agents.ClearCurrentIssue(assignee); err != nil {
			d.logger.Printf("error clearing current issue for agent %s: %v", assignee, err)
		} else {
			d.logger.Printf("freed agent %s after PR #%d merged", assignee, issue.PRNumber.Int64)

			// Reset prole worktree so it is clean for the next ticket
			agent, err := d.agents.Get(assignee)
			if err == nil && agent.Type == "prole" {
				if err := d.resetWorktree(assignee); err != nil {
					d.logger.Printf("error resetting worktree for prole %s: %v", assignee, err)
				} else {
					d.logger.Printf("reset worktree for prole %s after PR #%d merged",
						assignee, issue.PRNumber.Int64)
				}
			}
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

	// Look for human comments (not from bots or any registered reviewer agent).
	reviewerNames := d.reviewerAgentNames()
	for _, c := range comments {
		if c.IsBot || reviewerNames[c.Author] {
			continue
		}

		d.logger.Printf("human comment on PR #%d by %s — moving ticket %s-%d to repairing",
			prNum, c.Author, d.cfg.TicketPrefix, issue.ID)

		if err := d.issues.UpdateStatus(issue.ID, "repairing"); err != nil {
			d.logger.Printf("error updating ticket %d to repairing: %v", issue.ID, err)
			return
		}

		// Conductor handles repairing tickets via handleRepairingTickets
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
