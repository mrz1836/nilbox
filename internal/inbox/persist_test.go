package inbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
)

// refuse makes the database reject one kind of write with a trigger, so a test can fail
// a refresh at exactly one step of storing it. when, if not empty, limits the refusal to
// the rows it matches.
func refuse(t *testing.T, h *harness, event, when string) {
	t.Helper()

	if when != "" {
		when = " WHEN " + when
	}

	h.exec(t, "CREATE TRIGGER refused_write "+event+when+" BEGIN SELECT RAISE(ABORT, 'refused by the test'); END")
}

// listed returns the numbers of a snapshot's three lists.
func listed(snap Snapshot) [3][]int {
	return [3][]int{numbers(snap.ReviewRequested), numbers(snap.Authored), numbers(snap.Assigned)}
}

func TestRefreshThatCannotBeSavedKeepsTheStoredInbox(t *testing.T) {
	t.Parallel()

	archived := issue("archived-repo", 9, "OWNER")
	archived.Repo.IsArchived = true

	tests := []struct {
		name string

		// event and when say which write the database refuses.
		event, when string

		// stored runs a successful refresh first, so the failing one changes a stored
		// inbox rather than an empty one.
		stored bool

		// oldRate adds a rate reading old enough for the refresh to prune.
		oldRate bool

		want string
	}{
		{name: "account", event: "BEFORE INSERT ON accounts", want: "save the account"},
		{
			name: "archived repository", event: "BEFORE INSERT ON repos", when: "NEW.is_archived = 1",
			want: "save the repository acme-org/archived-repo",
		},
		{
			name: "listed repository", event: "BEFORE INSERT ON repos", when: "NEW.is_archived = 0",
			want: "save the repository acme-org/example-repo",
		},
		{
			name: "reason flags", event: "BEFORE UPDATE OF reason_review_requested ON items", stored: true,
			want: "clear the reason flags",
		},
		{name: "item", event: "BEFORE INSERT ON items", want: "save the item acme-org/example-repo#1"},
		{name: "details", event: "BEFORE INSERT ON item_details", want: "save the details of acme-org/example-repo#1"},
		{name: "rate limits", event: "BEFORE INSERT ON rate_snapshots", want: "save the rate limits"},
		{
			name: "old rate limits", event: "BEFORE DELETE ON rate_snapshots", stored: true, oldRate: true,
			want: "prune old rate limits",
		},
		{
			name: "status", event: "BEFORE INSERT ON prefs", when: "NEW.key = '" + prefViewer + "'",
			want: "save setting " + prefViewer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			h.client.inbox = fetched([]github.Item{pr(1, "OWNER", false)}, nil, []github.Item{archived})
			h.client.details = map[string]github.ItemDetails{nodeID(1): prDetails()}

			if tt.stored {
				h.refresh(t)
				h.clock.Advance(DefaultInterval)
			}

			if tt.oldRate {
				h.insertRate(t, 1, h.clock.Now().Add(-8*24*time.Hour))
			}

			before, err := h.svc.Snapshot(t.Context())
			require.NoError(t, err)

			rates := h.count(t, "rate_snapshots")

			refuse(t, h, tt.event, tt.when)

			_, err = h.svc.Refresh(t.Context())
			require.ErrorContains(t, err, "save the inbox")
			require.ErrorContains(t, err, tt.want)

			after, err := h.svc.Snapshot(t.Context())
			require.NoError(t, err)
			assert.Contains(t, after.LastError, tt.want, "the failure is recorded for the window")
			assert.Equal(t, listed(before), listed(after), "nothing of the failed refresh is stored")
			assert.Equal(t, before.LastRefreshedAt, after.LastRefreshedAt)
			assert.Equal(t, before.Account.Login, after.Account.Login)
			assert.Equal(t, rates, h.count(t, "rate_snapshots"), "no rate reading is added or pruned")
		})
	}
}

func TestSnapshotReportsUnreadableTables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		table string
		want  string
	}{
		{table: "prefs", want: "read the inbox status"},
		{table: "accounts", want: "read the account"},
		{table: "items", want: "read the inbox items"},
		{table: "rate_snapshots", want: "read the " + resourceGraphQL + " rate limit"},
	}

	for _, tt := range tests {
		t.Run(tt.table, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)

			// Renaming keeps the rows and the references to them, while every read of the
			// table by its name fails.
			h.exec(t, "ALTER TABLE "+tt.table+" RENAME TO "+tt.table+"_gone")

			_, err := h.svc.Snapshot(t.Context())
			require.ErrorContains(t, err, tt.want)
		})
	}
}

func TestSnapshotOfAViewerWithNoStoredAccount(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.setPrefs(t, map[string]string{prefViewer: octoUser})

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, octoUser, snap.Account.Login, "the account comes from the stored status")
	assert.Equal(t, [3][]int{{}, {}, {}}, listed(snap))
	assert.False(t, snap.Rate.GraphQL.Known)
}

func TestSnapshotWithoutRateReadings(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.seed(t, []github.Item{pr(1, "OWNER", false)}, nil, nil)

	h.exec(t, "DELETE FROM rate_snapshots")

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []int{1}, numbers(snap.ReviewRequested), "the items still show")
	assert.Equal(t, RateGauge{}, snap.Rate, "every budget is unknown until it is read again")
}
