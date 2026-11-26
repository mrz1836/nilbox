//go:build darwin

package terminal

import (
	"os/exec"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// folderOpener returns the driver registered under id as a FolderOpener, and whether it is one.
func folderOpener(t *testing.T, ex *fakeExecutor, id ID) (FolderOpener, bool) {
	t.Helper()

	d, err := DefaultRegistry(ex).Get(id)
	require.NoError(t, err)

	f, ok := d.(FolderOpener)

	return f, ok
}

func TestOnlyWarpOpensAFolderItself(t *testing.T) {
	t.Parallel()

	for id, want := range map[ID]bool{Warp: true, ITerm2: false, TerminalApp: false} {
		_, ok := folderOpener(t, &fakeExecutor{}, id)
		assert.Equal(t, want, ok, "%s: iTerm2 and Terminal.app open a folder through a script", id)
	}
}

func TestWarpDriverOpensAFolderByBundleID(t *testing.T) {
	t.Parallel()

	ex := &fakeExecutor{}

	f, ok := folderOpener(t, ex, Warp)
	require.True(t, ok)
	require.NoError(t, f.OpenFolder(t.Context(), awkwardFolder))

	assert.Equal(t, 1, ex.calls)
	assert.Equal(t, "open", ex.name)
	assert.Equal(t, []string{"-b", "dev.warp.Warp-Stable", awkwardFolder}, ex.args,
		"the folder itself, so Warp's own shell starts there: no script, and nothing run in it")
	assert.Greater(t, ex.deadline, time.Duration(0))
	assert.LessOrEqual(t, ex.deadline, openTimeout)
}

func TestWarpDriverReportsAFolderItCouldNotOpen(t *testing.T) {
	t.Parallel()

	ex := &fakeExecutor{
		stderr: "LSCopyApplicationURLsForBundleIdentifier() failed while trying to determine the application " +
			"with bundle identifier dev.warp.Warp-Stable.\n",
		err: &exec.ExitError{},
	}

	f, ok := folderOpener(t, ex, Warp)
	require.True(t, ok)

	err := f.OpenFolder(t.Context(), awkwardFolder)
	require.ErrorIs(t, err, ErrTerminalOpen)
	require.NotErrorIs(t, err, ErrAutomationDenied)
	assert.Contains(t, err.Error(), "Warp: LSCopyApplicationURLsForBundleIdentifier() failed")
}

func TestWarpDriverRefusesUnsafeFolders(t *testing.T) {
	t.Parallel()

	ex := &fakeExecutor{}

	f, ok := folderOpener(t, ex, Warp)
	require.True(t, ok)

	for _, dir := range []string{"", "work/example-repo", "-a Terminal", "/work/a\nb", "/work/a\x00b"} {
		require.ErrorIs(t, f.OpenFolder(t.Context(), dir), ErrInvalidFolder, "%q", dir)
	}

	assert.Zero(t, ex.calls, "nothing runs for an unsafe folder")
}
