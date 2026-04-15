package gtcmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/katerina7479/company_town/internal/cmdlog"
)

// captureLogOutput runs f() and returns everything written to os.Stdout and os.Stderr.
func captureLogOutput(f func()) (stdout, stderr string) {
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
	os.Create(path) // create empty file
	if err := logTail(path, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLogTail_missingFile_friendlyMessage(t *testing.T) {
	outStr, _ := captureLogOutput(func() {
		if err := logTail("/no/such/path/commands.log", nil); err != nil {
			t.Errorf("expected nil error for missing file, got: %v", err)
		}
	})
	if !strings.Contains(outStr, "no command log yet") {
		t.Errorf("expected friendly message, got: %q", outStr)
	}
}

func TestLogTail_defaultN(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	var entries []cmdlog.Entry
	for i := 0; i < 30; i++ {
		entries = append(entries, makeEntry("tin", "gt",
			[]string{"ticket", "show", strconv.Itoa(i)},
			nil, base.Add(time.Duration(i)*time.Minute)))
	}
	path := writeTestLog(t, entries)

	outStr, _ := captureLogOutput(func() {
		if err := logTail(path, nil); err != nil {
			t.Errorf("logTail: %v", err)
		}
	})

	// Default -n 20: should see entry 29 (last) but not entry 9 (11th from end).
	if !strings.Contains(outStr, "show 29") {
		t.Errorf("expected last entry in output, got: %s", outStr)
	}
	if strings.Contains(outStr, "show 9\n") {
		t.Errorf("expected entry 9 excluded (beyond default 20), got output containing it")
	}
}

func TestLogTail_fewerThanN(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	var entries []cmdlog.Entry
	for i := 0; i < 3; i++ {
		entries = append(entries, makeEntry("tin", "gt",
			[]string{"ticket", "show", strconv.Itoa(i)},
			nil, base.Add(time.Duration(i)*time.Minute)))
	}
	path := writeTestLog(t, entries)

	var lineCount int
	outStr, _ := captureLogOutput(func() {
		if err := logTail(path, []string{"-n", "10"}); err != nil {
			t.Errorf("logTail: %v", err)
		}
	})
	for _, l := range strings.Split(strings.TrimSpace(outStr), "\n") {
		if strings.Contains(l, "tin") {
			lineCount++
		}
	}
	if lineCount != 3 {
		t.Errorf("expected 3 entries (all), got %d", lineCount)
	}
}

func TestLogTail_returnsLastN(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	var entries []cmdlog.Entry
	for i := 0; i < 10; i++ {
		entries = append(entries, makeEntry("tin", "gt",
			[]string{"ticket", "show", string(rune('0' + i))},
			nil, base.Add(time.Duration(i)*time.Minute)))
	}
	path := writeTestLog(t, entries)

	outStr, _ := captureLogOutput(func() {
		if err := logTail(path, []string{"-n", "3"}); err != nil {
			t.Errorf("logTail: %v", err)
		}
	})

	if !strings.Contains(outStr, "9") {
		t.Errorf("expected last entry in output, got: %s", outStr)
	}
}

func TestLogTail_nRequiresValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commands.log")
	os.Create(path)

	err := logTail(path, []string{"-n"})
	if err == nil {
		t.Fatal("expected error for -n with no value")
	}
}

func TestLogTail_invalidNValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commands.log")
	os.Create(path)

	if err := logTail(path, []string{"-n", "not-a-number"}); err == nil {
		t.Fatal("expected error for invalid -n value")
	}
	if err := logTail(path, []string{"-n", "0"}); err == nil {
		t.Fatal("expected error for -n=0")
	}
}

func TestLogTail_unknownFlag(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commands.log")
	os.Create(path)

	if err := logTail(path, []string{"--bogus"}); err == nil {
		t.Fatal("expected error for unknown flag")
	}
}

func TestLogTail_nCapAt10000(t *testing.T) {
	path := writeTestLog(t, nil) // empty file is fine; we just test arg parsing
	// n > 10000 should not error — it gets capped silently
	if err := logTail(path, []string{"-n", "99999"}); err != nil {
		t.Fatalf("logTail with n=99999 should not error: %v", err)
	}
}

func TestLogTail_malformedLineWarning(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commands.log")
	os.WriteFile(path, []byte("NOT_JSON\n"), 0644)

	_, errStr := captureLogOutput(func() {
		if err := logTail(path, nil); err != nil {
			t.Errorf("logTail: %v", err)
		}
	})
	if !strings.Contains(errStr, "warning: skipped") {
		t.Errorf("expected malformed warning on stderr, got: %q", errStr)
	}
}

