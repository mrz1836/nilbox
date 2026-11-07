// Package store opens the application's local SQLite database.
//
// The database is opened with database/sql over a pure-Go SQLite driver, so it builds
// without CGO. Every pooled connection runs in write-ahead-logging mode with foreign keys
// on and a busy timeout, and begins its transactions IMMEDIATE, so a transaction that
// reads and then writes waits for the write lock up front instead of failing part way.
// Open then brings the schema up to date with the checksummed migrations in the migrate
// package. The schema is never created from the models: AutoMigrate is not used
// anywhere.
//
// Code that reads and writes through GORM gets a handle over the same pool from Gorm.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "github.com/glebarez/go-sqlite" // registers the "sqlite" database/sql driver

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/store/migrate"
)

const (
	// driverName is the database/sql driver the database is opened with.
	driverName = "sqlite"

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
)

// Open opens (creating if needed) the database at path and applies pending migrations.
// The caller closes the returned pool.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	if path == "" {
		return nil, ErrEmptyPath
	}

	// The driver splits the DSN at the first '?', so such a path would be truncated.
	if strings.Contains(path, "?") {
		return nil, ErrInvalidPath
	}

	if err := createFile(path); err != nil {
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

	if _, err = migrate.Apply(ctx, db, path, migrate.Options{}); err != nil {
		_ = db.Close()

		return nil, fmt.Errorf("migrate database: %w", err)
	}

	return db, nil
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
