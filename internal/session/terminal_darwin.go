//go:build darwin

package session

import (
	"os/exec"
	"strconv"
	"strings"
)

// readProcessEnvVar reads a single environment variable from the process
// environment of pid using ps eww, which appends the full environment to the
// process listing. Returns the empty string when the variable is not set.
func readProcessEnvVar(pid int, key string) (string, error) {
	out, err := exec.Command("ps", "eww", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return "", err
	}
	return parseEnvVarFromPS(string(out), key), nil
}

// parseEnvVarFromPS extracts the value of the named environment variable from
// ps eww output. The environment vars are appended to the CMD column separated
// by whitespace; the first match wins.
func parseEnvVarFromPS(s, key string) string {
	marker := key + "="
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
