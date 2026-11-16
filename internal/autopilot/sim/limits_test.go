package sim_test

import (
	"slices"
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

// two is a simulation with pull requests #1 and #2 in the search, each failing, and the
// flagship routine changed by edit, in Autopilot, after the refresh that matched them.
func two(t *testing.T, edit func(sp *spec.Spec), settings func(c *autopilotprefs.Config)) *sim.Sim {
	t.Helper()

	s := sim.New(t)
	s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
	s.World.Add(sim.Item{Number: 2, Watched: true, Checks: passing, Passed: 4})
	s.Begin()

	if settings != nil {
		s.Change(settings)
	}

	sp := sim.FixFailing()
	if edit != nil {
		edit(&sp)
	}

	r := s.Save("Fix failing", sp, autopilotprefs.ModeAuto)
	s.Enable(r.ID, autopilot.CurrentRun)
	s.Refresh()

	s.World.Fail(1)
	s.World.Fail(2)
	s.Refresh()
	s.Refresh()

	return s
}

// tally returns each match's state and what it waits on, sorted, whichever item each is
// for: two items that match at once start in the order the search lists them, the most
// recently updated first.
func tally(s *sim.Sim) []string {
	matches := s.Matches()

	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, string(m.State)+":"+string(m.BlockedBy)+string(m.Reason))
	}

	slices.Sort(out)

	return out
}

// states returns each match's state and what it waits on, by item.
func states(s *sim.Sim) map[int]string {
	out := map[int]string{}
	for _, m := range s.Matches() {
		out[m.Item.Number] = string(m.State) + ":" + string(m.BlockedBy) + string(m.Reason)
	}

	return out
}

func TestEveryLimitDelaysAStartAndOnlyItsOwnLimitsCloseOne(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name     string
		edit     func(sp *spec.Spec)
		settings func(c *autopilotprefs.Config)
		want     []string
	}{
		{
			name: "agents at once", settings: func(c *autopilotprefs.Config) { c.AgentsAtOnce = 1 },
			want: []string{"queued:slot", running},
		},
		{
			name: "agent runs a day", settings: func(c *autopilotprefs.Config) { c.RunsPerDay = 1 },
			want: []string{"queued:cap_runs", running},
		},
		{
			name: "the routine's runs a day", edit: func(sp *spec.Spec) { sp.Limits.RunsPerDay = new(1) },
			want: []string{"queued:cap_runs", running},
		},
		{
			name: "the daily cap", settings: func(c *autopilotprefs.Config) {
				c.DailyCap = autopilotprefs.DailyCap{Amount: 5, Unit: autopilotprefs.UnitUSD}
			},
			want: []string{"queued:cap_spend", running},
		},
		{
			name: "the routine's own budget", edit: func(sp *spec.Spec) { sp.Limits.BudgetPerDay = &spec.Budget{USD: new(4.0)} },
			want: []string{"queued:cap_spend", running},
		},
		{
			name: "active hours", edit: func(sp *spec.Spec) {
				sp.Limits.ActiveHours = &spec.ActiveHours{Days: []catalog.Weekday{catalog.Saturday}, From: "08:00", To: "20:00"}
			},
			want: []string{"queued:active_hours", "queued:active_hours"},
		},
		{
			name: "Pause Autopilot", settings: func(c *autopilotprefs.Config) { c.Paused = true },
			want: []string{"queued:paused", "queued:paused"},
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				s := two(t, c.edit, c.settings)
				assert.Equal(t, c.want, tally(s))
			})
		})
	}
}

func TestPauseHoldsLocalStepsTooAndResumeRunsThem(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.Begin()
		s.Change(func(c *autopilotprefs.Config) { c.Paused = true })

		r := s.Save("Pin bumps", sim.Pins(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.World.Add(sim.Item{Number: 1, Watched: true})
		s.Refresh()

		assert.Equal(t, []sim.Row{row(1, autopilot.StateQueued, autopilot.BlockPaused, "", "appears:item")}, s.Ledger())
		assert.Empty(t, s.Triage.Calls(), "Pause holds a local step too")

		s.Advance(time.Hour, true)
		s.Change(func(c *autopilotprefs.Config) { c.Paused = false })

		assert.Equal(t, []sim.Row{row(1, autopilot.StateDone, "", autopilot.ReasonRan, "appears:item")}, s.Ledger(),
			"a short pause runs what waited on Resume")
		assert.Len(t, s.Triage.Calls(), 1)
	})
}

