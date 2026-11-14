package migrate_test

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/store/migrate"
)

const (
	// firstFile is the first synthetic migration's file name.
	firstFile = "0001_first.sql"

	firstSQL  = "CREATE TABLE alpha (id INTEGER PRIMARY KEY, label TEXT NOT NULL);\nCREATE INDEX alpha_label ON alpha (label);\n"
	secondSQL = "CREATE TABLE beta (id INTEGER PRIMARY KEY);\n"
	thirdSQL  = "CREATE TABLE gamma (id INTEGER PRIMARY KEY);\n"
	fourthSQL = "CREATE TABLE delta (id INTEGER PRIMARY KEY);\n"
	fifthSQL  = "CREATE TABLE epsilon (id INTEGER PRIMARY KEY);\n"
)

// source builds an in-memory migration set from file name → SQL pairs.
func source(files map[string]string) fstest.MapFS {
	fsys := fstest.MapFS{}
	for name, body := range files {
		fsys[name] = &fstest.MapFile{Data: []byte(body)}
	}

	return fsys
}

// firstN returns the first n synthetic migrations.
func firstN(n int) fstest.MapFS {
	all := []struct{ name, body string }{
		{firstFile, firstSQL},
		{"0002_second.sql", secondSQL},
		{"0003_third.sql", thirdSQL},
		{"0004_fourth.sql", fourthSQL},
		{"0005_fifth.sql", fifthSQL},
	}

	files := make(map[string]string, n)
	for _, m := range all[:n] {
		files[m.name] = m.body
	}

	return source(files)
}

// clock returns a Now func that advances one minute per call, starting at a fixed time.
func clock() func() time.Time {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

	return func() time.Time {
		now = now.Add(time.Minute)

		return now
	}
}

// openDB opens a fresh database file in a temp dir.
func openDB(t *testing.T) (*sql.DB, string) {
	t.Helper()

	path := filepath.Join(t.TempDir(), "test.db")

	return openPath(t, path), path
}

// openPath opens the database file at path.
func openPath(t *testing.T, path string) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", path+"?_pragma=foreign_keys(1)")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	return db
}

// tables lists the non-internal tables in db.
func tables(t *testing.T, db *sql.DB) []string {
	t.Helper()

	rows, err := db.QueryContext(t.Context(),
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\' ORDER BY name`)
	require.NoError(t, err)

	defer func() { _ = rows.Close() }()

	var names []string

	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))

		names = append(names, name)
	}

	require.NoError(t, rows.Err())

	return names
}

// backups lists the backup files next to dbPath.
func backups(t *testing.T, dbPath string) []string {
	t.Helper()

	matches, err := filepath.Glob(dbPath + ".backup-*")
	require.NoError(t, err)

	return matches
}

// apply runs Apply with the given source and the shared clock.
func apply(t *testing.T, db *sql.DB, path string, src fstest.MapFS, now func() time.Time) migrate.Result {
	t.Helper()

	res, err := migrate.Apply(t.Context(), db, path, migrate.Options{Source: src, Now: now})
	require.NoError(t, err)

	return res
}

