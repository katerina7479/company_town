package commands

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	lipgloss "github.com/charmbracelet/lipgloss"

	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

// makeNode builds an IssueNode with the given status and optional ClosedAt.
func makeNode(status string, closedAt *time.Time, children ...*repo.IssueNode) *repo.IssueNode {
	issue := &repo.Issue{Status: status}
	if closedAt != nil {
		issue.ClosedAt = sql.NullTime{Time: *closedAt, Valid: true}
	}
	return &repo.IssueNode{Issue: issue, Children: children}
}

func TestFilterNode(t *testing.T) {
	now := time.Now()
	cutoff := now.Add(-4 * time.Hour)
	staleTime := now.Add(-5 * time.Hour) // before cutoff → stale
	freshTime := now.Add(-1 * time.Hour) // after cutoff → not stale

	t.Run("stale leaf is removed", func(t *testing.T) {
		node := makeNode("closed", &staleTime)
		if got := filterNode(node, cutoff); got != nil {
			t.Errorf("expected nil for stale leaf, got %+v", got)
		}
	})

	t.Run("non-stale leaf is kept", func(t *testing.T) {
		node := makeNode("open", nil)
		got := filterNode(node, cutoff)
		if got == nil {
			t.Fatal("expected non-nil for non-stale leaf")
		}
		if len(got.Children) != 0 {
			t.Errorf("expected no children, got %d", len(got.Children))
		}
	})

	t.Run("recently closed leaf is kept", func(t *testing.T) {
		node := makeNode("closed", &freshTime)
		got := filterNode(node, cutoff)
		if got == nil {
			t.Fatal("expected non-nil for recently closed leaf")
		}
	})

	t.Run("stale node with live child is kept", func(t *testing.T) {
		child := makeNode("open", nil)
		parent := makeNode("closed", &staleTime, child)
		got := filterNode(parent, cutoff)
		if got == nil {
			t.Fatal("expected stale parent with live child to be kept")
		}
		if len(got.Children) != 1 {
			t.Errorf("expected 1 surviving child, got %d", len(got.Children))
		}
	})

	t.Run("non-stale node with stale child has child removed", func(t *testing.T) {
		staleChild := makeNode("closed", &staleTime)
		parent := makeNode("open", nil, staleChild)
		got := filterNode(parent, cutoff)
		if got == nil {
			t.Fatal("expected non-stale parent to be kept")
		}
		if len(got.Children) != 0 {
			t.Errorf("expected stale child removed, got %d children", len(got.Children))
		}
	})

	t.Run("original node is not mutated", func(t *testing.T) {
		staleChild := makeNode("closed", &staleTime)
		liveChild := makeNode("open", nil)
		parent := makeNode("open", nil, staleChild, liveChild)
		_ = filterNode(parent, cutoff)
		if len(parent.Children) != 2 {
			t.Errorf("original node mutated: expected 2 children, got %d", len(parent.Children))
		}
	})
}

func TestFlattenTree(t *testing.T) {
	t.Run("empty input returns nil", func(t *testing.T) {
		result := flattenTree(nil, 0)
		if result != nil {
			t.Errorf("expected nil for empty input, got %v", result)
		}
	})

	t.Run("flat list returns same order at depth 0", func(t *testing.T) {
		n1 := makeNode("open", nil)
		n2 := makeNode("in_progress", nil)
		n3 := makeNode("closed", nil)
		result := flattenTree([]*repo.IssueNode{n1, n2, n3}, 0)
		if len(result) != 3 {
			t.Fatalf("expected 3 nodes, got %d", len(result))
		}
		for i, fn := range result {
			if fn.depth != 0 {
				t.Errorf("node %d: expected depth 0, got %d", i, fn.depth)
			}
		}
		if result[0].node != n1 || result[1].node != n2 || result[2].node != n3 {
			t.Error("flat list order not preserved")
		}
	})

	t.Run("nested tree returns pre-order depth-annotated slice", func(t *testing.T) {
		child1 := makeNode("open", nil)
		child2 := makeNode("open", nil)
		grandchild := makeNode("open", nil)
		// child2 has grandchild
		child2.Children = []*repo.IssueNode{grandchild}
		root := makeNode("open", nil, child1, child2)

		result := flattenTree([]*repo.IssueNode{root}, 0)
		// Expected pre-order: root(0), child1(1), child2(1), grandchild(2)
		if len(result) != 4 {
			t.Fatalf("expected 4 nodes, got %d", len(result))
		}
		expected := []struct {
			node  *repo.IssueNode
			depth int
		}{
			{root, 0},
			{child1, 1},
			{child2, 1},
			{grandchild, 2},
		}
		for i, e := range expected {
			if result[i].node != e.node {
				t.Errorf("index %d: wrong node", i)
			}
			if result[i].depth != e.depth {
				t.Errorf("index %d: expected depth %d, got %d", i, e.depth, result[i].depth)
			}
		}
	})
}

func TestFilterStaleClosedNodes(t *testing.T) {
	now := time.Now()
	cutoff := now.Add(-4 * time.Hour)
	staleTime := now.Add(-5 * time.Hour)

	t.Run("empty input returns nil", func(t *testing.T) {
		result := filterStaleClosedNodes(nil, cutoff)
		if result != nil {
			t.Errorf("expected nil for empty input, got %v", result)
		}
	})

	t.Run("all stale roots removed", func(t *testing.T) {
		roots := []*repo.IssueNode{
			makeNode("closed", &staleTime),
			makeNode("closed", &staleTime),
		}
		result := filterStaleClosedNodes(roots, cutoff)
		if len(result) != 0 {
			t.Errorf("expected 0 roots after filtering all stale, got %d", len(result))
		}
	})

	t.Run("live roots kept, stale roots removed", func(t *testing.T) {
		roots := []*repo.IssueNode{
			makeNode("open", nil),
			makeNode("closed", &staleTime),
			makeNode("in_progress", nil),
		}
		result := filterStaleClosedNodes(roots, cutoff)
		if len(result) != 2 {
			t.Errorf("expected 2 roots, got %d", len(result))
		}
	})
}

// newTestModel creates a dashboardModel with a real test DB and all injectable stubs.
func newTestModel(t *testing.T, killFn func(string) error, existsFn func(string) bool, sendKeysFn func(string, string) error, restartFn func(string, string) error) (*dashboardModel, *repo.AgentRepo) {
	t.Helper()
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	agents := repo.NewAgentRepo(conn, nil)
	m := &dashboardModel{
		conn:          conn,
		agents:        agents,
		issues:        repo.NewIssueRepo(conn, nil),
		killSession:   killFn,
		sessionExists: existsFn,
		sendKeys:      sendKeysFn,
		restartAgent:  restartFn,
		openPRFn:      func(int) error { return nil }, // no-op default
		sleepFn:       func(time.Duration) {},         // no-op in tests
		expanded:      make(map[int]bool),
	}
	return m, agents
}

// --- killAgentCmd tests ---

func TestKillAgentCmd_success(t *testing.T) {
	killed := ""
	m, agents := newTestModel(t,
		func(name string) error { killed = name; return nil },
		func(string) bool { return true },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)

	if err := agents.Register("obsidian", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}

	agent := &repo.Agent{Name: "obsidian", TmuxSession: sql.NullString{String: "ct-obsidian", Valid: true}}
	cmd := m.killAgentCmd(agent)
	msg := cmd().(actionResultMsg)

	if msg.err != nil {
		t.Fatalf("expected no error, got %v", msg.err)
	}
	if msg.text != "Killed obsidian" {
		t.Errorf("expected 'Killed obsidian', got %q", msg.text)
	}
	if killed != "ct-obsidian" {
		t.Errorf("expected killSession called with 'ct-obsidian', got %q", killed)
	}

	a, err := agents.Get("obsidian")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if a.Status != "dead" {
		t.Errorf("expected agent status=dead, got %q", a.Status)
	}
}

