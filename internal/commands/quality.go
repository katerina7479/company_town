package commands

import (
	"database/sql"
	"fmt"
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/katerina7479/company_town/internal/config"
	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

const (
	qualityRefreshInterval = 30 * time.Second
	sparklineHistory       = 10
	detailSparklineHistory = 30
	sparklineChars         = "▁▂▃▄▅▆▇█"
)

// quality-specific lipgloss styles
var (
	qPassStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true) // green
	qWarnStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true) // yellow
	qFailStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true) // bright red
	qErrorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))            // gray
	qDimStyle    = lipgloss.NewStyle().Faint(true)
	qHeaderStyle = lipgloss.NewStyle().Bold(true).Underline(true)
	qPanelStyle  = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("6")).
			Padding(0, 1)
	qUpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	qDownStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("9")) // red
	qFlatStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8")) // gray
)

// qualityRow holds the config + latest result + trend history for one check.
type qualityRow struct {
	cfg     config.QualityCheckConfig
	latest  *repo.QualityMetric
	history []*repo.QualityMetric // newest-first, up to sparklineHistory
}

type qualityDataMsg struct {
	rows []qualityRow
	err  error
}

type qualityDetailMsg struct {
	history []*repo.QualityMetric
	err     error
}

type qualityTickMsg struct{}

// qualityModel is the bubbletea model for ct quality.
type qualityModel struct {
	conn    *sql.DB
	metrics *repo.QualityMetricRepo
	cfg     *config.Config

	rows        []qualityRow
	cursor      int
	width       int
	height      int
	lastRefresh time.Time
	err         error

	// detail mode
	detailMode    bool
	detailHistory []*repo.QualityMetric
}

func newQualityModel() (*qualityModel, error) {
	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return nil, fmt.Errorf("opening db: %w", err)
	}
	return &qualityModel{
		conn:    conn,
		metrics: repo.NewQualityMetricRepo(conn),
		cfg:     cfg,
	}, nil
}

func (m qualityModel) Init() tea.Cmd {
	return tea.Batch(m.fetch, qualityTick())
}

func qualityTick() tea.Cmd {
	return tea.Tick(qualityRefreshInterval, func(time.Time) tea.Msg {
		return qualityTickMsg{}
	})
}

func (m qualityModel) fetch() tea.Msg {
	latest, err := m.metrics.LatestPerCheck()
	if err != nil {
		return qualityDataMsg{err: err}
	}

	latestByName := make(map[string]*repo.QualityMetric, len(latest))
	for _, r := range latest {
		latestByName[r.CheckName] = r
	}

	// Config checks first, then any DB-only names.
	seen := make(map[string]bool)
	var checks []config.QualityCheckConfig
	for _, c := range m.cfg.Quality.Checks {
		checks = append(checks, c)
		seen[c.Name] = true
	}
	for _, r := range latest {
		if !seen[r.CheckName] {
			checks = append(checks, config.QualityCheckConfig{Name: r.CheckName})
			seen[r.CheckName] = true
		}
	}

	rows := make([]qualityRow, 0, len(checks))
	for _, c := range checks {
		hist, _ := m.metrics.ListByCheck(c.Name, sparklineHistory)
		rows = append(rows, qualityRow{
			cfg:     c,
			latest:  latestByName[c.Name],
			history: hist,
		})
	}
	return qualityDataMsg{rows: rows}
}

func (m qualityModel) fetchDetail() tea.Msg {
	if m.cursor >= len(m.rows) {
		return qualityDetailMsg{}
	}
	name := m.rows[m.cursor].cfg.Name
	hist, err := m.metrics.ListByCheck(name, detailSparklineHistory)
	return qualityDetailMsg{history: hist, err: err}
}

