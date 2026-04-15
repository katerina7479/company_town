package main

import (
	"strings"
	"testing"
)

func TestParseTargetArgs(t *testing.T) {
	cases := []struct {
		name       string
		cmd        string
		args       []string
		allowClean bool
		wantTarget string
		wantClean  bool
		wantErr    string // substring match; empty means no error
	}{
		// ct stop cases
		{"stop empty", "ct stop", nil, true, "", false, ""},
		{"stop target only", "ct stop", []string{"daemon"}, true, "daemon", false, ""},
		{"stop clean only", "ct stop", []string{"--clean"}, true, "", true, ""},
		{"stop target then clean", "ct stop", []string{"daemon", "--clean"}, true, "daemon", true, ""},
		{"stop clean then target", "ct stop", []string{"--clean", "daemon"}, true, "daemon", true, ""},
		{"stop two targets", "ct stop", []string{"daemon", "mayor"}, true, "", false, "at most one target"},
		{"stop unknown flag", "ct stop", []string{"--weird"}, true, "", false, "unknown flag: --weird"},

		// ct nuke cases
		{"nuke empty", "ct nuke", nil, false, "", false, ""},
		{"nuke target only", "ct nuke", []string{"prole-tin"}, false, "prole-tin", false, ""},
		{"nuke rejects --clean", "ct nuke", []string{"--clean"}, false, "", false, "unknown flag: --clean"},
		{"nuke rejects --clean with target", "ct nuke", []string{"prole-tin", "--clean"}, false, "", false, "unknown flag: --clean"},
		{"nuke two targets", "ct nuke", []string{"a", "b"}, false, "", false, "at most one target"},
		{"nuke unknown flag", "ct nuke", []string{"--force"}, false, "", false, "unknown flag: --force"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotTarget, gotClean, err := parseTargetArgs(tc.cmd, tc.args, tc.allowClean)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if gotTarget != tc.wantTarget {
					t.Errorf("target: want %q, got %q", tc.wantTarget, gotTarget)
				}
				if gotClean != tc.wantClean {
					t.Errorf("clean: want %v, got %v", tc.wantClean, gotClean)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error: want substring %q, got %v", tc.wantErr, err)
			}
		})
	}
}
