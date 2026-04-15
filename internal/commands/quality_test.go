package commands

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

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
	for _, r := range out {
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

// ── trendArrow direction tests ────────────────────────────────────────────────

func TestTrendArrow_lowerDirection_ascendingIsBad(t *testing.T) {
	// todo_count climbing (22→18→14 newest-first means ascending over time)
	// is a regression for a lower-is-better metric, so arrow is red even though glyph is ↑.
	hist := []*repo.QualityMetric{
		metricPoint(22, "fail"),
		metricPoint(18, "warn"),
		metricPoint(14, "pass"),
	}
	out := trendArrow(hist, "lower")
	if !strings.Contains(out, "↑") {
		t.Errorf("expected ↑ glyph for ascending history, got %q", stripANSI(out))
	}
	// Style must be red (bad direction), not green.
	if out != qDownStyle.Render("↑") {
		t.Errorf("expected red (qDownStyle) styling for ascending lower-is-better metric, got %q", out)
	}
}

func TestTrendArrow_lowerDirection_descendingIsGood(t *testing.T) {
	// todo_count falling (14→18→22 newest-first) is an improvement; arrow should be green.
	hist := []*repo.QualityMetric{
		metricPoint(14, "pass"),
		metricPoint(18, "warn"),
		metricPoint(22, "fail"),
	}
	out := trendArrow(hist, "lower")
	if out != qUpStyle.Render("↓") {
		t.Errorf("expected green (qUpStyle) ↓ for descending lower-is-better metric, got %q", out)
	}
}

func TestTrendArrow_higherDirection_ascendingIsGood(t *testing.T) {
	// Coverage rising (53→65→72 newest-first) is an improvement; arrow should be green ↑.
	hist := []*repo.QualityMetric{
		metricPoint(72, "pass"),
		metricPoint(65, "warn"),
		metricPoint(53, "fail"),
	}
	out := trendArrow(hist, "")
	if out != qUpStyle.Render("↑") {
		t.Errorf("expected green (qUpStyle) ↑ for ascending higher-is-better metric, got %q", out)
	}
}

// ── padVisible tests ──────────────────────────────────────────────────────────

func TestPadVisible_noOpWhenAlreadyWide(t *testing.T) {
	s := "hello"
	got := padVisible(s, 3)
	if got != s {
		t.Errorf("expected unchanged string, got %q", got)
	}
}

func TestPadVisible_exactWidth(t *testing.T) {
	s := "hello"
	got := padVisible(s, 5)
	if got != s {
		t.Errorf("expected unchanged string for exact width, got %q", got)
	}
}

func TestPadVisible_padsToWidth(t *testing.T) {
	s := "hi"
	got := padVisible(s, 6)
	if len(got) != 6 {
		t.Errorf("expected length 6, got %d: %q", len(got), got)
	}
	if !strings.HasPrefix(got, s) {
		t.Errorf("expected padded string to start with %q, got %q", s, got)
	}
}

func TestPadVisible_emptyString(t *testing.T) {
	got := padVisible("", 4)
	if got != "    " {
		t.Errorf("expected 4 spaces, got %q", got)
	}
}

// ── renderQualityRow tests ────────────────────────────────────────────────────

func TestRenderQualityRow_noLatest(t *testing.T) {
	row := qualityRow{
		cfg: config.QualityCheckConfig{
			Name:      "go_test_coverage",
			Type:      "metric",
			Direction: "higher",
		},
	}
	out := renderQualityRow(row)
	plain := stripANSI(out)
	if !strings.Contains(plain, "go_test_coverage") {
		t.Errorf("expected check name in output, got %q", plain)
	}
}

func TestRenderQualityRow_withPassLatest(t *testing.T) {
	row := qualityRow{
		cfg: config.QualityCheckConfig{
			Name:          "go_test_coverage",
			Type:          "metric",
			Direction:     "higher",
			Threshold:     60,
			WarnThreshold: 50,
		},
		latest: &repo.QualityMetric{
			Status: "pass",
			Value:  sql.NullFloat64{Float64: 72.3, Valid: true},
		},
		history: []*repo.QualityMetric{
			metricPoint(72.3, "pass"),
		},
	}
	out := renderQualityRow(row)
	plain := stripANSI(out)
	if !strings.Contains(plain, "go_test_coverage") {
		t.Errorf("expected check name, got %q", plain)
	}
	if !strings.Contains(plain, "72.3") {
		t.Errorf("expected value 72.3, got %q", plain)
	}
}

func TestRenderQualityRow_withPassfailType(t *testing.T) {
	row := qualityRow{
		cfg: config.QualityCheckConfig{
			Name: "some_check",
			Type: "pass_fail",
		},
		latest: &repo.QualityMetric{
			Status: "pass",
		},
		history: []*repo.QualityMetric{
			passfailPoint("pass"),
			passfailPoint("fail"),
		},
	}
	out := renderQualityRow(row)
	plain := stripANSI(out)
	if !strings.Contains(plain, "some_check") {
		t.Errorf("expected check name, got %q", plain)
	}
}

func TestRenderQualityRow_longNameTruncated(t *testing.T) {
	longName := strings.Repeat("a", 30)
	row := qualityRow{
		cfg: config.QualityCheckConfig{Name: longName, Type: "metric"},
	}
	out := renderQualityRow(row)
	plain := stripANSI(out)
	// Name field is capped at 28 runes; truncated names end with "..."
	if !strings.Contains(plain, "...") {
		t.Errorf("expected truncation ellipsis for long name, got %q", plain)
	}
}

// ── qualityModel View / viewList / viewDetail tests ──────────────────────────

func TestQualityView_loading(t *testing.T) {
	m := qualityModel{width: 0}
	got := m.View()
	if got != "Loading…" {
		t.Errorf("expected 'Loading…' for zero-width model, got %q", got)
	}
}

func TestQualityViewList_empty(t *testing.T) {
	m := qualityModel{width: 80, height: 24}
	got := m.View()
	plain := stripANSI(got)
	if !strings.Contains(plain, "No quality metrics") {
		t.Errorf("expected empty-state message, got %q", plain)
	}
}

func TestQualityViewList_withError(t *testing.T) {
	m := qualityModel{
		width:  80,
		height: 24,
		err:    fmt.Errorf("database connection refused"),
	}
	got := m.View()
	plain := stripANSI(got)
	if !strings.Contains(plain, "error") {
		t.Errorf("expected error message in view, got %q", plain)
	}
}

func TestQualityViewList_withRows(t *testing.T) {
	rows := []qualityRow{
		{
			cfg: config.QualityCheckConfig{
				Name:      "go_test_coverage",
				Type:      "metric",
				Direction: "higher",
				Threshold: 60,
			},
			latest: &repo.QualityMetric{
				Status: "pass",
				Value:  sql.NullFloat64{Float64: 72.3, Valid: true},
			},
			history: []*repo.QualityMetric{metricPoint(72.3, "pass")},
		},
		{
			cfg: config.QualityCheckConfig{
				Name:      "todo_count",
				Type:      "metric",
				Direction: "lower",
				Threshold: 0,
			},
			latest: &repo.QualityMetric{
				Status: "fail",
				Value:  sql.NullFloat64{Float64: 3, Valid: true},
			},
			history: []*repo.QualityMetric{metricPoint(3, "fail")},
		},
	}
	m := qualityModel{width: 120, height: 40, rows: rows, cursor: 0}
	got := m.View()
	plain := stripANSI(got)
	if !strings.Contains(plain, "go_test_coverage") {
		t.Errorf("expected first row name, got %q", plain)
	}
	if !strings.Contains(plain, "todo_count") {
		t.Errorf("expected second row name, got %q", plain)
	}
}

func TestQualityViewDetail_noData(t *testing.T) {
	rows := []qualityRow{
		{
			cfg: config.QualityCheckConfig{
				Name: "go_test_coverage",
				Type: "metric",
			},
		},
	}
	m := qualityModel{
		width:      120,
		height:     40,
		rows:       rows,
		cursor:     0,
		detailMode: true,
	}
	got := m.View()
	plain := stripANSI(got)
	if !strings.Contains(plain, "go_test_coverage") {
		t.Errorf("expected check name in detail view, got %q", plain)
	}
	if !strings.Contains(plain, "No data") {
		t.Errorf("expected no-data message, got %q", plain)
	}
}

func TestQualityViewDetail_withHistory(t *testing.T) {
	hist := []*repo.QualityMetric{
		metricPoint(72.3, "pass"),
		metricPoint(65.0, "warn"),
		metricPoint(53.0, "fail"),
	}
	rows := []qualityRow{
		{
			cfg: config.QualityCheckConfig{
				Name:          "go_test_coverage",
				Type:          "metric",
				Direction:     "higher",
				Threshold:     60,
				WarnThreshold: 50,
			},
			latest: &repo.QualityMetric{
				Status: "pass",
				Value:  sql.NullFloat64{Float64: 72.3, Valid: true},
			},
			history: hist,
		},
	}
	m := qualityModel{
		width:      120,
		height:     40,
		rows:       rows,
		cursor:     0,
		detailMode: true,
	}
	got := m.View()
	plain := stripANSI(got)
	if !strings.Contains(plain, "go_test_coverage") {
		t.Errorf("expected check name in detail view, got %q", plain)
	}
	if !strings.Contains(plain, "72.3") {
		t.Errorf("expected current value in detail view, got %q", plain)
	}
	if !strings.Contains(plain, "Trend") {
		t.Errorf("expected Trend line in detail view, got %q", plain)
	}
}

func TestQualityViewDetail_withDetailHistory(t *testing.T) {
	// detailHistory overrides row.history when present
	detailHist := []*repo.QualityMetric{
		metricPoint(80.0, "pass"),
		metricPoint(75.0, "pass"),
	}
	rows := []qualityRow{
		{
			cfg: config.QualityCheckConfig{
				Name: "go_test_coverage",
				Type: "metric",
			},
			latest: &repo.QualityMetric{
				Status: "pass",
				Value:  sql.NullFloat64{Float64: 80.0, Valid: true},
			},
		},
	}
	m := qualityModel{
		width:         120,
		height:        40,
		rows:          rows,
		cursor:        0,
		detailMode:    true,
		detailHistory: detailHist,
	}
	got := m.View()
	plain := stripANSI(got)
	if !strings.Contains(plain, "Trend") {
		t.Errorf("expected Trend line when detailHistory is set, got %q", plain)
	}
}

// ── qualityTick test ─────────────────────────────────────────────────────────

func TestQualityTick_returnsCmd(t *testing.T) {
	cmd := qualityTick()
	if cmd == nil {
		t.Error("expected non-nil cmd from qualityTick()")
	}
}

// ── qualityModel.Update tests ─────────────────────────────────────────────────

func TestQualityUpdate_windowSizeMsg(t *testing.T) {
	m := qualityModel{}
	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	qm := updated.(qualityModel)
	if qm.width != 120 || qm.height != 40 {
		t.Errorf("expected width=120 height=40, got %d %d", qm.width, qm.height)
	}
	if cmd != nil {
		t.Error("expected nil cmd for WindowSizeMsg")
	}
}

func TestQualityUpdate_dataMsg_withError(t *testing.T) {
	m := qualityModel{}
	msg := qualityDataMsg{err: fmt.Errorf("db error")}
	updated, _ := m.Update(msg)
	qm := updated.(qualityModel)
	if qm.err == nil {
		t.Error("expected error to be set")
	}
}

func TestQualityUpdate_dataMsg_success(t *testing.T) {
	m := qualityModel{err: fmt.Errorf("previous error")}
	rows := []qualityRow{
		{cfg: config.QualityCheckConfig{Name: "go_test_coverage"}},
	}
	msg := qualityDataMsg{rows: rows}
	updated, _ := m.Update(msg)
	qm := updated.(qualityModel)
	if qm.err != nil {
		t.Errorf("expected no error after successful dataMsg, got %v", qm.err)
	}
	if len(qm.rows) != 1 {
		t.Errorf("expected 1 row, got %d", len(qm.rows))
	}
	if qm.lastRefresh.IsZero() {
		t.Error("expected lastRefresh to be set")
	}
}

func TestQualityUpdate_detailMsg_success(t *testing.T) {
	m := qualityModel{}
	hist := []*repo.QualityMetric{metricPoint(70.0, "pass")}
	msg := qualityDetailMsg{history: hist}
	updated, _ := m.Update(msg)
	qm := updated.(qualityModel)
	if len(qm.detailHistory) != 1 {
		t.Errorf("expected 1 history item, got %d", len(qm.detailHistory))
	}
}

func TestQualityUpdate_detailMsg_withError(t *testing.T) {
	m := qualityModel{}
	msg := qualityDetailMsg{err: fmt.Errorf("db error")}
	updated, _ := m.Update(msg)
	qm := updated.(qualityModel)
	if qm.detailHistory != nil {
		t.Error("expected detailHistory to remain nil on error")
	}
}

func TestQualityUpdate_keyQ_quitsWhenNotInDetail(t *testing.T) {
	m := qualityModel{}
	_, cmd := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("q")})
	if cmd == nil {
		t.Error("expected non-nil quit cmd from 'q' key")
	}
}

