package inbox

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/trust"
)

// The user's own triage. Each path clears the routine's mark on what it changes
// (item_local_state.set_aside_by for a dismissal, a snooze, or a restore, and pinned_by
// for a pin), so the window never says a routine did what the user did last; one that
// takes back what a routine did (a restore of what it dismissed or snoozed, a snooze over
// that, an unpin of what it pinned) tells the outcome sink so (Outcome.Reversed), which
// leaves that routine hands off the item (see triage_routine.go). Setting aside again what
// a routine's Restore brought back is the user handling the item, never a reversal.

// Dismiss hides an item from every list until it is restored, and returns the new
// snapshot. It replaces a snooze. Nothing is written to GitHub. Once the dismissal of an
// item that wasn't dismissed is stored, it tells the outcome sink of it.
func (s *Service) Dismiss(ctx context.Context, id int64) (Snapshot, error) {
	var (
		told   []Outcome
		stored int64
	)

	// The triage as it was is read in the dismissal's transaction, so a second click
	// tells nothing.
	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)
		now := s.now() // once the transaction holds the write lock, as every user path's

		was, found, err := s.triageBefore(ctx, q, id)
		if err != nil {
			return err
		}

		stored, err = q.SetTriage(ctx, queries.SetTriageParams{DismissedAt: &now, UpdatedAt: now, ID: id})
		if err != nil {
			return err
		}

		if found && was.DismissedAt == nil {
			told = triageOutcome(OutcomeDismissed, was, now)
		}

		return nil
	})

	switch {
	case err != nil:
		return Snapshot{}, fmt.Errorf("dismiss the item: %w", err)
	case stored == 0:
		return Snapshot{}, fmt.Errorf("%w (id %d)", item.ErrNotFound, id)
	}

	// The sink hears first, before the window and the engine are told the inbox changed, so
	// what takes back a routine's step is recorded before anything wakes the engine.
	s.settle(ctx, told)

	return s.changed(ctx)
}

// Snooze hides an item from every list for a number of days (snooze.MinDays to
// snooze.MaxDays), after which it is back on its own, and returns the new snapshot. It
// replaces a dismissal or an earlier snooze. The snooze ends at the same wall-clock time
// of day, so a change of the clocks in between does not move it.
//
// The item's activity as the last refresh read it is recorded with the snooze, so a
// later refresh can end it early when new activity passes the user's wake rules. Nothing
// is written to GitHub. A snooze over a routine's dismissal or snooze takes that back,
// which it tells the outcome sink of (OutcomeSnoozed).
func (s *Service) Snooze(ctx context.Context, id int64, days int) (Snapshot, error) {
	if !snooze.ValidDays(days) {
		return Snapshot{}, fmt.Errorf("%w: %d days; choose %d to %d", snooze.ErrInvalidDays, days, snooze.MinDays,
			snooze.MaxDays)
	}

	var (
		told   []Outcome
		stored int64
	)

	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		var snoozeErr error

		// The times are taken once the transaction holds the write lock, so a reversal's
		// time comes after every routine's step committed before it.
		now := s.now()
		until := s.clock.Now().AddDate(0, 0, days).UTC().Truncate(time.Second)

		stored, told, snoozeErr = s.snoozeIn(ctx, s.q.WithTx(tx), id, until, now)

		return snoozeErr
	})

	switch {
	case err != nil:
		return Snapshot{}, fmt.Errorf("snooze the item: %w", err)
	case stored == 0:
		return Snapshot{}, fmt.Errorf("%w (id %d)", item.ErrNotFound, id)
	}

	// The sink hears first, before the window and the engine are told the inbox changed, so
	// what takes back a routine's step is recorded before anything wakes the engine.
	s.settle(ctx, told)

	return s.changed(ctx)
}

// snoozeIn is Snooze's transaction, through q: it snoozes the item with id until until, at
// now, with its activity as the baseline, and returns how many rows it changed and what it
// tells the outcome sink: a snooze over a routine's dismissal or snooze.
func (s *Service) snoozeIn(ctx context.Context, q *queries.Queries, id int64, until, now time.Time,
) (int64, []Outcome, error) {
	baseline, err := activityBaseline(ctx, q, id)
	if err != nil {
		return 0, nil, err
	}

	was, found, err := s.triageBefore(ctx, q, id)
	if err != nil {
		return 0, nil, err
	}

	stored, err := q.SetTriage(ctx, queries.SetTriageParams{
		SnoozedUntil: &until, SnoozeBaseline: baseline, UpdatedAt: now, ID: id,
	})
	if err != nil {
		return 0, nil, err
	}

	if rev := setAsideReversal(was, id, now); found && rev.Routine != 0 {
		return stored, reversing(triageOutcome(OutcomeSnoozed, was, now), rev), nil
	}

	return stored, nil, nil
}

