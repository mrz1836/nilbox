package autopilot

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// A routine names each prompt its steps run by the prompt's UUID, beside a starter's key or
// a nickname, and each of the user's scripts by its UUID, beside its ID and name: the UUID
// finds the record whatever it is renamed, reset, imported over, or renumbered as, and the
// fallback finds it in a library that never had that UUID, as on another Mac. The prompt
// library learns from the Follower which routines use each prompt, so its usage names them
// and a prompt one runs can't be deleted; and the library, and Repo scripts, tell the
// Follower of every change to them, after which it stamps every routine with what it names
// now (Follow): the UUIDs, and the fallbacks brought up to date. Core runs it once at
// every start too, which gives a routine stored before UUIDs its own.

// PromptUse is one place a routine names a prompt: by its UUID, else its starter's key,
// else its nickname.
type PromptUse struct {
	Routine  string
	UUID     string
	Starter  string
	Nickname string
}

// PromptRename is a prompt's nickname changed, from and to, which Follow replays on a
// routine that names the prompt by its old nickname alone.
type PromptRename struct {
	From string
	To   string
}

// Follower is the prompt library's and Repo scripts' face of the routines: what they
// name, and the changes they follow. It is a type of its own, never the bound Service, so
// the window can't reach it.
type Follower struct {
	s *Service

	// mu makes each Follow read and write before the next one starts, so two never stamp
	// a routine from libraries read at different times.
	mu sync.Mutex
}

// NewFollower returns the follower over s.
func NewFollower(s *Service) *Follower {
	return &Follower{s: s}
}

// Uses returns every place a routine not deleted names a prompt: its agent step's (but the
// item's own default) and its GitHub step's replies. A routine that won't load names none.
func (f *Follower) Uses(ctx context.Context) ([]PromptUse, error) {
	rows, err := f.s.store.all(ctx)
	if err != nil {
		return nil, err
	}

	out := []PromptUse{}

	for _, row := range rows {
		sp, ok := loadedSpec(row.Spec)
		if !ok {
			continue
		}

		for _, ref := range spec.PromptRefs(&sp) {
			out = append(out, PromptUse{Routine: row.Name, UUID: ref.UUID, Starter: ref.Starter, Nickname: ref.Nickname})
		}
	}

	return out, nil
}

