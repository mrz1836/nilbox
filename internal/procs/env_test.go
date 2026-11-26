//go:build darwin || linux

package procs_test

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestEnvOfReadsOneVariableOfARunningProcess(t *testing.T) {
	t.Parallel()

	// The test binary plays the process, as an agent's own binary would: macOS hides the
	// environment of its platform binaries, such as /bin/sleep, from a caller that isn't
	// root.
	path, fake := testutil.FakeTool(t, "agent", testutil.FakeRun{Sleep: time.Minute})
	link := "warp://session/" + strings.Repeat("0a", 16)

	cmd := exec.CommandContext(t.Context(), path, "PROCS_TEST_ONLY_AN_ARGUMENT=x") //nolint:gosec // the fake tool the test made

	cmd.Env = append(os.Environ(), fake,
		"PROCS_TEST_LINK_X=a longer name", "PROCS_TEST_LINK="+link, "PROCS_TEST_EMPTY=")
	require.NoError(t, cmd.Start())
	t.Cleanup(func() { _ = cmd.Wait() }) // t.Context ends first, which stops it

	pid := cmd.Process.Pid

	got, ok := procs.EnvOf(pid, "PROCS_TEST_LINK")
	require.True(t, ok, "the test binary's environment can be read")
	assert.Equal(t, link, got)

	got, ok = procs.EnvOf(pid, "PROCS_TEST_EMPTY")
	assert.True(t, ok)
	assert.Empty(t, got, "set, and empty")

	for _, name := range []string{"PROCS_TEST_MISSING", "PROCS_TEST_ONLY_AN_ARGUMENT", "PROCS_TEST", ""} {
		got, ok = procs.EnvOf(pid, name)
		assert.False(t, ok, "%q", name)
		assert.Empty(t, got, "%q", name)
	}
}

func TestEnvOfAProcessThatIsGone(t *testing.T) {
	t.Parallel()

	path, fake := testutil.FakeTool(t, "done", testutil.FakeRun{})

	cmd := exec.CommandContext(t.Context(), path) //nolint:gosec // the fake tool the test made

	cmd.Env = append(os.Environ(), fake, "PROCS_TEST_LINK=x")
	require.NoError(t, cmd.Run())

	_, ok := procs.EnvOf(cmd.Process.Pid, "PROCS_TEST_LINK")
	assert.False(t, ok)
}
