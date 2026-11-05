package worktrees

import (
	"os"
	"os/exec"
	"strconv"
	"testing"

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

func TestStaleClaudeLock(t *testing.T) {
	if processLiveness(os.Getpid()) == livenessUnknown {
		t.Skip("process liveness is not supported on this OS")
	}

	dead := deadPID(t)

	assert.Equal(t, livenessAlive, processLiveness(os.Getpid()))
	assert.Equal(t, livenessDead, processLiveness(dead))
	assert.Equal(t, livenessUnknown, processLiveness(0))
	assert.Equal(t, livenessUnknown, processLiveness(-1))

	alive := "claude session pr-7 (pid " + strconv.Itoa(os.Getpid()) + " start now)"
	ended := "claude session pr-7 (pid " + strconv.Itoa(dead) + " start earlier)"

	assert.False(t, staleClaudeLock(alive), "a running session's lock is not stale")
	assert.True(t, staleClaudeLock(ended), "an ended session's lock is stale")
	assert.False(t, staleClaudeLock("locked by hand"), "only agent locks are judged")
	assert.False(t, staleClaudeLock("claude session x (pid 0)"), "an unknown PID is not called stale")
}
