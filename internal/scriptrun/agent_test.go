package scriptrun

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// agentSpec returns an agent's run, in the app, of a fake tool that plays run in a fresh
// worktree.
func (h *harness) agentSpec(run testutil.FakeRun) Spec {
	h.t.Helper()

	spec := h.spec(run)
	spec.Kind, spec.Title, spec.Source = KindAgent, "Fix checks · acme-org/sdk #12", "autopilot:1"

	return spec
}

// endings records how its one run ended, as its Finish saw it. It keys nothing by the
// run's ID, which Start returns only after the run's own goroutine may have finished it.
type endings struct {
	mu  sync.Mutex
	got Ending
}

// finish is a Finish that records the run's ending and keeps the engine's outcome.
func (e *endings) finish(_ context.Context, end Ending, out Outcome) Outcome {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.got = end

	return out
}

// ending returns how the run ended.
func (e *endings) ending() Ending {
	e.mu.Lock()
	defer e.mu.Unlock()

	return e.got
}

func TestStopKindStopsEveryAgentRunAndNoScriptRun(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	sleep := testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute}

	first, second := h.start(h.agentSpec(sleep)), h.start(h.agentSpec(sleep))
	script := h.start(h.spec(sleep))

	for _, id := range []int64{first, second, script} {
		h.waitReady(id)
	}

	assert.Len(t, h.e.Live(KindAgent), 2, "both agents' runs are going")

	stopped, err := h.e.StopKind(t.Context(), KindAgent)
	require.NoError(t, err)
	assert.Equal(t, 2, stopped)

	assert.Equal(t, StatusStopped, h.ended(first).Status)
	assert.Equal(t, StatusStopped, h.ended(second).Status)
	assert.Equal(t, StatusRunning, h.record(script).Status, "a script's run goes on")
	assert.Empty(t, h.e.Live(KindAgent))

	after := h.start(h.agentSpec(testutil.FakeRun{Stdout: "done\n"}))
	assert.Equal(t, StatusSucceeded, h.ended(after).Status, "a run can start after Stop all")

	require.NoError(t, h.s.Stop(t.Context(), script))
	h.ended(script)
}

func TestALetFinishQuitLetsAnOutlivingRunGoAndTheNextStartFollowsIt(t *testing.T) {
	t.Parallel()

	db, logs := testutil.DB(t), filepath.Join(t.TempDir(), "script-runs")
	cfg := Config{DB: db, Locks: clonelock.New(), LogDir: logs}

	h := newHarness(t, func(c *Config) { *c = cfg })
	sleep := testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute}

	outliving := h.agentSpec(sleep)
	outliving.OutlivesQuit = true
	kept := h.start(outliving)
	stopped := h.start(h.agentSpec(sleep))

	h.waitReady(kept)
	h.waitReady(stopped)

	rec := h.record(kept)

	t.Cleanup(func() { _ = syscall.Kill(-rec.PGID, syscall.SIGKILL) })

	h.e.LetFinish(true)
	h.e.StopAll()

	assert.Equal(t, StatusRunning, h.record(kept).Status, "a run the user lets finish stays recorded as going")
	assert.Equal(t, StatusStopped, h.record(stopped).Status, "a run that can't outlive the quit stops with it")
	require.NoError(t, syscall.Kill(rec.PID, 0), "its program still runs")

	next := NewEngine(Config{DB: db, Locks: clonelock.New(), LogDir: logs})
	next.ladder = fastLadder()

	t.Cleanup(next.StopAll)

	next.reconcile(t.Context())

	got, err := next.store.get(t.Context(), kept)
	require.NoError(t, err)
	assert.Equal(t, StatusOrphaned, got.Status, "the next start follows it again")
	require.Len(t, next.Live(KindAgent), 1)
	assert.True(t, next.Live(KindAgent)[0].Orphaned)
}

