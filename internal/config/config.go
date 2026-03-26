package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	DirName    = ".company_town"
	ConfigFile = "config.json"
)

type AgentConfig struct {
	Model string `json:"model"`
}

// ArtisanConfig maps specialty names to their configs.
// Specialties are user-defined (e.g., "qa", "backend", "embedded", "designer").
type ArtisanConfig map[string]AgentConfig

type AgentsConfig struct {
	Mayor     AgentConfig   `json:"mayor"`
	Architect AgentConfig   `json:"architect"`
	Artisan   ArtisanConfig `json:"artisan"`
	Conductor AgentConfig   `json:"conductor"`
	Prole     AgentConfig   `json:"prole"`
	Janitor   AgentConfig   `json:"janitor"`
}

type DoltConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Database string `json:"database"`
}

type Config struct {
	Version                 string       `json:"version"`
	TicketPrefix            string       `json:"ticket_prefix"`
	ProjectRoot             string       `json:"project_root"`
	GithubRepo              string       `json:"github_repo"`
	Dolt                    DoltConfig   `json:"dolt"`
	LogDir                  string       `json:"log_dir"`
	MaxProles               int          `json:"max_proles"`
	Agents                  AgentsConfig `json:"agents"`
	PollingIntervalSeconds  int          `json:"polling_interval_seconds"`
	ContextHandoffThreshold float64      `json:"context_handoff_threshold"`
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

	return &cfg, nil
}

// DefaultConfig returns a config with sensible defaults.
func DefaultConfig(projectRoot, githubRepo string) *Config {
	return &Config{
		Version:      "1.0.0",
		TicketPrefix: "ct",
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
			Mayor:     AgentConfig{Model: "claude-opus-4-5"},
			Architect: AgentConfig{Model: "claude-opus-4-5"},
			Artisan: ArtisanConfig{}, // User-defined in config.json
			Conductor: AgentConfig{Model: "claude-sonnet-4-5"},
			Prole:     AgentConfig{Model: "claude-sonnet-4-5"},
			Janitor:   AgentConfig{Model: "claude-sonnet-4-5"},
		},
		PollingIntervalSeconds:  30,
		ContextHandoffThreshold: 0.80,
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
