package gtcmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/katerina7479/company_town/internal/cmdlog"
)

// captureOutput runs f() and returns everything written to os.Stdout and os.Stderr.
func captureOutput(f func()) (stdout, stderr string) {
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	os.Stdout, os.Stderr = wOut, wErr
	f()
	wOut.Close()
	wErr.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	var bufOut, bufErr bytes.Buffer
	io.Copy(&bufOut, rOut)
	io.Copy(&bufErr, rErr)
	return bufOut.String(), bufErr.String()
}

func writeTestLog(t *testing.T, entries []cmdlog.Entry) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "commands.log")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, e := range entries {
		if err := enc.Encode(e); err != nil {
			t.Fatalf("encode entry: %v", err)
		}
	}
	return path
}

func makeEntry(actor, binary string, args []string, entities []cmdlog.Annotation, ts time.Time) cmdlog.Entry {
	return cmdlog.Entry{
		Timestamp:  ts,
		Actor:      actor,
		Binary:     binary,
		Args:       args,
		ExitCode:   0,
		DurationMs: 5,
		Entities:   entities,
	}
}

func TestCanonicalEntity_ticketRef(t *testing.T) {
	cases := []struct{ in, want string }{
		{"nc-56", "ticket=56"},
		{"CT-100", "ticket=100"},
		{"ticket=56", "ticket=56"},
		{"agent=copper", "agent=copper"},
		{"copper", "agent=copper"},
	}
	for _, tc := range cases {
		got := canonicalEntity(tc.in)
		if got != tc.want {
			t.Errorf("canonicalEntity(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLogTail_emptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commands.log")
	if err := logTail(path, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLogTail_returnsLastN(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	var entries []cmdlog.Entry
	for i := 0; i < 10; i++ {
		entries = append(entries, makeEntry("tin", "gt",
			[]string{"ticket", "show", string(rune('0'+i))},
			nil, base.Add(time.Duration(i)*time.Minute)))
	}
	path := writeTestLog(t, entries)

	outStr, _ := captureOutput(func() {
		if err := logTail(path, []string{"-n", "3"}); err != nil {
			t.Errorf("logTail: %v", err)
		}
	})

	if !strings.Contains(outStr, "9") {
		t.Errorf("expected last entry in output, got: %s", outStr)
	}
}

func TestLogTail_jsonPassthrough(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	path := writeTestLog(t, []cmdlog.Entry{
		makeEntry("copper", "gt", []string{"ticket", "status", "56", "repairing"}, nil, base),
	})

	outStr, _ := captureOutput(func() {
		if err := logTail(path, []string{"--json"}); err != nil {
			t.Errorf("logTail --json: %v", err)
		}
	})

	if !strings.Contains(outStr, `"actor":"copper"`) {
		t.Errorf("expected raw JSON in --json output, got: %s", outStr)
	}
}

func TestLogShow_filterByEntity(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	path := writeTestLog(t, []cmdlog.Entry{
		makeEntry("copper", "gt", []string{"ticket", "status", "56", "repairing"},
			[]cmdlog.Annotation{{Entity: "ticket=56", Before: "in_review", After: "repairing"}},
			base),
		makeEntry("iron", "gt", []string{"ticket", "status", "57", "closed"},
			[]cmdlog.Annotation{{Entity: "ticket=57", Before: "in_review", After: "closed"}},
			base.Add(time.Minute)),
	})

	outStr, _ := captureOutput(func() {
		if err := logShow(path, []string{"--entity", "nc-56"}); err != nil {
			t.Errorf("logShow: %v", err)
		}
	})

	if !strings.Contains(outStr, "copper") {
		t.Errorf("expected matching entry (copper/ticket=56), got: %s", outStr)
	}
	if strings.Contains(outStr, "iron") {
		t.Errorf("expected non-matching entry (iron/ticket=57) excluded, got: %s", outStr)
	}
}

func TestLogShow_filterByActor(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	path := writeTestLog(t, []cmdlog.Entry{
		makeEntry("copper", "gt", []string{"ticket", "show", "1"}, nil, base),
		makeEntry("iron", "gt", []string{"ticket", "show", "2"}, nil, base.Add(time.Minute)),
	})

	outStr, _ := captureOutput(func() {
		if err := logShow(path, []string{"--actor", "copper"}); err != nil {
			t.Errorf("logShow --actor: %v", err)
		}
	})

	if !strings.Contains(outStr, "copper") {
		t.Errorf("expected copper entry, got: %s", outStr)
	}
	if strings.Contains(outStr, "iron") {
		t.Errorf("expected iron excluded, got: %s", outStr)
	}
}

func TestLogShow_filterBySince(t *testing.T) {
	now := time.Now().UTC()
	path := writeTestLog(t, []cmdlog.Entry{
		makeEntry("copper", "gt", []string{"ticket", "show", "old"}, nil, now.Add(-2*time.Hour)),
		makeEntry("iron", "gt", []string{"ticket", "show", "recent"}, nil, now.Add(-10*time.Minute)),
	})

	outStr, _ := captureOutput(func() {
		if err := logShow(path, []string{"--since", "1h"}); err != nil {
			t.Errorf("logShow --since: %v", err)
		}
	})

	if !strings.Contains(outStr, "recent") {
		t.Errorf("expected recent entry in output, got: %s", outStr)
	}
	if strings.Contains(outStr, "old") {
		t.Errorf("expected old entry excluded, got: %s", outStr)
	}
}

func TestLogShow_noFilters(t *testing.T) {
	path := writeTestLog(t, nil)
	err := logShow(path, []string{})
	if err == nil {
		t.Fatal("expected error when no filters provided, got nil")
	}
}

func TestLogShow_missingFlagValue(t *testing.T) {
	path := writeTestLog(t, nil)
	for _, flag := range []string{"--entity", "--actor", "--since"} {
		err := logShow(path, []string{flag})
		if err == nil {
			t.Errorf("expected error for %s with no value, got nil", flag)
		}
	}
}

func TestLogShow_nonexistentFile(t *testing.T) {
	err := logShow("/no/such/path/commands.log", []string{"--actor", "x"})
	if err == nil {
		t.Fatal("expected error for nonexistent log file, got nil")
	}
}
