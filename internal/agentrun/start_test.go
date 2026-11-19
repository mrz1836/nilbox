package agentrun

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestStartRefusesAPlanARunCantStartFrom(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	for name, tc := range map[string]struct {
		edit func(*Plan)
		want string
	}{
		"no run in the ledger":   {edit: func(p *Plan) { p.RunID = 0 }, want: "no run in the ledger"},
		"a made-up session":      {edit: func(p *Plan) { p.SessionID = "../elsewhere" }, want: "the session ID"},
		"a relative worktree":    {edit: func(p *Plan) { p.Dir = "pr-12" }, want: "must be absolute paths"},
		"no command":             {edit: func(p *Plan) { p.Argv = nil }, want: "must be absolute paths"},
		"a relative binary":      {edit: func(p *Plan) { p.Argv = []string{"claude"} }, want: "must be absolute paths"},
		"a folder of another":    {edit: func(p *Plan) { p.Folder = filepath.Join(r.folders, "other") }, want: "isn't named by its session"},
		"no prompt":              {edit: func(p *Plan) { p.Prompt = "" }, want: "no prompt"},
		"no time limit":          {edit: func(p *Plan) { p.Fence.Timeout = 0 }, want: "no time limit"},
		"a time limit gone past": {edit: func(p *Plan) { p.Fence.Timeout = -time.Second }, want: "no time limit"},
	} {
		plan := r.plan(testutil.FakeRun{})
		tc.edit(&plan)

		_, err := r.runner.Start(t.Context(), plan)
		require.ErrorIs(t, err, ErrInvalidPlan, name)
		require.ErrorContains(t, err, tc.want, name)
		assert.NoDirExists(t, plan.Folder, "%s: nothing is written for a plan that can't run", name)
	}

	plan := r.planFor("an-agent-nobody-ships", testutil.FakeRun{})
	_, err := r.runner.Start(t.Context(), plan)
	require.Error(t, err, "an agent the registry doesn't know has no background half")
	assert.Empty(t, r.runner.Folders())
}

func TestStartFailsWhenTheRunsFolderCantBeWritten(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	blocked := r.plan(testutil.FakeRun{})
	require.NoError(t, os.WriteFile(blocked.Folder, []byte("a file, not a folder"), 0o600))

	_, err := r.runner.Start(t.Context(), blocked)
	require.ErrorContains(t, err, "make the run's folder")

	noPrompt := r.plan(testutil.FakeRun{})
	require.NoError(t, os.MkdirAll(noPrompt.PromptPath(), 0o700))

	_, err = r.runner.Start(t.Context(), noPrompt)
	require.ErrorContains(t, err, "write the run's prompt")

	for _, p := range []Plan{blocked, noPrompt} {
		live, aliveErr := r.runner.Alive(t.Context(), p.RunID)
		require.NoError(t, aliveErr)
		assert.Equal(t, LiveNever, live, "a run whose folder can't be written is never recorded")
		assert.Empty(t, r.sessions.of(p.SessionID))
	}
}

// heldStart starts plan on another goroutine, its Before holding it until its context
// ends, and returns once the run is under its worktree's lock, with what its Start returns.
func heldStart(t *testing.T, r *rig, plan Plan) <-chan error {
	t.Helper()

	entered := make(chan struct{})
	plan.Before = func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()

		return ctx.Err()
	}

	started := make(chan error, 1)

	go func() {
		_, err := r.runner.Start(context.WithoutCancel(t.Context()), plan)
		started <- err
	}()

	select {
	case <-entered:
	case <-time.After(waitFor):
		require.FailNow(t, "the run never reached its Before")
	}

	return started
}

func TestARunStillStartingIsAliveAndStopCallsItOff(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	plan := r.plan(testutil.FakeRun{Stdout: string(fixture(t, "success-structured.jsonl"))})
	started := heldStart(t, r, plan)

	live, err := r.runner.Alive(t.Context(), plan.RunID)
	require.NoError(t, err)
	assert.Equal(t, LiveAlive, live, "a run still starting is alive")

	assert.Equal(t, []Folder{{LedgerRunID: plan.RunID, Agent: agent.Claude, Dir: plan.Dir}}, r.runner.Folders(),
		"its worktree is in use before it has a run in the history or a process")

	_, followed := r.runner.Progress(plan.RunID)
	assert.False(t, followed, "nothing of its stream is read yet")

	require.NoError(t, r.runner.Stop(t.Context(), plan.RunID))
	require.ErrorIs(t, <-started, context.Canceled, "its start is called off")

	live, err = r.runner.Alive(t.Context(), plan.RunID)
	require.NoError(t, err)
	assert.Equal(t, LiveNever, live, "a run called off before it was recorded never was")
	assert.Empty(t, r.runner.Folders())
	assert.NoFileExists(t, plan.PromptPath())
}

