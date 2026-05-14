package runner_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/katerina7479/company_town/internal/runner"
)

func TestClaudeRunner_Command_includesRequiredFlags(t *testing.T) {
	r := ClaudeRunner{}
	cmd := r.Command("claude-opus-4-6", "ct-mayor", "/path/to/settings.json", "do the thing")
	for _, want := range []string{
		"claude",
		"--dangerously-skip-permissions",
		"--model",
		"claude-opus-4-6",
		"--name",
		"ct-mayor",
		"--settings",
		"/path/to/settings.json",
		"do the thing",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("expected %q in command %q", want, cmd)
		}
	}
}

func TestClaudeRunner_Command_omitsPromptWhenEmpty(t *testing.T) {
	r := ClaudeRunner{}
	cmd := r.Command("claude-sonnet-4-6", "ct-prole", "/settings.json", "")
	// Should still have the core flags.
	if !strings.Contains(cmd, "claude") {
		t.Errorf("expected 'claude' in command: %q", cmd)
	}
	// Trailing prompt arg should be absent when prompt is empty.
	parts := strings.Fields(cmd)
	last := parts[len(parts)-1]
	if last == "''" {
		t.Errorf("empty prompt left a bare '' at end of command: %q", cmd)
	}
}

func TestClaudeRunner_SettingsPath(t *testing.T) {
	r := ClaudeRunner{}
	got := r.SettingsPath("/some/agent/dir")
	want := filepath.Join("/some/agent/dir", ".claude", "settings.json")
	if got != want {
		t.Errorf("SettingsPath = %q, want %q", got, want)
	}
}

func TestClaudeRunner_ProvisionSettings_writesJSON(t *testing.T) {
	dir := t.TempDir()
	r := ClaudeRunner{}
	if err := r.ProvisionSettings(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".claude", "settings.json"))
	if err != nil {
		t.Fatalf("settings.json not created: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("settings.json is not valid JSON: %v", err)
	}
	if _, ok := m["permissions"]; !ok {
		t.Error("settings.json missing 'permissions' key")
	}
}

