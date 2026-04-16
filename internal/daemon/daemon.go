package daemon

import (
	"bytes"
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
	"github.com/katerina7479/company_town/internal/eventlog"
	"github.com/katerina7479/company_town/internal/prole"
	"github.com/katerina7479/company_town/internal/quality"
	"github.com/katerina7479/company_town/internal/repo"
	"github.com/katerina7479/company_town/internal/session"
)

// tickObservations holds per-handler counters accumulated during a single poll
// tick. Handlers write to it via nil-guarded d.obs assignments so the struct
// can be omitted in tests that don't care about the summary line.
type tickObservations struct {
	dead                     int  // proles deleted (handleDeadSessions)
	worktreesSkip            bool // stale-worktree handler was interval-guarded
	worktreesPruned          int  // stale worktrees pruned (0 if skipped)
	prBackfillFound          int  // tickets missing PR number
	prBackfillDone           int  // PR numbers successfully backfilled
	drafts                   int  // draft tickets found
	assignCandidates         int  // selectable ticket count
	assignSlots              int  // available prole slot count
	assignPaired             int  // tickets actually assigned
	inReview                 int  // in_review tickets with a PR number
	prEventsTotal            int  // tickets with PRs checked
	prEventsMerged           int  // PRs merged this tick
	prEventsRepairing        int  // open PRs moved to repairing (human comment)
	prEventsClosed           int  // PRs closed without merge
	prEventsConflict         int  // pr_open PRs moved to merge_conflict
	prEventsConflictResolved int  // merge_conflict PRs moved back to pr_open
	prEventsCIPass           int  // ci_running PRs promoted to in_review (all checks green)
	prEventsCIFail           int  // ci_running PRs moved to repairing (CI failure)
	prEventsTDDApproved      int  // tdd_tests PRs closed on human APPROVED review
	epics                    int  // completable epics found
	qualitySkip              bool // quality baseline was interval-guarded
	repairCycleEscalations   int  // tickets moved to on_hold for exceeding repair cycle threshold
}

// Daemon polls for state changes and routes work to agents.
type Daemon struct {
	cfg                 *config.Config
	issues              *repo.IssueRepo
	agents              *repo.AgentRepo
	logger              *log.Logger
	stop                chan struct{}
	session             session.Client
	capturePane         func(s string) (string, error)
	lastNudged          map[string]time.Time
	lastNudgeDigest     map[string]string // hash of ticket IDs from last nudge per key
	nudgeCooldown       time.Duration
	stuckAgentThreshold time.Duration
	nowFn               func() time.Time

	// obs accumulates per-handler observations for the tick summary line.
	// Set at the start of poll() and cleared afterwards; nil outside a poll.
	obs *tickObservations

	// Quality baseline
	runQualityBaseline  func() error
	lastQualityBaseline time.Time
	qualityInterval     time.Duration

	// Stale worktree pruning
	pruneStaleWorktrees func() (int, error)
	lastWorktreePrune   time.Time
	worktreeInterval    time.Duration

	// Idle prole worktree reset — reconciler that brings idle proles' worktrees
	// back to their standby branch at origin/main. Independent of PR merge
	// events (see NC-53).
	resetIdleProleWorktrees func() error
	lastWorktreeReset       time.Time
	worktreeResetInterval   time.Duration

	// PR number backfill
	lookupPRForBranch  func(branch string) (int, bool, error)
	lastPRBackfill     time.Time
	prBackfillInterval time.Duration

	// Agent restart
	restartAgent      func(agent *repo.Agent) error
	lastRestartedAt   map[string]time.Time
	restartCooldown   time.Duration
	restartDeadAgents bool

	// Repair-cycle escalation
	repairCycleThreshold int

	// Reviewer follow-up reminder
	followUpInterval     time.Duration
	followUpNReviews     int
	reviewsSinceFollowUp int
	lastFollowUpNudge    time.Time

	// Review comment fetching (injectable for tests)
	getReviewCommentsFn func(prNum int) ([]prComment, error)

	// PR state fetching (injectable for tests).
	// Returns: state (OPEN/MERGED/CLOSED), mergeable (MERGEABLE/CONFLICTING/UNKNOWN),
	// checks (passing/failing/pending), failing check names, whether PR is merged.
	getPRStateFn func(prNum int) (state, mergeable, checks string, failing []string, merged bool, err error)

	// tickFile is the path to the file where the last poll timestamp is written.
	// Empty string disables the write (e.g., in tests).
	tickFile string
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
	events := eventlog.NewLogger(ctDir)
	agentRepo := repo.NewAgentRepo(db, events)
	sessClient := session.New()

	return &Daemon{
		cfg:                 cfg,
		issues:              repo.NewIssueRepo(db, events),
		agents:              agentRepo,
		logger:              logger,
		stop:                make(chan struct{}),
		session:             sessClient,
		capturePane:         sessClient.CapturePane,
		lastNudged:          make(map[string]time.Time),
		lastNudgeDigest:     make(map[string]string),
		nudgeCooldown:       time.Duration(cfg.NudgeCooldownSeconds) * time.Second,
		stuckAgentThreshold: time.Duration(cfg.StuckAgentThresholdSeconds) * time.Second,
		nowFn:               time.Now,
		runQualityBaseline: func() error {
			return runAndPersistBaseline(runner, cfg.Quality.Checks, metrics, logger)
		},
		qualityInterval: time.Duration(cfg.Quality.BaselineIntervalSeconds) * time.Second,
		pruneStaleWorktrees: func() (int, error) {
			pruned, err := prole.PruneDeadWorktrees(cfg, repo.NewAgentRepo(db, events), logger)
			for _, name := range pruned {
				logger.Printf("pruned stale worktree for dead prole %s", name)
			}
			return len(pruned), err
		},
		worktreeInterval: time.Duration(cfg.WorktreePruneIntervalSeconds) * time.Second,
		resetIdleProleWorktrees: func() error {
			return prole.ResetIdleWorktrees(cfg, repo.NewAgentRepo(db, events), logger)
		},
		worktreeResetInterval: time.Duration(cfg.WorktreeResetIntervalSeconds) * time.Second,
		lookupPRForBranch: func(branch string) (int, bool, error) {
			return lookupPRForBranch(branch, cfg.ProjectRoot)
		},
		prBackfillInterval:   time.Duration(cfg.PRBackfillIntervalSeconds) * time.Second,
		restartDeadAgents:    cfg.RestartDeadAgents,
		restartCooldown:      time.Duration(cfg.RestartCooldownSeconds) * time.Second,
		lastRestartedAt:      make(map[string]time.Time),
		restartAgent:         makeRestartFn(cfg, agentRepo, logger),
		repairCycleThreshold: cfg.RepairCycleThreshold,
		followUpInterval:     time.Duration(cfg.ReviewerFollowUpIntervalSeconds) * time.Second,
		followUpNReviews:     cfg.ReviewerFollowUpNReviews,
		tickFile:             filepath.Join(ctDir, "run", "daemon-tick"),
	}, nil
}

// reviewComments calls getReviewCommentsFn if set, otherwise the real implementation.
func (d *Daemon) reviewComments(prNum int) ([]prComment, error) {
	if d.getReviewCommentsFn != nil {
		return d.getReviewCommentsFn(prNum)
	}
	return d.getReviewComments(prNum)
}

// agentStartPrompt returns the startup prompt for an agent type.
func agentStartPrompt(agentType, ticketPrefix string) string {
	switch agentType {
	case "architect":
		return fmt.Sprintf(
			"You are the Architect. Ticket prefix: %s. "+
				"Read your CLAUDE.md for instructions. "+
				"Check memory/handoff.md to resume previous work. "+
				"Begin your patrol loop: check for draft tickets and spec them out.",
			ticketPrefix,
		)
	case "reviewer":
		return fmt.Sprintf(
			"You are the Reviewer. Ticket prefix: %s. "+
				"Read your CLAUDE.md for instructions. "+
				"Check memory/handoff.md to resume previous work. "+
				"Begin patrol: check for in_review tickets and review their PRs.",
			ticketPrefix,
		)
	default:
		return ""
	}
}