// TestTheQuitsThreeAnswers holds the quit dialog's three answers to their backend: Keep
// running quits nothing and lets nothing go, Stop and quit stops every run, and Let them
// finish quits and lets the runs that can outlive the quit go, still recorded as going,
// while one that can't is stopped all the same. The window offers the third while every
// agent's run going says it can outlive the quit (Run.OutlivesQuit).
func TestTheQuitsThreeAnswers(t *testing.T) {
	t.Parallel()

	sleep := testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute}

	// answer starts an outliving agent's run and one that can't outlive the quit, answers
	// the quit with answer, quits, and returns both runs as recorded and how many quits the
	// answer asked for.
	answer := func(t *testing.T, answer func(*Service)) (kept, stopped Record, quits int) {
		t.Helper()

		h := newHarness(t, func(c *Config) { c.Quit = func() { quits++ } })
		outliving := h.agentSpec(sleep)
		outliving.OutlivesQuit = true
		keptID := h.start(outliving)
		stoppedID := h.start(h.agentSpec(sleep))

		h.waitReady(keptID)
		h.waitReady(stoppedID)

		rec := h.record(keptID)

		t.Cleanup(func() { _ = syscall.Kill(-rec.PGID, syscall.SIGKILL) })

		views, err := h.s.Runs(t.Context(), Filter{WithAgents: true})
		require.NoError(t, err)

		outlives := map[int64]bool{}
		for _, v := range views {
			outlives[v.ID] = v.OutlivesQuit
		}

		assert.Equal(t, map[int64]bool{keptID: true, stoppedID: false}, outlives,
			"the window reads which runs can outlive the quit")

		answer(h.s)
		h.e.StopAll()

		return h.record(keptID), h.record(stoppedID), quits
	}

	t.Run("keep running", func(t *testing.T) {
		t.Parallel()

		h := newHarness(t)
		h.e.LetFinish(true)
		h.s.KeepRunning(t.Context())

		assert.False(t, h.e.letFinish.Load(), "Keep running forgets an earlier Let them finish")
	})

	t.Run("stop and quit", func(t *testing.T) {
		t.Parallel()

		kept, stopped, quits := answer(t, func(s *Service) {
			s.e.LetFinish(true) // an earlier answer that didn't quit
			s.QuitAnyway(t.Context())
		})

		assert.Equal(t, 1, quits)
		assert.Equal(t, StatusStopped, kept.Status, "Stop and quit stops a run that could outlive the quit too")
		assert.Equal(t, StatusStopped, stopped.Status)
	})

	t.Run("let them finish", func(t *testing.T) {
		t.Parallel()

		kept, stopped, quits := answer(t, func(s *Service) { s.LetThemFinish(t.Context()) })

		assert.Equal(t, 1, quits)
		assert.Equal(t, StatusRunning, kept.Status, "the run the user lets finish stays recorded as going")
		require.NoError(t, syscall.Kill(kept.PID, 0), "its program still runs")
		assert.Equal(t, StatusStopped, stopped.Status, "a run that can't outlive the quit stops with it")
	})
}

func TestAnOrphanPastItsTimeLimitIsStoppedAtOnce(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	procs := newFakeProcs()
	procs.install(h.e)

	// The group ends at SIGINT, as an agent's does.
	h.e.signal = func(pgid int, sig syscall.Signal) error {
		procs.end(pgid)

		return procs.send(pgid, sig)
	}

	h.e.RegisterVerifier(KindAgent, verifierFunc(func(Record) Verification {
		return Verification{Timeout: time.Minute, OutlivesQuit: true}
	}))

	started := time.Now().Add(-2 * time.Hour)
	procs.run(4545, started)

	id := h.insertAgentRow(h.clone(), 4545, started)
	h.e.reconcile(t.Context())

	rec := h.ended(id)
	assert.Equal(t, StatusFailed, rec.Status, "an orphan past its limit is stopped, as at its limit")
	assert.Contains(t, rec.Summary, "past its limit of 1 minute")
	assert.Equal(t, []syscall.Signal{syscall.SIGINT}, procs.sent())
}

