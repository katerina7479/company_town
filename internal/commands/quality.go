package commands

import (
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
	sparklineChars         = "▁▂▃▄▅▆▇█"
)

// quality-specific lipgloss styles
var (
	qPassStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Bold(true)  // green
	qWarnStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true)  // yellow
	qFailStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true)  // bright red
	qErrorStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))             // gray
	qDimStyle   = lipgloss.NewStyle().Faint(true)
	qBoldStyle  = lipgloss.NewStyle().Bold(true)
	qHeaderStyle = lipgloss.NewStyle().Bold(true).Underline(true)
	qPanelStyle  = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("6")).
			Padding(0, 1)
)

// qualityRow holds the config + latest result + trend history for one check.
type qualityRow struct {
	cfg     config.QualityCheckConfig
	latest  *repo.QualityMetric   // nil if no results yet
	history []*repo.QualityMetric // newest-first, up to sparklineHistory
}

type qualityDataMsg struct {
	rows []qualityRow
	err  error
}

type qualityTickMsg struct{}

// qualityModel is the bubbletea model for ct quality.
type qualityModel struct {
	conn    interface{ Close() error }
	metrics *repo.QualityMetricRepo
	cfg     *config.Config

	rows        []qualityRow
	cursor      int
	width       int
	height      int
	lastRefresh time.Time
	err         error
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

	// Index latest results by check name.
	latestByName := make(map[string]*repo.QualityMetric, len(latest))
	for _, r := range latest {
		latestByName[r.CheckName] = r
	}

	// Collect all check names: config checks first, then any DB-only names.
	seen := make(map[string]bool)
	var checks []config.QualityCheckConfig
	for _, c := range m.cfg.Quality.Checks {
		checks = append(checks, c)
		seen[c.Name] = true
	}
	for _, r := range latest {
		if !seen[r.CheckName] {
			checks = append(checks, config.QualityCheckConfig{
				Name: r.CheckName,
			})
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

	case tea.KeyMsg:
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
		}
	}
	return m, nil
}

func (m qualityModel) View() string {
	if m.width == 0 {
		return "Loading…"
	}

	innerWidth := m.width - 4 // account for border + padding

	var sb strings.Builder

	// Header row
	sb.WriteString(qHeaderStyle.Render(
		fmt.Sprintf("%-28s %-8s %-10s %-12s %-12s %s",
			"CHECK", "STATUS", "VALUE", "THRESHOLD", "WARN", "TREND"),
	))
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("─", min(innerWidth, 90)))
	sb.WriteString("\n")

	if m.err != nil {
		sb.WriteString(qErrorStyle.Render(fmt.Sprintf("error: %v", m.err)))
		sb.WriteString("\n")
	} else if len(m.rows) == 0 {
		sb.WriteString(qDimStyle.Render("No quality checks configured or recorded yet."))
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

	// Footer
	sb.WriteString("\n")
	var refreshStr string
	if m.lastRefresh.IsZero() {
		refreshStr = "never"
	} else {
		refreshStr = m.lastRefresh.Format("15:04:05")
	}
	footer := qDimStyle.Render(
		fmt.Sprintf("Last refresh: %s  [r] refresh  [↑↓/jk] navigate  [q] quit", refreshStr),
	)

	content := sb.String()
	panelContent := qPanelStyle.Width(m.width - 2).Render(content + footer)
	return panelContent
}

func renderQualityRow(row qualityRow) string {
	name := row.cfg.Name
	if len(name) > 27 {
		name = name[:24] + "..."
	}

	// Status + value
	statusStr := qDimStyle.Render("no data")
	valueStr := qDimStyle.Render("—")
	if row.latest != nil {
		statusStr = colorQualityStatus(row.latest.Status)
		if row.latest.Value.Valid {
			valueStr = fmt.Sprintf("%.1f%%", row.latest.Value.Float64)
		} else {
			valueStr = row.latest.Status
		}
	}

	// Threshold / warn threshold
	threshStr := "—"
	warnStr := "—"
	if row.cfg.Type == "metric" {
		if row.cfg.Threshold > 0 {
			threshStr = fmt.Sprintf("%.1f%%", row.cfg.Threshold)
		}
		if row.cfg.WarnThreshold > 0 {
			warnStr = fmt.Sprintf("%.1f%%", row.cfg.WarnThreshold)
		}
	}

	// Sparkline
	spark := sparkline(row)

	return fmt.Sprintf("%-28s %-18s %-10s %-12s %-12s %s",
		name, statusStr, valueStr, threshStr, warnStr, spark)
}

func colorQualityStatus(status string) string {
	switch status {
	case "pass":
		return qPassStyle.Render("PASS")
	case "warn":
		return qWarnStyle.Render("WARN")
	case "fail":
		return qFailStyle.Render("FAIL")
	default:
		return qErrorStyle.Render("ERR ")
	}
}

// sparkline builds a small trend bar from the history (newest-right).
// For metric checks: bars show relative value height.
// For pass/fail checks: ▲ pass, ~ warn, ▼ fail, ? error.
func sparkline(row qualityRow) string {
	hist := row.history
	if len(hist) == 0 {
		return qDimStyle.Render("—")
	}

	// Reverse so oldest is first (left-to-right = time progression).
	reversed := make([]*repo.QualityMetric, len(hist))
	for i, r := range hist {
		reversed[len(hist)-1-i] = r
	}

	if row.cfg.Type == "metric" {
		return metricSparkline(reversed)
	}
	return passfailSparkline(reversed)
}

func metricSparkline(hist []*repo.QualityMetric) string {
	// Find min/max of values for normalization.
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
			idx = int(ratio * float64(len(chars)-1))
			if idx >= len(chars) {
				idx = len(chars) - 1
			}
		}
		sb.WriteRune(chars[idx])
	}
	return sb.String()
}

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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
