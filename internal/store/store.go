// Package store opens the application's local SQLite database.
//
// The database is opened with database/sql over a pure-Go SQLite driver, so it builds
// without CGO. Every pooled connection runs in write-ahead-logging mode with foreign keys
// on and a busy timeout, and begins its transactions IMMEDIATE, so a transaction that
// reads and then writes waits for the write lock up front instead of failing part way.
// Open then brings the schema up to date with the checksummed migrations in the migrate
// package, backing up a migrated database before it changes it. The schema is never
// created from the models: AutoMigrate is not used anywhere.
//
// Before the pool connects, Open reads the migration history over a connection that
// writes nothing, so a database it refuses or sets aside has not been changed by the
// attempt. It recovers from a database it cannot use, so the app starts whenever it
// safely can:
//
//   - A file that is not a database or is damaged, a schema the migrations did not
//     create, or a migration history this build disagrees with is set aside: once Open
//     has shown that no other connection has the file open, the file and its -wal and
//     -shm files are renamed to <path>.broken-<UTC stamp>, a fresh database is created
//     in its place, and Opened.Reset says what happened. When the fresh database cannot
//     be created either, the files are put back and the error is returned.
//   - A database a newer build migrated is refused with ErrNewerSchema, whose message
//     asks the user to update the app, and is left exactly as it is.
//   - Anything else, such as a database another process has open, a permission error,
//     a full disk, or a migration that failed after its backup, is returned as an
//     error, and the file is never moved.
//
// Code that reads and writes through GORM gets a handle over the same pool from Gorm.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/glebarez/go-sqlite" // registers the "sqlite" database/sql driver
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/store/migrate"
)

const (
	// driverName is the database/sql driver the database is opened with.
	driverName = "sqlite"

	// inspectParams configure the connection that reads the migration history before
	// the pool opens: it waits for a lock as the pool does, and writes nothing, not even
	// the journal mode.
	inspectParams = "_pragma=busy_timeout(5000)&_pragma=query_only(1)"

	// probeParams configure the connection that checks no other connection has the
	// database open before it is set aside. In exclusive locking mode, reading the
	// database takes a lock that any other open connection blocks, in WAL mode too, where
	// another connection's reads and writes never block the pool's. Another process that
	// has the database open keeps it open, so the probe waits only a moment, for one
	// that is just letting go.
	probeParams = "_pragma=busy_timeout(250)&_pragma=locking_mode(EXCLUSIVE)"

	// dsnParams configure every new connection. The driver writes a time.Time as
	// "2006-01-02 15:04:05.999999999-07:00" text by default, which is the format the
	// stored timestamps use. Asking for that format by name would make this driver
	// version skip the _txlock parameter.
	dsnParams = "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_txlock=immediate"

	// maxConns caps the pool, and every one of those connections is kept open when idle.
	// SQLite allows one writer at a time, so more connections would only queue on the
	// write lock; a few let reads run beside a write.
	maxConns = 4
)

var (
	// ErrEmptyPath is returned when no database path is given.
	ErrEmptyPath = errors.New("database path is empty")

	// ErrInvalidPath is returned for a path the driver cannot open as a plain file name.
	ErrInvalidPath = errors.New("database path must not contain '?'")

	// ErrNewerSchema is returned when a newer build of the app has migrated the database.
	// Its message asks the user to update the app; the database is left as it is.
	ErrNewerSchema = migrate.ErrNewerSchema
)

// Opened is an open, migrated database.
type Opened struct {
	// DB is the database's connection pool. The caller closes it.
	DB *sql.DB

	// Reset is set when the database at the path could not be used and was set aside
	// for a fresh, empty one, and is nil otherwise.
	Reset *Reset
}

// options tune open. The zero value is what Open uses.
type options struct {
	// migrations holds the migrations to apply. Nil means the embedded ones.
	migrations fs.FS

	// now stamps the name a database is set aside under. Nil means time.Now.
	now func() time.Time
}

// Open opens (creating if needed) the database at path and applies pending migrations,
// backing up a migrated database first. A database it cannot use is set aside for a
// fresh one, which Opened.Reset reports; see the package documentation for which
// failures that covers. The caller closes Opened.DB.
func Open(ctx context.Context, path string) (Opened, error) {
	return open(ctx, path, options{})
}

// open is Open with its migrations and clock chosen by opts.
func open(ctx context.Context, path string, opts options) (Opened, error) {
	if path == "" {
		return Opened{}, ErrEmptyPath
	}

	// The driver splits the DSN at the first '?', so such a path would be truncated.
	if strings.Contains(path, "?") {
		return Opened{}, ErrInvalidPath
	}

	if opts.now == nil {
		opts.now = time.Now
	}

	db, err := openMigrated(ctx, path, opts)
	if err == nil {
		return Opened{DB: db}, nil
	}

	reason, unusable := resetReason(err)
	if !unusable {
		return Opened{}, err
	}

	return startFresh(ctx, path, opts, Reset{Reason: reason, Err: err})
}

