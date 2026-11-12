package worktrees

import (
	"regexp"
	"strconv"
	"time"

	"github.com/mrz1836/nilbox/internal/procs"
)

// claudeLockPattern is the reason the coding agent gives `git worktree lock` while a
// session uses a worktree, e.g. "claude session pr-7 (pid 4242 start Fri Nov  6 17:14:26
// 2026)". It mirrors the agent's own pattern for recognizing its locks.
var claudeLockPattern = regexp.MustCompile(`^claude (?:agent|session) .{1,255} \(pid (\d{1,10})(?: start (.{1,255}))?\)$`)

// lockStartLayout is how the agent writes a session's start: C's asctime form, with no
// zone.
const lockStartLayout = time.ANSIC

// startSlack absorbs the whole seconds the agent writes a session's start in.
const startSlack = 2 * time.Second

// processTable looks up running processes. osProcesses is the real one.
type processTable interface {
	// lookup reports whether pid is running and, when it is and the OS says, when it
	// started; the time is zero otherwise.
	lookup(pid int) (procs.Liveness, time.Time)
}

// osProcesses asks the operating system.
type osProcesses struct{}

// lookup probes pid (procs.Probe, which never signals a PID out of range and calls it
// Unknown) and reads its start time.
func (osProcesses) lookup(pid int) (procs.Liveness, time.Time) {
	state := procs.Probe(pid)
	if state != procs.Running {
		return state, time.Time{}
	}

	return state, processStart(pid)
}

// agentLock is a lock reason the coding agent wrote.
type agentLock struct {
	pid int

	// start is the session's start as written, or "" when the reason has none.
	start string
}

// parseAgentLock reads a lock reason written by the coding agent.
func parseAgentLock(reason string) (agentLock, bool) {
	m := claudeLockPattern.FindStringSubmatch(reason)
	if m == nil {
		return agentLock{}, false
	}

	pid, err := strconv.Atoi(m[1])
	if err != nil {
		return agentLock{}, false
	}

	return agentLock{pid: pid, start: m[2]}, true
}

// latestStart is the latest moment the recorded start can name. The agent writes it
// without a zone, so it is read both as UTC and in local, the machine's zone, and the
// later reading is kept: that is never earlier than the real start, so a process that
// started after it cannot be the session. ok is false when there is no start or it does
// not parse.
func (l agentLock) latestStart(local *time.Location) (time.Time, bool) {
	if l.start == "" {
		return time.Time{}, false
	}

	asUTC, err := time.ParseInLocation(lockStartLayout, l.start, time.UTC)
	if err != nil {
		return time.Time{}, false
	}

	asLocal, err := time.ParseInLocation(lockStartLayout, l.start, local)
	if err != nil {
		return time.Time{}, false
	}

	if asLocal.After(asUTC) {
		return asLocal, true
	}

	return asUTC, true
}

// sessionEnded reports whether the session that wrote lock has certainly ended: no
// process has its PID any more, or the process that has it now started after the session
// did, so the PID was reused by another program (after a restart, say). Anything in doubt
// counts as still running, a PID out of range included, because a lock judged ended can be
// released. local is the machine's zone, which the recorded start may be in.
func sessionEnded(lock agentLock, table processTable, local *time.Location) bool {
	state, started := table.lookup(lock.pid)

	switch state {
	case procs.Gone:
		return true
	case procs.Running:
		latest, ok := lock.latestStart(local)

		return ok && !started.IsZero() && started.After(latest.Add(startSlack))
	case procs.Unknown:
		return false
	}

	return false
}

// endedAgentLock reports whether reason is an agent lock whose session has ended.
func endedAgentLock(reason string, table processTable, local *time.Location) bool {
	lock, ok := parseAgentLock(reason)

	return ok && sessionEnded(lock, table, local)
}
