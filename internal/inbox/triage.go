package inbox

import (
	"context"
	"database/sql"
	"encoding/json"
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

// Dismiss hides an item from every list until it is restored, and returns the new
// snapshot. It replaces a snooze. Nothing is written to GitHub.
func (s *Service) Dismiss(ctx context.Context, id int64) (Snapshot, error) {
	now := s.now()

	return s.setTriage(ctx, "dismiss", id, &now, nil, nil)
}

// Snooze hides an item from every list for a number of days (snooze.MinDays to
// snooze.MaxDays), after which it is back on its own, and returns the new snapshot. It
// replaces a dismissal or an earlier snooze. The snooze ends at the same wall-clock time
// of day, so a change of the clocks in between does not move it.
//
// The item's activity as the last refresh read it is recorded with the snooze, so a
// later refresh can end it early when new activity passes the user's wake rules. Nothing
// is written to GitHub.
func (s *Service) Snooze(ctx context.Context, id int64, days int) (Snapshot, error) {
	if !snooze.ValidDays(days) {
		return Snapshot{}, fmt.Errorf("%w: %d days; choose %d to %d", snooze.ErrInvalidDays, days, snooze.MinDays,
			snooze.MaxDays)
	}

	until := s.clock.Now().AddDate(0, 0, days).UTC().Truncate(time.Second)

	baseline, err := activityBaseline(ctx, s.q, id)
	if err != nil {
		return Snapshot{}, fmt.Errorf("snooze the item: %w", err)
	}

	return s.setTriage(ctx, "snooze", id, nil, &until, baseline)
}

// Restore brings a dismissed or snoozed item back to its lists, and returns the new
// snapshot. A pinned item stays pinned. Restoring an item that is neither does nothing.
func (s *Service) Restore(ctx context.Context, id int64) (Snapshot, error) {
	return s.clearState(ctx, "restore the item", id, func(ctx context.Context, q *queries.Queries) error {
		return q.Restore(ctx, queries.RestoreParams{UpdatedAt: s.now(), ItemID: id})
	})
}

// Pin keeps an item at the top of every list it is in, above the items not pinned, the
// most recently pinned first, and returns the new snapshot. A snoozed or dismissed item
// stays set aside and comes back pinned. Pinning a pinned item does nothing. Nothing is
// written to GitHub.
func (s *Service) Pin(ctx context.Context, id int64) (Snapshot, error) {
	now := s.now()

	return s.setState(ctx, "pin", id, func(ctx context.Context) (int64, error) {
		return s.q.Pin(ctx, queries.PinParams{PinnedAt: &now, UpdatedAt: now, ID: id})
	})
}

// Unpin puts a pinned item back in its place among the rest, by its last activity, and
// returns the new snapshot. It leaves a snooze or a dismissal as it is. Unpinning an item
// that is not pinned does nothing.
func (s *Service) Unpin(ctx context.Context, id int64) (Snapshot, error) {
	return s.clearState(ctx, "unpin the item", id, func(ctx context.Context, q *queries.Queries) error {
		return q.Unpin(ctx, queries.UnpinParams{UpdatedAt: s.now(), ItemID: id})
	})
}

// clearState runs unset, which clears some of the local state of the item with id, and
// deletes the row once nothing is left in it, both in one transaction; then it says the
// inbox changed and returns the new snapshot. what names the action in an error.
func (s *Service) clearState(
	ctx context.Context, what string, id int64, unset func(context.Context, *queries.Queries) error,
) (Snapshot, error) {
	err := store.InTx(ctx, s.db, func(tx *sql.Tx) error {
		q := s.q.WithTx(tx)
		if err := unset(ctx, q); err != nil {
			return err
		}

		return q.PruneState(ctx, id)
	})
	if err != nil {
		return Snapshot{}, fmt.Errorf("%s: %w", what, err)
	}

	s.notify()

	return s.Snapshot(ctx)
}

// setTriage stores an item's triage state, says the inbox changed, and returns the new
// snapshot. verb names the action in an error.
func (s *Service) setTriage(
	ctx context.Context, verb string, id int64, dismissedAt, snoozedUntil *time.Time, baseline *string,
) (Snapshot, error) {
	return s.setState(ctx, verb, id, func(ctx context.Context) (int64, error) {
		return s.q.SetTriage(ctx, queries.SetTriageParams{
			DismissedAt: dismissedAt, SnoozedUntil: snoozedUntil, SnoozeBaseline: baseline, UpdatedAt: s.now(), ID: id,
		})
	})
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

	s.notify()

	return s.Snapshot(ctx)
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
	})
}

// snoozedActivity is the activity a running snooze is compared with: its item's details
// as the refresh just stored them.
func snoozedActivity(r queries.ListRunningSnoozesRow) snooze.State {
	return snooze.State{
		Head: r.HeadOID, Commits: int(r.Commits), Comments: int(r.Comments), Decision: r.ReviewDecision,
		Reviewed: int(r.Reviewed), Approvals: int(r.Approvals), ChangesRequested: int(r.ChangesRequested),
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

	// now is in UTC, as every stored time is, so it compares with snoozed_until as text
	// in the same format.
	now := w.now

	rows, err := w.q.ListRunningSnoozes(ctx, queries.ListRunningSnoozesParams{AccountID: w.accountID, SnoozedUntil: &now})
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

	var then snooze.State
	if r.SnoozeBaseline == nil || json.Unmarshal([]byte(*r.SnoozeBaseline), &then) != nil {
		baseline, err := encodeJSON(now)
		if err != nil {
			return err
		}

		err = w.q.SetSnoozeBaseline(ctx, queries.SetSnoozeBaselineParams{SnoozeBaseline: baseline, ItemID: r.ItemID})
		if err != nil {
			return fmt.Errorf("record the snooze baseline: %w", err)
		}

		return nil
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

	endedAt := w.now

	err = w.q.WakeSnooze(ctx, queries.WakeSnoozeParams{
		SnoozedUntil: &endedAt, WakeReasons: encoded, UpdatedAt: w.now, ItemID: r.ItemID,
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

// wakeRules reads the user's wake rules from the prefs.
func wakeRules(ctx context.Context, p *prefs.Store) (snooze.Rules, error) {
	stored, err := p.GetMany(ctx, snooze.PrefWakeCommits, snooze.PrefWakeReviews, snooze.PrefWakeComments)
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
