package migrate_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store/migrate"
)

// oldBuild is the migrations an older build shipped, and newBuild the base a squash made
// of them, 0100, with a migration after it, as a later build ships them.
func oldBuild() fstest.MapFS { return firstN(2) }

func newBuild() fstest.MapFS {
	return source(map[string]string{"0100_base.sql": firstSQL + secondSQL, "0101_next.sql": thirdSQL})
}

// squashedOf freezes the history of the migrations in src as a squashed history carried
// over through version through, in a source of its own.
func squashedOf(t *testing.T, src fstest.MapFS, through int) fstest.MapFS {
	t.Helper()

	migrations, err := migrate.Load(src)
	require.NoError(t, err)

	h := migrate.SquashedHistory{Through: through, About: "An older build's history."}
	for _, m := range migrations {
		h.Migrations = append(h.Migrations, migrate.Applied{Version: m.Version, Name: m.Name, Checksum: m.Checksum})
	}

	body, err := json.Marshal(h)
	require.NoError(t, err)

	newest := migrations[len(migrations)-1].Version

	return fstest.MapFS{fmt.Sprintf("%04d.json", newest): &fstest.MapFile{Data: body}}
}

// newOptions are the new build's options, with the old build's history squashed.
func newOptions(t *testing.T) migrate.Options {
	t.Helper()

	return migrate.Options{Source: newBuild(), Squashed: squashedOf(t, oldBuild(), 100), Now: clock()}
}

// labels lists alpha's labels, in order.
func labels(t *testing.T, db *sql.DB) []string {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), `SELECT label FROM alpha ORDER BY id`)
	require.NoError(t, err)

	defer func() { _ = rows.Close() }()

	var got []string

	for rows.Next() {
		var label string
		require.NoError(t, rows.Scan(&label))

		got = append(got, label)
	}

	require.NoError(t, rows.Err())

	return got
}

func TestMigrateCarriesASquashedHistoryOver(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)
	apply(t, db, path, oldBuild(), clock())
	execAll(t, db, `INSERT INTO alpha (label) VALUES ('kept')`)

	res, err := migrate.Apply(t.Context(), db, path, newOptions(t))
	require.NoError(t, err)

	require.Len(t, backups(t, path), 1, "backed up once, before anything changed")
	assert.Equal(t, migrate.Result{From: 2, To: 101, CarriedOver: true, Applied: []int{101}, Backup: backups(t, path)[0]}, res)
	assert.True(t, strings.HasSuffix(res.Backup, "-v2"), "named for the history it holds, which the older build opens: %s", res.Backup)

	assert.Equal(t, []string{"kept"}, labels(t, db), "the data is kept")
	assert.Equal(t, []string{"alpha", "beta", "gamma", "schema_migrations"}, tables(t, db), "the later migration ran")
	assert.Equal(t, []int{100, 101}, recordedVersions(t, db), "recorded as the base and what followed")

	migrations, err := migrate.Load(newBuild())
	require.NoError(t, err)

	for _, m := range migrations {
		var checksum string
		require.NoError(t, db.QueryRowContext(t.Context(),
			`SELECT checksum FROM schema_migrations WHERE version = ?`, m.Version).Scan(&checksum))
		assert.Equal(t, m.Checksum, checksum, "%s is recorded with its own checksum", m.File)
	}

	again, err := migrate.Apply(t.Context(), db, path, newOptions(t))
	require.NoError(t, err)
	assert.Equal(t, migrate.Result{From: 101, To: 101}, again, "carried over once")
}

func TestTheOlderBuildRefusesADatabaseCarriedOverAsANewerOnes(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)
	apply(t, db, path, oldBuild(), clock())

	_, err := migrate.Apply(t.Context(), db, path, newOptions(t))
	require.NoError(t, err)

	older := migrate.Options{Source: oldBuild(), Now: clock()}

	_, err = migrate.Apply(t.Context(), db, path, older)
	require.ErrorIs(t, err, migrate.ErrNewerSchema, "so the older build leaves it as it is, and offers its backup")
	require.ErrorIs(t, migrate.Check(t.Context(), db, older), migrate.ErrNewerSchema)
	assert.Equal(t, []int{100, 101}, recordedVersions(t, db))
}

func TestCheckLetsASquashedHistoryThroughAndWritesNothing(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)
	apply(t, db, path, oldBuild(), clock())

	require.NoError(t, migrate.Check(t.Context(), db, newOptions(t)))
	assert.Equal(t, []int{1, 2}, recordedVersions(t, db))
	assert.Empty(t, backups(t, path))
}

func TestMigrateRefusesAHistoryItCannotCarryOver(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// ran is what the database ran before the new build opens it.
		ran  fstest.MapFS
		opts func(t *testing.T) migrate.Options
		want error
	}{
		{
			name: "an older release's, which the squash didn't freeze",
			ran:  firstN(1),
			opts: newOptions,
			want: migrate.ErrSquashedHistory,
		},
		{
			name: "one with a migration since edited",
			ran:  source(map[string]string{firstFile: firstSQL + "-- edited\n", "0002_second.sql": secondSQL}),
			opts: newOptions,
			want: migrate.ErrSquashedHistory,
		},
		{
			name: "one with a migration past the squash's",
			ran:  firstN(3),
			opts: newOptions,
			want: migrate.ErrSquashedHistory,
		},
		{
			name: "the squashed one, with no squashed histories given",
			ran:  oldBuild(),
			opts: func(*testing.T) migrate.Options { return migrate.Options{Source: newBuild(), Now: clock()} },
			want: migrate.ErrSquashedHistory,
		},
		{
			name: "a newer build's, which is no squashed one",
			ran: source(map[string]string{
				"0100_base.sql": firstSQL + secondSQL, "0101_next.sql": thirdSQL, "0102_later.sql": fourthSQL,
			}),
			opts: newOptions,
			want: migrate.ErrNewerSchema,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, path := openDB(t)
			apply(t, db, path, tt.ran, clock())
			before := recordedVersions(t, db)

			err := migrate.Check(t.Context(), db, tt.opts(t))
			require.ErrorIs(t, err, tt.want)

			_, err = migrate.Apply(t.Context(), db, path, tt.opts(t))
			require.ErrorIs(t, err, tt.want)

			if errors.Is(tt.want, migrate.ErrSquashedHistory) {
				require.NotErrorIs(t, err, migrate.ErrNewerSchema, "an older build's history is no newer one's")
				assert.Contains(t, err.Error(), "0100_base.sql")
			}

			assert.Equal(t, before, recordedVersions(t, db), "nothing is written")
			assert.Empty(t, backups(t, path))
		})
	}
}