// agentModel returns the model string for an agent type.
func agentModel(agentType string, cfg *config.Config) string {
	switch agentType {
	case "architect":
		return cfg.Agents.Architect.Model
	case "reviewer":
		return cfg.Agents.Reviewer.Model
	default:
		return ""
	}
}

// makeRestartFn creates the production restartAgent implementation.
func makeRestartFn(cfg *config.Config, agents *repo.AgentRepo, logger *log.Logger) func(*repo.Agent) error {
	return func(agent *repo.Agent) error {
		if agent.Type != "reviewer" && agent.Type != "architect" {
			return fmt.Errorf("restartAgent: unsupported agent type %q", agent.Type)
		}

		ctDir := config.CompanyTownDir(cfg.ProjectRoot)
		agentDir := filepath.Join(ctDir, "agents", agent.Type)
		model := agentModel(agent.Type, cfg)
		prompt := agentStartPrompt(agent.Type, cfg.TicketPrefix)
		sessionName := session.SessionName(agent.Name)

		if err := agents.UpdateStatus(agent.Name, repo.StatusIdle); err != nil {
			return fmt.Errorf("updating agent status: %w", err)
		}
		if err := agents.SetTmuxSession(agent.Name, sessionName); err != nil {
			return fmt.Errorf("recording tmux session: %w", err)
		}
		if err := session.CreateInteractive(session.AgentSessionConfig{
			Name:     sessionName,
			WorkDir:  cfg.ProjectRoot,
			Model:    model,
			AgentDir: agentDir,
			Prompt:   prompt,
			EnvVars:  map[string]string{"CT_AGENT_NAME": agent.Name},
		}); err != nil {
			agents.UpdateStatus(agent.Name, repo.StatusDead) //nolint:errcheck
			return fmt.Errorf("creating session for %s: %w", agent.Name, err)
		}

		logger.Printf("restarted agent %s (type: %s, session: %s)", agent.Name, agent.Type, sessionName)
		return nil
	}
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
			// Drop the heartbeat on clean shutdown so the dashboard flips to
			// "not running" immediately instead of showing a stale timestamp
			// aging toward the stale threshold.
			if d.tickFile != "" {
				_ = os.Remove(d.tickFile)
			}
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

// isAgentWorking returns true if the named agent has status "working".
// Used to suppress nudges to agents that are already actively processing.
func (d *Daemon) isAgentWorking(name string) bool {
	agent, err := d.agents.Get(name)
	if err != nil {
		return false // agent not found — allow nudge
	}
	return agent.Status == repo.StatusWorking
}

// recordNudge records the current time and ticket digest for key.
func (d *Daemon) recordNudge(key, digest string) {
	d.lastNudged[key] = d.nowFn()
	d.lastNudgeDigest[key] = digest
}

// shouldRestart returns true if enough time has passed since the last restart for agentName.
// Always returns true if restartCooldown is zero (cooldown disabled).
func (d *Daemon) shouldRestart(agentName string) bool {
	if d.restartCooldown == 0 {
		return true
	}
	last, ok := d.lastRestartedAt[agentName]
	if !ok {
		return true
	}
	return d.nowFn().Sub(last) >= d.restartCooldown
}

// recordRestart records the current time as the last restart for agentName.
func (d *Daemon) recordRestart(agentName string) {
	d.lastRestartedAt[agentName] = d.nowFn()
}

// digestChanged returns true if the given digest differs from the last recorded one for key.
func (d *Daemon) digestChanged(key, digest string) bool {
	prev, ok := d.lastNudgeDigest[key]
	if !ok {
		return true
	}
	return prev != digest
}

// ticketDigest builds a stable string from a sorted list of ticket IDs.
func ticketDigest(ids []int) string {
	sorted := make([]int, len(ids))
	copy(sorted, ids)
	// Simple insertion sort — nudge lists are small
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j-1] > sorted[j]; j-- {
			sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
		}
	}
	parts := make([]string, len(sorted))
	for i, id := range sorted {
		parts[i] = strconv.Itoa(id)
	}
	return strings.Join(parts, ",")
}

func (d *Daemon) poll() {
	d.obs = &tickObservations{}

	d.handleDeadSessions()
	d.handleStaleWorktrees()
	d.handleIdleProleWorktrees()
	d.handleBackfillPRNumbers()
	d.handleDraftTickets()
	d.handleAssignments()
	d.handleIdleAssignedProles()
	d.handleWorkingOpenTickets()
	d.handleInReviewTickets()
	d.handleFollowUpReminder()
	d.handlePREvents()
	d.handleRepairCycleEscalation()
	d.handleStuckPrompts()
	d.handleEpicAutoClose()
	d.handleQualityBaseline()

	logTickSummary(d.logger, *d.obs)
	d.obs = nil
	d.writeHeartbeat()
}

// stderrSnippetLen is the maximum bytes of captured stderr appended to a
// subprocess error. Long stderr (e.g. an HTML error page) is truncated so a
// single bad call cannot flood daemon.log.
const stderrSnippetLen = 200

// runCmd runs cmd and returns its stdout. If the command fails, up to
// stderrSnippetLen bytes of stderr are appended to the error so the log entry
// shows the actual failure reason (auth error, rate limit, network blip, etc.)
// rather than just an exit code.
func runCmd(cmd *exec.Cmd) ([]byte, error) {
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		snippet := strings.TrimSpace(stderr.String())
		if len(snippet) > stderrSnippetLen {
			snippet = snippet[:stderrSnippetLen] + "..."
		}
		if snippet != "" {
			return nil, fmt.Errorf("%w: %s", err, snippet)
		}
		return nil, err
	}
	return out, nil
}

// logTickSummary emits a single tick: summary line in fixed key=value format.
// Field order and format are a stability contract — operators grep/awk these
// tokens to monitor daemon health over time. Do not reorder or rename tokens
// without updating all downstream tooling.
//
// Format:
//
//	tick: dead=N worktrees=skip|N prBackfill=N/N drafts=N assign=N/N/N inReview=N prEvents=N/N/N/N/N/N/N/N/N repairEsc=N epics=N quality=skip|ran
//
// Fields:
//
//	dead         — proles deleted (dead sessions)
//	worktrees    — stale worktrees pruned, or "skip" when interval-guarded
//	prBackfill   — tickets missing PR / PR numbers successfully backfilled
//	drafts       — draft tickets found this tick
//	assign       — selectable tickets / available slots / actually assigned
//	inReview     — in_review tickets with a PR number
//	prEvents     — tickets with PRs / merged / moved-to-repairing / closed-without-merge / conflict / conflict-resolved / ci-pass / ci-fail / tdd-approved
//	repairEsc    — tickets moved to on_hold for exceeding repair-cycle threshold
//	epics        — completable epics found
//	quality      — "ran" when baseline executed, "skip" when interval-guarded
func logTickSummary(logger *log.Logger, obs tickObservations) {
	worktrees := fmt.Sprintf("%d", obs.worktreesPruned)
	if obs.worktreesSkip {
		worktrees = "skip"
	}
	quality := "ran"
	if obs.qualitySkip {
		quality = "skip"
	}
	logger.Printf("tick: dead=%d worktrees=%s prBackfill=%d/%d drafts=%d assign=%d/%d/%d inReview=%d prEvents=%d/%d/%d/%d/%d/%d/%d/%d/%d repairEsc=%d epics=%d quality=%s",
		obs.dead,
		worktrees,
		obs.prBackfillFound, obs.prBackfillDone,
		obs.drafts,
		obs.assignCandidates, obs.assignSlots, obs.assignPaired,
		obs.inReview,
		obs.prEventsTotal, obs.prEventsMerged, obs.prEventsRepairing, obs.prEventsClosed,
		obs.prEventsConflict, obs.prEventsConflictResolved, obs.prEventsCIPass, obs.prEventsCIFail,
		obs.prEventsTDDApproved,
		obs.repairCycleEscalations,
		obs.epics,
		quality,
	)
}

