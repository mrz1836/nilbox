//go:build mage

package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/brandgen"
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

func TestDevAppArgsFailures(t *testing.T) {
	t.Parallel()

	t.Run("the user's data can't be found", func(t *testing.T) {
		t.Parallel()

		_, err := newRig(t).runner().devAppArgs(true, func() (appdata.Paths, error) { return appdata.Paths{}, appdata.ErrNoHomeDir })

		require.ErrorIs(t, err, appdata.ErrNoHomeDir)
		assert.Contains(t, err.Error(), "find the app's data")
	})

	t.Run("no temporary folder for the sandbox", func(t *testing.T) {
		t.Parallel()

		r := newRig(t).runner()
		r.tmp = filepath.Join(t.TempDir(), "missing")

		_, err := r.devAppArgs(true, func() (appdata.Paths, error) { return appdata.NewPaths(t.TempDir()), nil })

		require.Error(t, err)
		assert.Contains(t, err.Error(), "make the sandbox")
	})
}

// builtRig returns a rig whose checkout holds the bundle built from source, with a pgrep
// that finds nothing running and an open that opens whatever it is given.
func builtRig(t *testing.T) *rig {
	t.Helper()

	g := openRig(t)
	testutil.WriteFile(t, bundleExecutable(g.path(devBundle)), "binary")

	return g
}

func TestReset(t *testing.T) {
	t.Parallel()

	t.Run("asks for the reset, then opens the bundle built from source", func(t *testing.T) {
		t.Parallel()

		g := builtRig(t)
		paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))

		require.NoError(t, g.runner().reset(paths))

		_, found, err := appdata.ReadResetRequest(paths.ResetRequest)
		require.NoError(t, err)
		assert.True(t, found)
		assert.Equal(t, []string{g.path(devBundle)}, g.linesOf("open"))
		assert.Equal(t, []string{"-f ^" + regexp.QuoteMeta(bundleExecutable(g.path(devBundle))) + "( |$)"}, g.linesOf("pgrep"),
			"it looks for that build running, by its executable's absolute path")
	})

	t.Run("builds the bundle first when it is missing", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		checkout(t, g.dir)
		appTools(g)

		paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))

		require.NoError(t, g.runner().reset(paths))

		lines := g.lines()
		assert.Contains(t, lines, "codesign --force --sign - "+g.path(devBundle))
		assert.Equal(t, "open "+g.path(devBundle), lines[len(lines)-1])
	})

	t.Run("asks nothing when the bundle can't be built", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		appTools(g)

		paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))

		require.ErrorIs(t, g.runner().reset(paths), brandgen.ErrStale)
		assert.NoFileExists(t, paths.ResetRequest)
	})
}

func TestFresh(t *testing.T) {
	t.Parallel()

	t.Run("opens the bundle built from source on a sandbox in the temporary folder", func(t *testing.T) {
		t.Parallel()

		g := builtRig(t)
		r := g.runner()
		paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))

		require.NoError(t, r.fresh(paths))

		made, err := filepath.Glob(filepath.Join(r.tmp, freshPrefix+"*"))
		require.NoError(t, err)
		require.Len(t, made, 1)
		assert.Equal(t, []string{"-n " + g.path(devBundle) + " --args --data-dir=" + made[0]}, g.linesOf("open"))
	})

	t.Run("makes no sandbox when the bundle can't be built", func(t *testing.T) {
		t.Parallel()

		g := newRig(t)
		appTools(g)
		r := g.runner()

		require.ErrorIs(t, r.fresh(appdata.NewPaths(t.TempDir())), brandgen.ErrStale)

		made, err := filepath.Glob(filepath.Join(r.tmp, freshPrefix+"*"))
		require.NoError(t, err)
		assert.Empty(t, made)
	})
}

func TestResetAppFailures(t *testing.T) {
	t.Parallel()

	t.Run("asks nothing when it can't tell whether the app runs", func(t *testing.T) {
		t.Parallel()

		g := openRig(t)
		g.tool("pgrep", exits(2))

		paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))

		require.ErrorContains(t, g.runner().resetApp(paths, "bin/Example.app", exampleExe, time.Second), "find the running app")
		assert.NoFileExists(t, paths.ResetRequest)
	})

	t.Run("opens nothing when the request can't be written", func(t *testing.T) {
		t.Parallel()

		g := openRig(t)
		file := filepath.Join(t.TempDir(), "file")
		testutil.WriteFile(t, file, "")

		err := g.runner().resetApp(appdata.NewPaths(filepath.Join(file, "data")), "bin/Example.app", exampleExe, time.Second)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "write the reset request")
		assert.Empty(t, g.linesOf("open"))
	})

	t.Run("says the reset still happens when the app can't be opened", func(t *testing.T) {
		t.Parallel()

		g := openRig(t)
		g.fixed("open", testutil.FakeRun{Stdout: "LSOpenURLsWithRole() failed\n", Exit: 1})

		paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))

		err := g.runner().resetApp(paths, "bin/Example.app", exampleExe, time.Second)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "the reset is asked for, and the next start carries it out: open bin/Example.app")
		assert.Contains(t, err.Error(), "LSOpenURLsWithRole() failed")
		assert.FileExists(t, paths.ResetRequest, "the next start carries it out")
	})

	t.Run("opens the app again once the running copy quit", func(t *testing.T) {
		t.Parallel()

		pid := startSleeper(t)

		synctest.Test(t, func(t *testing.T) {
			g := openRig(t)
			looks, opens := 0, 0

			g.tool("pgrep", func([]string) testutil.FakeRun {
				looks++
				if looks == 1 {
					return testutil.FakeRun{Stdout: strconv.Itoa(pid) + "\n"}
				}

				return testutil.FakeRun{Exit: 1}
			})
			g.tool("open", func([]string) testutil.FakeRun {
				opens++
				if opens == 1 {
					return testutil.FakeRun{Stdout: "procNotFound (-600)\n", Exit: 1} // the old copy is still listed
				}

				return testutil.FakeRun{}
			})

			paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))

			require.NoError(t, g.runner().resetApp(paths, "bin/Example.app", exampleExe, time.Second))

			assert.Equal(t, []string{"bin/Example.app", "bin/Example.app"}, g.linesOf("open"), "it tries again")
		})

		requireExited(t, pid)
	})
}

