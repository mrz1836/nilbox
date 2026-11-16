package autopilot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// BackupPart is the routines' part of a backup of the user's setup: the routines in the
// export format, and what importing them does. Core adapts it to a backup.Part; it is a
// type of its own, apart from the Service, so the window, which binds the Service, never
// reaches it. A backup holds the routines alone, never their history.
type BackupPart struct {
	s *Service
}

// NewBackupPart returns the backup's part over s.
func NewBackupPart(s *Service) *BackupPart {
	return &BackupPart{s: s}
}

// Bundle returns every routine not deleted in the export format, in the user's order,
// written at now by the version app.
func (bp *BackupPart) Bundle(ctx context.Context, app string, now time.Time) (Bundle, error) {
	rows, err := bp.s.store.all(ctx)
	if err != nil {
		return Bundle{}, err
	}

	return bundleOf(rows, app, now), nil
}

// BackupPlan is what importing a backup's routines does, as PlanBackup works it out.
type BackupPlan struct {
	backup.Plan

	mode backup.Mode

	// routines are the file's routines as they are stored, in the file's order, and
	// viewer the account a routine added belongs to.
	routines []keptRoutine
	viewer   string
}

// PlanBackup works out what importing b, a backup's routines, does under mode, changing
// nothing. Each routine must have a name and a definition that loads; one that doesn't
// (as one a newer build wrote) is left out, with a note on why, as is a second routine of
// a name. Routines are matched by name, ignoring case: a match whose definition,
// description, and mode are the file's is left as it is, on or off, another is saved over
// and switched off, and a routine of a name this Mac lacks is added, off, at the end.
// Replace also deletes every routine the file doesn't name, and puts the rest in the
// file's order. A routine here that a newer build wrote is never saved over or deleted,
// under either mode: it is left as it is, with a note. A routine whose mode this build
// doesn't know takes the mode new routines start in.
func (bp *BackupPart) PlanBackup(ctx context.Context, b Bundle, mode backup.Mode) (BackupPlan, error) {
	current, err := bp.s.store.all(ctx)
	if err != nil {
		return BackupPlan{}, err
	}

	out := BackupPlan{mode: mode, viewer: bp.s.viewer(ctx)}
	defaultMode := bp.s.defaultMode(ctx)
	v := bp.s.view(ctx)
	matched := map[int64]bool{}

	for _, r := range b.Routines {
		k, note := checkImport(r, defaultMode)
		if note == "" && out.holds(k.name) {
			note = fmt.Sprintf("The routine “%s” was left out: the file holds another of that name.", k.name)
		}

		if note != "" {
			out.AddNote(note)

			continue
		}

		change, kept := k.change(current, matched)
		if kept != "" {
			out.AddNote(newerNote(kept))
		}

		out.routines = append(out.routines, k)
		out.Add(backup.Item{Label: k.name, Change: change, Detail: spec.Sentence(k.spec.spec, v.names)})
	}

	if mode == backup.ModeReplace {
		out.planRemoves(current, matched, v)
	}

	return out, nil
}

// holds reports whether the plan imports a routine named name, ignoring case.
func (p *BackupPlan) holds(name string) bool {
	return slices.ContainsFunc(p.routines, func(o keptRoutine) bool { return sameName(o.name, name) })
}

// planRemoves lists each of current that matched doesn't hold as one Replace deletes, but
// a routine a newer build wrote, which is kept, with a note.
func (p *BackupPlan) planRemoves(current []queries.AutopilotRoutine, matched map[int64]bool, v view) {
	for _, c := range current {
		switch {
		case matched[c.ID]:
		case newerBuild(c):
			p.AddNote(newerNote(c.Name))
		default:
			p.Add(backup.Item{Label: c.Name, Change: backup.ChangeRemove, Detail: routineOf(c, v).Sentence})
		}
	}
}

// change says what importing k does to the routines in current: adds it, saves over the
// routine of its name, or leaves that routine as it is, marking the routine matched. A
// routine a newer build wrote is left as it is, and its name returned for a note.
func (k keptRoutine) change(current []queries.AutopilotRoutine, matched map[int64]bool) (backup.Change, string) {
	i := slices.IndexFunc(current, func(c queries.AutopilotRoutine) bool { return sameName(c.Name, k.name) })
	if i < 0 {
		return backup.ChangeAdd, ""
	}

	matched[current[i].ID] = true

	switch {
	case newerBuild(current[i]):
		return backup.ChangeSame, current[i].Name
	case k.same(current[i]):
		return backup.ChangeSame, ""
	default:
		return backup.ChangeUpdate, ""
	}
}

// newerBuild reports a routine whose stored spec a newer build wrote, which nothing here
// saves over, deletes by an import, or rewrites.
func newerBuild(row queries.AutopilotRoutine) bool {
	b := load(row.Spec).broken

	return b != nil && b.Reason == spec.ReasonNewer
}

// newerNote says an import leaves the routine name, which a newer build wrote, as it is.
func newerNote(name string) string {
	return fmt.Sprintf("The routine “%s” here was made by a newer %s: left as it is.", name, brand.Name)
}

