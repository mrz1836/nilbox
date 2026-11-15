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
