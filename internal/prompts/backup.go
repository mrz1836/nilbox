package prompts

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// BackupPart is the prompt library's part of a backup of the user's setup: the whole library in
// portable form, and what importing one does. Core adapts it to a backup.Part; it is a type of its own, apart
// from the Service, so the window, which binds the Service, never reaches it.
type BackupPart struct {
	s *Service
}

// NewBackupPart returns the backup's part over s.
func NewBackupPart(s *Service) *BackupPart {
	return &BackupPart{s: s}
}

// Label names the default in the window's words, as in "Default approval reply".
func (s DefaultSlot) Label() string {
	switch s {
	case DefaultSlotPR:
		return "Default prompt for pull requests"
	case DefaultSlotIssue:
		return "Default prompt for issues"
	case DefaultSlotApprove:
		return "Default approval reply"
	case DefaultSlotApproveBots:
		return "Default approval reply for bots"
	case DefaultSlotRequestChanges:
		return "Default reply for requested changes"
	case DefaultSlotComment:
		return "Default comment reply"
	default:
		return "Default"
	}
}

// Bundle returns the whole library in portable form, as Export writes it: every prompt and
// reply, the defaults, and the record of the starters it was given.
func (bp *BackupPart) Bundle(ctx context.Context) (Bundle, error) {
	s := bp.s

	lib, err := s.store.Library(ctx)
	if err != nil {
		return Bundle{}, err
	}

	added, err := s.store.seeded(ctx)
	if err != nil {
		return Bundle{}, err
	}

	return newBundle(lib, added, nil, s.clock.Now(), s.app), nil
}

// BackupPlan is what applying a backup's prompt library does, as PlanBackup works it out.
type BackupPlan struct {
	backup.Plan

	// Prompts names, by their nicknames in the file, the prompts the library will hold for
	// the file, with what each is.
	Prompts map[string]backup.PromptKind

	bundle Bundle
	mode   backup.Mode

	// usage is where each stored prompt is a default, read before the transaction.
	usage map[int64]Usage

	// keep holds, under Replace, the stored prompts a launch default the import keeps
	// still names, each with the places that name it.
	keep map[int64]string
}

// PlanBackup works out what applying b, a backup's prompt library, does under mode,
// changing nothing. Merge plans as Import does, the file's defaults included; Replace
// matches the file's prompts to the library's by nickname, saving over each match, adding
// the rest, and deleting every prompt the file doesn't hold, but one a launch default
// still names, which is kept with a note. clears reports that the import replaces the
// launch defaults too, so none is kept for one.
func (bp *BackupPart) PlanBackup(ctx context.Context, b Bundle, mode backup.Mode, clears bool) (BackupPlan, error) {
	s := bp.s

	if len(b.Prompts) > maxBundlePrompts {
		return BackupPlan{}, ErrBundleTooLarge
	}

	lib, err := s.store.Library(ctx)
	if err != nil {
		return BackupPlan{}, err
	}

	usage, err := s.loadUsage(ctx)
	if err != nil {
		return BackupPlan{}, err
	}

	out := BackupPlan{bundle: b, mode: mode, usage: usage, keep: map[int64]string{}}

	if mode != backup.ModeReplace {
		out.Plan, out.Prompts = planMerge(lib, b, usage)

		return out, nil
	}

	if !clears {
		out.keep = namedByLaunchDefaults(usage)
	}

	out.Plan, out.Prompts = planReplace(lib, b, out.keep).preview(lib, b)

	return out, nil
}

// namedByLaunchDefaults returns each prompt a launch default names, with the places that
// name it.
func namedByLaunchDefaults(usage map[int64]Usage) map[int64]string {
	out := map[int64]string{}

	for id, u := range usage {
		if places := slices.Concat(u.Orgs, u.Repos); len(places) > 0 {
			out[id] = textutil.Series(sortedList(places))
		}
	}

	return out
}

// ApplyBackup applies plan inside tx, the import's transaction, working it out again on
// the library tx holds, and returns what it did. It records in links the ID each of the
// file's nicknames ended up under.
func (bp *BackupPart) ApplyBackup(ctx context.Context, tx *sql.Tx, plan BackupPlan, links map[string]int64,
) (backup.Count, error) {
	st := bp.s.store.WithTx(tx)

	var (
		count backup.Count
		ids   map[string]int64
		err   error
	)

	if plan.mode == backup.ModeReplace {
		count, ids, err = applyReplace(ctx, st, plan)
	} else {
		count, ids, err = applyMerge(ctx, st, plan)
	}

	if err != nil {
		return backup.Count{}, err
	}

	for nickname, id := range ids {
		links[nickname] = id
	}

	return count, nil
}

