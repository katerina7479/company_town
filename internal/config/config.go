package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	DirName    = ".company_town"
	ConfigFile = "config.json"
)

type AgentConfig struct {
	Model    string          `json:"model"`
	Workflow *WorkflowConfig `json:"workflow,omitempty"`
}

// WorkflowConfig declares the optional accept/release ticket transitions for a
// role, plus an open-ended Actions map for the gt-agent-do escape hatch (nc-84).
type WorkflowConfig struct {
	Accept  *WorkflowAction            `json:"accept,omitempty"`
	Release *WorkflowAction            `json:"release,omitempty"`
	Actions map[string]*WorkflowAction `json:"actions,omitempty"`
}

// WorkflowAction wraps the optional ticket transition that fires when the
// agent invokes the corresponding verb (accept / release / do <action>).
type WorkflowAction struct {
	TicketTransition *TicketTransition `json:"ticket_transition,omitempty"`
}

// TicketTransition describes a status transition that is applied to the
// current ticket when a workflow action fires. The transition is a no-op if
// the ticket's current status does not match From.
type TicketTransition struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// ArtisanConfig maps specialty names to their configs.
// Specialties are user-defined (e.g., "qa", "backend", "embedded", "designer").
type ArtisanConfig map[string]AgentConfig

type AgentsConfig struct {
	Mayor     AgentConfig   `json:"mayor"`
	Architect AgentConfig   `json:"architect"`
	Artisan   ArtisanConfig `json:"artisan"`
	Reviewer  AgentConfig   `json:"reviewer"`
	Prole     AgentConfig   `json:"prole"`
}

type DoltConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
}

// QualityCheckConfig defines a single quality gate command and how to evaluate it.
type QualityCheckConfig struct {
	Name       string  `json:"name"`
	Command    string  `json:"command"`
	Type       string  `json:"type"`        // "pass_fail" or "metric"
	Target     float64 `json:"target"`      // aspirational target value for "metric" checks; pass when value >= Target
	WarnTarget float64 `json:"warn_target"` // warn when value >= WarnTarget but < Target; ignored when zero
	Enabled    bool    `json:"enabled"`
}

// QualityConfig holds all project-level quality check settings.
type QualityConfig struct {
	Enabled                 bool                 `json:"enabled"`
	BaselineIntervalSeconds int                  `json:"baseline_interval_seconds"`
	Checks                  []QualityCheckConfig `json:"checks"`
}

type Config struct {
	Version                      string        `json:"version"`
	TicketPrefix                 string        `json:"ticket_prefix"`
	ProjectRoot                  string        `json:"project_root"`
	GithubRepo                   string        `json:"github_repo"`
	Dolt                         DoltConfig    `json:"dolt"`
	LogDir                       string        `json:"log_dir"`
	MaxProles                    int           `json:"max_proles"`
	Agents                       AgentsConfig  `json:"agents"`
	PollingIntervalSeconds       int           `json:"polling_interval_seconds"`
	NudgeCooldownSeconds         int           `json:"nudge_cooldown_seconds"`
	ContextHandoffThreshold      float64       `json:"context_handoff_threshold"`
	StuckAgentThresholdSeconds   int           `json:"stuck_agent_threshold_seconds"`
	WorktreePruneIntervalSeconds int           `json:"worktree_prune_interval_seconds"`
	WorktreeResetIntervalSeconds int           `json:"worktree_reset_interval_seconds"`
	PRBackfillIntervalSeconds    int           `json:"pr_backfill_interval_seconds"`
	RestartDeadAgents            bool          `json:"restart_dead_agents"`
	RestartCooldownSeconds       int           `json:"restart_cooldown_seconds"`
	Quality                      QualityConfig `json:"quality"`
}

// CompanyTownDir returns the .company_town directory path for a project root.
func CompanyTownDir(projectRoot string) string {
	return filepath.Join(projectRoot, DirName)
}

// ConfigPath returns the config.json path for a project root.
func ConfigPath(projectRoot string) string {
	return filepath.Join(CompanyTownDir(projectRoot), ConfigFile)
}

