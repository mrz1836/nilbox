//go:build mage

package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// exampleExe is the executable of the bundle the reset and fresh tests open.
const exampleExe = "/Applications/Example.app/Contents/MacOS/example"

// openRig returns a rig with a pgrep that finds nothing running and an open that opens
// whatever it is given.
func openRig(t *testing.T) *rig {
	t.Helper()

	g := newRig(t)
	g.tool("pgrep", exits(1))
	g.fixed("open", testutil.FakeRun{})

	return g
}

func TestResetAppAsksForTheResetAndOpensTheApp(t *testing.T) {
	t.Parallel()

	g := openRig(t)
	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))

	require.NoError(t, g.runner().resetApp(paths, "bin/Example.app", exampleExe, time.Second))

	req, found, err := appdata.ReadResetRequest(paths.ResetRequest)
	require.NoError(t, err)
	assert.True(t, found, "the next start carries the reset out")
	assert.Equal(t, appdata.ResetByMagex, req.By)

	assert.Equal(t, []string{"bin/Example.app"}, g.linesOf("open"))
	assert.Contains(t, g.out.String(), "keeps your prompts, launch defaults, and your own scripts")
	assert.Contains(t, g.out.String(), ".reset-<stamp> in "+paths.Root)
}

func TestResetAppAsksNothingWhileTheAppKeepsRunning(t *testing.T) {
	t.Parallel()

	pid := startSleeper(t)

	synctest.Test(t, func(t *testing.T) {
		g := openRig(t)
		g.fixed("pgrep", testutil.FakeRun{Stdout: strconv.Itoa(pid) + "\n"})

		paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))

		err := g.runner().resetApp(paths, "bin/Example.app", exampleExe, 50*time.Millisecond)

		require.ErrorIs(t, err, errStillRunning)
		assert.NoFileExists(t, paths.ResetRequest, "a copy that holds the data never meets the request")
		assert.Empty(t, g.linesOf("open"), "nothing is opened")
	})
}

func TestOpenFreshOpensTheAppOnASandbox(t *testing.T) {
	t.Parallel()

	g := openRig(t)

	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))
	testutil.WriteFile(t, paths.Prompts, `{"prompts":[]}`)

	tmp := t.TempDir()
	earlier := filepath.Join(tmp, freshPrefix+"earlier")
	testutil.WriteFile(t, filepath.Join(earlier, brand.Slug+".db"), "an earlier sandbox's data")

	require.NoError(t, g.runner().openFresh(paths, tmp, "bin/Example.app", exampleExe, time.Second))

	assert.NoDirExists(t, earlier, "earlier sandboxes are removed")

	made, err := filepath.Glob(filepath.Join(tmp, freshPrefix+"*"))
	require.NoError(t, err)
	require.Len(t, made, 1)

	sandbox := appdata.NewPaths(made[0])
	assert.JSONEq(t, `{"prompts":[]}`, readFile(t, sandbox.Prompts), "the sandbox starts with the user's prompts")
	assert.NoFileExists(t, sandbox.LaunchDefaults, "a copy the user doesn't have is nothing to copy")
	assert.NoFileExists(t, sandbox.DB, "the app makes its own database")

	assert.Equal(t, []string{"-n bin/Example.app --args --data-dir=" + made[0]}, g.linesOf("open"))
	assert.Contains(t, g.out.String(), made[0])
	assert.Contains(t, g.out.String(), "magex app:run")
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
	t.Parallel()

	g := newRig(t)
	r := g.runner()

	args, err := r.devAppArgs(false, func() (appdata.Paths, error) {
		t.Error("the user's own data needs no folder of its own")

		return appdata.Paths{}, nil
	})
	require.NoError(t, err)
	assert.Empty(t, args, "the user's own data")

	data := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))

	args, err = r.devAppArgs(true, func() (appdata.Paths, error) { return data, nil })
	require.NoError(t, err)
	require.Len(t, args, 1)

	dir, ok := strings.CutPrefix(args[0], appdata.DataDirFlag+"=")
	require.True(t, ok, args[0])
	assert.DirExists(t, dir)
	assert.Equal(t, r.tmp, filepath.Dir(dir), "the sandbox is made in the temporary folder")
	assert.True(t, strings.HasPrefix(filepath.Base(dir), freshPrefix))
	assert.Contains(t, g.out.String(), "Running on fresh data in "+dir)
}
