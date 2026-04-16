package session

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newTestClient returns a tmuxClient with controllable seams for unit tests.
// check always reports the named session as present. exec captures all calls.
// sleep is a no-op so tests finish instantly.
func newTestClient(check func(string) bool, exec func(...string) error) *tmuxClient {
	if check == nil {
		check = func(string) bool { return true }
	}
	if exec == nil {
		exec = func(...string) error { return nil }
	}
	return &tmuxClient{
		check:   check,
		exec:    exec,
		sleep:   func() {},
		spawn:   func(string, ...string) ([]byte, error) { return nil, nil },
		capture: func(...string) ([]byte, error) { return nil, nil },
	}
}

// TestSendKeys_clearsInputBeforeSubmit verifies that SendKeys sends C-u to
// clear any accumulated input in the pane before injecting the nudge message.
// C-u (kill-line) is used instead of C-c so that a running tool call is not
// interrupted (nc-162). This prevents detached panes from accumulating
// pending nudges in the input box (nc-146).
func TestSendKeys_clearsInputBeforeSubmit(t *testing.T) {
	var calls [][]string
	c := newTestClient(nil, func(args ...string) error {
		calls = append(calls, append([]string{}, args...))
		return nil
	})

	if err := c.SendKeys("ct-tin", "hello"); err != nil {
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
	var calls [][]string
	c := newTestClient(nil, func(args ...string) error {
		calls = append(calls, append([]string{}, args...))
		return nil
	})

	if err := c.SendKeys("ct-mayor", "nudge text"); err != nil {
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

// TestSendKeys_wrapsSendError verifies that when the literal-text send-keys
// call fails, SendKeys returns an error that (a) wraps the underlying error so
// callers can use errors.Is/As and (b) includes the session name for context.
func TestSendKeys_wrapsSendError(t *testing.T) {
	sentinelErr := errors.New("exit status 1")
	callCount := 0
	c := newTestClient(
		func(string) bool { return true },
		func(args ...string) error {
			callCount++
			if callCount == 2 { // second call is the -l literal text send
				return sentinelErr
			}
			return nil
		},
	)

	err := c.SendKeys("ct-tin", "hello")
	if err == nil {
		t.Fatal("expected error from failed literal send")
	}
	if !errors.Is(err, sentinelErr) {
		t.Errorf("error should wrap the underlying exec error; got: %v", err)
	}
	if !strings.Contains(err.Error(), "ct-tin") {
		t.Errorf("error should mention session name; got: %v", err)
	}
}

// TestSendKeys_wrapsEnterError verifies that when the Enter send-keys call
// fails, SendKeys returns a wrapped error that includes the session name.
func TestSendKeys_wrapsEnterError(t *testing.T) {
	sentinelErr := errors.New("exit status 1")
	callCount := 0
	c := newTestClient(
		func(string) bool { return true },
		func(args ...string) error {
			callCount++
			if callCount == 3 { // third call is the Enter send
				return sentinelErr
			}
			return nil
		},
	)

	err := c.SendKeys("ct-mayor", "nudge")
	if err == nil {
		t.Fatal("expected error from failed Enter send")
	}
	if !errors.Is(err, sentinelErr) {
		t.Errorf("error should wrap the underlying exec error; got: %v", err)
	}
	if !strings.Contains(err.Error(), "ct-mayor") {
		t.Errorf("error should mention session name; got: %v", err)
	}
}

// TestSendKeys_sessionMissing verifies that SendKeys returns an error and does
// not call exec when the session does not exist.
func TestSendKeys_sessionMissing(t *testing.T) {
	var calls [][]string
	c := newTestClient(
		func(string) bool { return false },
		func(args ...string) error {
			calls = append(calls, args)
			return nil
		},
	)

	if err := c.SendKeys("ct-tin", "hello"); err == nil {
		t.Fatal("expected error for missing session")
	}
	if len(calls) != 0 {
		t.Errorf("expected no exec calls for missing session, got %d", len(calls))
	}
}

// TestSessionName verifies that SessionName prepends the ct- prefix.
func TestSessionName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"mayor", "ct-mayor"},
		{"architect", "ct-architect"},
		{"copper", "ct-copper"},
	}
	for _, tc := range cases {
		if got := SessionName(tc.in); got != tc.want {
			t.Errorf("SessionName(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestClient_Exists_present verifies Exists returns true when check reports present.
func TestClient_Exists_present(t *testing.T) {
	c := newTestClient(func(string) bool { return true }, nil)
	if !c.Exists("ct-iron") {
		t.Error("expected Exists to return true when check=true")
	}
}

// TestClient_Exists_absent verifies Exists returns false when check reports absent.
func TestClient_Exists_absent(t *testing.T) {
	c := newTestClient(func(string) bool { return false }, nil)
	if c.Exists("ct-iron") {
		t.Error("expected Exists to return false when check=false")
	}
}

// TestClient_Kill_sessionAbsent_isNoOp verifies Kill returns nil without calling
// exec when the session does not exist.
func TestClient_Kill_sessionAbsent_isNoOp(t *testing.T) {
	var calls [][]string
	c := newTestClient(
		func(string) bool { return false },
		func(args ...string) error {
			calls = append(calls, append([]string{}, args...))
			return nil
		},
	)
	if err := c.Kill("ct-iron"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(calls) != 0 {
		t.Errorf("expected no exec calls when session absent, got %d: %v", len(calls), calls)
	}
}

// TestClient_Kill_sessionPresent_callsKillSession verifies Kill issues kill-session
// targeting the correct session name.
func TestClient_Kill_sessionPresent_callsKillSession(t *testing.T) {
	var killedSession string
	c := newTestClient(
		func(string) bool { return true },
		func(args ...string) error {
			if len(args) >= 3 && args[0] == "kill-session" {
				killedSession = args[2] // -t <name>
			}
			return nil
		},
	)
	if err := c.Kill("ct-copper"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if killedSession != "ct-copper" {
		t.Errorf("expected kill-session targeting ct-copper, got %q", killedSession)
	}
}

// TestClient_Kill_execError_wrapsError verifies Kill returns a wrapped error when
// the kill-session exec call fails.
func TestClient_Kill_execError_wrapsError(t *testing.T) {
	sentinel := errors.New("tmux: server not found")
	c := newTestClient(
		func(string) bool { return true },
		func(...string) error { return sentinel },
	)
	err := c.Kill("ct-iron")
	if err == nil {
		t.Fatal("expected error from exec failure")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected wrapped sentinel error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "ct-iron") {
		t.Errorf("error should mention session name; got: %v", err)
	}
}

// TestProvisionClaudeSettings_creates verifies that provisionClaudeSettings writes a
// .claude/settings.json containing the permissions block when the file is absent.
func TestProvisionClaudeSettings_creates(t *testing.T) {
	dir := t.TempDir()
	if err := provisionClaudeSettings(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("settings file not created: %v", err)
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		t.Fatalf("settings file is not valid JSON: %v", err)
	}
	if _, ok := settings["permissions"]; !ok {
		t.Error("settings missing top-level 'permissions' key")
	}
}

// TestProvisionClaudeSettings_idempotent verifies that a second call does not
// overwrite a settings file that already exists.
func TestProvisionClaudeSettings_idempotent(t *testing.T) {
	dir := t.TempDir()
	if err := provisionClaudeSettings(dir); err != nil {
		t.Fatalf("first call: %v", err)
	}
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	custom := []byte(`{"custom":true}`)
	if err := os.WriteFile(settingsPath, custom, 0644); err != nil {
		t.Fatalf("writing custom content: %v", err)
	}
	if err := provisionClaudeSettings(dir); err != nil {
		t.Fatalf("second call: %v", err)
	}
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatalf("reading after second call: %v", err)
	}
	if string(data) != string(custom) {
		t.Error("provisionClaudeSettings overwrote existing settings file")
	}
}

// TestCreateInteractive_sessionAlreadyExists verifies the early return when the
// session already exists.
func TestCreateInteractive_sessionAlreadyExists(t *testing.T) {
	orig := defaultClient
	t.Cleanup(func() { defaultClient = orig })
	defaultClient = newTestClient(func(string) bool { return true }, nil)

	err := CreateInteractive(AgentSessionConfig{Name: "ct-iron"})
	if err == nil {
		t.Fatal("expected error when session already exists")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists': %v", err)
	}
}

// TestCreateInteractive_provisionsThenAttemptsTmux verifies that CreateInteractive
// provisions the .claude/settings.json file and then proceeds to the tmux
// new-session invocation. In test environments the tmux call will fail (no server
// or no claude binary), which is expected — the key is that the settings file is
// created and the code paths up to cmd.Run() are exercised.
func TestCreateInteractive_provisionsThenAttemptsTmux(t *testing.T) {
	orig := defaultClient
	t.Cleanup(func() { defaultClient = orig })
	defaultClient = newTestClient(func(string) bool { return false }, nil)

	dir := t.TempDir()
	sessionName := "ct-nc188-provision-test"
	// Clean up the session if tmux happens to be running and the call succeeds.
	t.Cleanup(func() {
		exec.Command("tmux", "kill-session", "-t", sessionName).Run() //nolint:errcheck
	})

	cfg := AgentSessionConfig{
		Name:     sessionName,
		AgentDir: dir,
		Model:    "claude-test",
		WorkDir:  dir,
		Prompt:   "test prompt",
		EnvVars:  map[string]string{"CT_AGENT_NAME": "test"},
	}
	// The return value is not asserted — tmux new-session may succeed or fail
	// depending on the environment. We only verify the settings side-effect.
	_ = CreateInteractive(cfg)

	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	if _, err := os.Stat(settingsPath); err != nil {
		t.Errorf("expected .claude/settings.json to be created before tmux call: %v", err)
	}
}

// TestAttach_sessionNotFound verifies that Attach returns an error immediately when
// the named session does not exist.
func TestAttach_sessionNotFound(t *testing.T) {
	orig := defaultClient
	t.Cleanup(func() { defaultClient = orig })
	defaultClient = newTestClient(func(string) bool { return false }, nil)

	err := Attach("ct-nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error should mention 'does not exist': %v", err)
	}
}

// TestAttach_tmuxNotOnPath verifies that Attach returns a "tmux not found" error
// when exec.LookPath cannot locate the tmux binary.
func TestAttach_tmuxNotOnPath(t *testing.T) {
	orig := defaultClient
	t.Cleanup(func() { defaultClient = orig })
	defaultClient = newTestClient(func(string) bool { return true }, nil)
	t.Setenv("PATH", "")

	err := Attach("ct-test")
	if err == nil {
		t.Fatal("expected error when tmux not on PATH")
	}
	if !strings.Contains(err.Error(), "tmux not found") {
		t.Errorf("expected 'tmux not found' in error: %v", err)
	}
}

// TestListCompanyTown_noPanic verifies that ListCompanyTown returns a nil error
// regardless of whether tmux is running, and never panics.
func TestListCompanyTown_noPanic(t *testing.T) {
	sessions, err := ListCompanyTown()
	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
	_ = sessions
}

// TestPackageLevelWrappers_delegateToDefaultClient verifies that the four
// package-level convenience functions (Exists, Kill, SendKeys, SpawnAttach)
// forward their arguments to defaultClient.
func TestPackageLevelWrappers_delegateToDefaultClient(t *testing.T) {
	orig := defaultClient
	t.Cleanup(func() { defaultClient = orig })

	var existsArg string
	defaultClient = newTestClient(
		func(name string) bool {
			existsArg = name
			return false // report every session as absent
		},
		func(...string) error { return nil },
	)

	// Exists wrapper.
	Exists("ct-wrapper-test")
	if existsArg != "ct-wrapper-test" {
		t.Errorf("Exists did not forward name to defaultClient, got %q", existsArg)
	}

	// Kill wrapper — session absent means no-op, returns nil.
	if err := Kill("ct-wrapper-test"); err != nil {
		t.Errorf("Kill returned unexpected error: %v", err)
	}

	// SendKeys wrapper — session absent, returns error.
	if err := SendKeys("ct-wrapper-test", "hello"); err == nil {
		t.Error("SendKeys should return error when session does not exist")
	}

	// SpawnAttach wrapper — unknown terminal returns ErrUnknownTerminal.
	t.Setenv("TERM_PROGRAM", "unknown-terminal-nc188")
	if err := SpawnAttach("ct-wrapper-test"); !errors.Is(err, ErrUnknownTerminal) {
		t.Errorf("SpawnAttach should return ErrUnknownTerminal, got: %v", err)
	}
}

// TestDefaultClient_checkClosure_doesNotPanic verifies that the real check closure
// created by New() (and used by defaultClient) executes without panicking.
// In most test environments tmux is not running so Exists returns false.
func TestDefaultClient_checkClosure_doesNotPanic(t *testing.T) {
	// Restore defaultClient after the test suite may have overridden it.
	// We are deliberately using the real defaultClient here to exercise
	// the closure created inside New().
	_ = Exists("ct-nc188-nonexistent-check-test")
}

// TestCapturePane_ReturnsContent verifies that CapturePane returns the output
// of `tmux capture-pane -p -t <name>` as a string.
func TestCapturePane_ReturnsContent(t *testing.T) {
	const paneContent = "line1\nline2\nAre you sure? (y/n)\n"
	c := newTestClient(nil, nil)
	c.capture = func(args ...string) ([]byte, error) {
		return []byte(paneContent), nil
	}

	got, err := c.CapturePane("ct-copper")
	if err != nil {
		t.Fatalf("CapturePane: %v", err)
	}
	if got != paneContent {
		t.Errorf("CapturePane = %q, want %q", got, paneContent)
	}
}

// TestCapturePane_SessionNotFound verifies that CapturePane returns an error
// when the session does not exist.
func TestCapturePane_SessionNotFound(t *testing.T) {
	c := newTestClient(func(string) bool { return false }, nil)

	_, err := c.CapturePane("ct-nonexistent")
	if err == nil {
		t.Fatal("expected error for non-existent session, got nil")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("expected 'does not exist' in error, got: %v", err)
	}
}