// BackupDone rewrites the library file once a backup's prompts are committed, and tells
// Changed, as every change to the library does.
func (bp *BackupPart) BackupDone(ctx context.Context) {
	bp.s.saved(ctx)
}

// planMerge works out the plan of importing b into lib as Import does: each prompt as the
// planner plans it, then each default it offers.
func planMerge(lib Library, b Bundle, usage map[int64]Usage) (backup.Plan, map[string]backup.PromptKind) {
	var p backup.Plan

	provides := map[string]backup.PromptKind{}
	pl := newPlanner(lib.Prompts, usage)
	plans := make([]planned, 0, len(b.Prompts))
	byID := promptsByID(lib.Prompts)

	for _, in := range b.Prompts {
		e := pl.plan(in)
		plans = append(plans, e)
		notePrompt(&p, e.Nickname, e.Notes)

		kind := kindOf(e.Prompt.Purpose, e.Prompt.AppliesTo)

		switch e.Outcome {
		case ImportAdd:
			p.Add(backup.Item{Label: e.Nickname, Change: backup.ChangeAdd})
		case ImportRename:
			p.Add(backup.Item{Label: e.Nickname, Change: backup.ChangeAdd, Detail: "renamed from “" + e.Prompt.Nickname + "”"})
		case ImportReplace:
			p.Add(backup.Item{Label: e.Nickname, Change: backup.ChangeUpdate, Detail: "saved over the starter"})
		case ImportSkip:
			held := byID[e.held]
			kind = kindOf(held.Purpose, held.AppliesTo)

			p.Add(backup.Item{Label: e.Nickname, Change: backup.ChangeSame})
		}

		if _, seen := provides[in.Nickname]; !seen {
			provides[in.Nickname] = kind
		}
	}

	for _, o := range offers(lib, plans, b.Prompts, b.Defaults) {
		p.Add(backup.Item{Label: o.Slot.Label(), Change: backup.ChangeUpdate, Detail: nowWords(o.Nickname, o.Current)})
	}

	return p, provides
}

// applyMerge imports plan's prompts and their defaults into the library st holds.
func applyMerge(ctx context.Context, st *Store, plan BackupPlan) (backup.Count, map[string]int64, error) {
	res, links, err := importInto(ctx, st, plan.bundle.Prompts, plan.usage)
	if err != nil {
		return backup.Count{}, nil, err
	}

	changed, err := restoreDefaults(ctx, st, plan.bundle.Defaults, links)
	if err != nil {
		return backup.Count{}, nil, err
	}

	return backup.Count{Add: res.Added, Update: res.Replaced + changed, Same: res.Skipped}, links, nil
}

// replaceStep is one of a file's prompts as Replace stores it.
type replaceStep struct {
	// file is the prompt's nickname in the file, and in the prompt as this build stores
	// it, with notes on what cleaning it changed.
	file  string
	in    BundlePrompt
	notes []string

	// change is ChangeAdd, ChangeUpdate, or ChangeSame; id is the stored prompt it is
	// saved over or matches, for the last two.
	change backup.Change
	id     int64

	// nickname is the nickname it is stored under.
	nickname string

	// keepStarter says whether an added prompt is stored with its starter key.
	keepStarter bool
}

// stored returns the step's prompt as Replace stores it.
func (r replaceStep) stored() Prompt {
	p := planned{ImportEntry: ImportEntry{Prompt: r.in, Nickname: r.nickname}, keepStarter: r.keepStarter}

	return p.stored()
}

// replacement is what Replace does to a library: each of the file's prompts, and the
// stored prompts it deletes and keeps.
type replacement struct {
	steps   []replaceStep
	removes []Prompt

	// kept are the stored prompts the file doesn't hold that a launch default still names,
	// with the places that do.
	kept []keptPrompt
}

// keptPrompt is a stored prompt Replace keeps, and the places that name it.
type keptPrompt struct {
	prompt Prompt
	places string
}

// planReplace works out what Replace does to lib: each of b's prompts saved over the
// stored prompt with its nickname, ignoring case, and its purpose, or else added under a
// free nickname; every stored prompt left over deleted, but those keep names.
func planReplace(lib Library, b Bundle, keep map[int64]string) replacement {
	var out replacement

	matched := map[int64]bool{}

	for _, raw := range b.Prompts {
		out.steps = append(out.steps, matchStep(lib.Prompts, matched, raw))
	}

	var taken []Prompt

	for _, q := range lib.Prompts {
		switch places, kept := keep[q.ID]; {
		case matched[q.ID]:
			taken = append(taken, q)
		case kept:
			out.kept = append(out.kept, keptPrompt{prompt: q, places: places})
			taken = append(taken, q)
		default:
			out.removes = append(out.removes, q)
		}
	}

	for i := range out.steps {
		if out.steps[i].change == backup.ChangeAdd {
			taken = append(taken, nameAdded(&out.steps[i], taken))
		}
	}

	return out
}

