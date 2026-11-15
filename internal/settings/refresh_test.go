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
	assert.Equal(t, RefreshIntervals{
		Inbox: 300, Checks: 45, ReleaseChecks: 45, Worktrees: 300, Notifications: 300, GitHubStatus: 1800,
	}, fresh.Refresh)
	assert.Equal(t, refreshChoices(), fresh.RefreshChoices)

	got, err := svc.SetRefreshInterval(t.Context(), RefreshInbox, 900)
	require.NoError(t, err)
	assert.Equal(t, 900, got.Refresh.Inbox)

	got, err = svc.SetRefreshInterval(t.Context(), RefreshChecks, 0)
	require.NoError(t, err)
	assert.Zero(t, got.Refresh.Checks, "the checks follow can be off")

	got, err = svc.SetRefreshInterval(t.Context(), RefreshReleaseChecks, 120)
	require.NoError(t, err)
	assert.Equal(t, 120, got.Refresh.ReleaseChecks, "the Release page's follow has its own interval")
	assert.Zero(t, got.Refresh.Checks, "which leaves the pull requests' alone")

	got, err = svc.SetRefreshInterval(t.Context(), RefreshReleaseChecks, 0)
	require.NoError(t, err)
	assert.Zero(t, got.Refresh.ReleaseChecks, "and can be off too")

	_, err = svc.SetRefreshInterval(t.Context(), RefreshWorktrees, 1800)
	require.NoError(t, err)

	got, err = svc.SetRefreshInterval(t.Context(), RefreshNotifications, 60)
	require.NoError(t, err)
	assert.Equal(t, 60, got.Refresh.Notifications, "the notifications every minute, GitHub's own poll interval")

	got, err = svc.SetRefreshInterval(t.Context(), RefreshGitHubStatus, 0)
	require.NoError(t, err)
	assert.Equal(t, RefreshIntervals{
		Inbox: 900, Checks: 0, ReleaseChecks: 0, Worktrees: 1800, Notifications: 60, GitHubStatus: 0,
	}, got.Refresh, "the GitHub status check can be off")
	assert.Equal(t, []event.Event{
		event.IntervalsChanged{},
		event.IntervalsChanged{},
		event.IntervalsChanged{},
		event.IntervalsChanged{},
		event.IntervalsChanged{},
		event.IntervalsChanged{},
		event.IntervalsChanged{},
	}, events.All())

	for name, refused := range map[string]struct {
		kind    RefreshKind
		seconds int
	}{
		"an unknown kind":            {"releases", 300},
		"an interval not offered":    {RefreshInbox, 7},
		"the inbox off":              {RefreshInbox, 0},
		"the worktrees every 30 s":   {RefreshWorktrees, 30},
		"the checks every 10 hours":  {RefreshChecks, 36000},
		"the release checks at 10 s": {RefreshReleaseChecks, 10},
		"the notifications off":      {RefreshNotifications, 0},
		"the notifications at 30 s":  {RefreshNotifications, 30},
		"the GitHub status at 60 s":  {RefreshGitHubStatus, 60},
	} {
		_, err = svc.SetRefreshInterval(t.Context(), refused.kind, refused.seconds)
		require.ErrorIs(t, err, ErrInvalidRefresh, name)
	}

	assert.Len(t, events.All(), 7, "a refused interval stores nothing and wakes nothing")

	reread, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, got.Refresh, reread.Refresh)
}

func TestRefreshIntervalsReadAStoredValueNotOfferedAsTheDefault(t *testing.T) {
	t.Parallel()

	got := decodeRefreshIntervals(map[string]string{
		KeyRefreshInbox: "7", KeyRefreshChecks: "often", KeyRefreshReleaseChecks: "15", KeyRefreshWorktrees: "600",
		KeyRefreshNotifications: "45", KeyRefreshGitHubStatus: "45",
	})

	assert.Equal(t, RefreshIntervals{
		Inbox: DefaultRefreshInbox, Checks: DefaultRefreshChecks, ReleaseChecks: DefaultRefreshReleaseChecks,
		Worktrees: 600, Notifications: DefaultRefreshNotifications, GitHubStatus: DefaultRefreshGitHubStatus,
	}, got)

	stored := decodeRefreshIntervals(map[string]string{KeyRefreshReleaseChecks: "0"})
	assert.Zero(t, stored.ReleaseChecks, "off is stored as off")
	assert.Equal(t, DefaultRefreshChecks, stored.Checks, "and leaves the pull requests' follow at its default")
}

func TestEveryTurnsAnIntervalIntoADuration(t *testing.T) {
	t.Parallel()

	r := RefreshIntervals{
		Inbox: 120, Checks: 0, ReleaseChecks: 30, Worktrees: 3600, Notifications: 60, GitHubStatus: 1800,
	}

	assert.Equal(t, 2*time.Minute, r.Every(RefreshInbox))
	assert.Zero(t, r.Every(RefreshChecks))
	assert.Equal(t, 30*time.Second, r.Every(RefreshReleaseChecks))
	assert.Equal(t, time.Hour, r.Every(RefreshWorktrees))
	assert.Equal(t, time.Minute, r.Every(RefreshNotifications))
	assert.Equal(t, 30*time.Minute, r.Every(RefreshGitHubStatus))
	assert.Zero(t, r.Every("releases"))
}

func TestRefreshKindsListTheReleaseChecksAfterThePullRequests(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []RefreshKind{
		RefreshInbox, RefreshChecks, RefreshReleaseChecks, RefreshWorktrees, RefreshNotifications, RefreshGitHubStatus,
	}, RefreshKinds())

	choices := refreshChoices()
	assert.Equal(t, choices.Checks, choices.ReleaseChecks, "the same cadence as the pull requests' checks")
	assert.Equal(t, 45, DefaultRefreshReleaseChecks)
	assert.Equal(t, "refresh.releaseChecks", KeyRefreshReleaseChecks)
}

func TestTheGitHubStatusIsCheckedEveryHalfHourByDefault(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 1800, DefaultRefreshIntervals().GitHubStatus)
	assert.Equal(t, []int{0, 300, 600, 900, 1800, 3600}, refreshChoices().GitHubStatus, "off, or 5 minutes to an hour")
	assert.Equal(t, "refresh.githubStatus", KeyRefreshGitHubStatus)
}
