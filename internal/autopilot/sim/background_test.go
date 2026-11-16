package sim_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/sim"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// A routine's Read only agent in the background, as the background runner runs it: from
// match to done, what holds its start back, and what it tells.

func TestAReadOnlyBackgroundRunGoesFromMatchToDoneAndToldOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.Reviews(), autopilotprefs.ModeAuto)

		m := only(t, s)
		require.Equal(t, autopilot.StateRunning, m.State, m.Words)
		require.Len(t, s.Runner.Started(), 1)

		working, err := s.Service.Working(t.Context())
		require.NoError(t, err)
		require.Len(t, working.Working, 1, "Activity's Working lists the run")
		assert.Equal(t, "background", working.Working[0].Where)

		cost := 0.12
		s.Runner.Finish(m.Runs[0].ID, autopilot.RunEnd{Outcome: autopilot.RunDone, Summary: "Finished: looks good.", Cost: &cost})
		s.Settle()

		assert.Equal(t, []sim.Row{row(1, autopilot.StateDone, "", autopilot.ReasonRan, "appears:item")}, s.Ledger())

		finished := noticesOf(s, autopilot.NoticeFinished)
		require.Len(t, finished, 1, "Agent finished is told")
		assert.Equal(t, "Routine: Finished: looks good. Cost $0.12.", finished[0].Body)

		s.Restart()
		s.Refresh()

		assert.Len(t, noticesOf(s, autopilot.NoticeFinished), 1, "a restart tells nothing twice")
		assert.Len(t, s.Runner.Started(), 1, "and runs nothing again")
	})
}

func TestAStartQueuedBehindASessionIsTriedAgainOnceATickPasses(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.Begin()

		busy := autopilot.Result{
			Wait: autopilot.BlockBusyWorktree, WaitWords: "Queued: pr-1 is in use by a session in your terminal; starts when it ends.",
		}
		s.Runner.AnswerWith(busy, busy)

		r := s.Save("Routine", sim.Reviews(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()

		m := only(t, s)
		assert.Equal(t, autopilot.StateQueued, m.State)
		assert.Equal(t, autopilot.BlockBusyWorktree, m.BlockedBy)
		assert.Zero(t, m.Attempt, "a start held back is no try")
		require.Len(t, s.Runner.Held(), 1, "tried once, not again on the cycle its answer woke")

		reads := s.World.Reads(1)

		needs, err := s.Service.NeedsYou(t.Context())
		require.NoError(t, err)
		require.Len(t, needs, 1, "Needs you lists it queued, saying what it waits on")
		assert.Equal(t, busy.WaitWords, needs[0].Words)

		s.Settle()
		assert.Len(t, s.Runner.Held(), 1, "nothing tries it again before a tick")

		s.Advance(2*time.Minute, false)

		assert.Len(t, s.Runner.Held(), 2, "once a tick has passed it is tried again")
		assert.Equal(t, reads+1, s.World.Reads(1), "each try reads the item once")

		s.Advance(2*time.Minute, false)

		assert.Equal(t, autopilot.StateRunning, only(t, s).State, "it starts by itself once the session ends")
	})
}

// noticesOf returns the notices of kind told so far.
func noticesOf(s *sim.Sim, kind autopilot.NoticeKind) []autopilot.Notice {
	var out []autopilot.Notice

	for _, n := range s.Notices.Told() {
		if n.Kind == kind {
			out = append(out, n)
		}
	}

	return out
}
