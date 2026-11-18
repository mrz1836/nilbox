package desktop_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/update"
)

// errNewerData is the error a database a newer build saved stops the start with.
var errNewerData = fmt.Errorf("open the database: %w", store.ErrNewerSchema)

// backupAt is a backup of schema taken at at, as store.Snapshots lists one.
func backupAt(name string, at time.Time, schema int) store.SnapshotFile {
	return store.SnapshotFile{Path: "/data/app.db.backup-" + name, Kind: store.SnapshotBackup, At: at, Schema: schema}
}

func TestRestoreOfferPicksTheNewestBackupThisBuildOpens(t *testing.T) {
	t.Parallel()

	zone := time.FixedZone("UTC-5", -5*60*60)
	now := time.Date(2026, time.November, 15, 9, 0, 0, 0, zone)
	nov14 := time.Date(2026, time.November, 14, 20, 2, 0, 0, time.UTC) // 3:02 PM in zone
	nov13 := nov14.Add(-24 * time.Hour)
	nov15 := nov14.Add(12 * time.Hour)

	tests := []struct {
		name    string
		err     error
		backups []store.SnapshotFile
		want    string // the backup offered, or "" for none
	}{
		{
			name: "the newest backup of a schema this build has",
			err:  errNewerData,
			backups: []store.SnapshotFile{
				backupAt("newest-but-newer", nov15, 5),
				backupAt("offered", nov14, 4),
				backupAt("older", nov13, 3),
			},
			want: "/data/app.db.backup-offered",
		},
		{
			name: "a set-aside or reset database is never offered",
			err:  errNewerData,
			backups: []store.SnapshotFile{
				{Path: "/data/app.db.broken-x", Kind: store.SnapshotSetAside, At: nov15},
				{Path: "/data/app.db.reset-x", Kind: store.SnapshotReset, At: nov15},
				backupAt("offered", nov13, 2),
			},
			want: "/data/app.db.backup-offered",
		},
		{
			name:    "a backup whose name gives no schema is never offered",
			err:     errNewerData,
			backups: []store.SnapshotFile{backupAt("unknown", nov15, 0)},
		},
		{
			name:    "only backups a newer build took",
			err:     errNewerData,
			backups: []store.SnapshotFile{backupAt("newer", nov15, 5)},
		},
		{name: "no backups", err: errNewerData},
		{
			name:    "any other failure offers nothing",
			err:     errFakeStartup,
			backups: []store.SnapshotFile{backupAt("offered", nov14, 4)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := desktop.RestoreOffer(tt.err, tt.backups, 4, now)

			if tt.want == "" {
				assert.False(t, ok)
				assert.Equal(t, desktop.Restore{}, got)

				return
			}

			require.True(t, ok)
			assert.Equal(t, tt.want, got.Backup)
		})
	}

	t.Run("words the backup's time in the user's", func(t *testing.T) {
		t.Parallel()

		got, ok := desktop.RestoreOffer(errNewerData, []store.SnapshotFile{backupAt("offered", nov14, 4)}, 4, now)
		require.True(t, ok)

		assert.Equal(t, nov14, got.At)
		assert.Equal(t, "Use my data from Nov 14, 3:02 PM", got.Button)
		assert.Equal(t, "Or go back to your data as it was backed up on Nov 14 at 3:02 PM. What changed since then "+
			"won't be there: the newer data is kept beside it, and Restore from a backup on the Prompts page can "+
			"bring its prompts back.", got.Detail)
	})

	t.Run("names the year of a backup from another", func(t *testing.T) {
		t.Parallel()

		lastYear := nov14.AddDate(-1, 0, 0)

		got, ok := desktop.RestoreOffer(errNewerData, []store.SnapshotFile{backupAt("offered", lastYear, 4)}, 4, now)
		require.True(t, ok)
		assert.Equal(t, "Use my data from Nov 14, 2025, 3:02 PM", got.Button)
	})
}

func TestAFailureOfferingABackupSaysWhatItLoses(t *testing.T) {
	t.Parallel()

	r := desktop.Restore{Backup: "/data/app.db.backup-x", Button: "Use my data from Nov 14, 3:02 PM", Detail: "Or go back."}
	newer := desktop.Newer{Way: update.WayUpdate}
	alone := desktop.StartupFailure(errNewerData, newer)
	got := alone.Offering(r)

	assert.Equal(t, brand.Name+" needs an update", got.Title)
	assert.Equal(t, alone.Message+"\n\nOr go back.", got.Message)
	require.NotNil(t, got.Restore)
	assert.Equal(t, r, *got.Restore)
	assert.Nil(t, alone.Restore, "a failure offers nothing on its own")

	assert.Equal(t, []desktop.Button{
		{Label: "Use my data from Nov 14, 3:02 PM", Choice: desktop.ChoicePutBack},
		{Label: "Update and Reopen", Choice: desktop.ChoiceUpdate, Default: true},
		{Label: quitLabel, Choice: desktop.ChoiceQuit, Cancel: true},
	}, got.Buttons(), "Return updates, Escape quits, and no key puts the backup back")

	got = desktop.PutBackFailure("/data/app.db", errFakeStartup).Offering(r)
	assert.Equal(t, []desktop.Button{
		{Label: "Use my data from Nov 14, 3:02 PM", Choice: desktop.ChoicePutBack},
		{Label: quitLabel, Choice: desktop.ChoiceQuit, Cancel: true},
	}, got.Buttons(), "with no other way forward, no key puts the backup back")
}

