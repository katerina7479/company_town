package commands

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
)

// refreshInterval controls how often the dashboard polls the database.
const refreshInterval = 5 * time.Second

// lipgloss styles
var (
	boldStyle = lipgloss.NewStyle().Bold(true)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().Bold(true).Underline(true)

	statusStyles = map[string]lipgloss.Style{
		// Agent statuses
		"working": lipgloss.NewStyle().Foreground(lipgloss.Color("2")),  // green
		"idle":    lipgloss.NewStyle().Foreground(lipgloss.Color("3")),  // yellow
		"dead":    lipgloss.NewStyle().Foreground(lipgloss.Color("1")),  // red
		// Ticket statuses
		"draft":        lipgloss.NewStyle().Foreground(lipgloss.Color("8")),   // dark gray
		"open":         lipgloss.NewStyle().Foreground(lipgloss.Color("4")),   // blue
		"in_progress":  lipgloss.NewStyle().Foreground(lipgloss.Color("6")),   // cyan
		"in_review":    lipgloss.NewStyle().Foreground(lipgloss.Color("5")),   // magenta
		"under_review": lipgloss.NewStyle().Foreground(lipgloss.Color("11")),  // bright yellow
		"pr_open":      lipgloss.NewStyle().Foreground(lipgloss.Color("10")),  // bright green
		"reviewed":     lipgloss.NewStyle().Foreground(lipgloss.Color("14")),  // bright cyan
		"repairing":    lipgloss.NewStyle().Foreground(lipgloss.Color("9")),   // bright red
		"on_hold":      lipgloss.NewStyle().Foreground(lipgloss.Color("208")), // orange
		"closed":       lipgloss.NewStyle().Foreground(lipgloss.Color("242")), // medium gray
	}

	footerStyle = lipgloss.NewStyle().Faint(true)
)

func colorStatus(status string) string {
	if s, ok := statusStyles[status]; ok {
		return s.Render(status)
	}
	return status
}

// formatDuration formats a duration as a compact human-readable string.
// e.g. "3m", "2h 15m", "4d 3h"
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	default:
		if minutes == 0 {
			return "<1m"
		}
		return fmt.Sprintf("%dm", minutes)
	}
}

// dashboardData holds a snapshot fetched from the database.
type dashboardData struct {
	agents []*repo.Agent
	roots  []*repo.IssueNode
	err    error
}

// tickMsg triggers a periodic refresh.
type tickMsg time.Time

// dataMsg delivers a freshly fetched snapshot.
type dataMsg dashboardData

// dashboardModel is the bubbletea model for the dashboard.
type dashboardModel struct {
	conn   interface{ Close() error }
	agents *repo.AgentRepo
	issues *repo.IssueRepo

	data dashboardData

	width  int
	height int
}

func newDashboardModel() (*dashboardModel, error) {
	conn, _, err := db.OpenFromWorkingDir()
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	return &dashboardModel{
		conn:   conn,
		agents: repo.NewAgentRepo(conn),
		issues: repo.NewIssueRepo(conn),
	}, nil
}

func (m *dashboardModel) fetch() tea.Msg {
	agents, err := m.agents.ListAll()
	if err != nil {
		return dataMsg{err: err}
	}
	roots, err := m.issues.ListHierarchy()
	if err != nil {
		return dataMsg{err: err}
	}
	return dataMsg{agents: agents, roots: roots}
}

func tickCmd() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m dashboardModel) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg { return m.fetch() },
		tickCmd(),
	)
}

func (m dashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.conn.Close()
			return m, tea.Quit
		case "r":
			return m, func() tea.Msg { return m.fetch() }
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tickMsg:
		return m, tea.Batch(
			func() tea.Msg { return m.fetch() },
			tickCmd(),
		)

	case dataMsg:
		m.data = dashboardData(msg)
	}

	return m, nil
}

