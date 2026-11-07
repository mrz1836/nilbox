package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"time"

	"github.com/glebarez/go-sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/store/migrate"
)

const (
	// asideInfix sits between a database's path and the stamp of when it was set aside.
	asideInfix = ".broken-"

	// maxAsideNameAttempts bounds the search for a free name to set a database aside
	// under.
	maxAsideNameAttempts = 100

	// primaryCodeMask keeps an SQLite result code's primary part: an extended code
	// carries it in its low byte.
	primaryCodeMask = 0xff
)

// ErrNoAsideName is returned when every name a database could be set aside under is
// taken.
var ErrNoAsideName = errors.New("no free name to set the database aside under")

// ResetReason says why Open could not use a database and set it aside.
type ResetReason string

const (
	// ResetCorrupt means the file is not an SQLite database, or it is damaged.
	ResetCorrupt ResetReason = "corrupt"

	// ResetUnmanaged means the database holds tables the app's migrations did not
	// create.
	ResetUnmanaged ResetReason = "unmanaged"

	// ResetHistory means the database's migration history disagrees with this build's:
	// a migration it records has changed since it ran, or one older than its newest
	// never ran.
	ResetHistory ResetReason = "history"
)

// Reset reports a database Open could not use. The database was renamed, with its -wal
// and -shm files, and a fresh one was created at the original path.
type Reset struct {
	// Path is where the database is now. Its -wal and -shm files, when it had them, sit
	// beside it under the same name with those suffixes, so SQLite still opens it whole.
	Path string

	// Reason says why it could not be used.
	Reason ResetReason

	// Err is the error opening or migrating it returned.
	Err error
}

// resetReason reports whether err, from opening or migrating a database, means the
// database cannot be used at all, and why. Only those databases are set aside. A newer
// build's database is not among them, and neither is a failure that says nothing about
// the file itself, such as a lock, a permission, or a full disk, nor a migration that
// failed to apply, whatever its error: the database was backed up before it ran.
func resetReason(err error) (ResetReason, bool) {
	switch {
	case errors.Is(err, migrate.ErrMigrationFailed), errors.Is(err, migrate.ErrNewerSchema):
		return "", false
	case errors.Is(err, migrate.ErrUnmanagedSchema):
		return ResetUnmanaged, true
	case errors.Is(err, migrate.ErrChecksumMismatch), errors.Is(err, migrate.ErrOutOfOrder):
		return ResetHistory, true
	}

	switch sqliteCode(err) {
	case sqlite3.SQLITE_NOTADB, sqlite3.SQLITE_CORRUPT:
		return ResetCorrupt, true
	default:
		return "", false
	}
}

// sqliteCode returns the primary SQLite result code of the driver error in err's chain,
// or SQLITE_OK when it holds none.
func sqliteCode(err error) int {
	var driverErr *sqlite.Error
	if !errors.As(err, &driverErr) {
		return sqlite3.SQLITE_OK
	}

	return driverErr.Code() & primaryCodeMask
}

// aside is a database set aside: its files renamed from path to target.
type aside struct {
	path   string
	target string

	// moved holds the suffixes of the files renamed, in the order they were.
	moved []string
}

// fileSuffixes are the suffixes of a database's files: its write-ahead log and
// shared-memory index first, then the database itself. They move in this order so a
// fresh database is never created beside an old log.
func fileSuffixes() [3]string { return [...]string{"-wal", "-shm", ""} }

// setAside renames the database at path, with its -wal and -shm files when it has them,
// to a free <path>.broken-<UTC stamp> name. When a rename fails, the files already moved
// are put back.
func setAside(path string, now time.Time) (aside, error) {
	target, err := freeAsideName(path + asideInfix + clock.Stamp(now))
	if err != nil {
		return aside{}, err
	}

	moved := aside{path: path, target: target}

	for _, suffix := range fileSuffixes() {
		err = os.Rename(path+suffix, target+suffix)
		if suffix != "" && errors.Is(err, fs.ErrNotExist) {
			continue
		}

		if err != nil {
			return aside{}, errors.Join(fmt.Errorf("set the database aside: %w", err), moved.rename())
		}

		moved.moved = append(moved.moved, suffix)
	}

	return moved, nil
}

// putBack removes the fresh database started at path, with its log files, and renames
// the set-aside files back, so the next start meets the database it could not use, and
// says so, again.
func (a aside) putBack() error {
	var errs []error

	for _, suffix := range fileSuffixes() {
		if err := os.Remove(a.path + suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("remove the fresh database: %w", err))
		}
	}

	return errors.Join(append(errs, a.rename())...)
}

// rename renames the files moved back to path.
func (a aside) rename() error {
	var errs []error

	for _, suffix := range a.moved {
		if err := os.Rename(a.target+suffix, a.path+suffix); err != nil {
			errs = append(errs, fmt.Errorf("put the database back: %w", err))
		}
	}

	return errors.Join(errs...)
}

// freeAsideName returns base, or base with a numeric suffix, such that neither it nor
// its -wal and -shm names exist.
func freeAsideName(base string) (string, error) {
	name := base

	for i := 1; i <= maxAsideNameAttempts; i++ {
		taken, err := anyExists(name, name+"-wal", name+"-shm")
		if err != nil {
			return "", err
		}

		if !taken {
			return name, nil
		}

		name = fmt.Sprintf("%s-%d", base, i)
	}

	return "", fmt.Errorf("%w: %s", ErrNoAsideName, base)
}

// anyExists reports whether any of names exists.
func anyExists(names ...string) (bool, error) {
	for _, name := range names {
		_, err := os.Lstat(name)
		if err == nil {
			return true, nil
		}

		if !errors.Is(err, fs.ErrNotExist) {
			return false, fmt.Errorf("check the name to set the database aside under: %w", err)
		}
	}

	return false, nil
}
