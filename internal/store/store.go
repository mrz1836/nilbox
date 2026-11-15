// Package store opens the application's local SQLite database.
//
// The database is opened with database/sql over the pure-Go modernc.org/sqlite driver,
// so it builds without CGO; no other package imports the driver. Every pooled connection runs in write-ahead-logging mode with foreign keys
// on and a busy timeout, and begins its transactions IMMEDIATE, so a transaction that
// reads and then writes waits for the write lock up front instead of failing part way.
// Open then brings the schema up to date with the checksummed migrations in the migrate
// package, backing up a migrated database before it changes it. The schema comes from
// those migrations alone.
//
// The store owns no table. Each table's owner package queries it through code sqlc
// generates from the owner's SQL, on the pool Open returns or in a transaction from
// InTx; the store gives every owner DBTX, the JSON column codecs, and
// IsForeignKeyViolation.
//
// Timestamps are written as UTC text in one sortable format,
// "2006-01-02 15:04:05.999999999+00:00", whatever a time.Time's location, so the
// database orders and compares them as text in time order. DATETIME columns read back as
// time.Time values in UTC.
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
// A database the app could use may be set aside too, when the user asks to start over:
// OpenFresh renames it, with its -wal and -shm files, to <path>.reset-<UTC stamp> once no
// other connection has it open, waiting a few seconds for a copy of the app that is still
// quitting, and starts a fresh database in its place, which Opened.Reset says with
// ResetRequested. PruneResetCopies keeps the newest few of those.
//
// Backup takes a backup of the open database whenever asked, as the app does before it
// installs an update, named and pruned as the migrations' own backups are, and
// RestoreBackup puts one of those backups back in the database's place, setting the
// database there aside as one Open could not use, so an older build can go back to the
// data from before a newer one migrated it.
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

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/clock"
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

	// dsnParams configure every new connection. _txlock begins every transaction
	// IMMEDIATE. _time_format writes a time.Time as "2006-01-02 15:04:05.999999999-07:00"
	// text, the format the stored timestamps use, and _timezone converts it to UTC first
	// and reads DATETIME columns back in UTC, so every stored stamp ends in +00:00 and
	// sorts as text in time order.
	dsnParams = "_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)" +
		"&_txlock=immediate&_time_format=sqlite&_timezone=UTC"

	// maxConns caps the pool, and every one of those connections is kept open when idle.
	// SQLite allows one writer at a time, so more connections would only queue on the
	// write lock; a few let reads run beside a write.
	maxConns = 4

	// freshBusyTries and freshBusyPause bound how long OpenFresh waits for another
	// connection to let go of the database: each check waits for the lock as the probe
	// does (a quarter second), then pauses as long, so about five seconds in all, as long
	// as a copy of the app that is quitting waits for its work before it closes the
	// database.
	freshBusyTries = 10
	freshBusyPause = 250 * time.Millisecond
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

	// Reset is set when the database at the path was set aside for a fresh, empty one:
	// Open could not use it, or OpenFresh set it aside as the user asked. It is nil
	// otherwise.
	Reset *Reset
}

// options tune open. The zero value is what Open uses.
type options struct {
	// migrations holds the migrations to apply. Nil means the embedded ones.
	migrations fs.FS

	// now stamps the name a database is set aside under. Nil means time.Now.
	now func() time.Time

	// busyTries is how many times OpenFresh checks that no other connection has the
	// database open before it gives up. Zero means freshBusyTries.
	busyTries int

	// busyWait waits between those checks. Nil waits freshBusyPause on the real clock.
	busyWait func(ctx context.Context) error

	// rename renames a restored copy into the database's place. Nil is os.Rename.
	rename func(oldPath, newPath string) error
}

// Open opens (creating if needed) the database at path and applies pending migrations,
// backing up a migrated database first. A database it cannot use is set aside for a
// fresh one, which Opened.Reset reports; see the package documentation for which
// failures that covers. The caller closes Opened.DB.
func Open(ctx context.Context, path string) (Opened, error) {
	return open(ctx, path, options{})
}

