package session

import (
	"fmt"
	"os/exec"
	"strings"
)

// agentTypeColors maps agent type names to portable tmux color names.
var agentTypeColors = map[string]string{
	"mayor":     "blue",
	"architect": "magenta",
	"reviewer":  "yellow",
	"daemon":    "brightblack",
	"prole":     "green",
	"artisan":   "cyan",
}

// styleSessionExec is the exec seam for tmux set-option calls.
// Tests replace this to capture argv without shelling out.
var styleSessionExec = func(args ...string) error {
	return exec.Command("tmux", args...).Run()
}

// AgentTypeFromSessionName derives the agent type from a tmux session name.
func AgentTypeFromSessionName(name string) string {
	trimmed := strings.TrimPrefix(name, SessionPrefix)
	if idx := strings.Index(trimmed, "-"); idx >= 0 {
		return trimmed[:idx]
	}
	return trimmed
}

// ApplyStatusBar sets tmux status bar options for a session.
// Returns nil without making any tmux calls if agentType is empty or unknown.
func ApplyStatusBar(sessionName, agentType string) error {
	color, ok := agentTypeColors[agentType]
	if !ok {
		return nil
	}

	statusRight := "C-b d to detach | %H:%M %d-%b-%y"
	statusStyle := fmt.Sprintf("bg=%s,fg=black", color)

	for _, opt := range [][2]string{
		{"status-right", statusRight},
		{"status-style", statusStyle},
	} {
		if err := styleSessionExec("set-option", "-t", sessionName, opt[0], opt[1]); err != nil {
			return fmt.Errorf("tmux set-option %s on %s: %w", opt[0], sessionName, err)
		}
	}

	return nil
}
