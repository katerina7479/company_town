package session

import (
	"errors"
	"strings"
	"testing"
)

// newSpawnClient returns a tmuxClient whose spawn seam captures the program
// and arguments from the first call, and returns out/err.
func newSpawnClient(t *testing.T, out []byte, err error) (*tmuxClient, *[]string) {
	t.Helper()
	var captured []string
	c := &tmuxClient{
		check: func(string) bool { return true },
		exec:  func(...string) error { return nil },
		sleep: func() {},
		spawn: func(prog string, args ...string) ([]byte, error) {
			captured = append([]string{prog}, args...)
			return out, err
		},
	}
	return c, &captured
}

func TestSpawnAttach_unknownTerminal(t *testing.T) {
	c := &tmuxClient{
		check: func(string) bool { return true },
		exec:  func(...string) error { return nil },
		sleep: func() {},
		spawn: func(string, ...string) ([]byte, error) { return nil, nil },
	}
	t.Setenv("TERM_PROGRAM", "xterm-256color")
	if !errors.Is(c.SpawnAttach("ct-iron"), ErrUnknownTerminal) {
		t.Error("expected ErrUnknownTerminal")
	}
}

func TestSpawnAttach_emptyTermProgram(t *testing.T) {
	c := &tmuxClient{
		check: func(string) bool { return true },
		exec:  func(...string) error { return nil },
		sleep: func() {},
		spawn: func(string, ...string) ([]byte, error) { return nil, nil },
	}
	t.Setenv("TERM_PROGRAM", "")
	if !errors.Is(c.SpawnAttach("ct-iron"), ErrUnknownTerminal) {
		t.Error("expected ErrUnknownTerminal for empty TERM_PROGRAM")
	}
}

func TestSpawnAttach_Ghostty(t *testing.T) {
	c, argv := newSpawnClient(t, nil, nil)
	t.Setenv("TERM_PROGRAM", "ghostty")
	if err := c.SpawnAttach("ct-mayor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*argv) == 0 || (*argv)[0] != "osascript" {
		t.Errorf("expected osascript call, got %v", *argv)
	}
	joined := strings.Join(*argv, " ")
	if !strings.Contains(joined, "tmux attach-session") || !strings.Contains(joined, "ct-mayor") {
		t.Errorf("argv missing attach cmd or session name: %s", joined)
	}
}

func TestSpawnAttach_GhosttyExecFailureReturnsWrappedError(t *testing.T) {
	c, _ := newSpawnClient(t, []byte("osascript: error"), errors.New("exit status 1"))
	t.Setenv("TERM_PROGRAM", "ghostty")
	err := c.SpawnAttach("ct-mayor")
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrUnknownTerminal) {
		t.Errorf("Ghostty failure must not return ErrUnknownTerminal: %v", err)
	}
	if !strings.Contains(err.Error(), "osascript") {
		t.Errorf("error should mention osascript: %v", err)
	}
}

func TestSpawnAttach_iTerm(t *testing.T) {
	c, argv := newSpawnClient(t, nil, nil)
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	if err := c.SpawnAttach("ct-mayor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(*argv, " ")
	if !strings.Contains(joined, "create window") {
		t.Errorf("iTerm script missing 'create window': %s", joined)
	}
}

func TestSpawnAttach_Terminal_app(t *testing.T) {
	c, argv := newSpawnClient(t, nil, nil)
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	if err := c.SpawnAttach("ct-mayor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(*argv, " ")
	if !strings.Contains(joined, `tell application "Terminal"`) {
		t.Errorf("Terminal.app script wrong: %s", joined)
	}
}

func TestSpawnAttach_iTermExecFailureReturnsWrappedError(t *testing.T) {
	c, _ := newSpawnClient(t, []byte("osascript: error"), errors.New("exit status 1"))
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	err := c.SpawnAttach("ct-mayor")
	if err == nil {
		t.Fatal("expected error from failed iTerm osascript call")
	}
	if errors.Is(err, ErrUnknownTerminal) {
		t.Errorf("iTerm failure must not return ErrUnknownTerminal: %v", err)
	}
	if !strings.Contains(err.Error(), "iTerm") {
		t.Errorf("error should mention iTerm: %v", err)
	}
}

func TestSpawnAttach_AppleTerminalExecFailureReturnsWrappedError(t *testing.T) {
	c, _ := newSpawnClient(t, []byte("osascript: error"), errors.New("exit status 1"))
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	err := c.SpawnAttach("ct-mayor")
	if err == nil {
		t.Fatal("expected error from failed Apple_Terminal osascript call")
	}
	if errors.Is(err, ErrUnknownTerminal) {
		t.Errorf("Apple_Terminal failure must not return ErrUnknownTerminal: %v", err)
	}
	if !strings.Contains(err.Error(), "Terminal.app") {
		t.Errorf("error should mention Terminal.app: %v", err)
	}
}

func TestOsascriptQuote(t *testing.T) {
	got := osascriptQuote("tmux attach-session -t 'ct-iron'")
	want := `"tmux attach-session -t 'ct-iron'"`
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestAttachArgv(t *testing.T) {
	got := attachArgv("ct-iron")
	want := "tmux attach-session -t 'ct-iron'"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
