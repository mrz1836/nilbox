package testutil

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/store"
)

// errTemplateLog is returned when the template database's write-ahead log still holds
// pages after its last connection closed, so its file alone is not the whole database.
var errTemplateLog = errors.New("the template database's write-ahead log was not checkpointed")

// template returns the bytes of a database store.Open created and migrated. It is built
// once per test binary, so every database DB and DBAt create starts as a copy of it and a
// test pays for writing a file rather than for running every migration again.
var template = sync.OnceValues(buildTemplate) //nolint:gochecknoglobals // built once for the whole test binary

// buildTemplate migrates a database in a folder of its own, closes it, and returns the
// database file's bytes. Closing the last connection checkpoints the write-ahead log into
// the file and removes the log, so the file alone holds the whole schema; the folder is
// removed again before it returns.
func buildTemplate() (body []byte, err error) {
	dir, err := os.MkdirTemp("", "testutil-db-*")
	if err != nil {
		return nil, fmt.Errorf("create the template database's folder: %w", err)
	}

	defer func() { err = errors.Join(err, os.RemoveAll(dir)) }()

	path := filepath.Join(dir, "template.db")

	opened, err := store.Open(context.Background(), path)
	if err != nil {
		return nil, fmt.Errorf("migrate the template database: %w", err)
	}

	if err = opened.DB.Close(); err != nil {
		return nil, fmt.Errorf("close the template database: %w", err)
	}

	if info, statErr := os.Stat(path + "-wal"); statErr == nil && info.Size() > 0 {
		return nil, errTemplateLog
	}

	if body, err = os.ReadFile(path); err != nil { //nolint:gosec // the template's own temp file
		return nil, fmt.Errorf("read the template database: %w", err)
	}

	return body, nil
}

// writeTemplate creates the database file at path, which does not exist yet, and its
// folder, as store.Open would, holding a copy of the migrated template.
func writeTemplate(t testing.TB, path string) {
	t.Helper()

	body, err := template()
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll(filepath.Dir(path), appdata.PrivateDirPerm))

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, appdata.PrivateFilePerm) //nolint:gosec // the test's own database path
	require.NoError(t, err)

	_, err = f.Write(body)
	require.NoError(t, errors.Join(err, f.Close()))
}

// DB opens a fresh, migrated database in a temporary directory and closes it when the
// test ends.
func DB(t testing.TB) *sql.DB {
	t.Helper()

	return DBAt(t, filepath.Join(t.TempDir(), "app.db"))
}

// DBAt opens the database at path, creating and migrating it when needed, and closes it
// when the test ends. A test of what survives a restart opens the same path twice. The
// test fails if the database at path could not be used and was set aside.
//
// A database that does not exist yet starts as a copy of one migrated once per test
// binary, which store.Open then opens as it would any migrated database, checking its
// migration history; an existing file is opened as it is.
func DBAt(t testing.TB, path string) *sql.DB {
	t.Helper()

	if _, err := os.Lstat(path); errors.Is(err, fs.ErrNotExist) {
		writeTemplate(t, path)
	}

	opened, err := store.Open(t.Context(), path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = opened.DB.Close() })
	require.Nil(t, opened.Reset, "the database at %s was set aside", path)

	return opened.DB
}

// ClosedDB returns a migrated database that is already closed, so every query on it
// fails, for testing how code reports a database it cannot use.
func ClosedDB(t testing.TB) *sql.DB {
	t.Helper()

	db := DB(t)
	require.NoError(t, db.Close())

	return db
}
