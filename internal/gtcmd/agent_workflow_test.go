package gtcmd

import (
	"bytes"
	"os"
	"strconv"
	"testing"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

// setupWorkflowTest creates a DB-backed agentWorkflowDeps with the given config.
// stderr is wired to a bytes.Buffer so tests can inspect warning output without
// pipe goroutines or race conditions.
func setupWorkflowTest(t *testing.T, cfg *config.Config) (agentWorkflowDeps, *bytes.Buffer) {
	t.Helper()
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	var buf bytes.Buffer
	return agentWorkflowDeps{
		agents: repo.NewAgentRepo(conn, nil),
		issues: repo.NewIssueRepo(conn, nil),
		cfg:    cfg,
		stderr: &buf,
	}, &buf
}

// reviewerCfg returns a config with a reviewer accept transition in_review→under_review.
func reviewerCfg() *config.Config {
	return &config.Config{
		TicketPrefix: "nc",
		Agents: config.AgentsConfig{
			Reviewer: config.AgentConfig{
				Model: "sonnet",
				Workflow: &config.WorkflowConfig{
					Accept: &config.WorkflowAction{
						TicketTransition: &config.TicketTransition{From: "in_review", To: "under_review"},
					},
				},
			},
			Prole: config.AgentConfig{
				Model:    "sonnet",
				Workflow: &config.WorkflowConfig{},
			},
		},
	}
}

// proleCfg returns a config where the prole has nil workflow.
func proleCfg() *config.Config {
	return &config.Config{
		TicketPrefix: "nc",
		Agents: config.AgentsConfig{
			Prole: config.AgentConfig{Model: "sonnet"},
		},
	}
}

func mustRegisterAgent(t *testing.T, agents *repo.AgentRepo, name, agentType string) {
	t.Helper()
	if err := agents.Register(name, agentType, nil); err != nil {
		t.Fatalf("Register %s: %v", name, err)
	}
}

func mustCreateTicket(t *testing.T, issues *repo.IssueRepo, status string) int {
	t.Helper()
	id, err := issues.Create("test ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create ticket: %v", err)
	}
	if err := issues.UpdateStatus(id, status); err != nil {
		t.Fatalf("UpdateStatus %s: %v", status, err)
	}
	return id
}

func setEnv(t *testing.T, key, val string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	os.Setenv(key, val)
	t.Cleanup(func() {
		if had {
			os.Setenv(key, old)
		} else {
			os.Unsetenv(key)
		}
	})
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	os.Unsetenv(key)
	t.Cleanup(func() {
		if had {
			os.Setenv(key, old)
		}
	})
}

// --- accept tests ---

func TestAgentAccept_updatesAgentRow(t *testing.T) {
	deps, _ := setupWorkflowTest(t, proleCfg())
	mustRegisterAgent(t, deps.agents, "iron", "prole")
	id := mustCreateTicket(t, deps.issues, "open")
	setEnv(t, "CT_AGENT_NAME", "iron")

	if err := agentAccept(deps, []string{idStr(id)}); err != nil {
		t.Fatalf("agentAccept: %v", err)
	}

	agent, _ := deps.agents.Get("iron")
	if agent.Status != "working" {
		t.Errorf("expected status=working, got %q", agent.Status)
	}
	if !agent.CurrentIssue.Valid || int(agent.CurrentIssue.Int64) != id {
		t.Errorf("expected current_issue=%d, got %v", id, agent.CurrentIssue)
	}
}

func TestAgentAccept_firesTicketTransition(t *testing.T) {
	deps, _ := setupWorkflowTest(t, reviewerCfg())
	mustRegisterAgent(t, deps.agents, "reviewer", "reviewer")
	id := mustCreateTicket(t, deps.issues, "in_review")
	setEnv(t, "CT_AGENT_NAME", "reviewer")

	if err := agentAccept(deps, []string{idStr(id)}); err != nil {
		t.Fatalf("agentAccept: %v", err)
	}

	issue, _ := deps.issues.Get(id)
	if issue.Status != "under_review" {
		t.Errorf("expected status=under_review, got %q", issue.Status)
	}
}

