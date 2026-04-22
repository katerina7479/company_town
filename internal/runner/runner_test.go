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
