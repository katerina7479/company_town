package session

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ErrUnknownTerminal is returned by SpawnAttach when $TERM_PROGRAM is
// unrecognized and no fallback spawn is possible.
var ErrUnknownTerminal = fmt.Errorf("unrecognized TERM_PROGRAM; falling back to in-place attach")

// SpawnAttach opens a new terminal window that runs `tmux attach -t sessionName`.
// The calling process (the dashboard) keeps running. On success the new window
// hosts the tmux session; the CEO detaches with C-b d to close that window.
//
// Supported terminals (macOS only):
//   - Ghostty   (TERM_PROGRAM=ghostty)
//   - iTerm2    (TERM_PROGRAM=iTerm.app)
//   - Terminal.app (TERM_PROGRAM=Apple_Terminal)
//
// Returns ErrUnknownTerminal when $TERM_PROGRAM is not one of the above, so
// callers can fall back to in-place attach.
func SpawnAttach(sessionName string) error {
	termProg := strings.TrimSpace(os.Getenv("TERM_PROGRAM"))

	switch termProg {
	case "ghostty":
		return spawnGhostty(sessionName)
	case "iTerm.app":
		return spawnITerm(sessionName)
	case "Apple_Terminal":
		return spawnAppleTerminal(sessionName)
	default:
		return ErrUnknownTerminal
	}
}

// attachCmd returns the shell command string for tmux attach.
func attachCmd(sessionName string) string {
	return "tmux attach-session -t " + shellQuote(sessionName)
}

// spawnGhostty opens a new Ghostty window. Ghostty does not have a stable
// CLI for opening windows, so we fall through to osascript.
func spawnGhostty(sessionName string) error {
	// Ghostty registers itself as the handler for com.mitchellh.ghostty, which
	// means `open -a Ghostty` would open a window in the running instance but
	// can't inject a command. Use osascript to open a new window instead.
	script := fmt.Sprintf(`
tell application "Ghostty"
	activate
	tell application "System Events" to keystroke "n" using command down
	delay 0.3
	tell application "System Events" to keystroke %s & return
end tell`, osascriptQuote(attachCmd(sessionName)))

	cmd := exec.Command("osascript", "-e", script)
	if err := cmd.Run(); err != nil {
		// Ghostty's AppleScript support can be limited; fall back to a generic
		// osascript approach that opens a new Ghostty process instance.
		return spawnViaOpen("Ghostty", sessionName)
	}
	return nil
}

// spawnITerm opens a new iTerm2 window running the tmux attach command.
func spawnITerm(sessionName string) error {
	script := fmt.Sprintf(`
tell application "iTerm"
	activate
	set newWin to (create window with default profile)
	tell current session of newWin
		write text %s
	end tell
end tell`, osascriptQuote(attachCmd(sessionName)))

	cmd := exec.Command("osascript", "-e", script)
	return cmd.Run()
}

// spawnAppleTerminal opens a new Terminal.app window running the tmux attach command.
func spawnAppleTerminal(sessionName string) error {
	script := fmt.Sprintf(`
tell application "Terminal"
	activate
	do script %s
end tell`, osascriptQuote(attachCmd(sessionName)))

	cmd := exec.Command("osascript", "-e", script)
	return cmd.Run()
}

// spawnViaOpen uses `open -a <app>` to launch the terminal and then sends
// the attach command via osascript keystroke injection. Used as a Ghostty fallback.
func spawnViaOpen(appName, sessionName string) error {
	// First open the app to ensure it's running.
	if err := exec.Command("open", "-a", appName).Run(); err != nil {
		return fmt.Errorf("open -a %s: %w", appName, err)
	}

	// Give the app a moment to open a window.
	script := fmt.Sprintf(`
delay 0.5
tell application "System Events"
	keystroke %s
	key code 52
end tell`, osascriptQuote(attachCmd(sessionName)))

	cmd := exec.Command("osascript", "-e", script)
	return cmd.Run()
}

// osascriptQuote wraps s in AppleScript string delimiters, escaping embedded quotes.
func osascriptQuote(s string) string {
	escaped := strings.ReplaceAll(s, `"`, `\" & quote & "`)
	return `"` + escaped + `"`
}
