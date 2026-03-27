package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCompanyTownDir(t *testing.T) {
	got := CompanyTownDir("/some/project")
	want := "/some/project/.company_town"
	if got != want {
		t.Errorf("CompanyTownDir = %q, want %q", got, want)
	}
}

func TestConfigPath(t *testing.T) {
	got := ConfigPath("/some/project")
	want := "/some/project/.company_town/config.json"
	if got != want {
		t.Errorf("ConfigPath = %q, want %q", got, want)
	}
}

func TestLoad_validConfig(t *testing.T) {
	dir := t.TempDir()
	ctDir := filepath.Join(dir, DirName)
	if err := os.MkdirAll(ctDir, 0755); err != nil {
		t.Fatal(err)
	}

	raw := `{
		"version": "1.0.0",
		"ticket_prefix": "XX",
		"project_root": "/tmp/proj",
		"github_repo": "acme/widget",
		"dolt": {"host": "127.0.0.1", "port": 3307, "database": "ct"},
		"log_dir": ".company_town/logs",
		"max_proles": 4,
		"polling_interval_seconds": 60,
		"nudge_cooldown_seconds": 120,
		"context_handoff_threshold": 0.75,
		"agents": {
			"mayor":     {"model": "claude-opus-4-5"},
			"architect": {"model": "claude-opus-4-5"},
			"conductor": {"model": "claude-sonnet-4-5"},
			"prole":     {"model": "claude-sonnet-4-5"},
			"janitor":   {"model": "claude-sonnet-4-5"},
			"artisan":   {"backend": {"model": "claude-sonnet-4-5"}}
		}
	}`
	if err := os.WriteFile(filepath.Join(ctDir, ConfigFile), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if cfg.TicketPrefix != "XX" {
		t.Errorf("TicketPrefix = %q, want %q", cfg.TicketPrefix, "XX")
	}
	if cfg.MaxProles != 4 {
		t.Errorf("MaxProles = %d, want 4", cfg.MaxProles)
	}
	if cfg.Dolt.Port != 3307 {
		t.Errorf("Dolt.Port = %d, want 3307", cfg.Dolt.Port)
	}
	if cfg.Agents.Artisan["backend"].Model != "claude-sonnet-4-5" {
		t.Errorf("Agents.Artisan[backend].Model = %q, want %q",
			cfg.Agents.Artisan["backend"].Model, "claude-sonnet-4-5")
	}
}

func TestLoad_missingFile(t *testing.T) {
	dir := t.TempDir()
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for missing config file, got nil")
	}
}

