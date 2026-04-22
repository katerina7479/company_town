package session

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/katerina7479/company_town/internal/runner"
)

// SessionPrefix is prepended to all Company Town tmux session names.
// It is a var so that callers can override it from config (session_prefix field).
// The default "ct-" preserves backwards compatibility for existing projects.
var SessionPrefix = "ct-"

// Client abstracts the tmux operations that callers need. The real
// implementation calls tmux; test implementations substitute controlled
// behaviour without swapping package-level variables.
type Client interface {
	Exists(name string) bool
	SendKeys(name, keys string) error
	Kill(name string) error
	SpawnAttach(name string) error
	CapturePane(name string) (string, error)
}

// tmuxClient is the real Client. Fields check, exec, sleep, spawn, and capture
// hold the exec seams that were previously package-level variables; moving them
// onto the struct allows each test to create an isolated instance without
// mutating global state.
type tmuxClient struct {
	check   func(name string) bool                            // tmux has-session
	exec    func(args ...string) error                        // tmux send-keys / kill-session
	sleep   func()                                            // pause inside SendKeys
	spawn   func(prog string, args ...string) ([]byte, error) // osascript etc.
	capture func(args ...string) ([]byte, error)              // tmux capture-pane
}

// New returns a Client backed by real tmux.
func New() Client {
	return &tmuxClient{
		check: func(name string) bool {
			return exec.Command("tmux", "has-session", "-t", name).Run() == nil
		},
		exec: func(args ...string) error {
			return exec.Command("tmux", args...).Run()
		},
		sleep: func() { time.Sleep(150 * time.Millisecond) },
		spawn: func(prog string, args ...string) ([]byte, error) {
			return exec.Command(prog, args...).CombinedOutput()
		},
		capture: func(args ...string) ([]byte, error) {
			return exec.Command("tmux", args...).Output()
		},
	}
}

// defaultClient is used by the package-level functions so that callers that
// have not yet migrated to injecting a Client continue to work unchanged.
var defaultClient = New()

// SessionName returns the tmux session name for an agent.
func SessionName(agentName string) string {
	return SessionPrefix + agentName
}

// Exists checks if a tmux session exists.
func Exists(name string) bool { return defaultClient.Exists(name) }

func (c *tmuxClient) Exists(name string) bool { return c.check(name) }

// AgentSessionConfig holds everything needed to launch an agent session.
type AgentSessionConfig struct {
	Name      string            // tmux session name (e.g. "ct-mayor")
	WorkDir   string            // project root
	Model     string            // claude model
	AgentDir  string            // .company_town/agents/<role>/ — contains CLAUDE.md
	Prompt    string            // initial prompt to send to Claude Code
	EnvVars   map[string]string // extra environment variables for the session
	AgentType string            // agent type for status bar coloring; derived from Name if empty
	Runner    runner.Runner     // agent CLI runtime; defaults to runner.Default() (ClaudeRunner) when nil
}

