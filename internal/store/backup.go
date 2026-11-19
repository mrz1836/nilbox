package store

import (
	"bytes"
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/store/migrate"
)

// sqliteHeader starts every SQLite database file.
const sqliteHeader = "SQLite format 3\x00"

// ErrNotABackup is returned by RestoreBackup for a file that isn't one of the database's
// own backups: not beside it, not named as one, not a regular file, or not a database.
var ErrNotABackup = errors.New("not a backup of the database")

// Backup copies the open database db, whose file is path, to a backup beside it, named and
// pruned as the backups taken before migrations are: <path>.backup-<UTC stamp>-v<schema>,
// schema being the newest migration the database has, with the newest three of those kept,
// the migrations' and these together, so Snapshots lists it as a SnapshotBackup. It
// returns the backup's path. The copy is taken with VACUUM INTO, a whole database whatever
// is written meanwhile, readable only by the user.
func Backup(ctx context.Context, db *sql.DB, path string) (string, error) {
	backup, err := migrate.Backup(ctx, db, path, migrate.Options{})
	if err != nil {
		return "", fmt.Errorf("back up the database: %w", err)
	}

	return backup, nil
}

// Schemas says which databases this build opens, by the newest migration each records, as
// a backup's name gives it (SnapshotFile.Schema).
type Schemas = migrate.Schemas

// KnownSchemas returns the Schemas of this build's migrations. A database whose newest is
// past Newest was saved by a newer build, which Open refuses (ErrNewerSchema); a backup
// whose schema Opens opens here, brought up to date, carried over first when a squash froze
// its history.
func KnownSchemas() (Schemas, error) {
	known, err := migrate.KnownSchemas(migrate.Options{})
	if err != nil {
		return Schemas{}, fmt.Errorf("read the migrations: %w", err)
	}

	return known, nil
}

// RestoreBackup puts backup, one of the backups beside the database at path (a
// SnapshotBackup, as Backup and the migrations take), in the database's place. The backup
// is first copied, not moved, into a temporary file beside the database, so the backup
// stays as it was and the slow part changes nothing; only then is the database there now,
// with its -wal and -shm files, set aside under the name Open gives one it can't use,
// <path>.broken-<UTC stamp>, and kept until the user removes it, and the copy renamed into
// its place. It returns where the database was set aside, or "" when there was none.
//
// Nothing may have the database open: it waits a moment for another connection to let go,
// as a copy of the app that is quitting, and fails otherwise, moving nothing. When the
// rename fails, the database is put back. Whatever fails, the copy is removed. The next
// Open opens the restored database and brings it up to date with the migrations.
func RestoreBackup(ctx context.Context, path, backup string) (string, error) {
	return restoreBackup(ctx, path, backup, options{})
}

// restoreBackup is RestoreBackup with opts: the time the database is set aside at, how long
// to wait for another connection to let go of it, and how the copy is renamed into place.
func restoreBackup(ctx context.Context, path, backup string, opts options) (string, error) {
	if err := checkBackup(path, backup); err != nil {
		return "", err
	}

	opts = opts.withDefaults()

	tmp, err := copyBeside(backup, path)
	if err != nil {
		return "", err
	}

	// After the rename, the copy is the database: removing tmp then finds nothing.
	defer func() { _ = os.Remove(tmp) }()

	_, err = os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", renameIntoEmpty(opts, tmp, path)
	}

	if err != nil {
		return "", fmt.Errorf("restore the backup: %w", err)
	}

	if err = waitUnshared(ctx, path, opts); err != nil {
		return "", fmt.Errorf("restore the backup: %w", err)
	}

	moved, err := setAside(path, opts.now(), asideInfix)
	if err != nil {
		return "", fmt.Errorf("restore the backup: %w", err)
	}

	if err = renameInto(opts, tmp, path); err != nil {
		if putErr := moved.putBack(); putErr != nil {
			return "", fmt.Errorf("%w; put the database back from %s: %w", err, moved.target, putErr)
		}

		return "", err
	}

	return moved.target, nil
}

