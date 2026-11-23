package event_test

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/timing"
)

// recorder is a Sink that keeps what it hears, safe for concurrent use.
type recorder struct {
	mu  sync.Mutex
	got []event.Event
}

func (r *recorder) Emit(e event.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.got = append(r.got, e)
}

func (r *recorder) events() []event.Event {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]event.Event(nil), r.got...)
}

func TestEveryEventHasItsOwnName(t *testing.T) {
	t.Parallel()

	all := []struct {
		e    event.Event
		name string
	}{
		{event.InboxUpdated{}, event.NameInboxUpdated},
		{event.InboxProgress{}, event.NameInboxProgress},
		{event.RevealItem{}, event.NameRevealItem},
		{event.WorktreesUpdated{}, event.NameWorktreesUpdated},
		{event.NotificationsUpdated{}, event.NameNotificationsUpdated},
		{event.BudgetUpdated{}, event.NameBudgetUpdated},
		{event.SessionsUpdated{}, event.NameSessionsUpdated},
		{event.ReviewProgress{}, event.NameReviewProgress},
		{event.IssueProgress{}, event.NameIssueProgress},
		{event.ThemeChanged{}, event.NameThemeChanged},
		{event.TextSizeChanged{}, event.NameTextSizeChanged},
		{event.MenuBarChanged{}, event.NameMenuBarChanged},
		{event.FirstPaint{}, event.NameFirstPaint},
		{event.ScriptRunsUpdated{}, event.NameScriptRunsUpdated},
		{event.ScriptRunProgress{}, event.NameScriptRunProgress},
		{event.ReleasesUpdated{}, event.NameReleasesUpdated},
		{event.ReleasesProgress{}, event.NameReleasesProgress},
		{event.HooksUpdated{}, event.NameHooksUpdated},
		{event.WorktreesProgress{}, event.NameWorktreesProgress},
		{event.ManifestsProgress{}, event.NameManifestsProgress},
		{event.IntervalsChanged{}, event.NameIntervalsChanged},
		{event.RepoRulesChanged{}, event.NameRepoRulesChanged},
		{event.ThreadRulesChanged{}, event.NameThreadRulesChanged},
		{event.StatusRulesChanged{}, event.NameStatusRulesChanged},
		{event.GHThreadsUpdated{}, event.NameGHThreadsUpdated},
		{event.GHThreadsBulk{}, event.NameGHThreadsBulk},
		{event.ServiceStatusUpdated{}, event.NameServiceStatusUpdated},
		{event.SetupFinished{}, event.NameSetupFinished},
		{event.ResetRequested{}, event.NameResetRequested},
		{event.UpdatesChanged{}, event.NameUpdatesChanged},
		{event.AutopilotChanged{}, event.NameAutopilotChanged},
		{event.InboxItemsChanged{}, event.NameInboxItemsChanged},
		{event.AutopilotUpdated{}, event.NameAutopilotUpdated},
		{event.AutopilotRun{}, event.NameAutopilotRun},
		{event.DescribeItUpdated{}, event.NameDescribeItUpdated},
		{event.DescribeItProposals{}, event.NameDescribeItProposals},
		{event.UpdatesUpdated{}, event.NameUpdatesUpdated},
		{event.UpdateRestartRequested{}, event.NameUpdateRestartRequested},
		{event.UpdateRestartFailed{}, event.NameUpdateRestartFailed},
		{event.UpdateQuitHeld{}, event.NameUpdateQuitHeld},
		{event.QuitKept{}, event.NameQuitKept},
	}

	seen := make(map[string]bool, len(all))

	for _, c := range all {
		assert.Equal(t, c.name, c.e.Name())
		assert.False(t, seen[c.name], "%s names two events", c.name)

		seen[c.name] = true
	}
}

func TestTheWindowHearsOnlyTheWindowEvents(t *testing.T) {
	t.Parallel()

	names := make([]string, 0, len(event.WindowEvents()))
	for _, e := range event.WindowEvents() {
		names = append(names, e.Name())
	}

	assert.Equal(t, []string{
		event.NameInboxUpdated, event.NameInboxProgress, event.NameRevealItem, event.NameWorktreesUpdated,
		event.NameNotificationsUpdated,
		event.NameBudgetUpdated, event.NameSessionsUpdated, event.NameReviewProgress, event.NameIssueProgress,
		event.NameToolsUpdated, event.NameScriptRunsUpdated, event.NameScriptRunProgress, event.NameQuitRequested,
		event.NameReleasesUpdated, event.NameReleasesProgress, event.NameHooksUpdated, event.NameWorktreesProgress,
		event.NameManifestsProgress, event.NameGHThreadsUpdated, event.NameGHThreadsBulk, event.NameServiceStatusUpdated, event.NameUpdatesUpdated, event.NameUpdateRestartRequested,
		event.NameAutopilotUpdated, event.NameAutopilotRun, event.NameDescribeItUpdated, event.NameDescribeItProposals,
	}, names)

	for _, e := range []event.Event{
		event.ThemeChanged{},
		event.TextSizeChanged{},
		event.MenuBarChanged{},
		event.FirstPaint{},
		event.IntegrationsChanged{},
		event.QuitConfirmed{},
		event.IntervalsChanged{},
		event.RepoRulesChanged{},
		event.ThreadRulesChanged{},
		event.StatusRulesChanged{},
		event.SetupFinished{},
		event.ResetRequested{},
		event.UpdatesChanged{},
		event.AutopilotChanged{},
		event.InboxItemsChanged{IDs: []int64{1}},
		event.UpdateRestartFailed{},
		event.UpdateQuitHeld{},
		event.QuitConfirmed{Restarting: true},
		event.QuitKept{},
	} {
		_, ok := e.(event.WindowEvent)
		assert.False(t, ok, "%s stays in Go", e.Name())
	}
}

