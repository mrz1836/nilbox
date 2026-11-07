// Package store opens the application's local SQLite database.
//
// The database uses GORM over a pure-Go SQLite driver, so it builds without CGO. Open
// enables write-ahead logging, foreign keys, and a busy timeout on every connection, then
// brings the schema up to date with the checksummed migrations in the migrate package.
// The schema is never created from the models: AutoMigrate is not used anywhere.
package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/store/migrate"
)

const (
	// pragmas run on every new connection.
	pragmas = "_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)"
)

var (
	// ErrEmptyPath is returned when no database path is given.
	ErrEmptyPath = errors.New("database path is empty")

	// ErrInvalidPath is returned for a path the driver cannot open as a plain file name.
	ErrInvalidPath = errors.New("database path must not contain '?'")
)

// Open opens (creating if needed) the database at path and applies pending migrations.
// Timestamps that GORM sets are in UTC.
func Open(ctx context.Context, path string) (*gorm.DB, error) {
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

	db, err := gorm.Open(sqlite.Open(path+"?"+pragmas), &gorm.Config{
		Logger:  logger.Discard, // never log SQL or its arguments
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if _, err = migrate.Apply(ctx, sqlDB, path, migrate.Options{}); err != nil {
		_ = sqlDB.Close()

		return nil, fmt.Errorf("migrate database: %w", err)
	}

	return db, nil
}

// Close closes the database's underlying connection pool.
func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("close database: %w", err)
	}

	if err = sqlDB.Close(); err != nil {
		return fmt.Errorf("close database: %w", err)
	}

	return nil
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
