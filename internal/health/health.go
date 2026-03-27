package health

// Status represents the outcome of a health check.
type Status string

const (
	StatusOK   Status = "ok"
	StatusWarn Status = "warn"
	StatusFail Status = "fail"
)

// Result holds the outcome of a single health check.
type Result struct {
	Name    string
	Status  Status
	Message string
}

// Check is implemented by anything that can report its health.
type Check interface {
	Name() string
	Run() Result
}

// Runner holds a set of checks and executes them.
type Runner struct {
	checks []Check
}

// Register adds a check to the runner.
func (r *Runner) Register(c Check) {
	r.checks = append(r.checks, c)
}

// RunAll executes every registered check and returns all results.
func (r *Runner) RunAll() []Result {
	results := make([]Result, len(r.checks))
	for i, c := range r.checks {
		results[i] = c.Run()
	}
	return results
}

// Overall returns the worst status across a set of results.
// StatusFail > StatusWarn > StatusOK.
func Overall(results []Result) Status {
	status := StatusOK
	for _, r := range results {
		switch r.Status {
		case StatusFail:
			return StatusFail
		case StatusWarn:
			status = StatusWarn
		}
	}
	return status
}
