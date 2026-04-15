package gtcmd

import (
	"strings"
	"testing"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

// newAgentRepo creates a test DB and returns a fresh AgentRepo.
func newAgentRepo(t *testing.T) *repo.AgentRepo {
	t.Helper()
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return repo.NewAgentRepo(conn, nil)
}

// --- agentRegister ---

func TestAgentRegister_usageError(t *testing.T) {
	agents := newAgentRepo(t)
	if err := agentRegister(agents, []string{"onlyone"}); err == nil {
		t.Fatal("expected usage error for < 2 args")
	}
	if err := agentRegister(agents, []string{}); err == nil {
		t.Fatal("expected usage error for 0 args")
	}
}

func TestAgentRegister_noSpecialty(t *testing.T) {
	agents := newAgentRepo(t)
	if err := agentRegister(agents, []string{"iron", "prole"}); err != nil {
		t.Fatalf("agentRegister: %v", err)
	}
	a, err := agents.Get("iron")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if a.Name != "iron" || a.Type != "prole" {
		t.Errorf("unexpected agent: %+v", a)
	}
	if a.Specialty.Valid {
		t.Errorf("expected no specialty, got %q", a.Specialty.String)
	}
}

func TestAgentRegister_withSpecialty(t *testing.T) {
	agents := newAgentRepo(t)
	if err := agentRegister(agents, []string{"tin", "prole", "--specialty", "frontend"}); err != nil {
		t.Fatalf("agentRegister with specialty: %v", err)
	}
	a, err := agents.Get("tin")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !a.Specialty.Valid || a.Specialty.String != "frontend" {
		t.Errorf("expected specialty='frontend', got %v", a.Specialty)
	}
}

// --- agentStatus ---

func TestAgentStatus_usageError(t *testing.T) {
	agents := newAgentRepo(t)
	if err := agentStatus(agents, []string{"iron"}); err == nil {
		t.Fatal("expected usage error for < 2 args")
	}
	if err := agentStatus(agents, []string{}); err == nil {
		t.Fatal("expected usage error for 0 args")
	}
}

func TestAgentStatus_withIssue(t *testing.T) {
	agents := newAgentRepo(t)
	if err := agents.Register("copper", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := agentStatus(agents, []string{"copper", "working", "--issue", "42"}); err != nil {
		t.Fatalf("agentStatus with issue: %v", err)
	}
	a, err := agents.Get("copper")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if a.Status != "working" {
		t.Errorf("expected status=working, got %q", a.Status)
	}
	if !a.CurrentIssue.Valid || a.CurrentIssue.Int64 != 42 {
		t.Errorf("expected current_issue=42, got %v", a.CurrentIssue)
	}
}

func TestAgentStatus_issueRequiresWorking(t *testing.T) {
	agents := newAgentRepo(t)
	if err := agents.Register("copper", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	err := agentStatus(agents, []string{"copper", "idle", "--issue", "42"})
	if err == nil {
		t.Fatal("expected error: --issue requires working status")
	}
	if !strings.Contains(err.Error(), "--issue") {
		t.Errorf("error should mention --issue: %v", err)
	}
}

func TestAgentStatus_idle(t *testing.T) {
	agents := newAgentRepo(t)
	if err := agents.Register("copper", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	issueID := 5
	if err := agents.SetCurrentIssue("copper", &issueID); err != nil {
		t.Fatalf("SetCurrentIssue: %v", err)
	}

	if err := agentStatus(agents, []string{"copper", "idle"}); err != nil {
		t.Fatalf("agentStatus idle: %v", err)
	}

	a, err := agents.Get("copper")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if a.Status != "idle" {
		t.Errorf("expected status=idle, got %q", a.Status)
	}
	if a.CurrentIssue.Valid {
		t.Errorf("expected current_issue=NULL after idle, got %d", a.CurrentIssue.Int64)
	}
}

func TestAgentStatus_defaultStatusUpdate(t *testing.T) {
	agents := newAgentRepo(t)
	if err := agents.Register("iron", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := agentStatus(agents, []string{"iron", "dead"}); err != nil {
		t.Fatalf("agentStatus dead: %v", err)
	}
	a, err := agents.Get("iron")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if a.Status != "dead" {
		t.Errorf("expected status=dead, got %q", a.Status)
	}
}

func TestAgentStatus_withPrefixedIssueID(t *testing.T) {
	agents := newAgentRepo(t)
	if err := agents.Register("tin", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := agentStatus(agents, []string{"tin", "working", "--issue", "nc-7"}); err != nil {
		t.Fatalf("agentStatus with prefixed issue: %v", err)
	}
	a, err := agents.Get("tin")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !a.CurrentIssue.Valid || a.CurrentIssue.Int64 != 7 {
		t.Errorf("expected current_issue=7, got %v", a.CurrentIssue)
	}
}

// --- statusIcon ---

func TestStatusIcon(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{"pass", "✓"},
		{"fail", "✗"},
		{"warn", "⚠"},
		{"unknown", "?"},
		{"", "?"},
	}
	for _, tc := range cases {
		got := statusIcon(tc.status)
		if got != tc.want {
			t.Errorf("statusIcon(%q) = %q, want %q", tc.status, got, tc.want)
		}
	}
}

// --- proleList ---

func TestProleList_empty(t *testing.T) {
	agents := newAgentRepo(t)
	cfg := &config.Config{TicketPrefix: "nc"}
	// No proles registered — should print "No proles." without error.
	if err := proleList(agents, cfg); err != nil {
		t.Fatalf("proleList empty: %v", err)
	}
}

func TestProleList_withProles(t *testing.T) {
	conn, err := db.NewTestDB()
	if err != nil {
		t.Fatalf("NewTestDB: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	agents := repo.NewAgentRepo(conn, nil)
	cfg := &config.Config{TicketPrefix: "nc"}

	// Register two proles and one non-prole
	if err := agents.Register("copper", "prole", nil); err != nil {
		t.Fatalf("Register copper: %v", err)
	}
	if err := agents.Register("tin", "prole", nil); err != nil {
		t.Fatalf("Register tin: %v", err)
	}
	if err := agents.Register("architect", "architect", nil); err != nil {
		t.Fatalf("Register architect: %v", err)
	}

	// Give copper a session and a current issue
	if err := agents.SetTmuxSession("copper", "ct-prole-copper"); err != nil {
		t.Fatalf("SetTmuxSession: %v", err)
	}
	issueID := 99
	if err := agents.SetCurrentIssue("copper", &issueID); err != nil {
		t.Fatalf("SetCurrentIssue: %v", err)
	}

	if err := proleList(agents, cfg); err != nil {
		t.Fatalf("proleList: %v", err)
	}
}

// TestAgentStatus_badIssueID verifies that agentStatus returns an error when the
// --issue flag is given a non-numeric value.
func TestAgentStatus_badIssueID(t *testing.T) {
	agents := newAgentRepo(t)
	if err := agents.Register("tin", "prole", nil); err != nil {
		t.Fatalf("Register: %v", err)
	}
	err := agentStatus(agents, []string{"tin", "working", "--issue", "not-valid-id"})
	if err == nil {
		t.Fatal("expected error for non-numeric --issue value, got nil")
	}
	if !strings.Contains(err.Error(), "invalid issue ID") {
		t.Errorf("expected 'invalid issue ID' in error, got: %v", err)
	}
}
