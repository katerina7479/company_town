package gtcmd

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/quality"
	"github.com/katerina7479/company_town/internal/repo"
)

// errChecksFailed is returned by checkRun when one or more checks did not pass.
// The caller is responsible for exiting non-zero after defers have run.
var errChecksFailed = errors.New("checks failed")

// Check dispatches gt check subcommands.
func Check(args []string) error {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gt check <run|list|history> [options]")
		os.Exit(1)
	}

	switch args[0] {
	case "run":
		err := checkRun()
		if err == errChecksFailed {
			os.Exit(1)
		}
		return err
	case "list":
		return checkList()
	case "history":
		return checkHistory(args[1:])
	default:
		return fmt.Errorf("unknown check command: %s", args[0])
	}
}

// checkRun executes all enabled quality checks, prints results, and persists them.
// Exits non-zero if any check did not pass.
func checkRun() error {
	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	if !cfg.Quality.Enabled {
		fmt.Println("Quality checks disabled (quality.enabled=false in config).")
		return nil
	}
	if len(cfg.Quality.Checks) == 0 {
		fmt.Println("No quality checks configured.")
		return nil
	}

	metrics := repo.NewQualityMetricRepo(conn)
	runner := quality.New(cfg.ProjectRoot)
	baseline := runner.Run(cfg.Quality.Checks)

	anyFail := false
	for _, r := range baseline.Results {
		icon := statusIcon(string(r.Status))
		line := fmt.Sprintf("%s  %-20s  %s", icon, r.CheckName, r.Status)
		if r.Value != nil {
			line += fmt.Sprintf("  (%.2f)", *r.Value)
		}
		if r.Err != "" {
			line += fmt.Sprintf("  [%s]", r.Err)
		}
		fmt.Println(line)

		m := &repo.QualityMetric{
			CheckName: r.CheckName,
			Status:    string(r.Status),
			Output:    r.Output,
			RunAt:     r.RunAt,
			Error:     r.Err,
		}
		if r.Value != nil {
			m.Value = sql.NullFloat64{Float64: *r.Value, Valid: true}
		}
		if err := metrics.Record(m); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not persist result for %q: %v\n", r.CheckName, err)
		}

		if r.Status != quality.StatusPass {
			anyFail = true
		}
	}

	fmt.Printf("\n%d check(s) run", len(baseline.Results))
	failed := baseline.FailedChecks()
	if len(failed) > 0 {
		names := make([]string, len(failed))
		for i, f := range failed {
			names[i] = f.CheckName
		}
		fmt.Printf(", %d failed: %v", len(failed), names)
	}
	fmt.Println()

	if anyFail {
		return errChecksFailed
	}
	return nil
}

// checkList prints the most recent result for each distinct check name.
func checkList() error {
	conn, _, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	metrics := repo.NewQualityMetricRepo(conn)
	latest, err := metrics.LatestPerCheck()
	if err != nil {
		return err
	}

	if len(latest) == 0 {
		fmt.Println("No quality check results recorded. Run `gt check run` first.")
		return nil
	}

	fmt.Printf("%-20s  %-6s  %-8s  %s\n", "CHECK", "STATUS", "VALUE", "RUN AT")
	fmt.Printf("%-20s  %-6s  %-8s  %s\n", "-----", "------", "-----", "------")
	for _, m := range latest {
		valStr := "-"
		if m.Value.Valid {
			valStr = fmt.Sprintf("%.2f", m.Value.Float64)
		}
		fmt.Printf("%-20s  %-6s  %-8s  %s\n",
			m.CheckName, m.Status, valStr, m.RunAt.Format("2006-01-02 15:04"))
	}
	return nil
}

// checkHistory prints recent results, optionally filtered to one check name.
// Usage: gt check history [<check-name>] [--limit <n>]
func checkHistory(args []string) error {
	conn, _, err := db.OpenFromWorkingDir()
	if err != nil {
		return err
	}
	defer conn.Close()

	checkName := ""
	limit := 20
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--limit":
			if i+1 >= len(args) {
				return fmt.Errorf("--limit requires a value")
			}
			i++
			n, err := strconv.Atoi(args[i])
			if err != nil || n < 1 {
				return fmt.Errorf("invalid --limit value: %s", args[i])
			}
			limit = n
		default:
			if checkName != "" {
				return fmt.Errorf("unexpected argument: %s", args[i])
			}
			checkName = args[i]
		}
	}

	metrics := repo.NewQualityMetricRepo(conn)
	var results []*repo.QualityMetric
	if checkName != "" {
		results, err = metrics.ListByCheck(checkName, limit)
	} else {
		results, err = metrics.ListRecent(limit)
	}
	if err != nil {
		return err
	}

	if len(results) == 0 {
		if checkName != "" {
			fmt.Printf("No results for check %q.\n", checkName)
		} else {
			fmt.Println("No quality check results recorded. Run `gt check run` first.")
		}
		return nil
	}

	fmt.Printf("%-20s  %-6s  %-8s  %s\n", "CHECK", "STATUS", "VALUE", "RUN AT")
	fmt.Printf("%-20s  %-6s  %-8s  %s\n", "-----", "------", "-----", "------")
	for _, m := range results {
		valStr := "-"
		if m.Value.Valid {
			valStr = fmt.Sprintf("%.2f", m.Value.Float64)
		}
		fmt.Printf("%-20s  %-6s  %-8s  %s\n",
			m.CheckName, m.Status, valStr, m.RunAt.Format("2006-01-02 15:04"))
	}
	return nil
}

func statusIcon(status string) string {
	switch status {
	case "pass":
		return "✓"
	case "fail":
		return "✗"
	case "warn":
		return "⚠"
	default:
		return "?"
	}
}
