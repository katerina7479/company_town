package config

import (
	"os"
	"path/filepath"
	"testing"
)

// --- DetectLanguagePreset ---

func TestDetectLanguagePreset_goProject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if got := DetectLanguagePreset(dir); got != "go" {
		t.Errorf("DetectLanguagePreset = %q, want %q", got, "go")
	}
}

func TestDetectLanguagePreset_pythonPyproject(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pyproject.toml"), []byte("[tool.pytest]\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if got := DetectLanguagePreset(dir); got != "python" {
		t.Errorf("DetectLanguagePreset = %q, want %q", got, "python")
	}
}

func TestDetectLanguagePreset_pythonSetupPy(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "setup.py"), []byte("from setuptools import setup\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if got := DetectLanguagePreset(dir); got != "python" {
		t.Errorf("DetectLanguagePreset = %q, want %q", got, "python")
	}
}

func TestDetectLanguagePreset_pythonRequirements(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if got := DetectLanguagePreset(dir); got != "python" {
		t.Errorf("DetectLanguagePreset = %q, want %q", got, "python")
	}
}

func TestDetectLanguagePreset_goTakesPrecedenceOverPython(t *testing.T) {
	// go.mod + requirements.txt in same dir: Go wins (checked first).
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask\n"), 0644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	if got := DetectLanguagePreset(dir); got != "go" {
		t.Errorf("DetectLanguagePreset = %q, want %q", got, "go")
	}
}

func TestDetectLanguagePreset_unknownProject(t *testing.T) {
	dir := t.TempDir()
	if got := DetectLanguagePreset(dir); got != "" {
		t.Errorf("DetectLanguagePreset = %q, want empty string", got)
	}
}

// --- GoQualityChecks ---

func TestGoQualityChecks_count(t *testing.T) {
	checks := GoQualityChecks()
	if len(checks) != 7 {
		t.Errorf("GoQualityChecks: expected 7 checks, got %d", len(checks))
	}
}

func TestGoQualityChecks_containsExpected(t *testing.T) {
	checks := GoQualityChecks()
	names := make(map[string]bool, len(checks))
	for _, c := range checks {
		names[c.Name] = true
	}
	for _, want := range []string{
		"go_test_coverage", "lint_warning_count", "loc_total",
		"todo_count", "test_count", "dependency_count", "open_ticket_count",
	} {
		if !names[want] {
			t.Errorf("GoQualityChecks: missing check %q", want)
		}
	}
}

func TestGoQualityChecks_allEnabledMetric(t *testing.T) {
	for _, c := range GoQualityChecks() {
		if !c.Enabled {
			t.Errorf("GoQualityChecks: check %q not enabled", c.Name)
		}
		if c.Type != "metric" {
			t.Errorf("GoQualityChecks: check %q type = %q, want metric", c.Name, c.Type)
		}
	}
}

// --- PythonQualityChecks ---

func TestPythonQualityChecks_count(t *testing.T) {
	checks := PythonQualityChecks()
	if len(checks) != 7 {
		t.Errorf("PythonQualityChecks: expected 7 checks, got %d", len(checks))
	}
}

func TestPythonQualityChecks_containsExpected(t *testing.T) {
	checks := PythonQualityChecks()
	names := make(map[string]bool, len(checks))
	for _, c := range checks {
		names[c.Name] = true
	}
	for _, want := range []string{
		"python_test_coverage", "lint_warning_count", "loc_total",
		"todo_count", "test_count", "dependency_count", "open_ticket_count",
	} {
		if !names[want] {
			t.Errorf("PythonQualityChecks: missing check %q", want)
		}
	}
}

func TestPythonQualityChecks_noGoChecks(t *testing.T) {
	checks := PythonQualityChecks()
	for _, c := range checks {
		if c.Name == "go_test_coverage" {
			t.Error("PythonQualityChecks must not contain go_test_coverage")
		}
	}
}

func TestPythonQualityChecks_allEnabledMetric(t *testing.T) {
	for _, c := range PythonQualityChecks() {
		if !c.Enabled {
			t.Errorf("PythonQualityChecks: check %q not enabled", c.Name)
		}
		if c.Type != "metric" {
			t.Errorf("PythonQualityChecks: check %q type = %q, want metric", c.Name, c.Type)
		}
	}
}

// --- AgnosticQualityChecks ---

func TestAgnosticQualityChecks_onlyOpenTicket(t *testing.T) {
	checks := AgnosticQualityChecks()
	if len(checks) != 1 {
		t.Fatalf("AgnosticQualityChecks: expected 1 check, got %d", len(checks))
	}
	if checks[0].Name != "open_ticket_count" {
		t.Errorf("AgnosticQualityChecks: expected open_ticket_count, got %q", checks[0].Name)
	}
}

// --- QualityChecksForPreset ---

func TestQualityChecksForPreset_go(t *testing.T) {
	checks := QualityChecksForPreset("go")
	if len(checks) != len(GoQualityChecks()) {
		t.Errorf("QualityChecksForPreset(go): got %d checks, want %d", len(checks), len(GoQualityChecks()))
	}
}

func TestQualityChecksForPreset_python(t *testing.T) {
	checks := QualityChecksForPreset("python")
	if len(checks) != len(PythonQualityChecks()) {
		t.Errorf("QualityChecksForPreset(python): got %d checks, want %d", len(checks), len(PythonQualityChecks()))
	}
}

func TestQualityChecksForPreset_emptyFallsToAgnostic(t *testing.T) {
	checks := QualityChecksForPreset("")
	agnostic := AgnosticQualityChecks()
	if len(checks) != len(agnostic) {
		t.Errorf("QualityChecksForPreset(''): got %d checks, want %d (agnostic)", len(checks), len(agnostic))
	}
}

func TestQualityChecksForPreset_unknownFallsToAgnostic(t *testing.T) {
	checks := QualityChecksForPreset("rust")
	agnostic := AgnosticQualityChecks()
	if len(checks) != len(agnostic) {
		t.Errorf("QualityChecksForPreset(rust): got %d checks, want %d (agnostic)", len(checks), len(agnostic))
	}
}
