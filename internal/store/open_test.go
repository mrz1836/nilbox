package store_test

import (
	"database/sql"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	settingsmodel "github.com/mrz1836/nilbox/internal/settings/model"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/store/migrate"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// olderMigrations returns the embedded migrations without the newest, as an earlier build
// shipped them, and the newest version.
func olderMigrations(t *testing.T) (fs.FS, int) {
	t.Helper()

	all, err := migrate.Load(migrate.Embedded())
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(all), 2)

	older := fstest.MapFS{}

	for _, m := range all[:len(all)-1] {
		body, readErr := fs.ReadFile(migrate.Embedded(), m.File)
		require.NoError(t, readErr)

		older[m.File] = &fstest.MapFile{Data: body}
	}

	return older, all[len(all)-1].Version
}

// rawDB opens the database file at path with no migrations, closing it when the test
// ends.
func rawDB(t *testing.T, path string) *sql.DB {
	t.Helper()

	raw, err := sql.Open("sqlite", path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = raw.Close() })

	return raw
}

// backupsOf returns the pre-migration backups written next to the database at path.
func backupsOf(t *testing.T, path string) []string {
	t.Helper()

	found, err := filepath.Glob(path + ".backup-*")
	require.NoError(t, err)

	return found
}

func TestOpenUpgradesAnOlderDatabaseAfterABackup(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	older, newest := olderMigrations(t)

	raw := rawDB(t, path)
	_, err := migrate.Apply(t.Context(), raw, path, migrate.Options{Source: older})
	require.NoError(t, err)
	_, err = raw.ExecContext(t.Context(), `INSERT INTO prefs (key, value, updated_at)
VALUES ('example.key', 'kept', CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	require.NoError(t, raw.Close())

	db := testutil.DBAt(t, path)

	var version int
	require.NoError(t, db.Raw("SELECT max(version) FROM schema_migrations").Scan(&version).Error)
	assert.Equal(t, newest, version, "the pending migration is applied")

	var pref settingsmodel.Pref
	require.NoError(t, db.First(&pref, "key = ?", "example.key").Error)
	assert.Equal(t, "kept", pref.Value, "the data survives the upgrade")

	assert.Len(t, backupsOf(t, path), 1, "the database is backed up before it is changed")
}

func TestOpenRefusesADatabaseFromANewerBuild(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")

	db, err := store.Open(t.Context(), path)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `INSERT INTO schema_migrations (version, name, checksum, applied_at)
VALUES (9999, 'from_the_future', 'abc', CURRENT_TIMESTAMP)`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = store.Open(t.Context(), path)
	require.ErrorIs(t, err, migrate.ErrUnknownVersion)
	require.ErrorContains(t, err, "migrate database")
	assert.Empty(t, backupsOf(t, path))
}

func TestOpenRefusesAChangedMigration(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")

	db, err := store.Open(t.Context(), path)
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), "UPDATE schema_migrations SET checksum = 'edited' WHERE version = 1")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	_, err = store.Open(t.Context(), path)
	require.ErrorIs(t, err, migrate.ErrChecksumMismatch)
	require.ErrorContains(t, err, "migrate database")
}

func TestOpenRefusesAFileThatIsNotADatabase(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	garbage := []byte("this is not a database file, just some text that is long enough to have a header")
	testutil.WriteFile(t, path, string(garbage))

	_, err := store.Open(t.Context(), path)
	require.ErrorContains(t, err, "open database")

	body, readErr := os.ReadFile(path) //nolint:gosec // the test's own file
	require.NoError(t, readErr)
	assert.Equal(t, garbage, body, "the file is left as it was")
	assert.Empty(t, backupsOf(t, path))
}

func TestOpenKeepsAnExistingFilesPermissions(t *testing.T) {
	t.Parallel()

	testutil.SkipAsRoot(t)

	path := filepath.Join(t.TempDir(), "app.db")
	require.NoError(t, os.WriteFile(path, nil, 0o640)) //nolint:gosec // a mode the app would never choose

	testutil.DBAt(t, path)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o640), info.Mode().Perm(), "only a new file is created private")
}

func TestStampsAreUTC(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)

	pref := settingsmodel.Pref{Key: "example.key", Value: "v"}
	require.NoError(t, db.Create(&pref).Error)

	var stored settingsmodel.Pref
	require.NoError(t, db.First(&stored, "key = ?", "example.key").Error)
	assert.Equal(t, "UTC", stored.UpdatedAt.Location().String())
}
