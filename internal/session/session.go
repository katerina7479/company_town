package session

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// SessionPrefix is prepended to all Company Town tmux session names.
	SessionPrefix = "ct-"
)

// SessionName returns the tmux session name for an agent.
func SessionName(agentName string) string {
	return SessionPrefix + agentName
}

// existsFn is the exec seam for Exists. Tests swap this to control session
// presence without requiring a real tmux process.
var existsFn = func(name string) bool {
	return exec.Command("tmux", "has-session", "-t", name).Run() == nil
}

// Exists checks if a tmux session exists.
func Exists(name string) bool {
	return existsFn(name)
}

// AgentSessionConfig holds everything needed to launch an agent session.
type AgentSessionConfig struct {
	Name      string            // tmux session name (e.g. "ct-mayor")
	WorkDir   string            // project root
	Model     string            // claude model
	AgentDir  string            // .company_town/agents/<role>/ — contains CLAUDE.md
	Prompt    string            // initial prompt to send to Claude Code
	EnvVars   map[string]string // extra environment variables for the session
	AgentType string            // agent type for status bar coloring; derived from Name if empty
}

// CreateInteractive creates a tmux session with Claude Code in interactive mode.
// It provisions a .claude/ dir inside the agent's directory with settings.json,
// then uses --settings so Claude Code auto-discovers the CLAUDE.md there.
func CreateInteractive(cfg AgentSessionConfig) error {
	if Exists(cfg.Name) {
		return fmt.Errorf("session %s already exists", cfg.Name)
	}

	// Ensure .claude/settings.json exists in the agent dir
	if err := provisionClaudeSettings(cfg.AgentDir); err != nil {
		return fmt.Errorf("provisioning claude settings: %w", err)
	}

	settingsPath := filepath.Join(cfg.AgentDir, ".claude", "settings.json")

	// Build the claude command
	parts := []string{
		"claude",
		"--dangerously-skip-permissions",
		"--model", shellQuote(cfg.Model),
		"--name", shellQuote(cfg.Name),
		"--settings", shellQuote(settingsPath),
	}

	// Add initial prompt if provided
	if cfg.Prompt != "" {
		parts = append(parts, shellQuote(cfg.Prompt))
	}

	claudeCmd := strings.Join(parts, " ")

	tmuxArgs := []string{"new-session", "-d", "-s", cfg.Name, "-c", cfg.WorkDir}
	for k, v := range cfg.EnvVars {
		tmuxArgs = append(tmuxArgs, "-e", k+"="+v)
	}
	tmuxArgs = append(tmuxArgs, claudeCmd)

	cmd := exec.Command("tmux", tmuxArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("creating tmux session %s: %w", cfg.Name, err)
	}

	agentType := cfg.AgentType
	if agentType == "" {
		agentType = AgentTypeFromSessionName(cfg.Name)
	}
	_ = ApplyStatusBar(cfg.Name, agentType)

	return nil
}

// provisionClaudeSettings creates a .claude/settings.json in the agent directory
// so Claude Code discovers the CLAUDE.md there.
func provisionClaudeSettings(agentDir string) error {
	claudeDir := filepath.Join(agentDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		return err
	}

	settingsPath := filepath.Join(claudeDir, "settings.json")
	if _, err := os.Stat(settingsPath); err == nil {
		return nil // already exists
	}

	settings := map[string]interface{}{
		"permissions": map[string]interface{}{
			"allow": []string{
				"Bash(make:*)",
				"Bash(go:*)",
				"Bash(git:*)",
				"Bash(gt:*)",
				"Bash(ct:*)",
			},
		},
	}

	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(settingsPath, data, 0644)
}

// Attach attaches the current terminal to a tmux session.
// This replaces the current process.
func Attach(name string) error {
	if !Exists(name) {
		return fmt.Errorf("session %s does not exist", name)
	}

	tmux, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("tmux not found: %w", err)
	}

	return execSyscall(tmux, []string{"tmux", "attach-session", "-t", name})
}

// Kill destroys a tmux session.
func Kill(name string) error {
	if !Exists(name) {
		return nil // already gone
	}

	cmd := exec.Command("tmux", "kill-session", "-t", name)
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("killing session %s: %w", name, err)
	}
	return nil
}

// ListCompanyTown returns all tmux sessions with the ct- prefix.
func ListCompanyTown() ([]string, error) {
	cmd := exec.Command("tmux", "list-sessions", "-F", "#{session_name}")
	output, err := cmd.Output()
	if err != nil {
		return nil, nil //nolint:nilerr // tmux not running means no sessions; return empty list
	}

	var sessions []string
	for _, line := range strings.Split(strings.TrimSpace(string(output)), "\n") {
		if strings.HasPrefix(line, SessionPrefix) {
			sessions = append(sessions, line)
		}
	}
	return sessions, nil
}

