package profiles

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// refuseRemoval makes every delete from the profile table fail.
const refuseRemoval = `CREATE TRIGGER refuse_removal BEFORE DELETE ON claude_profiles
BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`

// refuseSecond makes inserting a profile named "second" fail.
const refuseSecond = `CREATE TRIGGER refuse_second BEFORE INSERT ON claude_profiles WHEN NEW.name = 'second'
BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`

// openStore opens a fresh, migrated database and a Store over it.
func openStore(t *testing.T) (*Store, *sql.DB) {
	t.Helper()

	db := testutil.SQLDB(t)

	return NewStore(db, nil), db
}

// exec runs a statement the Store has no call for, such as an edit made by hand.
func exec(t *testing.T, db *sql.DB, query string) {
	t.Helper()

	_, err := db.ExecContext(t.Context(), query)
	require.NoError(t, err)
}

// stamps returns a stored profile's created_at and updated_at as the database holds them.
func stamps(t *testing.T, db *sql.DB, name string) (created, updated string) {
	t.Helper()

	require.NoError(t, db.QueryRowContext(t.Context(),
		"SELECT CAST(created_at AS TEXT), CAST(updated_at AS TEXT) FROM claude_profiles WHERE name = ?", name,
	).Scan(&created, &updated))

	return created, updated
}

func TestSyncStampsNewRowsFromTheClock(t *testing.T) {
	t.Parallel()

	db := testutil.SQLDB(t)
	clk := testutil.NewClock(time.Date(2026, 1, 10, 7, 0, 0, 0, time.FixedZone("UTC-5", -5*60*60)))
	s := NewStore(db, clk)

	require.NoError(t, s.Sync(t.Context(), []Profile{newDiscovered("work", "/opt/profiles/work")}))

	created, updated := stamps(t, db, "work")
	assert.Equal(t, "2026-01-10 12:00:00+00:00", created, "stored in UTC")
	assert.Equal(t, created, updated)

	// A later sync leaves the stored row exactly as it is, stamps included.
	clk.Advance(time.Hour)
	require.NoError(t, s.Sync(t.Context(), []Profile{newDiscovered("work", "/opt/profiles/elsewhere")}))

	got, err := s.Lookup(t.Context(), "work")
	require.NoError(t, err)
	assert.Equal(t, "/opt/profiles/work", got.ConfigDir)

	created, updated = stamps(t, db, "work")
	assert.Equal(t, "2026-01-10 12:00:00+00:00", created)
	assert.Equal(t, created, updated)
}

func TestSyncStoresAllOrNone(t *testing.T) {
	t.Parallel()

	s, db := openStore(t)
	exec(t, db, refuseSecond)

	err := s.Sync(t.Context(), []Profile{newDiscovered("first", "/opt/profiles/first"), newDiscovered("second", "")})
	require.ErrorContains(t, err, "store discovered profiles")
	require.ErrorContains(t, err, "refused by the test")

	all, err := s.All(t.Context())
	require.NoError(t, err)
	assert.Empty(t, all, "the profile stored before the failure is rolled back")
}

func TestSyncStoresEmptyListsForNilOnes(t *testing.T) {
	t.Parallel()

	s, db := openStore(t)
	require.NoError(t, s.Sync(t.Context(), []Profile{{Name: "bare"}}))

	var addDirs, extraArgs, env string
	require.NoError(t, db.QueryRowContext(t.Context(),
		"SELECT add_dirs, extra_args, env FROM claude_profiles WHERE name = 'bare'").Scan(&addDirs, &extraArgs, &env))
	assert.Equal(t, []string{"[]", "[]", "{}"}, []string{addDirs, extraArgs, env})

	got, err := s.Lookup(t.Context(), "bare")
	require.NoError(t, err)
	assert.Equal(t, Profile{Name: "bare", AddDirs: []string{}, ExtraArgs: []string{}, Env: map[string]string{}}, got)
}

func TestStoreRoundTripsEverySetting(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)
	want := Profile{
		Name:           "work",
		ConfigDir:      "/opt/profiles/work",
		Model:          "opus",
		Effort:         "high",
		PermissionMode: "acceptEdits",
		PlanMode:       true,
		AddDirs:        []string{"~/projects/shared", "/srv/docs"},
		ExtraArgs:      []string{"--verbose", "--append-system-prompt", "be brief"},
		Env:            map[string]string{"EXAMPLE_FLAG": "1", "OTHER": "two words"},
	}
	require.NoError(t, s.Sync(t.Context(), []Profile{want}))

	got, err := s.Lookup(t.Context(), "work")
	require.NoError(t, err)
	assert.Equal(t, want, got)

	all, err := s.All(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []Profile{want}, all)
}

func TestReadsReportARowTheyCannotDecode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		update string
		want   string
	}{
		{name: "extra directories", update: `UPDATE claude_profiles SET add_dirs = '[1]'`, want: "extra directories"},
		{name: "extra arguments", update: `UPDATE claude_profiles SET extra_args = '[{}]'`, want: "extra arguments"},
		{name: "environment", update: `UPDATE claude_profiles SET env = '{"EXAMPLE_FLAG": 1}'`, want: "environment"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, db := openStore(t)
			require.NoError(t, s.Sync(t.Context(), []Profile{newDiscovered("stray", "/opt/profiles/stray")}))
			exec(t, db, tt.update)

			_, err := s.All(t.Context())
			require.ErrorContains(t, err, "list profiles")
			require.ErrorContains(t, err, tt.want)

			_, err = s.Lookup(t.Context(), "stray")
			require.ErrorContains(t, err, "look up profile")
			require.ErrorContains(t, err, tt.want)
			require.NotErrorIs(t, err, ErrProfileNotFound)

			_, err = s.Prune(t.Context())
			require.ErrorContains(t, err, "read profiles to prune")
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestPruneWithNothingStored(t *testing.T) {
	t.Parallel()

	s, _ := openStore(t)

	removed, err := s.Prune(t.Context())
	require.NoError(t, err)
	assert.Nil(t, removed)
}