// writeHeartbeat stamps the current UTC time into d.tickFile so the dashboard
// can render daemon liveness. Empty tickFile disables the write (for tests).
// Errors are logged and swallowed — a failed heartbeat must not abort poll().
func (d *Daemon) writeHeartbeat() {
	if d.tickFile == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(d.tickFile), 0755); err != nil {
		d.logger.Printf("heartbeat: mkdir: %v", err)
		return
	}
	content := d.nowFn().UTC().Format(time.RFC3339Nano) + "\n"
	if err := os.WriteFile(d.tickFile, []byte(content), 0644); err != nil {
		d.logger.Printf("heartbeat: write: %v", err)
	}
}

// handleStaleWorktrees prunes git worktrees belonging to dead prole agents when they
// are git-clean (no uncommitted changes, no unpushed commits). Guarded by
// worktreeInterval so it does not spawn git subprocesses on every poll tick.
func (d *Daemon) handleStaleWorktrees() {
	if d.worktreeInterval > 0 && !d.nowFn().After(d.lastWorktreePrune.Add(d.worktreeInterval)) {
		if d.obs != nil {
			d.obs.worktreesSkip = true
		}
		return
	}
	n, err := d.pruneStaleWorktrees()
	if err != nil {
		d.logger.Printf("error pruning stale worktrees: %v", err)
	}
	if d.obs != nil {
		d.obs.worktreesPruned = n
	}
	d.lastWorktreePrune = d.nowFn()
}

// handleIdleProleWorktrees runs the idle-prole worktree reset reconciler.
// Brings any idle prole whose worktree has drifted off its standby branch
// back to a clean checkout of origin/main. Guarded by worktreeResetInterval
// so it does not spawn git subprocesses on every poll tick. See NC-53.
func (d *Daemon) handleIdleProleWorktrees() {
	if d.resetIdleProleWorktrees == nil {
		return
	}
	if d.worktreeResetInterval > 0 && !d.nowFn().After(d.lastWorktreeReset.Add(d.worktreeResetInterval)) {
		return
	}
	if err := d.resetIdleProleWorktrees(); err != nil {
		d.logger.Printf("error resetting idle prole worktrees: %v", err)
	}
	d.lastWorktreeReset = d.nowFn()
}

// handleIdleAssignedProles nudges idle proles that still have an assigned ticket
// in open, in_progress, or repairing status. This covers the reconciler blind spot
// where a prole returned to idle (e.g. after filing a PR or mid-session crash) but
// its ticket still lists it as assignee — handleAssignments skips the ticket because
// it already has an assignee, and handleStuckAgents skips the prole because it is
// not in working status.
//
// Iterates from the tickets table (canonical source of truth), not the agents table.
// This also catches the drift case where agents.current_issue was cleared but
// tickets.assignee was not, which agent-first iteration would silently miss.
//
// Nudge key: "prole-resume:<name>:<ticket-id>" — per (prole, ticket) so a new
// assignment re-primes immediately while a stuck prole is not spammed.
func (d *Daemon) handleIdleAssignedProles() {
	candidates, err := d.issues.ListAssignedInStatuses(repo.StatusOpen, repo.StatusInProgress, repo.StatusRepairing)
	if err != nil {
		d.logger.Printf("handleIdleAssignedProles: listing candidates: %v", err)
		return
	}

	for _, issue := range candidates {
		if !issue.Assignee.Valid || issue.Assignee.String == "" {
			continue
		}
		agentName := issue.Assignee.String

		agent, err := d.agents.Get(agentName)
		if err != nil {
			// Agent not registered yet — not our concern.
			continue
		}
		if agent.Type != "prole" {
			continue
		}
		if agent.Status != repo.StatusIdle {
			// Already working — no nudge needed.
			continue
		}

		if !agent.TmuxSession.Valid || agent.TmuxSession.String == "" {
			continue
		}
		if !d.session.Exists(agent.TmuxSession.String) {
			// handleDeadSessions owns the cleanup path for dead sessions.
			continue
		}

		nudgeKey := fmt.Sprintf("prole-resume:%s:%d", agentName, issue.ID)
		if !d.shouldNudge(nudgeKey) {
			continue
		}

		msg := fmt.Sprintf(
			"You are idle but ticket %s-%d is still assigned to you in [%s]. "+
				"Please run 'gt ticket show %d', begin work on it per your startup protocol, and push when ready.",
			d.cfg.TicketPrefix, issue.ID, issue.Status, issue.ID,
		)
		if err := d.session.SendKeys(agent.TmuxSession.String, msg); err != nil {
			d.logger.Printf("error nudging idle prole %s: %v", agentName, err)
			continue
		}
		d.logger.Printf("nudged idle prole %s to resume %s-%d", agentName, d.cfg.TicketPrefix, issue.ID)
		d.recordNudge(nudgeKey, "")
	}
}

// handleWorkingOpenTickets scans agents in "working" status whose current_issue
// ticket is still in "open" status, and flips the ticket to "in_progress".
// This reconciles the drift case where a prole called `gt agent accept` but
// skipped (or failed) the explicit `gt ticket status <id> in_progress` step —
// leaving the ticket stuck in "open" while the agent is actively working.
func (d *Daemon) handleWorkingOpenTickets() {
	working, err := d.agents.ListByStatus(repo.StatusWorking)
	if err != nil {
		d.logger.Printf("handleWorkingOpenTickets: listing working agents: %v", err)
		return
	}
	for _, agent := range working {
		if !agent.CurrentIssue.Valid {
			continue
		}
		id := int(agent.CurrentIssue.Int64)
		issue, err := d.issues.Get(id)
		if err != nil {
			d.logger.Printf("handleWorkingOpenTickets: get ticket %d: %v", id, err)
			continue
		}
		if issue.Status != "open" {
			continue
		}
		if err := d.issues.UpdateStatus(id, "in_progress"); err != nil {
			d.logger.Printf("handleWorkingOpenTickets: flip ticket %d to in_progress: %v", id, err)
			continue
		}
		d.logger.Printf("handleWorkingOpenTickets: ticket %d flipped to in_progress (agent %s working on open ticket)", id, agent.Name)
	}
}

// handleBackfillPRNumbers finds tickets with a branch but no pr_number and attempts
// to look up a matching open PR on GitHub. Guarded by prBackfillInterval.
func (d *Daemon) handleBackfillPRNumbers() {
	if d.prBackfillInterval > 0 && !d.nowFn().After(d.lastPRBackfill.Add(d.prBackfillInterval)) {
		return
	}

	tickets, err := d.issues.ListMissingPR()
	if err != nil {
		d.logger.Printf("error listing tickets missing PR: %v", err)
		d.lastPRBackfill = d.nowFn()
		return
	}

	if d.obs != nil {
		d.obs.prBackfillFound = len(tickets)
	}

	for _, issue := range tickets {
		if !issue.Branch.Valid || issue.Branch.String == "" {
			continue
		}
		prNum, found, err := d.lookupPRForBranch(issue.Branch.String)
		if err != nil {
			d.logger.Printf("error looking up PR for branch %s: %v", issue.Branch.String, err)
			continue
		}
		if !found {
			continue
		}
		if err := d.issues.SetPR(issue.ID, prNum); err != nil {
			d.logger.Printf("error backfilling PR for ticket %s-%d: %v",
				d.cfg.TicketPrefix, issue.ID, err)
			continue
		}
		d.logger.Printf("backfilled PR #%d for ticket %s-%d (branch %s)",
			prNum, d.cfg.TicketPrefix, issue.ID, issue.Branch.String)
		if d.obs != nil {
			d.obs.prBackfillDone++
		}
	}

	d.lastPRBackfill = d.nowFn()
}

