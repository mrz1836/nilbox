package prompts

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// promptsTable is the table a backup's prompts are read from.
const promptsTable = "prompts"

// Names an import gives a prompt that came without any.
const (
	untitledName     = "Imported prompt"
	untitledNickname = "Imported"
)

// starterKeyPattern matches a starter key a prompt file may carry; any other is dropped.
var starterKeyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// ImportOutcome says what importing a prompt does.
type ImportOutcome string

// What importing a prompt does.
const (
	// ImportAdd adds the prompt under its own nickname.
	ImportAdd ImportOutcome = "add"

	// ImportRename adds the prompt under a free nickname, because another prompt has its
	// own.
	ImportRename ImportOutcome = "rename"

	// ImportReplace saves the prompt over the library's copy of the same starter, which was
	// never changed, so nothing of the user's is lost: the starter's text stays one Reset
	// away.
	ImportReplace ImportOutcome = "replace"

	// ImportSkip leaves the prompt out, because the library already holds it.
	ImportSkip ImportOutcome = "skip"
)

// ImportEntry is one prompt a source holds, and what importing it would do.
type ImportEntry struct {
	// Prompt is the prompt as it would be imported: its names trimmed to fit, and any
	// setting this build would refuse left out.
	Prompt BundlePrompt `json:"prompt"`

	// Outcome is what importing it would do.
	Outcome ImportOutcome `json:"outcome"`

	// Nickname is the nickname it would be saved under, or the nickname of the prompt that
	// already holds it, for ImportSkip.
	Nickname string `json:"nickname"`

	// Replaces is the nickname of the starter it would be saved over, for ImportReplace.
	Replaces string `json:"replaces"`

	// Notes says what was changed to import it, and what to fix before it can launch.
	Notes []string `json:"notes"`
}

// ImportResult says what Import did.
type ImportResult struct {
	// Added counts the prompts added, Replaced the starters saved over, and Skipped the
	// prompts the library already held.
	Added    int `json:"added"`
	Replaced int `json:"replaced"`
	Skipped  int `json:"skipped"`

	// First is the ID of the first prompt added or saved over, for the window to open, or 0
	// when there was none.
	First int64 `json:"first"`
}

// planned is an ImportEntry with what storing it needs.
type planned struct {
	ImportEntry

	// target is the ID of the prompt it is saved over, for ImportReplace.
	target int64

	// keepStarter says whether it is stored with its starter key: a library holds one
	// prompt per starter, so a second one is stored as the user's own.
	keepStarter bool
}

// stored returns the prompt as Import stores it.
func (p planned) stored() Prompt {
	out := Prompt{
		Name: p.Prompt.Name, Nickname: p.Nickname, AppliesTo: p.Prompt.AppliesTo, Body: p.Prompt.Body,
		Model: p.Prompt.Model, Effort: p.Prompt.Effort, PlanMode: p.Prompt.PlanMode, Worktree: p.Prompt.Worktree,
		ExtraArgs: slices.Clone(textutil.NonNil(p.Prompt.ExtraArgs)),
	}

	if p.keepStarter {
		out.StarterKey = p.Prompt.StarterKey
	}

	return out
}

// planner works out what importing prompts into a library does, one prompt at a time,
// each as though the ones before it were imported already. It decides only from the
// library it was given, so a preview and the import that follows it agree.
type planner struct {
	// lib is the library, with each prompt planned so far added or saved over. A prompt
	// planned to be added has ID 0.
	lib []Prompt

	// usage says where each stored prompt is a default.
	usage map[int64]Usage

	// replaced marks the stored prompts already planned to be saved over.
	replaced map[int64]bool

	// faithful plans every prompt as an addition, as a restore does: it skips nothing and
	// saves over nothing, so the library comes back exactly as it was.
	faithful bool
}

// newPlanner returns a planner for importing into lib, whose prompts are defaults where
// usage says.
func newPlanner(lib []Prompt, usage map[int64]Usage) *planner {
	return &planner{lib: slices.Clone(lib), usage: usage, replaced: map[int64]bool{}}
}

