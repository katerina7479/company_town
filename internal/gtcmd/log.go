package gtcmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/katerina7479/company_town/internal/cmdlog"
)

// Log dispatches gt log subcommands.
func Log(args []string) error {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: gt log <tail|show> [flags]")
		os.Exit(1)
	}

	logPath := cmdlog.FindLogPath()
	if logPath == "" {
		return fmt.Errorf("could not locate commands.log (no project root found)")
	}

	switch args[0] {
	case "tail":
		return logTail(logPath, args[1:])
	case "show":
		return logShow(logPath, args[1:])
	default:
		return fmt.Errorf("unknown log command: %s", args[0])
	}
}

// logTail prints the last N lines of the command log, human-formatted.
// Flags: -n <N> (default 20), --json (raw JSONL).
func logTail(logPath string, args []string) error {
	n := 20
	jsonMode := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-n":
			if i+1 >= len(args) {
				return fmt.Errorf("-n requires a value")
			}
			i++
			v, err := strconv.Atoi(args[i])
			if err != nil || v <= 0 {
				return fmt.Errorf("invalid -n value: %s", args[i])
			}
			n = v
		case "--json":
			jsonMode = true
		default:
			return fmt.Errorf("unknown flag: %s", args[i])
		}
	}

	lines, err := readLastN(logPath, n)
	if err != nil {
		return err
	}

	for _, line := range lines {
		if jsonMode {
			fmt.Println(line)
			continue
		}
		renderLine(line)
	}
	return nil
}

// logShow filters the command log by entity, actor, and/or since, then prints.
// Flags: --entity <id>, --actor <name>, --since <duration>, --json.
func logShow(logPath string, args []string) error {
	var entityFilter, actorFilter string
	var sinceFilter time.Time
	jsonMode := false

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--entity":
			if i+1 >= len(args) {
				return fmt.Errorf("--entity requires a value")
			}
			i++
			entityFilter = args[i]
		case "--actor":
			if i+1 >= len(args) {
				return fmt.Errorf("--actor requires a value")
			}
			i++
			actorFilter = args[i]
		case "--since":
			if i+1 >= len(args) {
				return fmt.Errorf("--since requires a value")
			}
			i++
			d, err := time.ParseDuration(args[i])
			if err != nil {
				return fmt.Errorf("invalid --since duration %q: %w", args[i], err)
			}
			sinceFilter = time.Now().UTC().Add(-d)
		case "--json":
			jsonMode = true
		default:
			return fmt.Errorf("unknown flag: %s", args[i])
		}
	}

	if entityFilter == "" && actorFilter == "" && sinceFilter.IsZero() {
		return fmt.Errorf("usage: gt log show --entity <id> | --actor <name> | --since <duration>")
	}

	f, err := os.Open(logPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("commands.log not found at %s (no commands have been logged yet)", logPath)
	}
	if err != nil {
		return fmt.Errorf("opening commands.log: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var entry cmdlog.Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue // skip malformed lines
		}

		if !sinceFilter.IsZero() && entry.Timestamp.Before(sinceFilter) {
			continue
		}
		if actorFilter != "" && entry.Actor != actorFilter {
			continue
		}
		if entityFilter != "" && !matchesEntity(entry, entityFilter) {
			continue
		}

		if jsonMode {
			fmt.Println(line)
		} else {
			renderEntry(entry)
		}
	}
	return scanner.Err()
}

// matchesEntity returns true if any annotation in entry matches the given filter.
// Filter forms:
//   - "nc-56" or "CT-100" -> matches entity "ticket=<number>"
//   - "type=id" (e.g. "ticket=56", "agent=copper") -> exact entity match
//   - bare name (e.g. "copper") -> matches entity "agent=<name>"
func matchesEntity(entry cmdlog.Entry, filter string) bool {
	canonical := canonicalEntity(filter)
	for _, a := range entry.Entities {
		if a.Entity == canonical || strings.Contains(a.Entity, canonical) {
			return true
		}
	}
	return false
}

// canonicalEntity converts a user-supplied entity filter to the form stored
// in log entries ("type=id"). E.g. "nc-56" -> "ticket=56", "copper" -> "agent=copper".
func canonicalEntity(s string) string {
	// Already in "type=id" form.
	if strings.Contains(s, "=") {
		return s
	}
	// "PREFIX-NUMBER" ticket reference (e.g. "nc-56", "CT-100").
	if idx := strings.LastIndex(s, "-"); idx >= 0 {
		suffix := s[idx+1:]
		if _, err := strconv.Atoi(suffix); err == nil {
			return "ticket=" + suffix
		}
	}
	// Bare name -- assume agent.
	return "agent=" + s
}

// readLastN returns the last n non-empty lines from path.
// Returns nil (no error) if the file does not exist.
func readLastN(path string, n int) ([]string, error) {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("opening commands.log: %w", err)
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		if l := scanner.Text(); l != "" {
			lines = append(lines, l)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, nil
}

// renderLine parses a raw JSONL line and renders it; skips malformed lines.
func renderLine(raw string) {
	var entry cmdlog.Entry
	if err := json.Unmarshal([]byte(raw), &entry); err != nil {
		return
	}
	renderEntry(entry)
}

// renderEntry prints a human-readable single-line summary of an entry,
// followed by entity annotations if present.
func renderEntry(e cmdlog.Entry) {
	ts := e.Timestamp.Local().Format("2006-01-02 15:04:05")
	cmd := strings.Join(append([]string{e.Binary}, e.Args...), " ")
	status := fmt.Sprintf("exit:%d  %dms", e.ExitCode, e.DurationMs)
	fmt.Printf("%s  %-12s  %s  (%s)\n", ts, e.Actor, cmd, status)
	for _, a := range e.Entities {
		if a.Before != "" || a.After != "" {
			fmt.Printf("  %s  %s->%s\n", a.Entity, a.Before, a.After)
		} else {
			fmt.Printf("  %s\n", a.Entity)
		}
	}
	if e.Error != "" {
		fmt.Printf("  error: %s\n", e.Error)
	}
}
