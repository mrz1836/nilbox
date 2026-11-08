// Package migrate applies the application's SQLite schema migrations.
//
// Migrations are plain SQL files embedded in the binary and named NNNN_<name>.sql. They
// are forward-only: there is no down path, and an applied migration is never edited.
// Every applied migration is recorded in schema_migrations with the SHA-256 checksum of
// its file, and Apply refuses to touch a database whose history does not match the
// embedded set, checking in this order:
//
//   - application tables with no schema_migrations table (an unmanaged schema)
//   - an applied migration whose file has changed since it ran
//   - an applied version that is not embedded (a database from a newer build)
//   - an embedded version below the newest applied one that never ran
//
// Changed files are looked for before unknown versions, so a database migrated by an
// older build whose migrations were since combined into fewer files reads as changed,
// not as one from a newer build.
//
// Before applying anything to a database that already holds migrated data, Apply copies
// the database to a timestamped backup file next to it with VACUUM INTO, and keeps only
// the newest few backups. A brand-new, empty database has nothing to protect and is not
// backed up.
package migrate

import (
	"cmp"
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
)

// DefaultKeepBackups is how many pre-migration backups are kept when Options does not say.
const DefaultKeepBackups = 3

const (
	// maxBackupNameAttempts bounds the search for a free backup file name.
	maxBackupNameAttempts = 100

	// historyTable records the applied migrations.
	historyTable = "schema_migrations"
)

// BackupInfix sits between a database's path and the rest of a pre-migration backup's
// name: <path>.backup-<UTC stamp>-v<from>, where from is the newest version applied
// before the migrations ran.
const BackupInfix = ".backup-"

var (
	// ErrUnmanagedSchema is returned when the database has application tables but no
	// migration history, so its schema was not created by this package.
	ErrUnmanagedSchema = errors.New("database schema is not managed by migrations")

	// ErrNewerSchema is returned when the database records a migration this build does
	// not have, which means a newer build already migrated it. The message tells the user
	// to update the app.
	ErrNewerSchema = errors.New("the data was saved by a newer version of " + brand.Name +
		"; update " + brand.Name + " to open it")

	// ErrChecksumMismatch is returned when an applied migration's file has been edited.
	ErrChecksumMismatch = errors.New("applied migration has been modified")

	// ErrOutOfOrder is returned when a migration older than the newest applied one has
	// never been applied.
	ErrOutOfOrder = errors.New("migration is older than the newest applied migration")

	// ErrMigrationFailed wraps the error of a pending migration that could not be applied.
	// The migration was rolled back, and a database that held migrated data was backed up
	// before it ran.
	ErrMigrationFailed = errors.New("migration failed")

	// ErrInvalidMigration is returned for a migration file with a bad name, a duplicate
	// version, or no SQL.
	ErrInvalidMigration = errors.New("invalid migration file")

	// ErrNoBackupPath is returned when a backup is required but no database path is known.
	ErrNoBackupPath = errors.New("a database file path is required to back up before migrating")
)

// embedded holds the migrations shipped with the binary.
//
//go:embed migrations/*.sql
var embedded embed.FS

// fileNamePattern matches NNNN_<name>.sql and captures the version and the name.
var fileNamePattern = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

// Migration is one forward-only schema change.
type Migration struct {
	Version  int    // the NNNN prefix of the file name
	Name     string // the <name> part of the file name
	File     string // the file name
	Checksum string // hex SHA-256 of the file bytes
	SQL      string // the file contents
}

// Options tune Apply. The zero value applies the embedded migrations.
type Options struct {
	// Source holds NNNN_<name>.sql files at its root. Nil means the embedded migrations.
	Source fs.FS

	// Now returns the current time for applied_at and backup names. Nil means time.Now.
	Now func() time.Time

	// KeepBackups is how many pre-migration backups to keep. Zero or less means
	// DefaultKeepBackups.
	KeepBackups int
}