// prListEntry is one element from `gh pr list --json number,state,updatedAt`.
type prListEntry struct {
	Number    int       `json:"number"`
	State     string    `json:"state"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// pickMostRecentPR selects the most authoritative PR from a list returned by
// gh pr list. It sorts by UpdatedAt descending; ties are broken by state
// precedence: MERGED > OPEN > CLOSED. Returns 0 for an empty list.
func pickMostRecentPR(entries []prListEntry) int {
	if len(entries) == 0 {
		return 0
	}
	statePrecedence := func(s string) int {
		switch s {
		case "MERGED":
			return 0
		case "OPEN":
			return 1
		default: // CLOSED
			return 2
		}
	}
	best := entries[0]
	for _, e := range entries[1:] {
		if e.UpdatedAt.After(best.UpdatedAt) {
			best = e
		} else if e.UpdatedAt.Equal(best.UpdatedAt) && statePrecedence(e.State) < statePrecedence(best.State) {
			best = e
		}
	}
	return best.Number
}

// lookupPRForBranch queries GitHub for any PR (open or merged) matching the
// given head branch. Returns (prNumber, found, error). found is false when no
// matching PR exists. --state all is required so merged PRs are included —
// without it, gh pr list only returns open PRs and the backfill misses PRs
// that were merged before the ticket's pr_number column was populated.
// --limit 5 and pickMostRecentPR guard against rare branch-name collisions
// (e.g. a prior closed PR on the same branch) by always picking the most
// recently updated, with MERGED > OPEN > CLOSED as a tie-breaker.
func lookupPRForBranch(branch, projectRoot string) (int, bool, error) {
	cmd := exec.Command("gh", "pr", "list",
		"--head", branch,
		"--state", "all",
		"--json", "number,state,updatedAt",
		"--limit", "5",
	)
	cmd.Dir = projectRoot
	out, err := runCmd(cmd)
	if err != nil {
		return 0, false, fmt.Errorf("gh pr list: %w", err)
	}

	var results []prListEntry
	if err := json.Unmarshal(out, &results); err != nil {
		return 0, false, fmt.Errorf("parsing PR list: %w", err)
	}
	if len(results) == 0 {
		return 0, false, nil
	}
	return pickMostRecentPR(results), true, nil
}

// handleEpicAutoClose closes epics whose sub-tasks are all closed.
func (d *Daemon) handleEpicAutoClose() {
	epics, err := d.issues.ListEpicsWithAllChildrenClosed()
	if err != nil {
		d.logger.Printf("error listing completable epics: %v", err)
		return
	}

	if d.obs != nil {
		d.obs.epics = len(epics)
	}
	for _, epic := range epics {
		d.logger.Printf("auto-closing epic %s-%d (%s): all sub-tasks closed",
			d.cfg.TicketPrefix, epic.ID, epic.Title)

		if err := d.issues.UpdateStatus(epic.ID, repo.StatusClosed); err != nil {
			d.logger.Printf("error closing epic %d: %v", epic.ID, err)
			continue
		}

		mayorSession := session.SessionName("mayor")
		if d.session.Exists(mayorSession) {
			msg := fmt.Sprintf("Epic %s-%d (%s) auto-closed: all sub-tasks are complete.",
				d.cfg.TicketPrefix, epic.ID, epic.Title)
			if err := d.session.SendKeys(mayorSession, msg); err != nil {
				d.logger.Printf("error notifying Mayor of epic %d closure: %v", epic.ID, err)
			}
		}
	}
}

// handleStuckAgents detects working agents that have not changed status for longer than
// stuckAgentThreshold and escalates each one to the Mayor.
func (d *Daemon) handleStuckAgents() {
	if d.stuckAgentThreshold == 0 {
		return // disabled
	}

	mayorSession := session.SessionName("mayor")
	if !d.session.Exists(mayorSession) {
		return // Mayor not running, nowhere to escalate
	}

	agents, err := d.agents.ListByStatus(repo.StatusWorking)
	if err != nil {
		d.logger.Printf("error listing working agents: %v", err)
		return
	}

	now := d.nowFn()
	for _, agent := range agents {
		if agent.Name == "mayor" {
			continue // do not escalate the Mayor to itself
		}
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

		if err := d.session.SendKeys(mayorSession, msg); err != nil {
			d.logger.Printf("error escalating stuck agent %s to Mayor: %v", agent.Name, err)
		} else {
			d.recordNudge(nudgeKey, "")
		}
	}
}

// stuckPromptPatterns are substrings that indicate a prole's pane is showing
// a blocking prompt requiring human input. Matched case-insensitively against
// the last 20 lines of the captured pane so historical prompt text that has
// already scrolled past does not produce false positives.
var stuckPromptPatterns = []string{
	"do you want to",
	"allow ",
	"(y/n)",
	"[y/n]",
	"press enter to continue",
	"are you sure",
	"confirm?",
}

// detectBlockingPrompt checks the last 20 lines of a captured tmux pane for
// known blocking-prompt patterns. Returns the matched line (trimmed) or "" if
// no prompt is detected.
func detectBlockingPrompt(paneContent string) string {
	lines := strings.Split(paneContent, "\n")
	start := len(lines) - 20
	if start < 0 {
		start = 0
	}
	for _, line := range lines[start:] {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		for _, pattern := range stuckPromptPatterns {
			if strings.Contains(lower, pattern) {
				return trimmed
			}
		}
	}
	return ""
}

// handleStuckPrompts captures each working prole's tmux pane and checks for
// known blocking-prompt patterns. When a prompt is detected it escalates to
// the Mayor (rate-limited by the nudge cooldown) so a human can intervene.
// This is complementary to handleRepairCycleEscalation: that handler catches
// generic no-progress situations; this catches the specific frozen-on-a-prompt
// case, typically much sooner.
func (d *Daemon) handleStuckPrompts() {
	if d.capturePane == nil {
		return // not wired (e.g. old test setup)
	}

	mayorSession := session.SessionName("mayor")
	if !d.session.Exists(mayorSession) {
		return // Mayor not running, nowhere to escalate
	}

	agents, err := d.agents.ListByStatus(repo.StatusWorking)
	if err != nil {
		d.logger.Printf("error listing working agents for prompt check: %v", err)
		return
	}

	for _, agent := range agents {
		if agent.Type != "prole" {
			continue // only proles are pane-captured
		}
		if !agent.TmuxSession.Valid || agent.TmuxSession.String == "" {
			continue
		}
		sess := agent.TmuxSession.String
		if !d.session.Exists(sess) {
			continue
		}

		content, err := d.capturePane(sess)
		if err != nil {
			d.logger.Printf("error capturing pane for %s: %v", agent.Name, err)
			continue
		}

		prompt := detectBlockingPrompt(content)
		if prompt == "" {
			continue
		}

		nudgeKey := "prompt:" + agent.Name
		if !d.shouldNudge(nudgeKey) {
			continue
		}

		ticketInfo := "no assigned ticket"
		if agent.CurrentIssue.Valid {
			ticketInfo = fmt.Sprintf("%s-%d", d.cfg.TicketPrefix, agent.CurrentIssue.Int64)
		}

		d.logger.Printf("agent %s appears stuck on a prompt (ticket: %s): %s",
			agent.Name, ticketInfo, prompt)

		msg := fmt.Sprintf("STUCK PROMPT: Agent %s is blocked on an input prompt while working on %s. "+
			"Prompt detected: %q. "+
			"Please check the pane (ct attach %s) and respond to the prompt or kill the session.",
			agent.Name, ticketInfo, prompt, agent.Name)

		if err := d.session.SendKeys(mayorSession, msg); err != nil {
			d.logger.Printf("error escalating stuck prompt for %s: %v", agent.Name, err)
		} else {
			d.recordNudge(nudgeKey, "")
		}
	}
}

// handleRepairCycleEscalation detects tickets that have bounced between
// in_review and repairing more than repairCycleThreshold times. When the
// threshold is exceeded the ticket is moved to on_hold so a human can
// intervene instead of the system spinning indefinitely. The Mayor is
// notified with a brief summary.
func (d *Daemon) handleRepairCycleEscalation() {
	if d.repairCycleThreshold <= 0 {
		return // disabled
	}

	mayorSession := session.SessionName("mayor")
	if !d.session.Exists(mayorSession) {
		return // Mayor not running, nowhere to escalate
	}

	repairing, err := d.issues.List(repo.StatusRepairing)
	if err != nil {
		d.logger.Printf("error listing repairing tickets: %v", err)
		return
	}

	for _, issue := range repairing {
		if issue.RepairCycleCount < d.repairCycleThreshold {
			continue
		}

		nudgeKey := fmt.Sprintf("repair_cycle:%d", issue.ID)
		if !d.shouldNudge(nudgeKey) {
			continue
		}
		nudgeDigest := fmt.Sprintf("repair_cycle_count=%d", issue.RepairCycleCount)
		if !d.digestChanged(nudgeKey, nudgeDigest) {
			continue
		}

		d.logger.Printf("ticket %s-%d has bounced %d times (threshold %d) — moving to on_hold",
			d.cfg.TicketPrefix, issue.ID, issue.RepairCycleCount, d.repairCycleThreshold)

		if err := d.issues.UpdateStatus(issue.ID, repo.StatusOnHold); err != nil {
			d.logger.Printf("error moving ticket %d to on_hold: %v", issue.ID, err)
			continue
		}

		reason := fmt.Sprintf("escalated: bounced %d times (threshold %d) — human review required",
			issue.RepairCycleCount, d.repairCycleThreshold)
		if err := d.issues.SetRepairReason(issue.ID, reason); err != nil {
			d.logger.Printf("error setting repair_reason for ticket %d: %v", issue.ID, err)
		}

		msg := fmt.Sprintf(
			"ESCALATION: %s-%d (%s) has been sent back for repairs %d times and is now on_hold. "+
				"Please review the PR and decide whether to close, reassign, or unblock it manually.",
			d.cfg.TicketPrefix, issue.ID, issue.Title, issue.RepairCycleCount,
		)
		if err := d.session.SendKeys(mayorSession, msg); err != nil {
			d.logger.Printf("error notifying Mayor of repair-cycle escalation for ticket %d: %v", issue.ID, err)
		} else {
			d.recordNudge(nudgeKey, nudgeDigest)
		}

		if d.obs != nil {
			d.obs.repairCycleEscalations++
		}
	}
}

// handleQualityBaseline runs quality checks and persists results when the interval has elapsed.
func (d *Daemon) handleQualityBaseline() {
	if d.qualityInterval == 0 {
		if d.obs != nil {
			d.obs.qualitySkip = true
		}
		return // disabled
	}
	if !d.nowFn().After(d.lastQualityBaseline.Add(d.qualityInterval)) {
		if d.obs != nil {
			d.obs.qualitySkip = true
		}
		return
	}

	d.logger.Printf("running quality baseline")
	if err := d.runQualityBaseline(); err != nil {
		d.logger.Printf("quality baseline error: %v", err)
	} else {
		d.logger.Printf("quality baseline complete")
	}
	d.lastQualityBaseline = d.nowFn()
}

// handleDeadSessions reconciles the agents table with tmux reality.
// Proles without a live session are deleted (they're ephemeral); core agents
// are marked dead so restart cooldowns and history are preserved.
func (d *Daemon) handleDeadSessions() {
	agents, err := d.agents.ListAll()
	if err != nil {
		d.logger.Printf("error listing agents: %v", err)
		return
	}

	deleted := 0
	for _, agent := range agents {
		sessionAlive := agent.TmuxSession.Valid && agent.TmuxSession.String != "" && d.session.Exists(agent.TmuxSession.String)
		// Skip agents with a live session unless they are already marked dead.
		// A dead-status prole with a live session still needs to be cleaned up.
		if sessionAlive && agent.Status != repo.StatusDead {
			continue
		}
		if agent.Type == "prole" {
			if sessionAlive {
				// Zombie: row is dead but tmux session is still running — kill it.
				if err := d.session.Kill(agent.TmuxSession.String); err != nil {
					d.logger.Printf("error killing zombie session %s for %s: %v",
						agent.TmuxSession.String, agent.Name, err)
				} else {
					d.logger.Printf("killed zombie tmux session %s for dead prole %s",
						agent.TmuxSession.String, agent.Name)
				}
			}
			d.logger.Printf("prole %s is dead — cleaning up", agent.Name)
			if n, err := d.issues.ClearAssigneeByAgent(agent.Name); err != nil {
				d.logger.Printf("error clearing orphaned assignments for %s: %v", agent.Name, err)
			} else if n > 0 {
				d.logger.Printf("prole %s: cleared %d orphaned assignment(s) back to open", agent.Name, n)
			}
			if err := d.agents.Delete(agent.Name); err != nil {
				d.logger.Printf("error deleting prole %s: %v", agent.Name, err)
			} else {
				deleted++
			}
			continue
		}
		if agent.Status == repo.StatusDead {
			continue
		}
		d.logger.Printf("session %s for agent %s not found — marking dead",
			agent.TmuxSession.String, agent.Name)
		if err := d.agents.UpdateStatus(agent.Name, repo.StatusDead); err != nil {
			d.logger.Printf("error marking agent %s dead: %v", agent.Name, err)
		}
	}
	if d.obs != nil {
		d.obs.dead = deleted
	}
}

// handleDraftTickets prompts the Architect to pick up draft tickets.
func (d *Daemon) handleDraftTickets() {
	drafts, err := d.issues.List(repo.StatusDraft)
	if err != nil {
		d.logger.Printf("error listing draft tickets: %v", err)
		return
	}

	if d.obs != nil {
		d.obs.drafts = len(drafts)
	}

	if len(drafts) == 0 {
		return
	}

	architectSession := session.SessionName("architect")
	if !d.session.Exists(architectSession) {
		// Architect not running — attempt restart if enabled and off cooldown.
		if d.restartDeadAgents && d.restartAgent != nil && d.shouldRestart("architect") {
			architect, err := d.agents.Get("architect")
			if err == nil && (architect.Status == repo.StatusDead || architect.Status == repo.StatusIdle) {
				if err := d.restartAgent(architect); err != nil {
					d.logger.Printf("error restarting architect: %v", err)
				} else {
					d.recordRestart("architect")
				}
			}
		}
		return
	}

	if d.isAgentWorking("architect") {
		return // Architect is already working — don't pile on
	}

	draftIDs := make([]int, len(drafts))
	for i, issue := range drafts {
		draftIDs[i] = issue.ID
	}
	digest := ticketDigest(draftIDs)

	if !d.digestChanged(repo.StatusDraft, digest) || !d.shouldNudge(repo.StatusDraft) {
		return
	}

	ids := make([]string, len(drafts))
	for i, issue := range drafts {
		ids[i] = fmt.Sprintf("%s-%d (%s)", d.cfg.TicketPrefix, issue.ID, issue.Title)
	}

	msg := fmt.Sprintf("%d draft ticket(s) need spec: %s. "+
		"Run `gt ticket show <id>` on each and begin specification.",
		len(drafts), strings.Join(ids, "; "))

	if err := d.session.SendKeys(architectSession, msg); err != nil {
		d.logger.Printf("error nudging architect: %v", err)
	} else {
		d.logger.Printf("nudged architect: %d draft ticket(s)", len(drafts))
		d.recordNudge(repo.StatusDraft, digest)
	}
}

// handleInReviewTickets distributes in_review tickets across all active reviewer agents.
func (d *Daemon) handleInReviewTickets() {
	reviews, err := d.issues.List(repo.StatusInReview)
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

	if d.obs != nil {
		d.obs.inReview = len(withPR)
	}

	activeSessions := d.nudgeableReviewerSessions()
	if len(activeSessions) == 0 {
		// No active reviewer sessions — attempt to restart dead/idle reviewers.
		if d.restartDeadAgents && d.restartAgent != nil {
			d.restartDeadReviewers()
		}
		return
	}

	reviewIDs := make([]int, len(withPR))
	for i, issue := range withPR {
		reviewIDs[i] = issue.ID
	}
	digest := ticketDigest(reviewIDs)

	if !d.digestChanged(repo.StatusInReview, digest) || !d.shouldNudge(repo.StatusInReview) {
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
		if err := d.session.SendKeys(reviewerSession, msg); err != nil {
			d.logger.Printf("error nudging reviewer %s: %v", reviewerSession, err)
		} else {
			nudged++
		}
	}
	if nudged > 0 {
		d.logger.Printf("nudged %d reviewer(s): %d in_review ticket(s)", nudged, len(withPR))
		d.recordNudge(repo.StatusInReview, digest)
	}
}

// handleFollowUpReminder sends a periodic reminder to active reviewer sessions
// to file follow-up tickets for non-blocking notes from recent reviews.
//
// It fires when either of two conditions is met (whichever comes first):
//   - reviewsSinceFollowUp has reached followUpNReviews (count-based trigger)
//   - followUpInterval has elapsed since the last reminder (time-based trigger)
//
// After firing, the review counter is reset and the nudge timestamp updated so
// both triggers are re-armed together.
func (d *Daemon) handleFollowUpReminder() {
	sessions := d.nudgeableReviewerSessions()
	if len(sessions) == 0 {
		return
	}

	nReached := d.followUpNReviews > 0 && d.reviewsSinceFollowUp >= d.followUpNReviews
	var timeElapsed bool
	if d.followUpInterval > 0 {
		timeElapsed = d.nowFn().Sub(d.lastFollowUpNudge) >= d.followUpInterval
	} else {
		// Zero interval means disabled — only the count trigger fires.
		timeElapsed = false
	}

	if !nReached && !timeElapsed {
		return
	}

	msg := "Reminder: file follow-up tickets for any non-blocking notes from recent reviews."
	nudged := 0
	for _, s := range sessions {
		if err := d.session.SendKeys(s, msg); err != nil {
			d.logger.Printf("error sending follow-up reminder to reviewer %s: %v", s, err)
		} else {
			nudged++
		}
	}
	if nudged > 0 {
		d.logger.Printf("sent follow-up reminder to %d reviewer(s) (reviews since last nudge: %d)", nudged, d.reviewsSinceFollowUp)
		d.lastFollowUpNudge = d.nowFn()
		d.reviewsSinceFollowUp = 0
	}
}

// nudgeableReviewerSessions returns session names for reviewer agents that are
// alive, have an active tmux session, and are NOT already working.
func (d *Daemon) nudgeableReviewerSessions() []string {
	allAgents, err := d.agents.ListAll()
	if err != nil {
		d.logger.Printf("error listing agents for reviewer sessions: %v", err)
		return nil
	}
	var sessions []string
	for _, a := range allAgents {
		if a.Type != "reviewer" || a.Status == repo.StatusDead || a.Status == repo.StatusWorking {
			continue
		}
		s := session.SessionName(a.Name)
		if d.session.Exists(s) {
			sessions = append(sessions, s)
		}
	}
	return sessions
}

// restartDeadReviewers restarts any dead or idle reviewer agents that have no active tmux session,
// subject to the per-agent restart cooldown.
func (d *Daemon) restartDeadReviewers() {
	allAgents, err := d.agents.ListAll()
	if err != nil {
		d.logger.Printf("error listing agents for reviewer restart: %v", err)
		return
	}
	for _, a := range allAgents {
		if a.Type != "reviewer" {
			continue
		}
		if a.Status != repo.StatusDead && a.Status != repo.StatusIdle {
			continue
		}
		s := session.SessionName(a.Name)
		if d.session.Exists(s) {
			continue // session alive, no restart needed
		}
		if !d.shouldRestart(a.Name) {
			continue // on cooldown
		}
		if err := d.restartAgent(a); err != nil {
			d.logger.Printf("error restarting reviewer %s: %v", a.Name, err)
		} else {
			d.recordRestart(a.Name)
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

	if d.obs != nil {
		d.obs.prEventsTotal = len(tickets)
	}
	for _, issue := range tickets {
		if !issue.PRNumber.Valid {
			continue
		}

		prNum := int(issue.PRNumber.Int64)
		state, mergeable, checks, failing, merged, err := d.getPRState(prNum)
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
			d.handleOpenPR(issue, prNum, mergeable, checks, failing)
		}
	}
}

// handleOpenPR dispatches OPEN PR events based on mergeability, CI check status,
// and current ticket status.
//
// Precedence: merge conflicts are detected first (a conflicting branch cannot
// merge regardless of CI). CI failure is evaluated next for ci_running and
// pr_open tickets (pr_open is included as a defense against stale gt binaries
// that skip the ci_running state — nc-147). CI pass promotion is keyed to
// ci_running only so a pr_open ticket already in reviewer hands is not
// auto-promoted. Human-comment detection runs last for pr_open tickets.
//
// UNKNOWN mergeability is a no-op: GitHub returns this transiently (~5s after a
// push) while re-computing mergeability. We check for human comments but do not
// change ticket status, so a merge_conflict ticket never flips back until GitHub
// explicitly confirms MERGEABLE.
//
// under_review is a no-op for conflict detection: the reviewer owns the ticket
// in that state and the daemon must not grab it even if the branch is dirty.
func (d *Daemon) handleOpenPR(issue *repo.Issue, prNum int, mergeable, checks string, failing []string) {
	switch {
	case mergeable == "CONFLICTING" && (issue.Status == repo.StatusPROpen || issue.Status == repo.StatusCIRunning):
		d.handlePRConflict(issue, prNum)
	case mergeable == "MERGEABLE" && issue.Status == repo.StatusMergeConflict:
		d.handlePRConflictResolved(issue, prNum, checks)
	case (issue.Status == repo.StatusCIRunning || issue.Status == repo.StatusPROpen) && checks == "failing":
		d.handleCIFailure(issue, prNum, failing)
	case issue.Status == repo.StatusCIRunning && checks == "passing":
		d.handleCIPass(issue, prNum)
	// ci_running + pending: no-op, wait for checks to complete.
	default:
		d.checkForHumanComments(issue, prNum)
	}
}

// handleCIPass promotes a ci_running ticket to in_review once all CI checks pass.
// It clears the assignee so the reviewer can pick it up and the orphan-reconcile
// loop can recover the ticket if needed.
func (d *Daemon) handleCIPass(issue *repo.Issue, prNum int) {
	d.logger.Printf("CI passed on PR #%d for ticket %s-%d — promoting to in_review",
		prNum, d.cfg.TicketPrefix, issue.ID)

	if err := d.issues.UpdateStatus(issue.ID, repo.StatusInReview); err != nil {
		d.logger.Printf("error moving ticket %d to in_review: %v", issue.ID, err)
		return
	}
	if err := d.issues.ClearAssignee(issue.ID); err != nil {
		d.logger.Printf("error clearing assignee on ticket %d: %v", issue.ID, err)
	}
	if d.obs != nil {
		d.obs.prEventsCIPass++
	}
	d.reviewsSinceFollowUp++
}

// handleCIFailure moves a ci_running ticket to repairing and nudges the assigned
// prole with the names of the failing checks.
func (d *Daemon) handleCIFailure(issue *repo.Issue, prNum int, failedNames []string) {
	d.logger.Printf("CI failure on PR #%d for ticket %s-%d — moving to repairing",
		prNum, d.cfg.TicketPrefix, issue.ID)

	reason := "CI: " + strings.Join(failedNames, ", ")
	if !d.repairTransition(issue.ID, repo.StatusRepairing, reason) {
		return
	}
	if d.obs != nil {
		d.obs.prEventsCIFail++
	}

	if !issue.Assignee.Valid || issue.Assignee.String == "" {
		return
	}

	nudgeKey := fmt.Sprintf("ci_failed:%d", issue.ID)
	digest := fmt.Sprintf("%s-%d:ci:%s", d.cfg.TicketPrefix, issue.ID, strings.Join(failedNames, ","))
	if !d.digestChanged(nudgeKey, digest) || !d.shouldNudge(nudgeKey) {
		return
	}

	proleSession := session.SessionName(issue.Assignee.String)
	if !d.session.Exists(proleSession) {
		return
	}

	msg := fmt.Sprintf("CI FAILURE: PR #%d for ticket %s-%d (%s) has failing checks: %s. "+
		"Please fix the failures and push a corrected branch.",
		prNum, d.cfg.TicketPrefix, issue.ID, issue.Title, strings.Join(failedNames, ", "))

	if err := d.session.SendKeys(proleSession, msg); err != nil {
		d.logger.Printf("error nudging prole %s about CI failure on ticket %d: %v",
			issue.Assignee.String, issue.ID, err)
	} else {
		d.logger.Printf("nudged prole %s: CI failure on ticket %s-%d",
			issue.Assignee.String, d.cfg.TicketPrefix, issue.ID)
		d.recordNudge(nudgeKey, digest)
	}
}

// handlePRMerged is the reconciler that reacts to a merged PR: it closes the
// ticket and notifies the Mayor. Freeing the assignee agent and resetting its
// worktree are NOT this handler's job — proles free themselves via
// `gt agent status idle` in their Completion Protocol, and worktree hygiene
// is handled by the resetIdleProleWorktrees reconciler tick. See NC-53.
func (d *Daemon) handlePRMerged(issue *repo.Issue) {
	if issue.Status == repo.StatusClosed {
		return // already handled
	}

	d.logger.Printf("PR #%d merged for ticket %s-%d",
		issue.PRNumber.Int64, d.cfg.TicketPrefix, issue.ID)

	if err := d.issues.UpdateStatus(issue.ID, repo.StatusClosed); err != nil {
		d.logger.Printf("error closing ticket %d: %v", issue.ID, err)
		return
	}

	if d.obs != nil {
		d.obs.prEventsMerged++
	}

	mayorSession := session.SessionName("mayor")
	if d.session.Exists(mayorSession) {
		msg := fmt.Sprintf("PR #%d merged. Ticket %s-%d (%s) is now closed.",
			issue.PRNumber.Int64, d.cfg.TicketPrefix, issue.ID, issue.Title)
		d.session.SendKeys(mayorSession, msg) //nolint:errcheck // fire-and-forget notification to Mayor
	}
}

func (d *Daemon) handlePRClosed(issue *repo.Issue) {
	if issue.Status == repo.StatusClosed {
		return
	}

	// Dedup: a re-opened ticket may carry a stale pr_number indefinitely;
	// the backfill reconciler can also re-attach a closed PR from the branch
	// name. Without this guard the Mayor receives an identical ESCALATION on
	// every poll tick until the ticket is manually resolved.
	nudgeKey := fmt.Sprintf("pr_closed_escalation:%d", issue.ID)
	digest := fmt.Sprintf("%d:closed", issue.PRNumber.Int64)
	if !d.digestChanged(nudgeKey, digest) || !d.shouldNudge(nudgeKey) {
		return
	}

	d.logger.Printf("PR #%d closed without merge for ticket %s-%d — escalating to Mayor",
		issue.PRNumber.Int64, d.cfg.TicketPrefix, issue.ID)

	if d.obs != nil {
		d.obs.prEventsClosed++
	}

	// Escalate to Mayor
	mayorSession := session.SessionName("mayor")
	if d.session.Exists(mayorSession) {
		msg := fmt.Sprintf("ESCALATION: PR #%d for ticket %s-%d (%s) was closed without merging. "+
			"Please decide next action.",
			issue.PRNumber.Int64, d.cfg.TicketPrefix, issue.ID, issue.Title)
		d.session.SendKeys(mayorSession, msg) //nolint:errcheck // fire-and-forget notification to Mayor
	}
	d.recordNudge(nudgeKey, digest)
}

// handlePRConflict moves a pr_open ticket to merge_conflict and nudges the architect.
func (d *Daemon) handlePRConflict(issue *repo.Issue, prNum int) {
	d.logger.Printf("PR #%d has merge conflict for ticket %s-%d — moving to merge_conflict",
		prNum, d.cfg.TicketPrefix, issue.ID)

	if !d.repairTransition(issue.ID, repo.StatusMergeConflict, "merge conflict with main") {
		return
	}
	if d.obs != nil {
		d.obs.prEventsConflict++
	}

	architectSession := session.SessionName("architect")
	if !d.session.Exists(architectSession) {
		return
	}

	nudgeKey := fmt.Sprintf("merge_conflict:%d", issue.ID)
	digest := fmt.Sprintf("%s-%d:conflict", d.cfg.TicketPrefix, issue.ID)
	if !d.digestChanged(nudgeKey, digest) || !d.shouldNudge(nudgeKey) {
		return
	}

	msg := fmt.Sprintf("MERGE CONFLICT: PR #%d for ticket %s-%d (%s) has a merge conflict. "+
		"Please resolve the conflict and push a fixed branch.",
		prNum, d.cfg.TicketPrefix, issue.ID, issue.Title)

	if err := d.session.SendKeys(architectSession, msg); err != nil {
		d.logger.Printf("error nudging architect about merge conflict on ticket %d: %v", issue.ID, err)
	} else {
		d.logger.Printf("nudged architect: merge conflict on ticket %s-%d", d.cfg.TicketPrefix, issue.ID)
		d.recordNudge(nudgeKey, digest)
	}
}

// handlePRConflictResolved moves a merge_conflict ticket forward when the conflict clears.
// If CI checks are pending (the common case after a conflict-fix push that re-triggers CI)
// the ticket advances to ci_running so the daemon can track the new run and promote to
// in_review on pass. If checks are already passing (e.g. no CI configured) the ticket
// moves back to pr_open.
func (d *Daemon) handlePRConflictResolved(issue *repo.Issue, prNum int, checks string) {
	nextStatus := repo.StatusPROpen
	if checks == "pending" {
		nextStatus = repo.StatusCIRunning
	}

	d.logger.Printf("PR #%d conflict resolved for ticket %s-%d — moving to %s",
		prNum, d.cfg.TicketPrefix, issue.ID, nextStatus)

	if err := d.issues.UpdateStatus(issue.ID, nextStatus); err != nil {
		d.logger.Printf("error moving ticket %d to %s: %v", issue.ID, nextStatus, err)
		return
	}

	if d.obs != nil {
		d.obs.prEventsConflictResolved++
	}
}

func (d *Daemon) checkForHumanComments(issue *repo.Issue, prNum int) {
	// Only act when the ticket is in pr_open. During under_review the AI reviewer
	// owns the ticket — any review posted in that window is its own work.
	if issue.Status != repo.StatusPROpen {
		return
	}

	comments, err := d.reviewComments(prNum)
	if err != nil {
		d.logger.Printf("error checking comments on PR #%d: %v", prNum, err)
		return
	}

	// For tdd_tests tickets, a human APPROVED review closes the ticket (clearing
	// the dependency edge so the paired implementation ticket becomes selectable).
	// Check for approval before the general human-comment repairing path.
	if issue.IssueType == "tdd_tests" && d.checkForTDDTestsApproval(issue, prNum, comments) {
		return
	}

	for _, c := range comments {
		// Skip bot accounts and comments from the AI reviewer (sentinel prefix).
		if c.IsBot || strings.HasPrefix(strings.TrimSpace(c.Body), "[ct-reviewer]") {
			continue
		}

		d.logger.Printf("human comment on PR #%d by %s — moving ticket %s-%d to repairing",
			prNum, c.Author, d.cfg.TicketPrefix, issue.ID)

		excerpt := c.Body
		if runes := []rune(excerpt); len(runes) > 120 {
			excerpt = string(runes[:120]) + "…"
		}
		reason := fmt.Sprintf("review: %s: %s", c.Author, excerpt)
		if !d.repairTransition(issue.ID, repo.StatusRepairing, reason) {
			return
		}
		if d.obs != nil {
			d.obs.prEventsRepairing++
		}

		return // only need one human comment to trigger repair
	}
}

// checkForTDDTestsApproval closes a tdd_tests ticket when a human reviewer has
// posted an APPROVED review on its draft PR. After closing, it copies the
// ticket's branch and PR number to any open implementation tickets that depend
// on it, so assign.Execute can switch the prole's worktree to the existing
// branch without manual setup.
//
// Returns true if an approval was found and the ticket was closed (or the close
// failed — either way the caller should not continue processing). Returns false
// when no APPROVED human review exists.
func (d *Daemon) checkForTDDTestsApproval(issue *repo.Issue, prNum int, comments []prComment) bool {
	for _, c := range comments {
		if c.IsBot || c.State != "APPROVED" {
			continue
		}
		d.logger.Printf("human approval on PR #%d by %s — closing tdd_tests ticket %s-%d",
			prNum, c.Author, d.cfg.TicketPrefix, issue.ID)

		if err := d.issues.UpdateStatus(issue.ID, repo.StatusClosed); err != nil {
			d.logger.Printf("error closing tdd_tests ticket %d: %v", issue.ID, err)
			return true // found approval, but update failed — still stop processing
		}
		if d.obs != nil {
			d.obs.prEventsTDDApproved++
		}

		// Copy branch and PR to dependent implementation tickets so they inherit
		// the existing branch/PR when assigned to a prole.
		d.handoffBranchToImplementation(issue, prNum)

		return true
	}
	return false
}

// handoffBranchToImplementation copies the branch and PR number from a closed
// tdd_tests ticket to any open implementation tickets that depend on it. This
// ensures the prole assigned to the implementation ticket starts on the same
// branch and can push to the existing draft PR without manual setup.
func (d *Daemon) handoffBranchToImplementation(testsTicket *repo.Issue, prNum int) {
	dependents, err := d.issues.GetDependents(testsTicket.ID)
	if err != nil {
		d.logger.Printf("tdd handoff: error finding dependents of ticket %d: %v", testsTicket.ID, err)
		return
	}

	branch := testsTicket.Branch.String
	if !testsTicket.Branch.Valid || branch == "" {
		d.logger.Printf("tdd handoff: ticket %d has no branch set; skipping handoff", testsTicket.ID)
		return
	}

	for _, impl := range dependents {
		if err := d.issues.SetBranch(impl.ID, branch); err != nil {
			d.logger.Printf("tdd handoff: error setting branch on ticket %d: %v", impl.ID, err)
			continue
		}
		if err := d.issues.SetPR(impl.ID, prNum); err != nil {
			d.logger.Printf("tdd handoff: error setting PR on ticket %d: %v", impl.ID, err)
			continue
		}
		d.logger.Printf("tdd handoff: copied branch %q and PR #%d from tests ticket %s-%d to impl ticket %s-%d",
			branch, prNum, d.cfg.TicketPrefix, testsTicket.ID, d.cfg.TicketPrefix, impl.ID)
	}
}

// repairTransition moves a ticket to a repair-ish status (typically "repairing"
// or "merge_conflict") and records the reason. Returns true when the status
// update succeeded; the caller should return or skip further processing on
// false. SetRepairReason failure is non-fatal — logged but does not abort.
// Incrementing the per-handler obs counter is left to the caller because each
// call site tracks a different field.
func (d *Daemon) repairTransition(issueID int, status, reason string) bool {
	if err := d.issues.UpdateStatus(issueID, status); err != nil {
		d.logger.Printf("error moving ticket %d to %s: %v", issueID, status, err)
		return false
	}
	if err := d.issues.SetRepairReason(issueID, reason); err != nil {
		d.logger.Printf("error setting repair reason on ticket %d: %v", issueID, err)
	}
	return true
}

// prComment holds data from a GitHub PR review.
type prComment struct {
	Author string
	IsBot  bool
	Body   string
	State  string // GitHub review state: APPROVED, CHANGES_REQUESTED, COMMENTED, etc.
}

func (d *Daemon) getPRState(prNum int) (state, mergeable, checks string, failing []string, merged bool, err error) {
	if d.getPRStateFn != nil {
		return d.getPRStateFn(prNum)
	}
	cmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNum), "--json", "state,mergedAt,mergeable,statusCheckRollup")
	cmd.Dir = d.cfg.ProjectRoot
	out, execErr := runCmd(cmd)
	if execErr != nil {
		return "", "", "", nil, false, fmt.Errorf("gh pr view: %w", execErr)
	}

	var result struct {
		State             string  `json:"state"`
		MergedAt          *string `json:"mergedAt"`
		Mergeable         string  `json:"mergeable"`
		StatusCheckRollup []struct {
			Name       string `json:"name"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
		} `json:"statusCheckRollup"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return "", "", "", nil, false, fmt.Errorf("parsing PR state: %w", err)
	}

	checksStatus, failingNames := classifyChecks(result.StatusCheckRollup)
	return result.State, result.Mergeable, checksStatus, failingNames, result.MergedAt != nil, nil
}

// classifyChecks returns a check summary ("passing", "failing", or "pending")
// and the names of any failing checks. Failing takes precedence over pending;
// no checks is treated as passing (CI not configured).
//
// Failing conclusions: FAILURE, CANCELLED, TIMED_OUT, STARTUP_FAILURE, ACTION_REQUIRED.
// Pending statuses: IN_PROGRESS, QUEUED, WAITING.
// Passing conclusions: SUCCESS, NEUTRAL, SKIPPED.
func classifyChecks(checks []struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
}) (status string, failing []string) {
	hasPending := false
	for _, c := range checks {
		switch c.Status {
		case "IN_PROGRESS", "QUEUED", "WAITING":
			hasPending = true
		case "COMPLETED":
			switch c.Conclusion {
			case "FAILURE", "CANCELLED", "TIMED_OUT", "STARTUP_FAILURE", "ACTION_REQUIRED":
				failing = append(failing, c.Name)
			}
		}
	}
	if len(failing) > 0 {
		return "failing", failing
	}
	if hasPending {
		return "pending", nil
	}
	return "passing", nil
}

func (d *Daemon) getReviewComments(prNum int) ([]prComment, error) {
	cmd := exec.Command("gh", "api",
		fmt.Sprintf("repos/{owner}/{repo}/pulls/%d/reviews", prNum),
		"--jq", ".[] | {author: .user.login, authorType: .user.type, state: .state, body: .body}")
	cmd.Dir = d.cfg.ProjectRoot
	out, err := runCmd(cmd)
	if err != nil {
		return nil, fmt.Errorf("gh api: %w", err)
	}

	if len(strings.TrimSpace(string(out))) == 0 {
		return nil, nil
	}

	var comments []prComment
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if c, ok := parseReviewLine([]byte(line)); ok {
			comments = append(comments, c)
		}
	}

	return comments, nil
}

// parseReviewLine parses one JSONL line from the GitHub reviews API into a prComment.
func parseReviewLine(line []byte) (prComment, bool) {
	var review struct {
		Author     string `json:"author"`
		AuthorType string `json:"authorType"`
		State      string `json:"state"`
		Body       string `json:"body"`
	}
	if err := json.Unmarshal(line, &review); err != nil {
		return prComment{}, false
	}
	return prComment{
		Author: review.Author,
		IsBot:  review.AuthorType == "Bot",
		Body:   review.Body,
		State:  review.State,
	}, true
}
