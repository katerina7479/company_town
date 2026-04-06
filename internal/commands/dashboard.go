package commands

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
	"github.com/katerina7479/company_town/internal/session"
)

// refreshInterval controls how often the dashboard polls the database.
const refreshInterval = 5 * time.Second

// lipgloss styles
var (
	boldStyle = lipgloss.NewStyle().Bold(true)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1)

	panelFocusedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("6")). // cyan when focused
				Padding(0, 1)

	headerStyle = lipgloss.NewStyle().Bold(true).Underline(true)

	selectedStyle = lipgloss.NewStyle().Background(lipgloss.Color("237"))

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

// actionResultMsg carries the result of an agent action (kill, stop).
type actionResultMsg struct {
	text string
	err  error
}

// flatNode is an issue node with its render depth, used for cursor navigation.
type flatNode struct {
	node  *repo.IssueNode
	depth int
}

// dashboardModel is the bubbletea model for the dashboard.
type dashboardModel struct {
	conn   interface{ Close() error }
	agents *repo.AgentRepo
	issues *repo.IssueRepo

	data dashboardData

	width  int
	height int

	focusedPanel int // 0 = agents, 1 = tickets
	agentCursor  int
	ticketCursor int

	statusMsg string // transient feedback from agent actions
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
		case "tab":
			if m.focusedPanel == 0 {
				m.focusedPanel = 1
			} else {
				m.focusedPanel = 0
			}
		case "j", "down":
			if m.focusedPanel == 0 {
				if m.agentCursor < len(m.data.agents)-1 {
					m.agentCursor++
				}
			} else {
				flat := m.flatTickets()
				if m.ticketCursor < len(flat)-1 {
					m.ticketCursor++
				}
			}
		case "k", "up":
			if m.focusedPanel == 0 {
				if m.agentCursor > 0 {
					m.agentCursor--
				}
			} else {
				if m.ticketCursor > 0 {
					m.ticketCursor--
				}
			}

		case "a":
			if m.focusedPanel == 0 && len(m.data.agents) > 0 {
				agent := m.data.agents[m.agentCursor]
				sname := session.SessionName(agent.Name)
				if session.Exists(sname) {
					c := exec.Command("tmux", "attach-session", "-t", sname)
					return m, tea.ExecProcess(c, func(err error) tea.Msg {
						if err != nil {
							return actionResultMsg{err: fmt.Errorf("attach %s: %w", agent.Name, err)}
						}
						return actionResultMsg{text: "Detached from " + agent.Name}
					})
				}
				m.statusMsg = "No active session for " + agent.Name
			}

		case "x":
			if m.focusedPanel == 0 && len(m.data.agents) > 0 {
				agent := m.data.agents[m.agentCursor]
				return m, m.killAgentCmd(agent)
			}

		case "s":
			if m.focusedPanel == 0 && len(m.data.agents) > 0 {
				agent := m.data.agents[m.agentCursor]
				return m, m.stopAgentCmd(agent)
			}
		}

	case actionResultMsg:
		if msg.err != nil {
			m.statusMsg = "Error: " + msg.err.Error()
		} else {
			m.statusMsg = msg.text
		}
		return m, func() tea.Msg { return m.fetch() }

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
		// Clamp cursors in case list shrank after refresh.
		if len(m.data.agents) == 0 {
			m.agentCursor = 0
		} else if m.agentCursor >= len(m.data.agents) {
			m.agentCursor = len(m.data.agents) - 1
		}
		flat := m.flatTickets()
		if len(flat) == 0 {
			m.ticketCursor = 0
		} else if m.ticketCursor >= len(flat) {
			m.ticketCursor = len(flat) - 1
		}
	}

	return m, nil
}

// killAgentCmd kills the agent's tmux session and marks it dead in the DB.
func (m dashboardModel) killAgentCmd(a *repo.Agent) tea.Cmd {
	return func() tea.Msg {
		sname := session.SessionName(a.Name)
		if err := session.Kill(sname); err != nil {
			return actionResultMsg{err: fmt.Errorf("kill session %s: %w", a.Name, err)}
		}
		if err := m.agents.UpdateStatus(a.Name, "dead"); err != nil {
			return actionResultMsg{err: fmt.Errorf("update status %s: %w", a.Name, err)}
		}
		return actionResultMsg{text: fmt.Sprintf("Killed %s", a.Name)}
	}
}

// stopAgentCmd sends a graceful shutdown signal to the agent's tmux session.
func (m dashboardModel) stopAgentCmd(a *repo.Agent) tea.Cmd {
	return func() tea.Msg {
		sname := session.SessionName(a.Name)
		if !session.Exists(sname) {
			return actionResultMsg{err: fmt.Errorf("no active session for %s", a.Name)}
		}
		msg := "Complete your current work, follow the completion protocol, and go idle."
		if err := session.SendKeys(sname, msg); err != nil {
			return actionResultMsg{err: fmt.Errorf("send stop signal to %s: %w", a.Name, err)}
		}
		return actionResultMsg{text: fmt.Sprintf("Sent stop signal to %s", a.Name)}
	}
}

