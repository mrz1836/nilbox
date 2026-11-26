//go:build darwin

package terminal

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/procs"
)

// fakeExecutor records the programs it was asked to run instead of running them.
type fakeExecutor struct {
	name     string
	args     []string
	deadline time.Duration
	calls    int
	stdout   string
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

	return []byte(e.stdout), []byte(e.stderr), e.err
}

func TestDefaultRegistryOnDarwin(t *testing.T) {
	t.Parallel()

	r := DefaultRegistry(&fakeExecutor{})

	assert.Equal(t, []ID{ITerm2, TerminalApp, Warp}, r.IDs())

	d, err := r.Get("iterm2")
	require.NoError(t, err)
	assert.Equal(t, ITerm2, d.ID())

	d, err = r.Get("terminal")
	require.NoError(t, err)
	assert.Equal(t, TerminalApp, d.ID())

	d, err = r.Get("warp")
	require.NoError(t, err)
	assert.Equal(t, Warp, d.ID())
}

func TestITermDriverRunsOsascript(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	ex := &fakeExecutor{}

	d, err := DefaultRegistry(ex).Get("terminal")
	require.NoError(t, err)
	require.NoError(t, d.Open(t.Context(), awkwardScriptPath))

	assert.Equal(t, "open", ex.name)
	assert.Equal(t, []string{"-a", "Terminal", awkwardScriptPath}, ex.args)
	assert.Greater(t, ex.deadline, time.Duration(0))
}

func TestWarpDriverRunsOpenByBundleID(t *testing.T) {
	t.Parallel()

	ex := &fakeExecutor{}

	d, err := DefaultRegistry(ex).Get("warp")
	require.NoError(t, err)
	require.NoError(t, d.Open(t.Context(), awkwardScriptPath))

	assert.Equal(t, 1, ex.calls)
	assert.Equal(t, "open", ex.name)
	assert.Equal(t, []string{"-b", "dev.warp.Warp-Stable", awkwardScriptPath}, ex.args)
	assert.Greater(t, ex.deadline, time.Duration(0))
	assert.LessOrEqual(t, ex.deadline, openTimeout)
}

func TestWarpDriverReportsWarpMissing(t *testing.T) {
	t.Parallel()

	ex := &fakeExecutor{
		stderr: "LSCopyApplicationURLsForBundleIdentifier() failed while trying to determine the application " +
			"with bundle identifier dev.warp.Warp-Stable.\n",
		err: &exec.ExitError{},
	}

	d, err := DefaultRegistry(ex).Get("warp")
	require.NoError(t, err)

	err = d.Open(t.Context(), awkwardScriptPath)
	require.ErrorIs(t, err, ErrTerminalOpen)
	require.NotErrorIs(t, err, ErrAutomationDenied)
	assert.Contains(t, err.Error(), "Warp: LSCopyApplicationURLsForBundleIdentifier() failed")
}

func TestDriverReportsAutomationDenied(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

	ex := &fakeExecutor{stderr: "LSOpenURLsWithRole() failed", err: &exec.ExitError{}}

	d, err := DefaultRegistry(ex).Get("terminal")
	require.NoError(t, err)

	err = d.Open(t.Context(), awkwardScriptPath)
	require.ErrorIs(t, err, ErrTerminalOpen)
	assert.Contains(t, err.Error(), "Terminal: LSOpenURLsWithRole() failed")
}

func TestDriversRejectUnsafePaths(t *testing.T) {
	t.Parallel()

	for _, id := range []ID{ITerm2, Warp, TerminalApp} {
		t.Run(string(id), func(t *testing.T) {
			t.Parallel()

			ex := &fakeExecutor{}

			d, err := DefaultRegistry(ex).Get(id)
			require.NoError(t, err)

			require.ErrorIs(t, d.Open(t.Context(), "relative.command"), ErrInvalidScriptPath)
			require.ErrorIs(t, d.Open(t.Context(), "/tmp/a\nb.command"), ErrInvalidScriptPath)
			assert.Zero(t, ex.calls, "nothing runs for an unsafe path")
		})
	}
}