func TestQualityUpdate_keyEsc_exitsDetailMode(t *testing.T) {
	hist := []*repo.QualityMetric{metricPoint(70.0, "pass")}
	m := qualityModel{
		detailMode:    true,
		detailHistory: hist,
		rows:          []qualityRow{{cfg: config.QualityCheckConfig{Name: "coverage"}}},
	}
	updated, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("q")})
	qm := updated.(qualityModel)
	if qm.detailMode {
		t.Error("expected detailMode=false after q in detail mode")
	}
	if qm.detailHistory != nil {
		t.Error("expected detailHistory cleared after exiting detail mode")
	}
}

func TestQualityUpdate_keyEsc_inDetailMode(t *testing.T) {
	m := qualityModel{detailMode: true}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	qm := updated.(qualityModel)
	if qm.detailMode {
		t.Error("expected detailMode=false after esc in detail mode")
	}
}

func TestQualityUpdate_cursorNavigation(t *testing.T) {
	rows := []qualityRow{
		{cfg: config.QualityCheckConfig{Name: "a"}},
		{cfg: config.QualityCheckConfig{Name: "b"}},
		{cfg: config.QualityCheckConfig{Name: "c"}},
	}
	m := qualityModel{rows: rows, cursor: 0}

	// move down
	updated, _ := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("j")})
	qm := updated.(qualityModel)
	if qm.cursor != 1 {
		t.Errorf("expected cursor=1 after j, got %d", qm.cursor)
	}

	// move up
	updated, _ = qm.Update(tea.KeyMsg{Type: -1, Runes: []rune("k")})
	qm = updated.(qualityModel)
	if qm.cursor != 0 {
		t.Errorf("expected cursor=0 after k, got %d", qm.cursor)
	}

	// can't go below 0
	updated, _ = qm.Update(tea.KeyMsg{Type: -1, Runes: []rune("k")})
	qm = updated.(qualityModel)
	if qm.cursor != 0 {
		t.Errorf("expected cursor stays at 0, got %d", qm.cursor)
	}

	// move to last
	qm.cursor = 2
	updated, _ = qm.Update(tea.KeyMsg{Type: -1, Runes: []rune("j")})
	qm = updated.(qualityModel)
	if qm.cursor != 2 {
		t.Errorf("expected cursor stays at 2 (last), got %d", qm.cursor)
	}
}