func (m qualityModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case qualityTickMsg:
		return m, tea.Batch(m.fetch, qualityTick())

	case qualityDataMsg:
		if msg.err != nil {
			m.err = msg.err
		} else {
			m.rows = msg.rows
			m.err = nil
			m.lastRefresh = time.Now()
		}
		return m, nil

	case qualityDetailMsg:
		if msg.err == nil {
			m.detailHistory = msg.history
		}
		return m, nil

	case tea.KeyMsg:
		if m.detailMode {
			switch msg.String() {
			case "esc", "q", "ctrl+c":
				m.detailMode = false
				m.detailHistory = nil
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			return m, m.fetch
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.rows)-1 {
				m.cursor++
			}
		case "enter":
			if len(m.rows) > 0 {
				m.detailMode = true
				m.detailHistory = nil
				return m, m.fetchDetail
			}
		}
	}
	return m, nil
}

func (m qualityModel) View() string {
	if m.width == 0 {
		return "Loading…"
	}
	if m.detailMode && len(m.rows) > 0 {
		return m.viewDetail()
	}
	return m.viewList()
}

// ── list view ────────────────────────────────────────────────────────────────

func (m qualityModel) viewList() string {
	var sb strings.Builder

	header := fmt.Sprintf("%-28s  %-6s  %-12s  %-10s  %-10s  %-3s  %s",
		"CHECK", "STATUS", "VALUE", "TARGET", "WARN", "TRD", "SPARKLINE",
	)
	sb.WriteString(qHeaderStyle.Render(header))
	sb.WriteString("\n")
	sb.WriteString(qDimStyle.Render(strings.Repeat("─", min(m.width-6, 100))))
	sb.WriteString("\n")

	if m.err != nil {
		sb.WriteString(qErrorStyle.Render(fmt.Sprintf("error: %v", m.err)))
		sb.WriteString("\n")
	} else if len(m.rows) == 0 {
		sb.WriteString(qDimStyle.Render(
			"No quality metrics recorded yet — run `gt check run` or wait for the next daemon baseline cycle.",
		))
		sb.WriteString("\n")
	} else {
		for i, row := range m.rows {
			line := renderQualityRow(row)
			if i == m.cursor {
				line = lipgloss.NewStyle().Background(lipgloss.Color("237")).Render(line)
			}
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n")
	var refreshStr string
	if m.lastRefresh.IsZero() {
		refreshStr = "never"
	} else {
		refreshStr = m.lastRefresh.Format("15:04:05")
	}
	footer := qDimStyle.Render(
		fmt.Sprintf("Last refresh: %s  [r] refresh  [↑↓/jk] navigate  [enter] detail  [q] quit", refreshStr),
	)
	return qPanelStyle.Width(m.width - 2).Render(sb.String() + footer)
}

// ── detail view ───────────────────────────────────────────────────────────────

func (m qualityModel) viewDetail() string {
	row := m.rows[m.cursor]
	var sb strings.Builder

	sb.WriteString(qHeaderStyle.Render(row.cfg.Name))
	sb.WriteString("\n\n")

	if row.latest != nil {
		if row.latest.Value.Valid {
			sb.WriteString(fmt.Sprintf("  Current:  %s\n", formatMetricValue(row.latest.Value.Float64)))
		}
		sb.WriteString(fmt.Sprintf("  Status:   %s\n", colorQualityStatus(row.latest.Status)))
		sb.WriteString(fmt.Sprintf("  Recorded: %s\n", row.latest.RunAt.Format("2006-01-02 15:04:05")))
	} else {
		sb.WriteString(qDimStyle.Render("  No data recorded yet."))
		sb.WriteString("\n")
	}

	if row.cfg.Type == "metric" {
		sb.WriteString("\n")
		if row.cfg.Threshold > 0 {
			sb.WriteString(fmt.Sprintf("  Target:   %s\n", formatMetricValue(row.cfg.Threshold)))
		}
		if row.cfg.WarnThreshold > 0 {
			sb.WriteString(fmt.Sprintf("  Warn:     %s\n", formatMetricValue(row.cfg.WarnThreshold)))
		}
	}

	// detailHistory is a snapshot captured at enter-press time and does not
	// refresh on the 30s tick. This is intentional — the user is inspecting a
	// static slice while the background ticker keeps the list view current.
	hist := m.detailHistory
	if hist == nil {
		hist = row.history
	}
	if len(hist) > 0 {
		sb.WriteString("\n")
		reversed := reverseHistory(hist)
		var spark string
		if row.cfg.Type == "metric" {
			spark = metricSparkline(reversed)
		} else {
			spark = passfailSparkline(reversed)
		}
		sb.WriteString(fmt.Sprintf("  Trend (%d pts): %s\n", len(hist), spark))
		sb.WriteString("\n")
		sb.WriteString(qHeaderStyle.Render("  Recent values:"))
		sb.WriteString("\n")
		limit := 10
		if len(hist) < limit {
			limit = len(hist)
		}
		for _, r := range hist[:limit] {
			valStr := "—"
			if r.Value.Valid {
				valStr = formatMetricValue(r.Value.Float64)
			}
			sb.WriteString(fmt.Sprintf("    %s  %-10s  %s\n",
				r.RunAt.Format("2006-01-02 15:04"),
				valStr,
				colorQualityStatus(r.Status),
			))
		}
	}

	sb.WriteString("\n")
	footer := qDimStyle.Render("[esc] back")
	return qPanelStyle.Width(m.width - 2).Render(sb.String() + footer)
}

// ── rendering helpers ─────────────────────────────────────────────────────────

func renderQualityRow(row qualityRow) string {
	name := row.cfg.Name
	if len([]rune(name)) > 27 {
		name = string([]rune(name)[:24]) + "..."
	}

	// Status: pad to fixed visible width so columns align despite ANSI codes.
	statusRendered := qDimStyle.Render("—     ")
	if row.latest != nil {
		statusRendered = padVisible(colorQualityStatus(row.latest.Status), 6)
	}

	valueStr := qDimStyle.Render("—")
	if row.latest != nil {
		if row.latest.Value.Valid {
			valueStr = formatMetricValue(row.latest.Value.Float64)
		} else {
			valueStr = row.latest.Status
		}
	}

	threshStr := "—"
	warnStr := "—"
	if row.cfg.Type == "metric" {
		if row.cfg.Threshold > 0 {
			threshStr = formatMetricValue(row.cfg.Threshold)
		}
		if row.cfg.WarnThreshold > 0 {
			warnStr = formatMetricValue(row.cfg.WarnThreshold)
		}
	}

	arrow := trendArrow(row.history, row.cfg.Direction)
	spark := sparkline(row)

	return fmt.Sprintf("%-28s  %s  %-12s  %-10s  %-10s  %-3s  %s",
		name, statusRendered, valueStr, threshStr, warnStr, arrow, spark)
}

// colorQualityStatus returns a color-coded status label.
func colorQualityStatus(status string) string {
	switch status {
	case "pass":
		return qPassStyle.Render("PASS")
	case "warn":
		return qWarnStyle.Render("WARN")
	case "fail":
		return qFailStyle.Render("FAIL")
	default:
		return qErrorStyle.Render("ERR")
	}
}

// formatMetricValue formats a float for display: whole numbers as integers with
// comma separators; fractional values as one-decimal floats. No unit suffix —
// the check name provides context (avoiding "12,847%" or "9.0%" for count metrics).
func formatMetricValue(v float64) string {
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return commaSeparated(int64(v))
	}
	return fmt.Sprintf("%.1f", v)
}

// commaSeparated formats an integer with comma thousands separators.
func commaSeparated(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := fmt.Sprintf("%d", n)
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	if neg {
		return "-" + string(result)
	}
	return string(result)
}

// padVisible pads s to a fixed visible width, ignoring ANSI escape codes.
func padVisible(s string, width int) string {
	visible := lipgloss.Width(s)
	if visible >= width {
		return s
	}
	return s + strings.Repeat(" ", width-visible)
}

// trendArrow returns a colored ↑/↓/→ comparing the latest vs oldest value in
// newest-first history. direction is "lower" for lower-is-better metrics
// (e.g. todo_count, lint_warning_count); empty or "higher" for the default.
// The arrow glyph reflects the direction of change; the color reflects whether
// that change is good (green) or bad (red) given the metric's direction.
func trendArrow(hist []*repo.QualityMetric, direction string) string {
	var vals []float64
	for _, r := range hist {
		if r.Value.Valid {
			vals = append(vals, r.Value.Float64)
		}
	}
	if len(vals) < 2 {
		return qFlatStyle.Render("→")
	}
	cur := vals[0]
	old := vals[len(vals)-1]
	if old == 0 {
		return qFlatStyle.Render("→")
	}
	delta := (cur - old) / math.Abs(old)
	const threshold = 0.05
	var arrow string
	var good bool
	switch {
	case delta > threshold:
		arrow = "↑"
		good = direction != "lower" // rising is good unless lower-is-better
	case delta < -threshold:
		arrow = "↓"
		good = direction == "lower" // falling is good only if lower-is-better
	default:
		return qFlatStyle.Render("→")
	}
	if good {
		return qUpStyle.Render(arrow) // green
	}
	return qDownStyle.Render(arrow) // red
}

// sparkline dispatches to metric or pass/fail sparkline.
func sparkline(row qualityRow) string {
	if len(row.history) == 0 {
		return qDimStyle.Render("—")
	}
	reversed := reverseHistory(row.history)
	if row.cfg.Type == "metric" {
		return metricSparkline(reversed)
	}
	return passfailSparkline(reversed)
}

// reverseHistory reverses a newest-first slice to oldest-first for left-to-right display.
func reverseHistory(hist []*repo.QualityMetric) []*repo.QualityMetric {
	reversed := make([]*repo.QualityMetric, len(hist))
	for i, r := range hist {
		reversed[len(hist)-1-i] = r
	}
	return reversed
}

// metricSparkline renders a bar-chart sparkline for metric checks (oldest-first input).
func metricSparkline(hist []*repo.QualityMetric) string {
	minVal, maxVal := math.MaxFloat64, -math.MaxFloat64
	for _, r := range hist {
		if r.Value.Valid {
			if r.Value.Float64 < minVal {
				minVal = r.Value.Float64
			}
			if r.Value.Float64 > maxVal {
				maxVal = r.Value.Float64
			}
		}
	}

	chars := []rune(sparklineChars)
	var sb strings.Builder
	for _, r := range hist {
		if !r.Value.Valid {
			sb.WriteRune('?')
			continue
		}
		v := r.Value.Float64
		var idx int
		if maxVal == minVal {
			idx = len(chars) - 1
		} else {
			ratio := (v - minVal) / (maxVal - minVal)
			idx = int(math.Round(ratio * float64(len(chars)-1)))
			if idx < 0 {
				idx = 0
			}
			if idx >= len(chars) {
				idx = len(chars) - 1
			}
		}
		sb.WriteRune(chars[idx])
	}
	return sb.String()
}

// passfailSparkline renders a symbol sparkline for pass/fail checks (oldest-first input).
func passfailSparkline(hist []*repo.QualityMetric) string {
	var sb strings.Builder
	for _, r := range hist {
		switch r.Status {
		case "pass":
			sb.WriteString(qPassStyle.Render("▲"))
		case "warn":
			sb.WriteString(qWarnStyle.Render("~"))
		case "fail":
			sb.WriteString(qFailStyle.Render("▼"))
		default:
			sb.WriteString(qDimStyle.Render("?"))
		}
	}
	return sb.String()
}

// Quality runs the ct quality TUI dashboard.
func Quality() error {
	m, err := newQualityModel()
	if err != nil {
		return err
	}
	defer m.conn.Close()

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
