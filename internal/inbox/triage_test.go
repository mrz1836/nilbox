package inbox

import (
	"testing"
	"time"
	_ "time/tzdata" // the daylight-saving test needs a real zone on any machine

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/snooze"
)

// triageHarness refreshes an inbox where #1 is in two lists and #2 in one, and returns
// the harness with #1's and #2's IDs.
func triageHarness(t *testing.T) (h *harness, first, second int64) {
	t.Helper()

	h = newHarness(t)
	a, b := pr(1, "OWNER", false), pr(2, "OWNER", false)
	snap := h.seed(t, []github.Item{a, b}, nil, []github.Item{a})

	return h, itemByNumber(t, snap.ReviewRequested, 1).ID, itemByNumber(t, snap.ReviewRequested, 2).ID
}

func TestDismissUntilRestored(t *testing.T) {
	t.Parallel()

	h, id, other := triageHarness(t)
	notified := h.events.Count()

	snap, err := h.svc.Dismiss(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, notified+1, h.events.Count())

	// The item stays in both its lists, marked; the UI sets it apart.
	for _, l := range []List{snap.ReviewRequested, snap.Assigned} {
		it := itemByNumber(t, l, 1)
		assert.Equal(t, "2026-01-10T12:00:00Z", it.DismissedAt)
		assert.Empty(t, it.SnoozedUntil)
	}

	assert.Empty(t, itemByNumber(t, snap.ReviewRequested, 2).DismissedAt, "other items are untouched")

	// A refresh leaves it dismissed, however much later.
	h.clock.Advance(30 * 24 * time.Hour)
	snap = h.refresh(t)
	assert.Equal(t, "2026-01-10T12:00:00Z", itemByNumber(t, snap.ReviewRequested, 1).DismissedAt)

	snap, err = h.svc.Restore(t.Context(), id)
	require.NoError(t, err)
	assert.Empty(t, itemByNumber(t, snap.ReviewRequested, 1).DismissedAt)
	assert.Zero(t, h.count(t, "item_local_state"))

	// Restoring an item that is neither dismissed nor snoozed does nothing.
	_, err = h.svc.Restore(t.Context(), other)
	require.NoError(t, err)
}

func TestSnoozeForDays(t *testing.T) {
	t.Parallel()

	h, id, _ := triageHarness(t)
	h.clock.Set(time.Date(2026, 1, 10, 12, 0, 0, 500, time.UTC))

	snap, err := h.svc.Snooze(t.Context(), id, 3)
	require.NoError(t, err)

	it := itemByNumber(t, snap.Assigned, 1)
	assert.Equal(t, "2026-01-13T12:00:00Z", it.SnoozedUntil)
	assert.Empty(t, it.DismissedAt)

	// A dismissal replaces the snooze, and a snooze replaces a dismissal.
	snap, err = h.svc.Dismiss(t.Context(), id)
	require.NoError(t, err)
	it = itemByNumber(t, snap.Assigned, 1)
	assert.Empty(t, it.SnoozedUntil)
	assert.NotEmpty(t, it.DismissedAt)

	snap, err = h.svc.Snooze(t.Context(), id, 1)
	require.NoError(t, err)
	it = itemByNumber(t, snap.Assigned, 1)
	assert.Equal(t, "2026-01-11T12:00:00Z", it.SnoozedUntil)
	assert.Empty(t, it.DismissedAt)

	// Once the snooze has passed, the item still says it was snoozed, so the UI can mark
	// it as back.
	h.clock.Advance(48 * time.Hour)
	snap = h.refresh(t)
	assert.Equal(t, "2026-01-11T12:00:00Z", itemByNumber(t, snap.Assigned, 1).SnoozedUntil)
	assert.Equal(t, int64(1), h.count(t, "item_local_state"))
}