func TestAnOrphanWhoseOutputSaysItIsDoneIsStoppedWithoutCountingItStopped(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	procs := newFakeProcs()
	procs.install(h.e)

	// The group ends at SIGINT, as an agent's does.
	h.e.signal = func(pgid int, sig syscall.Signal) error {
		procs.end(pgid)

		return procs.send(pgid, sig)
	}

	out := &collector{}

	h.e.RegisterVerifier(KindAgent, verifierFunc(func(Record) Verification {
		return Verification{Callbacks: Callbacks{Output: out, Over: func() bool { return strings.Contains(out.String(), "result") }}}
	}))

	started := time.Now().Add(-time.Minute)
	procs.run(4747, started)

	// Before the restart, the agent wrote its result line, and waits on a shell it left.
	id := h.insertAgentRow(h.clone(), 4747, started)
	require.NoError(t, h.e.files.ensureDir())
	testutil.WriteFile(t, h.e.files.log(id), "result\n")

	h.e.reconcile(t.Context())

	rec := h.ended(id)

	assert.Equal(t, []syscall.Signal{syscall.SIGINT}, procs.sent(), "what is left of it is stopped")
	assert.Equal(t, StatusInterrupted, rec.Status, "no one stopped it: its output said it was done")
	assert.Equal(t, "result\n", out.String(), "its log was read from its start")
}

func TestAnOrphanWithinItsTimeLimitGoesOn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		procs := newFakeProcs()
		procs.install(h.e)

		h.e.RegisterVerifier(KindAgent, verifierFunc(func(Record) Verification {
			return Verification{Timeout: time.Hour}
		}))

		// It started 59 minutes before the restart: its limit, counted from its start, is a
		// minute away.
		started := time.Now().Add(-59 * time.Minute)
		procs.run(4646, started)

		id := h.insertAgentRow(h.clone(), 4646, started)
		h.e.reconcile(t.Context())

		time.Sleep(time.Minute - time.Second)
		synctest.Wait()

		assert.Equal(t, StatusOrphaned, h.record(id).Status)
		assert.Empty(t, procs.sent(), "nothing stops it before its limit")

		procs.end(4646)
		time.Sleep(tailEvery)
		synctest.Wait()

		assert.Equal(t, StatusInterrupted, h.record(id).Status)
		assert.Empty(t, procs.sent(), "an orphan that ends within its limit is never signaled")
	})
}

func TestAgentRunsKeepTheirOwnHistory(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.e.keep = retention{runs: 2, bytes: KeepLogBytes}
	h.e.agentKeep = retention{runs: 2, bytes: KeepAgentLogBytes}

	scripts, agents := make([]int64, 0, 3), make([]int64, 0, 3)

	for range 3 {
		scripts = append(scripts, h.ended(h.start(h.spec(testutil.FakeRun{Stdout: "s\n"}))).ID)
		agents = append(agents, h.ended(h.start(h.agentSpec(testutil.FakeRun{Stdout: "a\n"}))).ID)
	}

	h.waitCount(4, "two of each kind are kept")

	kept, err := h.s.Runs(t.Context(), Filter{WithAgents: true})
	require.NoError(t, err)

	ids := make([]int64, 0, len(kept))
	for _, r := range kept {
		ids = append(ids, r.ID)
	}

	assert.ElementsMatch(t, []int64{scripts[1], scripts[2], agents[1], agents[2]}, ids,
		"the agents' runs never push the scripts' out, nor the other way round")
}

func TestAgentRunsAreKeptNoLongerThanTheirLogs(t *testing.T) {
	t.Parallel()

	keepFor := time.Hour

	var mu sync.Mutex

	h := newHarness(t, func(c *Config) {
		c.AgentLogsFor = func(context.Context) time.Duration {
			mu.Lock()
			defer mu.Unlock()

			return keepFor
		}
	})

	script := h.ended(h.start(h.spec(testutil.FakeRun{Stdout: "s\n"}))).ID
	h.ended(h.start(h.agentSpec(testutil.FakeRun{Stdout: "a\n"})))

	h.waitCount(2, "both are kept while within an hour")

	mu.Lock()
	keepFor = time.Nanosecond
	mu.Unlock()

	third := h.ended(h.start(h.spec(testutil.FakeRun{Stdout: "s\n"}))).ID
	h.waitCount(2)

	// The agent's run applies the retention once its end is recorded, a moment after it is
	// let go of, so on a busy machine it may forget itself before the third run is recorded,
	// and the history hands the newest run's ID out again once it forgets that run, as
	// Clear history does: the third run then takes the agent's ID. What went is read from
	// the runs the history holds, never from the agent's run's ID alone.
	recs, err := h.e.store.list(t.Context())
	require.NoError(t, err)

	kinds := make(map[int64]Kind, len(recs))
	for _, rec := range recs {
		kinds[rec.ID] = rec.Kind
	}

	assert.Equal(t, map[int64]Kind{script: KindScript, third: KindScript}, kinds,
		"the agent's run went once its logs' time was up, and a script's run isn't kept by the agents' rule")
}

