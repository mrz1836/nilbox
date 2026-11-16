package sim_test

import (
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/sim"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// errCantSay is a runner that can't say whether a run goes.
var errCantSay = errors.New("the runner can't say")

// running is a match whose run goes, as tally says it.
const running = "running:"

func TestALongGapWithTheAppOpenAsksFirstUntilTheSearchesAreReadAgain(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)
		s.World.Add(sim.Item{Number: 2, Watched: true, Checks: passing, Passed: 4})
		s.Refresh()

		// The Mac sleeps three days with the app open. On waking, the first refresh's
		// searches fail, so what the engine sees of them is from before the gap.
		s.Sleep(72 * time.Hour)
		s.World.Fail(1)
		s.World.FailSearches(true)
		s.Refresh()
		assert.Empty(t, s.Runner.Started())

		s.World.FailSearches(false)
		s.Refresh()

		m := only(t, s)
		assert.Equal(t, autopilot.BlockCatchUp, m.BlockedBy, "the first reads after the gap still ask")
		assert.Empty(t, s.Runner.Started())

		// Once the searches were read after the gap, what changes next runs on its own.
		s.World.Fail(2)
		s.Refresh()
		assert.Len(t, s.Runner.Started(), 1, "the catch-up ended with the first full read after the gap")
		assert.Equal(t, autopilot.BlockCatchUp, s.Matches()[0].BlockedBy, "the match from the catch-up still waits")
	})
}

func TestAMatchThatRanNeverRunsAgainWhenItsRoutineIsSwitchedOffAndOn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		sp := sim.FixFailing()
		sp.Steps = []spec.Step{{Agent: &spec.AgentStep{Prompt: spec.PromptRef{Starter: "fix-checks"}}}, {Pin: &spec.Empty{}}}
		s, r := started(t, sp, autopilotprefs.ModeAuto)
		s.World.Fail(1)
		s.Refresh()

		run := only(t, s).Runs[0].ID

		_, err := s.Service.SetEnabled(t.Context(), r.ID, false, autopilot.CurrentRun)
		require.NoError(t, err)
		s.Runner.Finish(run, autopilot.RunEnd{})
		s.Settle()
		require.Equal(t, autopilot.ReasonRoutineOff, only(t, s).Reason, "switched off with its pin still to run")

		asked, err := s.Service.Matching(t.Context(), autopilot.Draft{ID: r.ID, Name: r.Name, Spec: r.Spec})
		require.NoError(t, err)
		assert.Zero(t, asked.Count, "a match that ran is a record")

		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()

		assert.Len(t, s.Runner.Started(), 1, "the same commit never runs twice")
		assert.Equal(t, autopilot.StateClosed, only(t, s).State)
	})
}

// backlogged counts the matches waiting as a backlog.
func backlogged(s *sim.Sim) int {
	var n int

	for _, m := range s.Matches() {
		if m.BlockedBy == autopilot.BlockBacklog {
			n++
		}
	}

	return n
}

func TestTheFirstPassIsCappedOnLeavingDryRun(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		for n := 1; n <= autopilot.FirstCap+5; n++ {
			s.World.Add(sim.Item{Number: n, Watched: true})
		}

		s.Begin()

		r := s.Save("Rebase", sim.Rebases(), autopilotprefs.ModeDryRun)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()

		_, err := s.Service.SetMode(t.Context(), r.ID, autopilotprefs.ModeAuto, autopilot.CurrentRun)
		require.NoError(t, err)
		s.Refresh()
		s.Refresh()

		assert.Empty(t, s.GitHub.Jobs(), "over the cap it queues none")
		assert.Equal(t, autopilot.FirstCap+5, backlogged(s), "they wait as a backlog")
	})
}