func TestSnoozeKeepsTheWallClockTime(t *testing.T) {
	t.Parallel()

	h, id, _ := triageHarness(t)

	newYork, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	// Noon on the Saturday before the clocks go forward: noon on Sunday is 23 hours on.
	h.clock.Set(time.Date(2026, 3, 7, 12, 0, 0, 0, newYork))

	snap, err := h.svc.Snooze(t.Context(), id, 1)
	require.NoError(t, err)

	until, err := time.Parse(time.RFC3339, itemByNumber(t, snap.ReviewRequested, 1).SnoozedUntil)
	require.NoError(t, err)
	assert.Equal(t, time.Date(2026, 3, 8, 12, 0, 0, 0, newYork).UTC(), until)
	assert.Equal(t, 23*time.Hour, until.Sub(h.clock.Now()))
}

func TestSnoozeRejectsInvalidLengths(t *testing.T) {
	t.Parallel()

	h, id, _ := triageHarness(t)
	notified := h.events.Count()

	for _, days := range []int{0, -1, snooze.MaxDays + 1} {
		_, err := h.svc.Snooze(t.Context(), id, days)
		require.ErrorIs(t, err, snooze.ErrInvalidDays, days)
	}

	_, err := h.svc.Snooze(t.Context(), id, 0)
	require.ErrorContains(t, err, "0 days; choose 1 to 365")

	_, err = h.svc.Snooze(t.Context(), id, snooze.MaxDays)
	require.NoError(t, err, "a year is the longest snooze")

	assert.Equal(t, notified+1, h.events.Count(), "only the valid snooze notifies")
}

func TestPinKeepsAnItemOnTop(t *testing.T) {
	t.Parallel()

	h, first, second := triageHarness(t)
	notified := h.events.Count()

	// #2 moved last, so it leads until #1 is pinned.
	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []int{2, 1}, numbers(snap.ReviewRequested))

	snap, err = h.svc.Pin(t.Context(), first)
	require.NoError(t, err)
	assert.Equal(t, notified+1, h.events.Count())
	assert.Equal(t, []int{1, 2}, numbers(snap.ReviewRequested))
	assert.Equal(t, "2026-01-10T12:00:00Z", itemByNumber(t, snap.Assigned, 1).PinnedAt, "pinned in every list")
	assert.Empty(t, itemByNumber(t, snap.ReviewRequested, 2).PinnedAt)

	// The most recently pinned comes first, even within the same second.
	h.clock.Advance(500 * time.Millisecond)
	snap, err = h.svc.Pin(t.Context(), second)
	require.NoError(t, err)
	assert.Equal(t, []int{2, 1}, numbers(snap.ReviewRequested))

	// Pinning a pinned item does not move it.
	h.clock.Advance(time.Minute)
	snap, err = h.svc.Pin(t.Context(), first)
	require.NoError(t, err)
	assert.Equal(t, []int{2, 1}, numbers(snap.ReviewRequested))
	assert.Equal(t, "2026-01-10T12:00:00Z", itemByNumber(t, snap.ReviewRequested, 1).PinnedAt)

	// A refresh keeps the pins.
	snap = h.refresh(t)
	assert.Equal(t, []int{2, 1}, numbers(snap.ReviewRequested))

	// Unpinning puts an item back among the rest by its last activity.
	snap, err = h.svc.Unpin(t.Context(), second)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, numbers(snap.ReviewRequested))
	assert.Empty(t, itemByNumber(t, snap.ReviewRequested, 2).PinnedAt)

	snap, err = h.svc.Unpin(t.Context(), first)
	require.NoError(t, err)
	assert.Equal(t, []int{2, 1}, numbers(snap.ReviewRequested))
	assert.Zero(t, h.count(t, "item_local_state"))

	// Unpinning an item that is not pinned does nothing.
	_, err = h.svc.Unpin(t.Context(), first)
	require.NoError(t, err)
}