// plan says what importing in does, and records it.
func (pl *planner) plan(in BundlePrompt) planned {
	p, notes := cleanImport(in)
	e := planned{ImportEntry: ImportEntry{Prompt: p, Notes: textutil.NonNil(notes)}}

	if !pl.faithful {
		if held, ok := pl.holding(p); ok {
			e.Outcome, e.Nickname = ImportSkip, held.Nickname

			return e
		}

		if at, ok := pl.replaceable(p); ok {
			target := pl.lib[at]
			e.Outcome, e.Replaces, e.target, e.keepStarter = ImportReplace, target.Nickname, target.ID, true
			e.Nickname = pl.freeNickname(p.Nickname, at)
			pl.replaced[target.ID] = true
			pl.lib[at] = e.stored()
			pl.lib[at].ID, pl.lib[at].StarterKey = target.ID, target.StarterKey

			return e
		}
	}

	e.Outcome, e.Nickname = ImportAdd, pl.freeNickname(p.Nickname, -1)
	if e.Nickname != p.Nickname {
		e.Outcome = ImportRename
	}

	e.keepStarter = p.StarterKey != "" && !pl.hasStarter(p.StarterKey)
	pl.lib = append(pl.lib, e.stored())

	return e
}

// holding returns the library's prompt that already holds p: one with the same title,
// text, and settings, whatever its nickname, or, when p is a starter the user never
// changed, the library's copy of that starter.
func (pl *planner) holding(p BundlePrompt) (Prompt, bool) {
	untouched := untouchedImport(p)

	for _, q := range pl.lib {
		if (untouched && q.StarterKey == p.StarterKey) || sameContent(q, p) {
			return q, true
		}
	}

	return Prompt{}, false
}

// replaceable returns the index of the stored starter p would be saved over: the
// library's copy of p's starter, still as it was added, not yet saved over by this
// import, and only when p applies to every kind of item that copy is a default for.
func (pl *planner) replaceable(p BundlePrompt) (int, bool) {
	if p.StarterKey == "" {
		return 0, false
	}

	for i, q := range pl.lib {
		if q.ID == 0 || q.StarterKey != p.StarterKey || pl.replaced[q.ID] || !untouchedStored(q) {
			continue
		}

		covers := !slices.ContainsFunc(pl.usage[q.ID].Kinds, func(k item.Kind) bool { return !p.AppliesTo.Covers(k) })
		if covers {
			return i, true
		}
	}

	return 0, false
}

// hasStarter reports whether the library holds a prompt from the starter key.
func (pl *planner) hasStarter(key string) bool {
	return slices.ContainsFunc(pl.lib, func(q Prompt) bool { return q.StarterKey == key })
}

// freeNickname returns base when no prompt but the one at index except (-1 for none) has
// it, ignoring case, or else base followed by the lowest free number, as in "Review 2",
// cut so it stays within the nickname limit.
func (pl *planner) freeNickname(base string, except int) string {
	base = textutil.Clip(base, maxNicknameRunes)

	for n := 1; ; n++ {
		candidate := base

		if n > 1 {
			suffix := " " + strconv.Itoa(n)
			candidate = textutil.Clip(base, maxNicknameRunes-utf8.RuneCountInString(suffix)) + suffix
		}

		if !pl.taken(candidate, except) {
			return candidate
		}
	}
}

// taken reports whether a prompt but the one at index except has nickname, ignoring
// case.
func (pl *planner) taken(nickname string, except int) bool {
	for i, q := range pl.lib {
		if i != except && strings.EqualFold(q.Nickname, nickname) {
			return true
		}
	}

	return false
}