func TestTheFirstPassIsCappedAgainWhenTheRoutineIsSwitchedBackOn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true})
		s.Begin()

		r := s.Save("Rebase", sim.Rebases(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()
		require.Len(t, s.GitHub.Jobs(), 1, "a first pass under the cap sends")

		_, err := s.Service.SetEnabled(t.Context(), r.ID, false, autopilot.CurrentRun)
		require.NoError(t, err)
		s.Settle()

		for n := 2; n <= autopilot.FirstCap+3; n++ {
			s.World.Add(sim.Item{Number: n, Watched: true})
		}

		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()

		assert.Len(t, s.GitHub.Jobs(), 1, "switched back on, its first pass is capped again")
		assert.Equal(t, autopilot.FirstCap+2, backlogged(s))
	})
}

func TestClearHistoryKeepsTheRecordOfAnOpenItemOutOfTheWatch(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.Pins(), autopilotprefs.ModeAuto)
		require.Len(t, s.Local.Jobs(), 1)

		s.World.Change(1, func(it *sim.Item) { it.Watched = false })
		s.Refresh()
		s.Refresh()

		cleared, err := s.Service.ClearHistory(t.Context())
		require.NoError(t, err)
		assert.Equal(t, autopilot.Cleared{Kept: 1}, cleared, "it may still block its fire key")

		s.World.Change(1, func(it *sim.Item) { it.Watched = true })
		s.Refresh()
		s.Refresh()

		assert.Len(t, s.Local.Jobs(), 1, "back in the watch, it isn't pinned again")
		assert.Len(t, s.Ledger(), 1)
	})
}

func TestOnlyFromNowOnIsRefusedWhereItCantBeKept(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true})
		s.Begin()
		s.Refresh()

		r := s.Save("Pin bumps", sim.Pins(), autopilotprefs.ModeAuto)

		other := s.Copy()
		other.Begin()

		_, err := other.Service.SetEnabled(t.Context(), r.ID, true, autopilot.CurrentFromNow)
		require.ErrorIs(t, err, autopilot.ErrFromNow)
		require.ErrorIs(t, err, autopilot.ErrElsewhere)

		s.Crash()

		_, err = s.Service.SetEnabled(t.Context(), r.ID, true, autopilot.CurrentFromNow)
		require.ErrorIs(t, err, autopilot.ErrNotRunning, "before the engine runs")

		got, err := s.Service.Get(t.Context(), r.ID)
		require.NoError(t, err)
		assert.False(t, got.Enabled, "the change wasn't made with its answer dropped")
	})
}

func TestTwoCopiesNeverUndoEachOthersWatches(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.Begin()
		s.Refresh()

		// A routine switched on in the copy that doesn't run the engine is picked up by the
		// one that does, within a tick.
		other := s.Copy()
		other.Begin()

		r := other.Save("Fix failing", sim.FixFailing(), autopilotprefs.ModeAsk)
		other.Enable(r.ID, autopilot.CurrentRun)
		s.World.Fail(1)
		s.Advance(time.Minute, false)
		s.Refresh()
		s.Refresh()

		m := only(t, s)
		require.Equal(t, autopilot.BlockAsk, m.BlockedBy)

		// A routine with a search of its own, switched on in the copy that runs the engine,
		// which the other copy's snapshot doesn't have: the other copy's refreshes send no
		// search at all, so they never drop the rows of that one.
		labeled := sim.Pins()
		labeled.Watch.Sources[0].Search.Labels = []string{"dependencies"}
		pins := s.Save("Pin labeled", labeled, autopilotprefs.ModeAsk)
		s.Enable(pins.ID, autopilot.CurrentRun)
		s.Refresh()

		watched, err := s.Service.Watching(t.Context(), pins.ID)
		require.NoError(t, err)
		require.Len(t, watched, 1)

		other.Refresh()
		other.Refresh()
		s.Advance(time.Minute, false)

		watched, err = s.Service.Watching(t.Context(), pins.ID)
		require.NoError(t, err)
		assert.Len(t, watched, 1, "the other copy's refresh left the holder's rows alone")
		assert.Equal(t, autopilot.BlockAsk, s.Matches()[0].BlockedBy, "the match still waits, never closed as left")
	})
}