// Result reports what Apply did.
type Result struct {
	From    int    // the newest applied version before Apply (0 for a new database)
	To      int    // the newest applied version after Apply
	Applied []int  // the versions applied by this call, in order
	Backup  string // the backup file written before applying, or "" when none was needed
}

// Embedded returns the migrations shipped with the binary.
func Embedded() fs.FS {
	sub, err := fs.Sub(embedded, "migrations")
	if err != nil {
		// fs.Sub fails only for an invalid directory name, and this one is a constant.
		return embedded
	}

	return sub
}

// Load reads and validates the migrations in src, sorted by version.
func Load(src fs.FS) ([]Migration, error) {
	entries, err := fs.ReadDir(src, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}

	migrations := make([]Migration, 0, len(entries))
	seen := make(map[int]string, len(entries))

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		m, err := loadFile(src, entry.Name())
		if err != nil {
			return nil, err
		}

		if prev, dup := seen[m.Version]; dup {
			return nil, fmt.Errorf("%w: %s and %s share version %d", ErrInvalidMigration, prev, m.File, m.Version)
		}

		seen[m.Version] = m.File
		migrations = append(migrations, m)
	}

	slices.SortFunc(migrations, func(a, b Migration) int { return cmp.Compare(a.Version, b.Version) })

	return migrations, nil
}

// Apply brings the database up to date with the migrations in opts.Source. dbPath is the
// database file, used for the pre-migration backup.
func Apply(ctx context.Context, db *sql.DB, dbPath string, opts Options) (Result, error) {
	opts = opts.withDefaults()

	migrations, err := Load(opts.Source)
	if err != nil {
		return Result{}, err
	}

	if err = ensureManaged(ctx, db); err != nil {
		return Result{}, err
	}

	applied, err := readApplied(ctx, db)
	if err != nil {
		return Result{}, err
	}

	from := newestVersion(applied)
	result := Result{From: from, To: from}

	pending, err := verify(migrations, applied, from)
	if err != nil || len(pending) == 0 {
		return result, err
	}

	// A brand-new database has no data to protect, so only a migrated one is backed up.
	if from > 0 {
		if result.Backup, err = backup(ctx, db, dbPath, from, opts); err != nil {
			return result, err
		}
	}

	return applyPending(ctx, db, pending, opts, result)
}

// Check reports whether Apply would refuse the database, without writing to it: it
// returns the error Apply would for an unmanaged schema or a history that does not match
// the migrations in opts.Source, and nil for a database Apply can bring up to date,
// including a new, empty one.
func Check(ctx context.Context, db *sql.DB, opts Options) error {
	opts = opts.withDefaults()

	migrations, err := Load(opts.Source)
	if err != nil {
		return err
	}

	tables, err := listTables(ctx, db)
	if err != nil {
		return err
	}

	managed, err := checkManaged(tables)
	if err != nil || !managed {
		return err
	}

	applied, err := readApplied(ctx, db)
	if err != nil {
		return err
	}

	_, err = verify(migrations, applied, newestVersion(applied))

	return err
}

// withDefaults fills in the zero-value fields.
func (o Options) withDefaults() Options {
	if o.Source == nil {
		o.Source = Embedded()
	}

	if o.Now == nil {
		o.Now = time.Now
	}

	if o.KeepBackups <= 0 {
		o.KeepBackups = DefaultKeepBackups
	}

	return o
}

// loadFile reads one migration file and validates its name and contents.
func loadFile(src fs.FS, file string) (Migration, error) {
	match := fileNamePattern.FindStringSubmatch(file)
	if match == nil {
		return Migration{}, fmt.Errorf("%w: %s: want NNNN_<name>.sql", ErrInvalidMigration, file)
	}

	version, err := strconv.Atoi(match[1])
	if err != nil || version == 0 {
		return Migration{}, fmt.Errorf("%w: %s: version must be 0001 or higher", ErrInvalidMigration, file)
	}

	body, err := fs.ReadFile(src, file)
	if err != nil {
		return Migration{}, fmt.Errorf("read migration %s: %w", file, err)
	}

	if strings.TrimSpace(string(body)) == "" {
		return Migration{}, fmt.Errorf("%w: %s is empty", ErrInvalidMigration, file)
	}

	sum := sha256.Sum256(body)

	return Migration{
		Version:  version,
		Name:     match[2],
		File:     file,
		Checksum: hex.EncodeToString(sum[:]),
		SQL:      string(body),
	}, nil
}

