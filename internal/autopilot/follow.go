package autopilot

import (
	"context"
	"fmt"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

// A routine names the prompts its steps run by a starter's key (the user's copy of that
// starter, whatever they renamed it) or else by its nickname, so it works on another Mac
// too. The prompt library learns from the PromptFollower which routines use each prompt,
// so its usage names them and a prompt one runs can't be deleted; and it tells the
// follower of a prompt renamed, which writes the new nickname into every routine that
// named the old one, so renaming a prompt never breaks a routine.

// PromptUse is one place a routine names a prompt: by its starter's key, else its
// nickname.
type PromptUse struct {
	Routine  string
	Starter  string
	Nickname string
}

// PromptFollower is the prompt library's face of the routines: what they name, and the
// renames they follow. It is a type of its own, never the bound Service, so the window
// can't reach it.
type PromptFollower struct {
	s *Service
}

// NewPromptFollower returns the follower over s.
func NewPromptFollower(s *Service) *PromptFollower {
	return &PromptFollower{s: s}
}

// Uses returns every place a routine not deleted names a prompt: its agent step's (but the
// item's own default) and its GitHub step's replies. A routine that won't load names none.
func (f *PromptFollower) Uses(ctx context.Context) ([]PromptUse, error) {
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

		for _, ref := range promptRefs(sp) {
			out = append(out, PromptUse{Routine: row.Name, Starter: ref.Starter, Nickname: ref.Nickname})
		}
	}

	return out, nil
}

// promptRefs returns the prompts sp's steps name, but the item's own default.
func promptRefs(sp spec.Spec) []spec.PromptRef {
	refs := promptRefPtrs(&sp)
	out := make([]spec.PromptRef, 0, len(refs))

	for _, ref := range refs {
		out = append(out, *ref)
	}

	return out
}

// Renamed writes the nickname to in place of from, ignoring case, in every routine not
// deleted that names a prompt by from (a starter's key is left as it is, since it follows
// its prompt on its own), in one transaction, and returns how many routines changed. A
// routine that won't load is left as it is. Once they are written, the routines' copy and
// searches are built again, as after any change.
func (f *PromptFollower) Renamed(ctx context.Context, from, to string) (int, error) {
	from, to = strings.TrimSpace(from), strings.TrimSpace(to)
	if from == "" || to == "" || from == to || f.s.store.db == nil {
		return 0, nil
	}

	changed := 0

	err := f.s.store.inTx(ctx, func(q *queries.Queries) error {
		var err error

		changed, err = f.renameAll(ctx, q, from, to)

		return err
	})
	if err != nil {
		return 0, fmt.Errorf("follow the prompt renamed from %q: %w", from, err)
	}

	if changed > 0 {
		f.s.changed(ctx)
	}

	return changed, nil
}

// renameAll writes to for from in every routine through q, and returns how many changed.
func (f *PromptFollower) renameAll(ctx context.Context, q *queries.Queries, from, to string) (int, error) {
	rows, err := q.ListRoutines(ctx)
	if err != nil {
		return 0, err
	}

	changed := 0

	for _, row := range rows {
		done, renameErr := f.rename(ctx, q, row, from, to)
		if renameErr != nil {
			return 0, renameErr
		}

		changed += countIf(done)
	}

	return changed, nil
}

// rename writes to for from in row's spec, and reports whether it changed it.
func (f *PromptFollower) rename(ctx context.Context, q *queries.Queries, row queries.AutopilotRoutine, from, to string,
) (bool, error) {
	sp, ok := loadedSpec(row.Spec)
	if !ok || !renameIn(&sp, from, to) {
		return false, nil
	}

	data, err := spec.Encode(spec.Normalize(sp))
	if err != nil {
		return false, err
	}

	// The user's acceptance of the routine as one that merges past the rules follows the
	// rename, which changes no step but a prompt's nickname, only while it stood.
	hash := ""
	if accepted(row) {
		hash = pastRulesHash(string(data))
	}

	n, err := q.SetRoutineSpec(ctx, queries.SetRoutineSpecParams{
		Spec: string(data), PastRulesHash: hash, UpdatedAt: f.s.clock.Now(), ID: row.ID, Was: row.Spec,
	})

	return n > 0, err
}

// renameIn writes to for each prompt sp's steps name by the nickname from, ignoring case,
// and reports whether it wrote any.
func renameIn(sp *spec.Spec, from, to string) bool {
	moved := false

	for _, ref := range promptRefPtrs(sp) {
		if ref.Starter == "" && strings.EqualFold(ref.Nickname, from) {
			ref.Nickname, moved = to, true
		}
	}

	return moved
}

// promptRefPtrs returns the prompts sp's steps name, as promptRefs does, to change them.
func promptRefPtrs(sp *spec.Spec) []*spec.PromptRef {
	var out []*spec.PromptRef

	for i := range sp.Steps {
		if a := sp.Steps[i].Agent; a != nil && !a.Prompt.ItemDefault {
			out = append(out, &a.Prompt)
		}

		for j := range sp.Steps[i].GitHub {
			if r := sp.Steps[i].GitHub[j].Reply; r != nil {
				out = append(out, &r.Prompt)
			}
		}
	}

	return out
}