func TestKillAgentCmd_killSessionFails(t *testing.T) {
	m, agents := newTestModel(t,
		func(string) error { return fmt.Errorf("tmux error") },
		func(string) bool { return true },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	agents.Register("quartz", "prole", nil)

	agent := &repo.Agent{Name: "quartz", TmuxSession: sql.NullString{String: "ct-quartz", Valid: true}}
	cmd := m.killAgentCmd(agent)
	msg := cmd().(actionResultMsg)

	if msg.err == nil {
		t.Fatal("expected error when killSession fails")
	}
	if msg.text != "" {
		t.Errorf("expected no success text on failure, got %q", msg.text)
	}
}

func TestKillAgentCmd_partialFailureMessage(t *testing.T) {
	// killSession succeeds but the agent is not in the DB → UpdateStatus fails.
	m, _ := newTestModel(t,
		func(string) error { return nil }, // kill succeeds
		func(string) bool { return true },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	// Do NOT register the agent — UpdateStatus will fail.
	agent := &repo.Agent{Name: "ghost", TmuxSession: sql.NullString{String: "ct-ghost", Valid: true}}
	cmd := m.killAgentCmd(agent)
	msg := cmd().(actionResultMsg)

	if msg.err == nil {
		t.Fatal("expected error when DB update fails after successful kill")
	}
	errStr := msg.err.Error()
	// Error message must communicate that the session was killed but status update failed.
	if !containsAll(errStr, "ghost", "killed", "status") {
		t.Errorf("error message should mention both kill and status failure, got: %q", errStr)
	}
}

// --- stopAgentCmd tests ---

func TestStopAgentCmd_success(t *testing.T) {
	var sentTo, sentMsg string
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return true }, // session exists
		func(name, msg string) error { sentTo = name; sentMsg = msg; return nil },
		func(string, string) error { return nil },
	)

	agent := &repo.Agent{Name: "reviewer", TmuxSession: sql.NullString{String: "ct-reviewer", Valid: true}}
	cmd := m.stopAgentCmd(agent)
	result := cmd().(actionResultMsg)

	if result.err != nil {
		t.Fatalf("expected no error, got %v", result.err)
	}
	if result.text != "Sent stop signal to reviewer" {
		t.Errorf("unexpected success text: %q", result.text)
	}
	if sentTo != "ct-reviewer" {
		t.Errorf("expected sendKeys to 'ct-reviewer', got %q", sentTo)
	}
	if sentMsg == "" {
		t.Error("expected non-empty stop message sent to agent")
	}
}

func TestStopAgentCmd_noSessionError(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false }, // session does not exist
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)

	agent := &repo.Agent{Name: "reviewer", TmuxSession: sql.NullString{String: "ct-reviewer", Valid: true}}
	cmd := m.stopAgentCmd(agent)
	result := cmd().(actionResultMsg)

	if result.err == nil {
		t.Fatal("expected error when session does not exist")
	}
	if result.text != "" {
		t.Errorf("expected no success text, got %q", result.text)
	}
}

func TestStopAgentCmd_sendKeysFails(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return true },
		func(string, string) error { return fmt.Errorf("tmux send error") },
		func(string, string) error { return nil },
	)

	agent := &repo.Agent{Name: "reviewer", TmuxSession: sql.NullString{String: "ct-reviewer", Valid: true}}
	cmd := m.stopAgentCmd(agent)
	result := cmd().(actionResultMsg)

	if result.err == nil {
		t.Fatal("expected error when sendKeys fails")
	}
}

// --- statusMsg cleared on data refresh ---

func TestDataMsg_clearsStatusMsg(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	m.statusMsg = "previous status"

	updated, _ := m.Update(dataMsg{agents: nil, roots: nil})
	dm := updated.(dashboardModel)
	if dm.statusMsg != "" {
		t.Errorf("expected statusMsg cleared on dataMsg, got %q", dm.statusMsg)
	}
}

// --- restartAgentCmd tests ---

func TestRestartAgentCmd_success(t *testing.T) {
	var killed, restartedName, restartedType string
	m, agents := newTestModel(t,
		func(name string) error { killed = name; return nil },
		func(string) bool { return true },
		func(string, string) error { return nil },
		func(name, agentType string) error { restartedName = name; restartedType = agentType; return nil },
	)

	agents.Register("copper", "prole", nil)
	agent := &repo.Agent{Name: "copper", Type: "prole", TmuxSession: sql.NullString{String: "ct-copper", Valid: true}}
	cmd := m.restartAgentCmd(agent)
	// restartAgentCmd returns dataMsg (a DB fetch) on success, not actionResultMsg.
	msg := cmd().(dataMsg)

	if msg.err != nil {
		t.Fatalf("expected no error in fetched data, got %v", msg.err)
	}
	if killed != "ct-copper" {
		t.Errorf("expected killSession called with 'ct-copper', got %q", killed)
	}
	if restartedName != "copper" || restartedType != "prole" {
		t.Errorf("expected restartAgent('copper', 'prole'), got (%q, %q)", restartedName, restartedType)
	}
}

func TestRestartAgentCmd_killFails(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return fmt.Errorf("tmux error") },
		func(string) bool { return true },
		func(string, string) error { return nil },
		func(string, string) error {
			t.Error("restartAgent should not be called after kill failure")
			return nil
		},
	)

	agent := &repo.Agent{Name: "copper", Type: "prole", TmuxSession: sql.NullString{String: "ct-copper", Valid: true}}
	cmd := m.restartAgentCmd(agent)
	msg := cmd().(actionResultMsg)

	if msg.err == nil {
		t.Fatal("expected error when kill fails")
	}
}

func TestRestartAgentCmd_restartFails(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return true },
		func(string, string) error { return nil },
		func(string, string) error { return fmt.Errorf("launch error") },
	)

	agent := &repo.Agent{Name: "copper", Type: "prole", TmuxSession: sql.NullString{String: "ct-copper", Valid: true}}
	cmd := m.restartAgentCmd(agent)
	msg := cmd().(actionResultMsg)

	if msg.err == nil {
		t.Fatal("expected error when restart fails")
	}
}

// --- nudge / input mode tests ---

func makeModelWithAgents(t *testing.T, sessionLive bool) (*dashboardModel, *[]struct{ session, msg string }) {
	t.Helper()
	sent := &[]struct{ session, msg string }{}
	m, agents := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return sessionLive },
		func(name, msg string) error {
			*sent = append(*sent, struct{ session, msg string }{name, msg})
			return nil
		},
		func(string, string) error { return nil },
	)
	agents.Register("copper", "prole", nil)
	// Use the canonical prole session name (ct-prole-<name>) that the system
	// actually records at prole creation time — the bug was using session.SessionName
	// which produced ct-copper instead.
	m.data.agents = []*repo.Agent{{
		Name:        "copper",
		Type:        "prole",
		TmuxSession: sql.NullString{String: "ct-prole-copper", Valid: true},
	}}
	m.focusedPanel = 0
	return m, sent
}

func TestNudge_entersInputMode(t *testing.T) {
	m, _ := makeModelWithAgents(t, true)

	updated, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("n")})
	dm := updated.(dashboardModel)

	if !dm.inputMode {
		t.Fatal("expected inputMode=true after pressing n")
	}
	if dm.inputAction != "nudge" {
		t.Errorf("expected inputAction=nudge, got %q", dm.inputAction)
	}
	if dm.inputTarget != "copper" {
		t.Errorf("expected inputTarget=copper, got %q", dm.inputTarget)
	}
	if dm.inputBuffer != "" {
		t.Errorf("expected empty inputBuffer, got %q", dm.inputBuffer)
	}
}