// startFresh sets aside the database at path, which reset says cannot be used, and
// opens a fresh one in its place. It moves nothing while another connection has the
// database open, and puts the files back when the fresh database cannot be opened.
func startFresh(ctx context.Context, path string, opts options, reset Reset) (Opened, error) {
	if err := checkUnshared(ctx, path); err != nil {
		return Opened{}, fmt.Errorf("%w; set it aside: %w", reset.Err, err)
	}

	moved, err := setAside(path, opts.now())
	if err != nil {
		return Opened{}, fmt.Errorf("%w; set it aside: %w", reset.Err, err)
	}

	reset.Path = moved.target

	db, err := openMigrated(ctx, path, opts)
	if err != nil {
		err = fmt.Errorf("%w; start a fresh database in its place: %w", reset.Err, err)

		if putErr := moved.putBack(); putErr != nil {
			return Opened{}, fmt.Errorf("%w; put it back from %s: %w", err, reset.Path, putErr)
		}

		return Opened{}, err
	}

	return Opened{DB: db, Reset: &reset}, nil
}

// openMigrated opens the database at path, creating it when needed, and applies the
// pending migrations. On an error the pool is closed again.
func openMigrated(ctx context.Context, path string, opts options) (*sql.DB, error) {
	if err := createFile(path); err != nil {
		return nil, err
	}

	if err := inspect(ctx, path, opts); err != nil {
		return nil, err
	}

	db, err := sql.Open(driverName, path+"?"+dsnParams)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns)
	db.SetConnMaxLifetime(0)
	db.SetConnMaxIdleTime(0)

	// sql.Open connects lazily; connecting now reports a file that is not a database,
	// or a path that is a directory, as an open error rather than a migration one.
	if err = db.PingContext(ctx); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("open database: %w", err)
	}

	_, err = migrate.Apply(ctx, db, path, migrate.Options{Source: opts.migrations, Now: opts.now})
	if err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("migrate database: %w", err)
	}

	return db, nil
}

// inspect reads the database at path, over one connection that writes nothing, and
// returns the error migrating it would: a file that is not a database, an unmanaged
// schema, or a history the migrations disagree with. The pool's connections switch the
// database to WAL mode as they connect, so only a database that passes is opened with
// them.
func inspect(ctx context.Context, path string, opts options) error {
	db, err := sql.Open(driverName, path+"?"+inspectParams)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	defer func() { _ = db.Close() }()

	db.SetMaxOpenConns(1)

	if err = migrate.Check(ctx, db, migrate.Options{Source: opts.migrations}); err != nil {
		return fmt.Errorf("open database: %w", err)
	}

	return nil
}

// checkUnshared returns an error unless this process alone has the database at path
// open. It takes the database's exclusive lock on a connection of its own, reads the
// database to make sure the lock is held, and lets go before returning, so the files
// can be renamed: closing a connection after its files were renamed could remove the
// -wal file a fresh database at the same path then uses. A file that is not a
// database, or is damaged, fails that read with the lock held, which is as good.
func checkUnshared(ctx context.Context, path string) error {
	db, err := sql.Open(driverName, path+"?"+probeParams)
	if err != nil {
		return fmt.Errorf("check that no other process has the database open: %w", err)
	}

	defer func() { _ = db.Close() }()

	db.SetMaxOpenConns(1)

	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("check that no other process has the database open: %w", err)
	}

	defer func() { _ = conn.Close() }()

	if _, err = conn.ExecContext(ctx, "BEGIN EXCLUSIVE"); err == nil {
		var tables int

		err = conn.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_master").Scan(&tables)
		_, _ = conn.ExecContext(ctx, "ROLLBACK")
	}

	switch code := sqliteCode(err); {
	case err == nil, code == sqlite3.SQLITE_NOTADB, code == sqlite3.SQLITE_CORRUPT:
		return nil
	default:
		return fmt.Errorf("check that no other process has the database open: %w", err)
	}
}

// createFile creates the database's parent directory and an empty, user-only database
// file when they do not exist yet. An existing file is left as it is.
func createFile(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), appdata.PrivateDirPerm); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}

	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, appdata.PrivateFilePerm) //nolint:gosec // path is the app's own data file
	if errors.Is(err, os.ErrExist) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("create database file: %w", err)
	}

	if err = f.Close(); err != nil {
		return fmt.Errorf("create database file: %w", err)
	}

	return nil
}
