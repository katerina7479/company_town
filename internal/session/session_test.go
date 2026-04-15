package session

import (
	"testing"
)

// TestSendKeys_clearsInputBeforeSubmit verifies that SendKeys sends C-c to
// clear any accumulated input in the pane before injecting the nudge message.
// This prevents detached panes from accumulating pending nudges in the input
// box (nc-146).
func TestSendKeys_clearsInputBeforeSubmit(t *testing.T) {
	// Override Exists so it reports the session as present.
	origExists := existsFn
	existsFn = func(string) bool { return true }
	defer func() { existsFn = origExists }()

	// Capture all send-keys calls.
	var calls [][]string
	orig := tmuxSendExec
	tmuxSendExec = func(args ...string) error {
		calls = append(calls, append([]string{}, args...))
		return nil
	}
	defer func() { tmuxSendExec = orig }()

	if err := SendKeys("ct-tin", "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 send-keys calls, got %d: %v", len(calls), calls)
	}

	// First call must be the C-c clear.
	clearArgs := calls[0]
	if len(clearArgs) < 4 || clearArgs[0] != "send-keys" || clearArgs[1] != "-t" || clearArgs[2] != "ct-tin" || clearArgs[3] != "C-c" {
		t.Errorf("first call should be 'send-keys -t ct-tin C-c', got %v", clearArgs)
	}

	// Second call must send the message with Enter.
	msgArgs := calls[1]
	if len(msgArgs) < 5 || msgArgs[0] != "send-keys" || msgArgs[1] != "-t" || msgArgs[2] != "ct-tin" || msgArgs[3] != "hello" || msgArgs[4] != "Enter" {
		t.Errorf("second call should be 'send-keys -t ct-tin hello Enter', got %v", msgArgs)
	}
}

// TestSendKeys_sessionMissing verifies that SendKeys returns an error and does
// not call send-keys when the session does not exist.
func TestSendKeys_sessionMissing(t *testing.T) {
	origExists := existsFn
	existsFn = func(string) bool { return false }
	defer func() { existsFn = origExists }()

	var calls [][]string
	orig := tmuxSendExec
	tmuxSendExec = func(args ...string) error {
		calls = append(calls, args)
		return nil
	}
	defer func() { tmuxSendExec = orig }()

	if err := SendKeys("ct-tin", "hello"); err == nil {
		t.Fatal("expected error for missing session")
	}
	if len(calls) != 0 {
		t.Errorf("expected no send-keys calls for missing session, got %d", len(calls))
	}
}