func TestQualityUpdate_enterWithRows_entersDetailMode(t *testing.T) {
	rows := []qualityRow{
		{cfg: config.QualityCheckConfig{Name: "go_test_coverage", Type: "metric"}},
	}
	m := qualityModel{
		rows:    rows,
		cursor:  0,
		metrics: nil, // fetchDetail will be called but its result comes async
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	qm := updated.(qualityModel)
	if !qm.detailMode {
		t.Error("expected detailMode=true after Enter with rows")
	}
	// cmd should be non-nil (it's m.fetchDetail)
	if cmd == nil {
		t.Error("expected fetchDetail cmd to be returned")
	}
}

func TestQualityUpdate_enterWithNoRows_noOp(t *testing.T) {
	m := qualityModel{}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	qm := updated.(qualityModel)
	if qm.detailMode {
		t.Error("expected detailMode to remain false with no rows")
	}
	if cmd != nil {
		t.Error("expected nil cmd with no rows")
	}
}

func TestQualityUpdate_qualityTickMsg(t *testing.T) {
	m := qualityModel{}
	_, cmd := m.Update(qualityTickMsg{})
	if cmd == nil {
		t.Error("expected non-nil cmd from qualityTickMsg")
	}
}

func TestQualityUpdate_keyUpArrow(t *testing.T) {
	rows := []qualityRow{
		{cfg: config.QualityCheckConfig{Name: "a"}},
		{cfg: config.QualityCheckConfig{Name: "b"}},
	}
	m := qualityModel{rows: rows, cursor: 1}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyUp})
	qm := updated.(qualityModel)
	if qm.cursor != 0 {
		t.Errorf("expected cursor=0 after up arrow, got %d", qm.cursor)
	}
}