// Load reads and parses the config.json file.
func Load(projectRoot string) (*Config, error) {
	data, err := os.ReadFile(ConfigPath(projectRoot))
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if cfg.TicketPrefix == "" {
		return nil, fmt.Errorf("config: ticket_prefix is required")
	}

	if err := validateAgentsWorkflow(&cfg.Agents); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// ValidateForStart checks that the config is safe to use for `ct start`.
// It rejects placeholder or missing github_repo values that would cause
// downstream failures (e.g. gt pr create against a non-existent repo).
// This is intentionally NOT called from Load so that ct init can write and
// reload its own freshly-generated placeholder config without error.
func ValidateForStart(cfg *Config) error {
	repo := cfg.GithubRepo
	if repo == "" {
		return fmt.Errorf("config: github_repo is unset — edit .company_town/config.json and set github_repo to \"owner/repo\" form (e.g., \"katerina7479/company_town\")")
	}
	if repo == "owner/repo" {
		return fmt.Errorf("config: github_repo is still the placeholder \"owner/repo\" — edit .company_town/config.json and set it to your actual repository")
	}
	if strings.HasPrefix(repo, "http://") || strings.HasPrefix(repo, "https://") {
		return fmt.Errorf("config: github_repo must be in \"owner/repo\" form, not a URL (got %q)", repo)
	}
	if !strings.Contains(repo, "/") {
		return fmt.Errorf("config: github_repo must be in \"owner/repo\" form (got %q)", repo)
	}
	return nil
}

// validateAgentsWorkflow checks that every declared TicketTransition has
// non-empty, non-equal From and To values.
func validateAgentsWorkflow(agents *AgentsConfig) error {
	type namedConfig struct {
		path string
		cfg  AgentConfig
	}
	named := []namedConfig{
		{"agents.mayor", agents.Mayor},
		{"agents.architect", agents.Architect},
		{"agents.reviewer", agents.Reviewer},
		{"agents.prole", agents.Prole},
	}
	for specialty, ac := range agents.Artisan {
		named = append(named, namedConfig{"agents.artisan." + specialty, ac})
	}
	for _, nc := range named {
		if err := validateWorkflow(nc.path, nc.cfg.Workflow); err != nil {
			return err
		}
	}
	return nil
}

// validateWorkflow validates all TicketTransitions in a WorkflowConfig.
func validateWorkflow(path string, wf *WorkflowConfig) error {
	if wf == nil {
		return nil
	}
	if wf.Accept != nil {
		if err := validateTransition(path+".workflow.accept", wf.Accept.TicketTransition); err != nil {
			return err
		}
	}
	if wf.Release != nil {
		if err := validateTransition(path+".workflow.release", wf.Release.TicketTransition); err != nil {
			return err
		}
	}
	for name, action := range wf.Actions {
		if action == nil {
			continue
		}
		if err := validateTransition(path+".workflow.actions."+name, action.TicketTransition); err != nil {
			return err
		}
	}
	return nil
}

// validateTransition checks that a TicketTransition has non-empty, non-equal From/To.
func validateTransition(path string, tt *TicketTransition) error {
	if tt == nil {
		return nil
	}
	if tt.From == "" || tt.To == "" {
		return fmt.Errorf("config: %s.ticket_transition: from and to must be non-empty and different", path)
	}
	if tt.From == tt.To {
		return fmt.Errorf("config: %s.ticket_transition: from and to must be non-empty and different", path)
	}
	return nil
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig(projectRoot, githubRepo string) *Config {
	return &Config{
		Version:      "1.0.0",
		TicketPrefix: "tk",
		ProjectRoot:  projectRoot,
		GithubRepo:   githubRepo,
		Dolt: DoltConfig{
			Host:     "127.0.0.1",
			Port:     3307,
			Database: "company_town",
		},
		LogDir:    filepath.Join(DirName, "logs"),
		MaxProles: 2,
		Agents: AgentsConfig{
			Mayor:     AgentConfig{Model: "claude-opus-4-6"},
			Architect: AgentConfig{Model: "claude-opus-4-6"},
			Artisan:   ArtisanConfig{}, // User-defined in config.json
			Reviewer: AgentConfig{
				Model: "claude-sonnet-4-6",
				Workflow: &WorkflowConfig{
					Accept: &WorkflowAction{
						TicketTransition: &TicketTransition{From: "in_review", To: "under_review"},
					},
					// Release is nil: reviewer release is handled by approve / request-changes verbs.
				},
			},
			Prole: AgentConfig{
				Model:    "claude-sonnet-4-6",
				Workflow: &WorkflowConfig{
					// Accept and Release are nil: prole acceptance is implicit in picking up
					// the assignment; there is no automatic ticket side-effect.
				},
			},
		},
		PollingIntervalSeconds:       30,
		NudgeCooldownSeconds:         300,
		ContextHandoffThreshold:      0.80,
		StuckAgentThresholdSeconds:   1800,
		WorktreePruneIntervalSeconds: 300,
		WorktreeResetIntervalSeconds: 60,
		PRBackfillIntervalSeconds:    300,
		RestartDeadAgents:            true,
		RestartCooldownSeconds:       300,
		Quality: QualityConfig{
			Enabled:                 true,
			BaselineIntervalSeconds: 3600,
			Checks: []QualityCheckConfig{
				{
					Name:    "go_build",
					Command: "go build ./...",
					Type:    "pass_fail",
					Enabled: true,
				},
				{
					Name:    "go_vet",
					Command: "go vet ./...",
					Type:    "pass_fail",
					Enabled: true,
				},
				{
					Name:    "go_lint",
					Command: "golangci-lint run ./...",
					Type:    "pass_fail",
					Enabled: true,
				},
				{
					Name:    "go_test",
					Command: "go test ./...",
					Type:    "pass_fail",
					Enabled: true,
				},
				{
					Name:    "go_test_race",
					Command: "go test -race -short ./...",
					Type:    "pass_fail",
					Enabled: true,
				},
				{
					Name:    "go_mod_verify",
					Command: "go mod verify",
					Type:    "pass_fail",
					Enabled: true,
				},
				{
					Name:       "go_test_coverage",
					Command:    "go test ./... -coverprofile=.company_town/.coverage.out >/dev/null 2>&1; go tool cover -func=.company_town/.coverage.out 2>/dev/null | awk '/^total:/ {gsub(\"%\",\"\"); print $3}'",
					Type:       "metric",
					Target:     80.0,
					WarnTarget: 70.0,
					Enabled:    true,
				},
			},
		},
	}
}

// Write serializes the config to disk.
func Write(projectRoot string, cfg *Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	return os.WriteFile(ConfigPath(projectRoot), data, 0644)
}
