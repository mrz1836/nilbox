package inbox

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/mrz1836/nilbox/internal/snooze"
	"github.com/mrz1836/nilbox/internal/trust"
)

// ErrItemNotFound is returned when a triaged item is not in the local inbox.
var ErrItemNotFound = errors.New("item not found; refresh the inbox and try again")

// setTriageSQL records an item's triage state, replacing any earlier one, and clears the
// reasons an earlier snooze woke for. It inserts nothing for an unknown item, which the
// caller reads as zero rows affected.
const setTriageSQL = `INSERT INTO item_local_state (item_id, dismissed_at, snoozed_until, snooze_baseline, wake_reasons, updated_at)
SELECT id, ?, ?, ?, NULL, ? FROM items WHERE id = ?
ON CONFLICT (item_id) DO UPDATE SET
    dismissed_at = excluded.dismissed_at,
    snoozed_until = excluded.snoozed_until,
    snooze_baseline = excluded.snooze_baseline,
    wake_reasons = NULL,
    updated_at = excluded.updated_at`

// pinSQL pins an item to the top of its lists, keeping its triage state and the time of an
// earlier pin, so pinning a pinned item does not move it. It inserts nothing for an
// unknown item, which the caller reads as zero rows affected.
const pinSQL = `INSERT INTO item_local_state (item_id, pinned_at, updated_at)
SELECT id, ?, ? FROM items WHERE id = ?
ON CONFLICT (item_id) DO UPDATE SET
    pinned_at = COALESCE(item_local_state.pinned_at, excluded.pinned_at),
    updated_at = excluded.updated_at`

// restoreSQL ends an item's dismissal or snooze, and forgets what woke the snooze, leaving
// a pin as it is.
const restoreSQL = `UPDATE item_local_state
SET dismissed_at = NULL, snoozed_until = NULL, snooze_baseline = NULL, wake_reasons = NULL, updated_at = ?
WHERE item_id = ?`

// unpinSQL unpins an item, leaving its dismissal or snooze as it is.
const unpinSQL = `UPDATE item_local_state SET pinned_at = NULL, updated_at = ? WHERE item_id = ?`

// pruneStateSQL deletes an item's local state once nothing is left in it: not pinned,
// dismissed, or snoozed, nor back from a snooze.
const pruneStateSQL = `DELETE FROM item_local_state
WHERE item_id = ? AND pinned_at IS NULL AND dismissed_at IS NULL AND snoozed_until IS NULL`

// activityColumns are an item's details a snooze compares, as snooze.State reads them.
const activityColumns = `d.head_oid AS head, d.commits, d.comments, d.review_decision AS decision,
    d.reviewed, d.approvals, d.changes_requested`

// activityRow is one item's activity as activityColumns read it.
type activityRow struct {
	Head             string `gorm:"column:head"`
	Commits          int    `gorm:"column:commits"`
	Comments         int    `gorm:"column:comments"`
	Decision         string `gorm:"column:decision"`
	Reviewed         int    `gorm:"column:reviewed"`
	Approvals        int    `gorm:"column:approvals"`
	ChangesRequested int    `gorm:"column:changes_requested"`
}

// state converts the row to a snooze.State.
func (r activityRow) state() snooze.State {
	return snooze.State{
		Head: r.Head, Commits: r.Commits, Comments: r.Comments, Decision: r.Decision,
		Reviewed: r.Reviewed, Approvals: r.Approvals, ChangesRequested: r.ChangesRequested,
	}
}

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

	baseline, err := activityBaseline(s.db.WithContext(ctx), id)
	if err != nil {
		return Snapshot{}, fmt.Errorf("snooze the item: %w", err)
	}

	return s.setTriage(ctx, "snooze", id, nil, &until, baseline)
}

// Restore brings a dismissed or snoozed item back to its lists, and returns the new
// snapshot. A pinned item stays pinned. Restoring an item that is neither does nothing.
func (s *Service) Restore(ctx context.Context, id int64) (Snapshot, error) {
	return s.clearState(ctx, "restore the item", restoreSQL, id)
}

// Pin keeps an item at the top of every list it is in, above the items not pinned, the
// most recently pinned first, and returns the new snapshot. A snoozed or dismissed item
// stays set aside and comes back pinned. Pinning a pinned item does nothing. Nothing is
// written to GitHub.
func (s *Service) Pin(ctx context.Context, id int64) (Snapshot, error) {
	now := s.now()

	return s.setState(ctx, "pin", id, pinSQL, now, now, id)
}

// Unpin puts a pinned item back in its place among the rest, by its last activity, and
// returns the new snapshot. It leaves a snooze or a dismissal as it is. Unpinning an item
// that is not pinned does nothing.
func (s *Service) Unpin(ctx context.Context, id int64) (Snapshot, error) {
	return s.clearState(ctx, "unpin the item", unpinSQL, id)
}

// clearState runs clearSQL, which clears some of an item's local state, deletes the row
// once nothing is left in it, tells the notifier, and returns the new snapshot. what
// names the action in an error.
func (s *Service) clearState(ctx context.Context, what, clearSQL string, id int64) (Snapshot, error) {
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(clearSQL, s.now(), id).Error; err != nil {
			return err
		}

		return tx.Exec(pruneStateSQL, id).Error
	})
	if err != nil {
		return Snapshot{}, fmt.Errorf("%s: %w", what, err)
	}

	s.notify()

	return s.Snapshot(ctx)
}