func TestMigrateFreshDatabase(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)

	res, err := migrate.Apply(t.Context(), db, path, migrate.Options{})
	require.NoError(t, err)

	// Every migration shipped in migrations/, in order.
	files, err := filepath.Glob(filepath.Join("migrations", "*.sql"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	slices.Sort(files)

	applied := make([]int, 0, len(files))
	for i := range files {
		applied = append(applied, i+1)
	}

	assert.Equal(t, migrate.Result{From: 0, To: len(files), Applied: applied}, res)
	assert.Equal(t, []string{
		"accounts", "agent_profiles", "agent_sessions", "avatars", "gh_thread_clears", "gh_thread_moments", "gh_threads",
		"issue_views", "item_details", "item_files", "item_local_state", "items", "launch_defaults", "notifications",
		"prefs", "prompts", "rate_snapshots", "repos", "review_diff_files", "review_diffs", "schema_migrations",
		"script_runs", "script_trust", "tool_installs", "user_scripts", "worktree_cleanups", "worktree_records",
	}, tables(t, db))

	// Every shipped migration is recorded with its name and the checksum of its file.
	rows, err := db.QueryContext(t.Context(), `SELECT version, name, checksum, applied_at FROM schema_migrations ORDER BY version`)
	require.NoError(t, err)

	defer func() { _ = rows.Close() }()

	var seen int

	for rows.Next() {
		var (
			name, checksum, appliedAt string
			version                   int
		)

		require.NoError(t, rows.Scan(&version, &name, &checksum, &appliedAt))
		require.Less(t, seen, len(files), "more migrations recorded than shipped")

		body, readErr := os.ReadFile(files[seen])
		require.NoError(t, readErr)

		sum := sha256.Sum256(body)
		wantName := strings.TrimSuffix(filepath.Base(files[seen])[len("0001_"):], ".sql")

		assert.Equal(t, seen+1, version)
		assert.Equal(t, wantName, name)
		assert.Equal(t, hex.EncodeToString(sum[:]), checksum)

		_, err = time.Parse(time.RFC3339, appliedAt)
		require.NoError(t, err)

		seen++
	}

	require.NoError(t, rows.Err())
	assert.Equal(t, len(files), seen)

	// A brand-new database has nothing to protect, so no backup is written.
	assert.Empty(t, backups(t, path))
}

func TestMigrateIsIdempotent(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)
	now := clock()

	first := apply(t, db, path, firstN(2), now)
	assert.Equal(t, []int{1, 2}, first.Applied)

	second := apply(t, db, path, firstN(2), now)
	assert.Equal(t, migrate.Result{From: 2, To: 2}, second)

	var count int
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT count(*) FROM schema_migrations`).Scan(&count))
	assert.Equal(t, 2, count)
}

func TestMigrateRefusesUnmanagedSchema(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)

	_, err := db.ExecContext(t.Context(), `CREATE TABLE legacy (id INTEGER PRIMARY KEY)`)
	require.NoError(t, err)

	_, err = migrate.Apply(t.Context(), db, path, migrate.Options{})
	require.ErrorIs(t, err, migrate.ErrUnmanagedSchema)
	assert.Contains(t, err.Error(), "legacy")

	// Nothing was created on the unmanaged database.
	assert.Equal(t, []string{"legacy"}, tables(t, db))
}

func TestMigrateRefusesChecksumMismatch(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)
	now := clock()

	apply(t, db, path, firstN(1), now)

	edited := source(map[string]string{
		firstFile:         firstSQL + "-- an edit after the fact\n",
		"0002_second.sql": secondSQL,
	})

	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{Source: edited, Now: now})
	require.ErrorIs(t, err, migrate.ErrChecksumMismatch)
	assert.Contains(t, err.Error(), firstFile)

	// The pending migration was not applied and no backup was taken.
	assert.NotContains(t, tables(t, db), "beta")
	assert.Empty(t, backups(t, path))
}

func TestMigrateRefusesANewerSchema(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)
	now := clock()

	apply(t, db, path, firstN(2), now)

	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{Source: firstN(1), Now: now})
	require.ErrorIs(t, err, migrate.ErrNewerSchema)
	assert.Contains(t, err.Error(), "version 2")
	assert.Contains(t, err.Error(), "update "+brand.Name, "the message tells the user what to do")
}

func TestMigrateReportsAChangedMigrationBeforeANewerSchema(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)
	now := clock()

	// Two migrations ran; this build has the first, edited, and not the second, as when
	// a build combines earlier migrations into one file.
	apply(t, db, path, firstN(2), now)

	combined := source(map[string]string{firstFile: firstSQL + secondSQL})

	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{Source: combined, Now: now})
	require.ErrorIs(t, err, migrate.ErrChecksumMismatch)
	require.NotErrorIs(t, err, migrate.ErrNewerSchema)
	assert.Contains(t, err.Error(), firstFile)
}

func TestMigrateRefusesOutOfOrder(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)
	now := clock()

	apply(t, db, path, source(map[string]string{
		firstFile:        firstSQL,
		"0003_third.sql": thirdSQL,
	}), now)

	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{Source: firstN(3), Now: now})
	require.ErrorIs(t, err, migrate.ErrOutOfOrder)
	assert.Contains(t, err.Error(), "0002_second.sql")
	assert.NotContains(t, tables(t, db), "beta")
}

func TestMigrateBacksUpBeforeApplying(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)
	now := clock()

	apply(t, db, path, firstN(1), now)

	_, err := db.ExecContext(t.Context(), `INSERT INTO alpha (label) VALUES ('kept')`)
	require.NoError(t, err)

	res := apply(t, db, path, firstN(2), now)
	assert.Equal(t, 1, res.From)
	assert.Equal(t, 2, res.To)
	assert.Equal(t, []int{2}, res.Applied)
	require.NotEmpty(t, res.Backup)
	assert.Equal(t, []string{res.Backup}, backups(t, path))
	assert.Regexp(t, `test\.db\.backup-\d{8}T\d{6}Z-v1$`, res.Backup)

	info, err := os.Stat(res.Backup)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	// The backup holds the pre-migration schema and data.
	bdb := openPath(t, res.Backup)
	assert.Equal(t, []string{"alpha", "schema_migrations"}, tables(t, bdb))

	var label string
	require.NoError(t, bdb.QueryRowContext(t.Context(), `SELECT label FROM alpha`).Scan(&label))
	assert.Equal(t, "kept", label)

	var newest int
	require.NoError(t, bdb.QueryRowContext(t.Context(), `SELECT max(version) FROM schema_migrations`).Scan(&newest))
	assert.Equal(t, 1, newest)

	// The live database moved on.
	assert.Contains(t, tables(t, db), "beta")
}

func TestMigrateNoBackupWhenNothingPending(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)
	now := clock()

	apply(t, db, path, firstN(1), now)

	res := apply(t, db, path, firstN(1), now)
	assert.Empty(t, res.Backup)
	assert.Empty(t, res.Applied)
	assert.Empty(t, backups(t, path))
}

func TestMigrateKeepsNewestBackups(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)
	now := clock()

	apply(t, db, path, firstN(1), now)

	var written []string

	for n := 2; n <= 5; n++ {
		res := apply(t, db, path, firstN(n), now)
		require.NotEmpty(t, res.Backup)

		written = append(written, res.Backup)
	}

	// Four backups were written (before v2..v5); only the newest three remain.
	assert.Equal(t, written[1:], backups(t, path))

	// A smaller keep count prunes further on the next backup.
	more := source(map[string]string{
		firstFile:         firstSQL,
		"0002_second.sql": secondSQL,
		"0003_third.sql":  thirdSQL,
		"0004_fourth.sql": fourthSQL,
		"0005_fifth.sql":  fifthSQL,
		"0006_sixth.sql":  "CREATE TABLE zeta (id INTEGER PRIMARY KEY);\n",
	})

	res, err := migrate.Apply(t.Context(), db, path, migrate.Options{Source: more, Now: now, KeepBackups: 1})
	require.NoError(t, err)
	assert.Equal(t, []string{res.Backup}, backups(t, path))
}

func TestMigrateBackupNameCollision(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)
	fixed := func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }

	apply(t, db, path, firstN(1), fixed)

	taken := path + ".backup-20260102T030405Z-v1"
	require.NoError(t, os.WriteFile(taken, []byte("not a database"), 0o600))

	res := apply(t, db, path, firstN(2), fixed)
	assert.Equal(t, taken+"-1", res.Backup)

	// The unrelated file with the base name is left alone.
	body, err := os.ReadFile(taken) //nolint:gosec // a file this test created in its temp dir
	require.NoError(t, err)
	assert.Equal(t, "not a database", string(body))
}

func TestMigrateBackupRequiresPath(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)
	now := clock()

	apply(t, db, path, firstN(1), now)

	_, err := migrate.Apply(t.Context(), db, "", migrate.Options{Source: firstN(2), Now: now})
	require.ErrorIs(t, err, migrate.ErrNoBackupPath)
	assert.NotContains(t, tables(t, db), "beta")
}

func TestMigrateFailedMigrationRollsBack(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)
	now := clock()

	apply(t, db, path, firstN(1), now)

	broken := source(map[string]string{
		firstFile:         firstSQL,
		"0002_broken.sql": "CREATE TABLE half (id INTEGER PRIMARY KEY);\nINSERT INTO missing_table VALUES (1);\n",
	})

	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{Source: broken, Now: now})
	require.ErrorIs(t, err, migrate.ErrMigrationFailed)
	assert.Contains(t, err.Error(), "0002_broken.sql")

	// Neither the half-created table nor a history row survived.
	assert.NotContains(t, tables(t, db), "half")

	var newest int
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT max(version) FROM schema_migrations`).Scan(&newest))
	assert.Equal(t, 1, newest)
}

func TestLoadRejectsInvalidFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{name: "bad name", files: map[string]string{"1_first.sql": firstSQL}, want: "1_first.sql"},
		{name: "uppercase name", files: map[string]string{"0001_First.sql": firstSQL}, want: "0001_First.sql"},
		{name: "version zero", files: map[string]string{"0000_first.sql": firstSQL}, want: "0001 or higher"},
		{name: "empty file", files: map[string]string{firstFile: " \n\t"}, want: "empty"},
		{
			name:  "duplicate version",
			files: map[string]string{firstFile: firstSQL, "0001_again.sql": secondSQL},
			want:  "share version 1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := migrate.Load(source(tt.files))
			require.ErrorIs(t, err, migrate.ErrInvalidMigration)
			assert.Contains(t, err.Error(), tt.want)
		})
	}
}

func TestLoadSortsAndSkipsNonMigrations(t *testing.T) {
	t.Parallel()

	fsys := source(map[string]string{
		"0002_second.sql": secondSQL,
		firstFile:         firstSQL,
		"README.md":       "notes",
	})
	fsys["archive/0009_old.sql"] = &fstest.MapFile{Data: []byte(thirdSQL)}

	migrations, err := migrate.Load(fsys)
	require.NoError(t, err)
	require.Len(t, migrations, 2)
	assert.Equal(t, 1, migrations[0].Version)
	assert.Equal(t, "first", migrations[0].Name)
	assert.Equal(t, firstFile, migrations[0].File)
	assert.Equal(t, firstSQL, migrations[0].SQL)
	assert.Equal(t, 2, migrations[1].Version)
}