func TestAgentAccept_skipsTransitionOnFromMismatch(t *testing.T) {
	deps, _ := setupWorkflowTest(t, reviewerCfg())
	mustRegisterAgent(t, deps.agents, "reviewer", "reviewer")
	id := mustCreateTicket(t, deps.issues, "repairing")
	setEnv(t, "CT_AGENT_NAME", "reviewer")

	if err := agentAccept(deps, []string{idStr(id)}); err != nil {
		t.Fatalf("agentAccept: %v", err)
	}

	// Agent row updated.
	agent, _ := deps.agents.Get("reviewer")
	if agent.Status != "working" {
		t.Errorf("expected status=working, got %q", agent.Status)
	}
	// Ticket status unchanged.
	issue, _ := deps.issues.Get(id)
	if issue.Status != "repairing" {
		t.Errorf("expected ticket status=repairing (untouched), got %q", issue.Status)
	}
}

func TestAgentAccept_noWorkflowJustUpdatesAgentRow(t *testing.T) {
	deps, _ := setupWorkflowTest(t, proleCfg())
	mustRegisterAgent(t, deps.agents, "copper", "prole")
	id := mustCreateTicket(t, deps.issues, "open")
	setEnv(t, "CT_AGENT_NAME", "copper")

	if err := agentAccept(deps, []string{idStr(id)}); err != nil {
		t.Fatalf("agentAccept: %v", err)
	}

	agent, _ := deps.agents.Get("copper")
	if agent.Status != "working" {
		t.Errorf("expected status=working, got %q", agent.Status)
	}
	issue, _ := deps.issues.Get(id)
	if issue.Status != "open" {
		t.Errorf("expected ticket status=open (untouched), got %q", issue.Status)
	}
}

func TestAgentAccept_missingCTAgentNameErrors(t *testing.T) {
	deps, _ := setupWorkflowTest(t, proleCfg())
	unsetEnv(t, "CT_AGENT_NAME")

	err := agentAccept(deps, []string{"1"})
	if err == nil {
		t.Fatal("expected error for missing CT_AGENT_NAME, got nil")
	}
}

func TestAgentAccept_missingTicketErrors(t *testing.T) {
	deps, _ := setupWorkflowTest(t, proleCfg())
	mustRegisterAgent(t, deps.agents, "iron", "prole")
	setEnv(t, "CT_AGENT_NAME", "iron")

	err := agentAccept(deps, []string{"99999"})
	if err == nil {
		t.Fatal("expected error for missing ticket, got nil")
	}
}

func TestAgentAccept_idempotent(t *testing.T) {
	deps, _ := setupWorkflowTest(t, reviewerCfg())
	mustRegisterAgent(t, deps.agents, "reviewer", "reviewer")
	id := mustCreateTicket(t, deps.issues, "in_review")
	setEnv(t, "CT_AGENT_NAME", "reviewer")

	if err := agentAccept(deps, []string{idStr(id)}); err != nil {
		t.Fatalf("first accept: %v", err)
	}
	// Second accept — ticket now under_review, not in_review: transition skipped.
	if err := agentAccept(deps, []string{idStr(id)}); err != nil {
		t.Fatalf("second accept: %v", err)
	}

	issue, _ := deps.issues.Get(id)
	if issue.Status != "under_review" {
		t.Errorf("expected under_review after idempotent accept, got %q", issue.Status)
	}
}

