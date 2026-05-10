//go:build !darwin && !linux

package session

// readProcessTermProgram is a stub on unsupported platforms.
// nc-296 adds Linux support via /proc/<pid>/environ.
func readProcessTermProgram(_ int) (string, error) {
	return "", nil
}
