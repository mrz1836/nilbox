package store

import (
	"context"
	"io/fs"
	"time"
)

// SetAside sets the database at path aside, as Open does when it cannot use one, and
// returns where it went.
func SetAside(path string, now time.Time) (string, error) {
	moved, err := setAside(path, now)

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
