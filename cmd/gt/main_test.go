package main

import (
	"strings"
	"testing"
)

// --- run() tests ---

func TestRun_noArgs(t *testing.T) {
	err := run(nil)
	if err == nil {
		t.Error("expected error when no args given, got nil")
	}
}

func TestRun_emptyArgs(t *testing.T) {
	err := run([]string{})
	if err == nil {
		t.Error("expected error for empty args slice, got nil")
	}
}

func TestRun_versionFlag(t *testing.T) {
	err := run([]string{"--version"})
	if err != nil {
		t.Errorf("unexpected error for --version: %v", err)
	}
}

func TestRun_versionCommand(t *testing.T) {
	err := run([]string{"version"})
	if err != nil {
		t.Errorf("unexpected error for version command: %v", err)
	}
}

func TestRun_unknownCommand(t *testing.T) {
	err := run([]string{"unknowncmd"})
	if err == nil {
		t.Error("expected error for unknown command, got nil")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Errorf("expected error message to contain 'unknown command', got: %v", err)
	}
}

func TestRun_unknownCommandWithDash(t *testing.T) {
	err := run([]string{"--notaflag"})
	if err == nil {
		t.Error("expected error for unknown flag-like command, got nil")
	}
}

func TestRun_validCommandDispatchesToHandler(t *testing.T) {
	// "status" is a valid command. It will fail with a DB connection error in
	// test environments (no Dolt server), but it must not fail with
	// "unknown command". This verifies the dispatch reaches the handler.
	err := run([]string{"status"})
	if err != nil && strings.Contains(err.Error(), "unknown command") {
		t.Errorf("'status' should not produce 'unknown command' error, got: %v", err)
	}
}

func TestRun_migrateDispatchesToHandler(t *testing.T) {
	// "migrate" is a valid command with no subcommand args. Same as status —
	// will fail on DB but must not fail as "unknown command".
	err := run([]string{"migrate"})
	if err != nil && strings.Contains(err.Error(), "unknown command") {
		t.Errorf("'migrate' should not produce 'unknown command' error, got: %v", err)
	}
}

// --- isAgentExemptVerb tests ---

func TestIsAgentExemptVerb_exemptVerbs(t *testing.T) {
	exempt := []string{"accept", "release", "do", "status"}
	for _, verb := range exempt {
		if !isAgentExemptVerb(verb) {
			t.Errorf("expected %q to be exempt from drift warning", verb)
		}
	}
}

func TestIsAgentExemptVerb_nonExemptVerbs(t *testing.T) {
	nonExempt := []string{"create", "register", "list", "show", "unknown", ""}
	for _, verb := range nonExempt {
		if isAgentExemptVerb(verb) {
			t.Errorf("expected %q to NOT be exempt from drift warning", verb)
		}
	}
}

// --- printUsage test ---

func TestPrintUsage_doesNotPanic(t *testing.T) {
	// printUsage writes to stdout; just verify it executes without panicking.
	printUsage()
}
