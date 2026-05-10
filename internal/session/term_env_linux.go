//go:build linux

package session

import (
	"bytes"
	"os"
)

// readClientEnv reads the environment of the process with the given pid from
// /proc/<pid>/environ, which is a NUL-delimited list of KEY=VALUE pairs.
func readClientEnv(pid string) (map[string]string, error) {
	data, err := os.ReadFile("/proc/" + pid + "/environ")
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	for _, kv := range bytes.Split(data, []byte{0}) {
		if i := bytes.IndexByte(kv, '='); i > 0 {
			out[string(kv[:i])] = string(kv[i+1:])
		}
	}
	return out, nil
}