// appliedMigration is one row of schema_migrations.
type appliedMigration struct {
	Version  int
	Name     string
	Checksum string
}

// ensureManaged creates schema_migrations on a new database and refuses a database whose
// tables were created some other way.
func ensureManaged(ctx context.Context, db *sql.DB) error {
	tables, err := listTables(ctx, db)
	if err != nil {
		return err
	}

	managed, err := checkManaged(tables)
	if err != nil || managed {
		return err
	}

	_, err = db.ExecContext(ctx, `CREATE TABLE schema_migrations (
		version    INTEGER PRIMARY KEY,
		name       TEXT    NOT NULL,
		checksum   TEXT    NOT NULL,
		applied_at TEXT    NOT NULL
	)`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	return nil
}

// checkManaged reports whether a database with tables has a migration history. A
// database with neither is new; one with tables but no history is refused.
func checkManaged(tables []string) (bool, error) {
	if slices.Contains(tables, historyTable) {
		return true, nil
	}

	if len(tables) > 0 {
		return false, fmt.Errorf("%w: found tables %s", ErrUnmanagedSchema, strings.Join(tables, ", "))
	}

	return false, nil
}

// listTables lists every table that is not an SQLite internal, including the migration
// history table.
func listTables(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite\_%' ESCAPE '\'
		ORDER BY name`)
	if err != nil {
		return nil, fmt.Errorf("inspect schema: %w", err)
	}

	defer func() { _ = rows.Close() }()

	var tables []string

	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("inspect schema: %w", err)
		}

		tables = append(tables, name)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("inspect schema: %w", err)
	}

	return tables, nil
}

// readApplied returns the recorded migrations keyed by version.
func readApplied(ctx context.Context, db *sql.DB) (map[int]appliedMigration, error) {
	rows, err := db.QueryContext(ctx, `SELECT version, name, checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("read migration history: %w", err)
	}

	defer func() { _ = rows.Close() }()

	applied := make(map[int]appliedMigration)

	for rows.Next() {
		var a appliedMigration
		if err = rows.Scan(&a.Version, &a.Name, &a.Checksum); err != nil {
			return nil, fmt.Errorf("read migration history: %w", err)
		}

		applied[a.Version] = a
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read migration history: %w", err)
	}

	return applied, nil
}

// newestVersion returns the highest applied version, or 0 when nothing is applied.
func newestVersion(applied map[int]appliedMigration) int {
	newest := 0
	for v := range applied {
		newest = max(newest, v)
	}

	return newest
}

// verify checks the recorded history against the available migrations and returns the
// ones still to apply, in order.
func verify(migrations []Migration, applied map[int]appliedMigration, newest int) ([]Migration, error) {
	if err := verifyHistory(migrations, applied); err != nil {
		return nil, err
	}

	var pending []Migration

	for _, m := range migrations {
		if _, done := applied[m.Version]; done {
			continue
		}

		if m.Version < newest {
			return nil, fmt.Errorf("%w: %s was never applied, but version %d was", ErrOutOfOrder, m.File, newest)
		}

		pending = append(pending, m)
	}

	return pending, nil
}

