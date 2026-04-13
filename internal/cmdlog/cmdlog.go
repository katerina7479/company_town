// Package cmdlog provides JSONL command logging for gt and ct invocations.
// Every invocation appends one structured line to .company_town/logs/commands.log,
// capturing the actor, arguments, timing, exit code, and touched entities.
//
// Usage in a CLI main:
//
//	err := cmdlog.Run(cmdlog.FindLogPath(), "gt", actor, os.Args[1:], func() error {
//	    return dispatch(cmd, args)
//	})
//
// Usage in a command handler that mutates state:
//
//	cmdlog.Annotate("ticket=56", "open", "in_progress")
package cmdlog

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
)

// Annotation records one entity touched during a command invocation.
// Entity should be in "type=id" form, e.g. "ticket=56" or "agent=copper".
// Before and After capture the field values before and after mutation; both
// are empty for read-only access.
type Annotation struct {
	Entity string `json:"entity"`
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
}

// Entry is one JSONL record written to commands.log per invocation.
type Entry struct {
	Timestamp  time.Time    `json:"timestamp"`
	Actor      string       `json:"actor"`
	Binary     string       `json:"binary"`
	Args       []string     `json:"args"`
	WorkDir    string       `json:"work_dir"`
	ExitCode   int          `json:"exit_code"`
	DurationMs int64        `json:"duration_ms"`
	Entities   []Annotation `json:"entities,omitempty"`
	Error      string       `json:"error,omitempty"`
}

// pending holds annotations accumulated during the current invocation.
// Protected by mu. Safe because gt/ct are single-dispatch CLI binaries.
var (
	mu      sync.Mutex
	pending []Annotation
)

// Annotate records that the current command touched entity.
// entity must be in "type=id" form (e.g. "ticket=56", "agent=copper").
// before and after are the state before and after mutation; pass empty strings
// for read-only touches.
//
// Annotate is a no-op if called outside of a Run invocation.
func Annotate(entity, before, after string) {
	mu.Lock()
	defer mu.Unlock()
	pending = append(pending, Annotation{Entity: entity, Before: before, After: after})
}

// Run wraps fn with command logging. It captures the start time, runs fn,
// collects any Annotate calls made during fn, and appends one JSONL line to
// logPath. If logPath is empty, no log entry is written. Log write errors are
// silently dropped — they never abort or slow the command. Run returns fn's
// error unchanged.
func Run(logPath, binary, actor string, args []string, fn func() error) error {
	mu.Lock()
	pending = nil
	mu.Unlock()

	start := time.Now()
	fnErr := fn()
	elapsed := time.Since(start)

	mu.Lock()
	ents := pending
	pending = nil
	mu.Unlock()

	if logPath != "" {
		exitCode := 0
		errStr := ""
		if fnErr != nil {
			exitCode = 1
			errStr = fnErr.Error()
		}

		wd, _ := os.Getwd()

		entry := Entry{
			Timestamp:  start.UTC(),
			Actor:      actor,
			Binary:     binary,
			Args:       args,
			WorkDir:    wd,
			ExitCode:   exitCode,
			DurationMs: elapsed.Milliseconds(),
			Entities:   ents,
			Error:      errStr,
		}

		_ = appendEntry(logPath, entry) // silently drop write errors
	}

	return fnErr
}

// Actor returns the effective actor for the current process: the value of
// CT_AGENT_NAME, then USER, then "unknown".
func Actor() string {
	if v := os.Getenv("CT_AGENT_NAME"); v != "" {
		return v
	}
	if v := os.Getenv("USER"); v != "" {
		return v
	}
	return "unknown"
}

// FindLogPath returns the commands.log path for the nearest company town
// project, by walking up from the current working directory. Returns empty
// string if no project root is found.
func FindLogPath() string {
	root, err := db.FindProjectRoot()
	if err != nil {
		return ""
	}
	return filepath.Join(root, config.DirName, "logs", "commands.log")
}

// appendEntry serialises entry as a JSONL line and appends it to path.
func appendEntry(path string, entry Entry) error {
	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("cmdlog: marshal entry: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("cmdlog: create log dir: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("cmdlog: open log file: %w", err)
	}
	defer f.Close()

	if _, err := fmt.Fprintf(f, "%s\n", line); err != nil {
		return fmt.Errorf("cmdlog: write entry: %w", err)
	}
	return nil
}
