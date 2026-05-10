//go:build darwin

package session

import (
	"os/exec"
	"strconv"
	"strings"
)

// readProcessTermProgram reads TERM_PROGRAM from the process environment of pid
// using ps eww, which appends the full environment to the process listing.
func readProcessTermProgram(pid int) (string, error) {
	out, err := exec.Command("ps", "eww", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", err
	}
	return parseTermProgramFromPS(string(out)), nil
}

// parseTermProgramFromPS extracts the TERM_PROGRAM value from ps eww output.
// The environment vars are appended to the CMD column separated by spaces.
func parseTermProgramFromPS(s string) string {
	const marker = "TERM_PROGRAM="
	idx := strings.Index(s, marker)
	if idx == -1 {
		return ""
	}
	rest := s[idx+len(marker):]
	end := strings.IndexAny(rest, " \t\n")
	if end == -1 {
		return rest
	}
	return rest[:end]
}