// matchStep works out the step for raw, one of a file's prompts: saved over the first
// stored prompt not yet matched with its nickname, ignoring case, and its purpose, which
// it then marks matched, or else added.
func matchStep(lib []Prompt, matched map[int64]bool, raw BundlePrompt) replaceStep {
	in, notes := cleanImport(raw)
	step := replaceStep{file: raw.Nickname, in: in, notes: textutil.NonNil(notes), change: backup.ChangeAdd}

	i := slices.IndexFunc(lib, func(q Prompt) bool {
		return !matched[q.ID] && strings.EqualFold(q.Nickname, in.Nickname) && purposeOf(q.Purpose) == purposeOf(in.Purpose)
	})
	if i < 0 {
		return step
	}

	q := lib[i]
	matched[q.ID] = true
	step.id, step.nickname, step.change = q.ID, in.Nickname, backup.ChangeUpdate

	if q.Nickname == in.Nickname && sameContent(q, in) {
		step.change = backup.ChangeSame
	}

	return step
}

// nameAdded names an added step: a nickname none of taken has, and its starter key when
// none of taken came from that starter. It returns the step's prompt, now taken too.
func nameAdded(step *replaceStep, taken []Prompt) Prompt {
	step.nickname = (&planner{lib: taken}).freeNickname(step.in.Nickname, -1)
	step.keepStarter = step.in.StarterKey != "" &&
		!slices.ContainsFunc(taken, func(q Prompt) bool { return q.StarterKey == step.in.StarterKey })

	return step.stored()
}

// preview returns the plan the window shows of r, applied to lib with b's defaults, and
// the prompts the library will hold for the file.
func (r replacement) preview(lib Library, b Bundle) (backup.Plan, map[string]backup.PromptKind) {
	var p backup.Plan

	provides := map[string]backup.PromptKind{}
	final := map[string]Prompt{}

	for _, step := range r.steps {
		notePrompt(&p, step.nickname, step.notes)
		p.Add(step.item())

		if _, seen := provides[step.file]; !seen {
			provides[step.file] = kindOf(step.in.Purpose, step.in.AppliesTo)
			final[step.file] = step.stored()
		}
	}

	for _, q := range r.removes {
		p.Add(backup.Item{Label: q.Nickname, Change: backup.ChangeRemove})
	}

	for _, k := range r.kept {
		p.AddNote(fmt.Sprintf("“%s” is kept: the launch defaults for %s use it.", k.prompt.Nickname, k.places))
	}

	previewDefaults(&p, lib, b.Defaults, final)

	return p, provides
}

// item is the step as the import dialog lists it.
func (r replaceStep) item() backup.Item {
	item := backup.Item{Label: r.nickname, Change: r.change}
	if r.change == backup.ChangeAdd && r.nickname != r.in.Nickname {
		item.Detail = "renamed from “" + r.in.Nickname + "”"
	}

	return item
}

// previewDefaults lists each of lib's defaults Replace changes: to the file's, by nickname
// among final (each of the file's nicknames with the prompt it becomes), where it fits; to
// none, for a reply slot the file names nothing that fits for.
func previewDefaults(p *backup.Plan, lib Library, defaults BundleDefaults, final map[string]Prompt) {
	byID := promptsByID(lib.Prompts)

	for _, slot := range DefaultSlots() {
		now := byID[lib.Defaults.Slot(slot)].Nickname
		next, ok := final[defaults.For(slot)]

		switch fits := ok && slot.takes(next.Purpose, next.ReplyAction, next.AppliesTo); {
		case fits && next.Nickname != now:
			p.Add(backup.Item{Label: slot.Label(), Change: backup.ChangeUpdate, Detail: nowWords(next.Nickname, now)})
		case !fits && now != "" && slot.reply():
			p.Add(backup.Item{Label: slot.Label(), Change: backup.ChangeUpdate, Detail: nowWords("", now)})
		}
	}
}

