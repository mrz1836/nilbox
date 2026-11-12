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
		{event.RevealItem{}, event.NameRevealItem},
		{event.WorktreesUpdated{}, event.NameWorktreesUpdated},
		{event.NotificationsUpdated{}, event.NameNotificationsUpdated},
		{event.BudgetUpdated{}, event.NameBudgetUpdated},
		{event.SessionsUpdated{}, event.NameSessionsUpdated},
		{event.ReviewProgress{}, event.NameReviewProgress},
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
		event.NameInboxUpdated, event.NameRevealItem, event.NameWorktreesUpdated, event.NameNotificationsUpdated,
		event.NameBudgetUpdated, event.NameSessionsUpdated, event.NameReviewProgress, event.NameToolsUpdated,
		event.NameScriptRunsUpdated, event.NameScriptRunProgress, event.NameQuitRequested, event.NameReleasesUpdated,
		event.NameReleasesProgress,
		event.NameHooksUpdated, event.NameWorktreesProgress,
	}, names)

	for _, e := range []event.Event{
		event.ThemeChanged{},
		event.TextSizeChanged{},
		event.MenuBarChanged{},
		event.FirstPaint{},
		event.IntegrationsChanged{},
		event.QuitConfirmed{},
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

	data, err := json.Marshal(event.RevealItem{ItemID: 7, URL: "https://github.com/acme-org/example-repo/pull/12"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"itemId":7,"url":"https://github.com/acme-org/example-repo/pull/12","runId":0,"runStarted":""}`, string(data))

	data, err = json.Marshal(event.RevealItem{RunID: 42, RunStarted: "2026-11-10T16:00:00Z"})
	require.NoError(t, err)
	assert.JSONEq(t, `{"itemId":0,"url":"","runId":42,"runStarted":"2026-11-10T16:00:00Z"}`, string(data), "a failed run's notification opens the run")
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