func TestScriptRunProgressIsSentAsTheFrontendReadsIt(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(event.ScriptRunProgress{ID: 42})
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":42}`, string(data))
}

func TestReleasesProgressIsSentAsTheFrontendReadsIt(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(event.ReleasesProgress{Phase: event.ReleasesAskingGitHub, Done: 45, Total: 151})
	require.NoError(t, err)
	assert.JSONEq(t, `{"phase":"github","done":45,"total":151}`, string(data))
}

func TestRevealItemIsSentAsTheFrontendReadsIt(t *testing.T) {
	t.Parallel()

	const none = `"autopilot":{"routineId":0,"matchId":0,"runId":0,"createdAt":"","activity":false}`

	data, err := json.Marshal(event.RevealItem{ItemID: 7, URL: "https://github.com/acme-org/example-repo/pull/12"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"itemId":7,"url":"https://github.com/acme-org/example-repo/pull/12","runId":0,"runStarted":"","notificationId":0,`+none+`}`, string(data))

	data, err = json.Marshal(event.RevealItem{RunID: 42, RunStarted: "2026-11-10T16:00:00Z"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"itemId":0,"url":"","runId":42,"runStarted":"2026-11-10T16:00:00Z","notificationId":0,`+none+`}`, string(data), "a failed run's notification opens the run")

	data, err = json.Marshal(event.RevealItem{ItemID: 7, NotificationID: 3})
	require.NoError(t, err)
	assert.JSONEq(t, `{"itemId":7,"url":"","runId":0,"runStarted":"","notificationId":3,`+none+`}`, string(data), "a banner's click marks its notification read")

	data, err = json.Marshal(event.RevealItem{
		ItemID: 7, NotificationID: 3,
		Autopilot: event.AutopilotTarget{RoutineID: 2, MatchID: 9, CreatedAt: "2026-11-16T09:00:00Z"},
	})
	require.NoError(t, err)
	assert.JSONEq(t, `{"itemId":7,"url":"","runId":0,"runStarted":"","notificationId":3,`+
		`"autopilot":{"routineId":2,"matchId":9,"runId":0,"createdAt":"2026-11-16T09:00:00Z","activity":false}}`, string(data),
		"an Autopilot notification's click carries its target")
}

func TestARevealNamesSomethingOnlyWithAPartSet(t *testing.T) {
	t.Parallel()

	assert.False(t, event.RevealItem{}.Named())
	assert.False(t, event.RevealItem{RunStarted: "2026-11-10T16:00:00Z"}.Named(), "a start alone names no run")
	assert.True(t, event.RevealItem{ItemID: 1}.Named())
	assert.True(t, event.RevealItem{URL: "https://github.com/acme-org"}.Named())
	assert.True(t, event.RevealItem{RunID: 1}.Named())
	assert.True(t, event.RevealItem{NotificationID: 1}.Named())
	assert.True(t, event.RevealItem{Autopilot: event.AutopilotTarget{RoutineID: 1}}.Named())
	assert.True(t, event.AutopilotTarget{}.IsZero())
	assert.True(t, event.AutopilotTarget{CreatedAt: "2026-11-10T16:00:00Z"}.IsZero(), "a time alone names nothing")
	assert.False(t, event.AutopilotTarget{MatchID: 1}.IsZero())
	assert.False(t, event.AutopilotTarget{RunID: 1}.IsZero())
	assert.False(t, event.AutopilotTarget{Activity: true}.IsZero(), "Activity is somewhere to open")
}

func TestOrFallsBackToDiscard(t *testing.T) {
	t.Parallel()

	assert.Equal(t, event.Discard{}, event.Or(nil))

	event.Or(nil).Emit(event.InboxUpdated{})

	r := &recorder{}
	event.Or(r).Emit(event.InboxUpdated{})
	assert.Equal(t, []event.Event{event.InboxUpdated{}}, r.events())
}

func TestARelayDropsEventsUntilConnected(t *testing.T) {
	t.Parallel()

	var relay event.Relay

	relay.Emit(event.InboxUpdated{})

	r := &recorder{}
	relay.Connect(r)

	paint := event.FirstPaint{Measurement: timing.Measurement{FirstPaintMS: 412, Source: timing.SourceOS}}

	relay.Emit(event.MenuBarChanged{On: true})
	relay.Emit(paint)

	assert.Equal(t, []event.Event{event.MenuBarChanged{On: true}, paint}, r.events())
}

func TestARelayIsSafeForConcurrentUse(t *testing.T) {
	t.Parallel()

	var (
		relay event.Relay
		wg    sync.WaitGroup
	)

	r := &recorder{}

	for range 8 {
		wg.Go(func() { relay.Emit(event.NotificationsUpdated{}) })
	}

	wg.Go(func() { relay.Connect(r) })
	wg.Wait()

	relay.Emit(event.NotificationsUpdated{})
	assert.NotEmpty(t, r.events(), "an event emitted once connected arrives")
	assert.LessOrEqual(t, len(r.events()), 9)
}