// cleanImport returns a prompt read from a file or a backup as this build would store
// it, with notes on what it changed and what to fix. It drops nothing the user wrote:
// names are trimmed to fit, a missing one is filled in, and a text with problems is kept
// for the user to fix in the editor. Only settings this build would refuse are left out,
// each with a note, so a file can never pass the agent a flag the editor would refuse.
func cleanImport(in BundlePrompt) (BundlePrompt, []string) {
	var notes []string

	p := in
	p.Name, p.Nickname = oneLine(p.Name), oneLine(p.Nickname)
	p.Body = strings.ReplaceAll(p.Body, "\r\n", "\n")
	p.Model = strings.TrimSpace(p.Model)
	p.Effort = strings.TrimSpace(p.Effort)
	p.ExtraArgs = slices.Clone(textutil.NonNil(p.ExtraArgs))

	if p.StarterKey = strings.TrimSpace(p.StarterKey); !starterKeyPattern.MatchString(p.StarterKey) {
		p.StarterKey = ""
	}

	p.Name, p.Nickname = cmpOr(p.Name, p.Nickname, untitledName), cmpOr(p.Nickname, p.Name, untitledNickname)
	p.Name, p.Nickname = textutil.Clip(p.Name, maxNameRunes), textutil.Clip(p.Nickname, maxNicknameRunes)

	switch p.AppliesTo {
	case AppliesToPR, AppliesToIssue, AppliesToAny:
	default:
		p.AppliesTo = AppliesToAny

		notes = append(notes, "It didn’t say what it’s for, so it applies to both pull requests and issues.")
	}

	if agent.ValidateModel(p.Model) != nil {
		notes = append(notes, fmt.Sprintf("Its model, %q, was left out: a model is one word, such as \"opus\".", p.Model))
		p.Model = ""
	}

	if agent.ValidateEffort(p.Effort) != nil {
		notes = append(notes, fmt.Sprintf("Its effort, %q, was left out: it isn’t one of the levels.", p.Effort))
		p.Effort = ""
	}

	if _, fields := agent.CheckOverrides("", "", agent.Join(p.ExtraArgs)); len(fields) > 0 {
		notes = append(notes, "Its extra flags were left out. "+fields[0].Message)
		p.ExtraArgs = []string{}
	}

	if t, _ := Parse(p.Body, p.AppliesTo); t == nil {
		notes = append(notes, "Its text has problems to fix before it can launch.")
	}

	return p, notes
}

// oneLine trims s and turns every run of spaces and control characters in it into one
// space.
func oneLine(s string) string {
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }), " ")
}

// cmpOr returns the first of values that is not empty.
func cmpOr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}

	return ""
}

// untouchedImport reports whether p is a starter exactly as this build adds it. Only
// the text decides, never the times a file records: a library brought back from a
// backup stamps its prompts anew, so their times say nothing of what was edited.
func untouchedImport(p BundlePrompt) bool {
	st, ok := starterFor(p.StarterKey)

	return ok && matchesStarter(bundledAsPrompt(p), st)
}

// untouchedStored reports whether a stored prompt is a starter exactly as this build adds
// it, so saving another version of it over it loses nothing.
func untouchedStored(p Prompt) bool {
	_, ok := starterFor(p.StarterKey)

	return ok && !p.Modified
}

// sameContent reports whether a stored prompt and an imported one have the same title,
// text, and settings, whatever their nicknames.
func sameContent(q Prompt, p BundlePrompt) bool {
	return q.Name == p.Name && q.AppliesTo == p.AppliesTo && q.Body == p.Body && q.Model == p.Model &&
		q.Effort == p.Effort && sameBool(q.PlanMode, p.PlanMode) && sameBool(q.Worktree, p.Worktree) &&
		slices.Equal(textutil.NonNil(q.ExtraArgs), textutil.NonNil(p.ExtraArgs))
}

// bundledAsPrompt returns a portable prompt as a Prompt, for comparing with a starter.
func bundledAsPrompt(p BundlePrompt) Prompt {
	return Prompt{
		Name: p.Name, Nickname: p.Nickname, AppliesTo: p.AppliesTo, Body: p.Body, Model: p.Model, Effort: p.Effort,
		PlanMode: p.PlanMode, Worktree: p.Worktree, ExtraArgs: textutil.NonNil(p.ExtraArgs), StarterKey: p.StarterKey,
	}
}

// importInto plans prompts against the library st holds and stores them, inside the
// transaction st is joined to. Starters it stores are recorded as added, so Seed never
// adds them again.
func importInto(ctx context.Context, st *Store, prompts []BundlePrompt, usage map[int64]Usage) (ImportResult, error) {
	lib, err := st.All(ctx)
	if err != nil {
		return ImportResult{}, err
	}

	pl := newPlanner(lib, usage)

	var (
		res  ImportResult
		keys []string
	)

	for _, in := range prompts {
		e := pl.plan(in)

		id, storeErr := storePlanned(ctx, st, e)
		if storeErr != nil {
			return ImportResult{}, storeErr
		}

		switch e.Outcome {
		case ImportSkip:
			res.Skipped++

			continue
		case ImportReplace:
			res.Replaced++
		case ImportAdd, ImportRename:
			res.Added++
		}

		if res.First == 0 {
			res.First = id
		}

		if e.keepStarter {
			keys = append(keys, e.Prompt.StarterKey)
		}
	}

	return res, recordStarters(ctx, st, keys)
}

