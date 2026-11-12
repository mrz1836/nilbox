package settings

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestRefreshIntervalsAreStoredAndWakeWhatRefreshes(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	svc := serviceOver(t, testutil.DB(t), t.TempDir(), WithEvents(events))

	fresh, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, RefreshIntervals{Inbox: 300, Checks: 45, Worktrees: 300}, fresh.Refresh)
	assert.Equal(t, refreshChoices(), fresh.RefreshChoices)

	got, err := svc.SetRefreshInterval(t.Context(), RefreshInbox, 900)
	require.NoError(t, err)
	assert.Equal(t, 900, got.Refresh.Inbox)

	got, err = svc.SetRefreshInterval(t.Context(), RefreshChecks, 0)
	require.NoError(t, err)
	assert.Zero(t, got.Refresh.Checks, "the checks follow can be off")

	got, err = svc.SetRefreshInterval(t.Context(), RefreshWorktrees, 1800)
	require.NoError(t, err)
	assert.Equal(t, RefreshIntervals{Inbox: 900, Checks: 0, Worktrees: 1800}, got.Refresh)
	assert.Equal(t, []event.Event{event.IntervalsChanged{}, event.IntervalsChanged{}, event.IntervalsChanged{}},
		events.All())

	for name, refused := range map[string]struct {
		kind    RefreshKind
		seconds int
	}{
		"an unknown kind":           {"releases", 300},
		"an interval not offered":   {RefreshInbox, 7},
		"the inbox off":             {RefreshInbox, 0},
		"the worktrees every 30 s":  {RefreshWorktrees, 30},
		"the checks every 10 hours": {RefreshChecks, 36000},
	} {
		_, err = svc.SetRefreshInterval(t.Context(), refused.kind, refused.seconds)
		require.ErrorIs(t, err, ErrInvalidRefresh, name)
	}

	assert.Len(t, events.All(), 3, "a refused interval stores nothing and wakes nothing")

	reread, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, got.Refresh, reread.Refresh)
}

func TestRefreshIntervalsReadAStoredValueNotOfferedAsTheDefault(t *testing.T) {
	t.Parallel()

	got := decodeRefreshIntervals(map[string]string{
		KeyRefreshInbox: "7", KeyRefreshChecks: "often", KeyRefreshWorktrees: "600",
	})

	assert.Equal(t, RefreshIntervals{Inbox: DefaultRefreshInbox, Checks: DefaultRefreshChecks, Worktrees: 600}, got)
}

func TestEveryTurnsAnIntervalIntoADuration(t *testing.T) {
	t.Parallel()

	r := RefreshIntervals{Inbox: 120, Checks: 0, Worktrees: 3600}

	assert.Equal(t, 2*time.Minute, r.Every(RefreshInbox))
	assert.Zero(t, r.Every(RefreshChecks))
	assert.Equal(t, time.Hour, r.Every(RefreshWorktrees))
	assert.Zero(t, r.Every("releases"))
}
