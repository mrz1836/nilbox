//go:build unix

package integrations

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// runEnded starts a fake tool playing run, kills it when kill is set, and returns how it
// ended.
func runEnded(t *testing.T, run testutil.FakeRun, kill bool) error {
	t.Helper()

	path, env := testutil.FakeTool(t, "tool", run)

	cmd := exec.CommandContext(t.Context(), path) //nolint:gosec // the fake tool the test made

	cmd.Env = append(os.Environ(), env)

	require.NoError(t, cmd.Start())

	if kill {
		require.NoError(t, cmd.Process.Kill())
	}

	return cmd.Wait()
}

func TestKilledTellsAProgramEndedBySIGKILL(t *testing.T) {
	t.Parallel()

	err := runEnded(t, testutil.FakeRun{Sleep: time.Minute}, true)
	require.Error(t, err)

	assert.True(t, killed(err))
	assert.True(t, killed(fmt.Errorf("run the tool: %w", err)), "found through a wrap")
}

func TestKilledLeavesEveryOtherEnd(t *testing.T) {
	t.Parallel()

	err := runEnded(t, testutil.FakeRun{Exit: 3}, false)
	require.Error(t, err)

	assert.False(t, killed(err), "an exit status is no signal")
	assert.False(t, killed(&exec.ExitError{}), "an exit with no process state")
	assert.False(t, killed(exec.ErrNotFound), "a program that never ran")
	assert.False(t, killed(nil))
}
