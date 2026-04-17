package config

import (
	"encoding/json"
	"testing"
)

func TestDefaultConfig_quality(t *testing.T) {
	cfg := DefaultConfig("/tmp/test", "github", "owner/repo")

	if !cfg.Quality.Enabled {
		t.Error("expected Quality.Enabled=true by default")
	}
	if cfg.Quality.Checks == nil {
		t.Error("expected Quality.Checks to be non-nil by default")
	}
	// DefaultConfig seeds only the language-agnostic check (open_ticket_count).
	// Language-specific checks are applied at init time via QualityChecksForPreset.
	if len(cfg.Quality.Checks) != 1 {
		t.Errorf("expected 1 default check (agnostic only), got %d", len(cfg.Quality.Checks))
	}
	if cfg.Quality.Checks[0].Name != "open_ticket_count" {
		t.Errorf("expected default check to be open_ticket_count, got %q", cfg.Quality.Checks[0].Name)
	}
}

func TestDefaultConfig_quality_newChecks(t *testing.T) {
	cfg := DefaultConfig("/tmp/test", "github", "owner/repo")

	names := make(map[string]bool, len(cfg.Quality.Checks))
	for _, c := range cfg.Quality.Checks {
		names[c.Name] = true
	}
	if !names["open_ticket_count"] {
		t.Error("expected open_ticket_count in DefaultConfig quality checks")
	}
	// Go-specific checks must NOT be in the base default.
	for _, goOnly := range []string{"go_test_coverage", "lint_warning_count", "dependency_count"} {
		if names[goOnly] {
			t.Errorf("Go-specific check %q must not be in DefaultConfig base checks", goOnly)
		}
	}
}

func TestDefaultConfig_quality_lowerDirectionChecks(t *testing.T) {
	cfg := DefaultConfig("/tmp/test", "github", "owner/repo")

	for _, c := range cfg.Quality.Checks {
		if c.Name == "open_ticket_count" && c.Direction != "lower" {
			t.Errorf("open_ticket_count: expected direction=lower, got %q", c.Direction)
		}
	}
}

func TestDefaultConfig_quality_allMetric(t *testing.T) {
	cfg := DefaultConfig("/tmp/test", "github", "owner/repo")
	for _, c := range cfg.Quality.Checks {
		if c.Type != "metric" {
			t.Errorf("check %q: expected type=metric, got %q", c.Name, c.Type)
		}
		if !c.Enabled {
			t.Errorf("check %q: expected enabled=true", c.Name)
		}
	}
}

func TestQualityConfig_roundTrip(t *testing.T) {
	cfg := DefaultConfig("/tmp/test", "github", "owner/repo")
	cfg.Quality = QualityConfig{
		Enabled: true,
		Checks: []QualityCheckConfig{
			{
				Name:    "tests",
				Command: "go test ./...",
				Type:    "pass_fail",
				Enabled: true,
			},
			{
				Name:      "coverage",
				Command:   "go test ./... -cover",
				Type:      "metric",
				Threshold: 80.0,
				Enabled:   true,
			},
		},
	}

	data, err := json.Marshal(cfg.Quality)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got QualityConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if !got.Enabled {
		t.Error("expected Enabled=true after round-trip")
	}
	if len(got.Checks) != 2 {
		t.Fatalf("expected 2 checks, got %d", len(got.Checks))
	}

	pass := got.Checks[0]
	if pass.Name != "tests" || pass.Type != "pass_fail" || !pass.Enabled {
		t.Errorf("pass_fail check round-trip wrong: %+v", pass)
	}

	metric := got.Checks[1]
	if metric.Name != "coverage" || metric.Type != "metric" || metric.Threshold != 80.0 {
		t.Errorf("metric check round-trip wrong: %+v", metric)
	}
}

func TestQualityCheckConfig_disabledCheck(t *testing.T) {
	check := QualityCheckConfig{
		Name:    "slow-test",
		Command: "go test ./... -run TestSlow",
		Type:    "pass_fail",
		Enabled: false,
	}
	data, _ := json.Marshal(check)
	var got QualityCheckConfig
	json.Unmarshal(data, &got) //nolint:errcheck // test-only round-trip
	if got.Enabled {
		t.Error("expected Enabled=false after round-trip")
	}
}

func TestQualityCheckConfig_zeroThreshold(t *testing.T) {
	// Threshold defaults to zero when not specified (pass_fail checks).
	check := QualityCheckConfig{
		Name:    "vet",
		Command: "go vet ./...",
		Type:    "pass_fail",
		Enabled: true,
	}
	if check.Threshold != 0 {
		t.Errorf("expected zero Threshold, got %f", check.Threshold)
	}
}

func TestQualityCheckConfig_warnThreshold_roundTrip(t *testing.T) {
	check := QualityCheckConfig{
		Name:          "coverage",
		Command:       "cover.sh",
		Type:          "metric",
		Threshold:     80.0,
		WarnThreshold: 70.0,
		Enabled:       true,
	}
	data, err := json.Marshal(check)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got QualityCheckConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.WarnThreshold != 70.0 {
		t.Errorf("expected WarnThreshold=70.0 after round-trip, got %f", got.WarnThreshold)
	}
	if got.Threshold != 80.0 {
		t.Errorf("expected Threshold=80.0 after round-trip, got %f", got.Threshold)
	}
}

func TestQualityCheckConfig_warnThreshold_zeroDefault(t *testing.T) {
	// WarnThreshold should default to zero when not specified.
	check := QualityCheckConfig{
		Name:      "coverage",
		Command:   "cover.sh",
		Type:      "metric",
		Threshold: 80.0,
		Enabled:   true,
	}
	data, _ := json.Marshal(check)
	var got QualityCheckConfig
	json.Unmarshal(data, &got) //nolint:errcheck // test-only round-trip
	if got.WarnThreshold != 0 {
		t.Errorf("expected zero WarnThreshold, got %f", got.WarnThreshold)
	}
}

func TestQualityCheckConfig_direction_roundTrip(t *testing.T) {
	check := QualityCheckConfig{
		Name:          "todo_count",
		Command:       "grep -rE 'TODO' --include='*.go' . | wc -l",
		Type:          "metric",
		Threshold:     0,
		WarnThreshold: 5,
		Direction:     "lower",
		Enabled:       true,
	}
	data, err := json.Marshal(check)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got QualityCheckConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Direction != "lower" {
		t.Errorf("expected Direction=lower after round-trip, got %q", got.Direction)
	}
}

func TestQualityCheckConfig_direction_defaultEmpty(t *testing.T) {
	// Direction is empty string by default (interpreted as "higher" by runner).
	check := QualityCheckConfig{
		Name:      "coverage",
		Command:   "cover.sh",
		Type:      "metric",
		Threshold: 80.0,
		Enabled:   true,
	}
	if check.Direction != "" {
		t.Errorf("expected empty Direction by default, got %q", check.Direction)
	}
}
