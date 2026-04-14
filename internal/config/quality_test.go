package config

import (
	"encoding/json"
	"testing"
)

func TestDefaultConfig_quality(t *testing.T) {
	cfg := DefaultConfig("/tmp/test", "owner/repo")

	if !cfg.Quality.Enabled {
		t.Error("expected Quality.Enabled=true by default")
	}
	if cfg.Quality.Checks == nil {
		t.Error("expected Quality.Checks to be non-nil by default")
	}
	// DefaultConfig seeds go_test_coverage so new ct init runs are coverage-aware.
	if len(cfg.Quality.Checks) != 1 {
		t.Errorf("expected 1 default check, got %d", len(cfg.Quality.Checks))
	}
	cov := cfg.Quality.Checks[0]
	if cov.Name != "go_test_coverage" {
		t.Errorf("expected name=go_test_coverage, got %q", cov.Name)
	}
	if cov.Type != "metric" {
		t.Errorf("expected type=metric, got %q", cov.Type)
	}
	if cov.Threshold != 70.0 {
		t.Errorf("expected threshold=70, got %v", cov.Threshold)
	}
	if cov.WarnThreshold != 60.0 {
		t.Errorf("expected warn_threshold=60, got %v", cov.WarnThreshold)
	}
	if !cov.Enabled {
		t.Error("expected go_test_coverage enabled=true")
	}
}

func TestQualityConfig_roundTrip(t *testing.T) {
	cfg := DefaultConfig("/tmp/test", "owner/repo")
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
	json.Unmarshal(data, &got)
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
		t.Errorf("expected zero threshold, got %f", check.Threshold)
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
	json.Unmarshal(data, &got)
	if got.WarnThreshold != 0 {
		t.Errorf("expected zero WarnThreshold, got %f", got.WarnThreshold)
	}
}
