package sim_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/sim"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
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

	t.Run("its end comes after the stop's failure", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s, run := stoppedForTheMerge(t, false)

			assert.Empty(t, s.Runner.Stopped(), "a failed stop is tried again a tick later, never at once")

			// Before that, the run ends stopped anyway.
			s.Runner.Finish(run, autopilot.RunEnd{Outcome: autopilot.RunStopped})
			s.Settle()

			closedForTheMerge(t, s)
		})
	})

	t.Run("its end comes before the stop's failure", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s, run := stoppedForTheMerge(t, true)

			// The run ends stopped while the stop is under way, and only then does the stop
			// fail.
			s.Runner.Finish(run, autopilot.RunEnd{Outcome: autopilot.RunStopped})
			s.Settle()

			closedForTheMerge(t, s)

			s.Runner.ReleaseStops()
			s.Settle()
			s.Advance(2*time.Minute, false)

			closedForTheMerge(t, s)
			assert.Equal(t, []int64{run}, s.Runner.StopsAsked(), "a run that ended is never stopped again")
		})
	})
}

// stoppedForTheMerge starts the agent on #1, merges it, and returns once the engine has
// asked the runner, whose next Stop fails, to stop the run for the merge: the stop has
// failed, or with hold is still under way. The run goes on.
func stoppedForTheMerge(t *testing.T, hold bool) (*sim.Sim, int64) {
	t.Helper()

	s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)
	s.World.Fail(1)
	s.Refresh()

	run := only(t, s).Runs[0].ID

	s.Runner.FailStops(1)

	if hold {
		s.Runner.HoldStops()
	}

	// The search drops #1 as it merges. The watch pass stores that after the inbox and
	// tells the engine nothing of an item that left, so the cycle the refresh woke may have
	// read #1 still watched; the next cycle, which the engine's tick would run, reads it
	// gone, reads its state, and stops the run.
	s.World.Merge(1)
	s.Refresh()
	s.Engine.Wake()
	s.Settle()

	require.Equal(t, []int64{run}, s.Runner.StopsAsked(), "the engine asked once to stop the run for the merge")
	require.Equal(t, []int64{run}, s.Runner.Going())

	return s, run
}

// closedForTheMerge checks that the one match closed for its item's merge, its run
// stopped.
func closedForTheMerge(t *testing.T, s *sim.Sim) {
	t.Helper()

	m := only(t, s)
	assert.Equal(t, autopilot.StateClosed, m.State, m.Words)
	assert.Equal(t, autopilot.ReasonMerged, m.Reason, "closed for the merge, not as the user's stop")
	require.Len(t, m.Runs, 1)
	assert.Equal(t, string(autopilot.RunStopped), m.Runs[0].Outcome)
}

func TestAStartThatNeverHappenedUsesUpNoRunOfTheDay(t *testing.T) {
	t.Parallel()

	oneADay := func() spec.Spec {
		sp := sim.FixFailing()
		sp.Limits.RunsPerDay = new(1)

		return sp
	}

	t.Run("the quit cut it off", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s, _ := started(t, oneADay(), autopilotprefs.ModeAuto)
			s.Runner.Hold()
			s.World.Fail(1)
			s.Refresh()
			s.Crash()
			s.Runner.Release()

			s.Begin()
			s.Refresh()
			s.Advance(3*time.Minute, false)

			assert.Len(t, s.Runner.Started(), 1, "the routine's one run today is still to come")
			assert.Equal(t, autopilot.StateRunning, only(t, s).State, only(t, s).Words)
		})
	})

	t.Run("the runner refused it as it closed", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			s, _ := started(t, oneADay(), autopilotprefs.ModeAuto)
			s.Runner.Close()
			s.World.Fail(1)
			s.Refresh()
			s.Refresh()
			require.Empty(t, s.Runner.Started())

			s.Runner.Reopen()
			s.Restart()
			s.Refresh()
			s.Advance(time.Minute, false)

			assert.Len(t, s.Runner.Started(), 1, "the routine's one run today is still to come")
			assert.Equal(t, autopilot.StateRunning, only(t, s).State, only(t, s).Words)
		})
	})
}

func TestARunsEndAndItsMatchsMoveAreWrittenTogether(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)
		s.World.Fail(1)
		s.Refresh()

		run := only(t, s).Runs[0].ID
		ctx := t.Context()

		// The match's move can't be written as the run's end comes.
		_, err := s.DB.ExecContext(ctx, "CREATE TRIGGER refused_move BEFORE UPDATE ON autopilot_matches "+
			"BEGIN SELECT RAISE(ABORT, 'refused by the test'); END")
		require.NoError(t, err)

		s.Runner.Finish(run, autopilot.RunEnd{Cost: new(1.0)})
		s.Settle()

		_, err = s.DB.ExecContext(ctx, "DROP TRIGGER refused_move")
		require.NoError(t, err)

		s.Advance(2*time.Minute, false)

		m := only(t, s)
		assert.Equal(t, autopilot.StateDone, m.State, "the end is written again, with the move: %s", m.Words)
		assert.Equal(t, string(autopilot.RunDone), m.Runs[0].Outcome)
		require.NotNil(t, m.Runs[0].Cost)
		assert.InDelta(t, 1.0, *m.Runs[0].Cost, 0.001)
	})
}

func TestAMatchLeftRunningOnAnEndedRunMovesOn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)
		s.World.Fail(1)
		s.Refresh()

		run := only(t, s).Runs[0].ID

		// The run's end was written and the match's move was lost, as two writes, one
		// after the other, could leave them.
		_, err := s.DB.ExecContext(t.Context(),
			"UPDATE autopilot_runs SET status = 'ended', outcome = 'done', run_commit = ? WHERE id = ?",
			sim.Commit(1, 2), run)
		require.NoError(t, err)
		s.Runner.Forget(run)

		s.Advance(2*time.Minute, false)

		m := only(t, s)
		assert.Equal(t, autopilot.StateVerifying, m.State, "it moves on by the end the ledger keeps: %s", m.Words)
	})
}

func TestAStartTheRunnerForgotIsNeverStartedAgainOnItsOwn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)
		s.Runner.HoldAnswers()
		s.World.Fail(1)
		s.Refresh()

		run := s.Runner.Started()[0].RunID

		// The runner took it, and it ended while the app was closed: the first check finds
		// it gone. Then the runner's history drops it, and it says it never started it.
		s.Crash()
		s.Runner.Release()
		s.Runner.Forget(run)
		s.Begin()

		s.Runner.Lose(run)
		s.Advance(3*time.Minute, false)

		assert.Empty(t, s.Runner.Started(), "one check said it ran: it is never started again on its own")
		assert.Equal(t, autopilot.BlockInterrupted, only(t, s).BlockedBy)
	})
}
