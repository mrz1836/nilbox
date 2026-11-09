package menubar

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/sessions"
)

// now is the moment the tests look at the inbox: noon UTC, 7 AM in New York.
var now = time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // a fixed test time

func newYork(t *testing.T) *time.Location {
	t.Helper()

	loc, err := time.LoadLocation("America/New_York")
	require.NoError(t, err)

	return loc
}

// it is an item in acme-org/example-repo.
func it(id int64, number int, title string) inbox.Item {
	return inbox.Item{ID: id, Number: number, Title: title, Repo: "acme-org/example-repo"}
}

// labels lists a section's entry labels.
func labels(s Section) []string {
	out := make([]string, 0, len(s.Entries))
	for _, e := range s.Entries {
		out = append(out, e.Label)
	}

	return out
}

func TestSummarize(t *testing.T) {
	t.Parallel()

	shared := it(1, 101, "Fix the login redirect")
	snoozed := it(3, 103, "Snoozed")
	snoozed.SnoozedUntil = "2026-03-05T09:00:00Z"
	dismissed := it(4, 104, "Dismissed")
	dismissed.DismissedAt = "2026-03-01T00:00:00Z"

	snap := inbox.Snapshot{
		ReviewRequested: inbox.List{Items: []inbox.Item{shared, it(2, 102, "Bump the deps"), snoozed}},
		Authored:        inbox.List{Items: []inbox.Item{dismissed}},
		Assigned:        inbox.List{Items: []inbox.Item{shared, it(5, 7, "Crash on start")}},
		LastRefreshedAt: "2026-03-04T11:58:00Z",
	}

	got := Summarize(snap, nil, now.In(newYork(t)))

	assert.Equal(t, 3, got.Count)
	assert.Equal(t, "3", got.Label)
	assert.Equal(t, "Updated 6:58 AM", got.Status)
	assert.False(t, got.Failed)
	assert.Equal(t, time.Date(2026, 3, 5, 9, 0, 0, 0, time.UTC), got.NextChange)

	require.Len(t, got.Sections, 2, "Authored has nothing that needs you")
	assert.Equal(t, "Review requested", got.Sections[0].Title)
	assert.Equal(t, []string{"example-repo #101 · Fix the login redirect", "example-repo #102 · Bump the deps"},
		labels(got.Sections[0]))
	assert.Equal(t, "Assigned", got.Sections[1].Title)
	assert.Equal(t, []string{"example-repo #7 · Crash on start"}, labels(got.Sections[1]),
		"an item already listed is not listed again")

	e := got.Sections[0].Entries[0]
	assert.Equal(t, int64(1), e.ItemID)
	assert.Equal(t, "acme-org/example-repo #101: Fix the login redirect", e.Tooltip)
}

func TestSummarizeAnEmptyInbox(t *testing.T) {
	t.Parallel()

	got := Summarize(inbox.Snapshot{}, nil, now)

	assert.Zero(t, got.Count)
	assert.Empty(t, got.Label)
	assert.Empty(t, got.Sections)
	assert.Equal(t, "Not refreshed yet", got.Status)
	assert.Equal(t, "The inbox has not loaded yet", got.Nothing)
	assert.True(t, got.NextChange.IsZero())

	refreshed := Summarize(inbox.Snapshot{LastRefreshedAt: "2026-03-04T11:58:00Z"}, nil, now)
	assert.Equal(t, "Nothing needs you", refreshed.Nothing)
}

func TestALongListShowsHowManyMore(t *testing.T) {
	t.Parallel()

	items := make([]inbox.Item, 0, PerSection+3)
	for i := range PerSection + 3 {
		items = append(items, it(int64(i+1), i+1, "Item"))
	}

	got := Summarize(inbox.Snapshot{Authored: inbox.List{Items: items}}, nil, now)

	require.Len(t, got.Sections, 1)
	assert.Len(t, got.Sections[0].Entries, PerSection)
	assert.Equal(t, 3, got.Sections[0].More)
	assert.Equal(t, "3 more…", MoreLabel(got.Sections[0].More))
	assert.Equal(t, "8", got.Label)
}

