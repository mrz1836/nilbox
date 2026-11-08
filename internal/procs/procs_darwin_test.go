//go:build darwin

package procs_test

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// exitError returns a real *exec.ExitError, from a command that exits 1.
func exitError(t *testing.T) error {
	t.Helper()

	err := exec.CommandContext(t.Context(), "/usr/bin/false").Run()
	require.Error(t, err)

	return err
}

func TestSystemAsksLsofForTheUsersProcesses(t *testing.T) {
	t.Parallel()

	runner := &testutil.Runner{Stdout: "p9\ncb\nfcwd\nn/srv/b\np7\nca\nfcwd\nn/srv/a\n"}

	got, err := procs.System{Run: runner}.Processes(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []procs.Process{{PID: 7, Command: "a", Dir: "/srv/a"}, {PID: 9, Command: "b", Dir: "/srv/b"}}, got,
		"by PID")

	calls := runner.Calls()
	require.Len(t, calls, 1)
	assert.True(t, strings.HasPrefix(strings.Join(calls[0], " "), "/usr/sbin/lsof -nP -a -u "))
	assert.Equal(t, []string{"-d", "cwd", "-FpcRn"}, calls[0][len(calls[0])-3:])
}

func TestSystemKeepsWhatLsofPrintedBeforeItsExit(t *testing.T) {
	t.Parallel()

	exited := exitError(t)
	runner := &testutil.Runner{Script: func(context.Context, []string) ([]byte, []byte, error) {
		return []byte("p7\ncclaude\nfcwd\nn/srv/a\n"), nil, exited
	}}

	got, err := procs.System{Run: runner}.Processes(t.Context())
	require.NoError(t, err, "a process that ended while lsof read is no failure")
	assert.Len(t, got, 1)

	_, err = procs.System{Run: &testutil.Runner{Err: exited}}.Processes(t.Context())
	require.ErrorContains(t, err, "list the running processes", "an exit with nothing printed is")

	_, err = procs.System{Run: &testutil.Runner{Err: testutil.ErrNotFound}}.Processes(t.Context())
	require.ErrorIs(t, err, testutil.ErrNotFound)
}

func TestSystemListsThisProcess(t *testing.T) {
	t.Parallel()

	got, err := procs.System{}.Processes(t.Context())
	if err != nil {
		t.Skipf("lsof is not usable here: %v", err)
	}

	assert.NotEmpty(t, got)
}
