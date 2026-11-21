package core

import (
	"context"
	"errors"
	"maps"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/scripts"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// errBroken is a run engine that broke.
var errBroken = errors.New("the run engine broke")

// fakeMine is the user's scripts.
type fakeMine []autopilot.MyScript

func (f fakeMine) MyScripts(context.Context) ([]autopilot.MyScript, error) { return f, nil }

// fakeClones finds every item's clone at dir, or none when dir is "".
type fakeClones struct{ dir string }

func (f fakeClones) CloneTarget(context.Context, int64) (launcher.CloneTarget, error) {
	return launcher.CloneTarget{Cloned: f.dir != "", Dir: f.dir}, nil
}

// fakeStarter records the runs it is asked to start, and answers with err.
type fakeStarter struct {
	mu   sync.Mutex
	runs []scripts.RoutineRun
	id   int64
	err  error
}

func (f *fakeStarter) Run(_ context.Context, req scripts.RoutineRun) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.runs = append(f.runs, req)

	return f.id, f.err
}

// fakeHistory is the run engine's history: its runs by ID, and the ones stopped.
type fakeHistory struct {
	mu      sync.Mutex
	runs    map[int64]scriptrun.Run
	stopped []int64
}

func (f *fakeHistory) Run(_ context.Context, id int64) (scriptrun.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	r, ok := f.runs[id]
	if !ok {
		return scriptrun.Run{}, scriptrun.ErrNotFound
	}

	return r, nil
}

func (f *fakeHistory) Stop(_ context.Context, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.stopped = append(f.stopped, id)

	return nil
}

// fakeLedger is the engine's ledger: each step's run's script run, and the ends told.
type fakeLedger struct {
	mu    sync.Mutex
	runs  map[int64]int64
	ended []autopilot.RunEnd
}

func (f *fakeLedger) ScriptRunOf(_ context.Context, runID int64) (autopilot.ScriptRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return autopilot.ScriptRun{ScriptRunID: f.runs[runID]}, nil
}

func (f *fakeLedger) ScriptRunsGoing(context.Context) (map[int64]int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := map[int64]int64{}
	maps.Copy(out, f.runs)

	return out, nil
}

func (f *fakeLedger) RunEnded(end autopilot.RunEnd) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.ended = append(f.ended, end)
}

// scriptRig is the scripts' runner over fakes, with one script, Lint, which asks when asks
// says so.
type scriptRig struct {
	steps   scriptSteps
	starter *fakeStarter
	history *fakeHistory
	ledger  *fakeLedger
}

func newScriptRig(asks bool, dir string) *scriptRig {
	r := &scriptRig{
		starter: &fakeStarter{id: 41}, history: &fakeHistory{runs: map[int64]scriptrun.Run{}},
		ledger: &fakeLedger{runs: map[int64]int64{}},
	}
	r.steps = scriptSteps{
		starter: r.starter, mine: fakeMine{{ID: 7, Name: "Lint", Command: "make lint", Asks: asks, Look: "v1"}},
		clones: fakeClones{dir: dir}, history: r.history, ledger: func() scriptLedger { return r.ledger },
	}

	return r
}

// lintJob is the job of Lint's step, on run 5, with the OKs confirmed.
func lintJob(confirmed ...string) autopilot.Job {
	return autopilot.Job{
		RunID: 5, Step: spec.Step{Script: &spec.ScriptStep{ID: 7, Name: "Lint"}}, Confirmed: confirmed,
		Item: autopilot.ItemRef{ID: 1, Repo: "acme-org/example-repo"},
	}
}

func TestAScriptThatAsksRunsOnlyWithTheUsersOKOfItAsItIs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		asks      bool
		confirmed []string
		runs      bool
	}{
		{name: "a script that doesn't ask", runs: true},
		{name: "a script that asks, with no OK", asks: true},
		{name: "a script that asks, OK'd as it is", asks: true, confirmed: []string{autopilot.ScriptKey(7, "v1")}, runs: true},
		{name: "a script that asks, OK'd as it was", asks: true, confirmed: []string{autopilot.ScriptKey(7, "v0")}},
		{name: "a script that asks, another's OK", asks: true, confirmed: []string{autopilot.ScriptKey(8, "v1")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newScriptRig(tt.asks, "~/projects/example-repo")

			res, err := r.steps.Dispatch(t.Context(), lintJob(tt.confirmed...))
			require.NoError(t, err)

			if !tt.runs {
				assert.Equal(t, autopilot.BlockScriptAsks, res.Wait)
				require.NotNil(t, res.Hold)
				assert.Equal(t, autopilot.ScriptKey(7, "v1"), res.Hold.Key, "the OK it waits for names the script as it is")
				assert.Empty(t, r.starter.runs, "nothing runs")

				return
			}

			assert.True(t, res.Running)
			require.NotNil(t, res.Launched)
			assert.Equal(t, int64(41), res.Launched.ScriptRunID)
			require.Len(t, r.starter.runs, 1)
			assert.Equal(t, scripts.RoutineRun{ID: 7, Dir: "~/projects/example-repo", Look: "v1"},
				scripts.RoutineRun{ID: r.starter.runs[0].ID, Dir: r.starter.runs[0].Dir, Look: r.starter.runs[0].Look},
				"it runs the script as it was checked, in the item's clone")
		})
	}
}

