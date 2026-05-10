package session

import (
	"errors"
	"runtime"
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
		capture: func(...string) ([]byte, error) { return nil, nil },
	}
	return c, &captured
}

func TestSpawnAttach_unknownTerminal(t *testing.T) {
	c := &tmuxClient{
		check:   func(string) bool { return true },
		exec:    func(...string) error { return nil },
		sleep:   func() {},
		spawn:   func(string, ...string) ([]byte, error) { return nil, nil },
		capture: func(...string) ([]byte, error) { return nil, nil },
	}
	t.Setenv("TMUX", "")
	t.Setenv("TERM_PROGRAM", "xterm-256color")
	if !errors.Is(c.SpawnAttach("ct-iron"), ErrUnknownTerminal) {
		t.Error("expected ErrUnknownTerminal")
	}
}

func TestSpawnAttach_emptyTermProgram(t *testing.T) {
	c := &tmuxClient{
		check:   func(string) bool { return true },
		exec:    func(...string) error { return nil },
		sleep:   func() {},
		spawn:   func(string, ...string) ([]byte, error) { return nil, nil },
		capture: func(...string) ([]byte, error) { return nil, nil },
	}
	t.Setenv("TMUX", "")
	t.Setenv("TERM_PROGRAM", "")
	if !errors.Is(c.SpawnAttach("ct-iron"), ErrUnknownTerminal) {
		t.Error("expected ErrUnknownTerminal for empty TERM_PROGRAM")
	}
}

func TestSpawnAttach_Ghostty(t *testing.T) {
	c, argv := newSpawnClient(t, nil, nil)
	t.Setenv("TMUX", "")
	t.Setenv("TERM_PROGRAM", "ghostty")
	if err := c.SpawnAttach("ct-mayor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*argv) == 0 {
		t.Fatal("expected spawn call, got empty argv")
	}
	joined := strings.Join(*argv, " ")
	if runtime.GOOS == "linux" {
		// Linux ghostty uses the CLI, not osascript.
		if (*argv)[0] != "ghostty" {
			t.Errorf("expected ghostty CLI spawn on Linux, got %v", *argv)
		}
	} else {
		if (*argv)[0] != "osascript" {
			t.Errorf("expected osascript call on macOS, got %v", *argv)
		}
		if !strings.Contains(joined, "tmux attach-session") || !strings.Contains(joined, "ct-mayor") {
			t.Errorf("argv missing attach cmd or session name: %s", joined)
		}
	}
}

func TestSpawnAttach_GhosttyExecFailureReturnsWrappedError(t *testing.T) {
	c, _ := newSpawnClient(t, []byte("ghostty: error"), errors.New("exit status 1"))
	t.Setenv("TMUX", "")
	t.Setenv("TERM_PROGRAM", "ghostty")
	err := c.SpawnAttach("ct-mayor")
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrUnknownTerminal) {
		t.Errorf("Ghostty failure must not return ErrUnknownTerminal: %v", err)
	}
	// Both macOS (osascript) and Linux (ghostty CLI) paths name the program in
	// the error, but the keyword differs per platform.
	if runtime.GOOS == "linux" {
		if !strings.Contains(err.Error(), "ghostty") {
			t.Errorf("error should mention ghostty: %v", err)
		}
	} else {
		if !strings.Contains(err.Error(), "osascript") {
			t.Errorf("error should mention osascript: %v", err)
		}
	}
}

