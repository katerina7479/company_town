package session

import (
	"errors"
	"strings"
	"testing"
)

func stubExec(t *testing.T, out []byte, err error) *[]string {
	t.Helper()
	var captured []string
	orig := spawnAttachExec
	spawnAttachExec = func(name string, args ...string) ([]byte, error) {
		captured = append([]string{name}, args...)
		return out, err
	}
	t.Cleanup(func() { spawnAttachExec = orig })
	return &captured
}

func TestSpawnAttach_unknownTerminal(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "xterm-256color")
	if !errors.Is(SpawnAttach("ct-iron"), ErrUnknownTerminal) {
		t.Error("expected ErrUnknownTerminal")
	}
}

func TestSpawnAttach_emptyTermProgram(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "")
	if !errors.Is(SpawnAttach("ct-iron"), ErrUnknownTerminal) {
		t.Error("expected ErrUnknownTerminal for empty TERM_PROGRAM")
	}
}

func TestSpawnAttach_Ghostty(t *testing.T) {
	argv := stubExec(t, nil, nil)
	t.Setenv("TERM_PROGRAM", "ghostty")
	if err := SpawnAttach("ct-mayor"); err != nil {
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
	stubExec(t, []byte("osascript: error"), errors.New("exit status 1"))
	t.Setenv("TERM_PROGRAM", "ghostty")
	err := SpawnAttach("ct-mayor")
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
	argv := stubExec(t, nil, nil)
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	if err := SpawnAttach("ct-mayor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(*argv, " ")
	if !strings.Contains(joined, "create window") {
		t.Errorf("iTerm script missing 'create window': %s", joined)
	}
}

func TestSpawnAttach_Terminal_app(t *testing.T) {
	argv := stubExec(t, nil, nil)
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	if err := SpawnAttach("ct-mayor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	joined := strings.Join(*argv, " ")
	if !strings.Contains(joined, `tell application "Terminal"`) {
		t.Errorf("Terminal.app script wrong: %s", joined)
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
