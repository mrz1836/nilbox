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
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestARoutineSwitchedOnThatNoLongerLoadsIsToldAsAProblemOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.Begin()

		r := s.Save("Pin new ones", sim.Pins(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		require.Empty(t, s.Notices.Told())

		// A newer build, in another copy of the app on these data, rewrote it.
		testutil.Exec(t, s.DB, `UPDATE autopilot_routines SET spec = ? WHERE id = ?`,
			`{"version":99,"steps":[{"teleport":{}}]}`, r.ID)
		s.Engine.Wake()
		s.Advance(30*time.Minute, true)

		told := s.Notices.Told()
		require.Len(t, told, 1, "told once, however long it stays so")
		assert.Equal(t, autopilot.NoticeProblem, told[0].Kind)
		assert.Equal(t, r.ID, told[0].RoutineID, "it opens its routine")
		assert.Equal(t, "Pin new ones", told[0].Title, "named, though it won't load")
		assert.Contains(t, told[0].Body, "newer")
		assert.True(t, r.CreatedAt.Equal(told[0].CreatedAt))

		status, err := s.Service.Engine(t.Context())
		require.NoError(t, err)
		require.Len(t, status.Problems, 1)
		assert.Equal(t, autopilot.ProblemWontLoad, status.Problems[0].Kind)
		assert.True(t, status.Problems[0].Told)
	})
}
