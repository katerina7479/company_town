package commands

import (
	"database/sql"
	"strings"
	"testing"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/repo"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func metricPoint(v float64, status string) *repo.QualityMetric {
	return &repo.QualityMetric{
		Status: status,
		Value:  sql.NullFloat64{Float64: v, Valid: true},
	}
}

func passfailPoint(status string) *repo.QualityMetric {
	return &repo.QualityMetric{Status: status}
}

// ── metricSparkline tests ─────────────────────────────────────────────────────

func TestMetricSparkline_empty(t *testing.T) {
	out := metricSparkline(nil)
	// empty input should produce empty string
	if out != "" {
		t.Errorf("expected empty string for nil input, got %q", out)
	}
}

func TestMetricSparkline_single(t *testing.T) {
	hist := []*repo.QualityMetric{metricPoint(70.0, "pass")}
	out := metricSparkline(hist)
	runes := []rune(out)
	if len(runes) != 1 {
		t.Fatalf("expected 1 rune, got %d: %q", len(runes), out)
	}
	// Single point with equal min/max maps to last char.
	chars := []rune(sparklineChars)
	if runes[0] != chars[len(chars)-1] {
		t.Errorf("single point: expected %q, got %q", string(chars[len(chars)-1]), string(runes[0]))
	}
}

func TestMetricSparkline_allEqual(t *testing.T) {
	hist := []*repo.QualityMetric{
		metricPoint(70.0, "pass"),
		metricPoint(70.0, "pass"),
		metricPoint(70.0, "pass"),
	}
	out := metricSparkline(hist)
	chars := []rune(sparklineChars)
	expected := string(chars[len(chars)-1])
	for _, r := range []rune(out) {
		if string(r) != expected {
			t.Errorf("all-equal: expected all chars to be %q, got %q in %q", expected, string(r), out)
		}
	}
}

func TestMetricSparkline_ascending(t *testing.T) {
	// oldest-first: values go up → rightmost char should be highest block
	hist := []*repo.QualityMetric{
		metricPoint(60.0, "fail"),
		metricPoint(65.0, "warn"),
		metricPoint(70.0, "pass"),
		metricPoint(75.0, "pass"),
		metricPoint(80.0, "pass"),
	}
	out := metricSparkline(hist)
	runes := []rune(out)
	chars := []rune(sparklineChars)
	last := runes[len(runes)-1]
	if last != chars[len(chars)-1] {
		t.Errorf("ascending: expected last char to be %q (█), got %q", string(chars[len(chars)-1]), string(last))
	}
}

func TestMetricSparkline_descending(t *testing.T) {
	// oldest-first: values go down → first char should be highest block
	hist := []*repo.QualityMetric{
		metricPoint(80.0, "pass"),
		metricPoint(75.0, "pass"),
		metricPoint(70.0, "pass"),
		metricPoint(65.0, "warn"),
		metricPoint(60.0, "fail"),
	}
	out := metricSparkline(hist)
	runes := []rune(out)
	chars := []rune(sparklineChars)
	first := runes[0]
	if first != chars[len(chars)-1] {
		t.Errorf("descending: expected first char to be %q (█), got %q", string(chars[len(chars)-1]), string(first))
	}
}

func TestMetricSparkline_lengthMatchesInput(t *testing.T) {
	hist := []*repo.QualityMetric{
		metricPoint(60.0, "fail"),
		metricPoint(70.0, "pass"),
		metricPoint(75.0, "pass"),
	}
	out := metricSparkline(hist)
	runes := []rune(out)
	if len(runes) != len(hist) {
		t.Errorf("expected %d runes, got %d: %q", len(hist), len(runes), out)
	}
}

// ── passfailSparkline tests ───────────────────────────────────────────────────

func TestPassfailSparkline_mix(t *testing.T) {
	hist := []*repo.QualityMetric{
		passfailPoint("pass"),
		passfailPoint("warn"),
		passfailPoint("fail"),
		passfailPoint("error"),
	}
	out := passfailSparkline(hist)

	// Strip ANSI codes by checking visible content via lipgloss.Width indirectly.
	// We verify the plain-text content by stripping escape sequences manually.
	plain := stripANSI(out)
	checks := []struct {
		want string
		desc string
	}{
		{"▲", "pass → ▲"},
		{"~", "warn → ~"},
		{"▼", "fail → ▼"},
		{"?", "error → ?"},
	}
	for _, c := range checks {
		if !strings.Contains(plain, c.want) {
			t.Errorf("passfail sparkline missing %s: got %q", c.desc, plain)
		}
	}
}

// ── colorQualityStatus tests ──────────────────────────────────────────────────

func TestColorQualityStatus_allStatuses(t *testing.T) {
	for _, status := range []string{"pass", "warn", "fail", "error"} {
		out := colorQualityStatus(status)
		if out == "" {
			t.Errorf("colorQualityStatus(%q) returned empty string", status)
		}
		plain := stripANSI(out)
		if plain == "" {
			t.Errorf("colorQualityStatus(%q) rendered to empty plain text", status)
		}
	}
}

// ── formatMetricValue tests ───────────────────────────────────────────────────

func TestFormatMetricValue_integer(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{0, "0"},
		{9, "9"},
		{287, "287"},
		{1000, "1,000"},
		{12847, "12,847"},
		{1000000, "1,000,000"},
	}
	for _, c := range cases {
		got := formatMetricValue(c.in)
		if got != c.want {
			t.Errorf("formatMetricValue(%.0f) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatMetricValue_float(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{72.3, "72.3"},
		{0.5, "0.5"},
		{99.9, "99.9"},
	}
	for _, c := range cases {
		got := formatMetricValue(c.in)
		if got != c.want {
			t.Errorf("formatMetricValue(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ── sparkline dispatch tests ──────────────────────────────────────────────────

func TestSparkline_emptyHistoryReturnsDash(t *testing.T) {
	row := qualityRow{
		cfg:     config.QualityCheckConfig{Type: "metric"},
		history: nil,
	}
	out := sparkline(row)
	plain := stripANSI(out)
	if !strings.Contains(plain, "—") {
		t.Errorf("empty history sparkline should contain '—', got %q", plain)
	}
}

func TestSparkline_dispatchesMetric(t *testing.T) {
	row := qualityRow{
		cfg: config.QualityCheckConfig{Type: "metric"},
		history: []*repo.QualityMetric{
			metricPoint(70.0, "pass"),
			metricPoint(75.0, "pass"),
		},
	}
	out := sparkline(row)
	// Should contain sparkline block chars, not ▲/▼
	plain := stripANSI(out)
	for _, c := range plain {
		if c == '▲' || c == '▼' {
			t.Errorf("metric sparkline should not contain pass/fail glyphs, got %q", plain)
		}
	}
}

func TestSparkline_dispatchesPassfail(t *testing.T) {
	row := qualityRow{
		cfg: config.QualityCheckConfig{Type: "pass_fail"},
		history: []*repo.QualityMetric{
			passfailPoint("pass"),
			passfailPoint("fail"),
		},
	}
	out := sparkline(row)
	plain := stripANSI(out)
	if !strings.Contains(plain, "▲") && !strings.Contains(plain, "▼") {
		t.Errorf("pass_fail sparkline should contain ▲ or ▼, got %q", plain)
	}
}

// ── stripANSI helper ──────────────────────────────────────────────────────────

// stripANSI removes ANSI escape sequences from s so tests can check plain text.
func stripANSI(s string) string {
	var out strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}