func TestSpawnAttach_iTerm(t *testing.T) {
	c, argv := newSpawnClient(t, nil, nil)
	t.Setenv("TMUX", "")
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
	t.Setenv("TMUX", "")
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
	t.Setenv("TMUX", "")
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
	t.Setenv("TMUX", "")
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

// --- Linux terminal tests ---

func TestSpawnAttach_GnomeTerminal(t *testing.T) {
	c, argv := newSpawnClient(t, nil, nil)
	t.Setenv("TMUX", "")
	t.Setenv("TERM_PROGRAM", "gnome-terminal")
	if err := c.SpawnAttach("ct-mayor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*argv) == 0 || (*argv)[0] != "gnome-terminal" {
		t.Errorf("expected gnome-terminal spawn, got %v", *argv)
	}
	joined := strings.Join(*argv, " ")
	if !strings.Contains(joined, "ct-mayor") {
		t.Errorf("argv missing session name: %s", joined)
	}
}

func TestSpawnAttach_Alacritty(t *testing.T) {
	c, argv := newSpawnClient(t, nil, nil)
	t.Setenv("TMUX", "")
	t.Setenv("TERM_PROGRAM", "alacritty")
	if err := c.SpawnAttach("ct-mayor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*argv) == 0 || (*argv)[0] != "alacritty" {
		t.Errorf("expected alacritty spawn, got %v", *argv)
	}
}

func TestSpawnAttach_Kitty(t *testing.T) {
	c, argv := newSpawnClient(t, nil, nil)
	t.Setenv("TMUX", "")
	t.Setenv("TERM_PROGRAM", "kitty")
	if err := c.SpawnAttach("ct-mayor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*argv) == 0 || (*argv)[0] != "kitty" {
		t.Errorf("expected kitty spawn, got %v", *argv)
	}
}

func TestSpawnAttach_Wezterm(t *testing.T) {
	c, argv := newSpawnClient(t, nil, nil)
	t.Setenv("TMUX", "")
	t.Setenv("TERM_PROGRAM", "wezterm")
	if err := c.SpawnAttach("ct-mayor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*argv) == 0 || (*argv)[0] != "wezterm" {
		t.Errorf("expected wezterm spawn, got %v", *argv)
	}
}

func TestSpawnAttach_Foot(t *testing.T) {
	c, argv := newSpawnClient(t, nil, nil)
	t.Setenv("TMUX", "")
	t.Setenv("TERM_PROGRAM", "foot")
	if err := c.SpawnAttach("ct-mayor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*argv) == 0 || (*argv)[0] != "foot" {
		t.Errorf("expected foot spawn, got %v", *argv)
	}
}

func TestSpawnAttach_Xterm(t *testing.T) {
	c, argv := newSpawnClient(t, nil, nil)
	t.Setenv("TMUX", "")
	t.Setenv("TERM_PROGRAM", "xterm")
	if err := c.SpawnAttach("ct-mayor"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*argv) == 0 || (*argv)[0] != "xterm" {
		t.Errorf("expected xterm spawn, got %v", *argv)
	}
}

func TestSpawnAttach_LinuxFailureReturnsWrappedError(t *testing.T) {
	c, _ := newSpawnClient(t, []byte("gnome-terminal: not found"), errors.New("exit status 1"))
	t.Setenv("TMUX", "")
	t.Setenv("TERM_PROGRAM", "gnome-terminal")
	err := c.SpawnAttach("ct-mayor")
	if err == nil {
		t.Fatal("expected error from failed gnome-terminal spawn")
	}
	if errors.Is(err, ErrUnknownTerminal) {
		t.Errorf("gnome-terminal failure must not return ErrUnknownTerminal: %v", err)
	}
	if !strings.Contains(err.Error(), "gnome-terminal") {
		t.Errorf("error should mention gnome-terminal: %v", err)
	}
}

// --- detectTerminalProgram tests ---

func TestDetectTerminalProgram_outsideTmux_returnsInheritedEnv(t *testing.T) {
	c, _ := newSpawnClient(t, nil, nil)
	t.Setenv("TMUX", "")
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	got := c.detectTerminalProgram()
	if got != "iTerm.app" {
		t.Errorf("expected iTerm.app, got %q", got)
	}
}

func TestDetectTerminalProgram_inTmux_clientPIDEmpty_fallsBackToEnv(t *testing.T) {
	c, _ := newSpawnClient(t, nil, nil)
	// capture returns empty output → pid is ""
	c.capture = func(...string) ([]byte, error) { return []byte(""), nil }
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	got := c.detectTerminalProgram()
	if got != "Apple_Terminal" {
		t.Errorf("expected Apple_Terminal fallback, got %q", got)
	}
}

func TestDetectTerminalProgram_inTmux_readEnvReturnsTermProgram(t *testing.T) {
	c, _ := newSpawnClient(t, nil, nil)
	c.capture = func(...string) ([]byte, error) { return []byte("42"), nil }
	c.readEnv = func(pid string) (map[string]string, error) {
		if pid != "42" {
			return nil, nil
		}
		return map[string]string{"TERM_PROGRAM": "kitty"}, nil
	}
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")
	t.Setenv("TERM_PROGRAM", "Apple_Terminal") // should be overridden by client env
	got := c.detectTerminalProgram()
	if got != "kitty" {
		t.Errorf("expected kitty from client env, got %q", got)
	}
}

func TestDetectTerminalProgram_inTmux_fallbackViaTermVar(t *testing.T) {
	c, _ := newSpawnClient(t, nil, nil)
	c.capture = func(...string) ([]byte, error) { return []byte("99"), nil }
	c.readEnv = func(string) (map[string]string, error) {
		// gnome-terminal doesn't set TERM_PROGRAM; it sets TERM=xterm-256color
		return map[string]string{"TERM": "xterm-kitty"}, nil
	}
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")
	t.Setenv("TERM_PROGRAM", "")
	got := c.detectTerminalProgram()
	if got != "kitty" {
		t.Errorf("expected kitty derived from TERM, got %q", got)
	}
}

func TestDetectTerminalProgram_inTmux_readEnvFails_fallsBackToEnv(t *testing.T) {
	c, _ := newSpawnClient(t, nil, nil)
	c.capture = func(...string) ([]byte, error) { return []byte("42"), nil }
	c.readEnv = func(string) (map[string]string, error) {
		return nil, errors.New("permission denied")
	}
	t.Setenv("TMUX", "/tmp/tmux-1000/default,12345,0")
	t.Setenv("TERM_PROGRAM", "wezterm")
	got := c.detectTerminalProgram()
	if got != "wezterm" {
		t.Errorf("expected wezterm fallback on readEnv error, got %q", got)
	}
}

// --- SpawnAttachWith override tests ---

func TestSpawnAttachWith_overrideBypassesDetection(t *testing.T) {
	c, argv := newSpawnClient(t, nil, nil)
	t.Setenv("TMUX", "")
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	// Override to kitty — should open kitty regardless of TERM_PROGRAM.
	if err := c.spawnAttachWith("ct-mayor", "kitty"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(*argv) == 0 || (*argv)[0] != "kitty" {
		t.Errorf("expected kitty spawn via override, got %v", *argv)
	}
}

// --- termFromBareTERM tests ---

func TestTermFromBareTERM(t *testing.T) {
	cases := []struct{ term, want string }{
		{"xterm-kitty", "kitty"},
		{"wezterm", "wezterm"},
		{"foot", "foot"},
		{"alacritty", "alacritty"},
		{"xterm-256color", ""},
		{"", ""},
	}
	for _, tc := range cases {
		got := termFromBareTERM(tc.term)
		if got != tc.want {
			t.Errorf("termFromBareTERM(%q) = %q, want %q", tc.term, got, tc.want)
		}
	}
}
