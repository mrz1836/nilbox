package inbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
)

func TestARefreshThatCantPruneOrForgetKeepsTheStoredInbox(t *testing.T) {
	t.Parallel()

	one, two := pr(1, "OWNER", false), pr(2, "OWNER", false)
	merged := two
	merged.State, merged.ClosedAt = github.StateMerged, time.Date(2026, 1, 10, 11, 0, 0, 0, time.UTC)

	tests := []struct {
		name string

		// setup stores what the failing refresh changes, after a refresh that listed #1 and
		// #2; next is what the failing refresh's inbox query answers.
		setup func(t *testing.T, h *harness)
		next  *github.Inbox

		// event and when say which write the database refuses.
		event, when string

		want string
	}{
		{
			name: "a merged result's state", next: fetched([]github.Item{one, merged}, nil, nil),
			event: "BEFORE UPDATE OF state ON items", when: "NEW.state = 'MERGED'",
			want: "save the state of acme-org/example-repo#2",
		},
		{
			name: "an old item",
			setup: func(t *testing.T, h *harness) {
				h.seed(t, []github.Item{one}, nil, nil)
				h.clock.Advance(pruneAfter + time.Hour)
			},
			next: fetched([]github.Item{one}, nil, nil), event: "BEFORE DELETE ON items", want: "prune the old items",
		},
		{
			name: "a routine's old restore mark",
			setup: func(t *testing.T, h *harness) {
				id := h.storedItemNumbered(t, 2).ID

				_, err := h.svc.Dismiss(t.Context(), id)
				require.NoError(t, err)
				h.triage(t, h.routine(t, "Bring back"), id, TriageRestore, 0)

				h.setPrefs(t, map[string]string{"autopilot.historyDays": "7"})
				h.clock.Advance(8 * 24 * time.Hour)
			},
			next:  fetched([]github.Item{one, two}, nil, nil),
			event: "BEFORE DELETE ON item_local_state", want: "prune the routines' old restore marks",
		},
		{
			name: "when an item that left a list arrived", next: fetched([]github.Item{one}, nil, nil),
			event: "BEFORE UPDATE OF review_requested_at ON items",
			when:  "NEW.review_requested_at IS NULL AND OLD.review_requested_at IS NOT NULL",
			want:  "forget when items left their lists arrived",
		},
		{
			name: "the file check of an item no longer listed",
			setup: func(t *testing.T, h *harness) {
				id := h.storedItemNumbered(t, 2).ID
				require.NoError(t, upsertFiles(t.Context(), h.svc.q, newFilesRow(id, fileCheck{files: changed(headA)}, h.clock.Now())))
			},
			next:  fetched([]github.Item{one}, nil, nil),
			event: "BEFORE DELETE ON item_files", want: "remove the files of items no longer listed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			h.client.details = map[string]github.ItemDetails{nodeID(1): prDetails(), nodeID(2): prDetails()}
			h.seed(t, []github.Item{one, two}, nil, nil)
			h.clock.Advance(DefaultInterval)

			if tt.setup != nil {
				tt.setup(t, h)
			}

			before, err := h.svc.Snapshot(t.Context())
			require.NoError(t, err)

			stored := h.storedItems(t)

			refuse(t, h, tt.event, tt.when)

			h.client.inbox = tt.next

			_, err = h.svc.Refresh(t.Context())
			require.ErrorContains(t, err, "save the inbox")
			require.ErrorContains(t, err, tt.want)

			after, err := h.svc.Snapshot(t.Context())
			require.NoError(t, err)
			assert.Contains(t, after.LastError, tt.want, "the failure is recorded for the window")
			assert.Equal(t, listed(before), listed(after), "nothing of the failed refresh is stored")
			assert.Equal(t, before.LastRefreshedAt, after.LastRefreshedAt)
			assert.Equal(t, stored, h.storedItems(t), "no item is pruned, and none changes")

			// Once the database takes the write again, the refresh stores.
			h.exec(t, "DROP TRIGGER refused_write")
			h.refresh(t)
		})
	}
}
