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

// The scenarios: each is a timeline, ending with the ledger it expects.

// started is a simulation with pull request #1 in the routines' search and a routine over
// sp in mode, switched on, after a first refresh.
func started(t *testing.T, sp spec.Spec, mode autopilotprefs.Mode) (*sim.Sim, autopilot.Routine) {
	t.Helper()

	s := sim.New(t)
	s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
	s.Begin()

	r := s.Save("Routine", sp, mode)
	s.Enable(r.ID, autopilot.CurrentRun)
	s.Refresh()

	return s, r
}

// row is a ledger row of #number.
func row(number int, state autopilot.State, blocker autopilot.Blocker, reason autopilot.Reason, key string) sim.Row {
	return sim.Row{Number: number, State: state, Blocker: blocker, Reason: reason, Key: key}
}

// failedKey is Checks fail's fire key on commit n of #1.
func failedKey(n int) string {
	return "checks_failed:commit:" + sim.Commit(1, n)
}

// passing is a head's checks all passing.
const passing = "SUCCESS"

// only returns the one match recorded.
func only(t *testing.T, s *sim.Sim) autopilot.Match {
	t.Helper()

	got := s.Matches()
	require.Len(t, got, 1)

	return got[0]
}

func TestALocalRoutineRunsOncePerFireKeyAcrossRestarts(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.Pins(), autopilotprefs.ModeAuto)

		want := []sim.Row{row(1, autopilot.StateDone, "", autopilot.ReasonRan, "appears:item")}
		assert.Equal(t, want, s.Ledger(), "the match ran once")
		require.Len(t, s.Triage.Calls(), 1, "the pin went to the triage")

		s.Refresh()
		s.Restart()
		s.Refresh()
		s.Advance(10*time.Minute, true)

		assert.Equal(t, want, s.Ledger(), "a refresh, a restart, and time never fire it again")
		assert.Len(t, s.Triage.Calls(), 1)
	})
}

func TestAnAgentRunsOncePerHeadCommitAcrossRestarts(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)

		s.World.Fail(1)
		s.Refresh()

		m := only(t, s)
		assert.Equal(t, autopilot.StateRunning, m.State, m.Words)
		require.Len(t, s.Runner.Started(), 1)
		assert.Equal(t, 1, s.World.Reads(1), "the item was read fresh right before the start")

		s.Restart()
		s.Refresh()
		s.Runner.Finish(m.Runs[0].ID, autopilot.RunEnd{})
		s.Settle()
		s.Restart()
		s.Refresh()

		assert.Equal(t, []sim.Row{row(1, autopilot.StateDone, "", autopilot.ReasonRan, failedKey(1))}, s.Ledger())
		assert.Len(t, s.Runner.Started(), 1, "the same commit never runs again")
	})
}

func TestACheckRunAgainOnACommitAnAgentRanOnStartsNoSecondRun(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.FixFailing(), autopilotprefs.ModeAuto)

		s.World.Fail(1)
		s.Refresh()

		m := only(t, s)
		require.Equal(t, autopilot.StateRunning, m.State, m.Words)
		s.Runner.Finish(m.Runs[0].ID, autopilot.RunEnd{})
		s.Settle()

		want := []sim.Row{row(1, autopilot.StateDone, "", autopilot.ReasonRan, failedKey(1))}
		require.Equal(t, want, s.Ledger())

		// Someone runs the failed check again on the same head: it passes, then fails
		// again, the same commit's checks failing a second time, across a restart too.
		s.World.Pass(1)
		s.Refresh()
		s.World.Fail(1)
		s.Refresh()
		s.Restart()
		s.World.Pass(1)
		s.Refresh()
		s.World.Fail(1)
		s.Advance(10*time.Minute, true)

		assert.Equal(t, want, s.Ledger(), "the same commit's checks failing again is the same occurrence")
		assert.Len(t, s.Runner.Started(), 1, "a check run again on a commit an agent ran on starts no second run")
	})
}

func TestTurningOnRunsOnTheCurrentMatchesOrRecordsThemAsHandled(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name    string
		current autopilot.Current
		want    []sim.Row
		pins    int
	}{
		{"run on them too", autopilot.CurrentRun, []sim.Row{row(1, autopilot.StateDone, "", autopilot.ReasonRan, "appears:item")}, 1},
		{"only from now on", autopilot.CurrentFromNow, []sim.Row{row(1, autopilot.StateBaseline, "", autopilot.ReasonFromNow, "appears:item")}, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				s := sim.New(t)
				s.World.Add(sim.Item{Number: 1, Watched: true})
				s.Begin()
				s.Refresh()

				r := s.Save("Pin bumps", sim.Pins(), autopilotprefs.ModeAuto)

				asked, err := s.Service.Matching(t.Context(), autopilot.Draft{ID: r.ID, Name: r.Name, Spec: r.Spec})
				require.NoError(t, err)
				assert.Equal(t, autopilot.Matching{Count: 1, PullRequests: 1}, asked, "1 pull request matches right now")

				s.Enable(r.ID, c.current)
				s.Refresh()
				assert.Equal(t, c.want, s.Ledger())
				assert.Len(t, s.Triage.Calls(), c.pins)

				s.World.Add(sim.Item{Number: 2, Watched: true})
				s.Refresh()

				got := s.Ledger()
				require.Len(t, got, 2)
				assert.Equal(t, row(2, autopilot.StateDone, "", autopilot.ReasonRan, "appears:item"), got[1],
					"what comes after runs either way")
			})
		})
	}
}

func TestDryRunRecordsWhatWouldRunAndBlocksNothingReal(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, r := started(t, sim.FixFailing(), autopilotprefs.ModeDryRun)

		s.World.Fail(1)
		s.Refresh()
		s.Refresh()

		dry := sim.Row{Number: 1, State: autopilot.StateDone, Reason: autopilot.ReasonWouldRun, DryRun: true, Key: failedKey(1)}
		assert.Equal(t, []sim.Row{dry}, s.Ledger(), "recorded once, as what would have run")
		assert.Empty(t, s.Runner.Started(), "nothing ran")

		// A week later it leaves Dry run: the question counts what matches with no real record.
		s.Sleep(7 * 24 * time.Hour)
		s.Refresh()

		asked, err := s.Service.Matching(t.Context(), autopilot.Draft{ID: r.ID, Name: r.Name, Spec: r.Spec})
		require.NoError(t, err)
		assert.Equal(t, 1, asked.Count, "a Dry run's record never counts as a real one")

		_, err = s.Service.SetMode(t.Context(), r.ID, autopilotprefs.ModeAuto, autopilot.CurrentRun)
		require.NoError(t, err)
		s.Refresh()

		got := s.Ledger()
		require.Len(t, got, 2)
		assert.Equal(t, dry, got[0])
		assert.Equal(t, row(1, autopilot.StateRunning, "", "", failedKey(1)), got[1], "then the right run")
		assert.Len(t, s.Runner.Started(), 1)
	})
}

func TestTheBuilderSuggestsWhatTheInboxStored(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Listed: true, Labels: []string{"dependencies"}, Checks: passing, Passed: 2})
		s.Begin()
		s.Refresh()

		got, err := s.Service.Suggest(t.Context())
		require.NoError(t, err)
		assert.Equal(t, autopilot.Suggestions{
			Authors: []string{}, Bots: []string{"dependabot"}, Owners: []string{sim.Owner}, Labels: []string{"dependencies"},
			Checks: []string{},
		}, got, "the bot that opened it, its repository's owner, and its label; no check was read")
	})
}
