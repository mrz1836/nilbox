//go:build darwin

package launcher

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeExecutor records the programs it was asked to run instead of running them.
type fakeExecutor struct {
	name     string
	args     []string
	deadline time.Duration
	calls    int
	stderr   string
	err      error
}

func (e *fakeExecutor) Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	e.calls++
	e.name = name
	e.args = args

	if d, ok := ctx.Deadline(); ok {
		e.deadline = time.Until(d)
	}

	return nil, []byte(e.stderr), e.err
}

func TestDefaultRegistryOnDarwin(t *testing.T) {
	r := DefaultRegistry(&fakeExecutor{})

	assert.Equal(t, []string{"iterm2", "terminal"}, r.IDs())

	d, err := r.Get("iterm2")
	require.NoError(t, err)
	assert.Equal(t, "iterm2", d.ID())

	d, err = r.Get("terminal")
	require.NoError(t, err)
	assert.Equal(t, "terminal", d.ID())
}

func TestITermDriverRunsOsascript(t *testing.T) {
	ex := &fakeExecutor{}

	d, err := DefaultRegistry(ex).Get("iterm2")
	require.NoError(t, err)
	require.NoError(t, d.Open(t.Context(), awkwardScriptPath))

	assert.Equal(t, 1, ex.calls)
	assert.Equal(t, "osascript", ex.name)
	assert.Equal(t, []string{"-e", itermAppleScript(awkwardScriptPath)}, ex.args)
	assert.Greater(t, ex.deadline, time.Duration(0))
	assert.LessOrEqual(t, ex.deadline, openTimeout)
}

func TestTerminalAppDriverRunsOpen(t *testing.T) {
	ex := &fakeExecutor{}

	d, err := DefaultRegistry(ex).Get("terminal")
	require.NoError(t, err)
	require.NoError(t, d.Open(t.Context(), awkwardScriptPath))

	assert.Equal(t, "open", ex.name)
	assert.Equal(t, []string{"-a", "Terminal", awkwardScriptPath}, ex.args)
	assert.Greater(t, ex.deadline, time.Duration(0))
}

func TestDriverReportsAutomationDenied(t *testing.T) {
	ex := &fakeExecutor{
		stderr: "execution error: Not authorized to send Apple events to iTerm. (-1743)",
		err:    &exec.ExitError{},
	}

	d, err := DefaultRegistry(ex).Get("iterm2")
	require.NoError(t, err)

	err = d.Open(t.Context(), awkwardScriptPath)
	require.ErrorIs(t, err, ErrAutomationDenied)
}

func TestDriverReportsOpenFailure(t *testing.T) {
	ex := &fakeExecutor{stderr: "LSOpenURLsWithRole() failed", err: &exec.ExitError{}}

	d, err := DefaultRegistry(ex).Get("terminal")
	require.NoError(t, err)

	err = d.Open(t.Context(), awkwardScriptPath)
	require.ErrorIs(t, err, ErrTerminalOpen)
	assert.Contains(t, err.Error(), "Terminal: LSOpenURLsWithRole() failed")
}

func TestDriversRejectUnsafePaths(t *testing.T) {
	for _, id := range []string{"iterm2", "terminal"} {
		t.Run(id, func(t *testing.T) {
			ex := &fakeExecutor{}

			d, err := DefaultRegistry(ex).Get(id)
			require.NoError(t, err)

			require.ErrorIs(t, d.Open(t.Context(), "relative.command"), ErrInvalidScriptPath)
			require.ErrorIs(t, d.Open(t.Context(), "/tmp/a\nb.command"), ErrInvalidScriptPath)
			assert.Zero(t, ex.calls, "nothing runs for an unsafe path")
		})
	}
}