func TestAStartTheRunnerRefusesAsItClosesStaysQueued(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)
		s.Runner.Close()
		s.World.Fail(1)
		s.Refresh()
		s.Refresh()

		m := only(t, s)
		assert.Equal(t, autopilot.StateQueued, m.State, m.Words)
		require.NotEmpty(t, m.Runs)
		assert.Equal(t, string(autopilot.RunInterrupted), m.Runs[0].Outcome)

		s.Runner.Reopen()
		s.Restart()
		s.Refresh()

		assert.Equal(t, autopilot.StateRunning, only(t, s).State, "the next start starts it")
	})
}

func TestAStartTheQuitCutsOffStaysQueuedForTheNextStart(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)
		s.Runner.Hold()
		s.World.Fail(1)
		s.Refresh()
		s.Crash()
		s.Runner.Release()

		s.Begin()
		s.Refresh()

		m := only(t, s)
		assert.Equal(t, autopilot.StateRunning, m.State, "never interrupted: it went back to the queue, and started again")
		require.Len(t, m.Runs, 2)
		assert.Equal(t, string(autopilot.RunInterrupted), m.Runs[0].Outcome)
		assert.Len(t, s.Runner.Going(), 1)
	})
}

func TestARunItsRunnerCantReportOnIsAskedAgainAndNeverGoesOnForGood(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)
		s.World.Fail(1)
		s.Refresh()

		run := only(t, s).Runs[0].ID

		s.Runner.FailAlive(errCantSay)
		s.Restart()
		s.Advance(time.Minute, false)
		assert.Equal(t, autopilot.StateRunning, only(t, s).State, "unknown is no answer")

		// The run ended with no one told; the runner answers again.
		s.Runner.Forget(run)
		s.Runner.FailAlive(nil)
		s.Advance(time.Minute, false)
		assert.Equal(t, autopilot.BlockInterrupted, only(t, s).BlockedBy, "the next tick finds it gone")
	})
}

func TestARunUnknownPastTheLongestTimeLimitEnds(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)
		s.World.Fail(1)
		s.Refresh()

		s.Runner.FailAlive(errCantSay)
		s.Sleep(4 * time.Hour)
		s.Advance(time.Minute, false)

		assert.Equal(t, autopilot.BlockInterrupted, only(t, s).BlockedBy)
	})
}

func TestNothingStartsWhileSignedOut(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true})
		s.Begin()
		s.Change(func(c *autopilotprefs.Config) { c.Paused = true })

		r := s.Save("Pin bumps", sim.Pins(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		require.Equal(t, autopilot.BlockPaused, only(t, s).BlockedBy)

		s.World.SignOut(true)
		s.Refresh()
		s.Change(func(c *autopilotprefs.Config) { c.Paused = false })

		assert.Equal(t, autopilot.BlockSignedOut, only(t, s).BlockedBy, "a local step waits too")
		assert.Empty(t, s.Local.Jobs())

		status, err := s.Service.Engine(t.Context())
		require.NoError(t, err)
		assert.True(t, status.SignedOut)

		s.World.SignOut(false)
		s.Refresh()
		assert.Equal(t, autopilot.StateDone, only(t, s).State)
		assert.Len(t, s.Local.Jobs(), 1)
	})
}

func TestAStateReadUnderWayIsSentOnceAndAFailedStopIsTriedAgain(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)
		s.World.Fail(1)
		s.Refresh()
		require.Equal(t, autopilot.StateRunning, only(t, s).State)

		// It merges: the search drops it, and its state is read while the cycles go on.
		s.World.HoldStates()
		s.World.Merge(1)
		s.Refresh()
		s.Engine.Wake()
		s.Settle()
		s.Engine.Wake()
		s.Settle()
		assert.Equal(t, 1, s.World.StateReads(), "one read under way at a time")

		s.Runner.FailStops(1)
		s.World.ReleaseStates()
		s.Settle()
		s.Advance(time.Minute, false)
		s.Advance(time.Minute, false)

		assert.Empty(t, s.Runner.Going(), "the stop that failed was tried again")
		assert.Equal(t, autopilot.ReasonMerged, only(t, s).Reason)
	})
}

