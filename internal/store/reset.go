package store

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	sqlite3 "modernc.org/sqlite/lib"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/store/migrate"
)

const (
	// asideInfix sits between a database's path and the stamp of when Open set it aside,
	// as one it could not use.
	asideInfix = ".broken-"

	// resetInfix sits between a database's path and the stamp of when OpenFresh set it
	// aside, as the user asked.
	resetInfix = ".reset-"

	// KeepResetCopies is how many of the databases OpenFresh set aside PruneResetCopies
	// keeps: the newest.
	KeepResetCopies = 3

	// maxAsideNameAttempts bounds the search for a free name to set a database aside
	// under.
	maxAsideNameAttempts = 100
)

// ErrNoAsideName is returned when every name a database could be set aside under is
// taken.
var ErrNoAsideName = errors.New("no free name to set the database aside under")

// ResetReason says why a database was set aside: Open could not use it, or the user asked
// for a fresh one (OpenFresh).
type ResetReason string

const (
	// ResetCorrupt means the file is not an SQLite database, or it is damaged.
	ResetCorrupt ResetReason = "corrupt"

	// ResetUnmanaged means the database holds tables the app's migrations did not
	// create.
	ResetUnmanaged ResetReason = "unmanaged"

	// ResetHistory means the database's migration history disagrees with this build's:
	// a migration it records has changed since it ran, one older than its newest never
	// ran, or it records migrations this build squashed in a history it can't carry over.
	ResetHistory ResetReason = "history"

	// ResetRequested means the user asked to start over: the database could be used, and
	// OpenFresh set it aside for a fresh one.
	ResetRequested ResetReason = "requested"
)

// Reset reports a database set aside: one Open could not use, or one OpenFresh set aside
// as the user asked. The database was renamed, with its -wal and -shm files, and a fresh
// one was created at the original path.
type Reset struct {
	// Path is where the database is now. Its -wal and -shm files, when it had them, sit
	// beside it under the same name with those suffixes, so SQLite still opens it whole.
	Path string

	// Reason says why it was set aside.
	Reason ResetReason

	// Err is the error opening or migrating it returned, or nil for a reset the user
	// asked for.
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
	case errors.Is(err, migrate.ErrChecksumMismatch), errors.Is(err, migrate.ErrOutOfOrder),
		errors.Is(err, migrate.ErrSquashedHistory):
		return ResetHistory, true
	}

	switch sqliteCode(err) {
	case sqlite3.SQLITE_NOTADB, sqlite3.SQLITE_CORRUPT:
		return ResetCorrupt, true
	default:
		return "", false
	}
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
// to a free <path><infix><UTC stamp> name: <path>.broken-<stamp> for one Open could not
// use, <path>.reset-<stamp> for one the user asked to start over from. When a rename
// fails, the files already moved are put back.
func setAside(path string, now time.Time, infix string) (aside, error) {
	target, err := freeAsideName(path + infix + clock.Stamp(now))
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

// PruneResetCopies deletes the databases OpenFresh set aside beside the database at path,
// with their -wal and -shm files, but the newest KeepResetCopies, so a reset the user asks
// for again and again never fills the disk. The databases Open set aside, as ones it could
// not use, are never deleted: the user may still want what they hold. A file it cannot
// delete is named in the error, and the rest are still deleted.
func PruneResetCopies(path string) error {
	dir, base := filepath.Split(path)

	copies, err := resetCopies(cmp.Or(dir, "."), base)
	if err != nil {
		return err
	}

	var errs []error

	for len(copies) > KeepResetCopies {
		errs = append(errs, removeCopy(filepath.Join(dir, copies[0]))...)
		copies = copies[1:]
	}

	return errors.Join(errs...)
}

// resetCopies lists the names of the databases OpenFresh set aside in dir beside the
// database named base, the oldest first: the stamp sorts as text in time order, and a name
// taken twice in one second gets a numeric suffix, which sorts after the plain one.
func resetCopies(dir, base string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("list the databases set aside by a reset: %w", err)
	}

	var copies []string

	for _, e := range entries {
		if file, ok := snapshotFile(base, e.Name()); ok && file.Kind == SnapshotReset && e.Type().IsRegular() {
			copies = append(copies, e.Name())
		}
	}

	slices.Sort(copies)

	return copies, nil
}

// removeCopy deletes the database set aside at path, with its -wal and -shm files, and
// returns what it could not delete.
func removeCopy(path string) []error {
	var errs []error

	for _, suffix := range fileSuffixes() {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("delete an old database set aside by a reset: %w", err))
		}
	}

	return errs
}
