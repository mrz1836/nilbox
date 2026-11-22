package sim_test

import (
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

// A routine whose trigger is on its way to an item its conditions count says so before it
// runs there (Upcoming), and stops saying so once it ran, or the item no longer fits.

// pinSyncs pins each watched pull request titled "[Sync]" once its checks pass, settled.
func pinSyncs(event catalog.Event) spec.Spec {
	return spec.Spec{
		Version: spec.Version,
		Watch:   spec.Watch{Sources: []spec.Source{sim.Search()}},
		Trigger: spec.Trigger{Event: event},
		Conditions: spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{
			{Field: catalog.FieldTitle, Op: catalog.OpStartsWith, Value: "[Sync]"},
		}},
		Steps: []spec.Step{{Pin: &spec.Empty{}}},
	}
}

// checksRunning is a pull request whose checks still run, titled title.
func checksRunning(number int, title string) sim.Item {
	return sim.Item{Number: number, Title: title, Watched: true, Checks: "PENDING", Passed: 2, Pending: 2}
}

// syncsRunning starts a sim with #1, a sync, and #2, not one, both with their checks still
// running, and routine in mode switched on.
func syncsRunning(t *testing.T, sp spec.Spec, mode autopilotprefs.Mode) (*sim.Sim, autopilot.Routine) {
	t.Helper()

	s := sim.New(t)
	s.World.Add(checksRunning(1, "[Sync] Update project files"))
	s.World.Add(checksRunning(2, "[Deps] Upgrade dependencies"))
	s.Begin()

	r := s.Save("Pin syncs", sp, mode)
	s.Enable(r.ID, autopilot.CurrentRun)
	s.Refresh()

	return s, r
}

// upcoming returns the routines that will run on the item number, as the window reads them.
func upcoming(t *testing.T, s *sim.Sim, number int) []autopilot.Upcoming {
	t.Helper()

	got, err := s.Service.Upcoming(t.Context(), []int64{s.ItemID(number)})
	require.NoError(t, err)

	return got
}

func TestARoutineSaysItWillRunWhileItsTriggerIsOnItsWay(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, r := syncsRunning(t, pinSyncs(catalog.EventChecksPassed), autopilotprefs.ModeAuto)

		got := upcoming(t, s, 1)
		require.Len(t, got, 1, "the sync, its checks running")
		assert.Equal(t, r.ID, got[0].RoutineID)
		assert.Equal(t, "Pin syncs", got[0].Routine)
		assert.Equal(t, autopilotprefs.ModeAuto, got[0].Mode)
		assert.Equal(t, catalog.EventChecksPassed, got[0].Trigger)
		assert.Equal(t, "when its checks pass", got[0].When)
		assert.Nil(t, got[0].At, "GitHub says when, not the clock")
		require.Len(t, got[0].Steps, 1)
		assert.Equal(t, catalog.StepPin, got[0].Steps[0].Kind)
		assert.Empty(t, upcoming(t, s, 2), "its title rules the other out")
		assert.Empty(t, s.Matches(), "nothing ran yet")

		// Its checks pass (the world's finished an hour ago, so they have settled; the
		// evaluator's golden decisions hold the settling in between).
		s.World.Change(1, func(it *sim.Item) { it.Checks, it.Passed, it.Pending = passing, 4, 0 })
		s.Advance(3*time.Minute, true)

		require.Len(t, s.Matches(), 1, "it ran")
		assert.Equal(t, autopilot.StateDone, only(t, s).State)
		assert.Empty(t, upcoming(t, s, 1), "it says nothing of what it already ran")

		// Its checks run again on the same commit, which it ran on already: it won't run.
		s.World.Change(1, func(it *sim.Item) { it.Checks, it.Passed, it.Pending = "PENDING", 2, 2 })
		s.Refresh()
		assert.Empty(t, upcoming(t, s, 1), "it ran on this commit, so it won't run again")
	})
}

