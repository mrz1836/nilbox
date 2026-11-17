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

// Run one of my scripts: a step that follows the routine's mode, recorded as a run and
// handed to the scripts' runner; a script set to ask before each run waits for the user's
// OK every time, whatever the mode, for exactly the script the user saw.

// lint is one of the user's scripts, which asks before each run when asks says so.
func lint(asks bool, look string) autopilot.MyScript {
	return autopilot.MyScript{ID: 7, Name: "Lint", Command: "make lint", Asks: asks, Look: look, Shows: "every clone"}
}

// runsLint is a routine that runs Lint on each watched pull request as it appears.
func runsLint() spec.Spec {
	return spec.Spec{
		Version: spec.Version,
		Watch:   spec.Watch{Sources: []spec.Source{sim.Search()}},
		Trigger: spec.Trigger{Event: catalog.EventAppears},
		Steps:   []spec.Step{{Script: &spec.ScriptStep{ID: 7, Name: "Lint"}}},
	}
}

// scriptRig is a simulation watching #1, with the user's scripts and the scripts' runner,
// and Run Lint saved and switched on in mode.
func scriptRig(t *testing.T, mode autopilotprefs.Mode, mine ...autopilot.MyScript) *sim.Sim {
	t.Helper()

	s := sim.New(t)
	s.Scripts = sim.NewRunner()
	s.MyScripts = &sim.MyScripts{}
	s.MyScripts.Set(mine...)
	s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
	s.Begin()

	r := s.Save("Run Lint", runsLint(), mode)
	s.Enable(r.ID, autopilot.CurrentRun)
	s.Refresh()
	s.Refresh()

	return s
}

func TestAScriptThatDoesntAskRunsInItsRoutinesMode(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := scriptRig(t, autopilotprefs.ModeAuto, lint(false, "v1"))

		require.Len(t, s.Scripts.Started(), 1, "Autopilot runs it at once")
		job := s.Scripts.Started()[0]
		require.NotNil(t, job.Step.Script)
		assert.Equal(t, int64(7), job.Step.Script.ID)
		assert.Empty(t, s.Runner.Started(), "the scripts' runner runs it, not the agents'")

		m := only(t, s)
		require.Len(t, m.Runs, 1)
		assert.Equal(t, "script", m.Runs[0].Runner)
		assert.Equal(t, autopilot.StateRunning, m.State)

		g, err := s.Service.Glance(t.Context())
		require.NoError(t, err)
		assert.Empty(t, g.Working, "a script is no agent at work")

		s.Scripts.Finish(job.RunID, autopilot.RunEnd{Outcome: autopilot.RunDone, Summary: "It exited with status 0."})
		s.Settle()

		m = only(t, s)
		assert.Equal(t, autopilot.StateDone, m.State)
		assert.Equal(t, autopilot.ReasonRan, m.Reason)
		assert.Empty(t, m.Steps[0].Outcome, "a script says nothing of a push")
	})
}

func TestAScriptThatFailsFailsItsStep(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := scriptRig(t, autopilotprefs.ModeAuto, lint(false, "v1"))
		job := s.Scripts.Started()[0]

		s.Scripts.Finish(job.RunID, autopilot.RunEnd{Outcome: autopilot.RunFailed, Error: "It exited with status 2."})
		s.Settle()

		m := only(t, s)
		assert.Equal(t, autopilot.StateDone, m.State)
		assert.Equal(t, autopilot.ReasonFailed, m.Reason)
		assert.Equal(t, "It exited with status 2.", m.Steps[0].Note)
	})
}

func TestAScriptThatAsksWaitsForTheUsersOKEvenOnAutopilot(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := scriptRig(t, autopilotprefs.ModeAuto, lint(true, "v1"))

		m := only(t, s)
		assert.Equal(t, autopilot.StateWaiting, m.State)
		assert.Equal(t, autopilot.BlockScriptAsks, m.BlockedBy)
		assert.Equal(t, `"Lint" asks before each run: run it?`, m.Words)
		assert.Empty(t, s.Scripts.Started(), "Autopilot's mode doesn't answer for the user")

		_, err := s.Service.Approve(t.Context(), []int64{m.ID})
		require.NoError(t, err)
		s.Settle()

		require.Len(t, s.Scripts.Started(), 1, "the user's OK runs it")
		assert.Equal(t, []string{autopilot.ScriptKey(7, "v1")}, s.Scripts.Started()[0].Confirmed,
			"and the job carries the OK, for exactly the script as the user saw it")
	})
}

func TestAskFirstAndAScriptThatAsksTakeOneOK(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := scriptRig(t, autopilotprefs.ModeAsk, lint(true, "v1"))

		m := only(t, s)
		require.Equal(t, autopilot.BlockScriptAsks, m.BlockedBy, "the script's own question, which names it")

		_, err := s.Service.Approve(t.Context(), []int64{m.ID})
		require.NoError(t, err)
		s.Settle()

		assert.Len(t, s.Scripts.Started(), 1, "one OK answers both the routine's mode and the script")
	})
}