// TestAgentAccept_artisanWorkflow demonstrates that an artisan specialty with
// a custom workflow works end-to-end — roles are pure data.
func TestAgentAccept_artisanWorkflow(t *testing.T) {
	cfg := &config.Config{
		TicketPrefix: "nc",
		Agents: config.AgentsConfig{
			Artisan: config.ArtisanConfig{
				"qa": {
					Model: "sonnet",
					Workflow: &config.WorkflowConfig{
						Accept: &config.WorkflowAction{
							TicketTransition: &config.TicketTransition{From: "qa_ready", To: "in_qa"},
						},
					},
				},
			},
			Prole: config.AgentConfig{Model: "sonnet"},
		},
	}
	deps, _ := setupWorkflowTest(t, cfg)
	mustRegisterAgent(t, deps.agents, "qa", "artisan")
	id := mustCreateTicket(t, deps.issues, "qa_ready")
	setEnv(t, "CT_AGENT_NAME", "qa")

	if err := agentAccept(deps, []string{idStr(id)}); err != nil {
		t.Fatalf("agentAccept: %v", err)
	}

	issue, _ := deps.issues.Get(id)
	if issue.Status != "in_qa" {
		t.Errorf("expected status=in_qa, got %q", issue.Status)
	}
}

// --- release tests ---

func TestAgentRelease_clearsAgentRow(t *testing.T) {
	deps, _ := setupWorkflowTest(t, proleCfg())
	mustRegisterAgent(t, deps.agents, "iron", "prole")
	id := mustCreateTicket(t, deps.issues, "in_progress")
	setEnv(t, "CT_AGENT_NAME", "iron")

	// Set agent to working state.
	deps.agents.SetCurrentIssue("iron", &id)

	if err := agentRelease(deps, nil); err != nil {
		t.Fatalf("agentRelease: %v", err)
	}

	agent, _ := deps.agents.Get("iron")
	if agent.Status != "idle" {
		t.Errorf("expected status=idle, got %q", agent.Status)
	}
	if agent.CurrentIssue.Valid {
		t.Errorf("expected current_issue=NULL, got %d", agent.CurrentIssue.Int64)
	}
}

func TestAgentRelease_firesTicketTransitionWhenConfigured(t *testing.T) {
	cfg := &config.Config{
		TicketPrefix: "nc",
		Agents: config.AgentsConfig{
			Reviewer: config.AgentConfig{
				Model: "sonnet",
				Workflow: &config.WorkflowConfig{
					Release: &config.WorkflowAction{
						TicketTransition: &config.TicketTransition{From: "under_review", To: "in_review"},
					},
				},
			},
			Prole: config.AgentConfig{Model: "sonnet"},
		},
	}
	deps, _ := setupWorkflowTest(t, cfg)
	mustRegisterAgent(t, deps.agents, "reviewer", "reviewer")
	id := mustCreateTicket(t, deps.issues, "under_review")
	deps.agents.SetCurrentIssue("reviewer", &id)
	setEnv(t, "CT_AGENT_NAME", "reviewer")

	if err := agentRelease(deps, nil); err != nil {
		t.Fatalf("agentRelease: %v", err)
	}

	issue, _ := deps.issues.Get(id)
	if issue.Status != "in_review" {
		t.Errorf("expected status=in_review after release, got %q", issue.Status)
	}
}

func TestAgentRelease_safeWhenAlreadyIdle(t *testing.T) {
	deps, _ := setupWorkflowTest(t, proleCfg())
	mustRegisterAgent(t, deps.agents, "iron", "prole")
	setEnv(t, "CT_AGENT_NAME", "iron")

	// No SetCurrentIssue — agent is already idle with no issue.
	if err := agentRelease(deps, nil); err != nil {
		t.Fatalf("agentRelease on idle agent: %v", err)
	}

	agent, _ := deps.agents.Get("iron")
	if agent.Status != "idle" {
		t.Errorf("expected status=idle, got %q", agent.Status)
	}
}

// --- do tests ---

// doCfg returns a config where the prole has a workflow with a custom action "submit".
func doCfg() *config.Config {
	return &config.Config{
		TicketPrefix: "nc",
		Agents: config.AgentsConfig{
			Prole: config.AgentConfig{
				Model: "sonnet",
				Workflow: &config.WorkflowConfig{
					Actions: map[string]*config.WorkflowAction{
						"submit": {
							TicketTransition: &config.TicketTransition{From: "in_progress", To: "in_review"},
						},
						"noop": {}, // non-nil action with no TicketTransition
					},
				},
			},
		},
	}
}

