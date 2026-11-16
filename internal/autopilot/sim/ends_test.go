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

// How a run that the quit, a crash, or its runner cut off ends, and how a stop's reason
// outlives a stop that failed.

func TestAGitHubActionTheQuitCutOffIsNeverSentAgainOnItsOwn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true})
		s.Begin()

		// The write goes out, and the quit cuts off its answer.
		s.GitHub.Hold()

		r := s.Save("Rebase", sim.Rebases(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()
		require.Len(t, s.GitHub.Jobs(), 1)

		s.Crash()
		s.GitHub.Release()
		s.Begin()
		s.Refresh()
		s.Advance(3*time.Minute, true)

		assert.Len(t, s.GitHub.Jobs(), 1, "never sent a second time on its own")

		m := only(t, s)
		assert.Equal(t, autopilot.BlockInterrupted, m.BlockedBy)
		assert.Contains(t, m.Words, "check the pull request on GitHub before you send it again")
		require.Len(t, m.Runs, 1)
		assert.Equal(t, string(autopilot.RunInterrupted), m.Runs[0].Outcome)
	})
}

func TestAStartTheRunnerTookIsNeverStartedAgainOnItsOwn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)

		// The runner starts the run, and the quit cuts off its answer; the run then ends
		// while the app is closed, with no one told.
		s.Runner.HoldAnswers()
		s.World.Fail(1)
		s.Refresh()
		require.Len(t, s.Runner.Started(), 1)

		s.Crash()
		s.Runner.Release()
		s.Runner.Forget(s.Runner.Started()[0].RunID)

		s.Begin()
		s.Refresh()
		s.Advance(3*time.Minute, false)

		assert.Len(t, s.Runner.Started(), 1, "it started once: it waits for the user")
		assert.Equal(t, autopilot.BlockInterrupted, only(t, s).BlockedBy)
	})
}

func TestAStartTheQuitCutOffIsDecidedAgainBeforeItStarts(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)
		s.Runner.Hold()
		s.World.Fail(1)
		s.Refresh()
		s.Crash()
		s.Runner.Release()

		// While the app was closed, the checks passed: the match no longer matches.
		s.World.Pass(1)
		s.Begin()
		s.Refresh()
		s.Advance(3*time.Minute, false)

		assert.Empty(t, s.Runner.Started(), "the agent never runs on a pull request whose checks pass")

		m := only(t, s)
		assert.Equal(t, autopilot.StateClosed, m.State, m.Words)
		assert.Equal(t, autopilot.ReasonNoLongerMatches, m.Reason)
		assert.Zero(t, m.Attempt, "the start that never reached the runner counts as no run")
	})
}

func TestARunWhoseRunnerLetsGoBeforeItsEndKeepsItsEnd(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)
		s.World.Fail(1)
		s.Refresh()

		run := only(t, s).Runs[0].ID
		pushed := sim.Commit(1, 2)

		// The runner lets go of the run, and its end comes a moment later: one check
		// finding it gone is not enough.
		deliver := s.Runner.FinishLate(run, autopilot.RunEnd{Commit: pushed, Cost: new(1.5)})
		s.Advance(time.Minute, false)
		deliver()
		s.Settle()

		m := only(t, s)
		assert.Equal(t, autopilot.StateVerifying, m.State, m.Words)
		assert.Equal(t, string(autopilot.RunDone), m.Runs[0].Outcome)
		assert.Equal(t, pushed, m.Runs[0].Commit)
		require.NotNil(t, m.Runs[0].Cost)
		assert.InDelta(t, 1.5, *m.Runs[0].Cost, 0.001)
	})
}

func TestARunnersEndAfterTheRunWasTakenForInterruptedIsKept(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)
		s.World.Fail(1)
		s.Refresh()

		run := only(t, s).Runs[0].ID
		pushed := sim.Commit(1, 2)

		deliver := s.Runner.FinishLate(run, autopilot.RunEnd{Commit: pushed, Cost: new(2.0)})
		s.Advance(3*time.Minute, false)
		require.Equal(t, autopilot.BlockInterrupted, only(t, s).BlockedBy, "two checks found it gone")

		deliver()
		s.Settle()

		m := only(t, s)
		assert.Equal(t, autopilot.StateVerifying, m.State, "its end moves it on: %s", m.Words)
		assert.Equal(t, string(autopilot.RunDone), m.Runs[0].Outcome)
		assert.Equal(t, pushed, m.Runs[0].Commit)
		require.NotNil(t, m.Runs[0].Cost)
		assert.InDelta(t, 2.0, *m.Runs[0].Cost, 0.001)
	})
}

func TestARunThatEndsStoppedAfterAFailedStopClosesForWhatTheStopWasFor(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)
		s.World.Fail(1)
		s.Refresh()

		run := only(t, s).Runs[0].ID

		// It merges, and the stop for it fails.
		s.Runner.FailStops(1)
		s.World.Merge(1)
		s.Refresh()
		s.Settle()

		assert.Empty(t, s.Runner.Stopped(), "a failed stop is tried again a tick later, never at once")
		assert.Equal(t, []int64{run}, s.Runner.Going())

		// Before that, the run ends stopped anyway.
		s.Runner.Finish(run, autopilot.RunEnd{Outcome: autopilot.RunStopped})
		s.Settle()

		m := only(t, s)
		assert.Equal(t, autopilot.StateClosed, m.State, m.Words)
		assert.Equal(t, autopilot.ReasonMerged, m.Reason, "closed for the merge, not as the user's stop")
	})
}
