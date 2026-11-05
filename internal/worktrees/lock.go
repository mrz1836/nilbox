package worktrees

import (
	"regexp"
	"strconv"
)

// claudeLockPattern is the reason the coding agent gives `git worktree lock` while a
// session uses a worktree, e.g. "claude session pr-7 (pid 4242 start Tue Nov 4 10:00:00)".
// It mirrors the agent's own pattern for recognizing its locks.
var claudeLockPattern = regexp.MustCompile(`^claude (?:agent|session) .{1,255} \(pid (\d{1,10})(?: start (.{1,255}))?\)$`)

// liveness is whether a process is still running.
type liveness int

const (
	// livenessUnknown means the process's state cannot be determined on this OS.
	livenessUnknown liveness = iota

	// livenessAlive means the process exists.
	livenessAlive

	// livenessDead means no process has the PID.
	livenessDead
)

// ParseClaudeLock returns the PID recorded in a lock reason written by the coding agent.
// ok is false for any other reason, including an empty one.
func ParseClaudeLock(reason string) (pid int, ok bool) {
	m := claudeLockPattern.FindStringSubmatch(reason)
	if m == nil {
		return 0, false
	}

	pid, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, false
	}

	return pid, true
}

// staleClaudeLock reports whether a lock reason was written by an agent session whose
// process is no longer running. The lock stays in place; this only feeds a display hint.
func staleClaudeLock(reason string) bool {
	pid, ok := ParseClaudeLock(reason)

	return ok && processLiveness(pid) == livenessDead
}
