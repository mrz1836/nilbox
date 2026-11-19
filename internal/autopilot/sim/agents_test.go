package sim_test

import (
	"encoding/json"
	"errors"
	"strings"
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
	"github.com/mrz1836/nilbox/internal/uierr"
)

// ready returns a simulation whose Mac lacks what missing names, with one watched pull
// request, not started.
func ready(t *testing.T, missing ...string) *sim.Sim {
	t.Helper()

	s := sim.New(t)
	s.Ready = sim.NewReady()
	s.Ready.Miss(missing...)
	s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})

	return s
}

// failNow saves and switches on the flagship in mode, fails #1's checks, and lets two
// refreshes settle.
func failNow(t *testing.T, s *sim.Sim, mode autopilotprefs.Mode) autopilot.Routine {
	t.Helper()

	r := s.Save("Fix failing", sim.FixFailing(), mode)
	s.Enable(r.ID, autopilot.CurrentRun)
	s.World.Fail(1)
	s.Refresh()
	s.Refresh()

	return r
}

func TestARoutineThisMacIsntReadyForWaitsAndStartsOnItsOwnOnceItIs(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := ready(t, "agent:claude")
		s.Begin()

		failNow(t, s, autopilotprefs.ModeAuto)

		got := only(t, s)
		assert.Equal(t, autopilot.StateBlocked, got.State)
		assert.Equal(t, autopilot.BlockNotReady, got.BlockedBy)
		assert.Contains(t, got.Words, "Not ready on this Mac: agent:claude is missing. Run `fix agent:claude`, then Check again.")
		assert.Empty(t, s.Runner.Started(), "a Mac without what the routine needs starts nothing")
		assert.Zero(t, s.World.Reads(1), "and spends nothing on a read before a start")

		problems := 0

		for _, n := range s.Notices.Told() {
			if n.Kind == autopilot.NoticeProblem {
				problems++

				assert.Contains(t, n.Body, "agent:claude")
			}
		}

		assert.Equal(t, 1, problems, "one problem told, once")

		told := len(s.Notices.Told())
		s.Refresh()
		assert.Len(t, s.Notices.Told(), told, "never twice")

		s.Ready.Fix("agent:claude")
		_, err := s.Service.CheckReadiness(t.Context())
		require.NoError(t, err)
		s.Settle()
		s.Settle()

		require.Len(t, s.Runner.Started(), 1, "on Autopilot it starts by itself once the Mac is ready")
		assert.Equal(t, autopilot.StateRunning, only(t, s).State)
	})
}

func TestAnAskFirstRoutineThisMacIsntReadyForStartsAfterTheOKOnceItIs(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := ready(t, "profile:claude/default")
		s.Begin()

		failNow(t, s, autopilotprefs.ModeAsk)

		waiting := only(t, s)
		require.Equal(t, autopilot.StateWaiting, waiting.State)
		assert.Equal(t, autopilot.BlockAsk, waiting.BlockedBy)

		_, err := s.Service.Approve(t.Context(), []int64{waiting.ID})
		require.NoError(t, err)
		s.Settle()

		blocked := only(t, s)
		assert.Equal(t, autopilot.StateBlocked, blocked.State)
		assert.Equal(t, autopilot.BlockNotReady, blocked.BlockedBy)
		assert.True(t, blocked.Approved, "the OK stands while it can't start")
		assert.Empty(t, s.Runner.Started())

		s.Ready.Fix("profile:claude/default")
		_, err = s.Service.CheckReadiness(t.Context())
		require.NoError(t, err)
		s.Settle()
		s.Settle()

		assert.Len(t, s.Runner.Started(), 1, "it starts on the OK already given")
	})
}

func TestReadinessSaysWhatEachRoutineNeedsAndWhatIsMissing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := ready(t, "tool:gh")
		s.Begin()

		r := failNow(t, s, autopilotprefs.ModeAuto)

		got, err := s.Service.Readiness(t.Context())
		require.NoError(t, err)
		require.NotNil(t, got.CheckedAt)
		assert.Equal(t, 1, got.NotReady)
		require.Len(t, got.Routines, 1)
		assert.Equal(t, r.ID, got.Routines[0].RoutineID)
		assert.False(t, got.Routines[0].Ready)
		require.Len(t, got.Routines[0].Problems, 1)
		assert.Equal(t, "tool:gh", got.Routines[0].Problems[0].ID)

		ids := make([]string, 0, len(got.Checks))
		for _, c := range got.Checks {
			ids = append(ids, c.ID)
		}

		assert.Equal(t, []string{
			"github", "tool:gh", "tool:git", "agent:claude", "profile:claude/default", "place:background",
		}, ids, "This Mac lists GitHub and its tools first, then the agents and their profiles, then the places")
	})
}

