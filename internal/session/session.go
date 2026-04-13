package session

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	// SessionPrefix is prepended to all Company Town tmux session names.
	SessionPrefix = "ct-"
)

// SessionName returns the tmux session name for an agent.
func SessionName(agentName string) string {
	return SessionPrefix + agentName
}

// Exists checks if a tmux session exists.
func Exists(name string) bool {
	cmd := exec.Command("tmux", "has-session", "-t", name)
	return cmd.Run() == nil
}

// AgentSessionConfig holds everything needed to launch an agent session.
type AgentSessionConfig struct {
	Name     string            // tmux session name (e.g. "ct-mayor")
	WorkDir  string            // project root
	Model    string            // claude model
	AgentDir string            // .company_town/agents/<role>/ — contains CLAUDE.md
	Prompt   string            // initial prompt to send to Claude Code
	EnvVars  map[string]string // extra environment variables for the session
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
		return nil, nil
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
func SendKeys(name, keys string) error {
	if !Exists(name) {
		return fmt.Errorf("session %s does not exist", name)
	}

	cmd := exec.Command("tmux", "send-keys", "-t", name, keys, "Enter")
	return cmd.Run()
}

func shellQuote(s string) string {
	escaped := strings.ReplaceAll(s, "'", "'\\''")
	return "'" + escaped + "'"
}