// Follow stamps every routine not deleted with what its steps name now (draft.Stamp): each
// prompt's and script's UUID, and its fallback brought up to date, a prompt's nickname and
// a script's ID and name. First it replays renames, in order, on each prompt a routine
// names by a nickname alone that no UUID resolves, as one stored before UUIDs, so a rename
// never breaks it; it gives every routine without a UUID its own, the deleted ones
// included; and it writes a routine stored in an older version of the format in this
// one's. It reads the library and the scripts, then every routine, and writes only when
// something changed, in one transaction, each routine only while its spec is still the
// one read, keeping the user's acceptance of it as one that merges past the rules. A write
// that changes no name the user reads, only UUIDs or the version, keeps when the routine
// last changed. A routine that won't load is left as it is. It returns how many routines it
// wrote, and builds the routines' copy and searches again only when it wrote any, so a
// database that came back empty never overwrites the copy.
func (f *Follower) Follow(ctx context.Context, renames []PromptRename) (int, error) {
	if f.s.store.db == nil {
		return 0, nil
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	writes, unnamed, err := f.plan(ctx, renames)
	if err != nil || len(writes) == 0 && len(unnamed) == 0 {
		return 0, err
	}

	changed := 0

	err = f.s.store.inTx(ctx, func(q *queries.Queries) error {
		var writeErr error

		changed, writeErr = f.write(ctx, q, writes, unnamed)

		return writeErr
	})
	if err != nil {
		return 0, fmt.Errorf("follow the prompts and scripts: %w", err)
	}

	if changed > 0 {
		f.s.changed(ctx)
	}

	return changed, nil
}

// plan reads what the routines name and every routine, and returns what Follow writes:
// each routine whose spec stamping changes, and the IDs of those without a UUID.
func (f *Follower) plan(ctx context.Context, renames []PromptRename) ([]followWrite, []int64, error) {
	library, mine, err := f.s.records(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("follow the prompts and scripts: %w", err)
	}

	rows, err := f.s.store.all(ctx)
	if err != nil {
		return nil, nil, err
	}

	unnamed, err := f.s.store.q.RoutinesWithoutUUID(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("read the routines without a UUID: %w", err)
	}

	var writes []followWrite

	for _, row := range rows {
		if w, ok := followed(row, library, mine, renames); ok {
			writes = append(writes, w)
		}
	}

	return writes, unnamed, nil
}

// followWrite is a routine Follow rewrites: its row as read, its spec as stamped, and
// whether a name the user reads changed.
type followWrite struct {
	row     queries.AutopilotRoutine
	data    string
	version int
	named   bool
}

// write stores writes and gives each of unnamed a UUID, through q, and returns how many
// routines changed, each once.
func (f *Follower) write(ctx context.Context, q *queries.Queries, writes []followWrite, unnamed []int64,
) (int, error) {
	changed := map[int64]bool{}
	now := f.s.clock.Now()

	for _, w := range writes {
		wrote, err := w.store(ctx, q, now)
		if err != nil {
			return 0, err
		}

		if wrote {
			changed[w.row.ID] = true
		}
	}

	for _, id := range unnamed {
		if err := q.SetRoutineUUID(ctx, queries.SetRoutineUUIDParams{UUID: textutil.NewUUID(), ID: id}); err != nil {
			return 0, fmt.Errorf("give the routine %d a UUID: %w", id, err)
		}

		changed[id] = true
	}

	return len(changed), nil
}

// store writes w through q while its spec is still the one read, stamped now when a name
// the user reads changed, and reports whether it did.
func (w followWrite) store(ctx context.Context, q *queries.Queries, now time.Time) (bool, error) {
	// The user's acceptance of the routine as one that merges past the rules follows a
	// write that changes no step but the names of what it runs, only while it stood.
	hash := ""
	if accepted(w.row) {
		hash = pastRulesHash(w.data)
	}

	at := w.row.UpdatedAt
	if w.named {
		at = now
	}

	n, err := q.SetRoutineSpec(ctx, queries.SetRoutineSpecParams{
		Spec: w.data, SpecVersion: int64(w.version), PastRulesHash: hash, UpdatedAt: at, ID: w.row.ID,
		Was: w.row.Spec,
	})
	if err != nil {
		return false, fmt.Errorf("save the routine %q: %w", w.row.Name, err)
	}

	return n > 0, nil
}

// followed works out row's spec renamed and stamped, and whether it differs from the one
// stored: false for a routine that won't load, or whose spec Follow leaves as it is.
func followed(row queries.AutopilotRoutine, library []draft.Prompt, mine []draft.Script, renames []PromptRename,
) (followWrite, bool) {
	sp, ok := loadedSpec(row.Spec)
	if !ok {
		return followWrite{}, false
	}

	before := readNames(sp)
	renameIn(&sp, library, renames)

	data, err := spec.Encode(spec.Normalize(sp))
	if err != nil {
		return followWrite{}, false
	}

	if stamped, stampedData, ok := draft.Stamped(library, mine, sp); ok {
		sp, data = stamped, stampedData
	}

	if len(data) > spec.MaxBytes || string(data) == row.Spec {
		return followWrite{}, false
	}

	return followWrite{row: row, data: string(data), version: sp.Version, named: !slices.Equal(before, readNames(sp))}, true
}

// renameIn replays renames, in order, on each prompt sp's steps name by a nickname alone
// (a starter's key follows its prompt on its own) that no prompt of library has the UUID
// of, ignoring case.
func renameIn(sp *spec.Spec, library []draft.Prompt, renames []PromptRename) {
	for _, ref := range spec.PromptRefs(sp) {
		if ref.Starter != "" || resolvesByUUID(library, *ref) {
			continue
		}

		for _, r := range renames {
			from, to := strings.TrimSpace(r.From), strings.TrimSpace(r.To)
			if from != "" && to != "" && strings.EqualFold(ref.Nickname, from) {
				ref.Nickname = to
			}
		}
	}
}

// resolvesByUUID reports a prompt ref whose UUID a prompt of library has.
func resolvesByUUID(library []draft.Prompt, ref spec.PromptRef) bool {
	return ref.UUID != "" && slices.ContainsFunc(library, func(p draft.Prompt) bool { return p.UUID == ref.UUID })
}

// readNames lists the names of what sp's steps run as the user reads them: each prompt's
// starter or nickname, and each script's name.
func readNames(sp spec.Spec) []string {
	refs, steps := spec.PromptRefs(&sp), spec.ScriptSteps(&sp)
	out := make([]string, 0, len(refs)+len(steps))

	for _, ref := range refs {
		out = append(out, ref.Starter+"\x00"+ref.Nickname)
	}

	for _, sc := range steps {
		out = append(out, sc.Name)
	}

	return out
}

// records reads what the routines name: the prompt library, and the user's scripts, each
// none without a source; a source that can't be read is an error.
func (s *Service) records(ctx context.Context) ([]draft.Prompt, []draft.Script, error) {
	var (
		library []draft.Prompt
		mine    []draft.Script
	)

	if s.library != nil {
		got, err := s.library.Prompts(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("read the prompt library: %w", err)
		}

		library = got
	}

	if s.myScripts != nil {
		got, err := s.myScripts.MyScripts(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("read your scripts: %w", err)
		}

		mine = draftScripts(got)
	}

	return library, mine, nil
}

// draftScripts returns the user's scripts as a script step names them.
func draftScripts(mine []MyScript) []draft.Script {
	out := make([]draft.Script, 0, len(mine))
	for _, m := range mine {
		out = append(out, draft.Script{UUID: m.UUID, ID: m.ID, Name: m.Name})
	}

	return out
}

// stamped returns sp stamped with what it names in the library and the user's scripts now
// (draft.Stamped), and its text, as Save stores it; false when nothing changed, or the
// library or the scripts can't be read, which the engine checks again before each start.
func (s *Service) stamped(ctx context.Context, sp spec.Spec) (spec.Spec, []byte, bool) {
	setup := s.liveSetup()
	library, _ := setup.Library(ctx)

	var mine []draft.Script
	if draft.HasStep(sp, catalog.StepScript) {
		mine, _ = setup.Scripts(ctx)
	}

	return draft.Stamped(library, mine, sp)
}
