package commands

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/katerina7479/company_town/internal/db"
	"github.com/katerina7479/company_town/internal/repo"
	"github.com/katerina7479/company_town/internal/session"
)

// defaultRestartAgent re-launches an agent by type: proles use `gt prole create`,
// all other agents use `gt start`. The command is started non-blocking.
func defaultRestartAgent(name, agentType string) error {
	var cmd *exec.Cmd
	if agentType == "prole" {
		cmd = exec.Command("gt", "prole", "create", name)
	} else {
		cmd = exec.Command("gt", "start", name)
	}
	return cmd.Start()
}

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

	priorityStyles = map[string]lipgloss.Style{
		"P0": lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true),  // bright red bold
		"P1": lipgloss.NewStyle().Foreground(lipgloss.Color("208")),           // orange
		"P2": lipgloss.NewStyle().Foreground(lipgloss.Color("3")),             // yellow
		"P3": lipgloss.NewStyle().Foreground(lipgloss.Color("242")),           // medium gray
	}

	typeStyles = map[string]lipgloss.Style{
		"epic":     lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true), // magenta bold
		"bug":      lipgloss.NewStyle().Foreground(lipgloss.Color("9")),            // bright red
		"refactor": lipgloss.NewStyle().Foreground(lipgloss.Color("4")),            // blue
	}

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

// priorityCell returns a fixed 5-visible-char cell for the priority column.
// e.g. "[P1] " or "     " when NULL.
func priorityCell(p sql.NullString) string {
	const width = 5
	if !p.Valid || p.String == "" {
		return strings.Repeat(" ", width)
	}
	label := fmt.Sprintf("[%s]", p.String) // e.g. "[P0]"
	if s, ok := priorityStyles[p.String]; ok {
		return s.Render(label) + " "
	}
	return fmt.Sprintf("%-*s", width, label)
}

// typeCell returns a fixed 1-visible-char cell for the issue type column.
// epic → "E", bug → "B", refactor → "R", task → " " (blank — task is the default type).
// Unknown future types also render blank. The outer Sprintf handles column spacing.
func typeCell(issueType string) string {
	letters := map[string]string{
		"epic":     "E",
		"bug":      "B",
		"refactor": "R",
	}
	letter, ok := letters[issueType]
	if !ok {
		return " " // task and unknown types get a blank cell
	}
	if s, ok2 := typeStyles[issueType]; ok2 {
		return s.Render(letter)
	}
	return letter
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
	agents         []*repo.Agent
	roots          []*repo.IssueNode
	lastDaemonTick *time.Time
	err            error
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

	killSession   func(name string) error
	sessionExists func(name string) bool
	sendKeys      func(name, msg string) error
	restartAgent  func(name, agentType string) error
	sleepFn       func(time.Duration)

	data dashboardData

	width  int
	height int

	ticketPrefix    string        // from config, used in status-change label
	tickFile        string        // path to daemon-tick file; empty disables reading
	pollingInterval time.Duration // daemon poll interval, used to compute stale threshold

	focusedPanel int // 0 = agents, 1 = tickets
	agentCursor  int
	ticketCursor int

	statusMsg string // transient feedback from agent actions

	// expanded holds ticket IDs that have been expanded to show full details.
	expanded map[int]bool

	// showClosed controls whether closed tickets are shown regardless of age.
	showClosed bool

	// Input mode — active when the user is typing a message (e.g. for nudge,
	// ticket creation, or status change).
	inputMode   bool
	inputBuffer string
	inputAction string // "nudge", "create_ticket", "status"
	inputTarget string // agent name (nudge) or ticket ID string (status)
}

