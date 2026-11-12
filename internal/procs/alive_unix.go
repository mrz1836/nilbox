//go:build unix

package procs

import (
	"errors"
	"syscall"
)

// Probe reports whether a process has pid, with the signal 0, which checks for one
// without signaling it: Running for one the signal reaches or that is another user's,
// Gone when none has it, and Unknown for any other answer. A pid ValidPID refuses is
// Unknown, never signaled.
func Probe(pid int) Liveness {
	if !ValidPID(pid) {
		return Unknown
	}

	err := syscall.Kill(pid, 0)

	switch {
	case err == nil, errors.Is(err, syscall.EPERM):
		return Running
	case errors.Is(err, syscall.ESRCH):
		return Gone
	default:
		return Unknown
	}
}

// Alive reports whether a process with pid runs: one the signal 0 reaches, or that
// exists but is another user's. It sends no signal.
func Alive(pid int) bool {
	return Probe(pid) == Running
}
