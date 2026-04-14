package session

import (
	"errors"
	"testing"
)

func TestSpawnAttach_unknownTerminal(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "xterm-256color")
	err := SpawnAttach("ct-iron")
	if !errors.Is(err, ErrUnknownTerminal) {
		t.Errorf("expected ErrUnknownTerminal, got %v", err)
	}
}

func TestSpawnAttach_emptyTermProgram(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "")
	err := SpawnAttach("ct-iron")
	if !errors.Is(err, ErrUnknownTerminal) {
		t.Errorf("expected ErrUnknownTerminal for empty TERM_PROGRAM, got %v", err)
	}
}

func TestOsascriptQuote_noSpecialChars(t *testing.T) {
	got := osascriptQuote("tmux attach-session -t 'ct-iron'")
	want := `"tmux attach-session -t 'ct-iron'"`
	if got != want {
		t.Errorf("osascriptQuote = %q, want %q", got, want)
	}
}

func TestOsascriptQuote_withDoubleQuotes(t *testing.T) {
	got := osascriptQuote(`say "hello"`)
	// embedded double quote becomes: \" & quote & "
	want := `"say \" & quote & "hello\" & quote & ""`
	if got != want {
		t.Errorf("osascriptQuote = %q, want %q", got, want)
	}
}

func TestAttachCmd(t *testing.T) {
	got := attachCmd("ct-iron")
	want := "tmux attach-session -t 'ct-iron'"
	if got != want {
		t.Errorf("attachCmd = %q, want %q", got, want)
	}
}