func newDashboardModel() (*dashboardModel, error) {
	conn, cfg, err := db.OpenFromWorkingDir()
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}
	ctDir := filepath.Join(cfg.ProjectRoot, ".company_town")
	pollingInterval := time.Duration(cfg.PollingIntervalSeconds) * time.Second
	if pollingInterval <= 0 {
		pollingInterval = 10 * time.Second
	}
	return &dashboardModel{
		conn:            conn,
		agents:          repo.NewAgentRepo(conn, nil),
		issues:          repo.NewIssueRepo(conn, nil),
		killSession:     session.Kill,
		sessionExists:   session.Exists,
		sendKeys:        session.SendKeys,
		restartAgent:    defaultRestartAgent,
		sleepFn:         time.Sleep,
		expanded:        make(map[int]bool),
		ticketPrefix:    cfg.TicketPrefix,
		tickFile:        filepath.Join(ctDir, "run", "daemon-tick"),
		pollingInterval: pollingInterval,
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
	var lastTick *time.Time
	if m.tickFile != "" {
		if data, err := os.ReadFile(m.tickFile); err == nil {
			if t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(string(data))); err == nil {
				lastTick = &t
			}
		}
	}
	return dataMsg{agents: agents, roots: roots, lastDaemonTick: lastTick}
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
		if m.inputMode {
			switch msg.String() {
			case "enter":
				switch m.inputAction {
				case "nudge":
					if m.inputBuffer != "" {
						sname := m.agentSessionName(m.inputTarget)
						if sname == "" {
							m.statusMsg = fmt.Sprintf("agent %s has no tmux session recorded", m.inputTarget)
						} else if err := m.sendKeys(sname, m.inputBuffer); err != nil {
							m.statusMsg = "nudge failed: " + err.Error()
						} else {
							m.statusMsg = "nudged " + m.inputTarget
						}
					}
				case "create_ticket":
					if m.inputBuffer != "" {
						if _, err := m.issues.Create(m.inputBuffer, "task", nil, nil, nil); err != nil {
							m.statusMsg = "create ticket failed: " + err.Error()
						}
					}
				case "status":
					if m.inputBuffer != "" {
						if !slices.Contains(repo.ValidStatuses, m.inputBuffer) {
							m.statusMsg = fmt.Sprintf("invalid status %q", m.inputBuffer)
						} else if id, err := strconv.Atoi(m.inputTarget); err != nil {
							m.statusMsg = "internal error: bad ticket id"
						} else if err := m.issues.UpdateStatus(id, m.inputBuffer); err != nil {
							m.statusMsg = "status update failed: " + err.Error()
						}
					}
				}
				m.inputMode = false
				m.inputBuffer = ""
				return m, func() tea.Msg { return m.fetch() }

			case "esc":
				m.inputMode = false
				m.inputBuffer = ""
				return m, nil

			case "backspace":
				if len(m.inputBuffer) > 0 {
					m.inputBuffer = m.inputBuffer[:len(m.inputBuffer)-1]
				}
				return m, nil

			default:
				if len(msg.String()) == 1 {
					m.inputBuffer += msg.String()
				}
				return m, nil
			}
		}

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

		case "enter":
			if m.focusedPanel == 1 {
				flat := m.flatTickets()
				if len(flat) > 0 {
					id := flat[m.ticketCursor].node.ID
					m.expanded[id] = !m.expanded[id]
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

		case "R":
			if m.focusedPanel == 0 && len(m.data.agents) > 0 {
				agent := m.data.agents[m.agentCursor]
				return m, m.restartAgentCmd(agent)
			}

		case "n":
			if m.focusedPanel == 0 && len(m.data.agents) > 0 {
				agent := m.data.agents[m.agentCursor]
				if !agent.TmuxSession.Valid || agent.TmuxSession.String == "" {
					m.statusMsg = fmt.Sprintf("agent %s has no tmux session recorded", agent.Name)
				} else if !m.sessionExists(agent.TmuxSession.String) {
					m.statusMsg = fmt.Sprintf("session %s not running", agent.TmuxSession.String)
				} else {
					m.inputMode = true
					m.inputAction = "nudge"
					m.inputTarget = agent.Name
					m.inputBuffer = ""
				}
			}

		case "o":
			if m.focusedPanel == 1 {
				flat := m.flatTickets()
				if len(flat) > 0 {
					node := flat[m.ticketCursor].node
					if node.PRNumber.Valid {
						return m, openPRCmd(int(node.PRNumber.Int64))
					}
					m.statusMsg = "no PR for this ticket"
				}
			}

		case "c":
			if m.focusedPanel == 1 {
				flat := m.flatTickets()
				if len(flat) > 0 {
					m.inputMode = true
					m.inputAction = "status"
					m.inputTarget = strconv.Itoa(flat[m.ticketCursor].node.ID)
					m.inputBuffer = ""
				}
			}

		case "C":
			m.inputMode = true
			m.inputAction = "create_ticket"
			m.inputTarget = ""
			m.inputBuffer = ""

		case "f":
			m.showClosed = !m.showClosed
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
		m.statusMsg = "" // clear transient status on each data refresh
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

// agentSessionName returns the recorded tmux session name for the agent with the
// given name, by looking it up in the most recent data snapshot. Returns empty
// string if the agent is not found or has no session recorded.
func (m dashboardModel) agentSessionName(name string) string {
	for _, a := range m.data.agents {
		if a.Name == name {
			if a.TmuxSession.Valid && a.TmuxSession.String != "" {
				return a.TmuxSession.String
			}
			return ""
		}
	}
	return ""
}

// killAgentCmd kills the agent's tmux session and marks it dead in the DB.
func (m dashboardModel) killAgentCmd(a *repo.Agent) tea.Cmd {
	return func() tea.Msg {
		sname := session.SessionName(a.Name)
		if err := m.killSession(sname); err != nil {
			return actionResultMsg{err: fmt.Errorf("kill session %s: %w", a.Name, err)}
		}
		if err := m.agents.UpdateStatus(a.Name, "dead"); err != nil {
			return actionResultMsg{err: fmt.Errorf("session for %s was killed but DB status update failed: %w", a.Name, err)}
		}
		return actionResultMsg{text: fmt.Sprintf("Killed %s", a.Name)}
	}
}

// stopAgentCmd sends a graceful shutdown signal to the agent's tmux session.
func (m dashboardModel) stopAgentCmd(a *repo.Agent) tea.Cmd {
	return func() tea.Msg {
		sname := session.SessionName(a.Name)
		if !m.sessionExists(sname) {
			return actionResultMsg{err: fmt.Errorf("no active session for %s", a.Name)}
		}
		msg := "Complete your current work, follow the completion protocol, and go idle."
		if err := m.sendKeys(sname, msg); err != nil {
			return actionResultMsg{err: fmt.Errorf("send stop signal to %s: %w", a.Name, err)}
		}
		return actionResultMsg{text: fmt.Sprintf("Sent stop signal to %s", a.Name)}
	}
}

// restartAgentCmd kills the agent's session, re-launches it, then sleeps briefly
// so the new tmux session has time to start before refreshing the dashboard.
func (m dashboardModel) restartAgentCmd(a *repo.Agent) tea.Cmd {
	return func() tea.Msg {
		sname := session.SessionName(a.Name)
		if err := m.killSession(sname); err != nil {
			return actionResultMsg{err: fmt.Errorf("kill session for %s: %w", a.Name, err)}
		}
		if err := m.restartAgent(a.Name, a.Type); err != nil {
			return actionResultMsg{err: fmt.Errorf("restart %s: %w", a.Name, err)}
		}
		m.sleepFn(2 * time.Second)
		return m.fetch()
	}
}

// openPRCmd opens a pull request in the browser using `gh pr view --web`.
func openPRCmd(prNumber int) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("gh", "pr", "view", strconv.Itoa(prNumber), "--web")
		if err := cmd.Run(); err != nil {
			return actionResultMsg{err: fmt.Errorf("open PR #%d: %w", prNumber, err)}
		}
		return actionResultMsg{text: fmt.Sprintf("Opened PR #%d in browser", prNumber)}
	}
}

