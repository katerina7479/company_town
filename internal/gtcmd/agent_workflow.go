package gtcmd

import (
	"fmt"
	"io"
	"os"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

// agentWorkflowDeps groups the injectable dependencies used by accept/release.
// Production code wires the real implementations; tests substitute fakes.
type agentWorkflowDeps struct {
	agents *repo.AgentRepo
	issues *repo.IssueRepo
	cfg    *config.Config
	stderr io.Writer
}

// agentAccept implements `gt agent accept <ticket-id>`.
func agentAccept(deps agentWorkflowDeps, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: gt agent accept <ticket-id>")
	}

	name := os.Getenv("CT_AGENT_NAME")
	if name == "" {
		return fmt.Errorf("gt agent accept requires CT_AGENT_NAME to be set")
	}

	id, err := parseTicketID(args[0])
	if err != nil {
		return err
	}

	// Verify the ticket exists.
	issue, err := deps.issues.Get(id)
	if err != nil {
		return fmt.Errorf("ticket %d not found: %w", id, err)
	}

	// Update agent row: working + current_issue.
	if err := deps.agents.SetCurrentIssue(name, &id); err != nil {
		return fmt.Errorf("updating agent row: %w", err)
	}

	// Apply the configured ticket transition if present.
	wf := roleWorkflow(deps.cfg, name)
	if wf != nil && wf.Accept != nil && wf.Accept.TicketTransition != nil {
		tt := wf.Accept.TicketTransition
		if issue.Status == tt.From {
			if err := deps.issues.UpdateStatus(id, tt.To); err != nil {
				return fmt.Errorf("transitioning ticket %d: %w", id, err)
			}
		} else {
			fmt.Fprintf(deps.stderr, "note: ticket %d is in status %q, not %q; agent row updated but ticket status left alone\n",
				id, issue.Status, tt.From)
		}
	}

	fmt.Printf("Agent %s accepted ticket %d\n", name, id)
	return nil
}

// agentRelease implements `gt agent release`.
func agentRelease(deps agentWorkflowDeps, args []string) error {
	name := os.Getenv("CT_AGENT_NAME")
	if name == "" {
		return fmt.Errorf("gt agent release requires CT_AGENT_NAME to be set")
	}

	// Read current issue before clearing.
	agent, err := deps.agents.Get(name)
	if err != nil {
		return fmt.Errorf("looking up agent %s: %w", name, err)
	}

	var currentIssueID *int
	if agent.CurrentIssue.Valid {
		id := int(agent.CurrentIssue.Int64)
		currentIssueID = &id
	}

	// Update agent row: idle, clear current_issue.
	if err := deps.agents.ClearCurrentIssue(name); err != nil {
		return fmt.Errorf("clearing agent row: %w", err)
	}

	// Apply the configured release ticket transition if there is one.
	wf := roleWorkflow(deps.cfg, name)
	if wf != nil && wf.Release != nil && wf.Release.TicketTransition != nil && currentIssueID != nil {
		tt := wf.Release.TicketTransition
		issue, err := deps.issues.Get(*currentIssueID)
		if err != nil {
			fmt.Fprintf(deps.stderr, "note: could not read ticket %d for release transition: %v\n", *currentIssueID, err)
		} else if issue.Status == tt.From {
			if err := deps.issues.UpdateStatus(*currentIssueID, tt.To); err != nil {
				return fmt.Errorf("transitioning ticket %d: %w", *currentIssueID, err)
			}
		} else {
			fmt.Fprintf(deps.stderr, "note: ticket %d is in status %q, not %q; agent row released but ticket status left alone\n",
				*currentIssueID, issue.Status, tt.From)
		}
	}

	fmt.Printf("Agent %s released\n", name)
	return nil
}

// roleWorkflow returns the WorkflowConfig for the named agent, consulting
// top-level roles first and then the artisan specialty map. Returns nil if
// the agent has no config entry.
func roleWorkflow(cfg *config.Config, name string) *config.WorkflowConfig {
	ac := roleAgentConfig(cfg, name)
	if ac == nil {
		return nil
	}
	return ac.Workflow
}

// roleAgentConfig returns the AgentConfig for the named agent. Top-level
// roles take precedence; artisan specialties are checked by specialty field.
// Returns nil if not found.
func roleAgentConfig(cfg *config.Config, name string) *config.AgentConfig {
	// Look up the agent's specialty from the agents table to match artisans.
	// We cannot query the DB here (no conn in scope), so we fall back to
	// name-matching against the artisan map, which is keyed by specialty.
	switch name {
	case "mayor":
		ac := cfg.Agents.Mayor
		return &ac
	case "architect":
		ac := cfg.Agents.Architect
		return &ac
	case "reviewer":
		ac := cfg.Agents.Reviewer
		return &ac
	}
	// Check artisan specialties by name — artisan agents are named after their specialty.
	if ac, ok := cfg.Agents.Artisan[name]; ok {
		return &ac
	}
	// Prole agents have names like "iron", "copper" — any unrecognised name
	// is treated as a prole.
	ac := cfg.Agents.Prole
	return &ac
}

// agentDo implements `gt agent do <action> <ticket-id>`.
// It runs a named workflow action from the caller's AgentConfig.Workflow.Actions map.
// Same semantics as accept/release: agent row is NOT changed (do is a ticket-only
// side-effect verb), the ticket transition fires iff the ticket's current status
// matches the configured From field.
func agentDo(deps agentWorkflowDeps, args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: gt agent do <action> <ticket-id>")
	}

	name := os.Getenv("CT_AGENT_NAME")
	if name == "" {
		return fmt.Errorf("gt agent do requires CT_AGENT_NAME to be set")
	}

	actionName := args[0]
	id, err := parseTicketID(args[1])
	if err != nil {
		return err
	}

	wf := roleWorkflow(deps.cfg, name)
	if wf == nil || wf.Actions == nil {
		return fmt.Errorf("no workflow actions configured for agent %q", name)
	}
	action, ok := wf.Actions[actionName]
	if !ok || action == nil {
		return fmt.Errorf("unknown workflow action %q for agent %q", actionName, name)
	}

	issue, err := deps.issues.Get(id)
	if err != nil {
		return fmt.Errorf("ticket %d not found: %w", id, err)
	}

	if action.TicketTransition == nil {
		// No-op transition: nothing to do.
		fmt.Printf("Action %q: no ticket transition configured\n", actionName)
		return nil
	}

	tt := action.TicketTransition
	if issue.Status != tt.From {
		fmt.Fprintf(deps.stderr, "note: ticket %d is in status %q, not %q; action %q skipped\n",
			id, issue.Status, tt.From, actionName)
		return nil
	}

	if err := deps.issues.UpdateStatus(id, tt.To); err != nil {
		return fmt.Errorf("applying action %q to ticket %d: %w", actionName, id, err)
	}

	fmt.Printf("Action %q applied: ticket %d → %s\n", actionName, id, tt.To)
	return nil
}

// openWorkflowDeps opens a DB connection and wires real dependencies for
// the accept/release verbs.
func openWorkflowDeps() (agentWorkflowDeps, func(), error) {
	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return agentWorkflowDeps{}, nil, err
	}
	deps := agentWorkflowDeps{
		agents: repo.NewAgentRepo(conn, nil),
		issues: repo.NewIssueRepo(conn, nil),
		cfg:    cfg,
		stderr: os.Stderr,
	}
	return deps, func() { conn.Close() }, nil
}