// flatTickets returns the flat ordered list of visible ticket nodes (same order as rendered).
func (m dashboardModel) flatTickets() []flatNode {
	cutoff := time.Now().Add(-4 * time.Hour)
	filtered := filterStaleClosedNodes(m.data.roots, cutoff)
	return flattenTree(filtered, 0)
}

// flattenTree recursively flattens a tree of issue nodes into a depth-annotated slice.
func flattenTree(nodes []*repo.IssueNode, depth int) []flatNode {
	var result []flatNode
	for _, n := range nodes {
		result = append(result, flatNode{n, depth})
		result = append(result, flattenTree(n.Children, depth+1)...)
	}
	return result
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

	var footerParts []string
	footerParts = append(footerParts, fmt.Sprintf(
		" q quit  r refresh  tab switch panel  j/k navigate  a attach  x kill  s stop  (auto-refresh every %s)",
		refreshInterval,
	))
	if m.statusMsg != "" {
		footerParts = append(footerParts, "  "+m.statusMsg)
	}
	footer := footerStyle.Render(strings.Join(footerParts, ""))

	return lipgloss.JoinVertical(lipgloss.Left, body, footer)
}

func (m dashboardModel) renderAgents(width, height int) string {
	var sb strings.Builder
	sb.WriteString(headerStyle.Render("Agents") + "\n\n")

	focused := m.focusedPanel == 0
	rowWidth := width

	if len(m.data.agents) == 0 {
		sb.WriteString(footerStyle.Render("(none registered)"))
	} else {
		for i, a := range m.data.agents {
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
			line := fmt.Sprintf("%s %s%s%s", name, status, issue, age)
			if focused && i == m.agentCursor {
				line = selectedStyle.Width(rowWidth).Render(line)
			}
			sb.WriteString(line + "\n")
		}
	}

	inner := sb.String()
	style := panelStyle
	if focused {
		style = panelFocusedStyle
	}
	return style.
		Width(width).
		Height(height - 2). // 2 = top+bottom border
		Render(inner)
}

func (m dashboardModel) renderTickets(width, height int) string {
	var sb strings.Builder
	sb.WriteString(headerStyle.Render("Tickets") + "\n\n")

	focused := m.focusedPanel == 1
	rowWidth := width

	flat := m.flatTickets()
	if len(flat) == 0 {
		sb.WriteString(footerStyle.Render("(no tickets)"))
	} else {
		for i, fn := range flat {
			line := renderIssueRow(fn.node, fn.depth, width)
			if focused && i == m.ticketCursor {
				line = selectedStyle.Width(rowWidth).Render(line)
			}
			sb.WriteString(line + "\n")
		}
	}

	inner := sb.String()
	style := panelStyle
	if focused {
		style = panelFocusedStyle
	}
	return style.
		Width(width).
		Height(height - 2).
		Render(inner)
}

// filterStaleClosedNodes returns a copy of the tree with closed tickets
// older than cutoff removed. Non-closed nodes are always kept; parent nodes
// are kept if they have any surviving children.
func filterStaleClosedNodes(roots []*repo.IssueNode, cutoff time.Time) []*repo.IssueNode {
	var result []*repo.IssueNode
	for _, root := range roots {
		if filtered := filterNode(root, cutoff); filtered != nil {
			result = append(result, filtered)
		}
	}
	return result
}

func filterNode(node *repo.IssueNode, cutoff time.Time) *repo.IssueNode {
	var children []*repo.IssueNode
	for _, child := range node.Children {
		if filtered := filterNode(child, cutoff); filtered != nil {
			children = append(children, filtered)
		}
	}

	isStale := node.Status == "closed" && node.ClosedAt.Valid && node.ClosedAt.Time.Before(cutoff)
	if isStale && len(children) == 0 {
		return nil
	}

	clone := *node
	clone.Children = children
	return &clone
}

// renderIssueRow renders a single ticket row as a string (without trailing newline).
func renderIssueRow(node *repo.IssueNode, depth int, width int) string {
	indent := strings.Repeat("  ", depth)
	bullet := "●"
	if depth > 0 {
		bullet = "◦"
	}

	prefix := fmt.Sprintf("%s%s", indent, bullet)
	idStr := fmt.Sprintf("%-6d", node.ID)
	statusStr := fmt.Sprintf("[%-11s]", node.Status)
	coloredStatus := colorStatus(node.Status)
	ageRaw := "(" + formatDuration(time.Since(node.UpdatedAt)) + ")"
	age := footerStyle.Render(ageRaw)

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

	return fmt.Sprintf("%s %s %s %s %s %s",
		prefix, idStr, coloredStatus, prStr, age, title,
	)
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
