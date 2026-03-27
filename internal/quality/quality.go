// Package quality defines shared types for the project quality check system.
// Config is in internal/config; DB persistence is in internal/repo; execution
// is in the quality check runner. This package holds only the data types
// shared across all three layers.
package quality

import "time"

// CheckType classifies how a check result is evaluated.
type CheckType string

const (
	// CheckTypePassFail is a check whose exit code determines pass (0) or fail.
	CheckTypePassFail CheckType = "pass_fail"
	// CheckTypeMetric is a check that emits a numeric value compared to a threshold.
	CheckTypeMetric CheckType = "metric"
)

// Status is the evaluated outcome of a quality check.
type Status string

const (
	StatusPass Status = "pass"
	StatusFail Status = "fail"
	StatusWarn Status = "warn" // metric check below threshold but not catastrophic
	StatusError Status = "error" // check could not be run (e.g. command not found)
)

// Result holds the outcome of a single quality check execution.
type Result struct {
	// CheckName matches QualityCheckConfig.Name.
	CheckName string
	// Status is the evaluated outcome.
	Status Status
	// Output is the combined stdout+stderr of the check command.
	Output string
	// Value is set for metric checks (e.g. coverage percentage).
	// Nil for pass_fail checks.
	Value *float64
	// RunAt is when the check was executed.
	RunAt time.Time
	// Err describes why the check could not run (distinct from a failing check).
	Err string
}

// Baseline is a snapshot of all quality check results for a project at a point in time.
type Baseline struct {
	RunAt   time.Time
	Results []Result
}

// Pass reports whether every result in the baseline passed.
func (b *Baseline) Pass() bool {
	for _, r := range b.Results {
		if r.Status != StatusPass {
			return false
		}
	}
	return true
}

// FailedChecks returns results that did not pass.
func (b *Baseline) FailedChecks() []Result {
	var failed []Result
	for _, r := range b.Results {
		if r.Status != StatusPass {
			failed = append(failed, r)
		}
	}
	return failed
}
