package sim_test

import (
	"errors"
	"os"
	"path/filepath"
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

var errNoNotices = errors.New("the notifications can't be written")

// securityFailures is the "Tell me about security failures" template, exactly as the
// template gallery holds it: Dependabot's pull requests, checks fail, a failed check's
// name holds a security word, Notify me.
func securityFailures(t *testing.T) spec.Spec {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "templates", "tell-me-about-security-failures.json"))
	require.NoError(t, err)

	sp, err := spec.Load(data)
	require.NoError(t, err)

	return sp
}

// securityRig is a simulation watching Dependabot's pull requests #1 to #n, their checks
// passing, with Tell me about security failures switched on in mode.
func securityRig(t *testing.T, n int, mode autopilotprefs.Mode) (*sim.Sim, autopilot.Routine) {
	t.Helper()

	s := sim.New(t)
	for i := 1; i <= n; i++ {
		s.World.Add(sim.Item{Number: i, Watched: true, Checks: passing, Passed: 4})
	}

	s.Begin()

	r := s.Save("Tell me about security failures", securityFailures(t), mode)
	s.Enable(r.ID, autopilot.CurrentRun)
	s.Refresh()

	return s, r
}

// failedOn is Checks fail's fire key on commit c of #number.
func failedOn(number, c int) string {
	return "checks_failed:commit:" + sim.Commit(number, c)
}

func TestTellMeAboutSecurityFailures(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, r := securityRig(t, 2, autopilotprefs.ModeAuto)
		assert.Empty(t, r.DryRunOnly, "Notify me runs in this build")
		assert.Empty(t, s.Ledger(), "every check passes")
		assert.Empty(t, s.Notices.Told())

		// A failure that isn't a security check's matches nothing.
		s.World.FailNamed(1, "ci / lint")
		s.Refresh()
		assert.Empty(t, s.Ledger())
		assert.Empty(t, s.Notices.Told(), "no security word, no notice")

		// A security check fails: one notice, about the pull request, headed by the routine.
		s.World.FailNamed(2, "Security / gitleaks", "ci / lint")
		s.Refresh()

		assert.Equal(t, []sim.Row{row(2, autopilot.StateDone, "", autopilot.ReasonRan, failedOn(2, 1))}, s.Ledger())

		told := s.Notices.Told()
		require.Len(t, told, 1)

		m := s.Matches()[0]
		n := told[0]
		assert.Equal(t, autopilot.NoticeAlert, n.Kind)
		assert.Equal(t, r.ID, n.RoutineID)
		assert.Equal(t, "Tell me about security failures", n.Routine, "the routine heads it")
		assert.Equal(t, m.ID, n.MatchID, "it opens the match's card")
		assert.True(t, m.CreatedAt.Equal(n.CreatedAt), "with when the match was made")
		require.NotNil(t, n.Item)
		assert.Equal(t, 2, n.Item.Number)
		assert.Equal(t, m.Item.ID, n.Item.ID, "its item, which a click shows")
		assert.Equal(t, "Bump example-lib from 1.0.0 to 1.0.2", n.Title)
		assert.Contains(t, n.Body, "2 checks failed on", "the reason: what fired it")
		assert.Contains(t, n.Body, "Security / gitleaks", "and the check that held the condition")
		assert.Nil(t, n.Banner, "the banner follows Settings → Notifications")
		assert.False(t, n.Burst, "one at a time shows at once")

		// Once per occurrence: later refreshes, ticks, and a restart tell it no more.
		s.Advance(15*time.Minute, true)
		s.Restart()
		s.Refresh()
		assert.Len(t, s.Notices.Told(), 1, "told once")

		// A new commit that fails a security check again is a new occurrence.
		s.World.Push(2, sim.Commit(2, 2))
		s.Refresh()
		s.World.FailNamed(2, "CodeQL")
		s.Refresh()

		told = s.Notices.Told()
		require.Len(t, told, 2)
		assert.Contains(t, told[1].Body, "CodeQL")
		assert.Equal(t, []sim.Row{
			row(2, autopilot.StateDone, "", autopilot.ReasonRan, failedOn(2, 1)),
			row(2, autopilot.StateDone, "", autopilot.ReasonRan, failedOn(2, 2)),
		}, s.Ledger())
	})
}

func TestSecurityFailuresAtOnceAreABurst(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := securityRig(t, 4, autopilotprefs.ModeAuto)

		for i := 1; i <= 4; i++ {
			s.World.FailNamed(i, "osv-scanner")
		}

		s.Refresh()

		told := s.Notices.Told()
		require.Len(t, told, 4, "one notice each")

		for _, n := range told {
			assert.True(t, n.Burst, "#%d comes with the others, so they're gathered into one banner", n.Item.Number)
		}

		assert.Len(t, s.Ledger(), 4)
	})
}