func TestClearHistoryLeavesTheAgentsRunsToAutopilot(t *testing.T) {
	t.Parallel()

	folders := t.TempDir()
	h := newHarness(t, func(c *Config) { c.Folders = folders })

	script := h.ended(h.start(h.spec(testutil.FakeRun{Stdout: "s\n"}))).ID

	agentFolder := filepath.Join(folders, "0b1c")
	require.NoError(t, os.Mkdir(agentFolder, 0o700))
	testutil.WriteFile(t, filepath.Join(agentFolder, "prompt.md"), "the prompt")

	spec := h.agentSpec(testutil.FakeRun{Stdout: "a\n"})
	spec.Folder = agentFolder
	agent := h.ended(h.start(spec)).ID

	require.NoError(t, h.s.ClearHistory(t.Context()))

	_, err := h.e.store.get(t.Context(), script)
	require.ErrorIs(t, err, ErrNotFound)
	assert.True(t, h.e.LogKept(t.Context(), agent), "the Runs page's Clear history leaves the agents' runs")
	assert.DirExists(t, agentFolder)

	require.NoError(t, h.e.ClearAgentHistory(t.Context()))

	assert.False(t, h.e.LogKept(t.Context(), agent))
	assert.NoDirExists(t, agentFolder, "the run's folder goes with its log")
}

func TestARunsFolderMustBeOneOfTheRunsOwn(t *testing.T) {
	t.Parallel()

	folders := t.TempDir()
	h := newHarness(t, func(c *Config) { c.Folders = folders })

	for _, dir := range []string{
		t.TempDir(), folders, filepath.Join(folders, "a", "b"), filepath.Join(folders, ".."), "relative",
	} {
		spec := h.agentSpec(testutil.FakeRun{})
		spec.Folder = dir

		_, err := h.e.Start(t.Context(), spec)
		require.ErrorIs(t, err, ErrInvalidSpec, dir)
	}
}

func TestAFailedAgentRunTellsFailedNothing(t *testing.T) {
	t.Parallel()

	var told []Failure

	var mu sync.Mutex

	h := newHarness(t, func(c *Config) {
		c.Failed = func(_ context.Context, f Failure) {
			mu.Lock()
			defer mu.Unlock()

			told = append(told, f)
		}
	})

	assert.Equal(t, StatusFailed, h.ended(h.start(h.agentSpec(testutil.FakeRun{Exit: 1}))).Status)
	assert.Equal(t, StatusFailed, h.ended(h.start(h.spec(testutil.FakeRun{Exit: 1}))).Status)

	mu.Lock()
	defer mu.Unlock()

	require.Len(t, told, 1, "only the script's failure is told: Autopilot tells an agent's")
	assert.True(t, strings.HasPrefix(told[0].Title, "A script failed"), told[0].Title)
}

func TestAnAgentRunReadsItsPromptOnStdin(t *testing.T) {
	t.Parallel()

	folders := t.TempDir()
	h := newHarness(t, func(c *Config) { c.Folders = folders })

	folder := filepath.Join(folders, "run")
	got := filepath.Join(t.TempDir(), "got")

	spec := h.agentSpec(testutil.FakeRun{StdinTo: got})
	spec.Folder, spec.Stdin = folder, filepath.Join(folder, "prompt.md")
	spec.Before = func(context.Context) ([]string, error) {
		if err := os.Mkdir(folder, 0o700); err != nil {
			return nil, err
		}

		return nil, os.WriteFile(spec.Stdin, []byte("Fix the checks."), 0o600)
	}

	rec := h.ended(h.start(spec))
	assert.Equal(t, StatusSucceeded, rec.Status)
	assert.Equal(t, folder, rec.Result.Folder, "the record names its folder")

	body, err := os.ReadFile(got) //nolint:gosec // the test's own temp file
	require.NoError(t, err)
	assert.Equal(t, "Fix the checks.", string(body))
	assert.NotContains(t, strings.Join(rec.Argv, " "), "Fix the checks.", "the prompt is on no command line")
}

