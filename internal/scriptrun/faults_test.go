package scriptrun

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestAHistoryThatCantBeReadSaysSo(t *testing.T) {
	t.Parallel()

	logs := &testutil.Logs{}
	e := NewEngine(Config{DB: testutil.ClosedDB(t), LogDir: filepath.Join(t.TempDir(), "script-runs"), Logger: logs.Logger()})
	s := NewService(e)
	ctx := t.Context()

	_, err := s.Runs(ctx, Filter{})
	require.ErrorContains(t, err, "read the runs")

	_, err = s.Run(ctx, 1)
	require.ErrorContains(t, err, "read the run")

	_, err = s.Output(ctx, 1, 0, 0)
	require.ErrorContains(t, err, "read the run")

	_, err = s.RunAgain(ctx, 1)
	require.ErrorContains(t, err, "read the run")

	require.ErrorContains(t, s.OpenInTerminal(ctx, 1), "read the run")
	require.ErrorContains(t, s.ShowLog(ctx, 1), "read the run")
	require.ErrorContains(t, s.MarkSeen(ctx, 1), "record that the run was seen")
	require.ErrorContains(t, s.ClearHistory(ctx), "read the runs")
	require.ErrorContains(t, e.ClearAgentHistory(ctx), "read the runs")
	assert.False(t, e.LogKept(ctx, 1))

	dir := testutil.RealTempDir(t)
	path, env := testutil.FakeTool(t, "tool", testutil.FakeRun{})

	_, err = e.Start(ctx, Spec{
		Kind: KindScript, Title: "Lint", Repo: testRepo(), Dir: dir, Argv: []string{path}, Env: append(os.Environ(), env),
	})
	require.ErrorContains(t, err, "record the run", "a run that can't be recorded never starts")
	assert.Empty(t, e.Live(KindScript))

	_, held := e.locks.Holder(dir)
	assert.False(t, held, "its clone is let go")

	e.reconcile(ctx)
	e.prune(ctx)
	assert.Contains(t, logs.String(), "couldn't read the runs left from before the app started")
	assert.Contains(t, logs.String(), "couldn't read the runs to keep the history short")
}