func TestTheRoutinesOwnBannerChoiceGoesWithItsNotice(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.Begin()

		sp := securityFailures(t)
		sp.Notify = &spec.Notices{Alert: new(false)}
		sp.Steps[0].Notify.Note = "Look before lunch"

		r := s.Save("Quiet security failures", sp, autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()

		s.World.FailNamed(1, "govulncheck")
		s.Refresh()

		told := s.Notices.Told()
		require.Len(t, told, 1)
		require.NotNil(t, told[0].Banner)
		assert.False(t, *told[0].Banner, "the routine keeps its notices to the history")
		assert.Contains(t, told[0].Body, "Look before lunch.", "the user's own line goes with the reason")
	})
}

func TestADryRunTellsNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := securityRig(t, 1, autopilotprefs.ModeDryRun)

		s.World.FailNamed(1, "Security / nancy")
		s.Refresh()

		assert.Equal(t, []sim.Row{{
			Number: 1, State: autopilot.StateDone, Reason: autopilot.ReasonWouldRun, DryRun: true, Key: failedOn(1, 1),
		}}, s.Ledger())
		assert.Empty(t, s.Notices.Told(), "a Dry run records what it would tell")
	})
}

func TestANoticeThatCantBeToldFailsItsStep(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := securityRig(t, 1, autopilotprefs.ModeAuto)
		s.Notices.Fail(errNoNotices)

		s.World.FailNamed(1, "Security / gitleaks")
		s.Refresh()

		m := only(t, s)
		assert.Equal(t, autopilot.StateDone, m.State)
		assert.Equal(t, autopilot.ReasonFailed, m.Reason, "the match says a step failed")
		require.Len(t, m.Steps, 1)
		assert.Equal(t, "failed", m.Steps[0].Status)
		assert.Contains(t, m.Steps[0].Note, "can't be written")
	})
}

func TestANoticeAQuitCutShortIsToldOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := securityRig(t, 1, autopilotprefs.ModeAuto)

		// The notice is recorded, then the app quits before the engine hears it was told.
		s.Notices.Hold(true)
		s.World.FailNamed(1, "Security / gitleaks")
		s.Refresh()
		require.Len(t, s.Notices.Calls(), 1)

		s.Crash()
		s.Notices.Hold(false)
		s.Begin()
		s.Refresh()

		calls := s.Notices.Calls()
		require.Len(t, calls, 2, "the step runs again after the quit")
		assert.Equal(t, calls[0].Key, calls[1].Key, "with the same key")
		assert.NotEmpty(t, calls[0].Key)
		assert.Len(t, s.Notices.Told(), 1, "so the user is told once")
		assert.Equal(t, []sim.Row{row(1, autopilot.StateDone, "", autopilot.ReasonRan, failedOn(1, 1))}, s.Ledger())
	})
}

func TestAProblemACrashCutShortIsToldOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := securityRig(t, 1, autopilotprefs.ModeAuto)

		// The problem is told, then the app crashes before the engine saves that it was.
		s.Notices.Hold(true)
		s.World.FailSearches(true)
		s.Advance(10*time.Minute, true)
		require.Len(t, s.Notices.Calls(), 1)

		s.Crash()
		s.Notices.Hold(false)
		s.Begin()
		s.Advance(10*time.Minute, true)

		calls := s.Notices.Calls()
		require.Len(t, calls, 2, "it is told again after the crash")
		assert.Equal(t, calls[0].Key, calls[1].Key, "with the same key, since its start was saved first")
		assert.Len(t, s.Notices.Told(), 1, "so the user is told once")
	})
}

func TestOneProblemOneNotice(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, r := securityRig(t, 1, autopilotprefs.ModeAuto)

		s.World.FailSearches(true)
		s.Advance(time.Hour, true)

		told := s.Notices.Told()
		require.Len(t, told, 1, "a search that fails for an hour is told once")

		n := told[0]
		assert.Equal(t, autopilot.NoticeProblem, n.Kind)
		assert.Equal(t, r.ID, n.RoutineID, "it opens its routine")
		assert.Equal(t, "Tell me about security failures", n.Title)
		assert.Contains(t, n.Body, "GitHub couldn't answer its search")
		assert.Nil(t, n.Item, "a problem is about no item")
		assert.Zero(t, n.MatchID)
		assert.True(t, r.CreatedAt.Equal(n.CreatedAt), "with when the routine was made")

		s.Restart()
		s.Refresh()
		assert.Len(t, s.Notices.Told(), 1, "a restart tells it no more")

		s.World.FailSearches(false)
		s.Advance(10*time.Minute, true)
		s.World.FailSearches(true)
		s.Advance(10*time.Minute, true)
		assert.Len(t, s.Notices.Told(), 2, "once it ended, coming back tells it again")
	})
}

func TestAProblemThatCantBeToldIsToldLater(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := securityRig(t, 1, autopilotprefs.ModeAuto)
		s.Notices.Fail(errNoNotices)

		s.World.FailSearches(true)
		s.Refresh()
		assert.Empty(t, s.Notices.Told())

		s.Notices.Fail(nil)
		s.Advance(10*time.Minute, true)

		assert.Len(t, s.Notices.Told(), 1, "told once it can be, and once")

		status, err := s.Service.Engine(t.Context())
		require.NoError(t, err)
		require.Len(t, status.Problems, 1)
		assert.True(t, status.Problems[0].Told)
	})
}
