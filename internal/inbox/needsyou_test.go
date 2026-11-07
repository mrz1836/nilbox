package inbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// needsYouNow is the moment the needs-you tests look at the inbox.
var needsYouNow = time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // a fixed test time

func TestItemTriageAtATime(t *testing.T) {
	tests := []struct {
		name      string
		item      Item
		dismissed bool
		snoozed   bool
	}{
		{name: "untouched", item: Item{}},
		{name: "dismissed", item: Item{DismissedAt: "2026-03-01T00:00:00Z"}, dismissed: true},
		{name: "snoozed", item: Item{SnoozedUntil: "2026-03-05T00:00:00Z"}, snoozed: true},
		{name: "back from a snooze", item: Item{SnoozedUntil: "2026-03-04T11:59:59Z"}},
		{name: "a snooze ending now has ended", item: Item{SnoozedUntil: "2026-03-04T12:00:00Z"}},
		{
			name:      "dismissed while snoozed",
			item:      Item{DismissedAt: "2026-03-01T00:00:00Z", SnoozedUntil: "2026-03-05T00:00:00Z"},
			dismissed: true,
		},
		{name: "an unreadable snooze", item: Item{SnoozedUntil: "soon"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.dismissed, tt.item.Dismissed())
			assert.Equal(t, tt.snoozed, tt.item.Snoozed(needsYouNow))
			assert.Equal(t, !tt.dismissed && !tt.snoozed, tt.item.NeedsYou(needsYouNow))
		})
	}
}

func TestNeedsYouCountsEachItemOnce(t *testing.T) {
	shared := Item{ID: 1}
	snap := Snapshot{
		ReviewRequested: List{Items: []Item{shared, {ID: 2}, {ID: 3, DismissedAt: "2026-03-01T00:00:00Z"}}},
		Authored:        List{Items: []Item{{ID: 4, SnoozedUntil: "2026-03-05T00:00:00Z"}, {ID: 5}}},
		Assigned:        List{Items: []Item{shared, {ID: 6, SnoozedUntil: "2026-03-01T00:00:00Z"}}},
	}

	assert.Equal(t, 4, snap.NeedsYou(needsYouNow), "1, 2, 5, and 6; 1 is in two lists")
	assert.Equal(t, 5, snap.NeedsYou(time.Date(2026, 3, 6, 0, 0, 0, 0, time.UTC)), "4's snooze has ended")
	assert.Zero(t, new(Snapshot).NeedsYou(needsYouNow))
}

func TestWaitingKeepsTheListsOrder(t *testing.T) {
	l := List{Items: []Item{{ID: 9}, {ID: 3, DismissedAt: "2026-03-01T00:00:00Z"}, {ID: 7}, {ID: 1}}}

	got := l.Waiting(needsYouNow)

	ids := make([]int64, 0, len(got))
	for _, it := range got {
		ids = append(ids, it.ID)
	}

	assert.Equal(t, []int64{9, 7, 1}, ids)
	assert.Empty(t, List{}.Waiting(needsYouNow))
}

func TestNextSnoozeEnd(t *testing.T) {
	snap := Snapshot{
		ReviewRequested: List{Items: []Item{{ID: 1, SnoozedUntil: "2026-03-07T00:00:00Z"}}},
		Authored: List{Items: []Item{
			{ID: 2, SnoozedUntil: "2026-03-05T09:30:00Z"},
			{ID: 3, SnoozedUntil: "2026-03-01T00:00:00Z"},
			{ID: 4, SnoozedUntil: "2026-03-04T13:00:00Z", DismissedAt: "2026-03-01T00:00:00Z"},
		}},
	}

	assert.Equal(t, time.Date(2026, 3, 5, 9, 30, 0, 0, time.UTC), snap.NextSnoozeEnd(needsYouNow))
	assert.True(t, new(Snapshot).NextSnoozeEnd(needsYouNow).IsZero())
}
