package quality

import (
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/katerina7479/company_town/internal/config"
)

// Runner executes quality checks defined in config and produces a Baseline.
type Runner struct {
	projectRoot string
	execCommand func(name string, args ...string) *exec.Cmd
}

// New creates a Runner that executes checks in the given project root.
func New(projectRoot string) *Runner {
	return &Runner{
		projectRoot: projectRoot,
		execCommand: exec.Command,
	}
}

// Run executes all enabled checks and returns a Baseline.
// Disabled checks are skipped entirely (not included in the Baseline).
func (r *Runner) Run(checks []config.QualityCheckConfig) *Baseline {
	baseline := &Baseline{RunAt: time.Now()}
	for _, cfg := range checks {
		if !cfg.Enabled {
			continue
		}
		baseline.Results = append(baseline.Results, r.runCheck(cfg))
	}
	return baseline
}

func (r *Runner) runCheck(cfg config.QualityCheckConfig) Result {
	result := Result{
		CheckName: cfg.Name,
		RunAt:     time.Now(),
	}

	cmd := r.execCommand("sh", "-c", cfg.Command)
	cmd.Dir = r.projectRoot
	out, err := cmd.CombinedOutput()
	result.Output = string(out)

	if cfg.Type == string(CheckTypeMetric) {
		return r.evalMetric(result, cfg.Target, cfg.WarnTarget, out, err)
	}
	return r.evalPassFail(result, err)
}

func (r *Runner) evalPassFail(result Result, err error) Result {
	if err == nil {
		result.Status = StatusPass
		return result
	}
	if isExitError(err) {
		result.Status = StatusFail
	} else {
		result.Status = StatusError
		result.Err = fmt.Sprintf("could not run check: %v", err)
	}
	return result
}

func (r *Runner) evalMetric(result Result, threshold, warnThreshold float64, out []byte, err error) Result {
	if err != nil {
		result.Status = StatusError
		result.Err = fmt.Sprintf("could not run check: %v", err)
		return result
	}

	raw := strings.TrimSpace(string(out))
	val, parseErr := strconv.ParseFloat(raw, 64)
	if parseErr != nil {
		result.Status = StatusError
		result.Err = fmt.Sprintf("could not parse metric value %q: %v", raw, parseErr)
		return result
	}

	result.Value = &val
	switch {
	case val >= threshold:
		result.Status = StatusPass
	case warnThreshold > 0 && val >= warnThreshold:
		result.Status = StatusWarn
	default:
		result.Status = StatusFail
	}
	return result
}

// isExitError reports whether err is a non-zero exit from a command that ran.
// A non-ExitError means the command could not be started at all.
func isExitError(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr)
}
