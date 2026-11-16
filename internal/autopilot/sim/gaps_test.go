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

// Gaps, truncated searches, and changes made in the other copy of the app.

func TestACatchUpBeginsOnWakingBeforeAnyRefresh(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.World.Add(sim.Item{Number: 2, Watched: true, Checks: passing, Passed: 4})
		s.Begin()

		sp := sim.FixFailing()
		sp.Limits.RunsPerDay = new(1)
		r := s.Save("Fix failing", sp, autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()

		s.World.Fail(1)
		s.World.Fail(2)
		s.Refresh()
		require.Equal(t, []string{"queued:cap_runs", running}, tally(s))

		s.Runner.Finish(s.Runner.Going()[0], autopilot.RunEnd{})
		s.Settle()

		// Three days asleep; on waking, the ticks run before any refresh does. The new day
		// frees the routine's run, but what it was decided on is three days old.
		s.Sleep(72 * time.Hour)
		s.Advance(2*time.Minute, false)

		assert.Len(t, s.Runner.Started(), 1, "nothing starts on what was read before the gap")
		assert.Contains(t, tally(s), "waiting:catch_up")
	})
}

func TestARoutineWhoseSearchKeepsFailingLeavesTheOthersToCatchUp(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.Begin()

		labeled := sim.FixFailing()
		labeled.Watch.Sources[0].Search.Labels = []string{"dependencies"}
		fix := s.Save("Fix labeled", labeled, autopilotprefs.ModeAuto)
		s.Enable(fix.ID, autopilot.CurrentRun)

		rebase := s.Save("Rebase", sim.Rebases(), autopilotprefs.ModeAuto)
		s.Enable(rebase.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()
		require.Len(t, s.GitHub.Jobs(), 1)

		// After a long gap, the labeled routine's search keeps failing.
		s.Sleep(72 * time.Hour)
		s.World.FailSearchesWith("dependencies")
		s.Refresh()

		// The rebase routine read its own search after the gap: what changes next runs.
		s.World.Add(sim.Item{Number: 2, Watched: true, Checks: passing, Passed: 4})
		s.Refresh()
		s.Refresh()
		assert.Len(t, s.GitHub.Jobs(), 2, "the other routine caught up")

		// The labeled routine still asks, and says why.
		s.World.Fail(1)
		s.Refresh()
		assert.Empty(t, s.Runner.Started())

		var asked []autopilot.Match

		for _, m := range s.Matches() {
			if m.RoutineID == fix.ID {
				asked = append(asked, m)
			}
		}

		require.Len(t, asked, 1)
		assert.Equal(t, autopilot.BlockCatchUp, asked[0].BlockedBy)
		assert.Contains(t, asked[0].Words, "Its search is failing")
	})
}

func TestAnItemPastATruncatedSearchIsStillReadForItsState(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Page = 2
		s.World.Add(sim.Item{Number: 1, Watched: true})
		s.World.Add(sim.Item{Number: 2, Watched: true})
		s.Begin()

		r := s.Save("Rebase", sim.Rebases(), autopilotprefs.ModeAsk)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()
		require.Equal(t, []string{"waiting:ask", "waiting:ask"}, tally(s))

		// Two more appear, so the search answers in part from now on; #1 merges.
		s.Advance(time.Minute, false)
		s.World.Add(sim.Item{Number: 3, Watched: true})
		s.World.Add(sim.Item{Number: 4, Watched: true})
		s.World.Merge(1)
		s.Refresh()

		// A week later, the inbox drops the tail it kept: #1 is out of the watch.
		s.Sleep(7*24*time.Hour + time.Hour)
		s.Refresh()
		s.Advance(2*time.Minute, true)

		var merged []autopilot.Match

		for _, m := range s.Matches() {
			if m.Item.Number == 1 {
				merged = append(merged, m)
			}
		}

		require.Len(t, merged, 1)
		assert.Equal(t, autopilot.StateClosed, merged[0].State, merged[0].Words)
		assert.Equal(t, autopilot.ReasonMerged, merged[0].Reason)
	})
}

func TestTheFirstPassIsCappedWhenTheOtherCopySwitchesTheRoutineOffAndOn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true})
		s.Begin()

		r := s.Save("Rebase", sim.Rebases(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()
		require.Len(t, s.GitHub.Jobs(), 1)

		other := s.Copy()
		other.Begin()
		s.Advance(time.Minute, false)

		// Off and on again in the other copy, between two cycles of this one.
		_, err := other.Service.SetEnabled(t.Context(), r.ID, false, autopilot.CurrentRun)
		require.NoError(t, err)

		for n := 2; n <= autopilot.FirstCap+3; n++ {
			s.World.Add(sim.Item{Number: n, Watched: true})
		}

		_, err = other.Service.SetEnabled(t.Context(), r.ID, true, autopilot.CurrentRun)
		require.NoError(t, err)

		s.Refresh()
		s.Refresh()

		assert.Len(t, s.GitHub.Jobs(), 1, "switched back on, its first pass is capped")
		assert.Equal(t, autopilot.FirstCap+2, backlogged(s))
	})
}

func TestOnlyFromNowOnIsKeptWhereTheQuestionDoesntApply(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true})
		s.Begin()
		s.Refresh()

		r := s.Save("Pin bumps", sim.Pins(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)

		other := s.Copy()
		other.Begin()

		ctx := t.Context()

		renamed, err := other.Service.Save(ctx, autopilot.Draft{
			ID: r.ID, Name: "Pin every bump", Spec: r.Spec, Mode: r.Mode, Current: autopilot.CurrentFromNow,
		})
		require.NoError(t, err, "a save that changes no Watch, When, or If asks nothing")
		assert.Equal(t, "Pin every bump", renamed.Name)

		changed := r.Spec
		changed.Watch.Sources[0].Search.Labels = []string{"dependencies"}
		_, err = other.Service.Save(ctx, autopilot.Draft{
			ID: r.ID, Name: renamed.Name, Spec: changed, Mode: r.Mode, Current: autopilot.CurrentFromNow,
		})
		require.ErrorIs(t, err, autopilot.ErrFromNow, "a changed watch asks")

		_, err = other.Service.SetMode(ctx, r.ID, autopilotprefs.ModeDryRun, autopilot.CurrentFromNow)
		require.NoError(t, err, "going into Dry run asks nothing")

		off, err := other.Service.SetEnabled(ctx, r.ID, false, autopilot.CurrentFromNow)
		require.NoError(t, err, "switching it off asks nothing")
		assert.False(t, off.Enabled)
	})
}