func TestLoadReportsUnreadableSource(t *testing.T) {
	t.Parallel()

	_, err := migrate.Load(os.DirFS(filepath.Join(t.TempDir(), "missing")))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read migrations")
}

func TestEmbeddedMigrations(t *testing.T) {
	t.Parallel()

	migrations, err := migrate.Load(migrate.Embedded())
	require.NoError(t, err)
	require.NotEmpty(t, migrations)
	assert.Equal(t, 1, migrations[0].Version)
	assert.Equal(t, "init", migrations[0].Name)

	for i, m := range migrations {
		assert.Equal(t, i+1, m.Version, "embedded migrations are contiguous from 0001")
		assert.True(t, strings.HasSuffix(m.File, ".sql"))
	}
}

func TestMigrateReportsInvalidSource(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)

	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{Source: source(map[string]string{"bad.sql": firstSQL})})
	require.ErrorIs(t, err, migrate.ErrInvalidMigration)
	assert.Empty(t, tables(t, db), "nothing is created when the source is invalid")
}

func TestMigrateReportsClosedDatabase(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)
	require.NoError(t, db.Close())

	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "inspect schema")
}

func TestMigrateReportsMalformedHistory(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup []string
	}{
		{
			name:  "missing columns",
			setup: []string{`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY)`},
		},
		{
			name: "non-numeric version",
			setup: []string{
				`CREATE TABLE schema_migrations (version TEXT, name TEXT, checksum TEXT)`,
				`INSERT INTO schema_migrations VALUES ('one', 'first', 'abc')`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, path := openDB(t)

			for _, stmt := range tt.setup {
				_, err := db.ExecContext(t.Context(), stmt)
				require.NoError(t, err)
			}

			_, err := migrate.Apply(t.Context(), db, path, migrate.Options{Source: firstN(1)})
			require.Error(t, err)
			assert.Contains(t, err.Error(), "read migration history")
		})
	}
}

func TestMigrateReportsReadOnlyDatabase(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "readonly.db")
	require.NoError(t, os.WriteFile(path, nil, 0o600))

	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = migrate.Apply(t.Context(), db, path, migrate.Options{Source: firstN(1)})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "create schema_migrations")
}

func TestMigrateReportsBackupFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		dbPath func(t *testing.T) string
		want   string
	}{
		{
			name:   "backup directory is missing",
			dbPath: func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing", "test.db") },
			want:   "back up database before migrating",
		},
		{
			name: "backup directory is a file",
			dbPath: func(t *testing.T) string {
				blocker := filepath.Join(t.TempDir(), "blocker")
				require.NoError(t, os.WriteFile(blocker, nil, 0o600))

				return filepath.Join(blocker, "test.db")
			},
			want: "check backup path",
		},
		{
			name: "every backup name is taken",
			dbPath: func(t *testing.T) string {
				dbPath := filepath.Join(t.TempDir(), "test.db")
				base := dbPath + ".backup-20260102T030405Z-v1"

				require.NoError(t, os.WriteFile(base, nil, 0o600))

				for i := 1; i <= 100; i++ {
					require.NoError(t, os.WriteFile(base+"-"+strconv.Itoa(i), nil, 0o600))
				}

				return dbPath
			},
			want: "no free backup file name",
		},
	}

	fixed := func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, path := openDB(t)
			apply(t, db, path, firstN(1), fixed)

			_, err := migrate.Apply(t.Context(), db, tt.dbPath(t), migrate.Options{Source: firstN(2), Now: fixed})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.want)
			assert.NotContains(t, tables(t, db), "beta", "nothing is applied without a backup")
		})
	}
}

func TestMigrateCannotEraseHistory(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)

	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{Source: source(map[string]string{
		"0001_erase.sql": "DROP TABLE schema_migrations;\n",
	})})
	require.ErrorIs(t, err, migrate.ErrMigrationFailed)
	assert.Contains(t, err.Error(), "record 0001_erase.sql")

	// The drop was rolled back together with the failed history insert.
	assert.Equal(t, []string{"schema_migrations"}, tables(t, db))
}

func TestCheckReportsWhatApplyWouldRefuse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// setup prepares the database; Check then runs with firstN(3).
		setup func(t *testing.T, db *sql.DB, path string)
		want  error
	}{
		{name: "new and empty", setup: func(*testing.T, *sql.DB, string) {}},
		{
			name:  "up to date",
			setup: func(t *testing.T, db *sql.DB, path string) { apply(t, db, path, firstN(3), clock()) },
		},
		{
			name:  "migrations pending",
			setup: func(t *testing.T, db *sql.DB, path string) { apply(t, db, path, firstN(1), clock()) },
		},
		{
			name: "unmanaged",
			setup: func(t *testing.T, db *sql.DB, _ string) {
				_, err := db.ExecContext(t.Context(), `CREATE TABLE legacy (id INTEGER PRIMARY KEY)`)
				require.NoError(t, err)
			},
			want: migrate.ErrUnmanagedSchema,
		},
		{
			name: "changed",
			setup: func(t *testing.T, db *sql.DB, path string) {
				apply(t, db, path, source(map[string]string{firstFile: firstSQL + "-- edited\n"}), clock())
			},
			want: migrate.ErrChecksumMismatch,
		},
		{
			name:  "newer",
			setup: func(t *testing.T, db *sql.DB, path string) { apply(t, db, path, firstN(4), clock()) },
			want:  migrate.ErrNewerSchema,
		},
		{
			name: "out of order",
			setup: func(t *testing.T, db *sql.DB, path string) {
				apply(t, db, path, source(map[string]string{firstFile: firstSQL, "0003_third.sql": thirdSQL}), clock())
			},
			want: migrate.ErrOutOfOrder,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db, path := openDB(t)
			tt.setup(t, db, path)
			before := tables(t, db)

			err := migrate.Check(t.Context(), db, migrate.Options{Source: firstN(3)})
			if tt.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.want)
			}

			assert.Equal(t, before, tables(t, db), "Check creates nothing")
		})
	}
}

func TestCheckReportsWhatItCannotRead(t *testing.T) {
	t.Parallel()

	db, _ := openDB(t)

	require.ErrorIs(t, migrate.Check(t.Context(), db, migrate.Options{Source: source(map[string]string{"bad.sql": firstSQL})}),
		migrate.ErrInvalidMigration)

	_, err := db.ExecContext(t.Context(), `CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY)`)
	require.NoError(t, err)
	require.ErrorContains(t, migrate.Check(t.Context(), db, migrate.Options{Source: firstN(1)}), "read migration history")

	require.NoError(t, db.Close())
	require.ErrorContains(t, migrate.Check(t.Context(), db, migrate.Options{Source: firstN(1)}), "inspect schema")
}
