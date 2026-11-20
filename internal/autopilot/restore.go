package autopilot

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"slices"

	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// routinesTable is the table the routines are kept in, in this build and in a database set
// aside.
const routinesTable = "autopilot_routines"

// restoreColumns are the columns a database set aside holds the routines in. A build that
// lacks one reads it as its empty value.
func restoreColumns() []string {
	return []string{
		"id", "account_login", "name", "description", "mode", "spec", "template_key", "sort_order", "deleted_at",
	}
}

// Restored says what Restore brought back.
type Restored struct {
	// Count is how many routines came back.
	Count int

	// From is where they came from: the database set aside, or the copy. It is "" when
	// none came back.
	From string
}

// keptRoutine is a routine brought back or imported, as it is stored: switched off.
type keptRoutine struct {
	name, desc, template, account string
	mode                          autopilotprefs.Mode
	spec                          storedSpec
}

// Restore brings back the routines the app lost with its database. It runs only while db
// holds no routine at all, the deleted ones included, as after the database was set aside,
// damaged, or deleted. It reads the database set aside at startup when there is one
// (setAside, "" for none) and has the routines' table, and otherwise the copy (nil for
// none), and stores every routine not deleted in it, in its order, switched off, each in
// its mode and with its account as far as the source says, and its spec as it was, so a
// routine a newer build wrote stays as that build wrote it. A routine whose definition
// isn't JSON at all is left out. It stores them all in one transaction, or none. A source
// it cannot read is skipped for the next, and named in the error.
func Restore(ctx context.Context, db *sql.DB, clk clock.Clock, setAside string, file *File) (Restored, error) {
	if db == nil {
		return Restored{}, ErrNotSetUp
	}

	n, err := queries.New(db).CountAllRoutines(ctx)
	if err != nil || n > 0 {
		return Restored{}, wrapRead(err)
	}

	var errs []error

	if setAside != "" {
		kept, readErr := readSetAside(ctx, setAside)

		switch {
		case readErr == nil:
			count, storeErr := storeKept(ctx, db, clk, kept)

			return restoredFrom(count, setAside), storeErr
		case !errors.Is(readErr, store.ErrNoTable):
			errs = append(errs, readErr)
		}
	}

	if file == nil {
		return Restored{}, errors.Join(errs...)
	}

	b, err := file.Load()

	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Restored{}, errors.Join(errs...)
	case err != nil:
		return Restored{}, errors.Join(append(errs, fmt.Errorf("read the copy of the routines: %w", err))...)
	}

	count, err := storeKept(ctx, db, clk, keptFromBundle(b))

	return restoredFrom(count, file.Path()), errors.Join(append(errs, err)...)
}

// restoredFrom says count routines came back from from.
func restoredFrom(count int, from string) Restored {
	if count == 0 {
		return Restored{}
	}

	return Restored{Count: count, From: from}
}

// wrapRead words a failure to read the routines, nil for none.
func wrapRead(err error) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("read the routines: %w", err)
}

// readSetAside reads the routines not deleted in the database at path, in their order.
// A database with no routines' table is store.ErrNoTable.
func readSetAside(ctx context.Context, path string) ([]keptRoutine, error) {
	snap, err := store.OpenSnapshot(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("read the routines in the database set aside: %w", err)
	}

	defer func() { _ = snap.Close() }()

	rows, err := snap.Rows(ctx, routinesTable, restoreColumns()...)
	if err != nil {
		return nil, fmt.Errorf("read the routines in the database set aside: %w", err)
	}

	rows = slices.DeleteFunc(rows, func(r store.Row) bool { return r.Text("deleted_at") != "" })

	slices.SortStableFunc(rows, func(a, b store.Row) int {
		pa, _ := a.Int("sort_order")
		pb, _ := b.Int("sort_order")
		ia, _ := a.Int("id")
		ib, _ := b.Int("id")

		return cmp.Or(cmp.Compare(pa, pb), cmp.Compare(ia, ib))
	})

	out := make([]keptRoutine, 0, len(rows))

	for _, row := range rows {
		k, ok := keptFrom(row.Text("name"), row.Text("description"), row.Text("template_key"),
			autopilotprefs.Mode(row.Text("mode")), json.RawMessage(row.Text("spec")))
		if ok {
			k.account = row.Text("account_login")
			out = append(out, k)
		}
	}

	return out, nil
}