func TestAnOutsideContributorsItemGetsAnAgentWhereItsOrgOrRepositoryAllowsIt(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name    string
		rules   map[string]bool
		started bool
	}{
		{name: "nothing chosen is off", rules: map[string]bool{}},
		{name: "its org allows it", rules: map[string]bool{sim.Owner: true}, started: true},
		{name: "its repository allows it", rules: map[string]bool{sim.Owner + "/" + sim.Repo: true}, started: true},
		{
			name:  "the repository's choice wins over its org's",
			rules: map[string]bool{sim.Owner: true, sim.Owner + "/" + sim.Repo: false},
		},
		{
			name:  "and the other way about",
			rules: map[string]bool{sim.Owner: false, sim.Owner + "/" + sim.Repo: true}, started: true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			synctest.Test(t, func(t *testing.T) {
				s := sim.New(t)
				s.Outside = &sim.Outside{}

				for target, on := range c.rules {
					s.Outside.Allow(target, on)
				}

				s.World.Add(sim.Item{Number: 1, Watched: true, Author: "stranger", Association: "NONE", Checks: passing})
				s.Begin()

				failNow(t, s, autopilotprefs.ModeAuto)

				if !c.started {
					assert.Equal(t, []sim.Row{row(1, autopilot.StateSkipped, "", autopilot.ReasonOutside, failedKey(1))},
						s.Ledger())
					assert.Contains(t, only(t, s).Words, "Autopilot can act on these in acme-org's launch defaults.")
					assert.Empty(t, s.Runner.Started())

					return
				}

				assert.Len(t, s.Runner.Started(), 1, "allowed, its agent starts, in plan mode, as the launcher holds it")
			})
		})
	}
}

func TestAStartTheLauncherHoldsWaitsForTheUsersRunOnExactlyThatHold(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.TellWaits = true
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.Begin()

		hold := &autopilot.Hold{Key: "agentFiles:" + sim.Commit(1, 1), Reason: "agentFiles", Head: sim.Commit(1, 1)}
		s.Runner.AnswerWith(autopilot.Result{
			Wait: autopilot.BlockHold, WaitWords: "Held: it changes agent configuration (.claude/settings.json).",
			Hold: hold,
		})

		failNow(t, s, autopilotprefs.ModeAuto)

		got := only(t, s)
		require.Equal(t, autopilot.StateWaiting, got.State)
		assert.Equal(t, autopilot.BlockHold, got.BlockedBy)
		require.NotNil(t, got.Hold, "the card shows the hold")
		assert.Equal(t, hold.Key, got.Hold.Key)
		assert.Equal(t, 0, got.Attempt, "a held start counts as no run")
		assert.Empty(t, s.Runner.Started())

		needs := 0

		for _, n := range s.Notices.Told() {
			if n.Kind == autopilot.NoticeNeedsYou {
				needs++

				assert.Equal(t, got.ID, n.MatchID, "its banner opens its card")
			}
		}

		assert.Equal(t, 1, needs, "one Needs your OK, however often a cycle sees it wait")

		s.Refresh()

		for _, j := range s.Runner.Held() {
			assert.Empty(t, j.Confirmed, "the routine never confirms a hold itself")
		}

		_, err := s.Service.Approve(t.Context(), []int64{got.ID})
		require.NoError(t, err)
		s.Settle()
		s.Settle()

		started := s.Runner.Started()
		require.Len(t, started, 1)
		assert.Equal(t, []string{hold.Key}, started[0].Confirmed, "the user's Run confirms exactly the hold shown")
	})
}

func TestASessionWorkingWhereARunWouldStartQueuesIt(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.Begin()

		s.Runner.AnswerWith(autopilot.Result{
			Wait:      autopilot.BlockBusyWorktree,
			WaitWords: "Queued: pr-1 is in use by a session in your terminal; starts when it ends.",
		})

		failNow(t, s, autopilotprefs.ModeAuto)

		assert.Len(t, s.Runner.Held(), 1)
		s.Advance(2*time.Minute, false)

		assert.Len(t, s.Runner.Started(), 1, "it starts by itself once the session is gone")
	})
}