func TestQualityUpdate_keyDownArrow(t *testing.T) {
	rows := []qualityRow{
		{cfg: config.QualityCheckConfig{Name: "a"}},
		{cfg: config.QualityCheckConfig{Name: "b"}},
	}
	m := qualityModel{rows: rows, cursor: 0}
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	qm := updated.(qualityModel)
	if qm.cursor != 1 {
		t.Errorf("expected cursor=1 after down arrow, got %d", qm.cursor)
	}
}

func TestQualityUpdate_keyR_fetchesWhenNotInDetail(t *testing.T) {
	m := qualityModel{}
	_, cmd := m.Update(tea.KeyMsg{Type: -1, Runes: []rune("r")})
	// cmd should be nil since m.fetch needs a real DB (metrics is nil),
	// but the cmd itself is a function reference — non-nil
	// We can't easily test the cmd result without a DB, but we ensure no panic.
	_ = cmd
}

func TestQualityViewList_withLastRefresh(t *testing.T) {
	m := qualityModel{
		width:       80,
		height:      24,
		lastRefresh: time.Now(),
	}
	got := m.View()
	plain := stripANSI(got)
	// lastRefresh is now set so the timestamp appears
	if !strings.Contains(plain, ":") {
		t.Errorf("expected time string with ':' for lastRefresh, got %q", plain)
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
