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
	dead             int  // proles deleted (handleDeadSessions)
	worktreesSkip    bool // stale-worktree handler was interval-guarded
	worktreesPruned  int  // stale worktrees pruned (0 if skipped)
	prBackfillFound  int  // tickets missing PR number
	prBackfillDone   int  // PR numbers successfully backfilled
	drafts           int  // draft tickets found
	assignCandidates int  // selectable ticket count
	assignSlots      int  // available prole slot count
	assignPaired     int  // tickets actually assigned
	inReview         int  // in_review tickets with a PR number
	prEventsTotal    int  // tickets with PRs checked
	prEventsMerged   int  // PRs merged this tick
	prEventsRepairing int // open PRs moved to repairing (human comment)
	prEventsClosed   int  // PRs closed without merge
	epics            int  // completable epics found
	qualitySkip      bool // quality baseline was interval-guarded
}

// Daemon polls for state changes and routes work to agents.
type Daemon struct {
	cfg           *config.Config
	issues        *repo.IssueRepo
	agents        *repo.AgentRepo
	logger        *log.Logger
	stop          chan struct{}
	sendKeys      func(session, msg string) error
	sessionExists func(session string) bool
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
	pruneStaleWorktrees  func() (int, error)
	lastWorktreePrune    time.Time
	worktreeInterval     time.Duration

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

	// Review comment fetching (injectable for tests)
	getReviewCommentsFn func(prNum int) ([]prComment, error)

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

	return &Daemon{
		cfg:           cfg,
		issues:        repo.NewIssueRepo(db, events),
		agents:        agentRepo,
		logger:        logger,
		stop:          make(chan struct{}),
		sendKeys:      session.SendKeys,
		sessionExists: session.Exists,
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
		prBackfillInterval: time.Duration(cfg.PRBackfillIntervalSeconds) * time.Second,
		restartDeadAgents:  cfg.RestartDeadAgents,
		restartCooldown:    time.Duration(cfg.RestartCooldownSeconds) * time.Second,
		lastRestartedAt:    make(map[string]time.Time),
		restartAgent:       makeRestartFn(cfg, agentRepo, logger),
		tickFile:           filepath.Join(ctDir, "run", "daemon-tick"),
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

		if err := agents.UpdateStatus(agent.Name, "idle"); err != nil {
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
		}); err != nil {
			agents.UpdateStatus(agent.Name, "dead") //nolint:errcheck
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
	return agent.Status == "working"
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
	d.handleInReviewTickets()
	d.handlePREvents()
	d.handleEpicAutoClose()
	d.handleQualityBaseline()

	logTickSummary(d.logger, *d.obs)
	d.obs = nil
	d.writeHeartbeat()
}

// logTickSummary emits a single tick: summary line in fixed key=value format.
// Field order and format are a stability contract — operators grep/awk these
// tokens to monitor daemon health over time. Do not reorder or rename tokens
// without updating all downstream tooling.
//
// Format:
//
//	tick: dead=N worktrees=skip|N prBackfill=N/N drafts=N assign=N/N/N inReview=N prEvents=N/N/N/N epics=N quality=skip|ran
//
// Fields:
//
//	dead         — proles deleted (dead sessions)
//	worktrees    — stale worktrees pruned, or "skip" when interval-guarded
//	prBackfill   — tickets missing PR / PR numbers successfully backfilled
//	drafts       — draft tickets found this tick
//	assign       — selectable tickets / available slots / actually assigned
//	inReview     — in_review tickets with a PR number
//	prEvents     — tickets with PRs / merged / moved-to-repairing / closed-without-merge
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
	logger.Printf("tick: dead=%d worktrees=%s prBackfill=%d/%d drafts=%d assign=%d/%d/%d inReview=%d prEvents=%d/%d/%d/%d epics=%d quality=%s",
		obs.dead,
		worktrees,
		obs.prBackfillFound, obs.prBackfillDone,
		obs.drafts,
		obs.assignCandidates, obs.assignSlots, obs.assignPaired,
		obs.inReview,
		obs.prEventsTotal, obs.prEventsMerged, obs.prEventsRepairing, obs.prEventsClosed,
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

// lookupPRForBranch queries GitHub for an open PR matching the given head branch.
// Returns (prNumber, found, error). found is false when no matching PR exists.
func lookupPRForBranch(branch, projectRoot string) (int, bool, error) {
	cmd := exec.Command("gh", "pr", "list", "--head", branch, "--json", "number", "--limit", "1")
	cmd.Dir = projectRoot
	out, err := cmd.Output()
	if err != nil {
		return 0, false, fmt.Errorf("gh pr list: %w", err)
	}

	var results []struct {
		Number int `json:"number"`
	}
	if err := json.Unmarshal(out, &results); err != nil {
		return 0, false, fmt.Errorf("parsing PR list: %w", err)
	}
	if len(results) == 0 {
		return 0, false, nil
	}
	return results[0].Number, true, nil
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

		if err := d.issues.UpdateStatus(epic.ID, "closed"); err != nil {
			d.logger.Printf("error closing epic %d: %v", epic.ID, err)
			continue
		}

		mayorSession := session.SessionName("mayor")
		if d.sessionExists(mayorSession) {
			msg := fmt.Sprintf("Epic %s-%d (%s) auto-closed: all sub-tasks are complete.",
				d.cfg.TicketPrefix, epic.ID, epic.Title)
			if err := d.sendKeys(mayorSession, msg); err != nil {
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

		if err := d.sendKeys(mayorSession, msg); err != nil {
			d.logger.Printf("error escalating stuck agent %s to Mayor: %v", agent.Name, err)
		} else {
			d.recordNudge(nudgeKey, "")
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
		if agent.TmuxSession.Valid && agent.TmuxSession.String != "" && d.sessionExists(agent.TmuxSession.String) {
			continue
		}
		if agent.Type == "prole" {
			d.logger.Printf("prole %s has no live tmux session — deleting", agent.Name)
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
		if agent.Status == "dead" {
			continue
		}
		d.logger.Printf("session %s for agent %s not found — marking dead",
			agent.TmuxSession.String, agent.Name)
		if err := d.agents.UpdateStatus(agent.Name, "dead"); err != nil {
			d.logger.Printf("error marking agent %s dead: %v", agent.Name, err)
		}
	}
	if d.obs != nil {
		d.obs.dead = deleted
	}
}


// handleDraftTickets prompts the Architect to pick up draft tickets.
func (d *Daemon) handleDraftTickets() {
	drafts, err := d.issues.List("draft")
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
	if !d.sessionExists(architectSession) {
		// Architect not running — attempt restart if enabled and off cooldown.
		if d.restartDeadAgents && d.restartAgent != nil && d.shouldRestart("architect") {
			architect, err := d.agents.Get("architect")
			if err == nil && (architect.Status == "dead" || architect.Status == "idle") {
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

	if !d.digestChanged("draft", digest) || !d.shouldNudge("draft") {
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
		d.recordNudge("draft", digest)
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

	if !d.digestChanged("in_review", digest) || !d.shouldNudge("in_review") {
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
		d.recordNudge("in_review", digest)
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
		if a.Type != "reviewer" || a.Status == "dead" || a.Status == "working" {
			continue
		}
		s := session.SessionName(a.Name)
		if d.sessionExists(s) {
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
		if a.Status != "dead" && a.Status != "idle" {
			continue
		}
		s := session.SessionName(a.Name)
		if d.sessionExists(s) {
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

// handlePRMerged is the reconciler that reacts to a merged PR: it closes the
// ticket and notifies the Mayor. Freeing the assignee agent and resetting its
// worktree are NOT this handler's job — proles free themselves via
// `gt agent status idle` in their Completion Protocol, and worktree hygiene
// is handled by the resetIdleProleWorktrees reconciler tick. See NC-53.
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

	if d.obs != nil {
		d.obs.prEventsMerged++
	}

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

	if d.obs != nil {
		d.obs.prEventsClosed++
	}

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
	// Only act when the ticket is in pr_open. During under_review the AI reviewer
	// owns the ticket — any review posted in that window is its own work.
	if issue.Status != "pr_open" {
		return
	}

	comments, err := d.reviewComments(prNum)
	if err != nil {
		d.logger.Printf("error checking comments on PR #%d: %v", prNum, err)
		return
	}

	for _, c := range comments {
		// Skip bot accounts and comments from the AI reviewer (sentinel prefix).
		if c.IsBot || strings.HasPrefix(strings.TrimSpace(c.Body), "[ct-reviewer]") {
			continue
		}

		d.logger.Printf("human comment on PR #%d by %s — moving ticket %s-%d to repairing",
			prNum, c.Author, d.cfg.TicketPrefix, issue.ID)

		if err := d.issues.UpdateStatus(issue.ID, "repairing"); err != nil {
			d.logger.Printf("error updating ticket %d to repairing: %v", issue.ID, err)
			return
		}

		if d.obs != nil {
			d.obs.prEventsRepairing++
		}

		return // only need one human comment to trigger repair
	}
}

// prComment holds data from a GitHub PR review.
type prComment struct {
	Author string
	IsBot  bool
	Body   string
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
		"--jq", ".[] | {author: .user.login, authorType: .user.type, state: .state, body: .body}")
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
	}, true
}
