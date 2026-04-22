// Package runner defines the Runner interface and built-in implementations for
// agent CLI runtimes. Company Town currently ships ClaudeRunner (the claude CLI);
// additional runners (e.g. Codex) will be added in follow-up tickets under nc-230.
package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Runner abstracts the agent CLI that Company Town launches inside tmux.
// Each Runner knows how to build its own launch command and provision any
// runner-specific config files that must exist before the session starts.
type Runner interface {
	// Command returns the shell command string to hand to tmux new-session.
	// settingsPath is the path to the runner-specific settings/config file
	// that ProvisionSettings wrote; pass an empty string if the runner has none.
	Command(model, sessionName, settingsPath, prompt string) string

	// ProvisionSettings writes any runner-specific config files into agentDir.
	// Called before the tmux session is created. Implementations must be
	// idempotent: if the files already exist, leave them unchanged.
	ProvisionSettings(agentDir string) error

	// SettingsPath returns the path to the runner-specific settings file inside
	// agentDir, or an empty string if the runner uses no settings file.
	SettingsPath(agentDir string) string
}

// Default returns the default Runner (ClaudeRunner).
func Default() Runner { return ClaudeRunner{} }

// ClaudeRunner drives the claude CLI (Claude Code).
type ClaudeRunner struct{}

// Command builds the claude command string for tmux.
func (ClaudeRunner) Command(model, sessionName, settingsPath, prompt string) string {
	parts := []string{
		"claude",
		"--dangerously-skip-permissions",
		"--model", shellQuote(model),
		"--name", shellQuote(sessionName),
		"--settings", shellQuote(settingsPath),
	}
	if prompt != "" {
		parts = append(parts, shellQuote(prompt))
	}
	return strings.Join(parts, " ")
}

// ProvisionSettings creates .claude/settings.json in agentDir if it does not
// already exist. This is a no-op when the file is already present (e.g. written
// by prole.deployProleSettings before the session is launched).
func (ClaudeRunner) ProvisionSettings(agentDir string) error {
	claudeDir := filepath.Join(agentDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0750); err != nil {
		return fmt.Errorf("creating .claude dir %s: %w", claudeDir, err)
	}

	settingsPath := filepath.Join(claudeDir, "settings.json")
	if _, err := os.Stat(settingsPath); err == nil {
		return nil // already exists — do not overwrite
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
		return fmt.Errorf("marshalling claude settings: %w", err)
	}
	if err := os.WriteFile(settingsPath, data, 0644); err != nil {
		return fmt.Errorf("writing claude settings to %s: %w", settingsPath, err)
	}
	return nil
}

// SettingsPath returns the .claude/settings.json path for agentDir.
func (ClaudeRunner) SettingsPath(agentDir string) string {
	return filepath.Join(agentDir, ".claude", "settings.json")
}

func shellQuote(s string) string {
	escaped := strings.ReplaceAll(s, "'", "'\\''")
	return "'" + escaped + "'"
}
