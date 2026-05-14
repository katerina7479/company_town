// Package runner defines the Runner interface and built-in implementations for
// agent CLI runtimes. Company Town currently ships ClaudeRunner (the claude CLI);
// additional runners (e.g. Codex) will be added in follow-up tickets under nc-308.
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

// New returns the Runner implementation for the given name.
// "" or "claude" → ClaudeRunner. "codex" → CodexRunner.
// Any other value returns an error naming the unsupported value.
func New(name string) (Runner, error) {
	switch name {
	case "", "claude":
		return ClaudeRunner{}, nil
	case "codex":
		return CodexRunner{}, nil
	default:
		return nil, fmt.Errorf("unsupported runner %q (supported: claude, codex)", name)
	}
}

// CodexRunner drives the codex CLI (OpenAI Codex).
//
// CLI invocation: codex --approval-policy full-auto --model <model> [prompt]
// Config: .codex/config.json in the agent's working directory, read automatically
// by the codex binary (no explicit --config flag needed). settingsPath in Command
// is the path ProvisionSettings wrote; Codex finds it via CWD convention so the
// flag is omitted from the command string.
type CodexRunner struct{}

// Command builds the codex command string for tmux.
// settingsPath is written to agentDir/.codex/config.json and picked up by Codex
// automatically from CWD; no explicit flag is required.
func (CodexRunner) Command(model, sessionName, settingsPath, prompt string) string {
	parts := []string{
		"codex",
		"--approval-policy", "full-auto",
		"--model", shellQuote(model),
	}
	if prompt != "" {
		parts = append(parts, shellQuote(prompt))
	}
	return strings.Join(parts, " ")
}

// ProvisionSettings creates .codex/config.json in agentDir if it does not
// already exist. Codex reads this file automatically from its working directory.
func (CodexRunner) ProvisionSettings(agentDir string) error {
	codexDir := filepath.Join(agentDir, ".codex")
	if err := os.MkdirAll(codexDir, 0750); err != nil {
		return fmt.Errorf("creating .codex dir %s: %w", codexDir, err)
	}

	configPath := filepath.Join(codexDir, "config.json")
	if _, err := os.Stat(configPath); err == nil {
		return nil // already exists — do not overwrite
	}

	cfg := map[string]interface{}{
		"approvalPolicy": "full-auto",
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshalling codex config: %w", err)
	}
	if err := os.WriteFile(configPath, data, 0644); err != nil { //nolint:gosec // configPath is derived from agentDir, not user input
		return fmt.Errorf("writing codex config to %s: %w", configPath, err)
	}
	return nil
}

// SettingsPath returns the .codex/config.json path for agentDir.
func (CodexRunner) SettingsPath(agentDir string) string {
	return filepath.Join(agentDir, ".codex", "config.json")
}

// BaseBashAllowList returns the Bash tool permissions that every agent session
// receives regardless of language. Callers may append language-specific entries.
func BaseBashAllowList() []string {
	return []string{
		"Bash(make:*)",
		"Bash(go:*)",
		"Bash(git:*)",
		"Bash(gt:*)",
		"Bash(ct:*)",
	}
}

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
			"allow": BaseBashAllowList(),
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