// tmuxSendExec is the exec seam for SendKeys. Tests swap this to capture
// send-keys calls without spawning a real tmux process.
var tmuxSendExec = func(args ...string) error {
	return exec.Command("tmux", args...).Run()
}

// sendKeySleepFn is the sleep seam between the literal-text send and the Enter
// keystroke. Tests swap this to a no-op to avoid the 150 ms delay.
var sendKeySleepFn = func() { time.Sleep(150 * time.Millisecond) }

// SendKeys sends keystrokes to a tmux session.
//
// It first sends C-c to clear any accumulated input in the pane — this prevents
// nudge messages from piling up in the input box when the session is detached
// and prior send-keys calls were never processed (nc-146).
//
// The message text and the Enter keystroke are sent as two separate invocations
// with a brief pause in between. Sending them in a single call looks like a
// paste to Claude Code's input handler, causing the trailing Enter to be
// consumed as a literal newline rather than a submit keypress when the pane is
// mid-response (nc-153).
func SendKeys(name, keys string) error {
	if !Exists(name) {
		return fmt.Errorf("session %s does not exist", name)
	}

	// Best-effort clear: interrupt any pending input so the message lands on a
	// clean line. Non-fatal if this fails (e.g., the pane is in a state where
	// C-c is ignored).
	_ = tmuxSendExec("send-keys", "-t", name, "C-c")

	// Send the message text using the -l (literal) flag so each character is
	// injected individually rather than interpreted as tmux key names or treated
	// as a bracketed-paste sequence.
	if err := tmuxSendExec("send-keys", "-t", name, "-l", keys); err != nil {
		return err
	}

	// Brief pause so the input handler can settle after receiving the text.
	// Without this, the Enter arrives while the paste event is still being
	// processed and is swallowed as a literal newline instead of triggering
	// submit.
	sendKeySleepFn()

	// Send Enter as its own call so Claude Code's input handler sees it as a
	// distinct keystroke, not a continuation of the pasted text.
	return tmuxSendExec("send-keys", "-t", name, "Enter")
}

func shellQuote(s string) string {
	escaped := strings.ReplaceAll(s, "'", "'\\''")
	return "'" + escaped + "'"
}

// ErrUnknownTerminal is returned by SpawnAttach when $TERM_PROGRAM is
// unrecognized. Callers should fall back to in-place tmux attach.
var ErrUnknownTerminal = fmt.Errorf("unrecognized TERM_PROGRAM; falling back to in-place attach")

// spawnAttachExec is the exec seam for terminal spawn functions.
// Tests swap this to capture argv and stub outputs.
var spawnAttachExec = func(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.CombinedOutput()
}

// SpawnAttach opens a new terminal window running tmux attach -t sessionName.
// The calling process keeps running. Supported: Ghostty, iTerm2, Terminal.app.
// Returns ErrUnknownTerminal for unrecognized $TERM_PROGRAM.
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

// attachArgv returns the tmux attach command string for use in a terminal script.
func attachArgv(sessionName string) string {
	return "tmux attach-session -t " + shellQuote(sessionName)
}

// osascriptQuote wraps s in AppleScript string delimiters, escaping double quotes.
func osascriptQuote(s string) string {
	escaped := strings.ReplaceAll(s, `"`, `\" & quote & "`)
	return `"` + escaped + `"`
}

func spawnGhostty(sessionName string) error {
	script := fmt.Sprintf(`
tell application "Ghostty"
	activate
	tell application "System Events"
		tell process "Ghostty"
			keystroke "n" using command down
			delay 0.3
			keystroke %s
			key code 52
		end tell
	end tell
end tell`, osascriptQuote(attachArgv(sessionName)))
	out, err := spawnAttachExec("osascript", "-e", script)
	if err != nil {
		return fmt.Errorf("ghostty osascript: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func spawnITerm(sessionName string) error {
	script := fmt.Sprintf(`
tell application "iTerm"
	activate
	set newWin to (create window with default profile)
	tell current session of newWin
		write text %s
	end tell
end tell`, osascriptQuote(attachArgv(sessionName)))
	out, err := spawnAttachExec("osascript", "-e", script)
	if err != nil {
		return fmt.Errorf("iTerm osascript: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func spawnAppleTerminal(sessionName string) error {
	script := fmt.Sprintf(`
tell application "Terminal"
	activate
	do script %s
end tell`, osascriptQuote(attachArgv(sessionName)))
	out, err := spawnAttachExec("osascript", "-e", script)
	if err != nil {
		return fmt.Errorf("Terminal.app osascript: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
