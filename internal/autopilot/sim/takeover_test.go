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

// Take over: the user stops a routine's agent and carries its conversation on in their
// terminal. Its match closes as taken over, done, with no try counted and nothing told,
// whatever the run had come to.

// takenOver asserts the only match closed as taken over, its run counted as no try, and no
// Agent finished told.
func takenOver(t *testing.T, s *sim.Sim) {
	t.Helper()

	m := only(t, s)
	assert.Equal(t, autopilot.StateDone, m.State, m.Words)
	assert.Equal(t, autopilot.ReasonTakenOver, m.Reason)
	assert.Contains(t, m.Words, "carries on in your terminal")
	assert.Empty(t, noticesOf(s, autopilot.NoticeFinished), "nothing tells the user a run they took over ended")

	var counted bool

	require.NoError(t, s.DB.QueryRowContext(t.Context(), "SELECT counted FROM autopilot_runs WHERE id = ?",
		m.Runs[0].ID).Scan(&counted))
	assert.False(t, counted, "a run taken over counts as no try")
}

func TestTakingOverAWorkingRunClosesItsMatchAsTakenOver(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.Reviews(), autopilotprefs.ModeAuto)

		m := only(t, s)
		require.Equal(t, autopilot.StateRunning, m.State, m.Words)
		run := m.Runs[0].ID

		view, err := s.Service.RunView(t.Context(), run)
		require.NoError(t, err)
		assert.True(t, view.Live)
		assert.Equal(t, m.ID, view.Match.ID, "the run view shows its match, with the routine's steps")
		assert.NotEmpty(t, view.Match.Steps)

		require.NoError(t, s.Engine.TakeOver(t.Context(), run), "told before the stop, as Take over does")

		s.Runner.Finish(run, autopilot.RunEnd{Outcome: autopilot.RunStopped})
		s.Settle()

		takenOver(t, s)

		view, err = s.Service.RunView(t.Context(), run)
		require.NoError(t, err)
		assert.False(t, view.Live)
		assert.Equal(t, string(autopilot.RunStopped), view.Run.Outcome)
	})
}

func TestTakingOverAnInterruptedRunClosesItsMatch(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.Reviews(), autopilotprefs.ModeAuto)
		run := only(t, s).Runs[0].ID

		s.Runner.Finish(run, autopilot.RunEnd{Outcome: autopilot.RunInterrupted})
		s.Settle()
		require.Equal(t, autopilot.BlockInterrupted, only(t, s).BlockedBy, "the Mac shut down mid-run")

		require.NoError(t, s.Engine.TakeOver(t.Context(), run))
		s.Settle()

		takenOver(t, s)
	})
}

func TestTakingOverARunTheUserStoppedClosesItsMatchAsTakenOver(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.Reviews(), autopilotprefs.ModeAuto)
		run := only(t, s).Runs[0].ID

		s.Runner.Finish(run, autopilot.RunEnd{Outcome: autopilot.RunStopped})
		s.Settle()
		require.Equal(t, autopilot.ReasonYouStopped, only(t, s).Reason)

		require.NoError(t, s.Engine.TakeOver(t.Context(), run))
		s.Settle()

		m := only(t, s)
		assert.Equal(t, autopilot.StateDone, m.State)
		assert.Equal(t, autopilot.ReasonTakenOver, m.Reason, "the session panel's Take over, once stopped")
	})
}

func TestTakingOverAFinishedRunLeavesItsMatchAsItWas(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.Reviews(), autopilotprefs.ModeAuto)
		run := only(t, s).Runs[0].ID

		s.Runner.Finish(run, autopilot.RunEnd{Outcome: autopilot.RunDone, Summary: "Finished: looks good."})
		s.Settle()

		require.NoError(t, s.Engine.TakeOver(t.Context(), run))
		s.Settle()

		m := only(t, s)
		assert.Equal(t, autopilot.StateDone, m.State)
		assert.Equal(t, autopilot.ReasonRan, m.Reason, "a match that moved on from the run keeps what it came to")

		err := s.Engine.TakeOver(t.Context(), run+100)
		require.ErrorIs(t, err, autopilot.ErrNoRun)
	})
}

func TestARunFollowedElsewhereHasNoTimelineHere(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.Reviews(), autopilotprefs.ModeAuto)
		run := only(t, s).Runs[0].ID

		_, err := s.DB.ExecContext(t.Context(), "UPDATE autopilot_runs SET runner = 'terminal' WHERE id = ?", run)
		require.NoError(t, err)

		page, err := s.Service.RunEvents(t.Context(), run, 0)
		require.NoError(t, err, "a run in the terminal is its session's to follow, not an error")
		assert.Empty(t, page.Events)
		assert.False(t, page.Done, "it still works")

		page, err = s.Service.RunEventsBefore(t.Context(), run, 100)
		require.NoError(t, err)
		assert.Empty(t, page.Events)

		view, err := s.Service.RunView(t.Context(), run)
		require.NoError(t, err)
		assert.False(t, view.CanTakeOver, "a session in the terminal resumes from its own panel")
	})
}

func TestARunsSessionNamesItsRunView(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s, _ := started(t, sim.Reviews(), autopilotprefs.ModeAuto)
		run := only(t, s).Runs[0].ID

		const session = "11111111-2222-4333-8444-555555555555"

		_, err := s.DB.ExecContext(t.Context(), "UPDATE autopilot_runs SET session_id = ?, agent = 'claude' WHERE id = ?",
			session, run)
		require.NoError(t, err)

		got, err := s.Service.RunBySession(t.Context(), session)
		require.NoError(t, err)
		assert.Equal(t, run, got, "a session's Watch opens its run's view")

		none, err := s.Service.RunBySession(t.Context(), "22222222-3333-4444-8555-666666666666")
		require.NoError(t, err)
		assert.Zero(t, none, "a session in the terminal is no run of Autopilot's")

		view, err := s.Service.RunView(t.Context(), run)
		require.NoError(t, err)
		assert.True(t, view.CanTakeOver, "a background run whose agent resumes its conversation")
	})
}
