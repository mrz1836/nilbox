package store_test

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// resetSuffix is what a database OpenFresh set aside at setAsideAt has added to its name.
const resetSuffix = ".reset-20260102T030405Z"

// noWait is a wait between OpenFresh's checks that never waits, and fails the test when
// it is called.
func noWait(t *testing.T) func(context.Context) error {
	t.Helper()

	return func(context.Context) error {
		t.Error("OpenFresh waited, with no other connection holding the database")

		return nil
	}
}

// resetFiles lists the files OpenFresh set aside next to the database at path.
func resetFiles(t *testing.T, path string) []string {
	t.Helper()

	found, err := filepath.Glob(path + ".reset-*")
	require.NoError(t, err)

	return found
}

// heldOpen opens the database at path in a pool of its own, which reads it, as another
// copy of the app does, and closes it when the test ends.
func heldOpen(t *testing.T, path string) *sql.DB {
	t.Helper()

	db := rawDB(t, path)

	var prefs int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM prefs").Scan(&prefs))

	return db
}

func TestOpenFreshSetsTheDatabaseAsideAsAReset(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	storePref(t, path)

	opened, err := store.OpenFreshWith(t.Context(), path, setAsideAt, 0, noWait(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = opened.DB.Close() })

	require.NotNil(t, opened.Reset)
	assert.Equal(t, store.Reset{Path: path + resetSuffix, Reason: store.ResetRequested}, *opened.Reset,
		"a reset the user asked for is no error")
	assertFresh(t, opened.DB)
	assertPrefKept(t, opened.Reset.Path)
	assert.Empty(t, setAsideFiles(t, path), "a reset is not named as a database that could not be used")
}

func TestOpenFreshWithNoDatabaseOpensOne(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "data", "app.db")

	opened, err := store.OpenFreshWith(t.Context(), path, setAsideAt, 0, noWait(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = opened.DB.Close() })

	assert.Nil(t, opened.Reset, "there was nothing to set aside")
	assertFresh(t, opened.DB)
	assert.Empty(t, resetFiles(t, path), "no empty file is set aside as a reset")
}

func TestOpenFreshSetsAsideADatabaseItCouldNotUseToo(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	garbage(t, path)

	opened, err := store.OpenFreshWith(t.Context(), path, setAsideAt, 0, noWait(t))
	require.NoError(t, err)
	t.Cleanup(func() { _ = opened.DB.Close() })

	require.NotNil(t, opened.Reset)
	assert.Equal(t, store.ResetRequested, opened.Reset.Reason)
	assertGarbage(t, path+resetSuffix)
	assertFresh(t, opened.DB)
}

func TestOpenFreshWaitsForAnotherConnectionToLetGo(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	storePref(t, path)

	other := heldOpen(t, path)
	waits := 0

	// The other copy of the app closes the database while OpenFresh waits.
	opened, err := store.OpenFreshWith(t.Context(), path, setAsideAt, 3, func(context.Context) error {
		waits++

		return other.Close()
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = opened.DB.Close() })

	assert.Equal(t, 1, waits, "it checked again once the other connection let go")
	require.NotNil(t, opened.Reset)
	assertPrefKept(t, opened.Reset.Path)
	assertFresh(t, opened.DB)
}

func TestOpenFreshLeavesADatabaseAnotherConnectionKeepsOpen(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	storePref(t, path)
	heldOpen(t, path)

	waits := 0

	_, err := store.OpenFreshWith(t.Context(), path, setAsideAt, 2, func(context.Context) error {
		waits++

		return nil
	})
	require.Error(t, err)
	assert.True(t, store.IsBusy(err), "the startup dialog says another copy may be running: %v", err)
	assert.Equal(t, 1, waits, "it waits between its checks, never after the last")
	assert.Empty(t, resetFiles(t, path), "a database in use is never moved")
	assertPrefKept(t, path)
}