// newerBuildSaved records in the database at path a migration this build doesn't have, as
// a newer build that migrated it would have.
func newerBuildSaved(t *testing.T, path string) {
	t.Helper()

	newest, err := store.LatestSchema()
	require.NoError(t, err)

	db := testutil.DBAt(t, path)
	testutil.Exec(t, db,
		`INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES (?, 'from_a_newer_build', 'x', ?)`,
		newest+1, "2026-11-15 00:00:00+00:00")
	require.NoError(t, db.Close(), "a newer build that quit holds nothing open")
}

func TestABackupPutBackOpensWhereTheNewerDataWouldNot(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), brand.Slug+".db")
	db := testutil.DBAt(t, path)

	backup, err := store.Backup(t.Context(), db, path)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	newerBuildSaved(t, path)

	_, err = store.Open(t.Context(), path)
	require.ErrorIs(t, err, store.ErrNewerSchema, "the fixture is data a newer build saved")

	r, ok, err := desktop.FindRestore(err, path, time.Now())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, backup, r.Backup)
	assert.True(t, strings.HasPrefix(r.Button, "Use my data from "), r.Button)

	aside, err := desktop.PutBack(t.Context(), path, r)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(aside, path+".broken-"), "the newer data is kept beside it: %s", aside)
	assert.FileExists(t, aside)
	assert.FileExists(t, backup, "the backup stays as it was")

	opened, err := store.Open(t.Context(), path)
	require.NoError(t, err, "the data put back opens")
	require.NoError(t, opened.DB.Close())
	assert.Nil(t, opened.Reset, "and is opened as it is, not set aside")
}

func TestFindRestoreOffersNothingWithoutTheData(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), brand.Slug+".db")
	db := testutil.DBAt(t, path)

	_, err := store.Backup(t.Context(), db, path)
	require.NoError(t, err)

	for name, tt := range map[string]struct {
		err error
		db  string
	}{
		"no database found":  {err: errNewerData, db: ""},
		"any other failure":  {err: errFakeStartup, db: path},
		"no backups beside":  {err: errNewerData, db: filepath.Join(t.TempDir(), brand.Slug+".db")},
		"a missing location": {err: errNewerData, db: filepath.Join(t.TempDir(), "missing", brand.Slug+".db")},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, ok, findErr := desktop.FindRestore(tt.err, tt.db, time.Now())
			require.NoError(t, findErr)
			assert.False(t, ok)
		})
	}
}

func TestAPutBackThatFailsSaysSoAndKeepsTheData(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, brand.Slug+".db")
	testutil.WriteFile(t, path, "the newer data")

	stray := filepath.Join(dir, "not-a-backup.db")
	testutil.WriteFile(t, stray, "x")

	_, err := desktop.PutBack(t.Context(), path, desktop.Restore{Backup: stray})
	require.ErrorIs(t, err, store.ErrNotABackup)

	got := desktop.PutBackFailure(path, err)
	assert.Equal(t, brand.Name+" couldn't put your data back", got.Title)
	assert.True(t, strings.HasPrefix(got.Message, "Your data couldn't be put back: "), got.Message)
	assert.True(t, strings.HasSuffix(got.Message, "\n\nUpdate "+brand.Name+" to open the newer data."), got.Message)
	assert.Nil(t, got.Restore)

	body, err := os.ReadFile(path) //nolint:gosec // the test's own file
	require.NoError(t, err)
	assert.Equal(t, "the newer data", string(body))
}

func TestAPutBackWhileAnotherCopyHoldsTheDataSaysToQuitIt(t *testing.T) {
	t.Parallel()

	err := lockedDatabaseError(t)
	require.True(t, store.IsBusy(err), "the fixture is a lock: %v", err)

	got := desktop.PutBackFailure(filepath.Join(t.TempDir(), brand.Slug+".db"), err)

	assert.Equal(t, "Another copy of "+brand.Name+" still has your data open, so nothing was changed. Quit it, "+
		"then open "+brand.Name+" again.", got.Message)
}

func TestAPutBackThatLeftNoDataSaysWhereTheNewerDataIs(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	db := filepath.Join(dir, brand.Slug+".db")
	testutil.WriteFile(t, filepath.Join(dir, brand.Slug+".db.broken-20261101T000000Z"), "an older incident")
	testutil.WriteFile(t, filepath.Join(dir, brand.Slug+".db.broken-20261115T120000Z"), "the newer data")

	got := desktop.PutBackFailure(db, errFakeStartup)

	assert.Equal(t, "Your newer data was set aside as "+brand.Slug+".db.broken-20261115T120000Z, and couldn't be put "+
		"back, so no data is in place. Your data is safe in that file, in "+dir+". Rename it to "+brand.Slug+
		".db, then open "+brand.Name+" again.\n\nCreate data directory: the disk is full.", got.Message)
}