func (m dashboardModel) View() string {
	if m.width == 0 {
		return "Loading…"
	}

	if m.data.err != nil {
		return fmt.Sprintf("error: %v\n\nPress q to quit.", m.data.err)
	}

	// Reserve 2 rows for the footer.
	contentHeight := m.height - 2

	// Split width roughly 35% agents / 65% tickets, minus 4 for panel borders/padding.
	agentWidth := m.width*35/100 - 4
	ticketWidth := m.width - agentWidth - 8 // borders + gap

	agentsPanel := m.renderAgents(agentWidth, contentHeight)
	ticketsPanel := m.renderTickets(ticketWidth, contentHeight)

	body := lipgloss.JoinHorizontal(lipgloss.Top, agentsPanel, "  ", ticketsPanel)

	footer := footerStyle.Render(fmt.Sprintf(
		" q quit  r refresh  (auto-refresh every %s)",
		refreshInterval,
	))

	return lipgloss.JoinVertical(lipgloss.Left, body, footer)
}

func (m dashboardModel) renderAgents(width, height int) string {
	var sb strings.Builder
	sb.WriteString(headerStyle.Render("Agents") + "\n\n")

	if len(m.data.agents) == 0 {
		sb.WriteString(footerStyle.Render("(none registered)"))
	} else {
		for _, a := range m.data.agents {
			name := boldStyle.Render(fmt.Sprintf("%-14s", a.Name))
			status := colorStatus(a.Status)
			issue := ""
			if a.CurrentIssue.Valid {
				issue = fmt.Sprintf(" → nc-%d", a.CurrentIssue.Int64)
			}
			age := ""
			if a.StatusChangedAt.Valid {
				age = footerStyle.Render(" (" + formatDuration(time.Since(a.StatusChangedAt.Time)) + ")")
			}
			sb.WriteString(fmt.Sprintf("%s %s%s%s\n", name, status, issue, age))
		}
	}

	inner := sb.String()
	return panelStyle.
		Width(width).
		Height(height - 2). // 2 = top+bottom border
		Render(inner)
}

func (m dashboardModel) renderTickets(width, height int) string {
	var sb strings.Builder
	sb.WriteString(headerStyle.Render("Tickets") + "\n\n")

	if len(m.data.roots) == 0 {
		sb.WriteString(footerStyle.Render("(no tickets)"))
	} else {
		for _, root := range m.data.roots {
			renderIssueNode(&sb, root, 0, width)
		}
	}

	inner := sb.String()
	return panelStyle.
		Width(width).
		Height(height - 2).
		Render(inner)
}

func renderIssueNode(sb *strings.Builder, node *repo.IssueNode, depth int, width int) {
	indent := strings.Repeat("  ", depth)
	bullet := "●"
	if depth > 0 {
		bullet = "◦"
	}

	prefix := fmt.Sprintf("%s%s", indent, bullet)
	idStr := fmt.Sprintf("%-6d", node.ID)
	statusStr := fmt.Sprintf("[%-11s]", node.Status)
	coloredStatus := colorStatus(node.Status)
	age := footerStyle.Render("(" + formatDuration(time.Since(node.UpdatedAt)) + ")")
	ageRaw := "(" + formatDuration(time.Since(node.UpdatedAt)) + ")"

	prStr := "      " // 6 chars blank when no PR
	if node.PRNumber.Valid {
		prStr = fmt.Sprintf("%-6s", fmt.Sprintf("#%d", node.PRNumber.Int64))
	}

	// Truncate title so the row fits inside the panel.
	// prefix + space + id + space + status + space + pr + space + age + space + title
	fixedLen := len(prefix) + 1 + len(idStr) + 1 + len(statusStr) + 1 + len(prStr) + 1 + len(ageRaw) + 1
	titleMax := width - fixedLen - 2
	title := node.Title
	if len(title) > titleMax && titleMax > 3 {
		title = title[:titleMax-1] + "…"
	}

	sb.WriteString(fmt.Sprintf("%s %s %s %s %s %s\n",
		prefix, idStr, coloredStatus, prStr, age, title,
	))

	for _, child := range node.Children {
		renderIssueNode(sb, child, depth+1, width)
	}
}

// Dashboard implements `ct dashboard` — opens the live TUI.
func Dashboard() error {
	m, err := newDashboardModel()
	if err != nil {
		return err
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