func TestAgentDo_happyPath(t *testing.T) {
	deps, _ := setupWorkflowTest(t, doCfg())
	mustRegisterAgent(t, deps.agents, "copper", "prole")
	id := mustCreateTicket(t, deps.issues, "in_progress")
	setEnv(t, "CT_AGENT_NAME", "copper")

	if err := agentDo(deps, []string{"submit", idStr(id)}); err != nil {
		t.Fatalf("agentDo: %v", err)
	}

	issue, _ := deps.issues.Get(id)
	if issue.Status != "in_review" {
		t.Errorf("expected status=in_review, got %q", issue.Status)
	}
	// Agent row must be untouched (do does not modify agent).
	agent, _ := deps.agents.Get("copper")
	if agent.Status != "idle" {
		t.Errorf("expected agent status=idle (unchanged), got %q", agent.Status)
	}
}

func TestAgentDo_statusMismatchSkips(t *testing.T) {
	deps, buf := setupWorkflowTest(t, doCfg())
	mustRegisterAgent(t, deps.agents, "copper", "prole")
	id := mustCreateTicket(t, deps.issues, "open") // wrong status
	setEnv(t, "CT_AGENT_NAME", "copper")

	if err := agentDo(deps, []string{"submit", idStr(id)}); err != nil {
		t.Fatalf("agentDo: %v", err)
	}

	// Ticket status must be unchanged.
	issue, _ := deps.issues.Get(id)
	if issue.Status != "open" {
		t.Errorf("expected status=open (unchanged), got %q", issue.Status)
	}
	// A note should have been printed to stderr.
	if buf.Len() == 0 {
		t.Error("expected a note on stderr for status mismatch, got nothing")
	}
}

func TestAgentDo_unknownActionErrors(t *testing.T) {
	deps, _ := setupWorkflowTest(t, doCfg())
	mustRegisterAgent(t, deps.agents, "copper", "prole")
	id := mustCreateTicket(t, deps.issues, "in_progress")
	setEnv(t, "CT_AGENT_NAME", "copper")

	err := agentDo(deps, []string{"no_such_action", idStr(id)})
	if err == nil {
		t.Fatal("expected error for unknown action, got nil")
	}
}

func TestAgentDo_nilTransitionNoops(t *testing.T) {
	deps, _ := setupWorkflowTest(t, doCfg())
	mustRegisterAgent(t, deps.agents, "copper", "prole")
	id := mustCreateTicket(t, deps.issues, "in_progress")
	setEnv(t, "CT_AGENT_NAME", "copper")

	// "noop" action has a nil entry in the map.
	if err := agentDo(deps, []string{"noop", idStr(id)}); err != nil {
		t.Fatalf("agentDo noop: %v", err)
	}

	issue, _ := deps.issues.Get(id)
	if issue.Status != "in_progress" {
		t.Errorf("expected status=in_progress (unchanged), got %q", issue.Status)
	}
}

func TestAgentDo_missingCTAgentNameErrors(t *testing.T) {
	deps, _ := setupWorkflowTest(t, doCfg())
	unsetEnv(t, "CT_AGENT_NAME")

	err := agentDo(deps, []string{"submit", "1"})
	if err == nil {
		t.Fatal("expected error for missing CT_AGENT_NAME, got nil")
	}
}

func TestAgentDo_noWorkflowActionsErrors(t *testing.T) {
	deps, _ := setupWorkflowTest(t, proleCfg()) // prole has nil workflow
	mustRegisterAgent(t, deps.agents, "copper", "prole")
	id := mustCreateTicket(t, deps.issues, "in_progress")
	setEnv(t, "CT_AGENT_NAME", "copper")

	err := agentDo(deps, []string{"submit", idStr(id)})
	if err == nil {
		t.Fatal("expected error when no workflow actions configured, got nil")
	}
}

// idStr converts an int to string for test argument slices.
func idStr(id int) string {
	return strconv.Itoa(id)
}