// applyReplace replaces the library st holds with plan's prompts and defaults.
func applyReplace(ctx context.Context, st *Store, plan BackupPlan) (backup.Count, map[string]int64, error) {
	lib, err := st.Library(ctx)
	if err != nil {
		return backup.Count{}, nil, err
	}

	r := planReplace(lib, plan.bundle, plan.keep)

	// Deleting first frees each nickname the file's prompts are stored under.
	removed, err := removeAll(ctx, st, r.removes)
	if err != nil {
		return backup.Count{}, nil, err
	}

	count, links, keys, err := storeSteps(ctx, st, r.steps)
	if err != nil {
		return backup.Count{}, nil, err
	}

	changed, err := replaceDefaults(ctx, st, lib.Defaults, plan.bundle.Defaults, links)
	if err != nil {
		return backup.Count{}, nil, err
	}

	count.Remove, count.Update = removed, count.Update+changed

	return count, links, recordStarters(ctx, st, append(slices.Clone(plan.bundle.StartersAdded), keys...))
}

// removeAll deletes the stored prompts removes and returns how many it deleted: one a launch
// default the import keeps still names stays.
func removeAll(ctx context.Context, st *Store, removes []Prompt) (int, error) {
	removed := 0

	for _, q := range removes {
		switch err := st.deleteRow(ctx, q.ID); {
		case store.IsForeignKeyViolation(err):
		case err != nil:
			return 0, err
		default:
			removed++
		}
	}

	return removed, nil
}

// storeSteps stores Replace's steps and returns what they did, the ID each of the file's
// nicknames ended up under (the first where two share one), and the starters they added.
func storeSteps(ctx context.Context, st *Store, steps []replaceStep) (backup.Count, map[string]int64, []string, error) {
	var (
		count backup.Count
		keys  []string
	)

	links := map[string]int64{}

	for _, step := range steps {
		id, err := storeStep(ctx, st, step)
		if err != nil {
			return backup.Count{}, nil, nil, err
		}

		if _, seen := links[step.file]; !seen {
			links[step.file] = id
		}

		count = count.With(step.change)

		if step.keepStarter {
			keys = append(keys, step.in.StarterKey)
		}
	}

	return count, links, keys, nil
}

// storeStep stores one of Replace's steps and returns the ID of its prompt.
func storeStep(ctx context.Context, st *Store, step replaceStep) (int64, error) {
	switch step.change {
	case backup.ChangeSame:
		return step.id, nil
	case backup.ChangeUpdate:
		if err := st.update(ctx, step.id, step.stored()); err != nil {
			return 0, fmt.Errorf("import %q: %w", step.nickname, err)
		}

		return step.id, nil
	case backup.ChangeAdd, backup.ChangeRemove:
	}

	id, err := st.insert(ctx, step.stored())
	if err != nil {
		return 0, fmt.Errorf("import %q: %w", step.nickname, err)
	}

	return id, nil
}

// replaceDefaults makes the library's defaults the file's, by nickname through links,
// each where its prompt fits; a slot the file names no prompt for, or one that doesn't
// fit, is left with none, the kinds' falling back to the first prompt that applies. It
// returns how many defaults changed from before.
func replaceDefaults(ctx context.Context, st *Store, before DefaultPrompts, defaults BundleDefaults,
	links map[string]int64,
) (int, error) {
	changed := 0

	for _, slot := range DefaultSlots() {
		p, ok, err := fittingPrompt(ctx, st, links, slot, defaults.For(slot))
		if err != nil {
			return 0, err
		}

		if err = setSlot(ctx, st, slot, p.ID); err != nil {
			return 0, err
		}

		// A kind's default left with none falls back to a prompt, which may be the same.
		if ok && before.Slot(slot) != p.ID || !ok && slot.reply() && before.Slot(slot) != 0 {
			changed++
		}
	}

	return changed, nil
}

// reply reports whether the slot is one of the default replies.
func (s DefaultSlot) reply() bool {
	_, kind := s.kind()

	return !kind
}

// notePrompt records each of a prompt's notes, saying which prompt it is about.
func notePrompt(p *backup.Plan, nickname string, notes []string) {
	for _, note := range notes {
		p.AddNote("“" + nickname + "”: " + note)
	}
}

// nowWords says what a default becomes and what it is now, as in "“LGTM” (now “Looks
// good”)"; "" names none.
func nowWords(next, now string) string {
	quote := func(s string) string {
		if s == "" {
			return "none"
		}

		return "“" + s + "”"
	}

	return quote(next) + " (now " + quote(now) + ")"
}

// kindOf says what a prompt with purpose applying to appliesTo is, for the launch
// defaults' check.
func kindOf(purpose Purpose, appliesTo AppliesTo) backup.PromptKind {
	return backup.PromptKind{Reply: purposeOf(purpose) == PurposeReply, AppliesTo: string(appliesTo)}
}

// promptsByID indexes prompts by ID.
func promptsByID(prompts []Prompt) map[int64]Prompt {
	out := make(map[int64]Prompt, len(prompts))
	for _, p := range prompts {
		out[p.ID] = p
	}

	return out
}