func TestTheStatus(t *testing.T) {
	t.Parallel()

	failed := Summarize(inbox.Snapshot{LastRefreshedAt: "2026-03-04T11:00:00Z", LastError: "network down"}, nil, now)
	assert.Equal(t, "Last refresh failed", failed.Status)
	assert.True(t, failed.Failed)
	assert.Equal(t, "network down", failed.Error)

	yesterday := Summarize(inbox.Snapshot{LastRefreshedAt: "2026-03-03T22:15:00Z"}, nil, now)
	assert.Equal(t, "Updated Mar 3, 10:15 PM", yesterday.Status)
}

func TestLabel(t *testing.T) {
	t.Parallel()

	assert.Empty(t, label(0))
	assert.Empty(t, label(-1))
	assert.Equal(t, "1", label(1))
	assert.Equal(t, "99", label(MaxCount))
	assert.Equal(t, "99+", label(MaxCount+1))
}

func TestAnEntryIsTidied(t *testing.T) {
	t.Parallel()

	long := it(9, 12, "  A   title\twith "+strings.Repeat("many words ", 10))
	long.Repo = "no-owner"

	e := entry(long)

	assert.True(t, strings.HasPrefix(e.Label, "no-owner #12 · A title with many words"))
	assert.True(t, strings.HasSuffix(e.Label, "…"))
	assert.Len(t, []rune(strings.TrimPrefix(e.Label, "no-owner #12 · ")), maxTitleRunes)
	assert.NotContains(t, e.Tooltip, "\t")
}

func TestSummarizeListsTheSessionsWaitingFirst(t *testing.T) {
	t.Parallel()

	waiting := []sessions.Session{
		{
			ID: "s1", Agent: agent.Claude, ItemID: 7, Repo: "acme-org/example-repo", Number: 12,
			Title: "Fix  the widget", URL: "https://github.com/acme-org/example-repo/pull/12",
			State: sessions.StateNeedsApproval,
		},
		{ID: "s2", Agent: agent.Codex, Repo: "acme-org/other-repo", Number: 3, State: sessions.StateNeedsInput},
	}

	got := Summarize(inbox.Snapshot{LastRefreshedAt: "2026-03-04T11:58:00Z"}, waiting, now)

	assert.Equal(t, []Waiting{
		{
			SessionID: "s1", ItemID: 7, URL: "https://github.com/acme-org/example-repo/pull/12",
			Label:   "example-repo #12 · Claude needs approval",
			Tooltip: "acme-org/example-repo #12: Fix the widget · Claude needs approval",
		},
		{
			SessionID: "s2", Label: "other-repo #3 · Codex needs your input",
			Tooltip: "acme-org/other-repo #3 · Codex needs your input",
		},
	}, got.Waiting)
	assert.Zero(t, got.Count, "the count stays the inbox's")
	assert.Equal(t, "Waiting for you", WaitingTitle)
}

func TestManySessionsWaitingShowHowManyMore(t *testing.T) {
	t.Parallel()

	waiting := make([]sessions.Session, 0, PerSection+2)
	for i := range PerSection + 2 {
		waiting = append(waiting, sessions.Session{
			ID: "s" + strconv.Itoa(i+1), Agent: agent.Claude, Repo: "acme-org/example-repo", Number: i + 1,
			State: sessions.StateNeedsApproval,
		})
	}

	got := Summarize(inbox.Snapshot{}, waiting, now)

	require.Len(t, got.Waiting, PerSection)
	assert.Equal(t, "s1", got.Waiting[0].SessionID, "the first ones, in order")
	assert.Equal(t, "s"+strconv.Itoa(PerSection), got.Waiting[PerSection-1].SessionID)
	assert.Equal(t, 2, got.WaitingMore)
	assert.Equal(t, "2 more…", MoreLabel(got.WaitingMore))
	assert.Len(t, WaitingEntries(waiting), PerSection+2, "the entries before the cut are every one")

	exactly := Summarize(inbox.Snapshot{}, waiting[:PerSection], now)
	assert.Len(t, exactly.Waiting, PerSection)
	assert.Zero(t, exactly.WaitingMore)
	assert.Empty(t, Summarize(inbox.Snapshot{}, nil, now).Waiting)
}