func TestClaudeRunner_ProvisionSettings_idempotent(t *testing.T) {
	dir := t.TempDir()
	r := ClaudeRunner{}
	if err := r.ProvisionSettings(dir); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(dir, ".claude", "settings.json")
	custom := []byte(`{"custom":true}`)
	if err := os.WriteFile(settingsPath, custom, 0644); err != nil {
		t.Fatal(err)
	}
	if err := r.ProvisionSettings(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(custom) {
		t.Errorf("ProvisionSettings overwrote existing settings; got %q", string(data))
	}
}

func TestDefault_returnsClaudeRunner(t *testing.T) {
	r := Default()
	if _, ok := r.(ClaudeRunner); !ok {
		t.Errorf("Default() = %T, want ClaudeRunner", r)
	}
}

func TestNew_defaults(t *testing.T) {
	for _, name := range []string{"", "claude"} {
		r, err := New(name)
		if err != nil {
			t.Fatalf("New(%q): unexpected error: %v", name, err)
		}
		if _, ok := r.(ClaudeRunner); !ok {
			t.Errorf("New(%q): expected ClaudeRunner, got %T", name, r)
		}
	}
}

func TestNew_codex(t *testing.T) {
	r, err := New("codex")
	if err != nil {
		t.Fatalf("New(codex): %v", err)
	}
	if _, ok := r.(CodexRunner); !ok {
		t.Errorf("expected CodexRunner, got %T", r)
	}
}

func TestNew_unsupported(t *testing.T) {
	_, err := New("emacs")
	if err == nil {
		t.Fatal("expected error for unsupported runner")
	}
	if !strings.Contains(err.Error(), "emacs") {
		t.Errorf("error should name the unsupported value, got %v", err)
	}
}

func TestCodexRunner_Command_includesRequiredFlags(t *testing.T) {
	cmd := CodexRunner{}.Command("o3-mini", "ct-reviewer", "/path/to/.codex/config.json", "do the thing")
	for _, want := range []string{
		"codex",
		"--approval-policy",
		"full-auto",
		"--model",
		"o3-mini",
		"do the thing",
	} {
		if !strings.Contains(cmd, want) {
			t.Errorf("expected %q in command %q", want, cmd)
		}
	}
}

func TestCodexRunner_Command_omitsPromptWhenEmpty(t *testing.T) {
	cmd := CodexRunner{}.Command("o3-mini", "ct-reviewer", "", "")
	if !strings.Contains(cmd, "codex") {
		t.Errorf("expected 'codex' in command: %q", cmd)
	}
	parts := strings.Fields(cmd)
	last := parts[len(parts)-1]
	if last == "''" {
		t.Errorf("empty prompt left a bare '' at end of command: %q", cmd)
	}
}

func TestCodexRunner_SettingsPath(t *testing.T) {
	got := CodexRunner{}.SettingsPath("/some/agent/dir")
	want := filepath.Join("/some/agent/dir", ".codex", "config.json")
	if got != want {
		t.Errorf("SettingsPath = %q, want %q", got, want)
	}
}

func TestCodexRunner_ProvisionSettings_writesJSON(t *testing.T) {
	dir := t.TempDir()
	r := CodexRunner{}
	if err := r.ProvisionSettings(dir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, ".codex", "config.json"))
	if err != nil {
		t.Fatalf("config.json not created: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("config.json is not valid JSON: %v", err)
	}
	if _, ok := m["approvalPolicy"]; !ok {
		t.Error("config.json missing 'approvalPolicy' key")
	}
}

func TestCodexRunner_ProvisionSettings_idempotent(t *testing.T) {
	dir := t.TempDir()
	r := CodexRunner{}
	if err := r.ProvisionSettings(dir); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dir, ".codex", "config.json")
	custom := []byte(`{"custom":true}`)
	if err := os.WriteFile(configPath, custom, 0644); err != nil {
		t.Fatal(err)
	}
	if err := r.ProvisionSettings(dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != string(custom) {
		t.Errorf("ProvisionSettings overwrote existing config; got %q", string(data))
	}
}

func TestClaudeRunner_StuckPromptPatterns_includesPermissionPrompt(t *testing.T) {
	patterns := ClaudeRunner{}.StuckPromptPatterns()
	if len(patterns) == 0 {
		t.Fatal("expected non-empty pattern set from ClaudeRunner")
	}
	// Must include the canonical Claude permission-prompt substring "allow ".
	var found bool
	for _, p := range patterns {
		if strings.EqualFold(p.Text, "allow ") {
			found = true
			if !p.Anchored {
				t.Error("'allow ' pattern should be anchored to prevent false positives on code diffs")
			}
			break
		}
	}
	if !found {
		t.Error("ClaudeRunner.StuckPromptPatterns() must include 'allow ' (anchored)")
	}
}

func TestClaudeRunner_StuckPromptPatterns_includesYN(t *testing.T) {
	patterns := ClaudeRunner{}.StuckPromptPatterns()
	var found bool
	for _, p := range patterns {
		if strings.Contains(p.Text, "y/n") {
			found = true
			break
		}
	}
	if !found {
		t.Error("ClaudeRunner.StuckPromptPatterns() must include a (y/n) pattern")
	}
}

func TestCodexRunner_StuckPromptPatterns_nonEmpty(t *testing.T) {
	patterns := CodexRunner{}.StuckPromptPatterns()
	if len(patterns) == 0 {
		t.Fatal("expected non-empty pattern set from CodexRunner")
	}
}

func TestCodexRunner_StuckPromptPatterns_includesCodexApprovalPrompt(t *testing.T) {
	patterns := CodexRunner{}.StuckPromptPatterns()
	var found bool
	for _, p := range patterns {
		if strings.Contains(strings.ToLower(p.Text), "apply these changes") {
			found = true
			break
		}
	}
	if !found {
		t.Error("CodexRunner.StuckPromptPatterns() must include 'apply these changes' (Codex approval prompt)")
	}
}

func TestCodexRunner_StuckPromptPatterns_doesNotIncludeClaudeAllowPattern(t *testing.T) {
	// Codex's pattern set must not include the Claude-specific "allow " anchored
	// pattern, since Codex agents would produce different pane content.
	patterns := CodexRunner{}.StuckPromptPatterns()
	for _, p := range patterns {
		if strings.EqualFold(p.Text, "allow ") && p.Anchored {
			t.Error("CodexRunner should not include Claude's anchored 'allow ' pattern")
		}
	}
}