func TestNudge_typeAndEnterCallsSendKeys(t *testing.T) {
	m, sent := makeModelWithAgents(t, true)

	// Enter input mode
	updated, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("n")})
	dm := updated.(dashboardModel)

	// Type "hello"
	for _, ch := range "hello" {
		upd, _ := dm.Update(tea.KeyMsg{Type: -1, Runes: []rune{ch}})
		dm = upd.(dashboardModel)
	}
	if dm.inputBuffer != "hello" {
		t.Fatalf("expected inputBuffer='hello', got %q", dm.inputBuffer)
	}

	// Press Enter to send
	upd, _ := dm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	dm = upd.(dashboardModel)

	if dm.inputMode {
		t.Error("expected inputMode=false after Enter")
	}
	if dm.inputBuffer != "" {
		t.Error("expected inputBuffer cleared after Enter")
	}
	if len(*sent) != 1 {
		t.Fatalf("expected 1 sendKeys call, got %d", len(*sent))
	}
	if (*sent)[0].session != "ct-prole-copper" {
		t.Errorf("expected sendKeys to 'ct-prole-copper', got %q", (*sent)[0].session)
	}
	if (*sent)[0].msg != "hello" {
		t.Errorf("expected message 'hello', got %q", (*sent)[0].msg)
	}
}

func TestNudge_escapeClears(t *testing.T) {
	m, sent := makeModelWithAgents(t, true)

	// Enter input mode
	updated, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("n")})
	dm := updated.(dashboardModel)

	// Type something, then Escape
	upd, _ := dm.Update(tea.KeyMsg{Type: -1, Runes: []rune("a")})
	dm = upd.(dashboardModel)

	upd, _ = dm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	dm = upd.(dashboardModel)

	if dm.inputMode {
		t.Error("expected inputMode=false after Escape")
	}
	if dm.inputBuffer != "" {
		t.Error("expected inputBuffer cleared after Escape")
	}
	if len(*sent) != 0 {
		t.Errorf("expected no sendKeys calls on cancel, got %d", len(*sent))
	}
}

func TestNudge_backspaceDeletesChar(t *testing.T) {
	m, _ := makeModelWithAgents(t, true)

	// Enter input mode and type
	updated, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("n")})
	dm := updated.(dashboardModel)

	for _, ch := range "hi" {
		upd, _ := dm.Update(tea.KeyMsg{Type: -1, Runes: []rune{ch}})
		dm = upd.(dashboardModel)
	}
	// Backspace once
	upd, _ := dm.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	dm = upd.(dashboardModel)

	if dm.inputBuffer != "h" {
		t.Errorf("expected inputBuffer='h' after backspace, got %q", dm.inputBuffer)
	}
}

func TestNudge_inputModeIsolatesNavKeys(t *testing.T) {
	m, _ := makeModelWithAgents(t, true)
	m.data.agents = append(m.data.agents, &repo.Agent{Name: "zinc", Type: "prole"})

	// Enter input mode
	updated, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("n")})
	dm := updated.(dashboardModel)

	// Press "j" — should be captured as text, NOT move cursor
	cursorBefore := dm.agentCursor
	upd, _ := dm.Update(tea.KeyMsg{Type: -1, Runes: []rune("j")})
	dm = upd.(dashboardModel)

	if dm.agentCursor != cursorBefore {
		t.Errorf("j should be text in input mode, not navigation (cursor moved from %d to %d)",
			cursorBefore, dm.agentCursor)
	}
	if dm.inputBuffer != "j" {
		t.Errorf("expected inputBuffer='j', got %q", dm.inputBuffer)
	}
}

func TestNudge_noopOnDeadAgent(t *testing.T) {
	// Session name recorded but tmux session is not running — pressing n should
	// NOT enter input mode and must set a status message (never silent).
	m, _ := makeModelWithAgents(t, false)

	updated, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("n")})
	dm := updated.(dashboardModel)

	if dm.inputMode {
		t.Error("expected inputMode=false for agent with no live session")
	}
	if dm.statusMsg == "" {
		t.Error("expected non-empty statusMsg when session is dead")
	}
}

func TestNudge_emptyBufferEnterDoesNotSendKeys(t *testing.T) {
	m, sent := makeModelWithAgents(t, true)

	// Enter input mode, press Enter immediately without typing
	updated, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("n")})
	dm := updated.(dashboardModel)

	upd, _ := dm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	dm = upd.(dashboardModel)

	if len(*sent) != 0 {
		t.Errorf("expected no sendKeys on empty buffer, got %d", len(*sent))
	}
	if dm.inputMode {
		t.Error("expected inputMode=false after Enter")
	}
}

func TestNudge_noSessionRecorded(t *testing.T) {
	// Agent has no tmux_session in DB — pressing n must set a status message, never silently swallow.
	sent := &[]struct{ session, msg string }{}
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return true }, // session would be alive, but name isn't recorded
		func(name, msg string) error {
			*sent = append(*sent, struct{ session, msg string }{name, msg})
			return nil
		},
		func(string, string) error { return nil },
	)
	// Agent with no TmuxSession set.
	m.data.agents = []*repo.Agent{{Name: "copper", Type: "prole"}}
	m.focusedPanel = 0

	updated, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("n")})
	dm := updated.(dashboardModel)

	if dm.inputMode {
		t.Error("expected inputMode=false when no session recorded")
	}
	if dm.statusMsg == "" {
		t.Error("expected non-empty statusMsg when no session recorded")
	}
	if len(*sent) != 0 {
		t.Errorf("expected no sendKeys calls, got %d", len(*sent))
	}
}

func TestNudge_sendKeysFails_showsError(t *testing.T) {
	// sendKeys returns an error — status bar must show the error, never silent.
	sent := &[]struct{ session, msg string }{}
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return true },
		func(name, msg string) error {
			*sent = append(*sent, struct{ session, msg string }{name, msg})
			return fmt.Errorf("tmux write failed")
		},
		func(string, string) error { return nil },
	)
	m.data.agents = []*repo.Agent{{
		Name:        "copper",
		Type:        "prole",
		TmuxSession: sql.NullString{String: "ct-prole-copper", Valid: true},
	}}
	m.focusedPanel = 0

	updated, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("n")})
	dm := updated.(dashboardModel)

	for _, ch := range "hello" {
		upd, _ := dm.Update(tea.KeyMsg{Type: -1, Runes: []rune{ch}})
		dm = upd.(dashboardModel)
	}
	upd, _ := dm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	dm = upd.(dashboardModel)

	if dm.statusMsg == "" {
		t.Error("expected non-empty statusMsg on sendKeys error")
	}
	if !strings.Contains(dm.statusMsg, "nudge failed") {
		t.Errorf("expected statusMsg to contain 'nudge failed', got %q", dm.statusMsg)
	}
}

func TestNudge_successSetsStatusMsg(t *testing.T) {
	// Successful nudge must set statusMsg = "nudged <name>".
	m, _ := makeModelWithAgents(t, true)

	updated, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("n")})
	dm := updated.(dashboardModel)

	for _, ch := range "wake up" {
		upd, _ := dm.Update(tea.KeyMsg{Type: -1, Runes: []rune{ch}})
		dm = upd.(dashboardModel)
	}
	upd, _ := dm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	dm = upd.(dashboardModel)

	if dm.statusMsg != "nudged copper" {
		t.Errorf("expected statusMsg='nudged copper', got %q", dm.statusMsg)
	}
}

func TestNudge_wrongPanelNoInputMode(t *testing.T) {
	// Pressing n while focused on the tickets panel (panel 1) must not open input mode.
	m, _ := makeModelWithAgents(t, true)
	m.focusedPanel = 1

	updated, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("n")})
	dm := updated.(dashboardModel)

	if dm.inputMode {
		t.Error("expected inputMode=false when ticket panel is focused")
	}
}

// --- ticket action tests (NC-11) ---