func TestAPinOutlastsSnoozesAndDismissals(t *testing.T) {
	t.Parallel()

	h, id, _ := triageHarness(t)

	_, err := h.svc.Pin(t.Context(), id)
	require.NoError(t, err)

	// Snoozing a pinned item keeps the pin, and so does ending the snooze.
	snap, err := h.svc.Snooze(t.Context(), id, 3)
	require.NoError(t, err)
	it := itemByNumber(t, snap.ReviewRequested, 1)
	assert.NotEmpty(t, it.SnoozedUntil)
	assert.Equal(t, "2026-01-10T12:00:00Z", it.PinnedAt)

	snap, err = h.svc.Restore(t.Context(), id)
	require.NoError(t, err)
	it = itemByNumber(t, snap.ReviewRequested, 1)
	assert.Empty(t, it.SnoozedUntil)
	assert.Equal(t, "2026-01-10T12:00:00Z", it.PinnedAt)
	assert.Equal(t, []int{1, 2}, numbers(snap.ReviewRequested))

	// The same for a dismissal.
	snap, err = h.svc.Dismiss(t.Context(), id)
	require.NoError(t, err)
	it = itemByNumber(t, snap.ReviewRequested, 1)
	assert.NotEmpty(t, it.DismissedAt)
	assert.NotEmpty(t, it.PinnedAt)

	// Unpinning a dismissed item leaves it dismissed.
	snap, err = h.svc.Unpin(t.Context(), id)
	require.NoError(t, err)
	it = itemByNumber(t, snap.ReviewRequested, 1)
	assert.NotEmpty(t, it.DismissedAt)
	assert.Empty(t, it.PinnedAt)
	assert.Equal(t, int64(1), h.count(t, "item_local_state"))

	_, err = h.svc.Restore(t.Context(), id)
	require.NoError(t, err)
	assert.Zero(t, h.count(t, "item_local_state"))
}

func TestTriageUnknownItem(t *testing.T) {
	t.Parallel()

	h, _, _ := triageHarness(t)
	notified := h.events.Count()

	_, err := h.svc.Dismiss(t.Context(), 999)
	require.ErrorIs(t, err, item.ErrNotFound)
	require.ErrorContains(t, err, "(id 999)")

	_, err = h.svc.Snooze(t.Context(), 999, 3)
	require.ErrorIs(t, err, item.ErrNotFound)

	_, err = h.svc.Pin(t.Context(), 999)
	require.ErrorIs(t, err, item.ErrNotFound)

	assert.Zero(t, h.count(t, "item_local_state"))
	assert.Equal(t, notified, h.events.Count())
}

func TestTriageDatabaseErrors(t *testing.T) {
	t.Parallel()

	h, id, _ := triageHarness(t)
	require.NoError(t, h.pool.Close())

	_, err := h.svc.Dismiss(t.Context(), id)
	require.ErrorContains(t, err, "dismiss the item")

	_, err = h.svc.Snooze(t.Context(), id, 3)
	require.ErrorContains(t, err, "snooze the item")

	_, err = h.svc.Restore(t.Context(), id)
	require.ErrorContains(t, err, "restore the item")

	_, err = h.svc.Pin(t.Context(), id)
	require.ErrorContains(t, err, "pin the item")

	_, err = h.svc.Unpin(t.Context(), id)
	require.ErrorContains(t, err, "unpin the item")
}

// wakeHarness refreshes an inbox with one pull request (#1) whose details are known, and
// returns the harness, the item's ID, and its details as the refresh read them.
func wakeHarness(t *testing.T) (h *harness, id int64, details github.ItemDetails) {
	t.Helper()

	h = newHarness(t)
	details = prDetails()
	details.HeadOID = "1111111111111111111111111111111111111111"
	details.Reviews = github.Reviews{Decision: "REVIEW_REQUIRED", Reviewers: 2}
	h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil)
	h.client.details = map[string]github.ItemDetails{nodeID(1): details}

	snap := h.refresh(t)

	return h, itemByNumber(t, snap.ReviewRequested, 1).ID, details
}

// later moves the clock on and refreshes with the item's new details.
func (h *harness) later(t *testing.T, details github.ItemDetails) Item {
	t.Helper()

	h.clock.Advance(time.Hour)
	h.client.details = map[string]github.ItemDetails{nodeID(1): details}

	return itemByNumber(t, h.refresh(t).ReviewRequested, 1)
}

// baseline reads the activity an item's snooze compares with, or nil when none is
// recorded.
func (h *harness) baseline(t *testing.T, id int64) *string {
	t.Helper()

	var baseline *string
	require.NoError(t, h.pool.QueryRowContext(t.Context(),
		"SELECT snooze_baseline FROM item_local_state WHERE item_id = ?", id).Scan(&baseline))

	return baseline
}

