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

// Autopilot (beta): off, the engine runs nothing; turned on, it starts as a start does;
// turned off, it starts nothing new and kills nothing.

// turn turns Autopilot (beta) on or off, as its setter does.
func turn(s *sim.Sim, on bool) {
	s.Change(func(c *autopilotprefs.Config) { c.Enabled = on })
}

// lockFree reports whether another copy could take the engine's lock now.
func lockFree(t *testing.T, s *sim.Sim) bool {
	t.Helper()

	other := autopilot.NewLock(s.Lock)

	held, err := other.TryLock()
	require.NoError(t, err)
	other.Unlock()

	return held
}

func TestAutopilotOffSendsNoSearchTakesNoLockAndRunsNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		turn(s, false)
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.Begin()

		// A routine made and switched on while Autopilot is off is stored, and nothing more.
		r := s.Save("Fix failing", sim.FixFailing(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.World.Fail(1)
		s.Refresh()
		s.Advance(10*time.Minute, true)

		assert.False(t, s.Engine.Enabled())
		assert.Zero(t, s.World.Searches(), "no routine search goes to GitHub")
		assert.Zero(t, s.World.Reads(1), "nor any read of what it watches")
		assert.Empty(t, s.Runner.Started(), "nothing starts")
		assert.Empty(t, s.Ledger(), "nothing is recorded")
		assert.True(t, lockFree(t, s), "the lock is free")
		assert.Empty(t, autopilot.NewSearches(s.Service).Current().Searches, "the inbox is handed no search")

		status, err := s.Service.Engine(t.Context())
		require.NoError(t, err)
		assert.False(t, status.Holds, "this copy runs nothing")
		assert.False(t, status.Elsewhere, "and no other copy does")

		_, err = s.Service.Approve(t.Context(), []int64{1})
		require.ErrorIs(t, err, autopilot.ErrOff, "a command is refused")

		_, err = s.Service.Test(t.Context(), autopilot.Draft{Name: r.Name, Spec: r.Spec})
		require.ErrorIs(t, err, autopilot.ErrOff, "so is a test, which would ask GitHub")
	})
}

func TestTurningAutopilotOnStartsItAsAStartDoes(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		turn(s, false)
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.Begin()

		r := s.Save("Fix failing", sim.FixFailing(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.World.Fail(1)
		s.Refresh()
		require.Empty(t, s.Runner.Started())

		// Turned on, it takes the lock and reads the routines' searches at once, without
		// waiting for the next refresh, then acts on what they return.
		turn(s, true)

		assert.True(t, s.Engine.Enabled())
		assert.False(t, lockFree(t, s), "it holds the lock")
		assert.Positive(t, s.World.Searches(), "it read the routines' searches")
		assert.Len(t, s.Runner.Started(), 1, "and started what matches")
		assert.Equal(t, []string{running}, tally(s))

		_, err := s.Service.Skip(t.Context(), only(t, s).ID)
		require.Error(t, err, "a running match can't be skipped")
		require.NotErrorIs(t, err, autopilot.ErrOff, "but the command is applied")
	})
}

func TestTurningAutopilotOffKillsNothingStartsNothingAndLetsGoOnceItsRunEnds(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.World.Add(sim.Item{Number: 2, Watched: true, Checks: passing, Passed: 4})
		s.Begin()

		r := s.Save("Fix failing", sim.FixFailing(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.World.Fail(1)
		s.Refresh()
		require.Len(t, s.Runner.Going(), 1)

		turn(s, false)
		sent := s.World.Searches()

		// The run carries on, nothing new starts, and no search goes out.
		s.World.Fail(2)
		s.Refresh()
		s.Advance(10*time.Minute, true)

		assert.Empty(t, s.Runner.StopsAsked(), "nothing is stopped")
		assert.Len(t, s.Runner.Going(), 1, "the run goes on")
		assert.Len(t, s.Runner.Started(), 1, "nothing new starts")
		assert.Equal(t, sent, s.World.Searches(), "no routine search goes out")
		assert.False(t, lockFree(t, s), "the lock is held while its run goes")

		// Its end is recorded, and then the lock goes.
		s.Runner.Finish(s.Runner.Going()[0], autopilot.RunEnd{})
		s.Settle()
		s.Advance(2*time.Minute, false)

		assert.True(t, lockFree(t, s), "the lock is free once the run ended")

		m := only(t, s)
		require.Len(t, m.Runs, 1)
		assert.Equal(t, "ended", m.Runs[0].Status, "the run's end is recorded")
		assert.Equal(t, autopilot.StateQueued, m.State, "and its match waits for its next step, which nothing starts")

		// Turned on again, everything goes on as it was, and what matched meanwhile runs.
		turn(s, true)
		s.Refresh()

		assert.Len(t, s.Runner.Started(), 2, "#2 starts once it is on again")
		assert.Equal(t, autopilot.StateDone, s.Matches()[0].State, "#1's match moves on")
		assert.False(t, lockFree(t, s))
	})
}

func TestTurningAutopilotOnAfterALongTimeOffCatchesUp(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)
		turn(s, false)

		// Four days off, the inbox refreshing all along, its refreshes never a day apart:
		// the gap is the engine's own, from the last refresh it saw.
		for range 4 {
			s.Sleep(23 * time.Hour)
			s.Refresh()
		}

		s.World.Fail(1)
		s.Refresh()

		turn(s, true)
		s.Refresh()

		assert.Empty(t, s.Runner.Started(), "nothing starts on what matched while it was off")
		assert.Equal(t, []string{"waiting:catch_up"}, tally(s), "it asks first, as after a long gap")
	})
}