func makeModelWithTickets(t *testing.T) (*dashboardModel, *repo.IssueRepo) {
	t.Helper()
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	m.ticketPrefix = "ct"
	return m, m.issues
}

func TestStatusChange_invalidStatusRejected(t *testing.T) {
	m, issues := makeModelWithTickets(t)

	id, err := issues.Create("Test ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := issues.UpdateStatus(id, "open"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	node := &repo.IssueNode{Issue: &repo.Issue{ID: id, Status: "open"}}
	m.data.roots = []*repo.IssueNode{node}
	m.focusedPanel = 1

	updated, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("c")})
	dm := updated.(dashboardModel)
	if !dm.inputMode || dm.inputAction != "status" {
		t.Fatal("expected inputMode=true with action=status after pressing c")
	}

	for _, ch := range "cosed" {
		upd, _ := dm.Update(tea.KeyMsg{Type: -1, Runes: []rune{ch}})
		dm = upd.(dashboardModel)
	}
	upd, _ := dm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	dm = upd.(dashboardModel)

	if dm.statusMsg == "" {
		t.Error("expected statusMsg to be set for invalid status")
	}

	issue, err := issues.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if issue.Status != "open" {
		t.Errorf("expected status to remain 'open', got %q", issue.Status)
	}
}

func TestStatusChange_validStatusAccepted(t *testing.T) {
	m, issues := makeModelWithTickets(t)

	id, err := issues.Create("Test ticket", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := issues.UpdateStatus(id, "open"); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}

	node := &repo.IssueNode{Issue: &repo.Issue{ID: id, Status: "open"}}
	m.data.roots = []*repo.IssueNode{node}
	m.focusedPanel = 1

	updated, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("c")})
	dm := updated.(dashboardModel)

	for _, ch := range "closed" {
		upd, _ := dm.Update(tea.KeyMsg{Type: -1, Runes: []rune{ch}})
		dm = upd.(dashboardModel)
	}
	upd, _ := dm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	dm = upd.(dashboardModel)

	if dm.inputMode {
		t.Error("expected inputMode=false after Enter")
	}

	issue, err := issues.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if issue.Status != "closed" {
		t.Errorf("expected status='closed', got %q", issue.Status)
	}
}

// --- global action tests (NC-12) ---

func TestShowClosed_toggleOnF(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	if m.showClosed {
		t.Fatal("expected showClosed=false initially")
	}

	updated, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("f")})
	dm := updated.(dashboardModel)
	if !dm.showClosed {
		t.Error("expected showClosed=true after pressing f")
	}

	updated, _ = dm.Update(tea.KeyMsg{Type: -1, Runes: []rune("f")})
	dm = updated.(dashboardModel)
	if dm.showClosed {
		t.Error("expected showClosed=false after pressing f again")
	}
}

func TestShowClosed_zeroTimeCutoffIncludesAll(t *testing.T) {
	// With showClosed=true, flatTickets uses a zero-time cutoff so all closed
	// nodes are kept regardless of age.
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)

	staleTime := time.Now().Add(-48 * time.Hour)
	staleNode := makeNode("closed", &staleTime)
	m.data.roots = []*repo.IssueNode{staleNode}

	m.showClosed = false
	if len(m.flatTickets()) != 0 {
		t.Errorf("expected stale closed node hidden when showClosed=false")
	}

	m.showClosed = true
	if len(m.flatTickets()) != 1 {
		t.Errorf("expected stale closed node shown when showClosed=true")
	}
}

func TestCreateTicket_createsInDB(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	issues := m.issues

	updated, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("C")})
	dm := updated.(dashboardModel)
	if !dm.inputMode || dm.inputAction != "create_ticket" {
		t.Fatal("expected inputMode=true with action=create_ticket after pressing C")
	}

	for _, ch := range "My new ticket" {
		upd, _ := dm.Update(tea.KeyMsg{Type: -1, Runes: []rune{ch}})
		dm = upd.(dashboardModel)
	}
	upd, _ := dm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	dm = upd.(dashboardModel)

	if dm.inputMode {
		t.Error("expected inputMode=false after Enter")
	}

	drafts, err := issues.List("draft")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	found := false
	for _, iss := range drafts {
		if iss.Title == "My new ticket" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected new ticket 'My new ticket' to exist in DB")
	}
}

func TestCreateTicket_emptyTitleNoOp(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	issues := m.issues

	before, _ := issues.List("draft")
	beforeCount := len(before)

	updated, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("C")})
	dm := updated.(dashboardModel)
	upd, _ := dm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = upd

	after, _ := issues.List("draft")
	if len(after) != beforeCount {
		t.Errorf("expected no new ticket on empty title, count went from %d to %d", beforeCount, len(after))
	}
}

