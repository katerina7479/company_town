package session

import (
	"testing"
)

// TestSendKeys_clearsInputBeforeSubmit verifies that SendKeys sends C-u to
// clear any accumulated input in the pane before injecting the nudge message.
// C-u (kill-line) is used instead of C-c so that a running tool call is not
// interrupted (nc-162). This prevents detached panes from accumulating
// pending nudges in the input box (nc-146).
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

	// Disable the sleep so the test runs fast.
	origSleep := sendKeySleepFn
	sendKeySleepFn = func() {}
	defer func() { sendKeySleepFn = origSleep }()

	if err := SendKeys("ct-tin", "hello"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(calls) != 3 {
		t.Fatalf("expected 3 send-keys calls, got %d: %v", len(calls), calls)
	}

	// First call must be the C-u kill-line clear (not C-c, which would interrupt
	// a running tool call).
	clearArgs := calls[0]
	if len(clearArgs) < 4 || clearArgs[0] != "send-keys" || clearArgs[1] != "-t" || clearArgs[2] != "ct-tin" || clearArgs[3] != "C-u" {
		t.Errorf("first call should be 'send-keys -t ct-tin C-u', got %v", clearArgs)
	}

	// Second call must send the message text with -l (literal), no Enter.
	msgArgs := calls[1]
	if len(msgArgs) < 5 || msgArgs[0] != "send-keys" || msgArgs[1] != "-t" || msgArgs[2] != "ct-tin" || msgArgs[3] != "-l" || msgArgs[4] != "hello" {
		t.Errorf("second call should be 'send-keys -t ct-tin -l hello', got %v", msgArgs)
	}

	// Third call must send Enter alone.
	enterArgs := calls[2]
	if len(enterArgs) < 4 || enterArgs[0] != "send-keys" || enterArgs[1] != "-t" || enterArgs[2] != "ct-tin" || enterArgs[3] != "Enter" {
		t.Errorf("third call should be 'send-keys -t ct-tin Enter', got %v", enterArgs)
	}
}

// TestSendKeys_EnterSentSeparately verifies that the Enter keystroke is sent as
// a distinct send-keys call after the message text, ensuring Claude Code's
// input handler sees it as a real submit rather than a paste continuation
// (nc-153).
func TestSendKeys_EnterSentSeparately(t *testing.T) {
	origExists := existsFn
	existsFn = func(string) bool { return true }
	defer func() { existsFn = origExists }()

	var calls [][]string
	orig := tmuxSendExec
	tmuxSendExec = func(args ...string) error {
		calls = append(calls, append([]string{}, args...))
		return nil
	}
	defer func() { tmuxSendExec = orig }()

	origSleep := sendKeySleepFn
	sendKeySleepFn = func() {}
	defer func() { sendKeySleepFn = origSleep }()

	if err := SendKeys("ct-mayor", "nudge text"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the text call does NOT contain Enter.
	textCall := calls[1]
	for _, arg := range textCall {
		if arg == "Enter" {
			t.Errorf("literal text call should not include 'Enter', got %v", textCall)
		}
	}

	// Verify the Enter call contains only Enter (no message text).
	enterCall := calls[2]
	for _, arg := range enterCall[3:] { // skip send-keys -t <name>
		if arg != "Enter" {
			t.Errorf("Enter call should only contain 'Enter' after target, got %v", enterCall)
		}
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