func TestDriversFocusATabByItsTTY(t *testing.T) {
	t.Parallel()

	tab := Tab{TTY: testTTY, PID: 4100}

	for id, script := range map[ID]func(string) string{ITerm2: itermFocusScript, TerminalApp: terminalAppFocusScript} {
		ex := &fakeExecutor{stdout: "found\n"}

		require.NoError(t, DefaultRegistry(ex).Focus(t.Context(), id, tab), id)
		assert.Equal(t, "osascript", ex.name)
		assert.Equal(t, []string{"-e", script(testTTY)}, ex.args)
		assert.LessOrEqual(t, ex.deadline, openTimeout)

		ex.stdout = "missing\n"
		require.ErrorIs(t, DefaultRegistry(ex).Focus(t.Context(), id, tab), ErrNoSuchTab, id)

		ex.stdout, ex.stderr, ex.err = "", "execution error: Not authorized to send Apple events (-1743)", &exec.ExitError{}
		require.ErrorIs(t, DefaultRegistry(ex).Focus(t.Context(), id, tab), ErrAutomationDenied, id)

		calls := ex.calls
		require.ErrorIs(t, DefaultRegistry(ex).Focus(t.Context(), id, Tab{TTY: `/dev/ttys004" then quit`, PID: 4100}),
			ErrInvalidTTY, id)
		require.ErrorIs(t, DefaultRegistry(ex).Focus(t.Context(), id, Tab{PID: 4100}), ErrInvalidTTY,
			"%s finds a tab by its tty alone", id)
		assert.Equal(t, calls, ex.calls, "nothing runs for a device that is not one")
	}
}

// fakeEnv is a process's environment as a Warp driver's env reads it: the variables of
// each process by PID. It records what it was asked.
type fakeEnv struct {
	vars  map[int]map[string]string
	asked []string
}

func (e *fakeEnv) read(pid int, name string) (string, bool) {
	e.asked = append(e.asked, fmt.Sprintf("%d %s", pid, name))
	value, ok := e.vars[pid][name]

	return value, ok
}

// warpFocusing returns a Warp driver over ex whose processes' environments are vars.
func warpFocusing(ex *fakeExecutor, vars map[int]map[string]string) (Registry, *fakeEnv) {
	env := &fakeEnv{vars: vars}

	return NewRegistry(warpDriver{executor: ex, env: env.read}), env
}

func TestWarpDriverFocusesByTheSessionsLink(t *testing.T) {
	t.Parallel()

	ex := &fakeExecutor{}
	r, env := warpFocusing(ex, map[int]map[string]string{
		4100: {"WARP_FOCUS_URL": warpTestLink, "TERM_PROGRAM": "WarpTerminal"},
	})

	require.NoError(t, r.Focus(t.Context(), Warp, Tab{TTY: testTTY, PID: 4100}))
	assert.Equal(t, []string{"4100 WARP_FOCUS_URL"}, env.asked, "it reads that one variable of the session's process")
	assert.Equal(t, 1, ex.calls)
	assert.Equal(t, "open", ex.name)
	assert.Equal(t, []string{"-b", "dev.warp.Warp-Stable", warpTestLink}, ex.args)
	assert.Greater(t, ex.deadline, time.Duration(0))
	assert.LessOrEqual(t, ex.deadline, openTimeout)

	require.NoError(t, r.Focus(t.Context(), Warp, Tab{PID: 4100}), "Warp needs no tty")
}

func TestWarpDriverFocusesARunByTheLinkItsScriptRecorded(t *testing.T) {
	t.Parallel()

	ex := &fakeExecutor{}
	r, env := warpFocusing(ex, map[int]map[string]string{}) // a shell, whose environment macOS hides

	require.NoError(t, r.Focus(t.Context(), Warp, Tab{TTY: testTTY, PID: 4100, Link: warpTestLink}))
	assert.Empty(t, env.asked, "the recorded link needs no environment")
	assert.Equal(t, 1, ex.calls)
	assert.Equal(t, "open", ex.name)
	assert.Equal(t, []string{"-b", "dev.warp.Warp-Stable", warpTestLink}, ex.args)
	assert.LessOrEqual(t, ex.deadline, openTimeout)

	for name, bad := range map[string]string{
		"an action":          "warp://action/new_tab?path=/tmp/launch/pr-7.command",
		"a trailing newline": warpTestLink + "\n",
		"a short link":       warpTestLink[:len(warpTestLink)-1],
	} {
		ex := &fakeExecutor{}
		r, env := warpFocusing(ex, map[int]map[string]string{})

		err := r.Focus(t.Context(), Warp, Tab{TTY: testTTY, PID: 4100, Link: bad})
		require.ErrorIs(t, err, ErrNoFocusLink, name)
		assert.NotContains(t, err.Error(), "path=", "%s: the error never repeats a value that isn't a link", name)
		assert.Equal(t, []string{"4100 WARP_FOCUS_URL"}, env.asked, "%s: it falls back on the process's link", name)
		assert.Zero(t, ex.calls, "%s: nothing is opened in Warp", name)
	}

	for _, id := range []ID{ITerm2, TerminalApp} {
		ex := &fakeExecutor{stdout: "found\n"}

		require.NoError(t, DefaultRegistry(ex).Focus(t.Context(), id, Tab{TTY: testTTY, PID: 4100, Link: warpTestLink}))
		assert.Equal(t, "osascript", ex.name, "%s finds the tab by its tty, whatever link it carries", id)
		assert.NotContains(t, strings.Join(ex.args, " "), warpTestLink, id)
	}
}

