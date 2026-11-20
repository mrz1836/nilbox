package autopilot

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// scriptsRead is the user's own Repo scripts, as a read returns them, or err.
type scriptsRead struct {
	list []MyScript
	err  error
}

func (s scriptsRead) MyScripts(context.Context) ([]MyScript, error) { return s.list, s.err }

func TestTheScriptsRunnerReadsEachScriptStepsRunFromTheLedger(t *testing.T) {
	t.Parallel()

	r := newStatsRig(t, time.Date(2026, time.November, 16, 9, 0, 0, 0, time.UTC))
	launched := r.goingRun(r.match(0, step(catalog.StepScript, "")), runnerScript, runRunning, time.Minute, "", "")
	starting := r.goingRun(r.match(0, step(catalog.StepScript, "")), runnerScript, runStarting, 0, "", "")
	ended := r.goingRun(r.match(0, step(catalog.StepScript, "")), runnerScript, runEnded, time.Hour, "", "")
	agent := r.goingRun(r.match(0, step(catalog.StepAgent, "")), runnerBackground, runRunning, time.Minute, "", "")

	require.NoError(t, r.q.RecordRunLaunch(t.Context(), queries.RecordRunLaunchParams{
		ScriptRunID: 41, UpdatedAt: r.now, ID: launched,
	}))

	e := NewEngine(r.svc, EngineConfig{Items: &quiet{}})

	got, err := e.ScriptRunOf(t.Context(), launched)
	require.NoError(t, err)
	assert.Equal(t, ScriptRun{ScriptRunID: 41}, got, "the run engine's run it started, still going")

	got, err = e.ScriptRunOf(t.Context(), starting)
	require.NoError(t, err)
	assert.Equal(t, ScriptRun{}, got, "none started yet")

	got, err = e.ScriptRunOf(t.Context(), ended)
	require.NoError(t, err)
	assert.True(t, got.Ended)

	_, err = e.ScriptRunOf(t.Context(), 999)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read run 999")

	going, err := e.ScriptRunsGoing(t.Context())
	require.NoError(t, err)
	assert.Equal(t, map[int64]int64{launched: 41, starting: 0}, going,
		"what Stop all stops: the scripts' runs going, an agent's and an ended one left out")
	assert.NotContains(t, going, agent)

	closed := NewEngine(NewService(Config{Pool: testutil.ClosedDB(t)}), EngineConfig{Items: &quiet{}})

	_, err = closed.ScriptRunsGoing(t.Context())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "read the script runs going")
}

func TestTheBuildersScriptsAreTheUsersOwnOrNone(t *testing.T) {
	t.Parallel()

	got, err := NewService(Config{}).MyScripts(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []MyScript{}, got, "without the user's scripts to read")

	got, err = NewService(Config{MyScripts: scriptsRead{}}).MyScripts(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []MyScript{}, got, "never null")

	lint := MyScript{ID: 7, Name: "Lint", Command: "make lint"}
	got, err = NewService(Config{MyScripts: scriptsRead{list: []MyScript{lint}}}).MyScripts(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []MyScript{lint}, got)

	got, err = NewService(Config{MyScripts: scriptsRead{err: errBroken}}).MyScripts(t.Context())
	require.ErrorIs(t, err, errBroken)
	assert.Equal(t, "Your scripts couldn't be read.", uierr.Message(err))
	assert.Equal(t, []MyScript{}, got)
}

// scriptSpec is a routine that runs the script picked as Lint, ID 7.
func scriptSpec() spec.Spec {
	return spec.Spec{
		Version: spec.Version,
		Watch:   spec.Watch{Sources: []spec.Source{{Lists: []catalog.ListSource{catalog.SourceAuthored}}}},
		Trigger: spec.Trigger{Event: catalog.EventAppears},
		Steps:   []spec.Step{{Notify: &spec.NotifyStep{}}, {Script: &spec.ScriptStep{ID: 7, Name: "Lint"}}},
	}
}

