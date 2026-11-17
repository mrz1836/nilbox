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

// Closing the loop: Wait for the checks on what the agent pushed, the verdict, the steps
// that follow it by Only if, the tries, and giving up.

// fixThenTell is the flagship's shape with its tail: fix, wait for the checks if it pushed,
// then tell the user if they still fail, and say so too if they pass, with no wait between
// tries.
func fixThenTell() spec.Spec {
	sp := sim.FixFailing()
	sp.Steps = append(sp.Steps,
		spec.Step{TellMe: &spec.TellStep{When: catalog.TellVerdict}, OnlyIf: catalog.OutcomeFailingChecks},
		spec.Step{Notify: &spec.NotifyStep{Note: "fixed"}, OnlyIf: catalog.OutcomePassedChecks},
	)
	sp.Limits.WaitMinutes = new(0)

	return sp
}

// pushAndFinish has the agent of #1's run push commit n and end, and lets the inbox see the
// push.
func pushAndFinish(t *testing.T, s *sim.Sim, run int64, n int) string {
	t.Helper()

	commit := sim.Commit(1, n)
	s.World.Push(1, commit)
	s.Runner.Finish(run, autopilot.RunEnd{Commit: commit, Head: commit})
	s.Refresh()

	return commit
}

// runningMatch returns the match now running, failing the test without one.
func runningMatch(t *testing.T, s *sim.Sim) autopilot.Match {
	t.Helper()

	for _, m := range s.Matches() {
		if m.State == autopilot.StateRunning {
			return m
		}
	}

	require.FailNow(t, "no match runs", "%+v", s.Ledger())

	return autopilot.Match{}
}

func TestAFixWhoseChecksPassIsFixedAndTakesThePassedBranch(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, r := started(t, fixThenTell(), autopilotprefs.ModeAuto)
		s.World.Fail(1)
		s.Refresh()

		run := only(t, s).Runs[0].ID
		fix := pushAndFinish(t, s, run, 2)

		m := only(t, s)
		require.Equal(t, autopilot.StateVerifying, m.State, m.Words)
		assert.Equal(t, "running", m.Steps[1].Status, "Wait for the checks is under way")

		s.World.Pass(1)
		s.Refresh()
		s.Refresh()

		m = only(t, s)
		assert.Equal(t, autopilot.StateDone, m.State, m.Words)
		assert.Equal(t, autopilot.ReasonRan, m.Reason)
		assert.Equal(t, autopilot.VerdictFixed, m.Steps[1].Verdict)
		assert.Equal(t, string(catalog.OutcomePassedChecks), m.Steps[1].Outcome)
		assert.Equal(t, "Fixed: checks pass on "+fix[:7]+".", m.Steps[1].Note)
		assert.Equal(t, autopilot.VerdictFixed, m.Runs[0].Verdict, "the run keeps its verdict")
		assert.Equal(t, "skipped", m.Steps[2].Status, "Tell me only if they still fail is skipped")
		assert.Equal(t, "done", m.Steps[3].Status, "and the step after it runs")

		checks := noticesOf(s, autopilot.NoticeChecks)
		require.Len(t, checks, 1, "Checks after a fix is told")
		assert.Equal(t, "Routine: Fixed: checks pass on "+fix[:7]+".", checks[0].Body)
		assert.Equal(t, m.Runs[0].ID, checks[0].RunID)

		week, err := s.Service.Stats(t.Context(), r.ID)
		require.NoError(t, err)
		assert.Equal(t, 1, week.Fixes, "This week counts the fix")

		s.Restart()
		s.Refresh()
		s.Advance(5*time.Minute, true)

		assert.Len(t, noticesOf(s, autopilot.NoticeChecks), 1, "told once across a restart")
		assert.Len(t, s.Runner.Started(), 1, "and nothing runs again")
	})
}

