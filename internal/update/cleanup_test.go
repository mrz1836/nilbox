package update_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/update"
)

func TestCleanUpRemovesOnlyTheExactNames(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.November, 15, 12, 0, 0, 0, time.UTC)
	old := now.Add(-2 * time.Hour)

	apps := t.TempDir()
	bundle := filepath.Join(apps, brand.Name+".app")
	tmp := t.TempDir()

	dir := func(path string, entries ...string) string {
		require.NoError(t, os.MkdirAll(path, 0o700))

		for _, entry := range entries {
			testutil.WriteFile(t, filepath.Join(path, entry), "")
		}

		return path
	}

	file := func(path string, at time.Time) string {
		testutil.WriteFile(t, path, "")
		require.NoError(t, os.Chtimes(path, at, at))

		return path
	}

	// helperLog writes a helper log whose first line names target.
	helperLog := func(path, target string, at time.Time) string {
		testutil.WriteFile(t, path, "2026-11-15T09:00:00Z: helper start: target="+target+
			" new=/elsewhere/wails-update-1/X.app pid=1\n2026-11-15T09:00:04Z: helper done\n")
		require.NoError(t, os.Chtimes(path, at, at))

		return path
	}

	other := filepath.Join(apps, "Other.app")

	dir(filepath.Join(bundle, "Contents"))

	gone := []string{
		dir(bundle + ".bak"),
		dir(filepath.Join(apps, "wails-update-1"), brand.Name+".app"),
		dir(filepath.Join(apps, "wails-update-2"), brand.Name+"_0.3.0_darwin_universal.zip"),
		dir(filepath.Join(apps, "wails-update-3")),
		// The last install's, by the process its record names, at once.
		file(filepath.Join(tmp, "wails-update-4242.log"), now),
		file(filepath.Join(tmp, "wails-update-4242-1731672000000000000.ready"), now),
		// An earlier install of this app's, whose log names its bundle.
		helperLog(filepath.Join(tmp, "wails-update-77.log"), bundle, old),
		file(filepath.Join(tmp, "wails-update-77-5.ready"), old),
	}

	kept := []string{
		bundle,
		dir(bundle + ".bak2"),
		dir(filepath.Join(apps, "Other.app.bak")),
		dir(filepath.Join(apps, "wails-updated-notes")),
		dir(filepath.Join(apps, "wails-update-other"), "Other.app"),
		file(filepath.Join(apps, "wails-update-file"), old),
		dir(filepath.Join(apps, "wails-update-")),
		file(filepath.Join(tmp, "wails-update-77.log.bak"), old),
		file(filepath.Join(tmp, "wails-updated-77.log"), old),
		file(filepath.Join(tmp, "wails-update-notes.log"), old),
		// Another app's install, however old: its log names another bundle.
		helperLog(filepath.Join(tmp, "wails-update-90.log"), other, old),
		file(filepath.Join(tmp, "wails-update-90-1.ready"), old),
		// A log with no helper start, and a readiness file no log of this app's names.
		file(filepath.Join(tmp, "wails-update-91.log"), old),
		file(filepath.Join(tmp, "wails-update-92-1.ready"), old),
		// This app's, but too recent to be sure its helper is done.
		helperLog(filepath.Join(tmp, "wails-update-78.log"), bundle, now.Add(-time.Minute)),
		file(filepath.Join(tmp, "wails-update-78-9.ready"), now.Add(-time.Minute)),
		dir(filepath.Join(tmp, "wails-update-79.log")),
	}

	assert.Empty(t, update.CleanUp(bundle, tmp, 4242, now))

	for _, path := range gone {
		assert.NoFileExists(t, path)
		assert.NoDirExists(t, path)
	}

	for _, path := range kept {
		_, err := os.Lstat(path)
		assert.NoError(t, err, "%s is left alone", path)
	}
}

func TestCleanUpOutsideABundleCleansTheTemporaryFolderAlone(t *testing.T) {
	t.Parallel()

	tmp := t.TempDir()
	log := filepath.Join(tmp, "wails-update-5.log")
	stray := filepath.Join(tmp, "wails-update-6.log")

	testutil.WriteFile(t, log, "")
	testutil.WriteFile(t, stray, "helper start: target=/Applications/"+brand.Name+".app new=/x pid=6\n")
	require.NoError(t, os.Chtimes(stray, time.Unix(0, 0), time.Unix(0, 0)))

	assert.Empty(t, update.CleanUp("", tmp, 5, time.Now()))
	assert.NoFileExists(t, log, "the last install's log")
	assert.FileExists(t, stray, "with no bundle, no other log can be told to be this app's")

	errs := update.CleanUp("", filepath.Join(tmp, "missing"), 5, time.Now())
	assert.Len(t, errs, 1, "a temporary folder it can't read is told")
}

func TestCleanUpSaysWhereItCouldntLook(t *testing.T) {
	t.Parallel()

	missing := filepath.Join(t.TempDir(), "missing")

	errs := update.CleanUp(filepath.Join(missing, "Applications", brand.Name+".app"), filepath.Join(missing, "tmp"), 5,
		time.Now())
	require.Len(t, errs, 2)
	require.ErrorContains(t, errs[0], "look beside the app for an update's leftovers")
	require.ErrorContains(t, errs[1], "look for an update's leftovers in the temporary folder")
}

func TestCleanUpLeavesWhatItCantReadAndSaysWhatItCantRemove(t *testing.T) {
	t.Parallel()
	testutil.SkipAsRoot(t)

	now := time.Date(2026, time.November, 15, 12, 0, 0, 0, time.UTC)
	old := now.Add(-2 * time.Hour)
	apps := t.TempDir()
	bundle := filepath.Join(apps, brand.Name+".app")
	tmp := t.TempDir()

	require.NoError(t, os.MkdirAll(filepath.Join(bundle, "Contents"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(bundle+".bak", "Contents"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(apps, "wails-update-1", brand.Name+".app"), 0o700))

	// A staging folder it can't look in, which may be another app's, and a helper log it
	// can't read, which may name another app's bundle.
	unreadable := filepath.Join(apps, "wails-update-2")
	require.NoError(t, os.Mkdir(unreadable, 0o700))

	log := filepath.Join(tmp, "wails-update-77.log")
	testutil.WriteFile(t, log, "2026-11-15T09:00:00Z: helper start: target="+bundle+" new=/x pid=77\n")
	require.NoError(t, os.Chtimes(log, old, old))

	for _, path := range []string{unreadable, log} {
		require.NoError(t, os.Chmod(path, 0o000))
	}

	require.NoError(t, os.Chmod(apps, 0o500)) //nolint:gosec // the test's own folder, made read-only
	t.Cleanup(func() {
		_ = os.Chmod(apps, 0o700)       //nolint:gosec // put back for t.TempDir's removal
		_ = os.Chmod(unreadable, 0o700) //nolint:gosec // put back for t.TempDir's removal
	})

	errs := update.CleanUp(bundle, tmp, 4242, now)
	require.Len(t, errs, 2, "the staging folder and the copy of the old app it couldn't remove")

	for _, err := range errs {
		require.ErrorContains(t, err, "remove an update's leftover")
	}

	assert.DirExists(t, bundle+".bak")
	assert.DirExists(t, filepath.Join(apps, "wails-update-1"))
	assert.DirExists(t, unreadable, "what it can't look in is left alone")
	assert.FileExists(t, log, "and what it can't read")
}