// verifyHistory checks that every applied migration is still available, unchanged. Every
// changed migration is reported before any unknown version: a database whose recorded
// history this build disagrees with is not one from a newer build, even when it also
// records versions this build does not have.
func verifyHistory(migrations []Migration, applied map[int]appliedMigration) error {
	known := make(map[int]Migration, len(migrations))
	for _, m := range migrations {
		known[m.Version] = m
	}

	versions := slices.Sorted(maps.Keys(applied))

	for _, v := range versions {
		if m, ok := known[v]; ok && m.Checksum != applied[v].Checksum {
			return fmt.Errorf("%w: %s", ErrChecksumMismatch, m.File)
		}
	}

	for _, v := range versions {
		if _, ok := known[v]; !ok {
			return fmt.Errorf("%w: it records version %d (%s), which this build does not have",
				ErrNewerSchema, v, applied[v].Name)
		}
	}

	return nil
}

// backup copies the database to <dbPath>.backup-<UTC stamp>-v<from> and prunes old
// backups. It returns the backup file path.
func backup(ctx context.Context, db *sql.DB, dbPath string, from int, opts Options) (string, error) {
	if dbPath == "" {
		return "", ErrNoBackupPath
	}

	target, err := freeBackupName(fmt.Sprintf("%s%s%s-v%d", dbPath, BackupInfix, clock.Stamp(opts.Now()), from))
	if err != nil {
		return "", err
	}

	if _, err = db.ExecContext(ctx, `VACUUM INTO ?`, target); err != nil {
		return "", fmt.Errorf("back up database before migrating: %w", err)
	}

	if err = os.Chmod(target, appdata.PrivateFilePerm); err != nil {
		return "", fmt.Errorf("restrict backup permissions: %w", err)
	}

	if err = pruneBackups(dbPath, opts.KeepBackups); err != nil {
		return "", err
	}

	return target, nil
}

// freeBackupName returns base, or base with a numeric suffix when base already exists.
func freeBackupName(base string) (string, error) {
	name := base

	for i := 1; i <= maxBackupNameAttempts; i++ {
		if _, err := os.Lstat(name); errors.Is(err, fs.ErrNotExist) {
			return name, nil
		} else if err != nil {
			return "", fmt.Errorf("check backup path: %w", err)
		}

		name = fmt.Sprintf("%s-%d", base, i)
	}

	return "", fmt.Errorf("%w: no free backup file name for %s", ErrNoBackupPath, base)
}

// pruneBackups deletes all but the newest keep backups of dbPath.
func pruneBackups(dbPath string, keep int) error {
	dir := filepath.Dir(dbPath)
	prefix := filepath.Base(dbPath) + BackupInfix

	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("list backups: %w", err)
	}

	var backups []string

	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			backups = append(backups, entry.Name())
		}
	}

	// Names embed a sortable UTC timestamp, so lexical order is chronological.
	slices.Sort(backups)

	for len(backups) > keep {
		if err = os.Remove(filepath.Join(dir, backups[0])); err != nil {
			return fmt.Errorf("prune old backup: %w", err)
		}

		backups = backups[1:]
	}

	return nil
}

// applyPending applies each pending migration in order, recording progress in result.
// A migration that fails is reported as ErrMigrationFailed.
func applyPending(ctx context.Context, db *sql.DB, pending []Migration, opts Options, result Result) (Result, error) {
	for _, m := range pending {
		if err := applyOne(ctx, db, m, opts.Now()); err != nil {
			return result, fmt.Errorf("%w: %w", ErrMigrationFailed, err)
		}

		result.Applied = append(result.Applied, m.Version)
		result.To = m.Version
	}

	return result, nil
}

// applyOne runs one migration and records it, atomically.
func applyOne(ctx context.Context, db *sql.DB, m Migration, now time.Time) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("apply %s: %w", m.File, err)
	}

	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, m.SQL); err != nil {
		return fmt.Errorf("apply %s: %w", m.File, err)
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version, name, checksum, applied_at) VALUES (?, ?, ?, ?)`,
		m.Version, m.Name, m.Checksum, clock.Format(now))
	if err != nil {
		return fmt.Errorf("record %s: %w", m.File, err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit %s: %w", m.File, err)
	}

	return nil
}
