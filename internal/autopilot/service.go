package autopilot

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// List returns every routine not deleted, in the user's order, each marked when it
// belongs to another account than the one signed in, or when it is broken, and with its
// figures (what it did today, what waits, when it last ran), from three reads of the
// ledger. A broken routine is listed with why, beside the others, and never rewritten.
func (s *Service) List(ctx context.Context) ([]Routine, error) {
	rows, err := s.store.all(ctx)
	if err != nil {
		return nil, err
	}

	figures, err := s.figures(ctx)
	if err != nil {
		return nil, err
	}

	v := s.view(ctx)
	out := make([]Routine, 0, len(rows))

	for _, row := range rows {
		r := routineOf(row, v)
		r.Figures = figures[row.ID]

		if s.store.q != nil {
			if r.Figures.Graduate, err = s.graduates(ctx, s.ledger(), r, row.GraduateDeclinedAt); err != nil {
				return nil, err
			}
		}

		out = append(out, r)
	}

	return out, nil
}

// figures reads every routine's figures from the ledger, by routine: the counts in one
// grouped read (today's from local midnight, in the clock's own zone), then when each
// routine last ran: its match that ran (or would have, in Dry run) that closed last, as it
// ran, however long it waited before.
func (s *Service) figures(ctx context.Context) (map[int64]RoutineFigures, error) {
	out := map[int64]RoutineFigures{}

	if s.store.q == nil {
		return out, nil
	}

	now := s.clock.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	l := s.ledger()

	rows, err := l.q.ListRoutineFigures(ctx, midnight.UTC())
	if err != nil {
		return nil, uierr.New("Autopilot's history couldn't be read.", err)
	}

	for _, r := range rows {
		out[r.RoutineID] = RoutineFigures{Today: int(r.Today), WouldRunToday: int(r.WouldRunToday), Waiting: int(r.Waiting)}
	}

	last, err := lastRuns(ctx, l)
	if err != nil {
		return nil, err
	}

	for routineID, h := range last {
		f := out[routineID]
		at := *h.ClosedAt
		f.LastAt, f.LastDryRun = &at, h.DryRun
		out[routineID] = f
	}

	return out, nil
}

// lastRuns reads, by routine, the match that ran last of each routine with one that ran
// (or would have, in Dry run): the one that closed last, with when that was.
func lastRuns(ctx context.Context, l *ledger) (map[int64]queries.ListMatchHeadsRow, error) {
	out := map[int64]queries.ListMatchHeadsRow{}

	runs, err := l.q.ListRoutineLastRuns(ctx)
	if err != nil {
		return nil, uierr.New("Autopilot's history couldn't be read.", err)
	}

	if len(runs) == 0 {
		return out, nil
	}

	routineOf := make(map[int64]int64, len(runs))
	ids := make([]int64, 0, len(runs))

	for _, r := range runs {
		routineOf[r.ID] = r.RoutineID
		ids = append(ids, r.ID)
	}

	got, err := l.q.ListMatchHeads(ctx, ids)
	if err != nil {
		return nil, uierr.New("Autopilot's history couldn't be read.", err)
	}

	for _, h := range got {
		if h.ClosedAt != nil {
			out[routineOf[h.ID]] = h
		}
	}

	return out, nil
}

// RoutineNames returns every routine's name by its ID, the deleted ones included, for the marks
// and history the window shows of a routine that may have gone since. It reads the
// database alone, and the list is never nil.
func (s *Service) RoutineNames(ctx context.Context) ([]RoutineName, error) {
	out := []RoutineName{}

	if s.store.q == nil {
		return out, nil
	}

	rows, err := s.ledger().q.ListRoutineNames(ctx)
	if err != nil {
		return out, uierr.New("The routines' names couldn't be read.", err)
	}

	for _, r := range rows {
		out = append(out, RoutineName{ID: r.ID, Name: r.Name, Deleted: r.DeletedAt != nil})
	}

	return out, nil
}

// Get returns the routine id, refusing one that is gone or deleted.
func (s *Service) Get(ctx context.Context, id int64) (Routine, error) {
	if s.store.q == nil {
		return Routine{}, ErrNotSetUp
	}

	row, err := s.store.get(ctx, s.store.q, id)
	if err != nil {
		return Routine{}, err
	}

	return routineOf(row, s.view(ctx)), nil
}

