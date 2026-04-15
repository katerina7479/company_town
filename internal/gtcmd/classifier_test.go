package gtcmd

import (
	"strings"
	"testing"

	"github.com/katerina7479/company_town/internal/config"
)

// TestRegistry_architectSpec verifies that the architect entry is fully
// populated for the start lifecycle and declares a signal file.
func TestRegistry_architectSpec(t *testing.T) {
	spec, ok := agentRegistry["architect"]
	if !ok {
		t.Fatal("architect not in agentRegistry")
	}
	if spec.agentType != "architect" {
		t.Errorf("agentType = %q, want %q", spec.agentType, "architect")
	}
	if spec.templateType != "architect" {
		t.Errorf("templateType = %q, want %q", spec.templateType, "architect")
	}
	if spec.agentSubDir != "architect" {
		t.Errorf("agentSubDir = %q, want %q", spec.agentSubDir, "architect")
	}
	if !spec.hasSignalFile {
		t.Error("expected architect to have hasSignalFile = true")
	}
	if spec.configFor == nil {
		t.Error("architect configFor must not be nil")
	}
	if spec.promptFn == nil {
		t.Error("architect promptFn must not be nil")
	}
}

// TestRegistry_reviewerSpec verifies that the reviewer entry is fully
// populated for the start lifecycle but does NOT declare a signal file.
func TestRegistry_reviewerSpec(t *testing.T) {
	spec, ok := agentRegistry["reviewer"]
	if !ok {
		t.Fatal("reviewer not in agentRegistry")
	}
	if spec.agentType != "reviewer" {
		t.Errorf("agentType = %q, want %q", spec.agentType, "reviewer")
	}
	if spec.agentSubDir != "reviewer" {
		t.Errorf("agentSubDir = %q, want %q", spec.agentSubDir, "reviewer")
	}
	if spec.hasSignalFile {
		t.Error("reviewer should not have hasSignalFile — it does not use a handoff signal file")
	}
	if spec.configFor == nil {
		t.Error("reviewer configFor must not be nil")
	}
	if spec.promptFn == nil {
		t.Error("reviewer promptFn must not be nil")
	}
}

// TestRegistry_mayorSpec verifies that mayor has a configFor function but
// no start-lifecycle fields, since mayor is not started via gt start.
func TestRegistry_mayorSpec(t *testing.T) {
	spec, ok := agentRegistry["mayor"]
	if !ok {
		t.Fatal("mayor not in agentRegistry")
	}
	if spec.configFor == nil {
		t.Error("mayor configFor must not be nil")
	}
	if spec.agentSubDir != "" {
		t.Errorf("mayor agentSubDir should be empty (not startable via gt start), got %q", spec.agentSubDir)
	}
	if spec.promptFn != nil {
		t.Error("mayor promptFn should be nil — mayor is not started via gt start")
	}
}

// TestRegistry_configForReturnsCorrectConfig verifies that each registered
// agent's configFor returns the right AgentConfig field from the project config.
func TestRegistry_configForReturnsCorrectConfig(t *testing.T) {
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Architect: config.AgentConfig{Model: "model-architect"},
			Reviewer:  config.AgentConfig{Model: "model-reviewer"},
			Mayor:     config.AgentConfig{Model: "model-mayor"},
		},
	}
	cases := []struct {
		name  string
		model string
	}{
		{"architect", "model-architect"},
		{"reviewer", "model-reviewer"},
		{"mayor", "model-mayor"},
	}
	for _, tc := range cases {
		spec, ok := agentRegistry[tc.name]
		if !ok {
			t.Errorf("%s not found in registry", tc.name)
			continue
		}
		ac := spec.configFor(cfg)
		if ac.Model != tc.model {
			t.Errorf("%s: configFor returned model %q, want %q", tc.name, ac.Model, tc.model)
		}
	}
}

// TestRegistry_promptFnContainsTicketPrefix verifies that each startable
// agent's promptFn interpolates the ticket prefix into the returned string.
func TestRegistry_promptFnContainsTicketPrefix(t *testing.T) {
	const prefix = "xz"
	cfg := &config.Config{TicketPrefix: prefix}
	for name, spec := range agentRegistry {
		if spec.promptFn == nil {
			continue // mayor is not startable — no prompt expected
		}
		got := spec.promptFn(cfg)
		if !strings.Contains(got, prefix) {
			t.Errorf("%s: prompt does not contain ticket prefix %q: %q", name, prefix, got)
		}
	}
}
