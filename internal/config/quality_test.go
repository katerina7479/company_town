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
		t.Error("expected Quality.Checks to be non-nil (empty slice) by default")
	}
	if len(cfg.Quality.Checks) != 0 {
		t.Errorf("expected 0 default checks, got %d", len(cfg.Quality.Checks))
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
