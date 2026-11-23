package prompts

import (
	"context"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
)

// Seed adds each starter prompt this build ships that was never added before, and makes
// the review and work-on-issue starters the default prompts, and each starter reply the
// default reply for its slot, where no default is set yet.
// A starter that was added once is never added again, so one the user deleted stays
// deleted. A starter whose nickname is taken gets a free one. It then gives each copy of a
// starter whose text is still one of the starter's earlier texts the text this build ships
// (Starter.Earlier), keeping the copy's title, nickname, and settings, and fills each
// situation's slot never filled before (see seedSlots). First it gives a UUID to every
// prompt without one, as a build from before UUIDs inserts them. It does all of it or, when
// a write fails, none.
func Seed(ctx context.Context, s *Store) error {
	return s.inTx(ctx, func(st *Store) error {
		if _, err := st.giveUUIDs(ctx); err != nil {
			return err
		}

		seeded, err := st.seeded(ctx)
		if err != nil {
			return err
		}

		if seeded, err = seedMissing(ctx, st, seeded); err != nil {
			return err
		}

		if err = st.setSeeded(ctx, seeded); err != nil {
			return err
		}

		if err = refreshStarters(ctx, st); err != nil {
			return err
		}

		return seedSlots(ctx, st)
	})
}

// seedSlots fills each situation's slot that was never filled before, once, in a new
// library and in one that had its starters before the slot existed alike: a slot with no
// default gets the library's copy of the slot's starter (DefaultSlot.starter), when it
// holds one that fits, and the slot is recorded as filled either way. So a slot the user
// cleared, or whose starter they deleted, is left as they left it, and a pull request in
// that situation launches with the prompt it would have anyway.
func seedSlots(ctx context.Context, st *Store) error {
	filled, err := st.seededSlots(ctx)
	if err != nil {
		return err
	}

	var fill []DefaultSlot

	for _, slot := range situationSlots() {
		if !slices.Contains(filled, string(slot)) {
			fill = append(fill, slot)
		}
	}

	if len(fill) == 0 {
		return nil
	}

	lib, err := st.Library(ctx)
	if err != nil {
		return err
	}

	for _, slot := range fill {
		if err = fillSlot(ctx, st, lib, slot); err != nil {
			return err
		}

		filled = append(filled, string(slot))
	}

	return st.setSeededSlots(ctx, filled)
}

// fillSlot makes the library's copy of slot's starter the slot's default, when lib holds
// one that fits and the slot has none.
func fillSlot(ctx context.Context, st *Store, lib Library, slot DefaultSlot) error {
	p, ok := slotStarterCopy(lib.Prompts, slot)
	if !ok || lib.Defaults.Slot(slot) != 0 {
		return nil
	}

	if err := st.setSituationDefault(ctx, slot, p.ID); err != nil {
		return fmt.Errorf("make %q %s: %w", p.Nickname, slot.describe(), err)
	}

	return nil
}

// slotStarterCopy returns the first of all, by nickname, that came from slot's starter and fits
// slot, and whether there is one.
func slotStarterCopy(all []Prompt, slot DefaultSlot) (Prompt, bool) {
	for _, p := range all {
		if p.StarterKey != "" && p.StarterKey == slot.starter() && slot.takes(p.Purpose, p.ReplyAction, p.AppliesTo) {
			return p, true
		}
	}

	return Prompt{}, false
}

// refreshStarters gives every stored copy of a starter whose text is still one of the
// starter's earlier texts the text this build ships. Only the text changes: the copy's
// title, nickname, and settings, which the user may have changed without touching the
// text, stay.
func refreshStarters(ctx context.Context, st *Store) error {
	all, err := st.All(ctx)
	if err != nil {
		return err
	}

	for _, p := range all {
		current, ok := upToDate(p)
		if !ok {
			continue
		}

		if err = st.update(ctx, p.ID, current); err != nil {
			return fmt.Errorf("bring the %q starter prompt up to date: %w", p.Nickname, err)
		}
	}

	return nil
}

// seedMissing adds every starter not in seeded and returns seeded with their keys.
func seedMissing(ctx context.Context, st *Store, seeded []string) ([]string, error) {
	defaults, err := st.storedDefaults(ctx)
	if err != nil {
		return nil, err
	}

	for _, starter := range Starters() {
		if slices.Contains(seeded, starter.Key) {
			continue
		}

		if err = seedStarter(ctx, st, starter, defaults); err != nil {
			return nil, err
		}

		seeded = append(seeded, starter.Key)
	}

	return seeded, nil
}

// seedStarter adds one starter, and makes it the default for its kind when it is the
// review or work-on-issue starter and no default is set, or the default reply for its slot
// when that slot has none.
func seedStarter(ctx context.Context, st *Store, starter Starter, defaults DefaultPrompts) error {
	nickname, err := st.freeNickname(ctx, starter.Nickname)
	if err != nil {
		return err
	}

	in := starter.input()
	in.Nickname = nickname

	p := fromInput(in, agent.Checked{Models: starter.Models, Args: starter.ExtraArgs})
	p.StarterKey = starter.Key

	id, err := st.insert(ctx, p)
	if err != nil {
		return fmt.Errorf("add the %q starter prompt: %w", starter.Name, err)
	}

	switch {
	case starter.Key == StarterReviewPR && defaults.PR == 0:
		return st.setDefault(ctx, item.KindPR, id)
	case starter.Key == StarterWorkOnIssue && defaults.Issue == 0:
		return st.setDefault(ctx, item.KindIssue, id)
	case starter.Slot != "" && defaults.Replies.For(starter.Slot) == 0:
		return st.setReplyDefault(ctx, starter.Slot, id)
	default:
		return nil
	}
}
