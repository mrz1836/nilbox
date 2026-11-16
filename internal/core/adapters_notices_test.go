package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/menubar"
	"github.com/mrz1836/nilbox/internal/notify"
	"github.com/mrz1836/nilbox/internal/notifyprefs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestEveryNoticeIsAKindOfNotification(t *testing.T) {
	t.Parallel()

	kinds := make([]notifyprefs.Kind, 0, len(autopilot.NoticeKinds()))

	for _, n := range autopilot.NoticeKinds() {
		kind, ok := noticeKinds[n]
		require.True(t, ok, "%s has a kind of notification", n)

		kinds = append(kinds, kind)
	}

	assert.Equal(t, notifyprefs.AutopilotKinds(), kinds, "each of Autopilot's kinds, once, in order")
	assert.Len(t, noticeKinds, len(autopilot.NoticeKinds()))

	_, err := noticeEntry(autopilot.Notice{Kind: "later"})
	require.ErrorIs(t, err, errUnknownNotice)
}

func TestANoticeIsRecordedWithItsItemHeadingAndTarget(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	opts := testOptions(t)
	opts.Events = events
	c := bootstrap(t, opts)

	sink := autopilotNotices{dispatcher: notify.NewDispatcher(notify.Config{Pool: c.DB, Events: events})}
	made := time.Date(2026, 11, 16, 9, 0, 0, 0, time.FixedZone("EST", -5*3600))

	require.NoError(t, sink.Notice(t.Context(), autopilot.Notice{
		Kind: autopilot.NoticeAlert, RoutineID: 3, Routine: "Tell me about security failures", MatchID: 12,
		CreatedAt: made, Title: "Bump golang.org/x/net", Body: "1 check failed on a1b2c3d.",
		Item: &autopilot.ItemRef{
			Kind: item.KindPR, Repo: "acme-org/example-repo", Number: 88,
			URL: "https://github.com/acme-org/example-repo/pull/88",
		},
	}))

	require.NoError(t, sink.Notice(t.Context(), autopilot.Notice{
		Kind: autopilot.NoticeProblem, RoutineID: 3, Routine: "Tell me about security failures",
		Title: "Tell me about security failures", Body: "GitHub couldn't answer its search: timeout.",
	}))

	log, err := c.Notifications.List(t.Context())
	require.NoError(t, err)
	require.Len(t, log.Items, 2)

	problem, alert := log.Items[0], log.Items[1]

	assert.Equal(t, notify.KindAutopilotAlert, alert.Kind)
	assert.Equal(t, "Tell me about security failures", alert.Heading, "the routine heads an alert")
	assert.Equal(t, "Bump golang.org/x/net", alert.Title)
	assert.Equal(t, "1 check failed on a1b2c3d.", alert.Detail)
	assert.Equal(t, "acme-org/example-repo", alert.Repo)
	assert.Equal(t, item.KindPR, alert.ItemKind)
	assert.Equal(t, 88, alert.Number)
	assert.Equal(t, event.AutopilotTarget{RoutineID: 3, MatchID: 12, CreatedAt: "2026-11-16T14:00:00Z"}, alert.Autopilot,
		"its match, with when it was made, in UTC")

	assert.Equal(t, notify.KindAutopilotProblem, problem.Kind)
	assert.Empty(t, problem.Heading, "a problem is headed by its kind")
	assert.Empty(t, problem.ItemKind, "about no item")
	assert.Equal(t, event.AutopilotTarget{RoutineID: 3}, problem.Autopilot, "it opens its routine")
	assert.Contains(t, events.All(), event.Event(event.NotificationsUpdated{}))
}

func TestTheShellReadsAutopilotsGlance(t *testing.T) {
	t.Parallel()

	started := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)

	got := shellAutopilotState(autopilot.Glance{
		Routines: 2, Enabled: 0, Paused: true, NeedsYou: 3,
		Working: []autopilot.WorkingRun{{
			RunID: 7, MatchID: 5, RoutineID: 2, Agent: "claude", Where: "background", StartedAt: started,
			CreatedAt: started.Add(-time.Second),
			Item:      autopilot.ItemRef{Repo: "acme-org/example-repo", Number: 88, Title: "Bump"},
		}},
	})

	assert.Equal(t, menubar.AutopilotState{
		Routines: 2, Paused: true, NeedsYou: 3,
		Working: []menubar.AgentRun{{
			RunID: 7, RoutineID: 2, MatchID: 5, CreatedAt: started.Add(-time.Second), Repo: "acme-org/example-repo",
			Number: 88, Title: "Bump", Agent: agent.Claude, StartedAt: started,
		}},
	}, got.Menu)
	assert.True(t, got.KeepsRunning, "an agent working keeps the app running, every routine off")

	idle := shellAutopilotState(autopilot.Glance{Routines: 4})
	assert.False(t, idle.KeepsRunning, "routines that are all off keep nothing running")
	assert.True(t, shellAutopilotState(autopilot.Glance{Routines: 1, Enabled: 1}).KeepsRunning, "a routine on does")
}

func TestTheShellPausesAutopilotThroughTheSettings(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	opts := testOptions(t)
	opts.Events = events
	c := bootstrap(t, opts)

	require.NoError(t, c.ShellAutopilot.SetPaused(t.Context(), true))

	got, err := c.Settings.Get(t.Context())
	require.NoError(t, err)
	assert.True(t, got.Autopilot.Paused, "written as Settings writes it")

	st, err := c.ShellAutopilot.State(t.Context())
	require.NoError(t, err)
	assert.True(t, st.Menu.Paused)
	assert.Zero(t, st.Menu.Routines)
	assert.False(t, st.KeepsRunning)

	require.NoError(t, c.ShellAutopilot.SetPaused(t.Context(), false))

	st, err = c.ShellAutopilot.State(t.Context())
	require.NoError(t, err)
	assert.False(t, st.Menu.Paused)

	none, err := ShellAutopilot{}.State(t.Context())
	require.NoError(t, err)
	assert.Equal(t, menubar.AutopilotState{}, none.Menu, "no Autopilot, nothing to show")
	require.NoError(t, ShellAutopilot{}.SetPaused(t.Context(), true))
}