// setTriage stores an item's triage state, tells the notifier, and returns the new
// snapshot. verb names the action in an error.
func (s *Service) setTriage(
	ctx context.Context, verb string, id int64, dismissedAt, snoozedUntil *time.Time, baseline *string,
) (Snapshot, error) {
	return s.setState(ctx, verb, id, setTriageSQL, dismissedAt, snoozedUntil, baseline, s.now(), id)
}

// setState runs query, which records some of the local state of the item with id and
// inserts nothing for an unknown item, then tells the notifier and returns the new
// snapshot. verb names the action in an error.
func (s *Service) setState(ctx context.Context, verb string, id int64, query string, args ...any) (Snapshot, error) {
	res := s.db.WithContext(ctx).Exec(query, args...)
	if res.Error != nil {
		return Snapshot{}, fmt.Errorf("%s the item: %w", verb, res.Error)
	}

	if res.RowsAffected == 0 {
		return Snapshot{}, fmt.Errorf("%w (id %d)", ErrItemNotFound, id)
	}

	s.notify()

	return s.Snapshot(ctx)
}

// activityBaseline returns an item's activity as the last refresh read it, as the JSON a
// snooze records, or nil when no refresh has read its details yet; the next refresh
// records it then.
func activityBaseline(db *gorm.DB, id int64) (*string, error) {
	var rows []activityRow

	err := db.Raw("SELECT "+activityColumns+" FROM item_details d WHERE d.item_id = ?", id).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("read the item's activity: %w", err)
	}

	if len(rows) == 0 {
		return nil, nil //nolint:nilnil // no details yet is not an error, and there is no baseline
	}

	return encodeJSON(rows[0].state())
}

// snoozedSQL reads the account's running snoozes on items whose details are known, with
// their baseline, the author's type, and the activity the refresh just stored.
const snoozedSQL = `SELECT ls.item_id, ls.snooze_baseline, i.author_type, ` + activityColumns + `
FROM item_local_state ls
JOIN items i ON i.id = ls.item_id
JOIN item_details d ON d.item_id = ls.item_id
WHERE i.account_id = ? AND ls.dismissed_at IS NULL AND ls.snoozed_until > ?`

// snoozedRow is one row of snoozedSQL.
type snoozedRow struct {
	Activity activityRow `gorm:"embedded"`

	ItemID     int64   `gorm:"column:item_id"`
	Baseline   *string `gorm:"column:snooze_baseline"`
	AuthorType string  `gorm:"column:author_type"`
}

// wakeSnoozed ends early every running snooze whose item's new activity passes the
// user's wake rules: the snooze ends now, with the reasons recorded for the inbox to
// show. A snooze recorded before its item's details were known takes them as its
// baseline instead.
func (w *writer) wakeSnoozed() error {
	rules, err := wakeRules(w.tx)
	if err != nil {
		return err
	}

	var rows []snoozedRow
	if err = w.tx.Raw(snoozedSQL, w.accountID, w.now).Scan(&rows).Error; err != nil {
		return fmt.Errorf("read the snoozed items: %w", err)
	}

	for _, r := range rows {
		if err = w.wake(rules, r); err != nil {
			return err
		}
	}

	return nil
}

// wake checks one running snooze and records what changed.
func (w *writer) wake(rules snooze.Rules, r snoozedRow) error {
	now := r.Activity.state()

	var then snooze.State
	if r.Baseline == nil || json.Unmarshal([]byte(*r.Baseline), &then) != nil {
		baseline, err := encodeJSON(now)
		if err != nil {
			return err
		}

		return w.exec("record the snooze baseline",
			"UPDATE item_local_state SET snooze_baseline = ? WHERE item_id = ?", baseline, r.ItemID)
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

	return w.exec("wake the snoozed item",
		"UPDATE item_local_state SET snoozed_until = ?, wake_reasons = ?, updated_at = ? WHERE item_id = ?",
		w.now, encoded, w.now, r.ItemID)
}

// exec runs one statement in the refresh's transaction; what names it in an error.
func (w *writer) exec(what, query string, args ...any) error {
	if err := w.tx.Exec(query, args...).Error; err != nil {
		return fmt.Errorf("%s: %w", what, err)
	}

	return nil
}

// wakeRules reads the user's wake rules from the prefs.
func wakeRules(db *gorm.DB) (snooze.Rules, error) {
	var rows []struct {
		Key   string `gorm:"column:key"`
		Value string `gorm:"column:value"`
	}

	keys := []string{snooze.PrefWakeCommits, snooze.PrefWakeReviews, snooze.PrefWakeComments}
	if err := db.Raw("SELECT key, value FROM prefs WHERE key IN ?", keys).Scan(&rows).Error; err != nil {
		return snooze.Rules{}, fmt.Errorf("read the snooze rules: %w", err)
	}

	prefs := make(map[string]string, len(rows))
	for _, r := range rows {
		prefs[r.Key] = r.Value
	}

	return snooze.DecodeRules(prefs), nil
}

// encodeJSON encodes v for a JSON column.
func encodeJSON(v any) (*string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("encode %T: %w", v, err)
	}

	out := string(b)

	return &out, nil
}