func TestAScriptsEndGoesToTheEngine(t *testing.T) {
	t.Parallel()

	r := newScriptRig(false, "~/projects/example-repo")

	_, err := r.steps.Dispatch(t.Context(), lintJob())
	require.NoError(t, err)

	r.starter.runs[0].Ended(t.Context(), 41, scripts.RoutineEnd{
		Outcome: scriptrun.Outcome{Status: scriptrun.StatusFailed, Summary: "It exited with status 2."},
	})

	require.Len(t, r.ledger.ended, 1)
	assert.Equal(t, autopilot.RunEnd{
		RunID: 5, Outcome: autopilot.RunFailed, Summary: "It exited with status 2.", Error: "It exited with status 2.",
	}, r.ledger.ended[0])
}

func TestAScriptsEndInTheEnginesWords(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status scriptrun.Status
		quit   bool
		want   autopilot.RunOutcome
	}{
		{name: "it succeeded", status: scriptrun.StatusSucceeded, want: autopilot.RunDone},
		{name: "it failed", status: scriptrun.StatusFailed, want: autopilot.RunFailed},
		{name: "the user stopped it", status: scriptrun.StatusStopped, want: autopilot.RunStopped},
		{name: "the app quit", status: scriptrun.StatusStopped, quit: true, want: autopilot.RunInterrupted},
		{name: "a crash cut it off", status: scriptrun.StatusInterrupted, want: autopilot.RunInterrupted},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, runEndOf(5, tt.status, "", tt.quit).Outcome)
		})
	}
}

func TestAScriptThatCantStartWaitsOrFails(t *testing.T) {
	t.Parallel()

	busy := &clonelock.BusyError{Holder: clonelock.Holder{Kind: clonelock.KindRelease, Repo: "acme-org/example-repo"}}

	tests := []struct {
		name  string
		dir   string
		err   error
		id    int64
		wait  autopilot.Blocker
		fails error
	}{
		{name: "no clone", wait: autopilot.BlockNoClone},
		{name: "a busy clone", dir: "~/projects/example-repo", err: uierr.New(busy.Error(), busy), wait: autopilot.BlockBusyWorktree},
		{
			name: "Scripts turned off", dir: "~/projects/example-repo", err: uierr.New("off", scripts.ErrCantRun),
			wait: autopilot.BlockNotReady,
		},
		{name: "the app quitting", dir: "~/projects/example-repo", err: scriptrun.ErrClosed, fails: autopilot.ErrClosed},
		{
			name: "a script that doesn't show there", dir: "~/projects/example-repo", err: scripts.ErrNotFound,
			fails: scripts.ErrNotFound,
		},
		{
			name: "a run recorded that couldn't start", dir: "~/projects/example-repo", id: 41,
			err: uierr.New("busy", busy), fails: clonelock.ErrBusy,
		},
		{name: "anything else", dir: "~/projects/example-repo", err: errBroken, fails: errBroken},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newScriptRig(false, tt.dir)
			r.starter.err, r.starter.id = tt.err, tt.id

			res, err := r.steps.Dispatch(t.Context(), lintJob())
			if tt.fails != nil {
				require.ErrorIs(t, err, tt.fails)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wait, res.Wait)
			assert.NotEmpty(t, res.WaitWords)
		})
	}
}

func TestAScriptThatIsGoneFailsItsStep(t *testing.T) {
	t.Parallel()

	r := newScriptRig(false, "~/projects/example-repo")
	r.steps.mine = fakeMine{}

	_, err := r.steps.Dispatch(t.Context(), lintJob())
	require.ErrorIs(t, err, scripts.ErrNotFound)
	assert.Equal(t, `"Lint" is no longer one of your scripts: pick another in the routine.`, uierr.Message(err))
}

func TestTheScriptsRunnerStopsAndFollowsTheRunEngine(t *testing.T) {
	t.Parallel()

	r := newScriptRig(false, "~/projects/example-repo")
	r.ledger.runs = map[int64]int64{5: 41, 6: 42, 7: 0, 8: 43}
	r.history.runs = map[int64]scriptrun.Run{
		41: {ID: 41, Status: scriptrun.StatusRunning},
		42: {ID: 42, Status: scriptrun.StatusSucceeded, Summary: "It exited with status 0."},
	}

	live, err := r.steps.Alive(t.Context(), 5)
	require.NoError(t, err)
	assert.Equal(t, autopilot.LiveAlive, live, "a run going")

	live, err = r.steps.Alive(t.Context(), 6)
	require.NoError(t, err)
	assert.Equal(t, autopilot.LiveAlive, live, "one that ended unheard is alive until its end, told now, is applied")
	require.Len(t, r.ledger.ended, 1)
	assert.Equal(t, autopilot.RunDone, r.ledger.ended[0].Outcome)

	live, err = r.steps.Alive(t.Context(), 7)
	require.NoError(t, err)
	assert.Equal(t, autopilot.LiveGone, live, "a start never recorded may have run: gone, never never")

	live, err = r.steps.Alive(t.Context(), 8)
	require.NoError(t, err)
	assert.Equal(t, autopilot.LiveGone, live, "a run the history no longer holds")

	require.NoError(t, r.steps.Stop(t.Context(), 5))
	require.ErrorIs(t, r.steps.Stop(t.Context(), 7), autopilot.ErrNoRun)
	require.NoError(t, r.steps.StopAll(t.Context()))
	assert.ElementsMatch(t, []int64{41, 41, 42, 43}, r.history.stopped)
}

func TestTheScriptsRunnerWaitsForTheEngine(t *testing.T) {
	t.Parallel()

	r := newScriptRig(false, "~/projects/example-repo")
	r.steps.ledger = func() scriptLedger { return nil }

	live, err := r.steps.Alive(t.Context(), 5)
	require.NoError(t, err)
	assert.Equal(t, autopilot.LiveUnknown, live)
	require.ErrorIs(t, r.steps.Stop(t.Context(), 5), autopilot.ErrNoEngine)
	require.NoError(t, r.steps.StopAll(t.Context()))
}
