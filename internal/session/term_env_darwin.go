//go:build darwin

package session

import (
	"os/exec"
	"strings"
)

// readClientEnv reads the environment of the process with the given pid by
// running "ps eww -p <pid>" and extracting KEY=VALUE tokens from its output.
// Values with spaces or embedded equals signs may be truncated by ps, but the
// variables we need (TERM_PROGRAM, TERM) are simple strings without spaces.
func readClientEnv(pid string) (map[string]string, error) {
	out, err := exec.Command("ps", "eww", "-p", pid).CombinedOutput()
	if err != nil {
		return nil, err
	}
	env := make(map[string]string)
	for _, tok := range strings.Fields(string(out)) {
		eq := strings.IndexByte(tok, '=')
		if eq < 1 {
			continue
		}
		key := tok[:eq]
		if isEnvKey(key) {
			env[key] = tok[eq+1:]
		}
	}
	return env, nil
}

// isEnvKey reports whether s looks like a valid shell environment variable name:
// [A-Za-z_][A-Za-z0-9_]*. Used to filter tokens in ps output that happen to
// contain '=' but are not env vars (e.g., command-line arguments).
func isEnvKey(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i, c := range s {
		alpha := (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_'
		digit := c >= '0' && c <= '9'
		if !alpha && (i == 0 || !digit) {
			return false
		}
	}
	return true
}
