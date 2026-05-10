//go:build linux

package session

import (
	"bytes"
	"os"
	"strconv"
)

// readProcessEnvVar reads a single environment variable from a Linux process's
// /proc/<pid>/environ file, which is a NUL-delimited list of KEY=VALUE pairs.
// Returns the empty string when the variable is not set. Same-user-only on
// Linux; permission errors bubble up unchanged.
func readProcessEnvVar(pid int, key string) (string, error) {
	data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/environ")
	if err != nil {
		return "", err
	}
	prefix := []byte(key + "=")
	for _, kv := range bytes.Split(data, []byte{0}) {
		if bytes.HasPrefix(kv, prefix) {
			return string(kv[len(prefix):]), nil
		}
	}
	return "", nil
}