// storePlanned stores one planned prompt and returns its ID, or 0 for one skipped.
func storePlanned(ctx context.Context, st *Store, e planned) (int64, error) {
	switch e.Outcome {
	case ImportSkip:
		return 0, nil
	case ImportReplace:
		if err := st.update(ctx, e.target, e.stored()); err != nil {
			return 0, fmt.Errorf("import %q: %w", e.Nickname, err)
		}

		return e.target, nil
	case ImportAdd, ImportRename:
		id, err := st.insert(ctx, e.stored())
		if err != nil {
			return 0, fmt.Errorf("import %q: %w", e.Nickname, err)
		}

		return id, nil
	default:
		return 0, fmt.Errorf("import %q: %w: %q", e.Nickname, ErrUnknownSource, e.Outcome)
	}
}

// recordStarters adds keys to the starters the library has been given.
func recordStarters(ctx context.Context, st *Store, keys []string) error {
	if len(keys) == 0 {
		return nil
	}

	seeded, err := st.seeded(ctx)
	if err != nil {
		return err
	}

	for _, key := range keys {
		seeded = appendUnique(seeded, key)
	}

	return st.setSeeded(ctx, seeded)
}

// promptColumns are the columns a backup's prompts are read from. A backup an older build
// wrote may lack some, and each one missing takes its default.
func promptColumns() []string {
	return []string{
		"id", "name", "nickname", "applies_to", "body", "model", "effort", "plan_mode", "worktree", "extra_args",
		"starter_key", "created_at", "updated_at",
	}
}

// readSnapshotBundle reads the prompt library in the database file at path, whichever
// build wrote it, as a Bundle: the prompts, and, where the backup still has them, the
// app's default prompts and the starters it had added. The file is never changed.
func readSnapshotBundle(ctx context.Context, path string) (Bundle, error) {
	b, _, err := readSnapshot(ctx, path)

	return b, err
}

// readSnapshot is readSnapshotBundle that also returns each prompt's nickname by its ID
// in the backup, for what in the backup names prompts by ID.
func readSnapshot(ctx context.Context, path string) (Bundle, map[int64]string, error) {
	snap, err := store.OpenSnapshot(ctx, path)
	if err != nil {
		return Bundle{}, nil, err
	}

	defer func() { _ = snap.Close() }()

	rows, err := snap.Rows(ctx, promptsTable, promptColumns()...)
	if err != nil {
		return Bundle{}, nil, err
	}

	b := Bundle{Format: BundleFormat, Version: BundleVersion, StartersAdded: []string{}, Prompts: []BundlePrompt{}}
	nicknames := map[int64]string{}

	for _, row := range rows {
		p := BundlePrompt{
			Name: row.Text("name"), Nickname: row.Text("nickname"), AppliesTo: AppliesTo(row.Text("applies_to")),
			Body: row.Text("body"), Model: row.Text("model"), Effort: row.Text("effort"),
			PlanMode: row.Bool("plan_mode"), Worktree: row.Bool("worktree"), ExtraArgs: row.Strings("extra_args"),
			StarterKey: row.Text("starter_key"), CreatedAt: row.Time("created_at"), UpdatedAt: row.Time("updated_at"),
		}

		if id, ok := row.Int("id"); ok {
			nicknames[id] = p.Nickname
		}

		b.Prompts = append(b.Prompts, p)
	}

	// The library's own records come back too where the backup has them; the prompts come
	// back without them.
	if values, prefsErr := prefs.New(snap.DB(), nil).GetMany(ctx, prefSeeded, prefDefaultPR, prefDefaultIssue); prefsErr == nil {
		if added, decodeErr := store.DecodeJSON[[]string](values[prefSeeded]); decodeErr == nil && added != nil {
			b.StartersAdded = added
		}

		b.Defaults.PR = nicknames[idText(values[prefDefaultPR])]
		b.Defaults.Issue = nicknames[idText(values[prefDefaultIssue])]
	}

	return b, nicknames, nil
}

// idText reads a stored prompt ID, or 0.
func idText(s string) int64 {
	id, _ := strconv.ParseInt(s, 10, 64) // a missing or bad value names no prompt

	return id
}