func containsAll(s string, substrings ...string) bool {
	for _, sub := range substrings {
		found := false
		for i := 0; i <= len(s)-len(sub); i++ {
			if s[i:i+len(sub)] == sub {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// --- expand/collapse toggle tests (NC-10) ---

// makeTicketNode builds an IssueNode with a given ID for expand/collapse tests.
func makeTicketNode(id int) *repo.IssueNode {
	return &repo.IssueNode{Issue: &repo.Issue{ID: id, Status: "open"}}
}

func TestExpand_enterTogglesExpanded(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	node := makeTicketNode(42)
	m.data.roots = []*repo.IssueNode{node}
	m.focusedPanel = 1 // ticket panel

	// First Enter: expand ticket 42.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	dm := updated.(dashboardModel)

	if !dm.expanded[42] {
		t.Error("expected expanded[42]=true after first Enter")
	}

	// Second Enter: collapse ticket 42.
	updated, _ = dm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	dm = updated.(dashboardModel)

	if dm.expanded[42] {
		t.Error("expected expanded[42]=false after second Enter")
	}
}

func TestExpand_enterOnAgentPanelDoesNotToggle(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	node := makeTicketNode(7)
	m.data.roots = []*repo.IssueNode{node}
	m.focusedPanel = 0 // agent panel — Enter should not expand tickets

	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	dm := updated.(dashboardModel)

	if dm.expanded[7] {
		t.Error("expected expanded[7]=false when Enter pressed on agent panel")
	}
}

func TestExpand_enterNoopWhenNoTickets(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	m.data.roots = nil
	m.focusedPanel = 1

	// Should not panic or crash.
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_ = updated
}

// --- renderTicketDetails tests (NC-10) ---

func makeDetailNode(description, assignee, branch string, prNumber int64) *repo.IssueNode {
	issue := &repo.Issue{
		ID:     1,
		Status: "open",
	}
	if description != "" {
		issue.Description = sql.NullString{String: description, Valid: true}
	}
	if assignee != "" {
		issue.Assignee = sql.NullString{String: assignee, Valid: true}
	}
	if branch != "" {
		issue.Branch = sql.NullString{String: branch, Valid: true}
	}
	if prNumber > 0 {
		issue.PRNumber = sql.NullInt64{Int64: prNumber, Valid: true}
	}
	return &repo.IssueNode{Issue: issue}
}

func TestRenderTicketDetails_descriptionShown(t *testing.T) {
	node := makeDetailNode("Some description text", "", "", 0)
	out := renderTicketDetails(node, 0, 80)
	if !containsAll(out, "Some description text") {
		t.Errorf("expected description in output, got:\n%s", out)
	}
}

func TestRenderTicketDetails_noDescriptionOmitted(t *testing.T) {
	node := makeDetailNode("", "alice", "", 0)
	out := renderTicketDetails(node, 0, 80)
	// There should be an assignee line but no empty description line.
	if !containsAll(out, "assignee:") {
		t.Errorf("expected assignee line in output, got:\n%s", out)
	}
}

func TestRenderTicketDetails_assigneeShown(t *testing.T) {
	node := makeDetailNode("", "copper", "", 0)
	out := renderTicketDetails(node, 0, 80)
	if !containsAll(out, "assignee:", "copper") {
		t.Errorf("expected assignee in output, got:\n%s", out)
	}
}

func TestRenderTicketDetails_noAssigneeOmitted(t *testing.T) {
	node := makeDetailNode("", "", "", 0)
	out := renderTicketDetails(node, 0, 80)
	if containsAll(out, "assignee:") {
		t.Errorf("expected no assignee line when assignee is null, got:\n%s", out)
	}
}

func TestRenderTicketDetails_prNumberShown(t *testing.T) {
	node := makeDetailNode("", "", "", 42)
	out := renderTicketDetails(node, 0, 80)
	if !containsAll(out, "PR:", "#42") {
		t.Errorf("expected PR number in output, got:\n%s", out)
	}
}

func TestRenderTicketDetails_noPROmitted(t *testing.T) {
	node := makeDetailNode("", "", "", 0)
	out := renderTicketDetails(node, 0, 80)
	if containsAll(out, "PR:") {
		t.Errorf("expected no PR line when PR is null, got:\n%s", out)
	}
}

func TestRenderTicketDetails_branchShown(t *testing.T) {
	node := makeDetailNode("", "", "prole/fig/10", 0)
	out := renderTicketDetails(node, 0, 80)
	if !containsAll(out, "branch:", "prole/fig/10") {
		t.Errorf("expected branch in output, got:\n%s", out)
	}
}

func TestRenderTicketDetails_timestampsAlwaysPresent(t *testing.T) {
	node := makeDetailNode("", "", "", 0)
	out := renderTicketDetails(node, 0, 80)
	if !containsAll(out, "created:", "updated:") {
		t.Errorf("expected timestamps in output, got:\n%s", out)
	}
}

func TestRenderTicketDetails_depthAffectsIndentation(t *testing.T) {
	node := makeDetailNode("", "alice", "", 0)
	depth0 := renderTicketDetails(node, 0, 80)
	depth1 := renderTicketDetails(node, 1, 80)
	// Depth 1 should have more leading spaces than depth 0.
	if len(depth1) <= len(depth0) {
		t.Errorf("expected deeper indent at depth=1 to produce longer output")
	}
}

// --- wordWrap tests (NC-10) ---

func TestWordWrap_zeroWidthReturnsUnchanged(t *testing.T) {
	s := "hello world this is a long string"
	if got := wordWrap(s, 0); got != s {
		t.Errorf("expected unchanged string for width=0, got %q", got)
	}
}

func TestWordWrap_shortStringUnchanged(t *testing.T) {
	s := "hello"
	if got := wordWrap(s, 20); got != s {
		t.Errorf("expected unchanged short string, got %q", got)
	}
}

func TestWordWrap_wrapsAtSpaceBelowWidth(t *testing.T) {
	// "hello world" with width=8 should wrap between "hello" and "world"
	got := wordWrap("hello world", 8)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), got)
	}
	if lines[0] != "hello" || lines[1] != "world" {
		t.Errorf("unexpected wrap result: %q", got)
	}
}

func TestWordWrap_longWordWithoutSpaceCutAtWidth(t *testing.T) {
	// "abcdefghij" with width=5 should hard-cut at 5
	got := wordWrap("abcdefghij", 5)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d: %q", len(lines), got)
	}
	if lines[0] != "abcde" {
		t.Errorf("expected first line 'abcde', got %q", lines[0])
	}
}

func TestWordWrap_multipleInputLinesPreserved(t *testing.T) {
	// Each input line should be wrapped independently.
	got := wordWrap("line one\nline two", 20)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines from multi-line input, got %d: %q", len(lines), got)
	}
	if lines[0] != "line one" || lines[1] != "line two" {
		t.Errorf("unexpected multiline result: %q", got)
	}
}

// --- daemon tick file tests (NC-57) ---

func TestFetch_populatesLastDaemonTickFromFile(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)

	dir := t.TempDir()
	tickFile := filepath.Join(dir, "daemon-tick")
	tickTime := time.Now().UTC().Truncate(time.Millisecond)
	if err := os.WriteFile(tickFile, []byte(tickTime.Format(time.RFC3339Nano)), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m.tickFile = tickFile

	msg := m.fetch().(dataMsg)
	if msg.lastDaemonTick == nil {
		t.Fatal("expected lastDaemonTick to be populated from tick file")
	}
	if !msg.lastDaemonTick.Equal(tickTime) {
		t.Errorf("expected tick time %v, got %v", tickTime, *msg.lastDaemonTick)
	}
}

func TestFetch_lastDaemonTickNilWhenNoFile(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	m.tickFile = filepath.Join(t.TempDir(), "nonexistent-daemon-tick")

	msg := m.fetch().(dataMsg)
	if msg.lastDaemonTick != nil {
		t.Errorf("expected lastDaemonTick=nil when file missing, got %v", msg.lastDaemonTick)
	}
}

func TestFetch_lastDaemonTickNilWhenTickFileEmpty(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)

	dir := t.TempDir()
	tickFile := filepath.Join(dir, "daemon-tick")
	if err := os.WriteFile(tickFile, []byte(""), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	m.tickFile = tickFile

	msg := m.fetch().(dataMsg)
	if msg.lastDaemonTick != nil {
		t.Errorf("expected lastDaemonTick=nil for empty tick file, got %v", msg.lastDaemonTick)
	}
}

func TestFetch_lastDaemonTickNilWhenTickFileDisabled(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	m.tickFile = "" // disabled

	msg := m.fetch().(dataMsg)
	if msg.lastDaemonTick != nil {
		t.Errorf("expected lastDaemonTick=nil when tickFile disabled, got %v", msg.lastDaemonTick)
	}
}

// --- renderDaemonLine state tests (NC-57 reviewer deviations 1+3) ---

func TestRenderDaemonLine_freshShowsCheck(t *testing.T) {
	m := dashboardModel{
		pollingInterval: 10 * time.Second,
		data: dashboardData{
			lastDaemonTick: ptrTime(time.Now().Add(-3 * time.Second)),
		},
	}
	out := m.renderDaemonLine()
	if !strings.Contains(out, "✓") {
		t.Errorf("fresh daemon should render with ✓, got %q", out)
	}
	if strings.Contains(out, "⚠") || strings.Contains(out, "✗") {
		t.Errorf("fresh daemon should not render warning/missing markers, got %q", out)
	}
	if !strings.Contains(out, "daemon:") {
		t.Errorf("expected 'daemon:' label, got %q", out)
	}
}

func TestRenderDaemonLine_staleShowsWarning(t *testing.T) {
	m := dashboardModel{
		pollingInterval: 10 * time.Second, // stale threshold = 30s floor
		data: dashboardData{
			lastDaemonTick: ptrTime(time.Now().Add(-2 * time.Minute)),
		},
	}
	out := m.renderDaemonLine()
	if !strings.Contains(out, "⚠") {
		t.Errorf("stale daemon should render with ⚠, got %q", out)
	}
	if strings.Contains(out, "✓") {
		t.Errorf("stale daemon should not render ✓, got %q", out)
	}
	if !strings.Contains(out, "expected every") {
		t.Errorf("stale daemon should include the interval hint, got %q", out)
	}
}

func TestRenderDaemonLine_missingShowsCross(t *testing.T) {
	m := dashboardModel{
		pollingInterval: 10 * time.Second,
		data: dashboardData{
			lastDaemonTick: nil,
		},
	}
	out := m.renderDaemonLine()
	if !strings.Contains(out, "✗") {
		t.Errorf("missing daemon should render with ✗, got %q", out)
	}
	if !strings.Contains(out, "not running") {
		t.Errorf("missing daemon should say 'not running', got %q", out)
	}
	if strings.Contains(out, "✓") || strings.Contains(out, "⚠") {
		t.Errorf("missing daemon should not render fresh/stale markers, got %q", out)
	}
}