func TestACarryOverStandsWhenALaterMigrationFails(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)
	apply(t, db, path, oldBuild(), clock())

	broken := newOptions(t)
	broken.Source = source(map[string]string{
		"0100_base.sql": firstSQL + secondSQL, "0101_next.sql": "INSERT INTO missing_table VALUES (1);\n",
	})

	_, err := migrate.Apply(t.Context(), db, path, broken)
	require.ErrorIs(t, err, migrate.ErrMigrationFailed)
	assert.Equal(t, []int{100}, recordedVersions(t, db), "the carry over is kept, and the failed migration rolled back")
	require.Len(t, backups(t, path), 1)

	res, err := migrate.Apply(t.Context(), db, path, newOptions(t))
	require.NoError(t, err)
	assert.False(t, res.CarriedOver, "a database carried over is this build's own")
	assert.Equal(t, []int{101}, res.Applied)
	assert.True(t, strings.HasSuffix(res.Backup, "-v100"), res.Backup)
}

func TestLoadRejectsInvalidSquashedHistories(t *testing.T) {
	t.Parallel()

	const sum = "1111111111111111111111111111111111111111111111111111111111111111"

	history := func(through int, versions ...string) string {
		return fmt.Sprintf(`{"through": %d, "about": "", "migrations": [%s]}`, through, strings.Join(versions, ", "))
	}
	version := func(v int, name, checksum string) string {
		return fmt.Sprintf(`{"version": %d, "name": %q, "checksum": %q}`, v, name, checksum)
	}

	tests := []struct {
		name, file, body, want string
	}{
		{name: "badly named", file: "two.json", body: history(100, version(2, "second", sum)), want: "want NNNN.json"},
		{name: "not JSON", file: "0002.json", body: "{", want: "0002.json"},
		{
			name: "a field no history has", file: "0002.json",
			body: `{"through": 100, "migrations": [` + version(2, "second", sum) + `], "extra": 1}`, want: "extra",
		},
		{name: "no migrations", file: "0002.json", body: history(100), want: "no migrations"},
		{
			name: "versions that don't rise", file: "0001.json",
			body: history(100, version(2, "second", sum), version(1, "first", sum)), want: "does not follow",
		},
		{
			name: "a version of the base's", file: "0100.json", body: history(100, version(100, "base", sum)),
			want: "not older than the oldest migration",
		},
		{name: "a name no file has", file: "0002.json", body: history(100, version(2, "Second", sum)), want: "no name"},
		{name: "no checksum", file: "0002.json", body: history(100, version(2, "second", "abc")), want: "no SHA-256"},
		{
			name: "named for another version", file: "0003.json", body: history(100, version(2, "second", sum)),
			want: "its newest version is 2",
		},
		{
			name: "carried over through no migration", file: "0002.json", body: history(99, version(2, "second", sum)),
			want: "through 99",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, _ := openDB(t)
			opts := migrate.Options{
				Source:   newBuild(),
				Squashed: fstest.MapFS{tt.file: &fstest.MapFile{Data: []byte(tt.body)}},
			}

			err := migrate.Check(t.Context(), db, opts)
			require.ErrorIs(t, err, migrate.ErrInvalidMigration)
			assert.Contains(t, err.Error(), tt.want)

			_, err = migrate.KnownSchemas(opts)
			require.ErrorIs(t, err, migrate.ErrInvalidMigration)
		})
	}
}

func TestLoadSkipsWhatIsNoSquashedHistory(t *testing.T) {
	t.Parallel()

	squashed := squashedOf(t, oldBuild(), 100)
	squashed["README.md"] = &fstest.MapFile{Data: []byte("notes")}
	squashed["0002.schema"] = &fstest.MapFile{Data: []byte("create table alpha\n")}
	squashed["older/0001.json"] = &fstest.MapFile{Data: []byte("{")}

	known, err := migrate.KnownSchemas(migrate.Options{Source: newBuild(), Squashed: squashed})
	require.NoError(t, err)
	assert.Equal(t, []int{2}, known.Squashed)
}

func TestKnownSchemasSaysWhichBackupsOpen(t *testing.T) {
	t.Parallel()

	known, err := migrate.KnownSchemas(newOptions(t))
	require.NoError(t, err)
	assert.Equal(t, migrate.Schemas{Oldest: 100, Newest: 101, Squashed: []int{2}}, known)

	for schema, opens := range map[int]bool{0: false, 1: false, 2: true, 3: false, 99: false, 100: true, 101: true, 102: false} {
		assert.Equal(t, opens, known.Opens(schema), "schema %d", schema)
	}

	own, err := migrate.KnownSchemas(migrate.Options{Source: oldBuild()})
	require.NoError(t, err)
	assert.Equal(t, migrate.Schemas{Oldest: 1, Newest: 2, Squashed: []int{}}, own, "a source of the caller's own has none squashed")
}