func TestADirtyWorktreeWaitsForTheUsersOK(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.Begin()

		dirty := &autopilot.Hold{Key: "dirty:pr-1:abc:3", Reason: "dirty", Worktree: "pr-1", Dirty: 3}
		s.Runner.AnswerWith(autopilot.Result{Wait: autopilot.BlockDirty, WaitWords: "pr-1 has 3 uncommitted files.", Hold: dirty})

		failNow(t, s, autopilotprefs.ModeAuto)

		got := only(t, s)
		assert.Equal(t, autopilot.StateWaiting, got.State)
		assert.Equal(t, autopilot.BlockDirty, got.BlockedBy)
		assert.Equal(t, "pr-1 has 3 uncommitted files.", got.Words)

		s.Advance(3*time.Minute, false)
		assert.Empty(t, s.Runner.Started(), "it waits for the user, never starting in their changes on its own")

		_, err := s.Service.Approve(t.Context(), []int64{got.ID})
		require.NoError(t, err)
		s.Settle()
		s.Settle()

		started := s.Runner.Started()
		require.Len(t, started, 1)
		assert.Equal(t, []string{dirty.Key}, started[0].Confirmed)
	})
}

func TestWhatHoldsAStartOnThisMacSpendsNoRead(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.Holds = &sim.Holds{}
		s.Holds.Set(autopilot.BlockNoClone, "No local clone of acme-org/example-repo.")
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.Begin()

		failNow(t, s, autopilotprefs.ModeAuto)

		got := only(t, s)
		assert.Equal(t, autopilot.StateWaiting, got.State)
		assert.Equal(t, autopilot.BlockNoClone, got.BlockedBy)
		assert.Equal(t, "No local clone of acme-org/example-repo.", got.Words)
		assert.Zero(t, s.World.Reads(1), "held on this Mac, it reads nothing from GitHub")
		assert.NotEmpty(t, s.Holds.Asked())

		s.Holds.Set("", "")

		_, err := s.Service.Approve(t.Context(), []int64{got.ID})
		require.NoError(t, err)
		s.Settle()
		s.Settle()

		assert.Positive(t, s.World.Reads(1), "once nothing holds it, it is read fresh right before it starts")
		assert.Len(t, s.Runner.Started(), 1)
	})
}

func TestAStepsPromptGoneSinceTheSaveKeepsItsRunFromStarting(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.Library = &sim.Library{}
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.Begin()

		r := s.Save("Fix failing", sim.FixFailing(), autopilotprefs.ModeAuto)
		s.Library.Remove("fix-checks")
		s.Enable(r.ID, autopilot.CurrentRun)
		s.World.Fail(1)
		s.Refresh()
		s.Refresh()

		got := only(t, s)
		assert.Equal(t, autopilot.StateBlocked, got.State)
		assert.Equal(t, autopilot.BlockNotReady, got.BlockedBy)
		assert.Contains(t, got.Words, "Its prompt can't run")
		assert.Empty(t, s.Runner.Started())
	})
}

func TestRunAnywayRunsARoutineOnAnItemItDoesntMatch(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		security := sim.FixFailing()
		security.Conditions = spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{
			{Field: catalog.FieldTitle, Op: catalog.OpContains, Value: "security"},
		}}

		s, r := started(t, security, autopilotprefs.ModeAuto)
		id := s.ItemID(1)

		_, err := s.Service.RunNow(t.Context(), r.ID, id, false)
		require.ErrorIs(t, err, autopilot.ErrNoMatch, "its title names no security fix, so it doesn't match")
		assert.JSONEq(t, `{"code":"noMatch"}`, refusalDetail(t, err), "the window offers Run anyway from the code")
		assert.True(t, strings.HasPrefix(uierr.Message(err), "It doesn't match it: "), "with why, in words: %q", uierr.Message(err))

		m, err := s.Service.RunNow(t.Context(), r.ID, id, true)
		require.NoError(t, err)
		assert.True(t, m.Manual)
		assert.Contains(t, m.FireKey, "manual:")
		s.Settle()
		s.Settle()

		assert.Len(t, s.Runner.Started(), 1, "Run anyway starts it, through every limit a start has")

		_, err = s.Service.Skip(t.Context(), m.ID+100)
		require.ErrorIs(t, err, autopilot.ErrNoMatch, "a match that is gone")

		_, coded := errors.AsType[*autopilot.RefusalError](err)
		assert.False(t, coded, "only a routine that doesn't match the item offers Run anyway")
	})
}

// refusalDetail is the detail the window gets beside err, as JSON: a RefusalError's code.
func refusalDetail(t *testing.T, err error) string {
	t.Helper()

	refused, ok := errors.AsType[*autopilot.RefusalError](err)
	require.True(t, ok, "a coded refusal")

	detail, jsonErr := json.Marshal(refused.Detail())
	require.NoError(t, jsonErr)

	return string(detail)
}
