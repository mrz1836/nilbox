//go:build mage

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// exampleExe is the executable of the bundle the reset and fresh tests open.
const exampleExe = "/Applications/Example.app/Contents/MacOS/example"

//nolint:paralleltest // changes the process's PATH and standard output
func TestResetAppAsksForTheResetAndOpensTheApp(t *testing.T) {
	dir := isolatedPath(t)
	fakeTool(t, dir, "pgrep", "exit 1")
	openLog := fakeLoggedTool(t, dir, "open", "exit 0")

	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))

	out := captureStdout(t, func() {
		require.NoError(t, resetApp(paths, "bin/Example.app", exampleExe, time.Second))
	})

	req, found, err := appdata.ReadResetRequest(paths.ResetRequest)
	require.NoError(t, err)
	assert.True(t, found, "the next start carries the reset out")
	assert.Equal(t, appdata.ResetByMagex, req.By)

	assert.Equal(t, []string{"bin/Example.app"}, logLines(t, openLog))
	assert.Contains(t, out, "keeps your prompts, launch defaults, and your own scripts")
	assert.Contains(t, out, ".reset-<stamp> in "+paths.Root)
}

func TestResetAppAsksNothingWhileTheAppKeepsRunning(t *testing.T) {
	t.Setenv("FAKE_PID", strconv.Itoa(startSleeper(t)))

	dir := isolatedPath(t)
	fakeTool(t, dir, "pgrep", `echo "$FAKE_PID"`)
	openLog := fakeLoggedTool(t, dir, "open", "exit 0")

	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))

	var err error

	captureStdout(t, func() { err = resetApp(paths, "bin/Example.app", exampleExe, 50*time.Millisecond) })

	require.ErrorIs(t, err, errStillRunning)
	assert.NoFileExists(t, paths.ResetRequest, "a copy that holds the data never meets the request")
	assert.NoFileExists(t, openLog, "nothing is opened")
}

//nolint:paralleltest // changes the process's PATH and standard output
func TestOpenFreshOpensTheAppOnASandbox(t *testing.T) {
	dir := isolatedPath(t)
	fakeTool(t, dir, "pgrep", "exit 1")
	openLog := fakeLoggedTool(t, dir, "open", "exit 0")

	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))
	testutil.WriteFile(t, paths.Prompts, `{"prompts":[]}`)

	tmp := t.TempDir()
	earlier := filepath.Join(tmp, freshPrefix+"earlier")
	testutil.WriteFile(t, filepath.Join(earlier, brand.Slug+".db"), "an earlier sandbox's data")

	out := captureStdout(t, func() {
		require.NoError(t, openFresh(paths, tmp, "bin/Example.app", exampleExe, time.Second))
	})

	assert.NoDirExists(t, earlier, "earlier sandboxes are removed")

	made, err := filepath.Glob(filepath.Join(tmp, freshPrefix+"*"))
	require.NoError(t, err)
	require.Len(t, made, 1)

	sandbox := appdata.NewPaths(made[0])
	assert.JSONEq(t, `{"prompts":[]}`, readFile(t, sandbox.Prompts), "the sandbox starts with the user's prompts")
	assert.NoFileExists(t, sandbox.LaunchDefaults, "a copy the user doesn't have is nothing to copy")
	assert.NoFileExists(t, sandbox.DB, "the app makes its own database")

	assert.Equal(t, []string{"-n bin/Example.app --args --data-dir=" + made[0]}, logLines(t, openLog))
	assert.Contains(t, out, made[0])
	assert.Contains(t, out, "magex app:run")
}

func TestFreshDataDirCopiesTheKeptFilesPrivately(t *testing.T) {
	t.Parallel()

	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))
	testutil.WriteFile(t, paths.Prompts, "prompts")
	testutil.WriteFile(t, paths.LaunchDefaults, "defaults")

	dir, err := freshDataDir(paths, t.TempDir())
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(filepath.Base(dir), freshPrefix))

	sandbox := appdata.NewPaths(dir)
	assert.Equal(t, "defaults", readFile(t, sandbox.LaunchDefaults))

	info, err := os.Stat(sandbox.Prompts)
	require.NoError(t, err)
	assert.Equal(t, appdata.PrivateFilePerm, info.Mode().Perm())
}

func TestDevAppArgs(t *testing.T) {
	args, err := devAppArgs(false)
	require.NoError(t, err)
	assert.Empty(t, args, "the user's own data")

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("TMPDIR", t.TempDir())

	captureStdout(t, func() { args, err = devAppArgs(true) })
	require.NoError(t, err)
	require.Len(t, args, 1)

	dir, ok := strings.CutPrefix(args[0], appdata.DataDirFlag+"=")
	require.True(t, ok, args[0])
	assert.DirExists(t, dir)
	assert.True(t, strings.HasPrefix(filepath.Base(dir), freshPrefix))
}
