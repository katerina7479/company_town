package session

import (
	"errors"
	"strings"
	"testing"
)

func TestAgentTypeFromSessionName(t *testing.T) {
	cases := []struct{ name, wantType string }{
		{"ct-mayor", "mayor"}, {"ct-architect", "architect"},
		{"ct-reviewer", "reviewer"}, {"ct-daemon", "daemon"},
		{"ct-prole-copper", "prole"}, {"ct-prole-iron", "prole"},
		{"ct-artisan-backend", "artisan"}, {"ct-artisan-frontend", "artisan"},
		{"mayor", "mayor"}, {"prole-copper", "prole"},
	}
	for _, c := range cases {
		if got := AgentTypeFromSessionName(c.name); got != c.wantType {
			t.Errorf("AgentTypeFromSessionName(%q) = %q, want %q", c.name, got, c.wantType)
		}
	}
}

func TestAgentTypeColors_knownTypes(t *testing.T) {
	for _, at := range []string{"mayor", "architect", "reviewer", "daemon", "prole", "artisan"} {
		if _, ok := agentTypeColors[at]; !ok {
			t.Errorf("agentTypeColors missing entry for %q", at)
		}
	}
}

func captureExec(captured *[][]string) func(...string) error {
	return func(args ...string) error {
		cp := make([]string, len(args))
		copy(cp, args)
		*captured = append(*captured, cp)
		return nil
	}
}

func TestStyleSession_UnknownTypeIsNoop(t *testing.T) {
	var calls [][]string
	orig := styleSessionExec; styleSessionExec = captureExec(&calls)
	defer func() { styleSessionExec = orig }()
	if err := ApplyStatusBar("ct-whatever", "unknown-type"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("expected 0 exec calls for unknown type, got %d", len(calls))
	}
}

func TestStyleSession_EmptyTypeIsNoop(t *testing.T) {
	var calls [][]string
	orig := styleSessionExec; styleSessionExec = captureExec(&calls)
	defer func() { styleSessionExec = orig }()
	if err := ApplyStatusBar("ct-mayor", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("expected 0 exec calls for empty type, got %d", len(calls))
	}
}

func TestStyleSession_EachAgentType(t *testing.T) {
	for _, agentType := range []string{"mayor", "architect", "reviewer", "daemon", "prole", "artisan"} {
		var calls [][]string
		orig := styleSessionExec; styleSessionExec = captureExec(&calls)
		err := ApplyStatusBar("ct-test", agentType)
		styleSessionExec = orig
		if err != nil {
			t.Fatalf("ApplyStatusBar(%q): %v", agentType, err)
		}
		if len(calls) != 2 {
			t.Errorf("agentType=%q: expected 2 calls, got %d", agentType, len(calls))
			continue
		}
		color := agentTypeColors[agentType]
		found := false
		for _, arg := range calls[1] {
			if strings.Contains(arg, color) { found = true; break }
		}
		if !found {
			t.Errorf("agentType=%q: color %q not in args %v", agentType, color, calls[1])
		}
	}
}

func TestStyleSession_StatusRightContainsHint(t *testing.T) {
	var calls [][]string
	orig := styleSessionExec; styleSessionExec = captureExec(&calls)
	defer func() { styleSessionExec = orig }()
	if err := ApplyStatusBar("ct-mayor", "mayor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) < 1 { t.Fatal("expected at least one call") }
	found := false
	for _, arg := range calls[0] {
		if strings.Contains(arg, "C-b d") { found = true; break }
	}
	if !found {
		t.Errorf("'C-b d' not found in status-right args %v", calls[0])
	}
}

func TestStyleSession_ExecErrorPropagates(t *testing.T) {
	orig := styleSessionExec
	styleSessionExec = func(args ...string) error { return errors.New("tmux not found") }
	defer func() { styleSessionExec = orig }()
	err := ApplyStatusBar("ct-mayor", "mayor")
	if err == nil { t.Fatal("expected error, got nil") }
	if !strings.Contains(err.Error(), "tmux set-option") {
		t.Errorf("expected 'tmux set-option' in error, got: %v", err)
	}
}