// save stores d, a new routine when its ID is 0 and otherwise in place of the routine
// with its ID, and returns it as stored. Its name must be one no other routine has,
// ignoring case, and its spec must load: a spec with problems is refused with each one,
// for the window to show where it is (ProblemsError, the error's detail). A new routine
// starts off, at the end of the list, in the mode d names or else the one Settings says
// new routines start in, and belongs to the account signed in; one saved before keeps
// its switch, its place, and its template, and its mode unless d names one. A routine with
// a step this build can't run yet is stored in Dry run, whatever the mode. A routine a
// newer build wrote can't be saved over, and one naming a prompt the library doesn't have,
// or one that doesn't fit, is refused as a spec with problems is.
func (s *Service) save(ctx context.Context, d Draft) (Routine, error) {
	saving, err := checkDraft(d)
	if err != nil {
		return Routine{}, err
	}

	problems := append(s.offers.placeProblems(saving.spec), s.promptProblems(ctx, saving.spec)...)
	if problems = append(problems, s.scriptProblems(ctx, saving.spec)...); len(problems) > 0 {
		v := &spec.ValidationError{Problems: problems}

		return Routine{}, uierr.New(textutil.Sentence("The routine isn't valid yet: "+problems[0].Message),
			&ProblemsError{err: v})
	}

	// The settings and the account are read before the transaction, which would wait on
	// a pool-backed read.
	if saving.mode == "" && d.ID == 0 {
		saving.mode = s.defaultMode(ctx)
	}

	saving.viewer = s.viewer(ctx)
	id := d.ID

	err = s.store.inTx(ctx, func(q *queries.Queries) error {
		if freeErr := checkFree(ctx, q, saving.name, d.ID); freeErr != nil {
			return freeErr
		}

		if d.ID != 0 {
			return s.update(ctx, q, d.ID, saving)
		}

		made, insertErr := s.insertNew(ctx, q, saving)
		id = made

		return insertErr
	})
	if err != nil {
		return Routine{}, err
	}

	s.changed(ctx)

	return s.Get(ctx, id)
}

// checkDraft returns d as Save stores it, refusing what it can't store: a name, a
// description, a template, or a mode its checks refuse, or a spec that won't load.
func checkDraft(d Draft) (saved, error) {
	name, err := checkName(d.Name)
	if err != nil {
		return saved{}, err
	}

	desc, err := checkDescription(d.Description)
	if err != nil {
		return saved{}, err
	}

	template, err := checkTemplate(d.TemplateKey)
	if err != nil {
		return saved{}, err
	}

	if d.Mode != "" {
		if err = checkMode(d.Mode); err != nil {
			return saved{}, err
		}
	}

	data, sp, err := checkSpec(d.Spec)
	if err != nil {
		return saved{}, err
	}

	return saved{name: name, desc: desc, template: template, mode: d.Mode, data: string(data), spec: sp}, nil
}

// saved is a routine Save checked, as it stores it.
type saved struct {
	name, desc, template string
	mode                 autopilotprefs.Mode
	data                 string
	spec                 spec.Spec
	viewer               string
}