// Stale threshold floor: a 1-second polling interval still yields a 30s
// threshold, so a 20s-old heartbeat should render fresh (not stale).
func TestRenderDaemonLine_staleFloorIs30Seconds(t *testing.T) {
	m := dashboardModel{
		pollingInterval: 1 * time.Second,
		data: dashboardData{
			lastDaemonTick: ptrTime(time.Now().Add(-20 * time.Second)),
		},
	}
	out := m.renderDaemonLine()
	if !strings.Contains(out, "✓") {
		t.Errorf("age 20s with 1s interval should be fresh (30s floor), got %q", out)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

// --- renderIssueRow / selected-row wrapping tests (NC-45) ---

// makeFullNode builds an IssueNode with all fields populated, for row-width tests.
func makeFullNode(status, title string) *repo.IssueNode {
	return &repo.IssueNode{Issue: &repo.Issue{
		ID:       99,
		Status:   status,
		Title:    title,
		PRNumber: sql.NullInt64{Int64: 42, Valid: true},
		Priority: sql.NullString{String: "P1", Valid: true},
	}}
}

func TestRenderIssueRow_fitsContentWidth(t *testing.T) {
	// Verify that renderIssueRow never produces a row visually wider than the
	// given content width, regardless of status length or title length.
	contentWidth := 100
	statuses := []string{"open", "in_progress", "in_review", "repairing", "closed"}
	longTitle := strings.Repeat("A", 200) // much longer than any realistic width

	for _, status := range statuses {
		node := makeFullNode(status, longTitle)
		row := renderIssueRow(node, 0, contentWidth)
		// lipgloss.Width strips ANSI codes and returns the visual cell width.
		got := lipgloss.Width(row)
		if got > contentWidth {
			t.Errorf("status=%q: row visual width %d exceeds content width %d",
				status, got, contentWidth)
		}
	}
}

func TestRenderIssueRow_selectedRowDoesNotWrap(t *testing.T) {
	// Regression test for NC-45: selectedStyle.Width(innerWidth) must not cause
	// the selected row to wrap to a second line inside the panel content area.
	contentWidth := 100
	node := makeFullNode("open", strings.Repeat("B", 200))

	row := renderIssueRow(node, 0, contentWidth)
	rendered := selectedStyle.Width(contentWidth).Render(row)

	// If wrapping occurs, lipgloss.Height > 1.
	if h := lipgloss.Height(rendered); h != 1 {
		t.Errorf("selected row wrapped to %d lines (expected 1); content width=%d",
			h, contentWidth)
	}
}

func TestRenderIssueRow_selectedRowDoesNotWrapShortStatus(t *testing.T) {
	// "open" (4 chars) is the shortest status; it maximises the padding the
	// selectedStyle adds, which was the source of the original wrap.
	contentWidth := 80
	node := makeFullNode("open", strings.Repeat("C", 200))

	row := renderIssueRow(node, 0, contentWidth)
	rendered := selectedStyle.Width(contentWidth).Render(row)

	if h := lipgloss.Height(rendered); h != 1 {
		t.Errorf("selected 'open' row wrapped to %d lines (expected 1); content width=%d",
			h, contentWidth)
	}
}

// --- NC-47: ticket type indicator ---

func TestTypeCell_taskIsBlank(t *testing.T) {
	cell := typeCell("task")
	if cell != " " {
		t.Errorf("typeCell('task') should return a single space, got %q", cell)
	}
}

func TestTypeCell_unknownEmptyIsBlank(t *testing.T) {
	cell := typeCell("")
	if cell != " " {
		t.Errorf("typeCell('') should return a single space, got %q", cell)
	}
}

func TestTypeCell_unknownStringIsBlank(t *testing.T) {
	// Future/unknown types must silently return blank — not panic, not print garbage.
	cell := typeCell("research")
	if cell != " " {
		t.Errorf("typeCell('research') should return a single space, got %q", cell)
	}
}

func TestTypeCell_epicIsE(t *testing.T) {
	cell := typeCell("epic")
	// Strip ANSI codes — the visible content should end with a space and contain "E".
	if !strings.Contains(cell, "E") {
		t.Errorf("typeCell('epic') should contain 'E', got %q", cell)
	}
}

func TestTypeCell_bugIsB(t *testing.T) {
	cell := typeCell("bug")
	if !strings.Contains(cell, "B") {
		t.Errorf("typeCell('bug') should contain 'B', got %q", cell)
	}
}

func TestTypeCell_refactorIsR(t *testing.T) {
	cell := typeCell("refactor")
	if !strings.Contains(cell, "R") {
		t.Errorf("typeCell('refactor') should contain 'R', got %q", cell)
	}
}

func TestRenderIssueRow_typeIndicatorPresent(t *testing.T) {
	// A bug ticket row must contain the "B" type indicator.
	node := &repo.IssueNode{
		Issue: &repo.Issue{
			ID:        99,
			IssueType: "bug",
			Status:    "open",
			Title:     "Something broken",
		},
	}
	row := renderIssueRow(node, 0, 120)
	if !strings.Contains(row, "B") {
		t.Errorf("renderIssueRow for bug ticket should contain 'B' type indicator, got: %q", row)
	}
}

func TestRenderIssueRow_taskTypeIndicatorAbsent(t *testing.T) {
	// A task ticket row must NOT contain a type letter (type cell is blank).
	// We verify by checking the row contains the title but no stray type letter
	// adjacent to the id/status region. We do this by checking typeCell directly.
	cell := typeCell("task")
	if strings.ContainsAny(cell, "EBRTS") {
		t.Errorf("typeCell('task') should not contain a type letter, got %q", cell)
	}
}

func TestRenderIssueRow_childEpicShowsChildBulletAndTypeLetter(t *testing.T) {
	// An epic at depth=1 must show both the child bullet (◦) and the type letter (E).
	// This pins the column position: if type is misplaced, ◦ and E may not both appear.
	node := &repo.IssueNode{
		Issue: &repo.Issue{
			ID:        7,
			IssueType: "epic",
			Status:    "open",
			Title:     "Child epic",
		},
	}
	row := renderIssueRow(node, 1, 120)
	if !strings.Contains(row, "◦") {
		t.Errorf("renderIssueRow for depth=1 should contain child bullet ◦, got: %q", row)
	}
	if !strings.Contains(row, "E") {
		t.Errorf("renderIssueRow for epic at depth=1 should contain type letter E, got: %q", row)
	}
}

// --- priorityCell tests ---

func TestPriorityCell_nullIsSpaces(t *testing.T) {
	got := priorityCell(sql.NullString{})
	if got != "     " {
		t.Errorf("expected 5 spaces for NULL priority, got %q", got)
	}
	if len(got) != 5 {
		t.Errorf("expected len 5, got %d", len(got))
	}
}

func TestPriorityCell_width5(t *testing.T) {
	for _, p := range []string{"P0", "P1", "P2", "P3", "P4", "P5"} {
		cell := priorityCell(sql.NullString{String: p, Valid: true})
		// In test mode lipgloss renders plain text (no TTY), so len == visible width.
		if len(cell) != 5 {
			t.Errorf("priorityCell(%q): expected len 5, got %d (%q)", p, len(cell), cell)
		}
	}
}

func TestPriorityCell_containsLabel(t *testing.T) {
	for _, p := range []string{"P0", "P1", "P2", "P3", "P4", "P5"} {
		cell := priorityCell(sql.NullString{String: p, Valid: true})
		want := fmt.Sprintf("[%s]", p)
		if !strings.Contains(cell, want) {
			t.Errorf("priorityCell(%q): expected cell to contain %q, got %q", p, want, cell)
		}
	}
}

func TestPriorityCell_p3NotStyled(t *testing.T) {
	// P3 is the neutral default tier — it must NOT be in priorityStyles.
	// Without a style entry it renders via the fallthrough fmt.Sprintf branch,
	// which produces no ANSI escape sequences (the load-bearing assertion for
	// the symmetric-around-neutral design).
	if _, ok := priorityStyles["P3"]; ok {
		t.Error("P3 must not be in priorityStyles — it should render with default terminal foreground")
	}
	cell := priorityCell(sql.NullString{String: "P3", Valid: true})
	if strings.Contains(cell, "\x1b[") || strings.Contains(cell, "[") {
		t.Errorf("P3 cell must have no ANSI escape codes, got %q", cell)
	}
}

// --- NC-90: assignee column in collapsed row ---

func TestRenderIssueRow_assigneeShownWhenSet(t *testing.T) {
	node := &repo.IssueNode{
		Issue: &repo.Issue{
			ID:       42,
			Status:   "in_progress",
			Title:    "Some ticket",
			Assignee: sql.NullString{String: "copper", Valid: true},
		},
	}
	row := renderIssueRow(node, 0, 120)
	if !strings.Contains(row, "copper") {
		t.Errorf("renderIssueRow for assigned ticket should contain assignee name, got: %q", row)
	}
}

func TestRenderIssueRow_assigneeBlankWhenUnset(t *testing.T) {
	// Two tickets with same-length titles: one assigned, one not.
	// Both rows must have equal visible width so columns stay aligned.
	assigned := &repo.IssueNode{
		Issue: &repo.Issue{
			ID:       1,
			Status:   "open",
			Title:    "Same title here",
			Assignee: sql.NullString{String: "iron", Valid: true},
		},
	}
	unassigned := &repo.IssueNode{
		Issue: &repo.Issue{
			ID:     2,
			Status: "open",
			Title:  "Same title here",
		},
	}
	rowA := renderIssueRow(assigned, 0, 120)
	rowU := renderIssueRow(unassigned, 0, 120)

	// Unassigned row must not contain a stray agent name.
	if strings.Contains(rowU, "iron") {
		t.Errorf("unassigned row should not contain 'iron', got: %q", rowU)
	}

	// Both rows must have the same visible width (lipgloss strips ANSI).
	wA := lipgloss.Width(rowA)
	wU := lipgloss.Width(rowU)
	if wA != wU {
		t.Errorf("assigned row width=%d, unassigned row width=%d — columns misaligned", wA, wU)
	}
}

func TestRenderIssueRow_assigneeTruncatedAt8Chars(t *testing.T) {
	// An agent name longer than 8 chars must be truncated, not overflow the cell.
	node := &repo.IssueNode{
		Issue: &repo.Issue{
			ID:       10,
			Status:   "in_progress",
			Title:    "Long assignee test",
			Assignee: sql.NullString{String: "verylongname", Valid: true},
		},
	}
	row := renderIssueRow(node, 0, 120)
	// Must contain the first 8 chars of the name.
	if !strings.Contains(row, "verylong") {
		t.Errorf("renderIssueRow should contain truncated assignee 'verylong', got: %q", row)
	}
	// Must not contain the full name beyond 8 chars.
	if strings.Contains(row, "verylongname") {
		t.Errorf("renderIssueRow should truncate assignee to 8 chars, got: %q", row)
	}
}

func TestColorStatus_mergeConflict(t *testing.T) {
	// merge_conflict must render as a non-empty styled string distinct from
	// the repairing style, so the dashboard operator can visually distinguish
	// "needs conflict resolution" from "prole is fixing reviewer feedback".
	mc := colorStatus("merge_conflict")
	if mc == "" {
		t.Fatal("colorStatus(merge_conflict) returned empty string")
	}
	// The rendered output must contain the status text.
	if !strings.Contains(mc, "merge_conflict") {
		t.Errorf("colorStatus(merge_conflict) output %q does not contain status text", mc)
	}
	// It must differ from the repairing style.
	rep := colorStatus("repairing")
	if mc == rep {
		t.Errorf("colorStatus(merge_conflict) == colorStatus(repairing): expected distinct styles")
	}
}

// --- openPRCmd tests ---

func TestOpenPRCmd_success(t *testing.T) {
	var called int
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	m.openPRFn = func(prNumber int) error {
		called = prNumber
		return nil
	}

	cmd := m.openPRCmd(42)
	msg := cmd().(actionResultMsg)

	if msg.err != nil {
		t.Fatalf("expected no error, got %v", msg.err)
	}
	if !strings.Contains(msg.text, "42") {
		t.Errorf("success text %q does not mention PR number", msg.text)
	}
	if called != 42 {
		t.Errorf("openPRFn called with %d, want 42", called)
	}
}

func TestOpenPRCmd_surfacesStderr(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	m.openPRFn = func(prNumber int) error {
		return fmt.Errorf("exit status 1\nno pull requests found for branch")
	}

	cmd := m.openPRCmd(99)
	msg := cmd().(actionResultMsg)

	if msg.err == nil {
		t.Fatal("expected error, got nil")
	}
	// Error must include the PR number for context
	if !strings.Contains(msg.err.Error(), "99") {
		t.Errorf("error %q does not mention PR number", msg.err.Error())
	}
	// Error must include the stderr text from gh
	if !strings.Contains(msg.err.Error(), "no pull requests found") {
		t.Errorf("error %q does not include stderr text", msg.err.Error())
	}
}

func TestCleanStderr_truncatesLongInput(t *testing.T) {
	// 1KB of stderr — must be capped at 200 + "..." = 203 bytes
	long := strings.Repeat("x", 1024)
	got := cleanStderr(long)
	if len(got) > 210 {
		t.Errorf("cleanStderr length %d exceeds 210; status line would overflow", len(got))
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("cleanStderr %q should end with ...", got)
	}
}

func TestCleanStderr_stripsANSI(t *testing.T) {
	input := "\x1b[31mred error text\x1b[0m"
	got := cleanStderr(input)
	if strings.Contains(got, "\x1b") {
		t.Errorf("cleanStderr still contains ANSI codes: %q", got)
	}
	if !strings.Contains(got, "red error text") {
		t.Errorf("cleanStderr %q does not contain the plain text", got)
	}
}

// --- NC-119: prole session name tests ---

func TestDashboard_attachProle_usesTmuxSessionColumn(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(name string) bool { return name == "ct-prole-iron" },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	m.data.agents = []*repo.Agent{{
		Name:        "iron",
		Type:        "prole",
		TmuxSession: sql.NullString{String: "ct-prole-iron", Valid: true},
	}}
	m.focusedPanel = 0

	_, cmd := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("a")})
	if cmd == nil {
		t.Fatal("expected a tea.Cmd (spawnAttachCmd), got nil")
	}
	msg := cmd()
	result, ok := msg.(spawnAttachResultMsg)
	if !ok {
		t.Fatalf("expected spawnAttachResultMsg, got %T", msg)
	}
	if result.sessionName != "ct-prole-iron" {
		t.Errorf("expected sessionName='ct-prole-iron', got %q", result.sessionName)
	}
}

