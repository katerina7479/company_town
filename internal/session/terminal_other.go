//go:build !darwin && !linux

package session

// readProcessEnvVar stubs client-env detection on platforms that have neither
// `ps eww` (macOS) nor /proc (Linux). detectTerminalProgram falls back to
// os.Getenv("TERM_PROGRAM") for these platforms.
func readProcessEnvVar(_ int, _ string) (string, error) {
	return "", nil
}
