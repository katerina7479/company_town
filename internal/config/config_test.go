package config

import (
	"os"
	"path/filepath"
	"strings"
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
	raw := `{"version":"1.0.0","ticket_prefix":"nc","project_root":"/tmp","github_repo":"x/y","dolt":{"host":"127.0.0.1","port":3307,"database":"ct"},"agents":` + agentsJSON + `}`
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
	if !strings.Contains(err.Error(), "from and to must be non-empty and different") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestConfigLoad_workflowSameFromTo(t *testing.T) {
	dir := writeConfig(t, `{"reviewer":{"model":"sonnet","workflow":{"accept":{"ticket_transition":{"from":"in_review","to":"in_review"}}}},"prole":{"model":"sonnet"}}`)
	_, err := Load(dir)
	if err == nil {
		t.Fatal("expected validation error for same from/to, got nil")
	}
	if !strings.Contains(err.Error(), "from and to must be non-empty and different") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDefaultConfig_TicketPrefixNotCt(t *testing.T) {
	cfg := DefaultConfig("/tmp/proj", "")
	if cfg.TicketPrefix == "ct" {
		t.Error("default ticket_prefix should not collide with binary name 'ct'")
	}
	if cfg.TicketPrefix == "" {
		t.Error("default ticket_prefix should not be empty (breaks config.Load)")
	}
}

func TestDefaultConfig_reviewerAcceptWorkflow(t *testing.T) {
	cfg := DefaultConfig("/tmp", "x/y")
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
	cfg := DefaultConfig("/my/project", "owner/repo")

	if cfg.ProjectRoot != "/my/project" {
		t.Errorf("ProjectRoot = %q, want %q", cfg.ProjectRoot, "/my/project")
	}
	if cfg.GithubRepo != "owner/repo" {
		t.Errorf("GithubRepo = %q, want %q", cfg.GithubRepo, "owner/repo")
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
	cfg := DefaultConfig("/tmp", "owner/repo")
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

func TestDefaultConfig_QualityChecks_count(t *testing.T) {
	cfg := DefaultConfig("/tmp/proj", "owner/repo")
	if !cfg.Quality.Enabled {
		t.Error("expected Quality.Enabled=true")
	}
	// 6 new pass_fail checks + go_test_coverage = 7 total
	if got := len(cfg.Quality.Checks); got != 7 {
		t.Errorf("expected 7 quality checks, got %d", got)
	}
}

func TestDefaultConfig_QualityChecks_names(t *testing.T) {
	cfg := DefaultConfig("/tmp/proj", "owner/repo")
	names := make(map[string]bool, len(cfg.Quality.Checks))
	for _, c := range cfg.Quality.Checks {
		names[c.Name] = true
	}
	for _, want := range []string{
		"go_build", "go_vet", "go_lint", "go_test",
		"go_test_race", "go_mod_verify", "go_test_coverage",
	} {
		if !names[want] {
			t.Errorf("expected check %q in DefaultConfig quality checks", want)
		}
	}
}

func TestDefaultConfig_QualityChecks_allEnabled(t *testing.T) {
	cfg := DefaultConfig("/tmp/proj", "owner/repo")
	for _, c := range cfg.Quality.Checks {
		if !c.Enabled {
			t.Errorf("expected check %q to be enabled by default", c.Name)
		}
	}
}

func TestDefaultConfig_QualityChecks_passFail(t *testing.T) {
	cfg := DefaultConfig("/tmp/proj", "owner/repo")
	passFail := map[string]bool{
		"go_build": true, "go_vet": true, "go_lint": true,
		"go_test": true, "go_test_race": true, "go_mod_verify": true,
	}
	for _, c := range cfg.Quality.Checks {
		if !passFail[c.Name] {
			continue
		}
		if c.Type != "pass_fail" {
			t.Errorf("check %q: expected type=pass_fail, got %q", c.Name, c.Type)
		}
		if c.Target != 0 {
			t.Errorf("check %q: pass_fail check should have Target=0, got %v", c.Name, c.Target)
		}
	}
}

func TestDefaultConfig_QualityChecks_coverageTargets(t *testing.T) {
	cfg := DefaultConfig("/tmp/proj", "owner/repo")
	var cov *QualityCheckConfig
	for i := range cfg.Quality.Checks {
		if cfg.Quality.Checks[i].Name == "go_test_coverage" {
			cov = &cfg.Quality.Checks[i]
			break
		}
	}
	if cov == nil {
		t.Fatal("go_test_coverage check not found")
	}
	if cov.Type != "metric" {
		t.Errorf("go_test_coverage type = %q, want metric", cov.Type)
	}
	if cov.Target != 80.0 {
		t.Errorf("go_test_coverage Target = %v, want 80.0", cov.Target)
	}
	if cov.WarnTarget != 70.0 {
		t.Errorf("go_test_coverage WarnTarget = %v, want 70.0", cov.WarnTarget)
	}
}

func TestValidateForStart_emptyRepo(t *testing.T) {
	cfg := &Config{GithubRepo: ""}
	err := ValidateForStart(cfg)
	if err == nil {
		t.Fatal("expected error for empty github_repo, got nil")
	}
	if !strings.Contains(err.Error(), "github_repo") {
		t.Errorf("error should mention github_repo: %v", err)
	}
}

func TestValidateForStart_placeholder(t *testing.T) {
	cfg := &Config{GithubRepo: "owner/repo"}
	err := ValidateForStart(cfg)
	if err == nil {
		t.Fatal("expected error for placeholder github_repo, got nil")
	}
	if !strings.Contains(err.Error(), "edit") {
		t.Errorf("error should mention 'edit': %v", err)
	}
}

func TestValidateForStart_urlForm(t *testing.T) {
	cfg := &Config{GithubRepo: "https://github.com/foo/bar"}
	err := ValidateForStart(cfg)
	if err == nil {
		t.Fatal("expected error for URL-form github_repo, got nil")
	}
	if !strings.Contains(err.Error(), "owner/repo") {
		t.Errorf("error should mention owner/repo form: %v", err)
	}
}

func TestValidateForStart_valid(t *testing.T) {
	cfg := &Config{GithubRepo: "foo/bar"}
	if err := ValidateForStart(cfg); err != nil {
		t.Errorf("expected no error for valid github_repo %q, got: %v", cfg.GithubRepo, err)
	}
}

func TestValidateForStart_noSlash(t *testing.T) {
	cfg := &Config{GithubRepo: "justrepo"}
	err := ValidateForStart(cfg)
	if err == nil {
		t.Fatal("expected error for github_repo with no slash, got nil")
	}
	if !strings.Contains(err.Error(), "owner/repo") {
		t.Errorf("error should mention owner/repo form: %v", err)
	}
}
