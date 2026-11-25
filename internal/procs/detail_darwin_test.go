//go:build darwin

package procs_test

import (
	"os/exec"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/procs"
)

func TestDetailOfReadsAProcessWithNoTerminal(t *testing.T) {
	t.Parallel()

	// A session of its own has no controlling terminal, whatever runs the tests.
	cmd := exec.CommandContext(t.Context(), "/bin/sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Wait() }) // t.Context ends first, which stops it

	got, ok := procs.DetailOf(cmd.Process.Pid)
	require.True(t, ok)
	assert.Equal(t, procs.Detail{Args: "/bin/sleep 30"}, got)
}

func TestDetailOfAProcessThatIsGone(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(t.Context(), "/usr/bin/true")
	require.NoError(t, cmd.Run())

	_, ok := procs.DetailOf(cmd.Process.Pid)
	assert.False(t, ok)
}