func TestDashboard_killProle_usesTmuxSessionColumn(t *testing.T) {
	killed := ""
	m, agents := newTestModel(t,
		func(name string) error { killed = name; return nil },
		func(string) bool { return true },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	if err := agents.Register("iron", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}

	agent := &repo.Agent{Name: "iron", TmuxSession: sql.NullString{String: "ct-prole-iron", Valid: true}}
	cmd := m.killAgentCmd(agent)
	msg := cmd().(actionResultMsg)

	if msg.err != nil {
		t.Fatalf("expected no error, got %v", msg.err)
	}
	if killed != "ct-prole-iron" {
		t.Errorf("expected killSession called with 'ct-prole-iron', got %q", killed)
	}
}

func TestDashboard_stopProle_usesTmuxSessionColumn(t *testing.T) {
	var sentTo string
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return true },
		func(name, _ string) error { sentTo = name; return nil },
		func(string, string) error { return nil },
	)

	agent := &repo.Agent{Name: "iron", TmuxSession: sql.NullString{String: "ct-prole-iron", Valid: true}}
	cmd := m.stopAgentCmd(agent)
	result := cmd().(actionResultMsg)

	if result.err != nil {
		t.Fatalf("expected no error, got %v", result.err)
	}
	if sentTo != "ct-prole-iron" {
		t.Errorf("expected sendKeys target 'ct-prole-iron', got %q", sentTo)
	}
}