func TestWarpDriverWithoutAFocusLink(t *testing.T) {
	t.Parallel()

	hex := strings.TrimPrefix(warpTestLink, "warp://session/")
	vars := map[int]map[string]string{
		4100: {"TERM_PROGRAM": "WarpTerminal"},
		4101: {"WARP_FOCUS_URL": "warp://action/new_tab?path=/tmp/launch/pr-7.command"},
		4102: {"WARP_FOCUS_URL": "warp://session/" + strings.ToUpper(hex)},
		4103: {"WARP_FOCUS_URL": "warp://session/" + hex[:16]},
		4104: {"WARP_FOCUS_URL": warpTestLink + "?path=/tmp"},
		4105: {"WARP_FOCUS_URL": ""},
	}

	for name, pid := range map[string]int{
		"no link, as an older Warp's or another terminal's session": 4100,
		"an action":                     4101,
		"uppercase hex":                 4102,
		"short hex":                     4103,
		"an extra query":                4104,
		"an empty link":                 4105,
		"a process whose env is hidden": 4200,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ex := &fakeExecutor{}
			r, _ := warpFocusing(ex, vars)

			err := r.Focus(t.Context(), Warp, Tab{TTY: testTTY, PID: pid})
			require.ErrorIs(t, err, ErrNoFocusLink)
			assert.NotContains(t, err.Error(), "path=", "the error never repeats a value that isn't a link")
			assert.Zero(t, ex.calls, "nothing is opened in Warp")
		})
	}
}

func TestWarpDriverRefusesAnInvalidPID(t *testing.T) {
	t.Parallel()

	for _, pid := range []int{0, -1, procs.MaxPID + 1} {
		ex := &fakeExecutor{}
		r, env := warpFocusing(ex, map[int]map[string]string{pid: {"WARP_FOCUS_URL": warpTestLink}})

		require.ErrorIs(t, r.Focus(t.Context(), Warp, Tab{TTY: testTTY, PID: pid}), ErrNoFocusLink, pid)
		assert.Empty(t, env.asked, "no environment is read for pid %d", pid)
		assert.Zero(t, ex.calls, "nothing is opened in Warp for pid %d", pid)
	}
}

func TestWarpDriverReportsAFailedFocus(t *testing.T) {
	t.Parallel()

	ex := &fakeExecutor{
		stderr: "LSCopyApplicationURLsForBundleIdentifier() failed while trying to determine the application " +
			"with bundle identifier dev.warp.Warp-Stable.\n",
		err: &exec.ExitError{},
	}
	r, _ := warpFocusing(ex, map[int]map[string]string{4100: {"WARP_FOCUS_URL": warpTestLink}})

	err := r.Focus(t.Context(), Warp, Tab{PID: 4100})
	require.ErrorIs(t, err, ErrTerminalOpen)
	require.NotErrorIs(t, err, ErrNoFocusLink)
	assert.Contains(t, err.Error(), "Warp: LSCopyApplicationURLsForBundleIdentifier() failed")
}

func TestDefaultRegistryReadsWarpsLinkFromTheProcess(t *testing.T) {
	t.Parallel()

	d, err := DefaultRegistry(&fakeExecutor{}).Get(Warp)
	require.NoError(t, err)

	w, ok := d.(warpDriver)
	require.True(t, ok)
	require.NotNil(t, w.env, "Warp's driver reads a process's environment with procs.EnvOf")

	_, found := w.env(0, warpFocusEnv)
	assert.False(t, found, "no process, no link")
}