// checkImport reads one of a backup's routines as it is stored, or says why it is left
// out: a name its checks refuse, or a definition that won't load.
func checkImport(r BundleRoutine, defaultMode autopilotprefs.Mode) (keptRoutine, string) {
	name, err := checkName(r.Name)
	if err != nil {
		return keptRoutine{}, "A routine was left out. " + uierr.Message(err)
	}

	sp, err := keepSpec(r.Spec)
	if err == nil && !sp.loads {
		err = sp.refusal
	}

	if err != nil {
		return keptRoutine{}, fmt.Sprintf("The routine “%s” was left out. %s", name, importRefusal(err))
	}

	k, _ := keptFrom(name, r.Description, r.Template, r.Mode, r.Spec)
	k.name = name

	if !r.Mode.Valid() {
		k.mode = catalogMode(defaultMode, sp.spec)
	}

	return k, ""
}

// importRefusal words why a routine's definition can't be imported.
func importRefusal(err error) string {
	if b, ok := errors.AsType[*spec.BrokenError](err); ok {
		return brokenWords(b)
	}

	return "Its definition can't be read."
}

// same reports whether k is the routine row as it stands: the same definition,
// description, and mode.
func (k keptRoutine) same(row queries.AutopilotRoutine) bool {
	stored := load(row.Spec)
	if stored.broken != nil {
		return false
	}

	a, errA := spec.Encode(stored.spec)
	b, errB := spec.Encode(k.spec.spec)

	return errA == nil && errB == nil && string(a) == string(b) && row.Description == k.desc &&
		storedMode(row.Mode) == k.mode
}

// ApplyBackup stores plan's routines inside tx, an import's transaction, and returns what
// it changed: each one saved over is switched off, each one added is off, and under
// Replace each routine the file doesn't name is deleted and the rest put in the file's
// order. Merge adds at the end, in the file's order, and deletes nothing.
func (bp *BackupPart) ApplyBackup(ctx context.Context, tx *sql.Tx, plan BackupPlan) (backup.Count, error) {
	if bp.s.store.q == nil {
		return backup.Count{}, ErrNotSetUp
	}

	q := bp.s.store.q.WithTx(tx)

	rows, err := q.ListRoutines(ctx)
	if err != nil {
		return backup.Count{}, fmt.Errorf("read the routines: %w", err)
	}

	im := routineImport{
		q: q, clock: bp.s.clock, viewer: plan.viewer, rows: rows, replace: plan.mode == backup.ModeReplace,
		matched: map[int64]bool{},
	}

	if !im.replace {
		im.order = slices.Clone(rows)
	}

	for _, k := range plan.routines {
		if err = im.place(ctx, k); err != nil {
			return backup.Count{}, err
		}
	}

	if im.replace {
		if err = im.dropUnmatched(ctx); err != nil {
			return backup.Count{}, err
		}
	}

	return im.count, renumber(ctx, q, im.order)
}

// BackupDone writes the copy of the routines again, once an import committed.
func (bp *BackupPart) BackupDone(ctx context.Context) {
	bp.s.changed(ctx)
}

// routineImport is an import of the routines as it is applied: the rows stored before it,
// those a routine of the file's matched, the order it stores, and what it did.
type routineImport struct {
	q       *queries.Queries
	clock   clock.Clock
	viewer  string
	rows    []queries.AutopilotRoutine
	replace bool

	matched map[int64]bool
	order   []queries.AutopilotRoutine
	count   backup.Count
}

// place stores k: over the routine of its name, which is switched off unless it is the
// same or a newer build wrote it, either of which is left as it is, or else as a new
// routine, off, at the end.
func (im *routineImport) place(ctx context.Context, k keptRoutine) error {
	i := slices.IndexFunc(im.rows, func(r queries.AutopilotRoutine) bool { return sameName(r.Name, k.name) })
	if i < 0 {
		k.account = im.viewer

		row, err := insertKept(ctx, im.q, im.clock, k, int64(len(im.rows)+len(im.order)))
		if err != nil {
			return err
		}

		im.count = im.count.With(backup.ChangeAdd)
		im.order = append(im.order, row)

		return nil
	}

	row := im.rows[i]
	im.matched[row.ID] = true

	if im.replace {
		im.order = append(im.order, row)
	}

	if newerBuild(row) || k.same(row) {
		im.count = im.count.With(backup.ChangeSame)

		return nil
	}

	account := row.AccountLogin
	if account == "" {
		account = im.viewer
	}

	_, err := im.q.UpdateRoutine(ctx, queries.UpdateRoutineParams{
		AccountLogin: account, Name: row.Name, NameKey: nameKey(row.Name), Description: k.desc, Mode: string(k.mode),
		Spec: k.spec.data, SpecVersion: k.spec.version, UpdatedAt: im.clock.Now(), ID: row.ID,
	})
	if err != nil {
		return fmt.Errorf("save the routine %q: %w", row.Name, err)
	}

	im.count = im.count.With(backup.ChangeUpdate)

	return nil
}

// dropUnmatched deletes each routine no routine of the file's matched, as Replace does,
// but one a newer build wrote, which keeps its place after the file's.
func (im *routineImport) dropUnmatched(ctx context.Context) error {
	for _, r := range im.rows {
		if im.matched[r.ID] {
			continue
		}

		if newerBuild(r) {
			im.order = append(im.order, r)

			continue
		}

		now := im.clock.Now()
		if _, err := im.q.DeleteRoutine(ctx, queries.DeleteRoutineParams{DeletedAt: &now, ID: r.ID}); err != nil {
			return fmt.Errorf("delete the routine %d: %w", r.ID, err)
		}

		im.count = im.count.With(backup.ChangeRemove)
	}

	return nil
}
