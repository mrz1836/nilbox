package worktrees

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseClaudeLock(t *testing.T) {
	tests := []struct {
		reason string
		pid    int
		ok     bool
	}{
		{reason: claudeArg, pid: 4242, ok: true},
		{reason: "claude session x (pid 1 start y)", pid: 1, ok: true},
		{reason: "claude agent issue-12 (pid 77)", pid: 77, ok: true},
		{reason: "claude session pr+7 (with parens) (pid 9 start Wed)", pid: 9, ok: true},
		{reason: "claude session x (pid 1234567890)", pid: 1234567890, ok: true},
		{reason: ""},
		{reason: "locked by hand"},
		{reason: "claude session  (pid 1)"},
		{reason: "claude worker x (pid 1)"},
		{reason: "claude session x (pid abc)"},
		{reason: "claude session x (pid 12345678901)"},
		{reason: "claude session x (pid 1) trailing"},
		{reason: "Claude session x (pid 1)"},
	}

	for _, tt := range tests {
		t.Run(tt.reason, func(t *testing.T) {
			pid, ok := ParseClaudeLock(tt.reason)
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.pid, pid)
		})
	}
}

func TestParseAgentLockKeepsTheStart(t *testing.T) {
	lock, ok := parseAgentLock("claude session pr-184 (pid 78068 start Fri Nov  6 17:14:26 2026)")
	require.True(t, ok)
	assert.Equal(t, agentLock{pid: 78068, start: "Fri Nov  6 17:14:26 2026"}, lock)

	lock, ok = parseAgentLock("claude agent issue-12 (pid 77)")
	require.True(t, ok)
	assert.Equal(t, agentLock{pid: 77}, lock)
}

func TestLatestStartReadsTheLaterOfUTCAndLocal(t *testing.T) {
	lock := agentLock{start: "Fri Nov  6 17:14:26 2026"}

	// The machine two hours behind UTC: "17:14" in its zone is later than in UTC.
	got, ok := lock.latestStart(time.FixedZone("behind", -2*60*60))
	require.True(t, ok)
	assert.True(t, got.Equal(time.Date(2026, 11, 6, 19, 14, 26, 0, time.UTC)), "read in the machine's zone: %s", got)

	// The machine ahead of UTC: the UTC reading is the later one.
	got, ok = lock.latestStart(time.FixedZone("ahead", 3*60*60))
	require.True(t, ok)
	assert.True(t, got.Equal(time.Date(2026, 11, 6, 17, 14, 26, 0, time.UTC)), "read as UTC: %s", got)

	_, ok = agentLock{}.latestStart(time.UTC)
	assert.False(t, ok, "no start")

	_, ok = agentLock{start: "yesterday"}.latestStart(time.UTC)
	assert.False(t, ok, "a start that does not parse")
}

// fakeProcesses answers lookups from a table; a PID not in it is dead.
type fakeProcesses map[int]fakeProcess

type fakeProcess struct {
	state   liveness
	started time.Time
}

func (f fakeProcesses) lookup(pid int) (liveness, time.Time) {
	p, ok := f[pid]
	if !ok {
		return livenessDead, time.Time{}
	}

	return p.state, p.started
}

func TestSessionEnded(t *testing.T) {
	start := "Fri Nov  6 17:14:26 2026"
	at := time.Date(2026, 11, 6, 17, 14, 26, 0, time.UTC)

	tests := []struct {
		name  string
		lock  agentLock
		procs fakeProcesses
		ended bool
	}{
		{
			name:  "no process has the PID",
			lock:  agentLock{pid: 10, start: start},
			procs: fakeProcesses{},
			ended: true,
		},
		{
			name:  "the session's own process, started within the second it recorded",
			lock:  agentLock{pid: 10, start: start},
			procs: fakeProcesses{10: {state: livenessAlive, started: at.Add(900 * time.Millisecond)}},
		},
		{
			name:  "the session's process, started before the time it recorded",
			lock:  agentLock{pid: 10, start: start},
			procs: fakeProcesses{10: {state: livenessAlive, started: at.Add(-3 * time.Second)}},
		},
		{
			name:  "a reused PID: the process started after the session",
			lock:  agentLock{pid: 10, start: start},
			procs: fakeProcesses{10: {state: livenessAlive, started: at.Add(10 * time.Hour)}},
			ended: true,
		},
		{
			name:  "a live process whose start is unknown counts as the session",
			lock:  agentLock{pid: 10, start: start},
			procs: fakeProcesses{10: {state: livenessAlive}},
		},
		{
			name:  "a live process when the lock has no start counts as the session",
			lock:  agentLock{pid: 10},
			procs: fakeProcesses{10: {state: livenessAlive, started: at.Add(10 * time.Hour)}},
		},
		{
			name:  "an unknown state counts as running",
			lock:  agentLock{pid: 10, start: start},
			procs: fakeProcesses{10: {state: livenessUnknown}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.ended, sessionEnded(tt.lock, tt.procs, time.UTC))
		})
	}
}

func TestEndedAgentLock(t *testing.T) {
	procs := fakeProcesses{7: {state: livenessAlive}}

	assert.True(t, endedAgentLock("claude session pr-7 (pid 8)", procs, time.UTC), "an ended session's lock")
	assert.False(t, endedAgentLock("claude session pr-7 (pid 7)", procs, time.UTC), "a running session's lock")
	assert.False(t, endedAgentLock("locked by hand", procs, time.UTC), "only agent locks are judged")
	assert.False(t, endedAgentLock("", procs, time.UTC))
}

// deadPID starts a short-lived child, waits for it so it is reaped, and returns its PID.
func deadPID(t *testing.T) int {
	t.Helper()

	bin, err := exec.LookPath("true")
	if err != nil {
		t.Skip("no true command to start a short-lived process")
	}

	cmd := exec.CommandContext(t.Context(), bin) //nolint:gosec // the test's own lookup of the true command
	require.NoError(t, cmd.Run())

	return cmd.Process.Pid
}

func TestOSProcesses(t *testing.T) {
	if processLiveness(os.Getpid()) == livenessUnknown {
		t.Skip("process liveness is not supported on this OS")
	}

	dead := deadPID(t)

	assert.Equal(t, livenessAlive, processLiveness(os.Getpid()))
	assert.Equal(t, livenessDead, processLiveness(dead))
	assert.Equal(t, livenessUnknown, processLiveness(0))
	assert.Equal(t, livenessUnknown, processLiveness(-1))

	state, started := osProcesses{}.lookup(dead)
	assert.Equal(t, livenessDead, state)
	assert.True(t, started.IsZero(), "a dead process has no start")

	state, started = osProcesses{}.lookup(os.Getpid())
	assert.Equal(t, livenessAlive, state)

	if runtime.GOOS == "darwin" {
		require.False(t, started.IsZero(), "the start of a running process is read")
		assert.True(t, started.Before(time.Now()), "it started in the past")
		assert.Less(t, time.Since(started), 24*time.Hour, "the test process started recently")
	} else {
		assert.True(t, started.IsZero(), "the start is not read on this OS")
	}

	assert.True(t, processStart(0).IsZero())

	own := "claude session pr-7 (pid " + strconv.Itoa(os.Getpid()) + " start " + time.Now().UTC().Format(lockStartLayout) + ")"
	ended := "claude session pr-7 (pid " + strconv.Itoa(dead) + ")"

	assert.False(t, endedAgentLock(own, osProcesses{}, time.UTC), "this test's own process is running")
	assert.True(t, endedAgentLock(ended, osProcesses{}, time.UTC), "a reaped process has ended")
}
