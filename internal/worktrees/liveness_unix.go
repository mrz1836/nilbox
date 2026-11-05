//go:build unix

package worktrees

import (
	"errors"
	"syscall"
)

// processLiveness probes pid with signal 0, which checks for the process without
// signaling it. EPERM means the process exists but belongs to another user.
func processLiveness(pid int) liveness {
	if pid <= 0 {
		return livenessUnknown
	}

	err := syscall.Kill(pid, 0)

	switch {
	case err == nil, errors.Is(err, syscall.EPERM):
		return livenessAlive
	case errors.Is(err, syscall.ESRCH):
		return livenessDead
	default:
		return livenessUnknown
	}
}
