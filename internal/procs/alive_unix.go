//go:build unix

package procs

import (
	"errors"
	"syscall"
)

// Alive reports whether a process with pid runs: one the signal 0 reaches, or that
// exists but is another user's. It sends no signal.
func Alive(pid int) bool {
	if pid <= 0 {
		return false
	}

	err := syscall.Kill(pid, 0)

	return err == nil || errors.Is(err, syscall.EPERM)
}