func TestLogTail_jsonPassthrough(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	path := writeTestLog(t, []cmdlog.Entry{
		makeEntry("copper", "gt", []string{"ticket", "status", "56", "repairing"}, nil, base),
	})

	outStr, _ := captureLogOutput(func() {
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

	outStr, _ := captureLogOutput(func() {
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

	outStr, _ := captureLogOutput(func() {
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

	outStr, _ := captureLogOutput(func() {
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
	// Spec §Implementation notes: missing file → friendly message + exit 0, not an error.
	outStr, _ := captureLogOutput(func() {
		err := logShow("/no/such/path/commands.log", []string{"--actor", "x"})
		if err != nil {
			t.Errorf("expected nil error for missing file, got: %v", err)
		}
	})
	if !strings.Contains(outStr, "no command log yet") {
		t.Errorf("expected friendly message, got: %q", outStr)
	}
}

func TestLogShow_filterCommand(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	path := writeTestLog(t, []cmdlog.Entry{
		makeEntry("copper", "gt", []string{"ticket", "status", "56", "repairing"}, nil, base),
		makeEntry("iron", "gt", []string{"ticket", "show", "56"}, nil, base.Add(time.Minute)),
	})

	outStr, _ := captureLogOutput(func() {
		if err := logShow(path, []string{"--command", "status"}); err != nil {
			t.Errorf("logShow --command: %v", err)
		}
	})

	if !strings.Contains(outStr, "copper") {
		t.Errorf("expected copper entry (status cmd), got: %s", outStr)
	}
	if strings.Contains(outStr, "show") {
		t.Errorf("expected iron/show entry excluded, got: %s", outStr)
	}
}

func TestLogShow_missingCommandFlagValue(t *testing.T) {
	path := writeTestLog(t, nil)
	err := logShow(path, []string{"--command"})
	if err == nil {
		t.Fatal("expected error for --command with no value, got nil")
	}
}

func TestLogShow_filterEntityShortcut(t *testing.T) {
	// Bare number "56" should match any entity with id=56 (ticket=56, agent=56, etc.).
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	path := writeTestLog(t, []cmdlog.Entry{
		makeEntry("copper", "gt", []string{"ticket", "status", "56", "repairing"},
			[]cmdlog.Annotation{{Entity: "ticket=56", Before: "in_review", After: "repairing"}},
			base),
		makeEntry("iron", "gt", []string{"ticket", "show", "57"},
			[]cmdlog.Annotation{{Entity: "ticket=57"}},
			base.Add(time.Minute)),
	})

	outStr, _ := captureLogOutput(func() {
		if err := logShow(path, []string{"--entity", "56"}); err != nil {
			t.Errorf("logShow bare-number entity: %v", err)
		}
	})

	if !strings.Contains(outStr, "copper") {
		t.Errorf("expected copper/ticket=56 entry, got: %s", outStr)
	}
	if strings.Contains(outStr, "iron") {
		t.Errorf("expected iron/ticket=57 excluded, got: %s", outStr)
	}
}

func TestCanonicalEntity_bareNumber(t *testing.T) {
	// Bare number must produce "=N" so strings.Contains matches any entity type.
	got := canonicalEntity("56")
	if got != "=56" {
		t.Errorf("canonicalEntity(\"56\") = %q, want \"=56\"", got)
	}
}

func TestLogShow_combinedFilters(t *testing.T) {
	// AND semantics: --actor copper --command status must only return entries matching both.
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	path := writeTestLog(t, []cmdlog.Entry{
		makeEntry("copper", "gt", []string{"ticket", "status", "1", "open"}, nil, base),
		makeEntry("copper", "gt", []string{"ticket", "show", "1"}, nil, base.Add(time.Minute)),
		makeEntry("iron", "gt", []string{"ticket", "status", "2", "open"}, nil, base.Add(2*time.Minute)),
	})

	outStr, _ := captureLogOutput(func() {
		if err := logShow(path, []string{"--actor", "copper", "--command", "status"}); err != nil {
			t.Errorf("logShow combined: %v", err)
		}
	})

	lines := strings.Split(strings.TrimSpace(outStr), "\n")
	var matchLines int
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			matchLines++
		}
	}
	// Only one entry: copper + status (not copper+show, not iron+status).
	if matchLines != 1 {
		t.Errorf("expected 1 matching line, got %d:\n%s", matchLines, outStr)
	}
	if !strings.Contains(outStr, "copper") {
		t.Errorf("expected copper entry in output, got: %s", outStr)
	}
}

func TestLogShow_jsonPassthrough(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	path := writeTestLog(t, []cmdlog.Entry{
		makeEntry("copper", "gt", []string{"ticket", "status", "56", "repairing"},
			[]cmdlog.Annotation{{Entity: "ticket=56"}}, base),
	})

	outStr, _ := captureLogOutput(func() {
		if err := logShow(path, []string{"--actor", "copper", "--json"}); err != nil {
			t.Errorf("logShow --json: %v", err)
		}
	})

	if !strings.Contains(outStr, `"actor":"copper"`) {
		t.Errorf("expected raw JSON in output, got: %s", outStr)
	}
}

func TestLogShow_malformedLineSkipped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "commands.log")

	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	good := makeEntry("copper", "gt", []string{"ticket", "show", "1"},
		[]cmdlog.Annotation{{Entity: "ticket=1"}}, base)
	data, _ := json.Marshal(good)

	content := string(data) + "\n" + "NOT_JSON_AT_ALL\n"
	os.WriteFile(path, []byte(content), 0644)

	_, errStr := captureLogOutput(func() {
		if err := logShow(path, []string{"--actor", "copper"}); err != nil {
			t.Errorf("logShow: %v", err)
		}
	})

	if !strings.Contains(errStr, "warning: skipped 1 malformed") {
		t.Errorf("expected malformed-line warning on stderr, got: %q", errStr)
	}
}

func TestRenderLine_malformedJSON(t *testing.T) {
	captureLogOutput(func() {
		got := renderLine("NOT_VALID_JSON")
		if got {
			t.Error("expected renderLine to return false for malformed JSON")
		}
	})
}

func TestRenderEntry_withErrorField(t *testing.T) {
	entry := cmdlog.Entry{
		Timestamp:  time.Now(),
		Actor:      "copper",
		Binary:     "gt",
		Args:       []string{"ticket", "show", "1"},
		ExitCode:   1,
		DurationMs: 5,
		Error:      "something went wrong",
	}
	outStr, _ := captureLogOutput(func() {
		renderEntry(entry)
	})
	if !strings.Contains(outStr, "error: something went wrong") {
		t.Errorf("expected error field in output, got: %q", outStr)
	}
}

func TestRenderEntry_annotationWithBeforeAfter(t *testing.T) {
	entry := cmdlog.Entry{
		Timestamp:  time.Now(),
		Actor:      "iron",
		Binary:     "gt",
		Args:       []string{"ticket", "status", "5", "closed"},
		DurationMs: 3,
		Entities: []cmdlog.Annotation{
			{Entity: "ticket=5", Before: "open", After: "closed"},
		},
	}
	outStr, _ := captureLogOutput(func() {
		renderEntry(entry)
	})
	if !strings.Contains(outStr, "open->closed") {
		t.Errorf("expected before/after in output, got: %q", outStr)
	}
}

func TestRenderEntry_annotationWithoutBeforeAfter(t *testing.T) {
	entry := cmdlog.Entry{
		Timestamp:  time.Now(),
		Actor:      "tin",
		Binary:     "gt",
		Args:       []string{"ticket", "show", "3"},
		DurationMs: 2,
		Entities: []cmdlog.Annotation{
			{Entity: "ticket=3"},
		},
	}
	outStr, _ := captureLogOutput(func() {
		renderEntry(entry)
	})
	if !strings.Contains(outStr, "ticket=3") {
		t.Errorf("expected entity in output, got: %q", outStr)
	}
}

func TestLogShow_nc56Replay(t *testing.T) {
	// Acceptance criteria for nc-73: the hypothetical nc-56 replay is achievable
	// with a single `gt log show --entity ticket=56` command. Seed a log with the
	// invocation pattern that SHOULD have caused nc-56 to land in repairing, then
	// verify the offending invocation is surfaced with its actor identified.
	base := time.Date(2024, 3, 1, 10, 0, 0, 0, time.UTC)
	path := writeTestLog(t, []cmdlog.Entry{
		makeEntry("daemon", "gt",
			[]string{"ticket", "status", "56", "repairing"},
			[]cmdlog.Annotation{{Entity: "ticket=56", Before: "pr_open", After: "repairing"}},
			base),
		makeEntry("iron", "gt",
			[]string{"ticket", "show", "56"},
			[]cmdlog.Annotation{{Entity: "ticket=56"}},
			base.Add(5*time.Minute)),
		makeEntry("iron", "gt",
			[]string{"ticket", "status", "42", "open"},
			[]cmdlog.Annotation{{Entity: "ticket=42"}},
			base.Add(10*time.Minute)),
	})

	outStr, _ := captureLogOutput(func() {
		if err := logShow(path, []string{"--entity", "ticket=56"}); err != nil {
			t.Errorf("logShow nc56 replay: %v", err)
		}
	})

	// The offending status transition must appear.
	if !strings.Contains(outStr, "daemon") {
		t.Errorf("expected daemon actor in output (the one who set repairing), got: %s", outStr)
	}
	if !strings.Contains(outStr, "repairing") {
		t.Errorf("expected repairing transition in output, got: %s", outStr)
	}
	// Unrelated ticket (42) must not appear.
	if strings.Contains(outStr, "ticket=42") {
		t.Errorf("expected ticket=42 excluded (different entity), got: %s", outStr)
	}
}
