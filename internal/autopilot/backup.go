package autopilot

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/textutil"
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

// BackupRecords are what a backup's routines name, as the import's transaction holds them:
// the prompt library and the user's scripts, with the prompts and scripts the same import
// brought, and Links, the UUID here of each prompt the file brings, by its nickname in the
// file, which a routine's prompt is found among first, so a prompt the import renamed (as
// "Review 2" beside the user's own "Review") stays the one the routine runs.
type BackupRecords struct {
	Prompts []LibraryPrompt
	Scripts []MyScript
	Links   map[string]string
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
// a name. Routines are matched by UUID, then by name, ignoring case, each routine here once
// (backupMatch): a match whose definition, description, and mode are the file's, the
// prompts and scripts it names stamped as they are here, is left as it is, on or off,
// another is saved over and switched off, keeping its name here, and a routine this Mac
// lacks is added, off, at the end, under a free name. Replace also deletes every routine
// the file doesn't match, and puts the rest in the file's order. A routine here that a
// newer build wrote is never saved over or deleted, under either mode: it is left as it
// is, with a note. A routine whose mode this build doesn't know takes the mode new
// routines start in. The preview reads the library and the scripts as they are, before
// the import changes them; ApplyBackup works it out again on what the import's
// transaction holds.
func (bp *BackupPart) PlanBackup(ctx context.Context, b Bundle, mode backup.Mode) (BackupPlan, error) {
	current, err := bp.s.store.all(ctx)
	if err != nil {
		return BackupPlan{}, err
	}

	out := BackupPlan{mode: mode, viewer: bp.s.viewer(ctx)}
	v := bp.s.view(ctx)
	pv := &backupPreview{
		current: current, matched: map[int64]bool{}, taken: slices.Clone(current), recs: bp.s.liveRecords(ctx),
		names: v.names, defaultMode: bp.s.defaultMode(ctx),
	}

	for _, r := range b.Routines {
		pv.plan(&out, r)
	}

	if mode == backup.ModeReplace {
		out.planRemoves(current, pv.matched, v)
	}

	return out, nil
}

// backupPreview is what PlanBackup works each of the file's routines out against: the
// routines here, those matched so far, the names taken, what the routines name here now,
// how the sentence names a starter, and the mode new routines start in.
type backupPreview struct {
	current     []queries.AutopilotRoutine
	matched     map[int64]bool
	taken       []queries.AutopilotRoutine
	recs        BackupRecords
	names       spec.PromptNames
	defaultMode autopilotprefs.Mode
}

// plan works out r, one of the file's routines, into out: left out with a note, or what
// importing it does, listed under the name it is saved under.
func (pv *backupPreview) plan(out *BackupPlan, r BundleRoutine) {
	k, note := checkImport(r, pv.defaultMode)
	if note == "" && out.holds(k.name) {
		note = fmt.Sprintf("The routine “%s” was left out: the file holds another of that name.", k.name)
	}

	if note != "" {
		out.AddNote(note)

		return
	}

	out.routines = append(out.routines, k)

	stamped := k.stampedWith(pv.recs)
	label, change, kept := stamped.change(pv.current, pv.matched, pv.recs)

	if kept != "" {
		out.AddNote(newerNote(kept))
	}

	if change == backup.ChangeAdd {
		label = freeName(k.name, pv.taken)
		pv.taken = append(pv.taken, queries.AutopilotRoutine{Name: label})
	}

	out.Add(backup.Item{Label: label, Change: change, Detail: spec.Sentence(stamped.spec.spec, pv.names)})
}

// liveRecords reads what the routines name as the pool holds it now, for a preview: none of
// a source that can't be read (logged).
func (s *Service) liveRecords(ctx context.Context) BackupRecords {
	setup := serviceSetup{s: s}
	library, _ := setup.Library(ctx)

	var mine []MyScript

	if s.myScripts != nil {
		got, err := s.myScripts.MyScripts(ctx)
		if err != nil {
			s.logger.WarnContext(ctx, "could not read the user's scripts to preview a backup's routines", "error", err)
		}

		mine = got
	}

	return BackupRecords{Prompts: library, Scripts: mine}
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

// backupMatch returns the index in current of the routine k, one of a backup's routines,
// is, which matched doesn't hold yet, -1 for none: the one with k's UUID, whatever it is
// called now, else the one with k's name, ignoring case. A preview and its apply match
// alike.
func backupMatch(current []queries.AutopilotRoutine, matched map[int64]bool, k keptRoutine) int {
	if u, ok := textutil.CanonicalUUID(strings.TrimSpace(k.uuid)); ok {
		if i := slices.IndexFunc(current, func(c queries.AutopilotRoutine) bool {
			return !matched[c.ID] && c.UUID == u
		}); i >= 0 {
			return i
		}
	}

	return slices.IndexFunc(current, func(c queries.AutopilotRoutine) bool {
		return !matched[c.ID] && sameName(c.Name, k.name)
	})
}

// change says what importing k, stamped with recs, does to the routines in current: adds
// it, saves over the routine it matches, or leaves that routine as it is, marking the
// routine matched, with the name to list it under: k's, or, for a routine here its UUID
// matched under another name, the name it keeps here. A routine a newer build wrote is
// left as it is, and its name returned for a note.
func (k keptRoutine) change(current []queries.AutopilotRoutine, matched map[int64]bool, recs BackupRecords,
) (string, backup.Change, string) {
	i := backupMatch(current, matched, k)
	if i < 0 {
		return k.name, backup.ChangeAdd, ""
	}

	row := current[i]
	matched[row.ID] = true

	label := k.name
	if !sameName(row.Name, k.name) {
		label = row.Name
	}

	switch {
	case newerBuild(row):
		return label, backup.ChangeSame, row.Name
	case k.same(row, recs):
		return label, backup.ChangeSame, ""
	default:
		return label, backup.ChangeUpdate, ""
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
	name, err := draft.CheckName(r.Name)
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
	k.name, k.uuid = name, r.UUID

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

// stampedWith returns k with each prompt and script its spec names stamped with what it is
// in recs (draft.Stamp), a prompt the file brings found first through recs.Links; a spec
// that doesn't load, or that would grow past spec.MaxBytes, stays as it is.
func (k keptRoutine) stampedWith(recs BackupRecords) keptRoutine {
	if !k.spec.loads {
		return k
	}

	sp := spec.Clone(k.spec.spec)
	recs.linkIn(&sp)
	draft.Stamp(recs.Prompts, draftScripts(recs.Scripts), &sp)

	data, err := spec.Encode(spec.Normalize(sp))
	if err != nil {
		return k
	}

	loaded, err := spec.Load(data)
	if err != nil {
		return k
	}

	k.spec.spec, k.spec.data, k.spec.version = loaded, string(data), int64(loaded.Version)

	return k
}

// linkIn names each prompt sp's steps name by nickname alone that the file brings by the
// UUID the import gave it here (Links), whatever UUID the file named it by.
func (r BackupRecords) linkIn(sp *spec.Spec) {
	if len(r.Links) == 0 {
		return
	}

	for _, ref := range spec.PromptRefs(sp) {
		if ref.Starter != "" {
			continue
		}

		if u, ok := r.Links[ref.Nickname]; ok && u != "" {
			ref.UUID = u
		}
	}
}

// stampedText returns the stored spec data with each prompt and script it names stamped
// with what it is in r, encoded as stored; "" for a spec that won't load.
func (r BackupRecords) stampedText(data string) string {
	sp, ok := loadedSpec(data)
	if !ok {
		return ""
	}

	draft.Stamp(r.Prompts, draftScripts(r.Scripts), &sp)

	out, err := spec.Encode(spec.Normalize(sp))
	if err != nil {
		return ""
	}

	return string(out)
}

// same reports whether k, stamped with recs, is the routine row as it stands: the same
// definition, the prompts and scripts it names stamped alike, description, and mode.
func (k keptRoutine) same(row queries.AutopilotRoutine, recs BackupRecords) bool {
	stored := recs.stampedText(row.Spec)

	return stored != "" && stored == k.spec.data && row.Description == k.desc && storedMode(row.Mode) == k.mode
}

// ApplyBackup stores plan's routines inside tx, an import's transaction, and returns what
// it changed, each routine of the file's stamped with recs, what tx holds, and matched as
// PlanBackup matched it: each one saved over is switched off and keeps its name, each one
// added is off, under a free name, and under Replace each routine the file doesn't match is
// deleted and the rest put in the file's order. Merge adds at the end, in the file's order,
// and deletes nothing.
func (bp *BackupPart) ApplyBackup(ctx context.Context, tx *sql.Tx, plan BackupPlan, recs BackupRecords,
) (backup.Count, error) {
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
		matched: map[int64]bool{}, recs: recs, taken: slices.Clone(rows),
	}

	if !im.replace {
		im.order = slices.Clone(rows)
	}

	for _, k := range plan.routines {
		if err = im.place(ctx, k.stampedWith(recs)); err != nil {
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
// those a routine of the file's matched, the names taken, the order it stores, and what it
// did.
type routineImport struct {
	q       *queries.Queries
	clock   clock.Clock
	viewer  string
	rows    []queries.AutopilotRoutine
	replace bool
	recs    BackupRecords

	matched map[int64]bool
	taken   []queries.AutopilotRoutine
	order   []queries.AutopilotRoutine
	count   backup.Count
}

// place stores k: over the routine it matches (backupMatch), which keeps its name and is
// switched off unless it is the same or a newer build wrote it, either of which is left as
// it is, or else as a new routine, off, at the end, under a free name.
func (im *routineImport) place(ctx context.Context, k keptRoutine) error {
	i := backupMatch(im.rows, im.matched, k)
	if i < 0 {
		k.account, k.name = im.viewer, freeName(k.name, im.taken)

		row, err := insertKept(ctx, im.q, im.clock, k, int64(len(im.rows)+len(im.order)))
		if err != nil {
			return err
		}

		im.count = im.count.With(backup.ChangeAdd)
		im.order = append(im.order, row)
		im.taken = append(im.taken, row)

		return nil
	}

	row := im.rows[i]
	im.matched[row.ID] = true

	if im.replace {
		im.order = append(im.order, row)
	}

	if newerBuild(row) || k.same(row, im.recs) {
		im.count = im.count.With(backup.ChangeSame)

		return nil
	}

	account := row.AccountLogin
	if account == "" {
		account = im.viewer
	}

	// A routine a backup rewrites is no longer the one its user accepted as merging past the
	// rules: that acceptance is never a backup's to carry.
	_, err := im.q.UpdateRoutine(ctx, queries.UpdateRoutineParams{
		AccountLogin: account, Name: row.Name, NameKey: draft.NameKey(row.Name), Description: k.desc, Mode: string(k.mode),
		Spec: k.spec.data, SpecVersion: k.spec.version, PastRulesHash: "", UpdatedAt: im.clock.Now(), ID: row.ID,
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
