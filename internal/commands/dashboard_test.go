package commands

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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

	agents := repo.NewAgentRepo(conn)
	m := &dashboardModel{
		conn:          conn,
		agents:        agents,
		issues:        repo.NewIssueRepo(conn),
		killSession:   killFn,
		sessionExists: existsFn,
		sendKeys:      sendKeysFn,
		restartAgent:  restartFn,
		sleepFn:       func(time.Duration) {}, // no-op in tests
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

	agent := &repo.Agent{Name: "obsidian"}
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

	agent := &repo.Agent{Name: "quartz"}
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
	agent := &repo.Agent{Name: "ghost"}
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

	agent := &repo.Agent{Name: "conductor"}
	cmd := m.stopAgentCmd(agent)
	result := cmd().(actionResultMsg)

	if result.err != nil {
		t.Fatalf("expected no error, got %v", result.err)
	}
	if result.text != "Sent stop signal to conductor" {
		t.Errorf("unexpected success text: %q", result.text)
	}
	if sentTo != "ct-conductor" {
		t.Errorf("expected sendKeys to 'ct-conductor', got %q", sentTo)
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

	agent := &repo.Agent{Name: "conductor"}
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

	agent := &repo.Agent{Name: "conductor"}
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
	agent := &repo.Agent{Name: "copper", Type: "prole"}
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
		func(string, string) error { t.Error("restartAgent should not be called after kill failure"); return nil },
	)

	agent := &repo.Agent{Name: "copper", Type: "prole"}
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

	agent := &repo.Agent{Name: "copper", Type: "prole"}
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
	m.data.agents = []*repo.Agent{{Name: "copper", Type: "prole"}}
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
	if (*sent)[0].session != "ct-copper" {
		t.Errorf("expected sendKeys to 'ct-copper', got %q", (*sent)[0].session)
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
	// Session does NOT exist — pressing n should NOT enter input mode.
	m, _ := makeModelWithAgents(t, false)

	updated, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("n")})
	dm := updated.(dashboardModel)

	if dm.inputMode {
		t.Error("expected inputMode=false for agent with no live session")
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