func TestLoad_invalidJSON(t *testing.T) {
	dir := t.TempDir()
	ctDir := filepath.Join(dir, DirName)
	if err := os.MkdirAll(ctDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ctDir, ConfigFile), []byte("not json"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestLoad_missingTicketPrefix(t *testing.T) {
	dir := t.TempDir()
	ctDir := filepath.Join(dir, DirName)
	if err := os.MkdirAll(ctDir, 0755); err != nil {
		t.Fatal(err)
	}
	raw := `{"version": "1.0.0", "project_root": "/tmp"}`
	if err := os.WriteFile(filepath.Join(ctDir, ConfigFile), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected error for missing ticket_prefix, got nil")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig("/my/project", "owner/repo")

	if cfg.ProjectRoot != "/my/project" {
		t.Errorf("ProjectRoot = %q, want %q", cfg.ProjectRoot, "/my/project")
	}
	if cfg.GithubRepo != "owner/repo" {
		t.Errorf("GithubRepo = %q, want %q", cfg.GithubRepo, "owner/repo")
	}
	if cfg.TicketPrefix != "ct" {
		t.Errorf("TicketPrefix = %q, want %q", cfg.TicketPrefix, "ct")
	}
	if cfg.MaxProles != 2 {
		t.Errorf("MaxProles = %d, want 2", cfg.MaxProles)
	}
	if cfg.PollingIntervalSeconds != 30 {
		t.Errorf("PollingIntervalSeconds = %d, want 30", cfg.PollingIntervalSeconds)
	}
	if cfg.NudgeCooldownSeconds != 300 {
		t.Errorf("NudgeCooldownSeconds = %d, want 300", cfg.NudgeCooldownSeconds)
	}
	if cfg.ContextHandoffThreshold != 0.80 {
		t.Errorf("ContextHandoffThreshold = %v, want 0.80", cfg.ContextHandoffThreshold)
	}
	if cfg.Agents.Mayor.Model == "" {
		t.Error("Agents.Mayor.Model should not be empty")
	}
}

func TestDefaultConfig_healthChecks(t *testing.T) {
	cfg := DefaultConfig("/my/project", "owner/repo")
	hc := cfg.HealthChecks

	if !hc.Enabled {
		t.Error("HealthChecks.Enabled should be true by default")
	}
	if hc.IntervalSeconds != 60 {
		t.Errorf("HealthChecks.IntervalSeconds = %d, want 60", hc.IntervalSeconds)
	}
	if hc.Checks == nil {
		t.Error("HealthChecks.Checks should not be nil")
	}
	if len(hc.Checks) != 0 {
		t.Errorf("HealthChecks.Checks should be empty by default, got %d", len(hc.Checks))
	}
}

func TestWrite_roundTrip(t *testing.T) {
	dir := t.TempDir()
	ctDir := filepath.Join(dir, DirName)
	if err := os.MkdirAll(ctDir, 0755); err != nil {
		t.Fatal(err)
	}

	original := DefaultConfig(dir, "owner/repo")
	original.TicketPrefix = "WT"
	original.MaxProles = 5

	if err := Write(dir, original); err != nil {
		t.Fatalf("Write: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load after Write: %v", err)
	}

	if loaded.TicketPrefix != "WT" {
		t.Errorf("TicketPrefix = %q, want %q", loaded.TicketPrefix, "WT")
	}
	if loaded.MaxProles != 5 {
		t.Errorf("MaxProles = %d, want 5", loaded.MaxProles)
	}
	if loaded.GithubRepo != original.GithubRepo {
		t.Errorf("GithubRepo = %q, want %q", loaded.GithubRepo, original.GithubRepo)
	}
}

// --- Health check config tests ---

func TestLoad_healthChecksConfig(t *testing.T) {
	dir := t.TempDir()
	ctDir := filepath.Join(dir, DirName)
	if err := os.MkdirAll(ctDir, 0755); err != nil {
		t.Fatal(err)
	}

	raw := `{
		"ticket_prefix": "HC",
		"health_checks": {
			"enabled": true,
			"interval_seconds": 120,
			"checks": [
				{
					"name": "dolt-running",
					"type": "process",
					"params": {"name": "dolt"},
					"severity": "critical",
					"enabled": true
				},
				{
					"name": "tmux-present",
					"type": "command",
					"params": {"cmd": "tmux ls", "exit_code": "0"},
					"severity": "warning",
					"enabled": false
				}
			]
		}
	}`
	if err := os.WriteFile(filepath.Join(ctDir, ConfigFile), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	hc := cfg.HealthChecks
	if !hc.Enabled {
		t.Error("HealthChecks.Enabled should be true")
	}
	if hc.IntervalSeconds != 120 {
		t.Errorf("HealthChecks.IntervalSeconds = %d, want 120", hc.IntervalSeconds)
	}
	if len(hc.Checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(hc.Checks))
	}

	c0 := hc.Checks[0]
	if c0.Name != "dolt-running" {
		t.Errorf("Checks[0].Name = %q, want %q", c0.Name, "dolt-running")
	}
	if c0.Type != "process" {
		t.Errorf("Checks[0].Type = %q, want %q", c0.Type, "process")
	}
	if c0.Params["name"] != "dolt" {
		t.Errorf("Checks[0].Params[name] = %q, want %q", c0.Params["name"], "dolt")
	}
	if c0.Severity != "critical" {
		t.Errorf("Checks[0].Severity = %q, want %q", c0.Severity, "critical")
	}
	if !c0.Enabled {
		t.Error("Checks[0].Enabled should be true")
	}

	c1 := hc.Checks[1]
	if c1.Enabled {
		t.Error("Checks[1].Enabled should be false")
	}
	if c1.Severity != "warning" {
		t.Errorf("Checks[1].Severity = %q, want %q", c1.Severity, "warning")
	}
}

func TestLoad_healthChecksDisabled(t *testing.T) {
	dir := t.TempDir()
	ctDir := filepath.Join(dir, DirName)
	if err := os.MkdirAll(ctDir, 0755); err != nil {
		t.Fatal(err)
	}

	raw := `{
		"ticket_prefix": "HC",
		"health_checks": {"enabled": false, "interval_seconds": 30, "checks": []}
	}`
	if err := os.WriteFile(filepath.Join(ctDir, ConfigFile), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.HealthChecks.Enabled {
		t.Error("HealthChecks.Enabled should be false")
	}
}

func TestWrite_roundTrip_healthChecks(t *testing.T) {
	dir := t.TempDir()
	ctDir := filepath.Join(dir, DirName)
	if err := os.MkdirAll(ctDir, 0755); err != nil {
		t.Fatal(err)
	}

	original := DefaultConfig(dir, "owner/repo")
	original.HealthChecks = HealthCheckConfig{
		Enabled:         true,
		IntervalSeconds: 90,
		Checks: []CheckConfig{
			{
				Name:     "dolt-running",
				Type:     "process",
				Params:   map[string]string{"name": "dolt"},
				Severity: "critical",
				Enabled:  true,
			},
		},
	}

	if err := Write(dir, original); err != nil {
		t.Fatalf("Write: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load after Write: %v", err)
	}

	hc := loaded.HealthChecks
	if hc.IntervalSeconds != 90 {
		t.Errorf("IntervalSeconds = %d, want 90", hc.IntervalSeconds)
	}
	if len(hc.Checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(hc.Checks))
	}
	if hc.Checks[0].Name != "dolt-running" {
		t.Errorf("Checks[0].Name = %q, want %q", hc.Checks[0].Name, "dolt-running")
	}
	if hc.Checks[0].Params["name"] != "dolt" {
		t.Errorf("Checks[0].Params[name] = %q, want %q", hc.Checks[0].Params["name"], "dolt")
	}
}

func TestCheckConfig_nilParams(t *testing.T) {
	dir := t.TempDir()
	ctDir := filepath.Join(dir, DirName)
	if err := os.MkdirAll(ctDir, 0755); err != nil {
		t.Fatal(err)
	}

	// A check with no params field should unmarshal cleanly
	raw := `{
		"ticket_prefix": "HC",
		"health_checks": {
			"enabled": true,
			"interval_seconds": 60,
			"checks": [{"name": "simple", "type": "file", "severity": "warning", "enabled": true}]
		}
	}`
	if err := os.WriteFile(filepath.Join(ctDir, ConfigFile), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cfg.HealthChecks.Checks) != 1 {
		t.Fatalf("expected 1 check, got %d", len(cfg.HealthChecks.Checks))
	}
	// Params may be nil when not specified — that's fine
	c := cfg.HealthChecks.Checks[0]
	if c.Name != "simple" {
		t.Errorf("Name = %q, want %q", c.Name, "simple")
	}
}
