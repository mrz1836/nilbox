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

	dir(filepath.Join(bundle, "Contents"))

	gone := []string{
		dir(bundle + ".bak"),
		dir(filepath.Join(apps, "wails-update-1"), brand.Name+".app"),
		dir(filepath.Join(apps, "wails-update-2"), brand.Name+"_0.3.0_darwin_universal.zip"),
		dir(filepath.Join(apps, "wails-update-3")),
		file(filepath.Join(tmp, "wails-update-4242.log"), now),
		file(filepath.Join(tmp, "wails-update-4242-1731672000000000000.ready"), now),
		file(filepath.Join(tmp, "wails-update-77.log"), old),
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
		file(filepath.Join(tmp, "wails-update-78.log"), now.Add(-time.Minute)),
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
	testutil.WriteFile(t, log, "")

	assert.Empty(t, update.CleanUp("", tmp, 5, time.Now()))
	assert.NoFileExists(t, log)

	errs := update.CleanUp("", filepath.Join(tmp, "missing"), 5, time.Now())
	assert.Len(t, errs, 1, "a temporary folder it can't read is told")
}