func TestARunTheHistoryCantForgetIsKept(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	h := newHarness(t, func(c *Config) { c.DB = db })

	id := h.ended(h.start(h.spec(testutil.FakeRun{Stdout: "done\n"}))).ID

	_, err := db.ExecContext(t.Context(), `CREATE TRIGGER refuse_forget BEFORE DELETE ON script_runs
		BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)
	require.NoError(t, err)

	require.ErrorContains(t, h.s.ClearHistory(t.Context()), "forget the oldest runs")

	_, err = h.e.store.get(t.Context(), id)
	require.NoError(t, err, "the run is still in the history")

	h.e.keep = retention{runs: 0, bytes: 0}
	h.e.prune(t.Context())
	assert.Contains(t, h.logs.String(), "couldn't keep the history short")
}

func TestStopKindSaysWhichRunsWouldntStop(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var refusing atomic.Bool

	refusing.Store(true)

	held := h.agentSpec(testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute})
	held.Progress = func() Progress {
		if refusing.Load() {
			return Progress{StopRefused: "It's pushing the branch."}
		}

		return Progress{}
	}

	stoppable := h.agentSpec(testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute})

	heldID, stoppableID := h.start(held), h.start(stoppable)
	h.waitReady(heldID)
	h.waitReady(stoppableID)

	stopped, err := h.e.StopKind(t.Context(), KindAgent)
	require.ErrorIs(t, err, ErrStopRefused)
	assert.Equal(t, "Some runs couldn't be stopped.", uierr.Message(err))
	assert.Equal(t, 1, stopped, "the run that could stop did")

	assert.Equal(t, StatusStopped, h.ended(stoppableID).Status)
	assert.Equal(t, StatusRunning, h.record(heldID).Status, "the run its kind holds back goes on")

	refusing.Store(false)
	require.NoError(t, h.s.Stop(t.Context(), heldID))
	assert.Equal(t, StatusStopped, h.ended(heldID).Status)
}

func TestTheNextStartRemovesOnlyTheFoldersNoRunNames(t *testing.T) {
	t.Parallel()

	folders := t.TempDir()
	clk := testutil.NewClock(time.Now())
	h := newHarness(t, func(c *Config) { c.Folders, c.Clock = folders, clk })

	named := filepath.Join(folders, "named")
	require.NoError(t, os.Mkdir(named, 0o700))

	spec := h.agentSpec(testutil.FakeRun{Stdout: "done\n"})
	spec.Folder = named
	h.ended(h.start(spec))

	forgotten, fresh, file := filepath.Join(folders, "forgotten"), filepath.Join(folders, "fresh"), filepath.Join(folders, "notes")
	require.NoError(t, os.Mkdir(forgotten, 0o700))
	require.NoError(t, os.Mkdir(fresh, 0o700))
	testutil.WriteFile(t, file, "not a run's folder")

	// An hour on, only fresh was made a moment ago, as a run's Before makes its folder.
	later := clk.Advance(folderSweepAfter + time.Minute)
	require.NoError(t, os.Chtimes(fresh, later, later))

	h.e.reconcile(t.Context())

	assert.DirExists(t, named, "a run the history holds keeps its folder")
	assert.NoDirExists(t, forgotten, "a folder no run names goes once it is an hour old")
	assert.DirExists(t, fresh, "a folder made a moment ago may be a run's that is just starting")
	assert.FileExists(t, file, "only folders are swept")
}

func TestTheFoldersSweepSaysWhatItCantRead(t *testing.T) {
	t.Parallel()

	notAFolder := filepath.Join(t.TempDir(), "agent-runs")
	testutil.WriteFile(t, notAFolder, "a file")

	h := newHarness(t, func(c *Config) { c.Folders = notAFolder })
	h.e.reconcile(t.Context())
	assert.Contains(t, h.logs.String(), "couldn't read the runs' own folders")

	gone := newHarness(t, func(c *Config) { c.Folders = filepath.Join(t.TempDir(), "never-made") })
	gone.e.reconcile(t.Context())
	assert.NotContains(t, gone.logs.String(), "couldn't read the runs' own folders", "no folder yet is nothing to sweep")
}

func TestAgentLogsAreKeptForTheirDefaultUnlessTheSettingSaysOtherwise(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		setting func(context.Context) time.Duration
		want    time.Duration
	}{
		"no setting":        {want: DefaultAgentLogsFor},
		"a setting of none": {setting: func(context.Context) time.Duration { return 0 }, want: DefaultAgentLogsFor},
		"a week":            {setting: func(context.Context) time.Duration { return 7 * 24 * time.Hour }, want: 7 * 24 * time.Hour},
	} {
		e := NewEngine(Config{AgentLogsFor: tc.setting})
		assert.Equal(t, tc.want, e.agentLogsFor(t.Context()), name)
	}
}

func TestARunThatCantOpenItsInputOrItsLogNeverStarts(t *testing.T) {
	t.Parallel()

	folders := t.TempDir()
	h := newHarness(t, func(c *Config) { c.Folders = folders })

	spec := h.agentSpec(testutil.FakeRun{})
	spec.Folder = filepath.Join(folders, "run")
	spec.Stdin = filepath.Join(spec.Folder, "prompt.md") // never written

	id, err := h.e.Start(t.Context(), spec)
	require.Error(t, err)
	assert.Equal(t, "The run couldn't start.", uierr.Message(err))

	rec := h.ended(id)
	assert.Equal(t, StatusFailed, rec.Status)
	assert.Contains(t, rec.Summary, "Open the run's input", "the record says why it couldn't start")

	// The next run's log is to go where something else already is: it is never opened in
	// that thing's place.
	next := id + 1
	require.NoError(t, os.MkdirAll(filepath.Join(h.e.files.log(next), "kept"), 0o700))

	id, err = h.e.Start(t.Context(), h.spec(testutil.FakeRun{}))
	require.Error(t, err)
	require.Equal(t, next, id)
	assert.Contains(t, h.ended(id).Summary, "Create the run's log")
}

func TestALogFolderThatCantBeMadeFailsTheStart(t *testing.T) {
	t.Parallel()

	blocked := filepath.Join(t.TempDir(), "file")
	testutil.WriteFile(t, blocked, "in the way")

	h := newHarness(t, func(c *Config) { c.LogDir = filepath.Join(blocked, "script-runs") })

	id, err := h.e.Start(t.Context(), h.spec(testutil.FakeRun{}))
	require.Error(t, err)
	assert.Contains(t, h.ended(id).Summary, "Create the runs' log folder")

	h.e.reconcile(t.Context())
	assert.Contains(t, h.logs.String(), "couldn't tidy the runs' logs", "the next start says it can't read the logs either")
}