func TestAFixThatStillFailsTriesAgainOnItsOwnCommitThenGivesUpUntilTryAgain(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, r := started(t, fixThenTell(), autopilotprefs.ModeAuto)
		s.World.Fail(1)
		s.Refresh()

		// The first try pushes a fix whose checks still fail.
		fix := pushAndFinish(t, s, runningMatch(t, s).Runs[0].ID, 2)
		s.World.FailNamed(1, "CodeQL")
		s.Refresh()
		s.Refresh()

		first := s.Matches()[0]
		assert.Equal(t, autopilot.StateDone, first.State, first.Words)
		assert.Equal(t, autopilot.VerdictFailing, first.Steps[1].Verdict)
		assert.Equal(t, "Still failing: CodeQL.", first.Steps[1].Note, "the first of two tries doesn't give up")
		assert.Equal(t, "done", first.Steps[2].Status, "Tell me only if they still fail runs")
		assert.Equal(t, "skipped", first.Steps[3].Status)

		// Its own commit failing is a new occurrence: the second try runs on it.
		s.Advance(2*time.Minute, false)

		second := runningMatch(t, s)
		assert.Equal(t, failedKeyOf(fix), second.FireKey)
		require.Len(t, s.Runner.Started(), 2)

		pushAndFinish(t, s, second.Runs[0].ID, 3)
		s.World.FailNamed(1, "CodeQL")
		s.Refresh()
		s.Refresh()

		matches := s.Matches()
		require.Len(t, matches, 3, "%+v", s.Ledger())
		assert.Equal(t, "Still failing: CodeQL. Gave up after 2 tries.", matches[1].Steps[1].Note)
		assert.Contains(t, noticesOf(s, autopilot.NoticeChecks)[1].Body, "Gave up after 2 tries.")

		third := matches[2]
		assert.Equal(t, autopilot.StateSkipped, third.State, "the next occurrence gives up")
		assert.Equal(t, autopilot.ReasonTries, third.Reason)
		assert.Equal(t, "Gave up after 2 tries.", third.Words)
		assert.Len(t, s.Runner.Started(), 2, "no third run on its own")

		opened, err := s.Service.TryAgain(t.Context(), r.ID, third.Item.ID)
		require.NoError(t, err)
		require.Len(t, opened, 1, "Try again opens the occurrence that gave up")

		s.Advance(2*time.Minute, false)
		assert.Len(t, s.Runner.Started(), 3, "and its run starts, its tries counted afresh")
	})
}

func TestADependabotRebaseMidRunLetsTheRunCarryOnAndOvertakesItsFixWithoutResettingTheTries(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, fixThenTell(), autopilotprefs.ModeAuto)
		s.World.Fail(1)
		s.Refresh()

		run := only(t, s).Runs[0].ID

		// The agent pushes its fix, then Dependabot rebases over it while the run is still
		// writing its report.
		fix := sim.Commit(1, 2)
		s.World.Push(1, fix)
		s.Refresh()

		rebased := sim.Commit(1, 9)
		s.World.Push(1, rebased)
		s.World.Fail(1)
		s.Refresh()

		first := s.Matches()[0]
		assert.Equal(t, autopilot.StateRunning, first.State, "the run carries on: %s", first.Words)
		assert.Len(t, s.Runner.Going(), 1)

		s.Runner.Finish(run, autopilot.RunEnd{Commit: fix, Head: fix})
		s.Refresh()

		first = s.Matches()[0]
		assert.Equal(t, autopilot.StateDone, first.State, first.Words)
		assert.Equal(t, autopilot.VerdictOvertaken, first.Steps[1].Verdict)
		assert.Contains(t, first.Steps[1].Note, "someone else pushed after it")
		assert.Equal(t, "skipped", first.Steps[2].Status, "an overtaken fix is neither passed nor still failing")

		// The rebase is a new occurrence, but no new count: the routine's second try is its
		// last.
		s.Advance(2*time.Minute, false)

		second := runningMatch(t, s)
		assert.Equal(t, failedKeyOf(rebased), second.FireKey)

		pushAndFinish(t, s, second.Runs[0].ID, 10)
		s.World.Fail(1)
		s.Refresh()
		s.Refresh()

		assert.Contains(t, s.Matches()[1].Steps[1].Note, "Gave up after 2 tries.", "Dependabot's rebase reset nothing")
	})
}

