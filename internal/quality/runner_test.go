package quality

import (
	"os/exec"
	"testing"

	"github.com/katerina7479/company_town/internal/config"
)

// fakeExec returns an execCommand function that always runs "sh -c <script>".
func fakeExec(script string) func(string, ...string) *exec.Cmd {
	return func(_ string, _ ...string) *exec.Cmd {
		return exec.Command("sh", "-c", script)
	}
}

func newTestRunner(script string) *Runner {
	return &Runner{
		projectRoot: ".",
		execCommand: fakeExec(script),
	}
}

func passFail(name, command string, enabled bool) config.QualityCheckConfig {
	return config.QualityCheckConfig{
		Name:    name,
		Command: command,
		Type:    string(CheckTypePassFail),
		Enabled: enabled,
	}
}

func metricCheck(name, command string, threshold float64) config.QualityCheckConfig {
	return config.QualityCheckConfig{
		Name:      name,
		Command:   command,
		Type:      string(CheckTypeMetric),
		Threshold: threshold,
		Enabled:   true,
	}
}

// --- pass_fail tests ---

func TestRunner_PassFail_pass(t *testing.T) {
	r := newTestRunner("exit 0")
	baseline := r.Run([]config.QualityCheckConfig{passFail("vet", "go vet ./...", true)})

	if len(baseline.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(baseline.Results))
	}
	res := baseline.Results[0]
	if res.Status != StatusPass {
		t.Errorf("expected StatusPass, got %q", res.Status)
	}
	if res.CheckName != "vet" {
		t.Errorf("expected CheckName=vet, got %q", res.CheckName)
	}
}

func TestRunner_PassFail_fail(t *testing.T) {
	r := newTestRunner("exit 1")
	baseline := r.Run([]config.QualityCheckConfig{passFail("tests", "go test ./...", true)})

	if len(baseline.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(baseline.Results))
	}
	if baseline.Results[0].Status != StatusFail {
		t.Errorf("expected StatusFail, got %q", baseline.Results[0].Status)
	}
}

func TestRunner_PassFail_commandNotFound(t *testing.T) {
	r := &Runner{
		projectRoot: ".",
		execCommand: func(_ string, _ ...string) *exec.Cmd {
			// Return a command that cannot be executed
			return exec.Command("__no_such_binary_xyz__")
		},
	}
	baseline := r.Run([]config.QualityCheckConfig{passFail("missing", "missing-tool", true)})

	if baseline.Results[0].Status != StatusError {
		t.Errorf("expected StatusError for missing binary, got %q", baseline.Results[0].Status)
	}
	if baseline.Results[0].Err == "" {
		t.Error("expected non-empty Err for missing binary")
	}
}

func TestRunner_PassFail_capturesOutput(t *testing.T) {
	r := newTestRunner("echo hello; exit 1")
	baseline := r.Run([]config.QualityCheckConfig{passFail("check", "cmd", true)})

	if baseline.Results[0].Output == "" {
		t.Error("expected non-empty Output")
	}
}

func TestRunner_DisabledChecksSkipped(t *testing.T) {
	r := newTestRunner("exit 0")
	checks := []config.QualityCheckConfig{
		passFail("enabled", "cmd", true),
		passFail("disabled", "cmd", false),
	}
	baseline := r.Run(checks)

	if len(baseline.Results) != 1 {
		t.Errorf("expected 1 result (disabled skipped), got %d", len(baseline.Results))
	}
	if baseline.Results[0].CheckName != "enabled" {
		t.Errorf("unexpected check name: %q", baseline.Results[0].CheckName)
	}
}

func TestRunner_EmptyChecks(t *testing.T) {
	r := newTestRunner("exit 0")
	baseline := r.Run(nil)

	if baseline == nil {
		t.Fatal("expected non-nil Baseline")
	}
	if len(baseline.Results) != 0 {
		t.Errorf("expected 0 results, got %d", len(baseline.Results))
	}
	if baseline.RunAt.IsZero() {
		t.Error("expected non-zero RunAt")
	}
}

func TestRunner_MultipleChecks_allPass(t *testing.T) {
	r := newTestRunner("exit 0")
	checks := []config.QualityCheckConfig{
		passFail("a", "cmd", true),
		passFail("b", "cmd", true),
		passFail("c", "cmd", true),
	}
	baseline := r.Run(checks)

	if len(baseline.Results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(baseline.Results))
	}
	if !baseline.Pass() {
		t.Error("expected baseline to pass")
	}
}

// --- metric tests ---

func TestRunner_Metric_pass(t *testing.T) {
	r := newTestRunner("echo 90.5")
	baseline := r.Run([]config.QualityCheckConfig{metricCheck("coverage", "cmd", 80.0)})

	res := baseline.Results[0]
	if res.Status != StatusPass {
		t.Errorf("expected StatusPass for 90.5 >= 80.0, got %q", res.Status)
	}
	if res.Value == nil || *res.Value != 90.5 {
		t.Errorf("expected Value=90.5, got %v", res.Value)
	}
}

func TestRunner_Metric_fail(t *testing.T) {
	r := newTestRunner("echo 70.0")
	baseline := r.Run([]config.QualityCheckConfig{metricCheck("coverage", "cmd", 80.0)})

	res := baseline.Results[0]
	if res.Status != StatusFail {
		t.Errorf("expected StatusFail for 70.0 < 80.0, got %q", res.Status)
	}
	if res.Value == nil || *res.Value != 70.0 {
		t.Errorf("expected Value=70.0, got %v", res.Value)
	}
}

func TestRunner_Metric_exactlyAtThreshold(t *testing.T) {
	r := newTestRunner("echo 80")
	baseline := r.Run([]config.QualityCheckConfig{metricCheck("coverage", "cmd", 80.0)})

	if baseline.Results[0].Status != StatusPass {
		t.Errorf("expected StatusPass at threshold, got %q", baseline.Results[0].Status)
	}
}

func TestRunner_Metric_unparseable(t *testing.T) {
	r := newTestRunner("echo 'not a number'")
	baseline := r.Run([]config.QualityCheckConfig{metricCheck("coverage", "cmd", 80.0)})

	res := baseline.Results[0]
	if res.Status != StatusError {
		t.Errorf("expected StatusError for unparseable output, got %q", res.Status)
	}
	if res.Err == "" {
		t.Error("expected non-empty Err for unparseable output")
	}
}

func TestRunner_Metric_commandFails(t *testing.T) {
	r := newTestRunner("exit 2")
	baseline := r.Run([]config.QualityCheckConfig{metricCheck("coverage", "cmd", 80.0)})

	if baseline.Results[0].Status != StatusError {
		t.Errorf("expected StatusError when metric command fails, got %q", baseline.Results[0].Status)
	}
}

func TestRunner_ResultsHaveRunAt(t *testing.T) {
	r := newTestRunner("exit 0")
	baseline := r.Run([]config.QualityCheckConfig{passFail("check", "cmd", true)})

	if baseline.Results[0].RunAt.IsZero() {
		t.Error("expected non-zero RunAt on result")
	}
}