// CreateInteractive creates a tmux session with the configured agent CLI runtime.
// It provisions any runner-specific config files inside the agent's directory,
// then launches the runner command inside a new tmux session.
func CreateInteractive(cfg AgentSessionConfig) error {
	if Exists(cfg.Name) {
		return fmt.Errorf("session %s already exists", cfg.Name)
	}

	r := cfg.Runner
	if r == nil {
		r = runner.Default()
	}

	if err := r.ProvisionSettings(cfg.AgentDir); err != nil {
		return fmt.Errorf("provisioning runner settings: %w", err)
	}

	settingsPath := r.SettingsPath(cfg.AgentDir)
	agentCmd := r.Command(cfg.Model, cfg.Name, settingsPath, cfg.Prompt)

	tmuxArgs := []string{"new-session", "-d", "-s", cfg.Name, "-c", cfg.WorkDir}
	for k, v := range cfg.EnvVars {
		tmuxArgs = append(tmuxArgs, "-e", k+"="+v)
	}
	tmuxArgs = append(tmuxArgs, agentCmd)

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
func Kill(name string) error { return defaultClient.Kill(name) }

func (c *tmuxClient) Kill(name string) error {
	if !c.check(name) {
		return nil // already gone
	}
	if err := c.exec("kill-session", "-t", name); err != nil {
		return fmt.Errorf("killing session %s: %w", name, err)
	}
	return nil
}

// CapturePane returns the visible text content of a tmux pane as a string.
// It uses `tmux capture-pane -p -t <name>` which writes the pane content to
// stdout. Returns an error if the session does not exist.
func CapturePane(name string) (string, error) { return defaultClient.CapturePane(name) }

func (c *tmuxClient) CapturePane(name string) (string, error) {
	if !c.check(name) {
		return "", fmt.Errorf("session %s does not exist", name)
	}
	out, err := c.capture("capture-pane", "-p", "-t", name)
	if err != nil {
		return "", fmt.Errorf("capturing pane %s: %w", name, err)
	}
	return string(out), nil
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

// SendKeys sends keystrokes to a tmux session.
//
// It first sends C-u (readline kill-line) to clear any accumulated input in
// the pane. This prevents nudge messages from piling up in the input box when
// the session is detached (nc-146). C-u is used instead of C-c because C-c
// sends SIGINT and can abort a running tool call; C-u only clears the input
// line (nc-162).
//
// The message text and the Enter keystroke are sent as two separate invocations
// with a brief pause in between. Sending them in a single call looks like a
// paste to Claude Code's input handler, causing the trailing Enter to be
// consumed as a literal newline rather than a submit keypress when the pane is
// mid-response (nc-153).
func SendKeys(name, keys string) error { return defaultClient.SendKeys(name, keys) }

func (c *tmuxClient) SendKeys(name, keys string) error {
	if !c.check(name) {
		return fmt.Errorf("session %s does not exist", name)
	}

	// Best-effort clear: kill the current input line so the message lands on a
	// clean line. C-u (readline kill-line) erases any buffered text without
	// sending SIGINT, so it does not interrupt a running tool call. C-c would
	// abort the current operation in Claude Code — wrong behaviour when the
	// agent is mid-response (nc-162). Non-fatal if this fails.
	_ = c.exec("send-keys", "-t", name, "C-u")

	// Send the message text using the -l (literal) flag so each character is
	// injected individually rather than interpreted as tmux key names or treated
	// as a bracketed-paste sequence.
	if err := c.exec("send-keys", "-t", name, "-l", keys); err != nil {
		return fmt.Errorf("sending keys to session %s: %w", name, err)
	}

	// Brief pause so the input handler can settle after receiving the text.
	// Without this, the Enter arrives while the paste event is still being
	// processed and is swallowed as a literal newline instead of triggering
	// submit.
	c.sleep()

	// Send Enter as its own call so Claude Code's input handler sees it as a
	// distinct keystroke, not a continuation of the pasted text.
	if err := c.exec("send-keys", "-t", name, "Enter"); err != nil {
		return fmt.Errorf("sending enter to session %s: %w", name, err)
	}
	return nil
}

func shellQuote(s string) string {
	escaped := strings.ReplaceAll(s, "'", "'\\''")
	return "'" + escaped + "'"
}

// ErrUnknownTerminal is returned by SpawnAttach when $TERM_PROGRAM is
// unrecognized. Callers should fall back to in-place tmux attach.
var ErrUnknownTerminal = fmt.Errorf("unrecognized TERM_PROGRAM; falling back to in-place attach")

// SpawnAttach opens a new terminal window running tmux attach -t sessionName.
// The calling process keeps running. Supported: Ghostty, iTerm2, Terminal.app.
// Returns ErrUnknownTerminal for unrecognized $TERM_PROGRAM.
func SpawnAttach(sessionName string) error { return defaultClient.SpawnAttach(sessionName) }

func (c *tmuxClient) SpawnAttach(sessionName string) error {
	termProg := strings.TrimSpace(os.Getenv("TERM_PROGRAM"))
	switch termProg {
	case "ghostty":
		return c.spawnGhostty(sessionName)
	case "iTerm.app":
		return c.spawnITerm(sessionName)
	case "Apple_Terminal":
		return c.spawnAppleTerminal(sessionName)
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

func (c *tmuxClient) spawnGhostty(sessionName string) error {
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
	out, err := c.spawn("osascript", "-e", script)
	if err != nil {
		return fmt.Errorf("ghostty osascript: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *tmuxClient) spawnITerm(sessionName string) error {
	script := fmt.Sprintf(`
tell application "iTerm"
	activate
	set newWin to (create window with default profile)
	tell current session of newWin
		write text %s
	end tell
end tell`, osascriptQuote(attachArgv(sessionName)))
	out, err := c.spawn("osascript", "-e", script)
	if err != nil {
		return fmt.Errorf("iTerm osascript: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func (c *tmuxClient) spawnAppleTerminal(sessionName string) error {
	script := fmt.Sprintf(`
tell application "Terminal"
	activate
	do script %s
end tell`, osascriptQuote(attachArgv(sessionName)))
	out, err := c.spawn("osascript", "-e", script)
	if err != nil {
		return fmt.Errorf("Terminal.app osascript: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