// OpenFresh starts over, as the user asked: it sets the database at path aside as
// <path>.reset-<UTC stamp>, with its -wal and -shm files, and opens a fresh, migrated one
// in its place, which Opened.Reset reports with ResetRequested and no error. It moves
// nothing while another connection has the database open, waiting about five seconds for
// one that is letting go, as a copy of the app that is quitting does, and then returns an
// error IsBusy tells apart; it puts the files back when the fresh database cannot be
// opened. With no database at path there is nothing to set aside, and it opens one as
// Open does. The caller closes Opened.DB.
func OpenFresh(ctx context.Context, path string) (Opened, error) {
	return openFresh(ctx, path, options{})
}

// open is Open with its migrations and clock chosen by opts.
func open(ctx context.Context, path string, opts options) (Opened, error) {
	if err := checkPath(path); err != nil {
		return Opened{}, err
	}

	opts = opts.withDefaults()

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

// openFresh is OpenFresh with its migrations, clock, and waits chosen by opts.
func openFresh(ctx context.Context, path string, opts options) (Opened, error) {
	if err := checkPath(path); err != nil {
		return Opened{}, err
	}

	opts = opts.withDefaults()

	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		// Setting aside an empty file the open would create holds nothing to bring back.
		return open(ctx, path, opts)
	} else if err != nil {
		return Opened{}, fmt.Errorf("start a fresh database: %w", err)
	}

	if err := waitUnshared(ctx, path, opts); err != nil {
		return Opened{}, fmt.Errorf("start a fresh database: set the old one aside: %w", err)
	}

	opened, err := replace(ctx, path, opts, resetInfix, Reset{Reason: ResetRequested})
	if err != nil {
		return Opened{}, fmt.Errorf("start a fresh database: %w", err)
	}

	return opened, nil
}

// checkPath returns an error for a path the database cannot be opened at.
func checkPath(path string) error {
	if path == "" {
		return ErrEmptyPath
	}

	// The driver splits the DSN at the first '?', so such a path would be truncated.
	if strings.Contains(path, "?") {
		return ErrInvalidPath
	}

	return nil
}

// withDefaults returns o with every unset option at its default.
func (o options) withDefaults() options {
	if o.now == nil {
		o.now = time.Now
	}

	if o.busyTries <= 0 {
		o.busyTries = freshBusyTries
	}

	if o.busyWait == nil {
		o.busyWait = func(ctx context.Context) error { return clock.Sleep(ctx, freshBusyPause) }
	}

	if o.rename == nil {
		o.rename = os.Rename
	}

	return o
}

// startFresh sets aside the database at path, which reset says cannot be used, and
// opens a fresh one in its place. It moves nothing while another connection has the
// database open, and puts the files back when the fresh database cannot be opened.
func startFresh(ctx context.Context, path string, opts options, reset Reset) (Opened, error) {
	if err := checkUnshared(ctx, path); err != nil {
		return Opened{}, fmt.Errorf("%w; set it aside: %w", reset.Err, err)
	}

	opened, err := replace(ctx, path, opts, asideInfix, reset)
	if err != nil {
		return Opened{}, fmt.Errorf("%w; %w", reset.Err, err)
	}

	return opened, nil
}

// replace renames the database at path to a name under infix and opens a fresh one in its
// place, which reset, with its new Path, reports. It puts the files back when the fresh
// database cannot be opened. The caller has checked no other connection has it open.
func replace(ctx context.Context, path string, opts options, infix string, reset Reset) (Opened, error) {
	moved, err := setAside(path, opts.now(), infix)
	if err != nil {
		return Opened{}, fmt.Errorf("set it aside: %w", err)
	}

	reset.Path = moved.target

	db, err := openMigrated(ctx, path, opts)
	if err != nil {
		err = fmt.Errorf("start a fresh database in its place: %w", err)

		if putErr := moved.putBack(); putErr != nil {
			return Opened{}, fmt.Errorf("%w; put it back from %s: %w", err, reset.Path, putErr)
		}

		return Opened{}, err
	}

	return Opened{DB: db, Reset: &reset}, nil
}

// waitUnshared returns once this process alone has the database at path open, as
// checkUnshared finds, checking again while another connection has it, up to opts'
// busyTries, with opts' busyWait between checks. Any other failure returns at once.
func waitUnshared(ctx context.Context, path string, opts options) error {
	for try := 1; ; try++ {
		err := checkUnshared(ctx, path)
		if err == nil || !IsBusy(err) || try >= opts.busyTries {
			return err
		}

		if err = opts.busyWait(ctx); err != nil {
			return fmt.Errorf("wait for another copy of the app to let go of the database: %w", err)
		}
	}
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