// keptFromBundle reads b's routines as they are brought back.
func keptFromBundle(b Bundle) []keptRoutine {
	out := make([]keptRoutine, 0, len(b.Routines))

	for _, r := range b.Routines {
		if k, ok := keptFrom(r.Name, r.Description, r.Template, r.Mode, r.Spec); ok {
			out = append(out, k)
		}
	}

	return out
}

// keptFrom reads one routine brought back as it is stored, forgiving what its checks
// would refuse: a name made one line and cut to fit, or "Routine" for none; a description
// or a template that won't do left out; a mode this build doesn't know read as Dry run,
// and a spec with a step this build can't run yet put in Dry run. It reports false for a
// spec that isn't JSON at all.
func keptFrom(name, desc, template string, mode autopilotprefs.Mode, raw json.RawMessage) (keptRoutine, bool) {
	sp, err := keepSpec(raw)
	if err != nil {
		return keptRoutine{}, false
	}

	k := keptRoutine{name: draft.FitName(textutil.OneLine(name), ""), mode: storedMode(string(mode)), spec: sp}
	if k.name == "" {
		k.name = "Routine"
	}

	if d, descErr := draft.CheckDescription(desc); descErr == nil {
		k.desc = d
	}

	if t, templateErr := draft.CheckTemplate(template); templateErr == nil {
		k.template = t
	}

	if sp.loads {
		k.mode = catalogMode(k.mode, sp.spec)
	}

	return k, true
}

// storeKept stores kept, in their order, switched off, when db holds no routine yet, all
// of them or none, and returns how many it stored. Two that share a name are told apart
// by a number.
func storeKept(ctx context.Context, db *sql.DB, clk clock.Clock, kept []keptRoutine) (int, error) {
	if len(kept) == 0 {
		return 0, nil
	}

	r := newRoutineStore(db, clk)
	stored := 0

	err := r.inTx(ctx, func(q *queries.Queries) error {
		n, err := q.CountAllRoutines(ctx)
		if err != nil || n > 0 {
			return wrapRead(err)
		}

		var rows []queries.AutopilotRoutine

		for i, k := range kept {
			k.name = freeName(k.name, rows)

			row, insertErr := insertKept(ctx, q, r.clock, k, int64(i))
			if insertErr != nil {
				return insertErr
			}

			rows = append(rows, row)
		}

		stored = len(rows)

		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("bring back the routines: %w", err)
	}

	return stored, nil
}

// insertKept stores k, switched off, at place in the order, and returns its row.
func insertKept(ctx context.Context, q *queries.Queries, clk clock.Clock, k keptRoutine, place int64,
) (queries.AutopilotRoutine, error) {
	now := clk.Now()
	row := queries.AutopilotRoutine{
		AccountLogin: k.account, Name: k.name, NameKey: draft.NameKey(k.name), Description: k.desc, Mode: string(k.mode),
		Spec: k.spec.data, SpecVersion: k.spec.version, TemplateKey: k.template, SortOrder: place,
		BrokenReason: string(k.spec.broken), CreatedAt: now, UpdatedAt: now,
	}

	id, err := q.InsertRoutine(ctx, queries.InsertRoutineParams{
		AccountLogin: row.AccountLogin, Name: row.Name, NameKey: row.NameKey, Description: row.Description,
		Mode: row.Mode, Spec: row.Spec, SpecVersion: row.SpecVersion, TemplateKey: row.TemplateKey,
		SortOrder: row.SortOrder, BrokenReason: row.BrokenReason, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return queries.AutopilotRoutine{}, fmt.Errorf("save the routine %q: %w", k.name, err)
	}

	row.ID = id

	return row, nil
}
