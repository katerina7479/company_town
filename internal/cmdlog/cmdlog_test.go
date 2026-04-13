package cmdlog_test

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/katerina7479/company_town/internal/cmdlog"
)

func TestRun_WritesJSONLEntry(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "logs", "commands.log")

	err := cmdlog.Run(logPath, "gt", "testactor", []string{"ticket", "status", "56", "in_progress"}, func() error {
		return nil
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	data, readErr := os.ReadFile(logPath)
	if readErr != nil {
		t.Fatalf("log file not created: %v", readErr)
	}

	var entry cmdlog.Entry
	if err := json.Unmarshal([]byte(trimNewline(string(data))), &entry); err != nil {
		t.Fatalf("could not parse log line as JSON: %v\nraw: %s", err, data)
	}

	if entry.Actor != "testactor" {
		t.Errorf("actor = %q, want %q", entry.Actor, "testactor")
	}
	if entry.Binary != "gt" {
		t.Errorf("binary = %q, want %q", entry.Binary, "gt")
	}
	if entry.ExitCode != 0 {
		t.Errorf("exit_code = %d, want 0", entry.ExitCode)
	}
	if len(entry.Args) != 4 {
		t.Errorf("args len = %d, want 4", len(entry.Args))
	}
}

func TestRun_RecordsAnnotations(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "commands.log")

	err := cmdlog.Run(logPath, "gt", "copper", []string{"ticket", "status", "56", "in_progress"}, func() error {
		cmdlog.Annotate("ticket=56", "open", "in_progress")
		return nil
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}

	line := readFirstLine(t, logPath)
	var entry cmdlog.Entry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(entry.Entities) != 1 {
		t.Fatalf("entities len = %d, want 1", len(entry.Entities))
	}
	ann := entry.Entities[0]
	if ann.Entity != "ticket=56" {
		t.Errorf("entity = %q, want %q", ann.Entity, "ticket=56")
	}
	if ann.Before != "open" {
		t.Errorf("before = %q, want %q", ann.Before, "open")
	}
	if ann.After != "in_progress" {
		t.Errorf("after = %q, want %q", ann.After, "in_progress")
	}
}

func TestRun_RecordsErrorAndExitCode(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "commands.log")

	wantErr := "something went wrong"
	runErr := cmdlog.Run(logPath, "gt", "copper", []string{"ticket", "status", "99", "bad"}, func() error {
		return &testError{wantErr}
	})
	if runErr == nil || runErr.Error() != wantErr {
		t.Fatalf("Run returned %v, want error %q", runErr, wantErr)
	}

	line := readFirstLine(t, logPath)
	var entry cmdlog.Entry
	if err := json.Unmarshal([]byte(line), &entry); err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if entry.ExitCode != 1 {
		t.Errorf("exit_code = %d, want 1", entry.ExitCode)
	}
	if entry.Error != wantErr {
		t.Errorf("error = %q, want %q", entry.Error, wantErr)
	}
}

func TestRun_EmptyLogPath_Skips(t *testing.T) {
	// Should not panic or error when logPath is empty.
	err := cmdlog.Run("", "gt", "actor", []string{"status"}, func() error {
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAnnotate_ResetsBeforeEachRun(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "commands.log")

	// First invocation annotates ticket=1.
	_ = cmdlog.Run(logPath, "gt", "actor", []string{"ticket", "status", "1", "open"}, func() error {
		cmdlog.Annotate("ticket=1", "", "open")
		return nil
	})

	// Second invocation annotates ticket=2 only.
	_ = cmdlog.Run(logPath, "gt", "actor", []string{"ticket", "status", "2", "open"}, func() error {
		cmdlog.Annotate("ticket=2", "", "open")
		return nil
	})

	lines := readAllLines(t, logPath)
	if len(lines) != 2 {
		t.Fatalf("expected 2 log lines, got %d", len(lines))
	}

	var second cmdlog.Entry
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("parse second line: %v", err)
	}
	if len(second.Entities) != 1 || second.Entities[0].Entity != "ticket=2" {
		t.Errorf("second entry entities = %v, want [{ticket=2}]", second.Entities)
	}
}

func TestActor_fallback(t *testing.T) {
	// CT_AGENT_NAME wins over USER; USER wins over unknown.
	t.Setenv("CT_AGENT_NAME", "architect")
	t.Setenv("USER", "alice")
	if got := cmdlog.Actor(); got != "architect" {
		t.Errorf("with CT_AGENT_NAME set: got %q, want %q", got, "architect")
	}

	t.Setenv("CT_AGENT_NAME", "")
	if got := cmdlog.Actor(); got != "alice" {
		t.Errorf("with USER set, no CT_AGENT_NAME: got %q, want %q", got, "alice")
	}

	t.Setenv("USER", "")
	if got := cmdlog.Actor(); got != "unknown" {
		t.Errorf("with neither set: got %q, want %q", got, "unknown")
	}
}

func TestRun_writeErrorDoesNotAbort(t *testing.T) {
	// Run against a path that cannot be written (a directory where a file is
	// expected) — the command fn should still execute and its return value should
	// be returned unchanged. No panic, no error from the log write itself.
	dir := t.TempDir()
	// Create a directory at the log path — opening it for append will fail.
	badPath := filepath.Join(dir, "commands.log")
	if err := os.Mkdir(badPath, 0755); err != nil {
		t.Fatalf("setup: %v", err)
	}

	called := false
	err := cmdlog.Run(badPath, "gt", "actor", []string{"status"}, func() error {
		called = true
		return nil
	})

	if !called {
		t.Error("fn was not called despite write error")
	}
	if err != nil {
		t.Errorf("Run returned non-nil error despite fn succeeding: %v", err)
	}
}

// helpers

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func readFirstLine(t *testing.T, path string) string {
	t.Helper()
	lines := readAllLines(t, path)
	if len(lines) == 0 {
		t.Fatal("log file is empty")
	}
	return lines[0]
}

func readAllLines(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open log: %v", err)
	}
	defer f.Close()

	var lines []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

type testError struct{ msg string }

func (e *testError) Error() string { return e.msg }
