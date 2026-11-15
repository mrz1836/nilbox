package store

import (
	"context"
	"io/fs"
	"time"
)

// StaleRestoreAfter is how old a copy a restore left must be before an open removes it.
const StaleRestoreAfter = staleRestoreAfter

// SetAside sets the database at path aside, as Open does when it cannot use one, and
// returns where it went.
func SetAside(path string, now time.Time) (string, error) {
	moved, err := setAside(path, now, asideInfix)

	return moved.target, err
}

// ResetReasonOf says whether Open sets aside a database that failed to open with err,
// and why.
func ResetReasonOf(err error) (ResetReason, bool) {
	return resetReason(err)
}

// OpenWith opens the database at path as Open does, applying the migrations in src (nil
// for the embedded ones) and naming a database it sets aside by now.
func OpenWith(ctx context.Context, path string, src fs.FS, now func() time.Time) (Opened, error) {
	return open(ctx, path, options{migrations: src, now: now})
}

// RestoreBackupWith restores backup at path as RestoreBackup does, checking tries times
// that no other connection has the database open, and calling wait between the checks.
func RestoreBackupWith(ctx context.Context, path, backup string, tries int, wait func(context.Context) error,
) (string, error) {
	return restoreBackup(ctx, path, backup, options{busyTries: tries, busyWait: wait})
}

// RestoreBackupRenaming restores backup at path as RestoreBackup does, renaming the copy
// into place with rename.
func RestoreBackupRenaming(ctx context.Context, path, backup string, rename func(oldPath, newPath string) error,
) (string, error) {
	return restoreBackup(ctx, path, backup, options{rename: rename})
}

// OpenFreshWith starts over at path as OpenFresh does, naming the database it sets aside
// by now, checking tries times that no other connection has it open, and calling wait
// between the checks.
func OpenFreshWith(ctx context.Context, path string, now func() time.Time, tries int, wait func(context.Context) error,
) (Opened, error) {
	return openFresh(ctx, path, options{now: now, busyTries: tries, busyWait: wait})
}
