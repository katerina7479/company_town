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
	// DefaultConfig now seeds 6 pass_fail checks + go_test_coverage = 7 total.
	if len(cfg.Quality.Checks) != 7 {
		t.Errorf("expected 7 default checks, got %d", len(cfg.Quality.Checks))
	}

	// Verify go_test_coverage is present with correct targets.
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
		t.Errorf("expected type=metric, got %q", cov.Type)
	}
	if cov.Target != 80.0 {
		t.Errorf("expected target=80, got %v", cov.Target)
	}
	if cov.WarnTarget != 70.0 {
		t.Errorf("expected warn_target=70, got %v", cov.WarnTarget)
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
				Name:    "coverage",
				Command: "go test ./... -cover",
				Type:    "metric",
				Target:  80.0,
				Enabled: true,
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
	if metric.Name != "coverage" || metric.Type != "metric" || metric.Target != 80.0 {
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

func TestQualityCheckConfig_zeroTarget(t *testing.T) {
	// Target defaults to zero when not specified (pass_fail checks).
	check := QualityCheckConfig{
		Name:    "vet",
		Command: "go vet ./...",
		Type:    "pass_fail",
		Enabled: true,
	}
	if check.Target != 0 {
		t.Errorf("expected zero Target, got %f", check.Target)
	}
}

func TestQualityCheckConfig_warnTarget_roundTrip(t *testing.T) {
	check := QualityCheckConfig{
		Name:       "coverage",
		Command:    "cover.sh",
		Type:       "metric",
		Target:     80.0,
		WarnTarget: 70.0,
		Enabled:    true,
	}
	data, err := json.Marshal(check)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got QualityCheckConfig
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.WarnTarget != 70.0 {
		t.Errorf("expected WarnTarget=70.0 after round-trip, got %f", got.WarnTarget)
	}
	if got.Target != 80.0 {
		t.Errorf("expected Target=80.0 after round-trip, got %f", got.Target)
	}
}

func TestQualityCheckConfig_warnTarget_zeroDefault(t *testing.T) {
	// WarnTarget should default to zero when not specified.
	check := QualityCheckConfig{
		Name:    "coverage",
		Command: "cover.sh",
		Type:    "metric",
		Target:  80.0,
		Enabled: true,
	}
	data, _ := json.Marshal(check)
	var got QualityCheckConfig
	json.Unmarshal(data, &got) //nolint:errcheck // test-only round-trip
	if got.WarnTarget != 0 {
		t.Errorf("expected zero WarnTarget, got %f", got.WarnTarget)
	}
}