func TestOpenFreshFailures(t *testing.T) {
	t.Parallel()

	t.Run("makes no sandbox when it can't tell whether the app runs", func(t *testing.T) {
		t.Parallel()

		g := openRig(t)
		g.tool("pgrep", exits(2))

		tmp := t.TempDir()

		require.ErrorContains(t, g.runner().openFresh(appdata.NewPaths(t.TempDir()), tmp, "bin/Example.app", exampleExe, time.Second),
			"find the running app")

		entries, err := os.ReadDir(tmp)
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("opens nothing without a sandbox", func(t *testing.T) {
		t.Parallel()

		g := openRig(t)

		err := g.runner().openFresh(appdata.NewPaths(t.TempDir()), filepath.Join(t.TempDir(), "missing"), "bin/Example.app",
			exampleExe, time.Second)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "make the sandbox")
		assert.Empty(t, g.linesOf("open"))
	})

	t.Run("the app can't be opened", func(t *testing.T) {
		t.Parallel()

		g := openRig(t)
		g.tool("open", exits(1))

		err := g.runner().openFresh(appdata.NewPaths(t.TempDir()), t.TempDir(), "bin/Example.app", exampleExe, time.Second)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "open -n bin/Example.app --args --data-dir=")
		assert.NotContains(t, g.out.String(), "is open on fresh data")
	})
}

func TestFreshDataDirFailures(t *testing.T) {
	t.Parallel()

	t.Run("a temporary folder whose name is no pattern", func(t *testing.T) {
		t.Parallel()

		tmp := filepath.Join(t.TempDir(), "a[b")
		mkdir(t, tmp)

		_, err := freshDataDir(appdata.NewPaths(t.TempDir()), tmp)

		require.ErrorIs(t, err, filepath.ErrBadPattern)
		assert.Contains(t, err.Error(), "find the earlier sandboxes")
	})

	t.Run("an earlier sandbox that can't be removed", func(t *testing.T) {
		t.Parallel()

		skipAsRoot(t)

		tmp := t.TempDir()
		testutil.WriteFile(t, filepath.Join(tmp, freshPrefix+"earlier", "data.db"), "data")
		require.NoError(t, os.Chmod(filepath.Join(tmp, freshPrefix+"earlier"), 0o500))       //nolint:gosec // a folder nothing can be removed from is the fixture
		t.Cleanup(func() { _ = os.Chmod(filepath.Join(tmp, freshPrefix+"earlier"), 0o700) }) //nolint:gosec // lets the test's cleanup remove it

		_, err := freshDataDir(appdata.NewPaths(t.TempDir()), tmp)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "remove an earlier sandbox")
	})

	t.Run("a kept copy that can't be read", func(t *testing.T) {
		t.Parallel()

		skipAsRoot(t)

		paths := appdata.NewPaths(t.TempDir())
		testutil.WriteFile(t, paths.Prompts, "prompts")
		require.NoError(t, os.Chmod(paths.Prompts, 0o000))

		_, err := freshDataDir(paths, t.TempDir())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "copy "+filepath.Base(paths.Prompts)+" into the sandbox")
	})

	t.Run("a kept copy that is a folder", func(t *testing.T) {
		t.Parallel()

		paths := appdata.NewPaths(t.TempDir())
		mkdir(t, paths.LaunchDefaults)

		_, err := freshDataDir(paths, t.TempDir())

		require.Error(t, err)
		assert.Contains(t, err.Error(), "copy "+filepath.Base(paths.LaunchDefaults)+" into the sandbox")
	})
}

func TestCopyKept(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	from := filepath.Join(dir, "prompts.json")
	to := filepath.Join(dir, "copy.json")

	require.NoError(t, copyKept(filepath.Join(dir, "missing"), to), "a copy the user doesn't have is nothing to copy")
	assert.NoFileExists(t, to)

	testutil.WriteFile(t, from, "prompts")
	require.NoError(t, copyKept(from, to))
	assert.Equal(t, "prompts", readFile(t, to))

	err := copyKept(from, to)
	require.ErrorIs(t, err, os.ErrExist, "a file in the sandbox already is never overwritten")
}
