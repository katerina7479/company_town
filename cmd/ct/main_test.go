package main

import (
	"testing"
)

func TestParseStopArgs_noArgs(t *testing.T) {
	target, clean, err := parseStopArgs([]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target != "" || clean != false {
		t.Errorf("expected target=%q clean=false, got target=%q clean=%v", "", target, clean)
	}
}

func TestParseStopArgs_targetOnly(t *testing.T) {
	target, clean, err := parseStopArgs([]string{"daemon"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target != "daemon" || clean != false {
		t.Errorf("expected target=daemon clean=false, got target=%q clean=%v", target, clean)
	}
}

func TestParseStopArgs_targetAndClean(t *testing.T) {
	target, clean, err := parseStopArgs([]string{"daemon", "--clean"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target != "daemon" || !clean {
		t.Errorf("expected target=daemon clean=true, got target=%q clean=%v", target, clean)
	}
}

func TestParseStopArgs_cleanBeforeTarget(t *testing.T) {
	target, clean, err := parseStopArgs([]string{"--clean", "daemon"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target != "daemon" || !clean {
		t.Errorf("expected target=daemon clean=true, got target=%q clean=%v", target, clean)
	}
}

func TestParseStopArgs_unknownFlag(t *testing.T) {
	_, _, err := parseStopArgs([]string{"--bogus"})
	if err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestParseStopArgs_twoPositionals(t *testing.T) {
	_, _, err := parseStopArgs([]string{"a", "b"})
	if err == nil {
		t.Fatal("expected error for two positional args")
	}
}
