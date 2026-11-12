//go:build unix

package procs_test

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/procs"
)

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

func TestProbe(t *testing.T) {
	t.Parallel()

	dead := deadPID(t)

	assert.Equal(t, procs.Running, procs.Probe(os.Getpid()), "this test runs")
	assert.Equal(t, procs.Gone, procs.Probe(dead), "a reaped child has ended")

	for _, pid := range []int{0, -1, procs.MaxPID + 1, 1<<32 - 1} {
		assert.Equal(t, procs.Unknown, procs.Probe(pid), "pid %d is never signaled", pid)
	}

	assert.False(t, procs.Alive(dead), "Alive is Probe's Running")
}
