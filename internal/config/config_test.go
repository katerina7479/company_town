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
	if cfg.PollingIntervalSeconds != 60 {
		t.Errorf("PollingIntervalSeconds = %d, want 60", cfg.PollingIntervalSeconds)
	}
	if cfg.NudgeCooldownSeconds != 120 {
		t.Errorf("NudgeCooldownSeconds = %d, want 120", cfg.NudgeCooldownSeconds)
	}
	if cfg.ContextHandoffThreshold != 0.75 {
		t.Errorf("ContextHandoffThreshold = %v, want 0.75", cfg.ContextHandoffThreshold)
	}
	if cfg.Agents.Mayor.Model != "claude-opus-4-5" {
		t.Errorf("Agents.Mayor.Model = %q, want %q", cfg.Agents.Mayor.Model, "claude-opus-4-5")
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
	// Valid JSON but no ticket_prefix
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
	if cfg.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", cfg.Version, "1.0.0")
	}
	if cfg.Dolt.Port != 3307 {
		t.Errorf("Dolt.Port = %d, want 3307", cfg.Dolt.Port)
	}
	if cfg.Dolt.Host != "127.0.0.1" {
		t.Errorf("Dolt.Host = %q, want %q", cfg.Dolt.Host, "127.0.0.1")
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
	if cfg.Agents.Artisan == nil {
		t.Error("Agents.Artisan should not be nil")
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

func TestDefaultConfig_conductorDefaults(t *testing.T) {
	cfg := DefaultConfig("/my/project", "owner/repo")

	if !cfg.IsConductorEnabled() {
		t.Error("ConductorEnabled should default to true")
	}
	if cfg.ConductorEnabled == nil {
		t.Error("ConductorEnabled pointer should not be nil after DefaultConfig")
	}
	if cfg.ConductorModel != "claude-sonnet-4-6" {
		t.Errorf("ConductorModel = %q, want %q", cfg.ConductorModel, "claude-sonnet-4-6")
	}
}

func TestLoad_conductorEnabledDefaultsWhenOmitted(t *testing.T) {
	dir := t.TempDir()
	ctDir := filepath.Join(dir, DirName)
	if err := os.MkdirAll(ctDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Config with no conductor_* fields — omitted keys must default to true/claude-sonnet-4-6.
	raw := `{"ticket_prefix": "TC", "project_root": "/tmp"}`
	if err := os.WriteFile(filepath.Join(ctDir, ConfigFile), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.IsConductorEnabled() {
		t.Error("IsConductorEnabled() should be true when key is omitted from config file")
	}
	if cfg.ConductorModel != "claude-sonnet-4-6" {
		t.Errorf("ConductorModel = %q, want default %q", cfg.ConductorModel, "claude-sonnet-4-6")
	}
}

func TestLoad_conductorEnabledFalseHonored(t *testing.T) {
	dir := t.TempDir()
	ctDir := filepath.Join(dir, DirName)
	if err := os.MkdirAll(ctDir, 0755); err != nil {
		t.Fatal(err)
	}

	raw := `{"ticket_prefix": "TC", "project_root": "/tmp", "conductor_enabled": false}`
	if err := os.WriteFile(filepath.Join(ctDir, ConfigFile), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.IsConductorEnabled() {
		t.Error("IsConductorEnabled() should be false when explicitly set to false")
	}
}

func TestLoad_conductorModelExplicitHonored(t *testing.T) {
	dir := t.TempDir()
	ctDir := filepath.Join(dir, DirName)
	if err := os.MkdirAll(ctDir, 0755); err != nil {
		t.Fatal(err)
	}

	raw := `{"ticket_prefix": "TC", "project_root": "/tmp", "conductor_model": "claude-opus-4-6"}`
	if err := os.WriteFile(filepath.Join(ctDir, ConfigFile), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ConductorModel != "claude-opus-4-6" {
		t.Errorf("ConductorModel = %q, want %q", cfg.ConductorModel, "claude-opus-4-6")
	}
}

func TestWrite_conductorRoundTrip(t *testing.T) {
	dir := t.TempDir()
	ctDir := filepath.Join(dir, DirName)
	if err := os.MkdirAll(ctDir, 0755); err != nil {
		t.Fatal(err)
	}

	original := DefaultConfig(dir, "owner/repo")
	original.ConductorEnabled = boolPtr(false)
	original.ConductorModel = "claude-opus-4-6"

	if err := Write(dir, original); err != nil {
		t.Fatalf("Write: %v", err)
	}

	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.IsConductorEnabled() {
		t.Error("IsConductorEnabled() should be false after round-trip")
	}
	if loaded.ConductorModel != "claude-opus-4-6" {
		t.Errorf("ConductorModel = %q, want %q", loaded.ConductorModel, "claude-opus-4-6")
	}
}

func TestWrite_createsValidJSON(t *testing.T) {
	dir := t.TempDir()
	ctDir := filepath.Join(dir, DirName)
	if err := os.MkdirAll(ctDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig(dir, "test/repo")
	if err := Write(dir, cfg); err != nil {
		t.Fatalf("Write: %v", err)
	}

	data, err := os.ReadFile(ConfigPath(dir))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(data) == 0 {
		t.Error("written config file is empty")
	}
	// Verify it is valid JSON by loading it back
	if _, err := Load(dir); err != nil {
		t.Errorf("written config is not loadable: %v", err)
	}
}