// setPrefs writes raw prefs, as the settings service would.
func (h *harness) setPrefs(t *testing.T, values map[string]string) {
	t.Helper()

	require.NoError(t, prefs.New(h.pool, h.clock).SetMany(t.Context(), values))
}

func TestSnoozeRecordsTheItemsActivity(t *testing.T) {
	t.Parallel()

	h, id, details := wakeHarness(t)

	_, err := h.svc.Snooze(t.Context(), id, 3)
	require.NoError(t, err)

	baseline := h.baseline(t, id)
	require.NotNil(t, baseline)
	assert.JSONEq(t, `{"head":"1111111111111111111111111111111111111111","commits":3,"comments":4,
		"decision":"REVIEW_REQUIRED","reviewed":0,"approvals":0,"changesRequested":0}`, *baseline)
	assert.Equal(t, 3, details.Commits)
}

func TestNewCommitsWakeASnooze(t *testing.T) {
	t.Parallel()

	h, id, details := wakeHarness(t)

	_, err := h.svc.Snooze(t.Context(), id, 3)
	require.NoError(t, err)

	// Nothing new: still snoozed.
	it := h.later(t, details)
	assert.Equal(t, "2026-01-13T12:00:00Z", it.SnoozedUntil)
	assert.Nil(t, it.WokeBy)

	details.Commits = 5
	details.HeadOID = "2222222222222222222222222222222222222222"
	it = h.later(t, details)

	assert.Equal(t, "2026-01-10T14:00:00Z", it.SnoozedUntil, "the snooze ends at the refresh that saw the commits")
	require.NotNil(t, it.WokeBy)
	assert.Equal(t, snooze.Reasons{Commits: 2}, *it.WokeBy)

	// The snooze has ended, so later activity changes nothing.
	details.Commits = 9
	it = h.later(t, details)
	assert.Equal(t, "2026-01-10T14:00:00Z", it.SnoozedUntil)
	assert.Equal(t, snooze.Reasons{Commits: 2}, *it.WokeBy)

	// Snoozing again starts over, without the old reasons.
	snap, err := h.svc.Snooze(t.Context(), id, 1)
	require.NoError(t, err)
	assert.Nil(t, itemByNumber(t, snap.ReviewRequested, 1).WokeBy)
}

func TestTheWakeRulesComeFromThePrefs(t *testing.T) {
	t.Parallel()

	h, id, details := wakeHarness(t)
	h.setPrefs(t, map[string]string{
		snooze.PrefWakeCommits:  "0",
		snooze.PrefWakeReviews:  string(snooze.ReviewsNever),
		snooze.PrefWakeComments: "3",
	})

	_, err := h.svc.Snooze(t.Context(), id, 3)
	require.NoError(t, err)

	// Commits and an approval are ignored; two comments are under the threshold.
	details.Commits = 6
	details.Comments = 6
	details.Reviews.Approvals = 1
	details.Reviews.Decision = "APPROVED"
	it := h.later(t, details)
	assert.Nil(t, it.WokeBy)
	assert.Equal(t, "2026-01-13T12:00:00Z", it.SnoozedUntil)

	details.Comments = 7
	it = h.later(t, details)
	require.NotNil(t, it.WokeBy)
	assert.Equal(t, snooze.Reasons{Comments: 3}, *it.WokeBy)
}

func TestAReviewWakesASnooze(t *testing.T) {
	t.Parallel()

	h, id, details := wakeHarness(t)

	_, err := h.svc.Snooze(t.Context(), id, 3)
	require.NoError(t, err)

	details.Reviews.ChangesRequested = 1
	details.Reviews.Reviewed = 1
	it := h.later(t, details)

	require.NotNil(t, it.WokeBy)
	assert.Equal(t, snooze.Reasons{Review: snooze.ReviewChangesRequested}, *it.WokeBy)
}