// Restore brings a dismissed or snoozed item back to its lists, and returns the new
// snapshot. A pinned item stays pinned. Restoring an item that is neither does nothing.
// Once an item that was dismissed or snoozed is restored, it tells the outcome sink of it,
// and that it took back a routine's dismissal or snooze of it, when it did.
func (s *Service) Restore(ctx context.Context, id int64) (Snapshot, error) {
	var told []Outcome

	err := s.storeCleared(ctx, "restore the item", id, func(ctx context.Context, q *queries.Queries) error {
		now := s.now() // once the transaction holds the write lock, as Snooze's

		was, found, err := s.triageBefore(ctx, q, id)
		if err != nil {
			return err
		}

		if found && setAside(was, now) {
			told = reversing(triageOutcome(OutcomeRestored, was, now), setAsideReversal(was, id, now))
		}

		return q.Restore(ctx, queries.RestoreParams{UpdatedAt: now, ItemID: id})
	})
	if err != nil {
		return Snapshot{}, err
	}

	// The sink hears first, before the window and the engine are told the inbox changed, so
	// what takes back a routine's step is recorded before anything wakes the engine.
	s.settle(ctx, told)

	return s.changed(ctx)
}

// Pin keeps an item at the top of every list it is in, above the items not pinned, the
// most recently pinned first, and returns the new snapshot. A snoozed or dismissed item
// stays set aside and comes back pinned. Pinning a pinned item keeps its place, and makes
// a routine's pin the user's. Nothing is written to GitHub.
func (s *Service) Pin(ctx context.Context, id int64) (Snapshot, error) {
	now := s.now()

	return s.setState(ctx, "pin", id, func(ctx context.Context) (int64, error) {
		return s.q.Pin(ctx, queries.PinParams{PinnedAt: &now, UpdatedAt: now, ID: id})
	})
}

// Unpin puts a pinned item back in its place among the rest, by its last activity, and
// returns the new snapshot. It leaves a snooze or a dismissal as it is. Unpinning an item
// that is not pinned does nothing. Unpinning what a routine pinned takes it back, which it
// tells the outcome sink of (OutcomeUnpinned).
func (s *Service) Unpin(ctx context.Context, id int64) (Snapshot, error) {
	var told []Outcome

	err := s.storeCleared(ctx, "unpin the item", id, func(ctx context.Context, q *queries.Queries) error {
		now := s.now() // once the transaction holds the write lock, as Snooze's

		was, found, err := s.triageBefore(ctx, q, id)
		if err != nil {
			return err
		}

		if found && was.PinnedAt != nil && was.PinnedBy != nil {
			told = reversing(triageOutcome(OutcomeUnpinned, was, now), Reversal{Routine: *was.PinnedBy, Item: id, Did: TriagePin})
		}

		return q.Unpin(ctx, queries.UnpinParams{UpdatedAt: now, ItemID: id})
	})
	if err != nil {
		return Snapshot{}, err
	}

	// The sink hears first, before the window and the engine are told the inbox changed, so
	// what takes back a routine's step is recorded before anything wakes the engine.
	s.settle(ctx, told)

	return s.changed(ctx)
}

// storeCleared runs unset, which clears some of the local state of the item with id, then
// deletes the row once nothing is left in it, both in one transaction. what names the
// action in an error.
func (s *Service) storeCleared(
	ctx context.Context, what string, id int64, unset func(context.Context, *queries.Queries) error,
) error {
	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)
		if err := unset(ctx, q); err != nil {
			return err
		}

		return q.PruneState(ctx, id)
	})
	if err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}

	return nil
}

// setState runs set, which records some of the local state of the item with id and
// returns how many rows it changed: none for an unknown item. Then it says the inbox
// changed and returns the new snapshot. verb names the action in an error.
func (s *Service) setState(
	ctx context.Context, verb string, id int64, set func(context.Context) (int64, error),
) (Snapshot, error) {
	n, err := set(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("%s the item: %w", verb, err)
	}

	if n == 0 {
		return Snapshot{}, fmt.Errorf("%w (id %d)", item.ErrNotFound, id)
	}

	return s.changed(ctx)
}

// activityBaseline returns an item's activity as the last refresh read it, as the JSON a
// snooze records, or nil when no refresh has read its details yet; the next refresh
// records it then.
func activityBaseline(ctx context.Context, q *queries.Queries, id int64) (*string, error) {
	row, err := q.GetActivity(ctx, id)

	switch {
	case errors.Is(err, sql.ErrNoRows):
		return nil, nil //nolint:nilnil // no details yet is not an error, and there is no baseline
	case err != nil:
		return nil, fmt.Errorf("read the item's activity: %w", err)
	}

	return encodeJSON(snooze.State{
		Head: row.HeadOID, Commits: int(row.Commits), Comments: int(row.Comments), Decision: row.ReviewDecision,
		Reviewed: int(row.Reviewed), Approvals: int(row.Approvals), ChangesRequested: int(row.ChangesRequested),
		Draft: row.IsDraft,
	})
}