// renameIntoEmpty renames the copy tmp to path, where no database is. A -wal or -shm left
// there is of a database that is gone, and would be read with the copy as if it were the
// copy's own log, so it is removed first.
func renameIntoEmpty(opts options, tmp, path string) error {
	var errs []error

	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(path + suffix); err != nil && !errors.Is(err, fs.ErrNotExist) {
			errs = append(errs, fmt.Errorf("restore the backup: remove a log with no database: %w", err))
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return renameInto(opts, tmp, path)
}

const (
	// restoreCopyPrefix and restoreCopySuffix frame the name of the copy a restore makes
	// beside the database: .<base>.restore-<digits>.tmp, as os.CreateTemp fills in its
	// pattern.
	restoreCopyPrefix = ".restore-"
	restoreCopySuffix = ".tmp"

	// staleRestoreAfter is how old a copy a restore left beside the database must be
	// before the next open removes it: older than any restore still copying.
	staleRestoreAfter = 5 * time.Minute
)

// sweepRestoreCopies removes the copies of a backup that a restore cut short (a crash, a
// power cut) left beside the database at path, each as large as the database: named
// exactly as copyBeside names them, regular files, and last written staleRestoreAfter
// before now or earlier, so a restore under way in another copy of the app keeps its own.
// A copy it can't remove is left for the next open: none holds anything the app needs.
func sweepRestoreCopies(path string, now time.Time) {
	dir, base := filepath.Split(filepath.Clean(path))
	dir = cmp.Or(dir, ".")

	listed, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, item := range listed {
		if !isRestoreCopy(base, item.Name()) || !item.Type().IsRegular() {
			continue
		}

		info, err := item.Info()
		if err != nil || now.Sub(info.ModTime()) < staleRestoreAfter {
			continue
		}

		_ = os.Remove(filepath.Join(dir, item.Name()))
	}
}

// isRestoreCopy reports whether name is a copy a restore makes beside the database named
// base: .<base>.restore-<digits>.tmp.
func isRestoreCopy(base, name string) bool {
	middle, ok := strings.CutPrefix(name, "."+base+restoreCopyPrefix)
	if !ok {
		return false
	}

	digits, ok := strings.CutSuffix(middle, restoreCopySuffix)
	if !ok || digits == "" {
		return false
	}

	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}

	return true
}

// renameInto renames the copy tmp to path with opts' rename.
func renameInto(opts options, tmp, path string) error {
	if err := opts.rename(tmp, path); err != nil {
		return fmt.Errorf("restore the backup: %w", err)
	}

	return nil
}

// checkBackup refuses a backup that isn't a regular file named as one of the database's
// backups, beside it, starting with SQLite's header.
func checkBackup(path, backup string) error {
	dir, base := filepath.Split(filepath.Clean(path))
	backupDir, name := filepath.Split(filepath.Clean(backup))

	file, ok := snapshotFile(base, name)
	if !ok || file.Kind != SnapshotBackup || filepath.Clean(backupDir) != filepath.Clean(dir) {
		return fmt.Errorf("%w: %s", ErrNotABackup, name)
	}

	info, err := os.Lstat(backup)
	if err != nil {
		return fmt.Errorf("read the backup: %w", err)
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("%w: %s is not a file", ErrNotABackup, name)
	}

	head, err := readHead(backup, len(sqliteHeader))
	if err != nil {
		return err
	}

	if !bytes.Equal(head, []byte(sqliteHeader)) {
		return fmt.Errorf("%w: %s is not a database", ErrNotABackup, name)
	}

	return nil
}

// readHead reads the first n bytes of the file at path, or fewer when it is shorter.
func readHead(path string, n int) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // one of the database's own backups, beside it
	if err != nil {
		return nil, fmt.Errorf("read the backup: %w", err)
	}

	defer func() { _ = f.Close() }()

	head := make([]byte, n)

	read, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("read the backup: %w", err)
	}

	return head[:read], nil
}

// copyBeside copies the file at from into a user-only temporary file beside path, flushed
// to disk, and returns its path. A copy that fails leaves nothing.
func copyBeside(from, path string) (string, error) {
	src, err := os.Open(from) //nolint:gosec // one of the database's own backups, beside it
	if err != nil {
		return "", fmt.Errorf("restore the backup: %w", err)
	}

	defer func() { _ = src.Close() }()

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+restoreCopyPrefix+"*"+restoreCopySuffix)
	if err != nil {
		return "", fmt.Errorf("restore the backup: %w", err)
	}

	name := tmp.Name()
	fail := func(err error) (string, error) {
		return "", errors.Join(fmt.Errorf("restore the backup: %w", err), tmp.Close(), os.Remove(name))
	}

	if err = tmp.Chmod(appdata.PrivateFilePerm); err != nil {
		return fail(err)
	}

	if _, err = io.Copy(tmp, src); err != nil {
		return fail(err)
	}

	if err = tmp.Sync(); err != nil {
		return fail(err)
	}

	if err = tmp.Close(); err != nil {
		return "", errors.Join(fmt.Errorf("restore the backup: %w", err), os.Remove(name))
	}

	return name, nil
}
