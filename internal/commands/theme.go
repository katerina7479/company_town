package commands

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/katerina7479/company_town/internal/repo"
)

// StyleTheme holds all lipgloss styles used by the dashboard. Keeping them in
// a single struct makes it easy to inject a blank theme in tests (which have
// no real terminal and cannot render ANSI sequences meaningfully) and to swap
// themes in the future without touching rendering logic.
type StyleTheme struct {
	Bold         lipgloss.Style
	Panel        lipgloss.Style
	PanelFocused lipgloss.Style
	Header       lipgloss.Style
	Selected     lipgloss.Style
	Footer       lipgloss.Style
	Priority     map[string]lipgloss.Style
	Type         map[string]lipgloss.Style
	Status       map[string]lipgloss.Style
}

// DefaultTheme returns the production colour scheme used by the dashboard.
func DefaultTheme() StyleTheme {
	return StyleTheme{
		Bold: lipgloss.NewStyle().Bold(true),

		Panel: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(0, 1),

		PanelFocused: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("6")). // cyan when focused
			Padding(0, 1),

		Header:   lipgloss.NewStyle().Bold(true).Underline(true),
		Selected: lipgloss.NewStyle().Background(lipgloss.AdaptiveColor{Light: "252", Dark: "237"}),
		Footer:   lipgloss.NewStyle().Faint(true),

		Priority: map[string]lipgloss.Style{
			"P0": lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Bold(true), // bright red bold
			"P1": lipgloss.NewStyle().Foreground(lipgloss.Color("208")),          // orange
			"P2": lipgloss.NewStyle().Foreground(lipgloss.Color("3")),            // yellow
			// P3 intentionally absent: default foreground conveys "average/normal"
			"P4": lipgloss.NewStyle().Foreground(lipgloss.Color("242")), // medium gray (below average)
			"P5": lipgloss.NewStyle().Foreground(lipgloss.Color("238")), // dark gray (trivial/archive)
		},

		Type: map[string]lipgloss.Style{
			"epic":     lipgloss.NewStyle().Foreground(lipgloss.Color("5")).Bold(true), // magenta bold
			"bug":      lipgloss.NewStyle().Foreground(lipgloss.Color("9")),            // bright red
			"refactor": lipgloss.NewStyle().Foreground(lipgloss.Color("4")),            // blue
		},

		Status: map[string]lipgloss.Style{
			// Agent statuses
			"working": lipgloss.NewStyle().Foreground(lipgloss.Color("2")), // green
			"idle":    lipgloss.NewStyle().Foreground(lipgloss.Color("3")), // yellow
			"dead":    lipgloss.NewStyle().Foreground(lipgloss.Color("1")), // red
			// Ticket statuses
			repo.StatusDraft:         lipgloss.NewStyle().Foreground(lipgloss.Color("8")),   // dark gray
			repo.StatusOpen:          lipgloss.NewStyle().Foreground(lipgloss.Color("4")),   // blue
			repo.StatusInProgress:    lipgloss.NewStyle().Foreground(lipgloss.Color("6")),   // cyan
			repo.StatusCIRunning:     lipgloss.NewStyle().Foreground(lipgloss.Color("12")),  // bright blue — CI gating
			repo.StatusInReview:      lipgloss.NewStyle().Foreground(lipgloss.Color("5")),   // magenta
			repo.StatusUnderReview:   lipgloss.NewStyle().Foreground(lipgloss.Color("11")),  // bright yellow
			repo.StatusPROpen:        lipgloss.NewStyle().Foreground(lipgloss.Color("10")),  // bright green
			repo.StatusReviewed:      lipgloss.NewStyle().Foreground(lipgloss.Color("14")),  // bright cyan
			repo.StatusRepairing:     lipgloss.NewStyle().Foreground(lipgloss.Color("9")),   // bright red
			repo.StatusOnHold:        lipgloss.NewStyle().Foreground(lipgloss.Color("208")), // orange
			repo.StatusMergeConflict: lipgloss.NewStyle().Foreground(lipgloss.Color("196")), // bold red — needs human resolution
			repo.StatusClosed:        lipgloss.NewStyle().Foreground(lipgloss.Color("242")), // medium gray
			repo.StatusCancelled:     lipgloss.NewStyle().Foreground(lipgloss.Color("240")), // dark gray — abandoned, never landed
		},
	}
}

// ColorStatus renders status with its associated colour. Returns the status
// string unchanged when no style is registered for it.
func (t StyleTheme) ColorStatus(status string) string {
	if s, ok := t.Status[status]; ok {
		return s.Render(status)
	}
	return status
}

// PriorityCell returns a fixed 5-visible-char cell for the priority column.
// e.g. "[P1] " or "     " when NULL.
func (t StyleTheme) PriorityCell(p sql.NullString) string {
	const width = 5
	if !p.Valid || p.String == "" {
		return strings.Repeat(" ", width)
	}
	label := fmt.Sprintf("[%s]", p.String) // e.g. "[P0]"
	if s, ok := t.Priority[p.String]; ok {
		return s.Render(label) + " "
	}
	return fmt.Sprintf("%-*s", width, label)
}

// TypeCell returns a fixed 1-visible-char cell for the issue type column.
// epic → "E", bug → "B", refactor → "R", task → " " (blank — task is the
// default type). Unknown future types also render blank.
func (t StyleTheme) TypeCell(issueType string) string {
	letters := map[string]string{
		"epic":     "E",
		"bug":      "B",
		"refactor": "R",
	}
	letter, ok := letters[issueType]
	if !ok {
		return " " // task and unknown types get a blank cell
	}
	if s, ok2 := t.Type[issueType]; ok2 {
		return s.Render(letter)
	}
	return letter
}