func TestOverStopsWhatIsLeftOfARunWithoutCountingItStopped(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	out := &collector{}

	var ends endings

	spec := h.agentSpec(testutil.FakeRun{Stdout: "result\n", Sleep: time.Minute})
	spec.Output = out
	spec.Over = func() bool { return strings.Contains(out.String(), "result") }
	spec.Finish = ends.finish

	id := h.start(spec)
	h.ended(id)

	end := ends.ending()
	assert.False(t, end.Stopped, "the run ended by itself, its output said")
	assert.False(t, end.Killed, "SIGINT ended it")
}

func TestEndingSaysTheGroupWasKilled(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var ends endings

	ready := filepath.Join(t.TempDir(), "ready")
	spec := h.agentSpec(testutil.FakeRun{
		IgnoreSignals: true, Sleep: time.Minute, Files: []testutil.FakeFile{{Path: ready}},
	})
	spec.Finish = ends.finish

	id := h.start(spec)

	require.Eventually(t, func() bool {
		_, err := os.Stat(ready)

		return err == nil
	}, wait, poll, "the fake ignores signals")
	require.NoError(t, h.s.Stop(t.Context(), id))

	h.ended(id)
	assert.True(t, ends.ending().Killed, "only SIGKILL ended it")
}

func TestTheRunsPageLeavesTheAgentsRunsOut(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	script := h.ended(h.start(h.spec(testutil.FakeRun{}))).ID
	agent := h.ended(h.start(h.agentSpec(testutil.FakeRun{}))).ID

	ids := func(f Filter) []int64 {
		runs, err := h.s.Runs(t.Context(), f)
		require.NoError(t, err)

		out := make([]int64, 0, len(runs))
		for _, r := range runs {
			out = append(out, r.ID)
		}

		return out
	}

	assert.Equal(t, []int64{script}, ids(Filter{}), "the Runs page lists every kind but an agent's")
	assert.ElementsMatch(t, []int64{script, agent}, ids(Filter{WithAgents: true}), "the quit names them all")
	assert.Equal(t, []int64{agent}, ids(Filter{Kinds: []Kind{KindAgent}}))
}

func TestAnAgentRunHoldsItsWorktree(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	spec := h.agentSpec(testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute})
	id := h.start(spec)
	h.waitReady(id)

	_, held, ok := h.locks.TryLock(spec.Dir, clonelock.Holder{Kind: clonelock.KindWorktree})
	require.False(t, ok)
	assert.Equal(t, clonelock.KindAgent, held.Kind)
	assert.Equal(t, "An agent in the background is using acme-org/sdk right now.", held.Sentence())

	other := h.agentSpec(testutil.FakeRun{Stdout: "b\n"})
	assert.Equal(t, StatusSucceeded, h.ended(h.start(other)).Status, "another worktree of the clone runs beside it")

	require.NoError(t, h.s.Stop(t.Context(), id))
	h.ended(id)
}

// insertAgentRow records an agent's run as going, as an earlier start of the app did,
// with its process.
func (h *harness) insertAgentRow(dir string, pid int, startedAt time.Time) int64 {
	h.t.Helper()

	ctx := h.t.Context()

	id, err := h.e.store.insert(ctx, started{
		spec:    Spec{Kind: KindAgent, Title: "Fix checks", Repo: testRepo(), Dir: dir, Argv: []string{"/usr/bin/true"}},
		command: "true", runsIn: RunsInApp, at: startedAt, result: Result{},
	})
	require.NoError(h.t, err)
	require.NoError(h.t, h.e.store.setProcess(ctx, id, pid, pid, startedAt))

	return id
}

// verifierFunc is a Verifier from a function.
type verifierFunc func(Record) Verification

// Verify calls f.
func (f verifierFunc) Verify(run Record) Verification {
	return f(run)
}
