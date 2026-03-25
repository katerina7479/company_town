//go:build !windows

package session

import (
	"os"
	"syscall"
)

// execSyscall replaces the current process with the given command.
func execSyscall(path string, args []string) error {
	return syscall.Exec(path, args, os.Environ())
}
