package desktop

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Restore is a backup of the data the startup dialog offers to put back, when a newer
// build saved the data this one can't open: the newest backup beside the database whose
// schema this build has.
type Restore struct {
	// Backup is the backup's file, beside the database.
	Backup string

	// At is when it was taken.
	At time.Time

	// Button is the dialog's button that puts it back: "Use my data from Nov 14, 3:02 PM".
	Button string

	// Detail is what the dialog adds under the failure's message: what putting it back keeps
	// and loses.
	Detail string
}

// RestoreOffer works out the backup the startup dialog offers to put back for err, from
// backups, the files kept beside the database (store.Snapshots), and known, the schemas
// this build opens (store.KnownSchemas): the newest backup whose schema is known and one
// this build opens and brings up to date, its own or a history a squash froze. ok is false
// unless err is a database a newer build saved (store.ErrNewerSchema) and there is such a
// backup. Times are worded in now's location, with the year only when it isn't now's.
func RestoreOffer(err error, backups []store.SnapshotFile, known store.Schemas, now time.Time) (Restore, bool) {
	if !errors.Is(err, store.ErrNewerSchema) {
		return Restore{}, false
	}

	var (
		best  store.SnapshotFile
		found bool
	)

	for _, b := range backups {
		if b.Kind != store.SnapshotBackup || !known.Opens(b.Schema) {
			continue
		}

		if !found || b.At.After(best.At) {
			best, found = b, true
		}
	}

	if !found {
		return Restore{}, false
	}

	at := best.At.In(now.Location())
	day, clock := at.Format("Jan 2"), at.Format("3:04 PM")

	if at.Year() != now.Year() {
		day = at.Format("Jan 2, 2006")
	}

	return Restore{
		Backup: best.Path,
		At:     best.At,
		Button: "Use my data from " + day + ", " + clock,
		Detail: "Or go back to your data as it was backed up on " + day + " at " + clock + ". What changed since " +
			"then won't be there: the newer data is kept beside it, and Restore from a backup on the Prompts " +
			"page can bring its prompts back.",
	}, true
}

// FindRestore is RestoreOffer over the backups beside the database at db as they are now,
// and the migrations this build has. A db of "" (the data couldn't be found) offers none;
// a folder that can't be listed offers none, with the error.
func FindRestore(err error, db string, now time.Time) (Restore, bool, error) {
	if db == "" || !errors.Is(err, store.ErrNewerSchema) {
		return Restore{}, false, nil
	}

	backups, listErr := store.Snapshots(db)
	if listErr != nil {
		return Restore{}, false, listErr //nolint:wrapcheck // store words its own failure
	}

	known, schemaErr := store.KnownSchemas()
	if schemaErr != nil {
		return Restore{}, false, schemaErr //nolint:wrapcheck // store words its own failure
	}

	r, ok := RestoreOffer(err, backups, known, now)

	return r, ok, nil
}

// Offering returns f with r offered beside it: its detail under the message, and its button.
func (f Failure) Offering(r Restore) Failure {
	f.Message += "\n\n" + r.Detail
	f.Restore = &r

	return f
}

// PutBack puts the backup r offers in place of the database at db, which a newer build
// saved: the newer database, with its -wal and -shm files, is set aside beside it, kept
// until the user removes it, and the backup is copied into its place (store.RestoreBackup).
// The next start opens it and brings it up to date. It returns where the newer database
// was set aside. A failure is worded for the dialog that says so (PutBackFailure words
// what to do): almost always the newer database is still in place, but when the copy
// couldn't be renamed into place and the newer database couldn't be put back either, no
// database is left there, and the newer one is the file set aside.
func PutBack(ctx context.Context, db string, r Restore) (string, error) {
	aside, err := store.RestoreBackup(ctx, db, r.Backup)
	if err != nil {
		return "", uierr.New("Your data couldn't be put back: "+uierr.Message(err), err)
	}

	return aside, nil
}

// PutBackFailure words err, a failure to put a backup back in place of the database at db,
// for the dialog that says so before the app quits, with what to do: quit the other copy
// that holds the data open; when no database is left at db, rename the newer one, set
// aside beside it, back into place; otherwise update the app to open the newer data.
func PutBackFailure(db string, err error) Failure {
	title := brand.Name + " couldn't put your data back"

	if store.IsBusy(err) {
		return Failure{
			Title: title,
			Message: "Another copy of " + brand.Name + " still has your data open, so nothing was changed. Quit it, " +
				"then open " + brand.Name + " again.",
		}
	}

	if aside, gone := setAsideOnly(db); gone {
		return Failure{
			Title: title,
			Message: "Your newer data was set aside as " + filepath.Base(aside) + ", and couldn't be put back, so " +
				"no data is in place. Your data is safe in that file, in " + filepath.Dir(db) + ". Rename it to " +
				filepath.Base(db) + ", then open " + brand.Name + " again.\n\n" + uierr.Message(err),
		}
	}

	return Failure{
		Title:   title,
		Message: uierr.Message(err) + "\n\nUpdate " + brand.Name + " to open the newer data.",
	}
}

// setAsideOnly reports whether no database is at db, and names the newest one set aside
// beside it, when there is one.
func setAsideOnly(db string) (string, bool) {
	if db == "" {
		return "", false
	}

	if _, err := os.Lstat(db); !errors.Is(err, fs.ErrNotExist) {
		return "", false
	}

	snapshots, err := store.Snapshots(db)
	if err != nil {
		return "", false
	}

	for _, s := range snapshots { // the newest first
		if s.Kind == store.SnapshotSetAside {
			return s.Path, true
		}
	}

	return "", false
}
