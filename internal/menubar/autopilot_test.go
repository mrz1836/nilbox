package menubar

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/inbox"
)

// run is an agent run on acme-org/example-repo #number, started ago before now.
func run(id int64, number int, ago time.Duration) AgentRun {
	return AgentRun{
		RunID: id, RoutineID: 2, MatchID: id + 10, CreatedAt: now.Add(-ago - time.Second), Repo: "acme-org/example-repo",
		Number: number, Title: "Bump  golang.org/x/net", Agent: agent.Claude, StartedAt: now.Add(-ago),
	}
}

func TestTheAutopilotGroupShowsOnlyWhileARoutineExists(t *testing.T) {
	t.Parallel()

	none := Summarize(inbox.Snapshot{}, nil, AutopilotState{Paused: true, NeedsYou: 2, Working: []AgentRun{run(1, 88, 0)}}, now)
	assert.Equal(t, Autopilot{}, none.Autopilot, "a user without a routine never sees Autopilot named")

	one := Summarize(inbox.Snapshot{}, nil, AutopilotState{Routines: 1}, now)
	assert.Equal(t, Autopilot{Shown: true}, one.Autopilot, "a heading and Pause Autopilot, unticked")
	assert.Equal(t, "Autopilot", AutopilotTitle)
	assert.Equal(t, "Pause Autopilot", PauseLabel)
}

func TestTheAutopilotGroupsRows(t *testing.T) {
	t.Parallel()

	st := AutopilotState{
		Routines: 3, Paused: true, NeedsYou: 1,
		Working: []AgentRun{run(1, 88, 6*time.Minute+20*time.Second), run(2, 12, 30*time.Second)},
	}

	got := Summarize(inbox.Snapshot{}, nil, st, now)

	assert.Equal(t, Autopilot{
		Shown: true, Paused: true, NeedsYou: "1 needs your OK",
		Working: []Working{
			{
				RunID: 1, RoutineID: 2, MatchID: 11, Created: "2026-03-04T11:53:39Z",
				Label:   "example-repo #88 · Claude · 6m",
				Tooltip: "acme-org/example-repo #88: Bump golang.org/x/net · Claude working for 6m",
			},
			{
				RunID: 2, RoutineID: 2, MatchID: 12, Created: "2026-03-04T11:59:29Z",
				Label:   "example-repo #12 · Claude · <1m",
				Tooltip: "acme-org/example-repo #12: Bump golang.org/x/net · Claude working for <1m",
			},
		},
		NextChange: now.Add(30 * time.Second),
	}, got.Autopilot, "the soonest a run's minutes change is when the menu redraws")
	assert.Zero(t, got.Count, "the count stays the inbox's")
}

func TestManyAgentsWorkingShowHowManyMore(t *testing.T) {
	t.Parallel()

	working := make([]AgentRun, 0, PerSection+2)
	for i := range PerSection + 2 {
		working = append(working, run(int64(i+1), i+1, time.Duration(i+1)*time.Minute))
	}

	got := AutopilotGroup(AutopilotState{Routines: 1, Working: working}, now)

	require.Len(t, got.Working, PerSection)
	assert.Equal(t, int64(1), got.Working[0].RunID, "the oldest first, as they come")
	assert.Equal(t, 2, got.WorkingMore)
	assert.Equal(t, now.Add(time.Minute), got.NextChange, "every run's minute counts, the rows left out too")
}

func TestNeedsYouLabel(t *testing.T) {
	t.Parallel()

	for n, want := range map[int]string{-1: "", 0: "", 1: "1 needs your OK", 2: "2 need your OK", 40: "40 need your OK"} {
		assert.Equal(t, want, NeedsYouLabel(n), strconv.Itoa(n))
	}
}

func TestElapsed(t *testing.T) {
	t.Parallel()

	for d, want := range map[time.Duration]string{
		-time.Minute:                 "<1m",
		0:                            "<1m",
		59 * time.Second:             "<1m",
		time.Minute:                  "1m",
		59*time.Minute + time.Second: "59m",
		time.Hour:                    "1h",
		2*time.Hour + 5*time.Minute:  "2h 5m",
	} {
		assert.Equal(t, want, Elapsed(d), d.String())
	}
}

func TestARunThatHasntStartedYetTicksAfterItsFirstMinute(t *testing.T) {
	t.Parallel()

	later := run(1, 1, -time.Minute) // its clock reads a minute ahead
	got := AutopilotGroup(AutopilotState{Routines: 1, Working: []AgentRun{later}}, now)
	assert.Equal(t, later.StartedAt.Add(time.Minute), got.NextChange)
	assert.Equal(t, "example-repo #1 · Claude · <1m", got.Working[0].Label)

	noCreated := run(2, 2, time.Minute)
	noCreated.CreatedAt = time.Time{}
	assert.Empty(t, AutopilotGroup(AutopilotState{Routines: 1, Working: []AgentRun{noCreated}}, now).Working[0].Created)
}

func TestSameAutopilot(t *testing.T) {
	t.Parallel()

	a := AutopilotGroup(AutopilotState{Routines: 1, NeedsYou: 1, Working: []AgentRun{run(1, 88, 6*time.Minute)}}, now)
	assert.True(t, SameAutopilot(a, a))

	later := AutopilotGroup(AutopilotState{Routines: 1, NeedsYou: 1, Working: []AgentRun{run(1, 88, 6*time.Minute)}},
		now.Add(10*time.Second))
	assert.True(t, SameAutopilot(a, later), "a change the rows don't show redraws nothing")

	for _, b := range []Autopilot{
		{},
		AutopilotGroup(AutopilotState{Routines: 1, NeedsYou: 2, Working: []AgentRun{run(1, 88, 6*time.Minute)}}, now),
		AutopilotGroup(AutopilotState{Routines: 1, NeedsYou: 1, Paused: true, Working: []AgentRun{run(1, 88, 6*time.Minute)}}, now),
		AutopilotGroup(AutopilotState{Routines: 1, NeedsYou: 1, Working: []AgentRun{run(1, 88, 7*time.Minute)}}, now),
		AutopilotGroup(AutopilotState{Routines: 1, NeedsYou: 1}, now),
	} {
		assert.False(t, SameAutopilot(a, b), "%+v", b)
	}
}