// snoozedActivity is the activity a running snooze is compared with: its item's details
// as the refresh just stored them.
func snoozedActivity(r queries.ListRunningSnoozesRow) snooze.State {
	return snooze.State{
		Head: r.HeadOID, Commits: int(r.Commits), Comments: int(r.Comments), Decision: r.ReviewDecision,
		Reviewed: int(r.Reviewed), Approvals: int(r.Approvals), ChangesRequested: int(r.ChangesRequested),
		Draft: r.IsDraft,
	}
}

// wakeSnoozed ends early every running snooze whose item's new activity passes the
// user's wake rules: the snooze ends now, with the reasons recorded for the inbox to
// show. A snooze recorded before its item's details were known takes them as its
// baseline instead.
func (w *writer) wakeSnoozed(ctx context.Context) error {
	rules, err := wakeRules(ctx, w.prefs)
	if err != nil {
		return err
	}

	// w.now is in UTC, as every stored time is, so it compares with snoozed_until as text
	// in the same format.
	rows, err := w.q.ListRunningSnoozes(ctx, queries.ListRunningSnoozesParams{
		AccountID: w.accountID, SnoozedUntil: new(w.now),
	})
	if err != nil {
		return fmt.Errorf("read the snoozed items: %w", err)
	}

	for _, r := range rows {
		if err = w.wake(ctx, rules, r); err != nil {
			return err
		}
	}

	return nil
}

// wake checks one running snooze and records what changed.
func (w *writer) wake(ctx context.Context, rules snooze.Rules, r queries.ListRunningSnoozesRow) error {
	now := snoozedActivity(r)

	then, err := store.DecodeJSON[snooze.State](store.Deref(r.SnoozeBaseline))
	if r.SnoozeBaseline == nil || err != nil {
		return w.recordBaseline(ctx, r.ItemID, now)
	}

	if now.Draft && !recordsDraft(*r.SnoozeBaseline) {
		// A snooze an earlier build recorded holds no draft flag, so it reads as ready:
		// a draft snoozed then would never wake when marked ready. While the item is
		// still a draft, record that much and compare as usual.
		then.Draft = true
		if err = w.recordBaseline(ctx, r.ItemID, then); err != nil {
			return err
		}
	}

	bot := trust.Author{Type: r.AuthorType}.IsBot()

	reasons := snooze.Wake(rules, then, now, bot)
	if !reasons.Woke() {
		return nil
	}

	encoded, err := encodeJSON(reasons)
	if err != nil {
		return err
	}

	err = w.q.WakeSnooze(ctx, queries.WakeSnoozeParams{
		SnoozedUntil: new(w.now), WakeReasons: encoded, UpdatedAt: w.now, ItemID: r.ItemID,
	})
	if err != nil {
		return fmt.Errorf("wake the snoozed item: %w", err)
	}

	if w.woke == nil {
		w.woke = make(map[int64]snooze.Reasons)
	}

	w.woke[r.ItemID] = reasons

	return nil
}

// recordsDraft reports whether a recorded baseline says whether its item was a draft,
// which a baseline an earlier build recorded doesn't.
func recordsDraft(baseline string) bool {
	keys, err := store.DecodeJSON[struct {
		Draft *bool `json:"draft"`
	}](baseline)

	return err == nil && keys.Draft != nil
}

// recordBaseline records now as the activity the item's snooze compares with, when none
// is recorded or the one recorded can't be read.
func (w *writer) recordBaseline(ctx context.Context, id int64, now snooze.State) error {
	baseline, err := encodeJSON(now)
	if err != nil {
		return err
	}

	err = w.q.SetSnoozeBaseline(ctx, queries.SetSnoozeBaselineParams{SnoozeBaseline: baseline, ItemID: id})
	if err != nil {
		return fmt.Errorf("record the snooze baseline: %w", err)
	}

	return nil
}

// wakeRules reads the user's wake rules from the prefs.
func wakeRules(ctx context.Context, p *prefs.Store) (snooze.Rules, error) {
	stored, err := p.GetMany(ctx, snooze.Keys()...)
	if err != nil {
		return snooze.Rules{}, fmt.Errorf("read the snooze rules: %w", err)
	}

	return snooze.DecodeRules(stored), nil
}

// encodeJSON encodes v for a nullable JSON column.
func encodeJSON(v any) (*string, error) {
	text, err := store.EncodeJSON(v)
	if err != nil {
		return nil, err
	}

	return &text, nil
}
