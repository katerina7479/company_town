package config

import (
	"errors"
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
		"platform": "github",
		"repo": "acme/widget",
		"dolt": {"host": "127.0.0.1", "port": 3307, "database": "ct"},
		"log_dir": ".company_town/logs",
		"max_proles": 4,
		"polling_interval_seconds": 60,
		"nudge_cooldown_seconds": 120,
		"context_handoff_threshold": 0.75,
		"agents": {
			"mayor":     {"model": "claude-opus-4-6"},
			"architect": {"model": "claude-opus-4-6"},
			"prole":     {"model": "claude-sonnet-4-6"},
			"artisan":   {"backend": {"model": "claude-sonnet-4-6"}}
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
	if cfg.Agents.Mayor.Model != "claude-opus-4-6" {
		t.Errorf("Agents.Mayor.Model = %q, want %q", cfg.Agents.Mayor.Model, "claude-opus-4-6")
	}
	if cfg.Agents.Artisan["backend"].Model != "claude-sonnet-4-6" {
		t.Errorf("Agents.Artisan[backend].Model = %q, want %q",
			cfg.Agents.Artisan["backend"].Model, "claude-sonnet-4-6")
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

// writeConfig is a test helper that writes a minimal valid config JSON with
// the given agents block substituted in.
func writeConfig(t *testing.T, agentsJSON string) string {
	t.Helper()
	dir := t.TempDir()
	ctDir := filepath.Join(dir, DirName)
	if err := os.MkdirAll(ctDir, 0755); err != nil {
		t.Fatal(err)
	}
	raw := `{"version":"1.0.0","ticket_prefix":"nc","project_root":"/tmp","platform":"github","repo":"x/y","dolt":{"host":"127.0.0.1","port":3307,"database":"ct"},"agents":` + agentsJSON + `}`
	if err := os.WriteFile(filepath.Join(ctDir, ConfigFile), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestConfigLoad_workflowAbsent(t *testing.T) {
	dir := writeConfig(t, `{"reviewer":{"model":"sonnet"},"prole":{"model":"sonnet"}}`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Agents.Reviewer.Workflow != nil {
		t.Errorf("expected Reviewer.Workflow=nil, got %+v", cfg.Agents.Reviewer.Workflow)
	}
}

func TestConfigLoad_workflowValid(t *testing.T) {
	dir := writeConfig(t, `{"reviewer":{"model":"sonnet","workflow":{"accept":{"ticket_transition":{"from":"in_review","to":"under_review"}}}},"prole":{"model":"sonnet"}}`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	tt := cfg.Agents.Reviewer.Workflow.Accept.TicketTransition
	if tt == nil || tt.From != "in_review" || tt.To != "under_review" {
		t.Errorf("unexpected transition: %+v", tt)
	}
}

func TestConfigLoad_workflowEmptyFrom(t *testing.T) {
	dir := writeConfig(t, `{"reviewer":{"model":"sonnet","workflow":{"accept":{"ticket_transition":{"from":"","to":"under_review"}}}},"prole":{"model":"sonnet"}}`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected validation error for empty from, got nil")
	}
	if !errors.Is(err, ErrInvalidTicketTransition) {
		t.Errorf("expected ErrInvalidTicketTransition, got: %v", err)
	}
}

func TestConfigLoad_workflowSameFromTo(t *testing.T) {
	dir := writeConfig(t, `{"reviewer":{"model":"sonnet","workflow":{"accept":{"ticket_transition":{"from":"in_review","to":"in_review"}}}},"prole":{"model":"sonnet"}}`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected validation error for same from/to, got nil")
	}
	if !errors.Is(err, ErrInvalidTicketTransition) {
		t.Errorf("expected ErrInvalidTicketTransition, got: %v", err)
	}
}

func TestDefaultConfig_TicketPrefixNotCt(t *testing.T) {
	cfg := DefaultConfig("/tmp/proj", "github", "")
	if cfg.TicketPrefix == "ct" {
		t.Error("default ticket_prefix should not collide with binary name 'ct'")
	}
	if cfg.TicketPrefix == "" {
		t.Error("default ticket_prefix should not be empty (breaks config.Load)")
	}
}

func TestDefaultConfig_reviewerAcceptWorkflow(t *testing.T) {
	cfg := DefaultConfig("/tmp", "github", "x/y")
	wf := cfg.Agents.Reviewer.Workflow
	if wf == nil || wf.Accept == nil || wf.Accept.TicketTransition == nil {
		t.Fatal("expected reviewer accept workflow, got nil")
	}
	tt := wf.Accept.TicketTransition
	if tt.From != "in_review" || tt.To != "under_review" {
		t.Errorf("expected in_review→under_review, got %s→%s", tt.From, tt.To)
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
	cfg := DefaultConfig("/my/project", "github", "owner/repo")

	if cfg.ProjectRoot != "/my/project" {
		t.Errorf("ProjectRoot = %q, want %q", cfg.ProjectRoot, "/my/project")
	}
	if cfg.Repo != "owner/repo" {
		t.Errorf("Repo = %q, want %q", cfg.Repo, "owner/repo")
	}
	if cfg.TicketPrefix != "tk" {
		t.Errorf("TicketPrefix = %q, want %q", cfg.TicketPrefix, "tk")
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

func TestDefaultConfig_ModelsAreCurrent(t *testing.T) {
	cfg := DefaultConfig("/tmp", "github", "owner/repo")
	if cfg.Agents.Mayor.Model != "claude-opus-4-6" {
		t.Errorf("Mayor.Model = %q, want claude-opus-4-6", cfg.Agents.Mayor.Model)
	}
	if cfg.Agents.Architect.Model != "claude-opus-4-6" {
		t.Errorf("Architect.Model = %q, want claude-opus-4-6", cfg.Agents.Architect.Model)
	}
	if cfg.Agents.Reviewer.Model != "claude-sonnet-4-6" {
		t.Errorf("Reviewer.Model = %q, want claude-sonnet-4-6", cfg.Agents.Reviewer.Model)
	}
	if cfg.Agents.Prole.Model != "claude-sonnet-4-6" {
		t.Errorf("Prole.Model = %q, want claude-sonnet-4-6", cfg.Agents.Prole.Model)
	}
}

func TestWrite_roundTrip(t *testing.T) {
	dir := t.TempDir()
	ctDir := filepath.Join(dir, DirName)
	if err := os.MkdirAll(ctDir, 0755); err != nil {
		t.Fatal(err)
	}

	original := DefaultConfig(dir, "github", "owner/repo")
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
	if loaded.Repo != original.Repo {
		t.Errorf("Repo = %q, want %q", loaded.Repo, original.Repo)
	}
}

func TestWrite_createsValidJSON(t *testing.T) {
	dir := t.TempDir()
	ctDir := filepath.Join(dir, DirName)
	if err := os.MkdirAll(ctDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig(dir, "github", "test/repo")
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

func TestValidateForStart_emptyRepo(t *testing.T) {
	cfg := &Config{Platform: "github", Repo: ""}
	err := ValidateForStart(cfg)
	if err == nil {
		t.Fatal("expected error for empty repo, got nil")
	}
	if !errors.Is(err, ErrInvalidRepo) {
		t.Errorf("expected ErrInvalidRepo, got: %v", err)
	}
}

func TestValidateForStart_placeholder(t *testing.T) {
	cfg := &Config{Platform: "github", Repo: "owner/repo"}
	err := ValidateForStart(cfg)
	if err == nil {
		t.Fatal("expected error for placeholder repo, got nil")
	}
	if !errors.Is(err, ErrInvalidRepo) {
		t.Errorf("expected ErrInvalidRepo, got: %v", err)
	}
}

func TestValidateForStart_urlForm(t *testing.T) {
	cfg := &Config{Platform: "github", Repo: "https://github.com/foo/bar"}
	err := ValidateForStart(cfg)
	if err == nil {
		t.Fatal("expected error for URL-form repo, got nil")
	}
	if !errors.Is(err, ErrInvalidRepo) {
		t.Errorf("expected ErrInvalidRepo, got: %v", err)
	}
}

func TestValidateForStart_valid(t *testing.T) {
	cfg := &Config{Platform: "github", Repo: "foo/bar"}
	if err := ValidateForStart(cfg); err != nil {
		t.Errorf("expected no error for valid repo %q, got: %v", cfg.Repo, err)
	}
}

func TestValidateForStart_noSlash(t *testing.T) {
	cfg := &Config{Platform: "github", Repo: "justrepo"}
	err := ValidateForStart(cfg)
	if err == nil {
		t.Fatal("expected error for repo with no slash, got nil")
	}
	if !errors.Is(err, ErrInvalidRepo) {
		t.Errorf("expected ErrInvalidRepo, got: %v", err)
	}
}

func TestValidateForStart_unknownPlatform(t *testing.T) {
	cfg := &Config{Platform: "bitbucket", Repo: "foo/bar"}
	err := ValidateForStart(cfg)
	if err == nil {
		t.Fatal("expected error for unknown platform, got nil")
	}
	if !errors.Is(err, ErrInvalidPlatform) {
		t.Errorf("expected ErrInvalidPlatform, got: %v", err)
	}
}

func TestValidateForStart_missingPlatform(t *testing.T) {
	cfg := &Config{Platform: "", Repo: "foo/bar"}
	err := ValidateForStart(cfg)
	if err == nil {
		t.Fatal("expected error for missing platform, got nil")
	}
	if !errors.Is(err, ErrInvalidPlatform) {
		t.Errorf("expected ErrInvalidPlatform, got: %v", err)
	}
}

func TestValidateForStart_gitlabValid(t *testing.T) {
	cfg := &Config{Platform: "gitlab", Repo: "mygroup/myrepo"}
	if err := ValidateForStart(cfg); err != nil {
		t.Errorf("expected no error for valid gitlab repo, got: %v", err)
	}
}

func TestValidateForStart_gitlabEmptyRepo(t *testing.T) {
	cfg := &Config{Platform: "gitlab", Repo: ""}
	err := ValidateForStart(cfg)
	if err == nil {
		t.Fatal("expected error for empty gitlab repo, got nil")
	}
	if !errors.Is(err, ErrInvalidRepo) {
		t.Errorf("expected ErrInvalidRepo, got: %v", err)
	}
}

func TestValidateForStart_gitlabPlaceholder(t *testing.T) {
	cfg := &Config{Platform: "gitlab", Repo: "namespace/project"}
	err := ValidateForStart(cfg)
	if err == nil {
		t.Fatal("expected error for placeholder gitlab repo, got nil")
	}
	if !errors.Is(err, ErrInvalidRepo) {
		t.Errorf("expected ErrInvalidRepo, got: %v", err)
	}
}

func TestValidateForStart_gitlabURLForm(t *testing.T) {
	cfg := &Config{Platform: "gitlab", Repo: "https://gitlab.com/mygroup/myrepo"}
	err := ValidateForStart(cfg)
	if err == nil {
		t.Fatal("expected error for URL-form gitlab repo, got nil")
	}
	if !errors.Is(err, ErrInvalidRepo) {
		t.Errorf("expected ErrInvalidRepo, got: %v", err)
	}
}

func TestValidateForStart_gitlabNoSlash(t *testing.T) {
	cfg := &Config{Platform: "gitlab", Repo: "justrepo"}
	err := ValidateForStart(cfg)
	if err == nil {
		t.Fatal("expected error for gitlab repo with no slash, got nil")
	}
	if !errors.Is(err, ErrInvalidRepo) {
		t.Errorf("expected ErrInvalidRepo, got: %v", err)
	}
}

func TestConfig_TDD_DefaultsFalse(t *testing.T) {
	dir := writeConfig(t, `{"mayor":{"model":"claude-opus-4-6"},"prole":{"model":"claude-sonnet-4-6"}}`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.TDD {
		t.Error("TDD should default to false when absent from config JSON")
	}
}

func TestConfig_TDD_RoundTrips(t *testing.T) {
	dir := t.TempDir()
	ctDir := filepath.Join(dir, DirName)
	if err := os.MkdirAll(ctDir, 0750); err != nil {
		t.Fatal(err)
	}

	original := DefaultConfig(dir, "github", "owner/repo")
	original.TDD = true

	if err := Write(dir, original); err != nil {
		t.Fatalf("Write: %v", err)
	}
	loaded, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !loaded.TDD {
		t.Error("TDD should round-trip as true after Write+Load")
	}
}