func TestTriesCloseAMatchAsGaveUpAndTryAgainReopensIt(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		sp := sim.FixFailing()
		sp.Limits.TriesPerItem, sp.Limits.WaitMinutes = new(1), new(0)
		s, r := started(t, sp, autopilotprefs.ModeAuto)

		s.World.Fail(1)
		s.Refresh()

		first := only(t, s)
		s.Runner.Finish(first.Runs[0].ID, autopilot.RunEnd{})
		s.Settle()

		// Dependabot rebases: a new commit, which fails too, but the try is used.
		s.World.Push(1, sim.Commit(1, 2))
		s.World.Fail(1)
		s.Refresh()

		want := []sim.Row{
			row(1, autopilot.StateDone, "", autopilot.ReasonRan, failedKey(1)),
			row(1, autopilot.StateSkipped, "", autopilot.ReasonTries, failedKey(2)),
		}
		assert.Equal(t, want, s.Ledger(), "a rebase doesn't reset the tries")
		require.Len(t, s.Runner.Started(), 1)

		opened, err := s.Service.TryAgain(t.Context(), r.ID, first.Item.ID)
		require.NoError(t, err)
		require.Len(t, opened, 1)
		s.Refresh()

		assert.Equal(t, autopilot.StateRunning, s.Ledger()[1].State, "Try again runs it again")
		assert.Len(t, s.Runner.Started(), 2)
	})
}

func TestTheWaitBetweenTriesDelaysTheNextTry(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		sp := sim.FixFailing()
		sp.Limits.WaitMinutes = new(30)
		s, _ := started(t, sp, autopilotprefs.ModeAuto)

		s.World.Fail(1)
		s.Refresh()
		s.Runner.Finish(only(t, s).Runs[0].ID, autopilot.RunEnd{})
		s.Settle()

		s.World.Push(1, sim.Commit(1, 2))
		s.World.Fail(1)
		s.Refresh()
		assert.Equal(t, row(1, autopilot.StateQueued, autopilot.BlockWait, "", failedKey(2)), s.Ledger()[1])

		s.Advance(30*time.Minute, true)
		assert.Equal(t, autopilot.StateRunning, s.Ledger()[1].State, "it starts once the wait passed")
		assert.Len(t, s.Runner.Started(), 2)
	})
}

func TestAnOutsideContributorsItemNeverGetsAnAgent(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, Author: "stranger", Association: "NONE", Checks: passing})
		s.Begin()

		r := s.Save("Fix failing", sim.FixFailing(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.World.Fail(1)
		s.Refresh()
		s.Refresh()

		assert.Equal(t, []sim.Row{row(1, autopilot.StateSkipped, "", autopilot.ReasonOutside, failedKey(1))}, s.Ledger())
		assert.Empty(t, s.Runner.Started())
	})
}

func TestAForksPullRequestCountsAsAnOutsideContributorsItem(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, CrossRepo: true, Checks: passing})
		s.Begin()

		r := s.Save("Fix failing", sim.FixFailing(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.World.Fail(1)
		s.Refresh()
		s.Refresh()

		assert.Equal(t, []sim.Row{row(1, autopilot.StateSkipped, "", autopilot.ReasonOutside, failedKey(1))}, s.Ledger(),
			"whoever opened it, before its branch is checked")
	})
}

func TestTwoRoutinesOnOneItemRunOneAtATime(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)

		second := s.Save("Also fix", sim.FixFailing(), autopilotprefs.ModeAuto)
		s.Enable(second.ID, autopilot.CurrentRun)
		s.World.Fail(1)
		s.Refresh()
		s.Refresh()

		got := states(s)
		require.Len(t, s.Matches(), 2)
		assert.Len(t, s.Runner.Started(), 1, "one active run per item")

		var busy int

		for _, m := range s.Matches() {
			if m.BlockedBy == autopilot.BlockBusyWorktree {
				busy++
			}
		}

		assert.Equal(t, 1, busy, "the other waits for it: %v", got)
	})
}

func TestGitHubActionsKeepToTheDailyCountAndTheFirstPassCap(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true})
		s.World.Add(sim.Item{Number: 2, Watched: true})
		s.Begin()
		s.Change(func(c *autopilotprefs.Config) { c.ActionsPerDay = 1 })

		r := s.Save("Rebase", sim.Rebases(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()

		assert.Equal(t, []string{"done:ran", "queued:cap_actions"}, tally(s))
		assert.Len(t, s.GitHub.Jobs(), 1)

		s.Sleep(16 * time.Hour) // 01:00 the next day, well within the catch-up setting
		s.Refresh()
		assert.Equal(t, map[int]string{1: "done:ran", 2: "done:ran"}, states(s), "the next day it sends the other")
	})
}

func TestAFirstPassOverItsCapWaitsAsABacklog(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		for n := 1; n <= autopilot.FirstCap+1; n++ {
			s.World.Add(sim.Item{Number: n, Watched: true})
		}

		s.Begin()
		s.Refresh()

		r := s.Save("Rebase", sim.Rebases(), autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()

		matches := s.Matches()
		backlog := make([]int64, 0, len(matches))

		for _, m := range matches {
			assert.Equal(t, autopilot.BlockBacklog, m.BlockedBy, "#%d", m.Item.Number)
			backlog = append(backlog, m.ID)
		}

		require.Len(t, backlog, autopilot.FirstCap+1)
		assert.Empty(t, s.GitHub.Jobs(), "over the cap it queues none")

		_, err := s.Service.Approve(t.Context(), backlog[:2])
		require.NoError(t, err)
		s.Refresh()
		assert.Len(t, s.GitHub.Jobs(), 2, "Run on them sends those")
	})
}