func TestABotsForcePushDoesNotWakeASnooze(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	details := prDetails()
	details.HeadOID = "1111111111111111111111111111111111111111"

	bump := pr(1, "NONE", false)
	bump.AuthorLogin = "dependabot"
	bump.AuthorType = "Bot"
	h.client.inbox = fetched([]github.Item{bump}, nil, nil)
	h.client.details = map[string]github.ItemDetails{nodeID(1): details}
	id := itemByNumber(t, h.refresh(t).ReviewRequested, 1).ID

	_, err := h.svc.Snooze(t.Context(), id, 3)
	require.NoError(t, err)

	details.HeadOID = "2222222222222222222222222222222222222222"
	it := h.later(t, details)
	assert.Nil(t, it.WokeBy, "a rebase")

	details.Commits++
	it = h.later(t, details)
	require.NotNil(t, it.WokeBy, "a new commit still counts")
	assert.Equal(t, snooze.Reasons{Commits: 1}, *it.WokeBy)
}

func TestASnoozeBeforeTheDetailsTakesItsBaselineLater(t *testing.T) {
	t.Parallel()

	h, id, _ := triageHarness(t)

	_, err := h.svc.Snooze(t.Context(), id, 3)
	require.NoError(t, err)

	assert.Nil(t, h.baseline(t, id), "no details are known yet")

	details := prDetails()
	h.client.details = map[string]github.ItemDetails{nodeID(1): details}
	snap := h.refresh(t)
	assert.Nil(t, itemByNumber(t, snap.ReviewRequested, 1).WokeBy, "the first details are the baseline, not news")

	require.NotNil(t, h.baseline(t, id))

	details.Commits += 2
	h.client.details = map[string]github.ItemDetails{nodeID(1): details}
	snap = h.refresh(t)
	require.NotNil(t, itemByNumber(t, snap.ReviewRequested, 1).WokeBy)
}

func TestAnUnreadableSnoozeBaselineIsTakenAgain(t *testing.T) {
	t.Parallel()

	h, id, details := wakeHarness(t)

	_, err := h.svc.Snooze(t.Context(), id, 3)
	require.NoError(t, err)

	_, err = h.pool.ExecContext(t.Context(),
		`UPDATE item_local_state SET snooze_baseline = '{"commits":"many"}' WHERE item_id = ?`, id)
	require.NoError(t, err, "an object, as the schema holds it to, that this build can't read")

	details.Commits += 2
	it := h.later(t, details)
	assert.Nil(t, it.WokeBy, "the activity is the new baseline, not news")

	baseline := h.baseline(t, id)
	require.NotNil(t, baseline)
	assert.Contains(t, *baseline, `"commits":5`)

	details.Commits++
	it = h.later(t, details)
	require.NotNil(t, it.WokeBy, "a commit after the new baseline wakes it")
	assert.Equal(t, snooze.Reasons{Commits: 1}, *it.WokeBy)
}

func TestDismissedItemsNeverWake(t *testing.T) {
	t.Parallel()

	h, id, details := wakeHarness(t)

	_, err := h.svc.Dismiss(t.Context(), id)
	require.NoError(t, err)

	details.Commits += 4
	it := h.later(t, details)
	assert.NotEmpty(t, it.DismissedAt)
	assert.Nil(t, it.WokeBy)
}

func TestUnreadableWakeReasonsShowNone(t *testing.T) {
	t.Parallel()

	assert.Nil(t, decodeReasons(nil))

	assert.Nil(t, decodeReasons(new("{}")))
	assert.Nil(t, decodeReasons(new("[1]")))
	assert.Equal(t, &snooze.Reasons{Commits: 2}, decodeReasons(new(`{"commits":2}`)))

	// A review outcome a different build stored reads as none.
	assert.Equal(t, &snooze.Reasons{Commits: 2, Review: snooze.ReviewNone},
		decodeReasons(new(`{"commits":2,"review":"praised"}`)))

	assert.Nil(t, decodeReasons(new(`{"review":"praised"}`)), "nothing this build knows woke it")
}

func TestWakeRulesDatabaseError(t *testing.T) {
	t.Parallel()

	h, _, _ := triageHarness(t)
	require.NoError(t, h.pool.Close())

	_, err := wakeRules(t.Context(), h.svc.prefs)
	require.ErrorContains(t, err, "read the snooze rules")

	_, err = activityBaseline(t.Context(), h.svc.q, 1)
	require.ErrorContains(t, err, "read the item's activity")
}