func TestDashboard_restartProle_usesTmuxSessionColumn(t *testing.T) {
	killed := ""
	m, agents := newTestModel(t,
		func(name string) error { killed = name; return nil },
		func(string) bool { return true },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	if err := agents.Register("iron", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}

	agent := &repo.Agent{Name: "iron", Type: "prole", TmuxSession: sql.NullString{String: "ct-prole-iron", Valid: true}}
	cmd := m.restartAgentCmd(agent)
	cmd()

	if killed != "ct-prole-iron" {
		t.Errorf("expected killSession called with 'ct-prole-iron', got %q", killed)
	}
}

func TestDashboard_killMayor_unchanged(t *testing.T) {
	killed := ""
	m, agents := newTestModel(t,
		func(name string) error { killed = name; return nil },
		func(string) bool { return true },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	if err := agents.Register("mayor", "mayor", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}

	agent := &repo.Agent{Name: "mayor", TmuxSession: sql.NullString{String: "ct-mayor", Valid: true}}
	cmd := m.killAgentCmd(agent)
	msg := cmd().(actionResultMsg)

	if msg.err != nil {
		t.Fatalf("expected no error, got %v", msg.err)
	}
	if killed != "ct-mayor" {
		t.Errorf("expected killSession('ct-mayor'), got %q", killed)
	}
}

func TestDashboard_attachAgent_emptyTmuxSession_statusMsg(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return true },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	m.data.agents = []*repo.Agent{{
		Name: "ghost",
		Type: "prole",
	}}
	m.focusedPanel = 0

	updated, cmd := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("a")})
	dm := updated.(dashboardModel)

	if cmd != nil {
		t.Error("expected no cmd when TmuxSession is empty")
	}
	if !strings.Contains(dm.statusMsg, "no tmux session recorded") {
		t.Errorf("expected statusMsg to contain 'no tmux session recorded', got %q", dm.statusMsg)
	}
}

// --- repair_reason display tests ---

func TestRenderTicketDetails_repairReasonShownWhenSet(t *testing.T) {
	node := makeDetailNode("", "", "", 0)
	node.RepairReason = sql.NullString{String: "CI: lint, test", Valid: true}
	out := renderTicketDetails(node, 0, 80)
	if !strings.Contains(out, "repair:") || !strings.Contains(out, "CI: lint, test") {
		t.Errorf("expected repair_reason in output, got:\n%s", out)
	}
}

func TestRenderTicketDetails_repairReasonOmittedWhenNull(t *testing.T) {
	node := makeDetailNode("", "", "", 0)
	out := renderTicketDetails(node, 0, 80)
	if strings.Contains(out, "repair:") {
		t.Errorf("expected no repair line when repair_reason is null, got:\n%s", out)
	}
}

// --- nc-150: interactive edit of repair_reason ---

// makeRepairingTicket creates a repairing ticket in the test DB and seeds the
// dashboard model's data snapshot with it. Returns the ticket ID.
func makeRepairingTicket(t *testing.T, m *dashboardModel, status string) int {
	t.Helper()
	id, err := m.issues.Create("Fix CI failure", "bug", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create ticket: %v", err)
	}
	if err := m.issues.UpdateStatus(id, status); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	node := &repo.IssueNode{Issue: &repo.Issue{
		ID:     id,
		Status: status,
		Title:  "Fix CI failure",
	}}
	m.data.roots = []*repo.IssueNode{node}
	m.focusedPanel = 1
	m.ticketCursor = 0
	return id
}

func TestDashboard_EditRepairReason_entersInputMode(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	makeRepairingTicket(t, m, "repairing")

	upd, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("e")})
	dm := upd.(dashboardModel)

	if !dm.inputMode {
		t.Fatal("expected inputMode=true after pressing e on a repairing ticket")
	}
	if dm.inputAction != "repair_reason" {
		t.Errorf("expected inputAction=repair_reason, got %q", dm.inputAction)
	}
	if dm.inputBuffer != "" {
		t.Errorf("expected empty inputBuffer for ticket with no existing reason, got %q", dm.inputBuffer)
	}
}

func TestDashboard_EditRepairReason_seededWithExistingValue(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	id := makeRepairingTicket(t, m, "repairing")
	// Pre-seed repair_reason in the DB and the data snapshot.
	if err := m.issues.SetRepairReason(id, "existing note"); err != nil {
		t.Fatalf("SetRepairReason: %v", err)
	}
	m.data.roots[0].RepairReason = sql.NullString{String: "existing note", Valid: true}

	upd, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("e")})
	dm := upd.(dashboardModel)

	if !dm.inputMode {
		t.Fatal("expected inputMode=true")
	}
	if dm.inputBuffer != "existing note" {
		t.Errorf("expected inputBuffer seeded with existing reason, got %q", dm.inputBuffer)
	}
}

func TestDashboard_EditRepairReason_saveCallsSetRepairReason(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	id := makeRepairingTicket(t, m, "repairing")
	m.ticketPrefix = "nc"

	// Enter edit mode.
	upd, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("e")})
	dm := upd.(dashboardModel)

	// Type a reason.
	for _, ch := range "prole stuck on env var" {
		upd, _ = dm.Update(tea.KeyMsg{Type: -1, Runes: []rune{ch}})
		dm = upd.(dashboardModel)
	}

	// Press Enter to commit.
	upd, _ = dm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	dm = upd.(dashboardModel)

	if dm.inputMode {
		t.Error("expected inputMode=false after Enter")
	}

	// Verify the DB was updated.
	got, err := m.issues.Get(id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !got.RepairReason.Valid || got.RepairReason.String != "prole stuck on env var" {
		t.Errorf("expected repair_reason=%q, got valid=%v value=%q",
			"prole stuck on env var", got.RepairReason.Valid, got.RepairReason.String)
	}
}

func TestDashboard_EditRepairReason_noopOnUnsupportedStatus(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	// Create an in_progress ticket (not a repair-ish state).
	id, err := m.issues.Create("active work", "task", nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	m.data.roots = []*repo.IssueNode{{Issue: &repo.Issue{
		ID:     id,
		Status: "in_progress",
	}}}
	m.focusedPanel = 1
	m.ticketCursor = 0

	upd, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("e")})
	dm := upd.(dashboardModel)

	if dm.inputMode {
		t.Error("expected inputMode=false for in_progress ticket")
	}
	if dm.statusMsg == "" {
		t.Error("expected a status hint explaining why e is a no-op")
	}
}

func TestDashboard_EditRepairReason_enabledForMergeConflict(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	makeRepairingTicket(t, m, "merge_conflict")

	upd, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("e")})
	dm := upd.(dashboardModel)

	if !dm.inputMode {
		t.Fatal("expected inputMode=true for merge_conflict ticket")
	}
	if dm.inputAction != "repair_reason" {
		t.Errorf("expected inputAction=repair_reason, got %q", dm.inputAction)
	}
}

func TestDashboard_EditRepairReason_enabledForOnHold(t *testing.T) {
	m, _ := newTestModel(t,
		func(string) error { return nil },
		func(string) bool { return false },
		func(string, string) error { return nil },
		func(string, string) error { return nil },
	)
	makeRepairingTicket(t, m, "on_hold")

	upd, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("e")})
	dm := upd.(dashboardModel)

	if !dm.inputMode {
		t.Fatal("expected inputMode=true for on_hold ticket")
	}
}
