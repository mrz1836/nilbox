package autopilot

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/clock"
)

// File is the copy of the routines the app keeps in a plain file beside its database,
// autopilot.json, in the export format (see Bundle). The database stays the routines'
// home; the file is written whole after every change to a routine, so the routines
// outlive a database that is lost, damaged, or set aside: Restore brings them back from
// it. A person can read it, copy it, or keep it in their own backups, since it is plain
// JSON readable only by them.
type File struct {
	path  string
	app   string
	clock clock.Clock

	// mu makes each Save read the routines and write the file before the next one starts,
	// so the file always ends up holding the latest routines.
	mu sync.Mutex
}

// NewFile returns the copy at path, which records app as the version that wrote it and
// stamps it from c (nil for the system clock).
func NewFile(path, app string, c clock.Clock) *File {
	return &File{path: path, app: app, clock: clock.Or(c)}
}

// Path returns where the file is.
func (f *File) Path() string {
	return f.path
}

// Save writes every routine not deleted in db to the file, in the user's order, replacing
// it in one step.
func (f *File) Save(ctx context.Context, db *sql.DB) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	rows, err := queries.New(db).ListRoutines(ctx)
	if err != nil {
		return fmt.Errorf("read the routines: %w", err)
	}

	data, err := EncodeBundle(bundleOf(rows, f.app, f.clock.Now()))
	if err != nil {
		return err
	}

	return appdata.WriteFile(f.path, data)
}

// Load reads the file. A file that is not there is an error matching fs.ErrNotExist.
func (f *File) Load() (Bundle, error) {
	data, err := appdata.ReadFile(f.path, MaxBundleBytes)
	if err != nil {
		return Bundle{}, err
	}

	return DecodeBundle(data)
}
