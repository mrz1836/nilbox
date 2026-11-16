package sim_test

import (
	"strconv"
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

func TestACatchUpAfterALongGapAsksFirst(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)

		// The app is closed for three days; the pull request fails meanwhile.
		s.Crash()
		s.Sleep(3 * 24 * time.Hour)
		s.World.Fail(1)
		s.Begin()
		s.Refresh()
		s.Refresh()

		m := only(t, s)
		assert.Equal(t, row(1, autopilot.StateWaiting, autopilot.BlockCatchUp, "", failedKey(1)), s.Ledger()[0])
		assert.Empty(t, s.Runner.Started(), "nothing catches up on its own")

		_, err := s.Service.Approve(t.Context(), []int64{m.ID})
		require.NoError(t, err)
		s.Refresh()
		assert.Len(t, s.Runner.Started(), 1, "it runs once the user says so")
	})
}

func TestAnOvernightGapCatchesUpOnItsOwn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)

		s.Crash()
		s.Sleep(12 * time.Hour)
		s.World.Fail(1)
		s.Begin()
		s.Refresh()

		assert.Len(t, s.Runner.Started(), 1)
	})
}

func TestPausedOverAWeekendResumeAsksBeforeItCatchesUp(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)
		s.Change(func(c *autopilotprefs.Config) { c.Paused = true })

		s.World.Fail(1)
		s.Refresh()
		assert.Equal(t, row(1, autopilot.StateQueued, autopilot.BlockPaused, "", failedKey(1)), s.Ledger()[0])

		s.Sleep(60 * time.Hour)
		s.Refresh()
		s.Change(func(c *autopilotprefs.Config) { c.Paused = false })

		m := only(t, s)
		assert.Equal(t, autopilot.BlockCatchUp, m.BlockedBy, "after a long pause, Resume asks first")

		_, err := s.Service.Approve(t.Context(), []int64{m.ID})
		require.NoError(t, err)
		s.Refresh()
		assert.Equal(t, autopilot.StateRunning, only(t, s).State, "then the match runs")
	})
}

func TestOverTheDailyCapLateAtNightTheRunStartsAfterMidnightOnce(t *testing.T) {
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

		// 23:30: the first run spends $4 of the $5 cap.
		s.Sleep(14*time.Hour + 30*time.Minute)
		s.World.Fail(1)
		s.Refresh()

		first := s.Matches()[0]
		s.Runner.Finish(first.Runs[0].ID, autopilot.RunEnd{Cost: new(4.0)})
		s.Settle()

		// 23:50: the second would go over it.
		s.Advance(20*time.Minute, true)
		s.World.Fail(2)
		s.Refresh()
		assert.Equal(t, map[int]string{1: "done:ran", 2: "queued:cap_spend"}, states(s))

		s.Advance(30*time.Minute, true)

		assert.Equal(t, map[int]string{1: "done:ran", 2: running}, states(s), "after midnight it starts")
		assert.Len(t, s.Runner.Started(), 2, "once")
	})
}

func TestARoutineEditedWhileAMatchWaits(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, r := started(t, sim.FixFailing(), autopilotprefs.ModeAsk)
		s.World.Fail(1)
		s.Refresh()
		assert.Equal(t, autopilot.BlockAsk, only(t, s).BlockedBy)

		// Its steps change: the waiting match takes the new ones.
		edited := r.Spec
		edited.Steps = append(edited.Steps, spec0Tell())
		_, err := s.Service.Save(t.Context(), autopilot.Draft{ID: r.ID, Name: r.Name, Spec: edited})
		require.NoError(t, err)
		s.Settle()

		m := only(t, s)
		assert.Equal(t, autopilot.StateWaiting, m.State)
		assert.Len(t, m.Steps, 3, "it takes the new settings")

		// Its If changes so it no longer matches: the match closes.
		edited.Conditions.Match = "all"
		edited.Conditions.Of = append(edited.Conditions.Of, notDependabot())
		_, err = s.Service.Save(t.Context(), autopilot.Draft{ID: r.ID, Name: r.Name, Spec: edited})
		require.NoError(t, err)
		s.Refresh()

		assert.Equal(t, []sim.Row{row(1, autopilot.StateClosed, "", autopilot.ReasonRoutineChanged, failedKey(1))}, s.Ledger())
	})
}

func TestADoubleApproveAndAnApproveRacingACycleStartOneRun(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAsk)
		s.World.Fail(1)
		s.Refresh()

		id := only(t, s).ID
		done := make(chan struct{})

		go func() {
			defer close(done)

			_, _ = s.Service.Approve(t.Context(), []int64{id})
		}()

		_, err := s.Service.Approve(t.Context(), []int64{id})
		require.NoError(t, err)

		s.Engine.Wake()
		<-done
		s.Refresh()

		_, err = s.Service.Approve(t.Context(), []int64{id})
		require.NoError(t, err)
		s.Refresh()

		assert.Len(t, s.Runner.Started(), 1, "one run")
		assert.Equal(t, autopilot.StateRunning, only(t, s).State)
	})
}

func TestASearchThatFailsForAnHourClosesNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAsk)
		s.World.Fail(1)
		s.Refresh()

		s.World.FailSearches(true)
		s.Advance(time.Hour, true)

		assert.Equal(t, []sim.Row{row(1, autopilot.StateWaiting, autopilot.BlockAsk, "", failedKey(1))}, s.Ledger())

		status, err := s.Service.Engine(t.Context())
		require.NoError(t, err)
		require.Len(t, status.Problems, 1, "the problem is recorded once")
		assert.Equal(t, autopilot.ProblemSearchFailed, status.Problems[0].Kind)

		s.World.FailSearches(false)
		s.Refresh()

		status, err = s.Service.Engine(t.Context())
		require.NoError(t, err)
		assert.Empty(t, status.Problems, "and gone once the search answers")
	})
}

func TestTheItemMergedWhileItsMatchWaitsAndWhileItRuns(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.World.Add(sim.Item{Number: 2, Watched: true, Checks: passing, Passed: 4})
		s.Begin()
		s.Change(func(c *autopilotprefs.Config) { c.AgentsAtOnce = 1 })

		r := s.Save("Fix failing", sim.FixFailing(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.World.Fail(1)
		s.World.Fail(2)
		s.Refresh()
		s.Refresh()
		require.Equal(t, []string{"queued:slot", running}, tally(s))

		s.World.Merge(1)
		s.World.Merge(2)
		s.Refresh()
		s.Refresh()

		assert.Equal(t, map[int]string{1: "closed:merged", 2: "closed:merged"}, states(s))
		assert.Len(t, s.Runner.Stopped(), 1, "the running agent was stopped")
		assert.Empty(t, s.Runner.Going())
	})
}

func TestRetentionOnALongLivedItemNeverFiresItAgain(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.Pins(), autopilotprefs.ModeAuto)
		s.World.Add(sim.Item{Number: 2, Watched: true})
		s.Refresh()
		s.Change(func(c *autopilotprefs.Config) { c.HistoryDays, c.LogDays = 7, 7 })
		require.Len(t, s.Ledger(), 2)

		s.World.Merge(2)
		s.Refresh()

		for range 10 {
			s.Sleep(24 * time.Hour)
			s.Refresh()
		}

		assert.Equal(t, []sim.Row{row(1, autopilot.StateDone, "", autopilot.ReasonRan, "appears:item")}, s.Ledger(),
			"the merged item's record goes, the open one's stays")
		assert.Len(t, s.Triage.Calls(), 2, "nothing fired again")

		cleared, err := s.Service.ClearHistory(t.Context())
		require.NoError(t, err)
		assert.Equal(t, autopilot.Cleared{Kept: 1}, cleared)
		s.Refresh()
		assert.Len(t, s.Ledger(), 1, "Clear history keeps what blocks a fire key")
		assert.Len(t, s.Triage.Calls(), 2)
	})
}

func TestTwoCopiesOnOneDataFolderRunOneEngine(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAsk)

		other := s.Copy()
		other.Begin()
		other.Refresh()

		status, err := other.Service.Engine(t.Context())
		require.NoError(t, err)
		assert.True(t, status.Elsewhere, "the second copy says another runs Autopilot")
		assert.False(t, status.Holds)

		s.World.Fail(1)
		s.Refresh()
		other.Refresh()
		require.Len(t, s.Ledger(), 1, "one engine recorded the match, once")

		_, err = other.Service.Approve(t.Context(), []int64{only(t, s).ID})
		require.ErrorIs(t, err, autopilot.ErrElsewhere)

		// The first quits, as a restart to update's old process does: the other takes over
		// within a minute.
		s.Stop()
		other.Advance(time.Minute, false)

		status, err = other.Service.Engine(t.Context())
		require.NoError(t, err)
		assert.True(t, status.Holds, "the new copy has the engine within a minute")

		_, err = other.Service.Approve(t.Context(), []int64{only(t, other).ID})
		require.NoError(t, err)
		other.Refresh()
		assert.Len(t, other.Runner.Started(), 1)
	})
}

func TestAPushToTheRunsOwnCommitGoesToVerifying(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)
		s.World.Fail(1)
		s.Refresh()

		fix := sim.Commit(1, 2)
		s.World.Push(1, fix)
		s.Runner.Finish(only(t, s).Runs[0].ID, autopilot.RunEnd{Commit: fix, Head: fix})
		s.Refresh()
		s.Refresh()

		m := only(t, s)
		assert.Equal(t, autopilot.StateVerifying, m.State, "the head moved to its own commit: verifying, never moved on")
		assert.Equal(t, fix, m.Runs[0].Commit)

		// Its own commit fails: New commits' loop guard and Checks fail's key would move on,
		// but a verifying match keeps its place.
		s.World.Fail(1)
		s.Refresh()
		assert.Equal(t, autopilot.StateVerifying, s.Matches()[0].State)
	})
}

