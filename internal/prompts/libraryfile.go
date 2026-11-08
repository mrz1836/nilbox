package prompts

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"sync"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/item"
)

// LibraryFile is the copy of the prompt library the app keeps in a plain file beside its
// database, in the prompt file format (see Bundle). The database stays the library's
// home; the file is written whole after every change to the library, so the prompts
// outlive a database that is lost, damaged, or set aside: Restore brings them back from
// it, and Import lists it among the backups. A person can read it, copy it, or keep it
// in their own backups, since it is plain JSON.
type LibraryFile struct {
	path  string
	app   string
	clock clock.Clock

	// mu makes each Save read the library and write the file before the next one starts,
	// so the file always ends up holding the latest library.
	mu sync.Mutex
}

// NewLibraryFile returns the library file at path, which records app as the version that
// wrote it and stamps it from c (nil for the system clock).
func NewLibraryFile(path, app string, c clock.Clock) *LibraryFile {
	return &LibraryFile{path: path, app: app, clock: clock.Or(c)}
}

// Path returns where the file is.
func (f *LibraryFile) Path() string {
	return f.path
}

// Save writes the whole library st holds to the file, replacing it in one step.
func (f *LibraryFile) Save(ctx context.Context, st *Store) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	lib, err := st.Library(ctx)
	if err != nil {
		return err
	}

	added, err := st.seeded(ctx)
	if err != nil {
		return err
	}

	data, err := encodeBundle(newBundle(lib, added, nil, f.clock.Now(), f.app))
	if err != nil {
		return err
	}

	return writeFileAtomic(f.path, data)
}

// Load reads the file. A file that is not there is an error matching fs.ErrNotExist.
func (f *LibraryFile) Load() (Bundle, error) {
	return readBundleFile(f.path)
}

// Restored says what Restore brought back.
type Restored struct {
	// Count is how many prompts came back.
	Count int

	// From is the file they came from: the database set aside, or the library file. It is
	// "" when nothing was restored.
	From string
}

// Restore brings back a library the app lost. It runs only on a library that is new,
// one no starter was ever added to and that holds no prompt, as after a database was set
// aside, damaged, or deleted, and before Seed adds the starters. It reads the database
// set aside at startup when there is one (setAside is "" when there is not), and
// otherwise the library file (nil for none), and adds every prompt in it, with the
// app's default prompts and the record of the starters already added, so a starter the
// user deleted stays deleted. A source it cannot read is skipped for the next, and named
// in the error; nothing is restored when neither can be read.
func Restore(ctx context.Context, st *Store, setAside string, file *LibraryFile) (Restored, error) {
	exists, err := st.libraryExists(ctx)
	if err != nil || exists {
		return Restored{}, err
	}

	var errs []error

	if setAside != "" {
		b, readErr := readSnapshotBundle(ctx, setAside)
		if readErr == nil {
			return restoreBundle(ctx, st, b, setAside)
		}

		errs = append(errs, fmt.Errorf("read the prompts in the database set aside: %w", readErr))
	}

	if file == nil {
		return Restored{}, errors.Join(errs...)
	}

	b, err := file.Load()

	switch {
	case err == nil:
		restored, restoreErr := restoreBundle(ctx, st, b, file.Path())

		return restored, errors.Join(append(errs, restoreErr)...)
	case !errors.Is(err, fs.ErrNotExist):
		errs = append(errs, fmt.Errorf("read the copy of the prompt library: %w", err))
	}

	return Restored{}, errors.Join(errs...)
}

// restoreBundle adds every prompt in b to the new library st holds, with b's default
// prompts and its record of the starters added, all of it or, when a write fails,
// none. from is the file b was read from.
func restoreBundle(ctx context.Context, st *Store, b Bundle, from string) (Restored, error) {
	count := 0

	err := st.inTx(ctx, func(tx *Store) error {
		ids, added, err := restorePrompts(ctx, tx, b)
		if err != nil {
			return err
		}

		count = len(b.Prompts)

		if err = tx.setSeeded(ctx, added); err != nil {
			return err
		}

		return restoreDefaults(ctx, tx, b.Defaults, ids)
	})
	if err != nil {
		return Restored{}, fmt.Errorf("restore the prompts: %w", err)
	}

	return Restored{Count: count, From: from}, nil
}

// restorePrompts adds every prompt in b, as it was, under a free nickname where two
// share one. It returns the ID each nickname in b was stored under, the first where two
// share one, and the starters the library has been given: b's record of them and every
// starter it stored.
func restorePrompts(ctx context.Context, st *Store, b Bundle) (map[string]int64, []string, error) {
	pl := newPlanner(nil, nil)
	pl.faithful = true

	ids := map[string]int64{}
	added := appendUniqueAll(nil, b.StartersAdded)

	for _, in := range b.Prompts {
		e := pl.plan(in)

		id, err := storePlanned(ctx, st, e)
		if err != nil {
			return nil, nil, err
		}

		if _, seen := ids[in.Nickname]; !seen {
			ids[in.Nickname] = id
		}

		if e.keepStarter {
			added = appendUnique(added, e.Prompt.StarterKey)
		}
	}

	return ids, added, nil
}

// restoreDefaults makes the prompts defaults names, by their nicknames in the file, the
// app's default prompts again, each where it applies to the kind.
func restoreDefaults(ctx context.Context, st *Store, defaults BundleDefaults, ids map[string]int64) error {
	for kind, nickname := range map[item.Kind]string{item.KindPR: defaults.PR, item.KindIssue: defaults.Issue} {
		id, ok := ids[nickname]
		if nickname == "" || !ok {
			continue
		}

		p, err := st.Lookup(ctx, id)
		if err != nil {
			return err
		}

		if !p.AppliesTo.Covers(kind) {
			continue
		}

		if err = st.setDefault(ctx, kind, id); err != nil {
			return err
		}
	}

	return nil
}

// appendUniqueAll appends each of items that list does not hold yet.
func appendUniqueAll[S comparable](list, items []S) []S {
	for _, s := range items {
		list = appendUnique(list, s)
	}

	return list
}