func TestARunAcrossMidnightKeepsItsReservationOnTheNewDay(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.World.Add(sim.Item{Number: 2, Watched: true, Checks: passing, Passed: 4})
		s.Begin()
		s.Change(func(c *autopilotprefs.Config) {
			c.DailyCap = autopilotprefs.DailyCap{Amount: 5, Unit: autopilotprefs.UnitUSD}
		})

		r := s.Save("Fix failing", sim.FixFailing(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()

		// 23:50: the first run starts, reserving $3, and works past midnight.
		s.Sleep(14*time.Hour + 50*time.Minute)
		s.World.Fail(1)
		s.Refresh()

		s.Sleep(15 * time.Minute)
		s.World.Fail(2)
		s.Refresh()
		assert.Equal(t, []string{"queued:cap_spend", running}, tally(s), "at 00:05 its reservation counts today")

		s.Runner.Finish(s.Matches()[0].Runs[0].ID, autopilot.RunEnd{Cost: new(3.0)})
		s.Advance(time.Minute, true)
		assert.Equal(t, []string{"done:ran", running}, tally(s), "its cost is yesterday's, so the second starts")
	})
}

func TestADayRunsFromLocalMidnightAcrossTheChangeOfClocks(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ny, err := time.LoadLocation("America/New_York")
		require.NoError(t, err)

		s := sim.New(t)
		// Saturday 31 October 2026, 23:00 local; the clocks go back at 02:00 on 1 November.
		s.Clock.Set(time.Date(2026, time.October, 31, 23, 0, 0, 0, ny))

		for n := 1; n <= 4; n++ {
			s.World.Add(sim.Item{Number: n, Watched: true, Checks: passing, Passed: 4})
		}

		s.Begin()
		// A day between two refreshes asks before catching up; this timeline is about days.
		s.Change(func(c *autopilotprefs.Config) { c.CatchUpHours = autopilotprefs.MaxCatchUpHours })

		sp := sim.FixFailing()
		sp.Limits.RunsPerDay = new(1)
		r := s.Save("Fix failing", sp, autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()

		failAndRefresh := func(n int) {
			s.World.Fail(n)
			s.Refresh()
		}

		// Each run ends once it started, so no slot holds the next.
		finishAll := func() {
			for _, id := range s.Runner.Going() {
				s.Runner.Finish(id, autopilot.RunEnd{})
			}

			s.Settle()
		}

		failAndRefresh(1)
		failAndRefresh(2)
		assert.Equal(t, []string{"queued:cap_runs", running}, tally(s), "one run on 31 October")
		finishAll()

		s.Sleep(70 * time.Minute) // 00:10 local on 1 November
		s.Refresh()
		assert.Len(t, s.Runner.Started(), 2, "the next local day starts the second")
		finishAll()

		// 23:30 EST on 1 November is already 2 November in UTC: still the same local day.
		s.Sleep(24*time.Hour + 20*time.Minute)
		failAndRefresh(3)
		assert.Len(t, s.Runner.Started(), 2, "1 November has had its run")

		s.Sleep(40 * time.Minute) // 00:10 local on 2 November
		s.Refresh()
		assert.Len(t, s.Runner.Started(), 3)
		finishAll()

		// Active hours follow local time too: Monday 08:00 to 09:00.
		active := sim.FixFailing()
		active.Conditions = spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{numberIs(4)}}
		active.Limits.ActiveHours = &spec.ActiveHours{Days: []catalog.Weekday{catalog.Monday}, From: "08:00", To: "09:00"}
		hours := s.Save("Mornings", active, autopilotprefs.ModeAuto)
		s.Enable(hours.ID, autopilot.CurrentRun)
		s.Sleep(7*time.Hour + 30*time.Minute) // 07:40 local
		failAndRefresh(4)
		assert.Len(t, s.Runner.Started(), 3, "before 08:00 local it waits")

		s.Sleep(25 * time.Minute) // 08:05 local
		s.Advance(time.Minute, true)
		assert.Len(t, s.Runner.Started(), 4)
	})
}