func TestAScriptChangedSinceTheOKAsksAgain(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := scriptRig(t, autopilotprefs.ModeAuto, lint(true, "v1"))
		m := only(t, s)

		// The user says to run it, but Autopilot is paused, so it doesn't start yet.
		s.Change(func(c *autopilotprefs.Config) { c.Paused = true })
		_, err := s.Service.Approve(t.Context(), []int64{m.ID})
		require.NoError(t, err)
		s.Settle()
		require.Equal(t, autopilot.BlockPaused, only(t, s).BlockedBy)

		// Meanwhile the user edits the script's command.
		s.MyScripts.Set(lint(true, "v2"))
		s.Change(func(c *autopilotprefs.Config) { c.Paused = false })
		s.Advance(2*time.Minute, false)

		assert.Empty(t, s.Scripts.Started(), "the OK was for the script as it was")
		assert.Equal(t, autopilot.BlockScriptAsks, only(t, s).BlockedBy)

		_, err = s.Service.Approve(t.Context(), []int64{m.ID})
		require.NoError(t, err)
		s.Settle()

		require.Len(t, s.Scripts.Started(), 1)
		assert.Contains(t, s.Scripts.Started()[0].Confirmed, autopilot.ScriptKey(7, "v2"))
	})
}

func TestRunNowStillAsksForAScriptThatAsks(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.Scripts = sim.NewRunner()
		s.MyScripts = &sim.MyScripts{}
		s.MyScripts.Set(lint(true, "v1"))
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.Begin()

		sp := runsLint()
		sp.Trigger = spec.Trigger{Event: catalog.EventManual}
		r := s.Save("Run Lint", sp, autopilotprefs.ModeAuto)
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()

		m, err := s.Service.RunNow(t.Context(), r.ID, s.ItemID(1), false)
		require.NoError(t, err)
		s.Settle()

		assert.True(t, m.Manual)
		assert.Equal(t, autopilot.BlockScriptAsks, only(t, s).BlockedBy, "a click on the routine isn't the script's OK")
		assert.Empty(t, s.Scripts.Started())
	})
}

func TestAScriptTakesNoAgentsSlot(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.Scripts = sim.NewRunner()
		s.MyScripts = &sim.MyScripts{}
		s.MyScripts.Set(lint(false, "v1"))
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.World.Add(sim.Item{Number: 2, Watched: true, Checks: passing, Passed: 4})
		s.Begin()
		s.Change(func(c *autopilotprefs.Config) { c.AgentsAtOnce = 1 })

		agent := s.Save("Fix failing", sim.FixFailing(), autopilotprefs.ModeAuto)
		s.Enable(agent.ID, autopilot.CurrentRun)

		onTwo := runsLint()
		onTwo.Conditions = spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{numberIs(2)}}
		script := s.Save("Run Lint", onTwo, autopilotprefs.ModeAuto)
		s.Enable(script.ID, autopilot.CurrentRun)
		s.Refresh()
		s.World.Fail(1)
		s.Refresh()
		s.Refresh()

		assert.Len(t, s.Runner.Started(), 1, "the agent takes the one slot")
		assert.Len(t, s.Scripts.Started(), 1, "the script on #2 runs beside it")
	})
}

func TestARoutineNamingAScriptThatIsGoneIsntSaved(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.Scripts = sim.NewRunner()
		s.MyScripts = &sim.MyScripts{}
		s.Begin()

		_, err := s.Service.Save(t.Context(), autopilot.Draft{Name: "Run Lint", Spec: runsLint()})
		require.Error(t, err)

		var problems *autopilot.ProblemsError

		require.ErrorAs(t, err, &problems)
		assert.Contains(t, err.Error(), `"Lint" is no longer one of your scripts`)
	})
}

func TestAScriptSavedUnderAnotherNameWaitsToBePickedAgain(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := sim.New(t)
		s.Scripts = sim.NewRunner()
		s.MyScripts = &sim.MyScripts{}
		s.MyScripts.Set(lint(false, "v1"))
		s.World.Add(sim.Item{Number: 1, Watched: true, Checks: passing, Passed: 4})
		s.Begin()

		r := s.Save("Run Lint", runsLint(), autopilotprefs.ModeAuto)

		// Script 7 is now another script than the one the routine picked, as an ID a routine
		// brought from another Mac, or a backup restored, may name: it never runs in its
		// place.
		s.MyScripts.Set(autopilot.MyScript{ID: 7, Name: "Deploy", Command: "make deploy", Look: "v2", Shows: "every clone"})
		s.Enable(r.ID, autopilot.CurrentRun)
		s.Refresh()
		s.Refresh()

		m := only(t, s)
		assert.Equal(t, autopilot.StateBlocked, m.State)
		assert.Equal(t, autopilot.BlockNotReady, m.BlockedBy)
		assert.Equal(t, `The script picked as "Lint" is now "Deploy": pick it again in the routine.`, m.Words)
		assert.Empty(t, s.Scripts.Started())

		_, err := s.Service.Save(t.Context(), autopilot.Draft{Name: "Run Lint again", Spec: runsLint()})

		var problems *autopilot.ProblemsError

		require.ErrorAs(t, err, &problems)
		assert.Contains(t, err.Error(), `The script picked as "Lint" is now "Deploy": pick it again`)

		// Under the name it was picked under again, it runs.
		s.MyScripts.Set(lint(false, "v1"))
		s.Advance(time.Minute, false)

		require.Len(t, s.Scripts.Started(), 1, "its own script runs once it has its name again")
	})
}

func TestStopAllStopsTheScriptsToo(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := scriptRig(t, autopilotprefs.ModeAuto, lint(false, "v1"))
		require.Len(t, s.Scripts.Going(), 1)

		require.NoError(t, s.Service.StopAll(t.Context()))
		s.Settle()

		assert.Empty(t, s.Scripts.Going())
	})
}