func TestOpenFreshStopsWaitingWhenItsContextEnds(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	storePref(t, path)
	heldOpen(t, path)

	ctx, cancel := context.WithCancel(t.Context())

	_, err := store.OpenFreshWith(ctx, path, setAsideAt, 5, func(ctx context.Context) error {
		cancel()

		return ctx.Err()
	})
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, resetFiles(t, path))
}

func TestOpenFreshRefusesAPathItCannotOpen(t *testing.T) {
	t.Parallel()

	_, err := store.OpenFresh(t.Context(), "")
	require.ErrorIs(t, err, store.ErrEmptyPath)

	_, err = store.OpenFresh(t.Context(), filepath.Join(t.TempDir(), "app.db?mode=ro"))
	require.ErrorIs(t, err, store.ErrInvalidPath)
}

func TestPruneResetCopiesKeepsTheNewest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")

	resets := []string{
		"app.db.reset-20261101T000000Z",
		"app.db.reset-20261102T000000Z",
		"app.db.reset-20261103T000000Z",
		"app.db.reset-20261104T000000Z",
		"app.db.reset-20261104T000000Z-1",
	}

	others := []string{
		"app.db",
		"app.db.broken-20261001T000000Z",
		"app.db.backup-20261001T000000Z-v1",
		"other.db.reset-20261001T000000Z",
	}

	for _, name := range append(append([]string{}, resets...), others...) {
		testutil.WriteFile(t, filepath.Join(dir, name), "x")
	}

	// The oldest copies' log files go with them.
	testutil.WriteFile(t, filepath.Join(dir, resets[0]+"-wal"), "x")
	testutil.WriteFile(t, filepath.Join(dir, resets[1]+"-shm"), "x")

	require.NoError(t, store.PruneResetCopies(path))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	left := make([]string, 0, len(entries))
	for _, e := range entries {
		left = append(left, e.Name())
	}

	assert.ElementsMatch(t, append(append([]string{}, resets[2:]...), others...), left,
		"the newest three are kept, and nothing else is touched")
	assert.Len(t, resetFiles(t, path), store.KeepResetCopies)
}

func TestPruneResetCopiesOfAMissingDirectoryFails(t *testing.T) {
	t.Parallel()

	require.Error(t, store.PruneResetCopies(filepath.Join(t.TempDir(), "missing", "app.db")))
}

func TestPruneResetCopiesNamesWhatItCantDeleteAndDeletesTheRest(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "app.db")

	resets := []string{
		"app.db.reset-20261101T000000Z",
		"app.db.reset-20261102T000000Z",
		"app.db.reset-20261103T000000Z",
		"app.db.reset-20261104T000000Z",
		"app.db.reset-20261105T000000Z",
	}

	for _, name := range resets {
		testutil.WriteFile(t, filepath.Join(dir, name), "x")
	}

	// The oldest copy's log is a folder with a file in it, which no remove deletes.
	stuck := filepath.Join(dir, resets[0]+"-wal")
	require.NoError(t, os.Mkdir(stuck, 0o700))
	testutil.WriteFile(t, filepath.Join(stuck, "inside"), "x")

	err := store.PruneResetCopies(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "delete an old database set aside by a reset")
	assert.Contains(t, err.Error(), filepath.Base(stuck), "the error names what it couldn't delete")

	assert.Equal(t, []string{
		stuck, filepath.Join(dir, resets[2]), filepath.Join(dir, resets[3]), filepath.Join(dir, resets[4]),
	}, resetFiles(t, path), "the old copies are still deleted, all but the log it couldn't, and the newest kept")
}

func TestOpenFreshFailsWhereItCantLookForTheDatabase(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "data")
	testutil.WriteFile(t, file, "x")

	_, err := store.OpenFresh(t.Context(), filepath.Join(file, "app.db"))
	require.Error(t, err, "a file in the folder's place")
	assert.Contains(t, err.Error(), "start a fresh database")
	assert.Equal(t, "x", string(readFile(t, file)), "the file is left as it was")
}
