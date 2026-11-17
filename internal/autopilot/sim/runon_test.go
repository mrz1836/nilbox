package sim_test

import (
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/sim"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// Run on the matches now: from Test, once the user confirmed the list, the routine runs on
// what it matches now and would run on, each with the user's OK.

func TestRunOnTheMatchesNowRunsOnWhatTestOffers(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: "FAILURE", Failed: 1, Passed: 3})
		s.World.Add(sim.Item{Number: 2, Watched: true, Checks: "FAILURE", Failed: 1, Passed: 3})
		s.World.Add(sim.Item{Number: 3, Watched: true, Checks: passing, Passed: 4})
		s.Begin()

		// Switched on for what changes from now on, it records the two failing as handled.
		r := s.Save("Fix failing", sim.FixFailing(), autopilotprefs.ModeAsk)
		s.Enable(r.ID, autopilot.CurrentFromNow)
		s.Refresh()
		s.Refresh()
		require.Equal(t, []string{"baseline:from_now", "baseline:from_now"}, tally(s))
		require.Empty(t, s.Runner.Started())

		// Test offers to run on the two; the one passing doesn't match.
		res, err := s.Service.Test(t.Context(), autopilot.Draft{ID: r.ID, Name: r.Name, Spec: r.Spec, Mode: r.Mode})
		require.NoError(t, err)

		offered := map[int]bool{}
		for _, it := range res.Items {
			offered[it.Number] = it.RunsNow
		}

		assert.Equal(t, map[int]bool{1: true, 2: true, 3: false}, offered)

		// The user confirms the list: both start with their OK, though the routine asks first.
		ids := []int64{s.ItemID(1), s.ItemID(2), s.ItemID(3)}

		ran, err := s.Service.RunOnMatches(t.Context(), r.ID, ids)
		require.NoError(t, err)
		s.Settle()

		assert.Len(t, ran.Matches, 2)
		assert.Equal(t, 1, ran.Skipped)
		assert.Equal(t, "Running on 2 items. 1 no longer matches, or has a record already.", ran.Words)
		assert.Len(t, s.Runner.Started(), 2, "with the user's OK, Ask first doesn't ask again")

		for _, m := range s.Matches() {
			assert.True(t, m.Approved, m.Item.Number)
			assert.False(t, m.Manual, "each is the routine's own match for its occurrence")
		}

		// Asked again, it runs on nothing: each occurrence has its record now.
		again, err := s.Service.RunOnMatches(t.Context(), r.ID, ids)
		require.NoError(t, err)
		assert.Empty(t, again.Matches)
		assert.Equal(t, 3, again.Skipped)
		assert.Equal(t, "It runs on none of them. 3 no longer match, or have a record already.", again.Words)

		// The routine's own cycles add nothing to what it ran.
		s.Refresh()
		assert.Len(t, s.Matches(), 2)
	})
}

func TestRunOnTheMatchesNowRefusesARoutineThatIsOff(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.Begin()

		r := s.Save("Pin every bump", sim.Pins(), autopilotprefs.ModeAuto)
		s.Refresh()

		_, err := s.Service.RunOnMatches(t.Context(), r.ID, []int64{1})
		require.ErrorIs(t, err, autopilot.ErrRoutineOff)
		assert.Empty(t, s.Matches())

		_, err = s.Service.RunOnMatches(t.Context(), r.ID, make([]int64, 201))
		require.ErrorIs(t, err, autopilot.ErrTooMany)
	})
}