func TestStopAllCallsOffARunStillStarting(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	plan := r.plan(testutil.FakeRun{})
	started := heldStart(t, r, plan)

	require.NoError(t, r.runner.StopAll(t.Context()))
	require.ErrorIs(t, <-started, context.Canceled)

	after := r.plan(testutil.FakeRun{Stdout: string(fixture(t, "success-structured.jsonl"))})
	r.start(after)
	assert.Equal(t, StatusDone, r.end(after.RunID).Outcome.Status, "the engine stays open for later starts")
}

func TestARunTheRunnerNeverHeardOf(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	require.ErrorIs(t, r.runner.Stop(t.Context(), 404), ErrNoRun)

	_, ok := r.runner.ScriptRun(t.Context(), 404)
	assert.False(t, ok)

	_, ok = r.runner.Progress(404)
	assert.False(t, ok)

	require.NoError(t, r.runner.Wait(t.Context(), 404), "there is nothing to wait for")
}

func TestARunThatCantStartTellsOnlyItsStart(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	plan := r.plan(testutil.FakeRun{})
	plan.Argv = []string{filepath.Join(t.TempDir(), "claude-gone")}

	id, err := r.runner.Start(t.Context(), plan)
	require.Error(t, err, "the start's error says it couldn't start")
	require.Positive(t, id, "it was recorded before it couldn't start")

	assert.Equal(t, scriptrun.StatusFailed, r.record(id).Status)
	assert.Empty(t, r.sessions.of(plan.SessionID), "it never became a session")
	require.NoError(t, r.runner.Wait(t.Context(), plan.RunID), "Wait returns for a start that failed")

	r.mu.Lock()
	_, told := r.ends[plan.RunID]
	r.mu.Unlock()
	assert.False(t, told, "Ended hears only of runs that ran")

	live, err := r.runner.Alive(t.Context(), plan.RunID)
	require.NoError(t, err)
	assert.Equal(t, LiveGone, live, "the history holds it as ended")

	page, err := r.runner.EventsBefore(t.Context(), plan.RunID, 0)
	require.NoError(t, err)
	assert.True(t, page.Done)
	require.NotNil(t, page.Ending)
	assert.Contains(t, page.Ending.Summary, "It couldn't start", "with no outcome of its own, the record says how it ended")
	assert.NotNil(t, page.Ending.Changed)
	assert.NotNil(t, page.Ending.Todos)
}

func TestALaterRunnerReadsTheRunsFromTheHistory(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	plan := r.plan(testutil.FakeRun{Stdout: lines(t, "success-structured.jsonl", 2), Sleep: time.Minute})
	id := r.start(plan)

	// Another runner over the same history, which follows none of the runs.
	_, _, later := r.build()

	live, err := later.Alive(t.Context(), plan.RunID)
	require.NoError(t, err)
	assert.Equal(t, LiveAlive, live, "the history holds it as going")

	got, ok := later.ScriptRun(t.Context(), plan.RunID)
	require.True(t, ok)
	assert.Equal(t, id, got)

	require.NoError(t, r.runner.Stop(t.Context(), plan.RunID))
	assert.Equal(t, StatusStopped, r.end(plan.RunID).Outcome.Status)

	live, err = later.Alive(t.Context(), plan.RunID)
	require.NoError(t, err)
	assert.Equal(t, LiveGone, live)
}

func TestARunnerWhoseHistoryCantBeReadKnowsNothing(t *testing.T) {
	t.Parallel()

	engine := scriptrun.NewEngine(scriptrun.Config{DB: testutil.ClosedDB(t), LogDir: t.TempDir()})
	runner := New(Config{Engine: engine, Runs: scriptrun.NewService(engine)})

	live, err := runner.Alive(t.Context(), 1)
	require.Error(t, err)
	assert.Equal(t, LiveUnknown, live, "neither alive nor gone: the history couldn't say")

	_, ok := runner.ScriptRun(t.Context(), 1)
	assert.False(t, ok)

	_, err = runner.EventsBefore(t.Context(), 1, 0)
	require.ErrorIs(t, err, ErrNoRun)
}