// flatTickets returns the flat ordered list of visible ticket nodes (same order as rendered).
func (m dashboardModel) flatTickets() []flatNode {
	var cutoff time.Time
	if m.showClosed {
		cutoff = time.Time{} // zero value is before any real timestamp — keeps all nodes
	} else {
		cutoff = time.Now().Add(-4 * time.Hour)
	}
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
	contentHeight := m.height - 3 // 2 for panel border + 1 extra footer line (daemon status)

	// Split width roughly 35% agents / 65% tickets, minus 4 for panel borders/padding.
	agentWidth := m.width*35/100 - 4
	ticketWidth := m.width - agentWidth - 8 // borders + gap

	agentsPanel := m.renderAgents(agentWidth, contentHeight)
	ticketsPanel := m.renderTickets(ticketWidth, contentHeight)

	body := lipgloss.JoinHorizontal(lipgloss.Top, agentsPanel, "  ", ticketsPanel)

	var footer string
	if m.inputMode {
		var label string
		switch m.inputAction {
		case "nudge":
			label = "nudge " + m.inputTarget
		case "create_ticket":
			label = "new ticket title"
		case "status":
			label = fmt.Sprintf("status %s-%s  [draft/open/in_progress/in_review/pr_open/repairing/closed]", m.ticketPrefix, m.inputTarget)
		}
		footer = boldStyle.Render(fmt.Sprintf(" [%s] > %s█", label, m.inputBuffer))
	} else {
		hint := " q quit  r refresh  tab switch panel  j/k navigate"
		if m.focusedPanel == 0 {
			hint += "  a attach  x kill  s stop  R restart  n nudge"
		} else {
			filterFlag := " "
			if m.showClosed {
				filterFlag = "*"
			}
			hint += fmt.Sprintf("  enter expand  o open PR  c change status  C new ticket  f[%s]filter closed", filterFlag)
		}
		hint += fmt.Sprintf("  (auto-refresh every %s)", refreshInterval)
		if m.statusMsg != "" {
			hint += "  " + m.statusMsg
		}
		// Two-line footer: daemon liveness above the key hint.
		footer = m.renderDaemonLine() + "\n" + footerStyle.Render(hint)
	}

	return lipgloss.JoinVertical(lipgloss.Left, body, footer)
}