// insertNew stores sv as a new routine at the end of the list, off, and returns its ID.
func (s *Service) insertNew(ctx context.Context, q *queries.Queries, sv saved) (int64, error) {
	rows, err := q.ListRoutines(ctx)
	if err != nil {
		return 0, fmt.Errorf("read the routines: %w", err)
	}

	now := s.clock.Now()

	id, err := q.InsertRoutine(ctx, queries.InsertRoutineParams{
		AccountLogin: sv.viewer, Name: sv.name, NameKey: nameKey(sv.name), Description: sv.desc,
		Mode: string(s.offers.modeFor(sv.mode, sv.spec)), Spec: sv.data, SpecVersion: int64(sv.spec.Version), TemplateKey: sv.template, SortOrder: int64(len(rows)),
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return 0, fmt.Errorf("save the routine: %w", err)
	}

	return id, nil
}

// update stores sv in place of the routine id, which keeps its switch, its place, and its
// template, and its mode when sv names none. A routine of no account becomes the account
// signed in's, since the user edited it; a routine a newer build wrote is refused.
func (s *Service) update(ctx context.Context, q *queries.Queries, id int64, sv saved) error {
	row, err := s.store.get(ctx, q, id)
	if err != nil {
		return err
	}

	if b := load(row.Spec).broken; b != nil && b.Reason == spec.ReasonNewer {
		return uierr.New(brokenWords(b), fmt.Errorf("%w: %w", ErrBroken, b))
	}

	mode := sv.mode
	if mode == "" {
		mode = storedMode(row.Mode)
	}

	account := row.AccountLogin
	if account == "" {
		account = sv.viewer
	}

	n, err := q.UpdateRoutine(ctx, queries.UpdateRoutineParams{
		AccountLogin: account, Name: sv.name, NameKey: nameKey(sv.name), Description: sv.desc, Enabled: row.Enabled,
		Mode: string(s.offers.modeFor(mode, sv.spec)), Spec: sv.data, SpecVersion: int64(sv.spec.Version),
		EnabledAt: row.EnabledAt, UpdatedAt: s.clock.Now(), ID: id,
	})

	switch {
	case err != nil:
		return fmt.Errorf("save the routine %d: %w", id, err)
	case n == 0:
		return goneError(id)
	}

	return nil
}

// Delete deletes the routine id: it is switched off and leaves the list, and its record
// stays, so the history that names it keeps its name.
func (s *Service) Delete(ctx context.Context, id int64) error {
	err := s.reorder(ctx, func(q *queries.Queries, rows []queries.AutopilotRoutine) ([]queries.AutopilotRoutine, error) {
		now := s.clock.Now()

		n, err := q.DeleteRoutine(ctx, queries.DeleteRoutineParams{DeletedAt: &now, ID: id})

		switch {
		case err != nil:
			return nil, fmt.Errorf("delete the routine %d: %w", id, err)
		case n == 0:
			return nil, goneError(id)
		}

		return slices.DeleteFunc(rows, func(r queries.AutopilotRoutine) bool { return r.ID == id }), nil
	})
	if err != nil {
		return err
	}

	s.changed(ctx)

	return nil
}

// Duplicate saves a copy of the routine id, switched off and called "<name> copy", right
// after it in the list, and returns the copy. The copy belongs to the account signed in,
// or to the routine's own when none is.
func (s *Service) Duplicate(ctx context.Context, id int64) (Routine, error) {
	viewer := s.viewer(ctx)

	var made int64

	err := s.reorder(ctx, func(q *queries.Queries, rows []queries.AutopilotRoutine) ([]queries.AutopilotRoutine, error) {
		i := slices.IndexFunc(rows, func(r queries.AutopilotRoutine) bool { return r.ID == id })
		if i < 0 {
			return nil, goneError(id)
		}

		row := rows[i]
		row.Name, row.Enabled, row.EnabledAt, row.LastError = copyName(row.Name, rows), false, nil, ""

		if viewer != "" {
			row.AccountLogin = viewer
		}

		now := s.clock.Now()

		var err error

		row.ID, err = q.InsertRoutine(ctx, queries.InsertRoutineParams{
			AccountLogin: row.AccountLogin, Name: row.Name, NameKey: nameKey(row.Name), Description: row.Description,
			Mode: row.Mode, Spec: row.Spec, SpecVersion: row.SpecVersion, TemplateKey: row.TemplateKey, SortOrder: int64(len(rows)),
			BrokenReason: row.BrokenReason, CreatedAt: now, UpdatedAt: now,
		})
		if err != nil {
			return nil, fmt.Errorf("copy the routine %d: %w", id, err)
		}

		made, row.SortOrder = row.ID, int64(len(rows))

		return slices.Insert(rows, i+1, row), nil
	})
	if err != nil {
		return Routine{}, err
	}

	s.changed(ctx)

	return s.Get(ctx, made)
}

// setEnabled switches the routine id on or off, and returns it. A broken routine can't be
// switched on; switching on one that is on keeps when it was.
func (s *Service) setEnabled(ctx context.Context, id int64, on bool) (Routine, error) {
	return s.change(ctx, id, func(q *queries.Queries, row queries.AutopilotRoutine) error {
		if b := load(row.Spec).broken; on && b != nil {
			return uierr.New(brokenWords(b)+" It can't be switched on.", fmt.Errorf("%w: %w", ErrBroken, b))
		}

		params := queries.SetRoutineEnabledParams{Enabled: on, UpdatedAt: s.clock.Now(), ID: id}

		switch {
		case on && row.Enabled:
			params.EnabledAt = row.EnabledAt
		case on:
			params.EnabledAt = &params.UpdatedAt
		}

		if _, err := q.SetRoutineEnabled(ctx, params); err != nil {
			return fmt.Errorf("switch the routine %d: %w", id, err)
		}

		return nil
	})
}

// setMode sets how the routine id acts on a match, and returns it. A broken routine's mode
// can't be changed, and a routine with a step this build can't run yet stays in Dry run.
func (s *Service) setMode(ctx context.Context, id int64, mode autopilotprefs.Mode) (Routine, error) {
	if !mode.Valid() {
		return Routine{}, checkMode(mode)
	}

	return s.change(ctx, id, func(q *queries.Queries, row queries.AutopilotRoutine) error {
		l := load(row.Spec)
		if l.broken != nil {
			return uierr.New(brokenWords(l.broken)+" Its mode can't be changed.", fmt.Errorf("%w: %w", ErrBroken, l.broken))
		}

		if kinds := s.offers.notOffered(l.spec); len(kinds) > 0 && mode != autopilotprefs.ModeDryRun {
			return dryRunOnlyError(kinds)
		}

		params := queries.SetRoutineModeParams{Mode: string(mode), UpdatedAt: s.clock.Now(), ID: id}
		if _, err := q.SetRoutineMode(ctx, params); err != nil {
			return fmt.Errorf("set the routine %d's mode: %w", id, err)
		}

		return nil
	})
}

// change runs write in one transaction over the routine id as stored, refusing one that
// is gone or deleted, writes the copy of the routines again, and returns the routine.
func (s *Service) change(ctx context.Context, id int64,
	write func(q *queries.Queries, row queries.AutopilotRoutine) error,
) (Routine, error) {
	err := s.store.inTx(ctx, func(q *queries.Queries) error {
		row, err := s.store.get(ctx, q, id)
		if err != nil {
			return err
		}

		return write(q, row)
	})
	if err != nil {
		return Routine{}, err
	}

	s.changed(ctx)

	return s.Get(ctx, id)
}

// Reorder puts the routines ids names first, in that order, and the others after them as
// they were, and returns the list. An id named twice counts once; one that is gone or
// deleted is refused, and nothing moves.
func (s *Service) Reorder(ctx context.Context, ids []int64) ([]Routine, error) {
	err := s.reorder(ctx, func(_ *queries.Queries, rows []queries.AutopilotRoutine) ([]queries.AutopilotRoutine, error) {
		return ordered(rows, ids)
	})
	if err != nil {
		return nil, err
	}

	s.changed(ctx)

	return s.List(ctx)
}

// ordered returns rows with those ids names first, in that order, each once, and the
// others after them as they were, refusing an id no row has.
func ordered(rows []queries.AutopilotRoutine, ids []int64) ([]queries.AutopilotRoutine, error) {
	order := make([]queries.AutopilotRoutine, 0, len(rows))
	placed := map[int64]bool{}

	for _, id := range ids {
		i := slices.IndexFunc(rows, func(r queries.AutopilotRoutine) bool { return r.ID == id })

		switch {
		case i < 0:
			return nil, goneError(id)
		case !placed[id]:
			order = append(order, rows[i])
			placed[id] = true
		}
	}

	for _, r := range rows {
		if !placed[r.ID] {
			order = append(order, r)
		}
	}

	return order, nil
}

// reorder runs change in one transaction over the routines as stored, in their order, and
// stores the order it returns.
func (s *Service) reorder(ctx context.Context,
	change func(q *queries.Queries, rows []queries.AutopilotRoutine) ([]queries.AutopilotRoutine, error),
) error {
	return s.store.inTx(ctx, func(q *queries.Queries) error {
		rows, err := q.ListRoutines(ctx)
		if err != nil {
			return fmt.Errorf("read the routines: %w", err)
		}

		order, err := change(q, rows)
		if err != nil {
			return err
		}

		return renumber(ctx, q, order)
	})
}

// changed builds the snapshot of the routines' searches again, and writes the copy of the
// routines again, after a change to one. A failure is logged: the copy is written again
// after the next change.
func (s *Service) changed(ctx context.Context) {
	s.rebuildWatches(ctx)

	// A routine saved or switched on may need what this Mac wasn't checked for yet.
	s.ready.markDue()

	if s.engine != nil {
		s.engine.Wake()
		s.engine.notify()
	}

	if s.file == nil || s.store.db == nil {
		return
	}

	if err := s.file.Save(ctx, s.store.db); err != nil {
		s.logger.WarnContext(ctx, "could not save the copy of the routines", "path", s.file.Path(), "error", err)
	}
}

// view returns what the routines are shown with: the account signed in and the starter
// prompts' names. One that can't be read is logged, and left out.
func (s *Service) view(ctx context.Context) view {
	v := view{viewer: s.viewer(ctx), offers: s.offers}

	if s.names == nil {
		return v
	}

	names, err := s.names.StarterNames(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "could not read the starter prompts' names for the routines", "error", err)

		return v
	}

	v.names = func(p spec.PromptRef) string { return names[p.Starter] }

	return v
}

// viewer returns the account signed in, "" for none or one that can't be read, logged.
func (s *Service) viewer(ctx context.Context) string {
	if s.accounts == nil {
		return ""
	}

	login, err := s.accounts.ViewerLogin(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "could not read the account signed in for the routines", "error", err)

		return ""
	}

	return login
}

// defaultMode returns the mode new routines start in, the default when the settings can't
// be read, logged.
func (s *Service) defaultMode(ctx context.Context) autopilotprefs.Mode {
	if s.prefs == nil {
		return autopilotprefs.DefaultMode
	}

	c, err := s.prefs.Autopilot(ctx)
	if err != nil {
		s.logger.WarnContext(ctx, "could not read the mode new routines start in; using the default", "error", err)

		return autopilotprefs.DefaultMode
	}

	return c.DefaultMode
}
