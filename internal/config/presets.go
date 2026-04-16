package config

import (
	"os"
	"path/filepath"
)

// DetectLanguagePreset inspects dir for language marker files and returns
// "go", "python", or "" when no known language is detected.
func DetectLanguagePreset(dir string) string {
	if fileExistsIn(dir, "go.mod") {
		return "go"
	}
	for _, f := range []string{"pyproject.toml", "setup.py", "requirements.txt"} {
		if fileExistsIn(dir, f) {
			return "python"
		}
	}
	return ""
}

func fileExistsIn(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}

// GoQualityChecks returns the standard quality checks for a Go project.
func GoQualityChecks() []QualityCheckConfig {
	return []QualityCheckConfig{
		{
			Name:          "go_test_coverage",
			Command:       "go test $(go list ./...) -coverprofile=.company_town/.coverage.out >/dev/null 2>&1; go tool cover -func=.company_town/.coverage.out 2>/dev/null | awk '/^total:/ {gsub(\"%\",\"\"); print $3}'",
			Type:          "metric",
			Threshold:     60.0,
			WarnThreshold: 50.0,
			Enabled:       true,
		},
		{
			Name:          "lint_warning_count",
			Command:       "golangci-lint run --no-color ./... 2>&1 | grep -cE '\\.go:[0-9]+:[0-9]+:'; true",
			Type:          "metric",
			Threshold:     0,
			WarnThreshold: 10,
			Direction:     "lower",
			Enabled:       true,
		},
		{
			Name:      "loc_total",
			Command:   "git ls-files '*.go' | grep -v '_test\\.go' | tr '\\n' '\\0' | xargs -0 wc -l 2>/dev/null | awk 'END{print $1+0}'",
			Type:      "metric",
			Threshold: 1000,
			Enabled:   true,
		},
		{
			Name:          "todo_count",
			Command:       "grep -rE 'TODO|FIXME|XXX' --include='*.go' . 2>/dev/null | wc -l",
			Type:          "metric",
			Threshold:     0,
			WarnThreshold: 5,
			Direction:     "lower",
			Enabled:       true,
		},
		{
			Name:          "test_count",
			Command:       "grep -r '^func Test' --include='*.go' . 2>/dev/null | wc -l",
			Type:          "metric",
			Threshold:     50,
			WarnThreshold: 20,
			Enabled:       true,
		},
		{
			Name:          "dependency_count",
			Command:       "go list -m all 2>/dev/null | tail -n +2 | wc -l",
			Type:          "metric",
			Threshold:     50,
			WarnThreshold: 75,
			Direction:     "lower",
			Enabled:       true,
		},
		agnosticOpenTicketCheck(),
	}
}

// PythonQualityChecks returns the standard quality checks for a Python project.
func PythonQualityChecks() []QualityCheckConfig {
	return []QualityCheckConfig{
		{
			Name:          "python_test_coverage",
			Command:       "python -m pytest --cov=. --cov-report=term-missing 2>/dev/null | awk '/^TOTAL/ {gsub(\"%\",\"\"); print $4+0}'",
			Type:          "metric",
			Threshold:     60.0,
			WarnThreshold: 50.0,
			Enabled:       true,
		},
		{
			Name:          "lint_warning_count",
			Command:       "ruff check . 2>/dev/null | grep -c '^[^ ]'; true",
			Type:          "metric",
			Threshold:     0,
			WarnThreshold: 10,
			Direction:     "lower",
			Enabled:       true,
		},
		{
			Name:      "loc_total",
			Command:   "git ls-files '*.py' | grep -vE 'test_.*\\.py|.*_test\\.py' | tr '\\n' '\\0' | xargs -0 wc -l 2>/dev/null | awk 'END{print $1+0}'",
			Type:      "metric",
			Threshold: 1000,
			Enabled:   true,
		},
		{
			Name:          "todo_count",
			Command:       "grep -rE 'TODO|FIXME|XXX' --include='*.py' . 2>/dev/null | wc -l",
			Type:          "metric",
			Threshold:     0,
			WarnThreshold: 5,
			Direction:     "lower",
			Enabled:       true,
		},
		{
			Name:          "test_count",
			Command:       "grep -r '^\\s*def test_' --include='*.py' . 2>/dev/null | wc -l",
			Type:          "metric",
			Threshold:     50,
			WarnThreshold: 20,
			Enabled:       true,
		},
		{
			Name:          "dependency_count",
			Command:       "pip freeze 2>/dev/null | wc -l",
			Type:          "metric",
			Threshold:     50,
			WarnThreshold: 75,
			Direction:     "lower",
			Enabled:       true,
		},
		agnosticOpenTicketCheck(),
	}
}

// AgnosticQualityChecks returns quality checks that work for any project
// regardless of language.
func AgnosticQualityChecks() []QualityCheckConfig {
	return []QualityCheckConfig{agnosticOpenTicketCheck()}
}

// QualityChecksForPreset returns the checks for a named language preset.
// Recognised values are "go" and "python"; anything else returns AgnosticQualityChecks.
func QualityChecksForPreset(preset string) []QualityCheckConfig {
	switch preset {
	case "go":
		return GoQualityChecks()
	case "python":
		return PythonQualityChecks()
	default:
		return AgnosticQualityChecks()
	}
}

func agnosticOpenTicketCheck() QualityCheckConfig {
	return QualityCheckConfig{
		Name:          "open_ticket_count",
		Command:       "gt ticket list 2>/dev/null | grep -cE '\\[(open|draft|in_progress|ci_running|in_review|under_review|repairing|pr_open|reviewed|merge_conflict)\\]'; true",
		Type:          "metric",
		Threshold:     10,
		WarnThreshold: 20,
		Direction:     "lower",
		Enabled:       true,
	}
}