func TestAPushRefusedAsTheBranchMovedIsRejectedNotFailed(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		sp := fixThenTell()
		sp.Steps[1].OnlyIf = ""

		s, _ := started(t, sp, autopilotprefs.ModeAuto)
		s.World.Fail(1)
		s.Refresh()

		run := only(t, s).Runs[0].ID

		// Dependabot rebases before the agent's push, which never force-pushes: refused.
		s.World.Push(1, sim.Commit(1, 9))
		s.Refresh()
		s.Runner.Finish(run, autopilot.RunEnd{Head: sim.Commit(1, 2), Summary: "Finished: the push was refused."})
		s.Advance(2*time.Minute, true)

		m := s.Matches()[0]
		assert.Equal(t, autopilot.StateDone, m.State, m.Words)
		assert.Equal(t, autopilot.VerdictRejected, m.Steps[1].Verdict)
		assert.Equal(t, "Its push was rejected: the branch moved while it worked.", m.Steps[1].Note)
		assert.Empty(t, noticesOf(s, autopilot.NoticeChecks), "no fix was checked, so nothing tells of one")
	})
}

func TestChecksThatNeverFinishAreUnknownAfterTheGiveUpTime(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		sp := fixThenTell()
		sp.Steps[1].WaitForChecks.GiveUpHours = new(1)

		s, _ := started(t, sp, autopilotprefs.ModeAuto)
		s.World.Fail(1)
		s.Refresh()

		fix := pushAndFinish(t, s, only(t, s).Runs[0].ID, 2)

		s.Advance(50*time.Minute, true)
		assert.Equal(t, autopilot.StateVerifying, only(t, s).State, "still running is no verdict yet")

		s.Advance(15*time.Minute, true)

		m := only(t, s)
		assert.Equal(t, autopilot.StateDone, m.State, m.Words)
		assert.Equal(t, autopilot.VerdictUnknown, m.Steps[1].Verdict)
		assert.Equal(t, "Unknown: the checks on "+fix[:7]+" hadn't finished after 1 hour.", m.Steps[1].Note)
		assert.Equal(t, "skipped", m.Steps[2].Status)
		assert.Equal(t, "skipped", m.Steps[3].Status)
	})
}

func TestNewCommitsNeverFiresOnARoutinesOwnPushEvenMidRun(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.Begin()

		fixer := s.Save("Fix", fixThenTell(), autopilotprefs.ModeAuto)
		s.Enable(fixer.ID, autopilot.CurrentRun)

		commits := spec.Spec{
			Version: spec.Version, Watch: spec.Watch{Sources: []spec.Source{sim.Search()}},
			Trigger: spec.Trigger{Event: catalog.EventNewCommits, FirstToo: new(false)},
			Steps:   []spec.Step{{Notify: &spec.NotifyStep{}}},
		}
		teller := s.Save("New commits", commits, autopilotprefs.ModeAuto)
		s.Enable(teller.ID, autopilot.CurrentRun)
		s.Refresh()

		s.World.Fail(1)
		s.Refresh()

		run := runningMatch(t, s).Runs[0].ID

		// The agent pushes while it works: the other routine waits to tell its push apart.
		fix := sim.Commit(1, 2)
		s.World.Push(1, fix)
		s.Refresh()
		assert.Empty(t, matchesOf(s, teller.ID), "nothing fires on a commit while a routine's agent works")

		s.Runner.Finish(run, autopilot.RunEnd{Commit: fix, Head: fix})
		s.Advance(3*time.Minute, true)
		assert.Empty(t, matchesOf(s, teller.ID), "nor once its run records it as its own")

		theirs := sim.Commit(1, 7)
		s.World.Push(1, theirs)
		s.Refresh()

		got := matchesOf(s, teller.ID)
		require.Len(t, got, 1, "someone else's push fires it")
		assert.Equal(t, "new_commits:commit:"+theirs, got[0].FireKey)
	})
}

// failedKeyOf is Checks fail's fire key on commit.
func failedKeyOf(commit string) string {
	return "checks_failed:commit:" + commit
}

// matchesOf returns the routine id's matches, oldest first.
func matchesOf(s *sim.Sim, id int64) []autopilot.Match {
	var out []autopilot.Match

	for _, m := range s.Matches() {
		if m.RoutineID == id {
			out = append(out, m)
		}
	}

	return out
}
