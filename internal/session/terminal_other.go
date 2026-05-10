//go:build !darwin

package session

// readProcessTermProgram stubs client-env detection on non-darwin platforms.
// On Linux, detectTerminalProgram falls back to os.Getenv("TERM_PROGRAM").
// nc-296 replaces this stub with a /proc/<pid>/environ implementation.
func readProcessTermProgram(_ int) (string, error) {
	return "", nil
}