func TestAScriptsProblemsAreCheckedOnlyAgainstScriptsThatCanBeRead(t *testing.T) {
	t.Parallel()

	logs := &testutil.Logs{}
	lint := MyScript{ID: 7, Name: "Lint"}

	// scriptProblems are the problems Save and Describe find of sp, which runs no agent and
	// names no prompt, so they are its scripts' alone.
	scriptProblems := func(s *Service, sp spec.Spec) []spec.Problem {
		return draft.Problems(t.Context(), sp, s.liveSetup(), s.offers)
	}

	problems := scriptProblems(NewService(Config{MyScripts: scriptsRead{list: []MyScript{lint}}}), scriptSpec())
	assert.Empty(t, problems, "the script it picked, under its name")

	problems = scriptProblems(NewService(Config{MyScripts: scriptsRead{}}), scriptSpec())
	require.Len(t, problems, 1)
	assert.Equal(t, spec.Problem{
		Part: spec.PartSteps, Field: "[1].script.id", Message: `"Lint" is no longer one of your scripts: pick another`,
	}, problems[0], "the step's own place in the routine")

	problems = scriptProblems(NewService(Config{MyScripts: scriptsRead{err: errBroken}, Logger: logs.Logger()}),
		scriptSpec())
	assert.Empty(t, problems, "scripts that can't be read find none here")
	assert.Contains(t, logs.String(), "could not read the user's scripts to check a routine's")

	assert.Empty(t, scriptProblems(NewService(Config{MyScripts: scriptsRead{}}), notifyOnly()),
		"a routine that runs no script")
}

// notifyOnly is a routine with a Notify me step alone.
func notifyOnly() spec.Spec {
	sp := scriptSpec()
	sp.Steps = sp.Steps[:1]

	return sp
}

func TestAScriptStepWaitsWhileTheUsersScriptsCantBeRead(t *testing.T) {
	t.Parallel()

	logs := &testutil.Logs{}
	svc := NewService(Config{Pool: testutil.DB(t), MyScripts: scriptsRead{err: errBroken}, Logger: logs.Logger()})
	e := NewEngine(svc, EngineConfig{Items: &quiet{}})
	p := &pass{e: e, routines: []*routineRun{{spec: notifyOnly()}}}

	p.scriptsOf(t.Context())
	assert.False(t, p.scriptsFailed, "no routine runs a script, so none is read")

	p.routines = append(p.routines, &routineRun{spec: scriptSpec()})
	p.scriptsOf(t.Context())
	assert.True(t, p.scriptsFailed)
	assert.Contains(t, logs.String(), "Autopilot could not read the user's scripts")

	sr := &stepRun{Step: scriptSpec().Steps[1]}
	assert.Equal(t, wait(BlockNotReady, "Your scripts couldn't be read; it tries again soon."), p.scriptAsks(sr),
		"rather than run one that may ask before each run")

	assert.Equal(t, verdict{}, p.scriptAsks(&stepRun{Step: scriptSpec().Steps[0]}), "a step that runs no script")

	// Read, a script that is gone is let through: its runner refuses it.
	p.scriptsFailed, p.scripts = false, map[int64]MyScript{}
	assert.Equal(t, verdict{}, p.scriptAsks(sr))
}

func TestAScriptThatAsksIsHeldForTheOKOfExactlyTheScriptAsItIs(t *testing.T) {
	t.Parallel()

	p := &pass{scripts: map[int64]MyScript{7: {ID: 7, Name: "Lint", Asks: true, Look: "v1"}}}
	sr := &stepRun{Step: scriptSpec().Steps[1]}

	assert.Equal(t, wait(BlockScriptAsks, `"Lint" asks before each run: run it?`), p.scriptAsks(sr))
	assert.Equal(t, &Hold{Key: ScriptKey(7, "v1"), Reason: HoldScriptAsks}, sr.Hold)
	assert.Equal(t, Result{
		Wait: BlockScriptAsks, WaitWords: `"Lint" asks before each run: run it?`,
		Hold: &Hold{Key: "script:7:v1", Reason: HoldScriptAsks},
	}, ScriptAsks(p.scripts[7]), "the runner's own check asks the same")

	sr.Confirmed = []string{ScriptKey(7, "v1")}
	assert.Equal(t, verdict{}, p.scriptAsks(sr), "the user's OK for it as it is")

	p.scripts[7] = MyScript{ID: 7, Name: "Lint", Asks: true, Look: "v2"}
	assert.Equal(t, BlockScriptAsks, p.scriptAsks(sr).block, "changed since, it asks again")
}