func TestARoutineThatWontRunByItselfNeverSaysItWill(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		sp   spec.Spec
		mode autopilotprefs.Mode
	}{
		{name: "a Dry run", sp: pinSyncs(catalog.EventChecksPassed), mode: autopilotprefs.ModeDryRun},
		{name: "a trigger no one can foresee", sp: pinSyncs(catalog.EventApproved), mode: autopilotprefs.ModeAuto},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				s, _ := syncsRunning(t, tt.sp, tt.mode)

				assert.Empty(t, upcoming(t, s, 1))
			})
		})
	}
}

func TestLeavingAnItemAloneTakesBackThatARoutineWillRunThere(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, r := syncsRunning(t, pinSyncs(catalog.EventChecksPassed), autopilotprefs.ModeAuto)
		require.Len(t, upcoming(t, s, 1), 1)

		require.NoError(t, s.Service.LeaveAlone(t.Context(), r.ID, s.ItemID(1)))
		assert.Empty(t, upcoming(t, s, 1), "at once")

		// Its checks pass, and settle: it leaves it alone all the same.
		s.World.Change(1, func(it *sim.Item) { it.Checks, it.Passed, it.Pending = passing, 4, 0 })
		s.Advance(3*time.Minute, true)

		assert.Empty(t, upcoming(t, s, 1))
		assert.Empty(t, s.Matches(), "it never ran there")

		page, err := s.Service.Activity(t.Context(), 0, 0)
		require.NoError(t, err)
		require.Len(t, page.LeftAlone, 1, "Activity says so, with Let it act again")
		assert.Equal(t, "You told it to leave this one alone.", page.LeftAlone[0].Why)
	})
}

func TestNothingSaysItWillRunWhilePaused(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := syncsRunning(t, pinSyncs(catalog.EventChecksPassed), autopilotprefs.ModeAuto)
		require.Len(t, upcoming(t, s, 1), 1)

		s.Change(func(c *autopilotprefs.Config) { c.Paused = true })
		assert.Empty(t, upcoming(t, s, 1))

		s.Change(func(c *autopilotprefs.Config) { c.Paused = false })
		assert.Len(t, upcoming(t, s, 1), 1)
	})
}

func TestARoutineInAskFirstSaysItAsksOnlyWhenItWould(t *testing.T) {
	t.Parallel()

	labels := pinSyncs(catalog.EventChecksPassed)
	labels.Steps = []spec.Step{{GitHub: []spec.GitHubAction{{Labels: &spec.LabelsAction{Add: []string{"synced"}}}}}}

	tests := []struct {
		name string
		sp   spec.Spec
		asks bool
	}{
		{name: "a pin runs by itself in Ask first", sp: pinSyncs(catalog.EventChecksPassed)},
		{name: "a GitHub step waits for an OK", sp: labels, asks: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				s, _ := syncsRunning(t, tt.sp, autopilotprefs.ModeAsk)

				got := upcoming(t, s, 1)
				require.Len(t, got, 1)
				assert.Equal(t, autopilotprefs.ModeAsk, got[0].Mode)
				assert.Equal(t, tt.asks, got[0].AsksFirst)
			})
		})
	}
}

func TestARoutineThatCountsOnlyPassingChecksSaysItWillRunWhileTheyRun(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		// The routine fires when the checks pass, and counts only items whose checks pass:
		// while they run, it will run, as the checks will have passed when it does.
		sp := pinSyncs(catalog.EventChecksPassed)
		sp.Conditions.Of = append(sp.Conditions.Of,
			spec.Clause{Field: catalog.FieldChecksState, Op: catalog.OpOneOf, Values: []string{"success"}})

		s, _ := syncsRunning(t, sp, autopilotprefs.ModeAuto)

		got := upcoming(t, s, 1)
		require.Len(t, got, 1)
		assert.Equal(t, "when its checks pass", got[0].When)
		assert.Empty(t, upcoming(t, s, 2), "its title still rules the other out")
	})
}