// renderDaemonLine renders a one-line daemon liveness status for the footer.
// Three states: fresh (✓ green), stale (⚠ red), missing (✗ red). Stale is
// defined as age > 3 × pollingInterval, floor 30s.
func (m dashboardModel) renderDaemonLine() string {
	if m.data.lastDaemonTick == nil {
		return statusStyles["dead"].Render(" daemon: not running ✗")
	}
	age := time.Since(*m.data.lastDaemonTick)
	staleThreshold := 3 * m.pollingInterval
	if staleThreshold < 30*time.Second {
		staleThreshold = 30 * time.Second
	}
	if age > staleThreshold {
		label := fmt.Sprintf(" daemon: %s ago ⚠  (expected every %s)", formatDuration(age), m.pollingInterval)
		return statusStyles["dead"].Render(label)
	}
	label := fmt.Sprintf(" daemon: %s ago ✓", formatDuration(age))
	return statusStyles["working"].Render(label)
}

func (m dashboardModel) renderAgents(width, height int) string {
	var sb strings.Builder
	sb.WriteString(headerStyle.Render("Agents") + "\n\n")

	focused := m.focusedPanel == 0
	// innerWidth is the content area width: outer width minus border (2) and padding (2).
	innerWidth := width - 4
	rowWidth := innerWidth

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
	// innerWidth is the content area width: outer width minus border (2) and padding (2).
	innerWidth := width - 4
	rowWidth := innerWidth

	flat := m.flatTickets()
	if len(flat) == 0 {
		sb.WriteString(footerStyle.Render("(no tickets)"))
	} else {
		for i, fn := range flat {
			line := renderIssueRow(fn.node, fn.depth, innerWidth)
			if focused && i == m.ticketCursor {
				line = selectedStyle.Width(rowWidth).Render(line)
			}
			sb.WriteString(line + "\n")
			if m.expanded[fn.node.ID] {
				sb.WriteString(renderTicketDetails(fn.node, fn.depth, innerWidth))
			}
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
	coloredStatus := colorStatus(node.Status)
	ageRaw := "(" + formatDuration(time.Since(node.UpdatedAt)) + ")"
	age := footerStyle.Render(ageRaw)

	prStr := "      " // 6 chars blank when no PR
	if node.PRNumber.Valid {
		prStr = fmt.Sprintf("%-6s", fmt.Sprintf("#%d", node.PRNumber.Int64))
	}

	const priorityWidth = 5 // visible chars: "[P0] " or "     "
	pri := priorityCell(node.Priority)

	const typeWidth = 1 // visible char: "E" / "B" / "R" / " " (blank for task)
	typ := typeCell(node.IssueType)

	// Truncate title so the row fits inside the panel content area. `width` is
	// the inner content width (outer panel width minus border and padding),
	// passed in from renderTickets.
	// prefix + space + type + space + id + space + status + space + priority + space + pr + space + age + space + title
	// Use lipgloss.Width(prefix) because the selected-row bullet (●) is 3 bytes / 1 cell;
	// len() would over-count by 2. Use len(node.Status) — the raw status is what the
	// row actually renders via coloredStatus, not any bracket-framed variant.
	fixedLen := lipgloss.Width(prefix) + 1 + typeWidth + 1 + len(idStr) + 1 + len(node.Status) + 1 + priorityWidth + 1 + len(prStr) + 1 + len(ageRaw) + 1
	titleMax := width - fixedLen
	title := node.Title
	if len(title) > titleMax && titleMax > 3 {
		title = title[:titleMax-1] + "…"
	}

	return fmt.Sprintf("%s %s %s %s %s %s %s %s",
		prefix, typ, idStr, coloredStatus, pri, prStr, age, title,
	)
}

// renderTicketDetails renders the expanded detail lines for a ticket.
// Returns a string (may be empty) to append after the ticket row.
func renderTicketDetails(node *repo.IssueNode, depth int, width int) string {
	var sb strings.Builder
	detailIndent := strings.Repeat("  ", depth+1)

	if node.Description.Valid && node.Description.String != "" {
		wrapped := wordWrap(node.Description.String, width-len(detailIndent)-4)
		for _, line := range strings.Split(wrapped, "\n") {
			sb.WriteString(fmt.Sprintf("%s  %s\n", detailIndent, footerStyle.Render(line)))
		}
	}

	if node.Assignee.Valid {
		sb.WriteString(fmt.Sprintf("%s  assignee: %s\n", detailIndent, footerStyle.Render(node.Assignee.String)))
	}

	if node.PRNumber.Valid {
		sb.WriteString(fmt.Sprintf("%s  PR: %s\n", detailIndent, footerStyle.Render(fmt.Sprintf("#%d", node.PRNumber.Int64))))
	}

	if node.Branch.Valid {
		sb.WriteString(fmt.Sprintf("%s  branch: %s\n", detailIndent, footerStyle.Render(node.Branch.String)))
	}

	sb.WriteString(fmt.Sprintf("%s  %s\n", detailIndent,
		footerStyle.Render(fmt.Sprintf("created: %s  updated: %s",
			node.CreatedAt.Format("2006-01-02 15:04"),
			node.UpdatedAt.Format("2006-01-02 15:04"),
		)),
	))
	sb.WriteString("\n")
	return sb.String()
}

// wordWrap wraps s to fit within width characters per line.
func wordWrap(s string, width int) string {
	if width <= 0 {
		return s
	}
	var result strings.Builder
	for _, line := range strings.Split(s, "\n") {
		for len(line) > width {
			i := strings.LastIndex(line[:width], " ")
			if i < 0 {
				i = width
			}
			result.WriteString(line[:i])
			result.WriteByte('\n')
			line = strings.TrimSpace(line[i:])
		}
		result.WriteString(line)
		result.WriteByte('\n')
	}
	return strings.TrimRight(result.String(), "\n")
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