func TestANewCommitFromSomeoneElseMidRunLetsTheRunCarryOn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)
		s.World.Fail(1)
		s.Refresh()

		s.World.Push(1, sim.Commit(1, 9))
		s.World.Fail(1)
		s.Refresh()
		s.Refresh()

		assert.Equal(t, autopilot.StateRunning, s.Matches()[0].State, "its row isn't closed")
		assert.Len(t, s.Runner.Going(), 1, "the run carries on")
		assert.Len(t, s.Runner.Started(), 1)
	})
}

func TestAFailedCheckRunAgainThatPassesClosesTheMatchAndAFailAgainReopensIt(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAsk)
		s.World.Fail(1)
		s.Refresh()

		s.World.Pass(1)
		s.Refresh()
		assert.Equal(t, []sim.Row{row(1, autopilot.StateClosed, "", autopilot.ReasonNoLongerMatches, failedKey(1))}, s.Ledger())

		s.World.Fail(1)
		s.Refresh()
		assert.Equal(t, []sim.Row{row(1, autopilot.StateWaiting, autopilot.BlockAsk, "", failedKey(1))}, s.Ledger(),
			"the same commit failing again reopens it")
	})
}

func TestAnItemPushedPastATruncatedSearchKeepsItsMatch(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAsk)
		s.World.Fail(1)
		s.Refresh()

		s.World.Page = 2
		s.World.Add(sim.Item{Number: 2, Watched: true})
		s.World.Add(sim.Item{Number: 3, Watched: true})
		s.Refresh()
		s.Advance(time.Hour, true)

		assert.Equal(t, []sim.Row{row(1, autopilot.StateWaiting, autopilot.BlockAsk, "", failedKey(1))}, s.Ledger(),
			"past the search's page, it is stale, not gone")
	})
}

func TestStopBeforeTheFirstRefreshOfflineStopsTheRun(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)
		s.World.Fail(1)
		s.Refresh()

		run := only(t, s).Runs[0].ID

		// The app crashes, and starts an hour later with no connection.
		s.World.Offline(true)
		s.Crash()
		s.Sleep(time.Hour)
		s.Begin()
		s.Refresh()

		require.NoError(t, s.Service.StopRun(t.Context(), run))
		s.Settle()

		assert.Equal(t, []int64{run}, s.Runner.Stopped())
		assert.Equal(t, []sim.Row{row(1, autopilot.StateDismissed, "", autopilot.ReasonYouStopped, failedKey(1))}, s.Ledger())

		status, err := s.Service.Engine(t.Context())
		require.NoError(t, err)
		assert.False(t, status.Primed, "no refresh came")
	})
}

func TestApproveMidCycleWhileAStartWaitsOnTheNetworkIsAppliedWithinASecond(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.World.Add(sim.Item{Number: 2, Watched: true, Checks: passing, Passed: 4})
		s.Begin()

		auto := s.Save("Fix now", sim.FixFailing(), autopilotprefs.ModeAuto)
		ask := sim.FixFailing()
		ask.Conditions = spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{numberIs(2)}}
		asks := s.Save("Fix when asked", ask, autopilotprefs.ModeAsk)

		auto.Spec.Conditions = spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{numberIs(1)}}
		_, err := s.Service.Save(t.Context(), autopilot.Draft{ID: auto.ID, Name: auto.Name, Spec: auto.Spec})
		require.NoError(t, err)
		s.Enable(auto.ID, autopilot.CurrentRun)
		s.Enable(asks.ID, autopilot.CurrentRun)
		s.Refresh()

		// #1's start waits on GitHub, its read held.
		s.World.Hold(1)
		s.World.Fail(1)
		s.World.Fail(2)
		s.Refresh()

		var waiting autopilot.Match

		for _, m := range s.Matches() {
			if m.Item.Number == 2 {
				waiting = m
			}
		}

		require.Equal(t, autopilot.BlockAsk, waiting.BlockedBy)

		before := time.Now()
		_, err = s.Service.Approve(t.Context(), []int64{waiting.ID})
		require.NoError(t, err)
		assert.Less(t, time.Since(before), time.Second, "applied at once")

		got, err := s.Service.ItemHistory(t.Context(), waiting.Item.ID)
		require.NoError(t, err)
		require.Len(t, got.Matches, 1)
		assert.True(t, got.Matches[0].Approved)

		s.World.Release(1)
		s.Refresh()
		assert.Len(t, s.Runner.Started(), 2)
	})
}

// spec0Tell is a Tell me step.
func spec0Tell() spec.Step { return spec.Step{TellMe: &spec.TellStep{}} }

// notDependabot is a condition no Dependabot pull request meets.
func notDependabot() spec.Clause {
	return spec.Clause{Field: catalog.FieldAuthor, Op: "isNot", Value: "dependabot"}
}

// numberIs is a condition on the item's title naming its number.
func numberIs(n int) spec.Clause {
	return spec.Clause{Field: catalog.FieldTitle, Op: "contains", Value: "1.0." + strconv.Itoa(n)}
}
