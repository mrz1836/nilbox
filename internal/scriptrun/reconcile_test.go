package scriptrun

import (
	"context"
	"database/sql"
	"os"
	"slices"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// startedLong is when the runs left behind started, as their processes report it.
func startedLong() time.Time {
	return time.Date(2026, 11, 10, 8, 0, 0, 0, time.UTC)
}

func TestReconcileFollowsAnOrphanedRunUntilItsGroupEnds(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	procs := newFakeProcs()
	procs.install(h.e)
	procs.run(4242, startedLong())

	dir := h.clone()
	id := h.insertRow(RunsInApp, dir, 4242, 4242, startedLong())

	h.e.reconcile(t.Context())

	rec := h.record(id)
	assert.Equal(t, StatusOrphaned, rec.Status, "its group is alive, led by the process the run started")
	assert.Equal(t, orphanRunning, rec.Summary)
	assert.True(t, h.e.Active(), "quitting would stop it")
	assert.True(t, h.e.InUse("mage-x"), "its tool is in use")

	holder, held := h.locks.Holder(dir)
	require.True(t, held, "it holds its clone under a placeholder lock")
	assert.Equal(t, clonelock.KindOrphan, holder.Kind)
	assert.Equal(t, id, holder.RunID)
	assert.Equal(t, "acme-org/sdk", holder.Repo)

	run, err := h.s.Run(t.Context(), id)
	require.NoError(t, err)
	assert.True(t, run.CanStop, "an orphaned run can be stopped")

	procs.end(4242)

	rec = h.ended(id)
	assert.Equal(t, StatusInterrupted, rec.Status, "its exit status can't be known")
	assert.Equal(t, orphanEnded, rec.Summary)
	assert.Equal(t, map[string]any{"a": float64(1)}, rec.Result.Intent, "its intent is kept")

	_, held = h.locks.Holder(dir)
	assert.False(t, held)
}

func TestReconcileKeepsAToolsOrphanedUpdateTellingItUpdates(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	procs := newFakeProcs()
	procs.install(h.e)
	procs.run(4343, startedLong())

	dir := h.clone()
	id, err := h.e.store.insert(t.Context(), started{
		spec:    Spec{Kind: KindUpdate, Title: "Update magex", Dir: dir, Argv: []string{"/usr/bin/true"}},
		command: "magex update:install", runsIn: RunsInApp, at: time.Now(), result: Result{Tool: "mage-x"},
	})
	require.NoError(t, err)
	require.NoError(t, h.e.store.setProcess(t.Context(), id, 4343, 4343, startedLong()))

	h.e.reconcile(t.Context())

	assert.Equal(t, id, h.e.Updating("mage-x"), "however it started, the tool is updating")
	assert.False(t, h.e.InUse("mage-x"))

	holder, held := h.locks.Holder(dir)
	require.True(t, held, "it holds its copy's folder, which an install then can't take")
	assert.Equal(t, clonelock.KindOrphan, holder.Kind)

	procs.end(4343)
	h.ended(id)

	assert.Zero(t, h.e.Updating("mage-x"))
}

func TestReconcileTellsOrphanedFromInterrupted(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		runsIn  RunsIn
		pid     int
		start   time.Time // what the process table says
		exit    string    // the exit file, if any
		pidWait time.Duration
		status  Status
		summary string
		code    *int
	}{
		{
			name: "an app run whose group is alive", runsIn: RunsInApp, pid: 501, start: startedLong(),
			status: StatusOrphaned, summary: orphanRunning,
		},
		{
			name: "an app run whose PID another process took", runsIn: RunsInApp, pid: 502,
			start: startedLong().Add(time.Hour), status: StatusInterrupted, summary: appClosed,
		},
		{
			name: "an app run whose group is gone", runsIn: RunsInApp, pid: 503, status: StatusInterrupted,
			summary: appClosed,
		},
		{
			name: "an app run that never recorded its process", runsIn: RunsInApp, status: StatusInterrupted,
			summary: appClosed,
		},
		{
			name: "a terminal run whose exit file is there", runsIn: RunsInTerminal, pid: 504, exit: "0\n",
			status: StatusSucceeded, summary: "Finished.", code: new(0),
		},
		{
			name: "a terminal run that failed meanwhile", runsIn: RunsInTerminal, pid: 505, exit: "2\n",
			status: StatusFailed, summary: "It exited with status 2.", code: new(2),
		},
		{
			name: "a terminal run whose exit file is garbled", runsIn: RunsInTerminal, pid: 506, exit: "x\n",
			status: StatusInterrupted, summary: exitUnreadable,
		},
		{
			name: "a terminal run whose shell still runs", runsIn: RunsInTerminal, pid: 507, start: startedLong(),
			status: StatusRunning,
		},
		{
			name: "a terminal run whose shell's PID another process took", runsIn: RunsInTerminal, pid: 508,
			start: startedLong().Add(-time.Hour), status: StatusInterrupted, summary: terminalClosed,
		},
		{
			name: "a terminal run whose shell is gone", runsIn: RunsInTerminal, pid: 509, status: StatusInterrupted,
			summary: terminalClosed,
		},
		{
			name: "a terminal run still young enough to record its shell", runsIn: RunsInTerminal,
			pidWait: time.Hour, status: StatusRunning,
		},
		{
			name: "a terminal run that never recorded its shell", runsIn: RunsInTerminal, status: StatusInterrupted,
			summary: terminalClosed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			h.e.pidWait = tt.pidWait

			procs := newFakeProcs()
			procs.install(h.e)

			if !tt.start.IsZero() {
				procs.run(tt.pid, tt.start)
			}

			dir := h.clone()
			id := h.insertRow(tt.runsIn, dir, tt.pid, tt.pid, startedLong())

			if tt.exit != "" {
				require.NoError(t, h.e.files.ensureDir())
				testutil.WriteFile(t, h.e.files.exit(id), tt.exit)
			}

			h.e.reconcile(t.Context())

			rec := h.record(id)
			assert.Equal(t, tt.status, rec.Status)
			assert.Equal(t, tt.code, rec.ExitCode)

			if tt.summary != "" {
				assert.Equal(t, tt.summary, rec.Summary)
			}

			_, held := h.locks.Holder(dir)
			assert.Equal(t, rec.Status.Live(), held, "a run followed again holds its clone; one settled doesn't")
			assert.Equal(t, rec.Status.Live(), h.e.liveRun(id) != nil)
		})
	}
}

func TestReconcileAdoptsAShellRecordedWhileTheAppWasAway(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	procs := newFakeProcs()
	procs.install(h.e)

	id := h.insertRow(RunsInTerminal, h.clone(), 0, 0, time.Time{})
	shellAt := h.record(id).StartedAt.Add(time.Second) // the terminal began it just after
	procs.run(777, shellAt)

	require.NoError(t, h.e.files.ensureDir())
	testutil.WriteFile(t, h.e.files.pid(id), "777\n")

	h.e.reconcile(t.Context())

	rec := h.record(id)
	assert.Equal(t, StatusRunning, rec.Status, "its shell still runs")
	assert.Equal(t, 777, rec.PID)
	assert.True(t, rec.ProcStartedAt.Equal(shellAt))

	testutil.WriteFile(t, h.e.files.exit(id), "0\n")

	assert.Equal(t, StatusSucceeded, h.ended(id).Status, "it is followed to its exit file")
}

func TestReconcileTellsAShellFromAStrangerWithItsPID(t *testing.T) {
	t.Parallel()

	for name, offset := range map[string]time.Duration{
		"started before the run": -time.Hour,
		"started long after":     time.Hour,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			procs := newFakeProcs()
			procs.install(h.e)

			id := h.insertRow(RunsInTerminal, h.clone(), 0, 0, time.Time{})
			procs.run(777, h.record(id).StartedAt.Add(offset))

			require.NoError(t, h.e.files.ensureDir())
			testutil.WriteFile(t, h.e.files.pid(id), "777\n")

			h.e.pidWait = 0 // too old to wait for its shell

			h.e.reconcile(t.Context())

			rec := h.record(id)
			assert.Equal(t, StatusInterrupted, rec.Status, "the shell that took the run ended unseen")
			assert.Equal(t, terminalClosed, rec.Summary)
			assert.Zero(t, rec.PID, "the stranger is never recorded as the run's shell")
		})
	}
}

func TestReconcileLeavesRunsItStartedItselfAlone(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	id := h.start(h.spec(testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute}))
	h.waitReady(id)

	procs := newFakeProcs() // nothing alive, as far as reconcile could tell
	procs.install(h.e)

	h.e.reconcile(t.Context())

	assert.Equal(t, StatusRunning, h.record(id).Status, "a run this engine follows isn't one left behind")
}

func TestStoppingAnOrphanedRunClimbsTheLadder(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	procs := newFakeProcs()
	procs.install(h.e)
	procs.run(4343, startedLong())

	id := h.insertRow(RunsInApp, h.clone(), 4343, 4343, startedLong())
	h.e.reconcile(t.Context())
	require.Equal(t, StatusOrphaned, h.record(id).Status)

	require.NoError(t, h.s.Stop(t.Context(), id))
	require.Eventually(t, func() bool { return len(procs.sent()) >= 2 }, wait, poll)
	assert.Equal(t, []syscall.Signal{syscall.SIGINT, syscall.SIGTERM}, procs.sent()[:2])

	procs.end(4343)

	rec := h.ended(id)
	assert.Equal(t, StatusStopped, rec.Status)
	assert.Equal(t, "Stopped.", rec.Summary)
}

func TestTheOrphansLadderInFakeTime(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		e := NewEngine(Config{})
		procs := newFakeProcs()
		procs.install(e)
		procs.run(99, startedLong())

		var at []time.Duration

		begun := time.Now()
		e.signal = func(_ int, sig syscall.Signal) error {
			at = append(at, time.Since(begun))

			return procs.send(99, sig)
		}

		l := &live{pid: 99, pgid: 99, procStart: startedLong(), done: make(chan struct{}), abandon: make(chan struct{})}

		e.climb(l, execpath.StopLadder())

		assert.Equal(t, []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGKILL}, procs.sent())
		assert.Equal(t, []time.Duration{0, 5 * time.Second, 15 * time.Second}, at)

		procs.end(99)

		e.climb(l, execpath.StopLadder())
		assert.Len(t, procs.sent(), 3, "a group that is gone gets nothing")
	})
}

func TestAShutdownStopsAnOrphanedRun(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	procs := newFakeProcs()
	procs.install(h.e)
	procs.run(4444, startedLong())

	id := h.insertRow(RunsInApp, h.clone(), 4444, 4444, startedLong())
	h.e.reconcile(t.Context())

	// The group ends at SIGKILL.
	h.e.signal = func(pgid int, sig syscall.Signal) error {
		if sig == syscall.SIGKILL {
			procs.end(pgid)
		}

		return procs.send(pgid, sig)
	}

	h.e.StopAll()

	rec := h.record(id)
	assert.Equal(t, StatusStopped, rec.Status)
	assert.Equal(t, []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL}, procs.sent())
}

func TestReconcileFollowsARealOrphan(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	// The run an earlier start of the app left: a program in its own group.
	path, env := testutil.FakeTool(t, "tool", testutil.FakeRun{Sleep: time.Minute})
	proc, err := execpath.Start(t.Context(), execpath.Cmd{Path: path, Env: append(os.Environ(), env)})
	require.NoError(t, err)

	t.Cleanup(func() {
		proc.Stop(fastLadder())
		_, _ = proc.Wait()
	})

	start := h.e.startedAt(proc.Pid())
	require.False(t, start.IsZero())

	stranger := h.insertRow(RunsInApp, h.clone(), proc.Pid(), proc.Pgid(), start.Add(-time.Hour))
	orphan := h.insertRow(RunsInApp, h.clone(), proc.Pid(), proc.Pgid(), start)

	h.e.reconcile(t.Context())

	assert.Equal(t, StatusInterrupted, h.record(stranger).Status, "the group is alive but led by another process")
	assert.Equal(t, StatusOrphaned, h.record(orphan).Status)

	require.NoError(t, h.s.Stop(t.Context(), orphan))

	rec := h.ended(orphan)
	assert.Equal(t, StatusStopped, rec.Status)

	_, err = proc.Wait()
	require.Error(t, err, "the stop ladder ended the program")
}

func TestReconcileRemovesTheFilesOfForgottenRuns(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	kept := h.ended(h.start(h.spec(testutil.FakeRun{Stdout: "kept\n"})))

	for _, name := range []string{"999.log", "999.pid", "999.exit", "999.exit.tmp", "1000.log.compact"} {
		testutil.WriteFile(t, h.e.files.dir+"/"+name, "x")
	}

	testutil.WriteFile(t, h.e.files.dir+"/notes.txt", "not a run's")

	h.e.reconcile(t.Context())

	entries, err := os.ReadDir(h.e.files.dir)
	require.NoError(t, err)

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	assert.ElementsMatch(t, []string{"1.log", "notes.txt"}, names)
	assert.Equal(t, "kept\n", h.logText(kept.ID))
}

func TestReconcileAfterTheEngineStoppedDoesNothing(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	procs := newFakeProcs()
	procs.install(h.e)
	procs.run(4545, startedLong())

	id := h.insertRow(RunsInApp, h.clone(), 4545, 4545, startedLong())

	h.e.StopAll()
	h.e.reconcile(context.WithoutCancel(t.Context()))

	assert.Equal(t, StatusRunning, h.record(id).Status, "the next start settles it")
}

// fakeVerifier verifies the runs from before a restart: it hands each a collector, and
// its Finish records how the run ended and says it verified it.
type fakeVerifier struct {
	output *collector

	mu    sync.Mutex
	asked []Record
	ended []Ending
	given []Outcome
}

func newFakeVerifier() *fakeVerifier {
	return &fakeVerifier{output: &collector{}}
}

func (f *fakeVerifier) Verify(run Record) Verification {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.asked = append(f.asked, run)

	return Verification{Callbacks: Callbacks{Output: f.output, Finish: func(_ context.Context, end Ending, out Outcome) Outcome {
		f.mu.Lock()
		defer f.mu.Unlock()

		f.ended, f.given = append(f.ended, end), append(f.given, out)

		return Outcome{Status: StatusSucceeded, Summary: "Verified.", Detail: map[string]string{"tag": "v1.4.3"}}
	}}}
}

// seen returns the runs it was asked about, and the endings and outcomes its Finish got.
func (f *fakeVerifier) seen() ([]Record, []Ending, []Outcome) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.asked), slices.Clone(f.ended), slices.Clone(f.given)
}

// writeLog writes a run's log, as the run did before the app restarted.
func (h *harness) writeLog(id int64, text string) {
	h.t.Helper()

	require.NoError(h.t, h.e.files.ensureDir())
	testutil.WriteFile(h.t, h.e.files.log(id), text)
}

func TestReconcileVerifiesAnInterruptedRun(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	procs := newFakeProcs()
	procs.install(h.e)

	v := newFakeVerifier()
	h.e.RegisterVerifier(KindScript, v)

	id := h.insertRow(RunsInApp, h.clone(), 4646, 4646, startedLong())
	h.writeLog(id, "Created tag v1.4.3\n")

	h.e.reconcile(t.Context())

	rec := h.ended(id)
	assert.Equal(t, StatusSucceeded, rec.Status, "its kind's verifier worked out how it ended")
	assert.Equal(t, "Verified.", rec.Summary)
	assert.Equal(t, map[string]any{"tag": "v1.4.3"}, rec.Result.Outcome)
	assert.Equal(t, map[string]any{"a": float64(1)}, rec.Result.Intent, "its intent is kept")

	asked, ended, given := v.seen()
	require.Len(t, asked, 1)
	assert.Equal(t, id, asked[0].ID)
	assert.Equal(t, map[string]any{"a": float64(1)}, asked[0].Result.Intent, "it verifies from the record's intent")
	assert.Equal(t, []Ending{{Interrupted: appClosed}}, ended)
	require.Len(t, given, 1)
	assert.Equal(t, StatusInterrupted, given[0].Status, "it is given the engine's own outcome")
	assert.Equal(t, "Created tag v1.4.3\n", v.output.String(), "its reader read the log from the start")
}

func TestReconcileVerifiesAnOrphanedRunWhenItEnds(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		procs := newFakeProcs()
		procs.install(h.e)
		procs.run(4747, startedLong())

		v := newFakeVerifier()
		h.e.RegisterVerifier(KindScript, v)

		id := h.insertRow(RunsInApp, h.clone(), 4747, 4747, startedLong())
		h.writeLog(id, "Pulling latest changes...\n")

		h.e.reconcile(t.Context())

		assert.Equal(t, StatusOrphaned, h.record(id).Status)

		asked, ended, _ := v.seen()
		assert.Len(t, asked, 1, "it is asked as the run is picked up")
		assert.Empty(t, ended, "its Finish waits for the run to end")

		time.Sleep(h.e.tailEvery)
		synctest.Wait()
		assert.Equal(t, "Pulling latest changes...\n", v.output.String(), "the log is read as the run goes on")

		procs.end(4747)
		time.Sleep(h.e.tailEvery)
		synctest.Wait()

		rec := h.record(id)
		assert.Equal(t, StatusSucceeded, rec.Status)
		assert.Nil(t, h.e.liveRun(id))

		_, ended, _ = v.seen()
		assert.Equal(t, []Ending{{Interrupted: orphanEnded}}, ended)
	})
}

// heldVerifier verifies runs from before a restart slowly: each one's Finish waits until
// let is closed, or its context ends, when it leaves the run unsettled.
type heldVerifier struct {
	let chan struct{}
}

func (v heldVerifier) Verify(Record) Verification {
	return Verification{Callbacks: Callbacks{Finish: func(ctx context.Context, _ Ending, _ Outcome) Outcome {
		select {
		case <-v.let:
			return Outcome{Status: StatusSucceeded, Summary: "Verified."}
		case <-ctx.Done():
			return Outcome{Unsettled: true}
		}
	}}}
}

func TestReconcileHoldsEveryRunBeforeItVerifiesAny(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	procs := newFakeProcs()
	procs.install(h.e)

	v := heldVerifier{let: make(chan struct{})}
	h.e.RegisterVerifier(KindScript, v)

	first, second := h.clone(), h.clone()
	ids := []int64{
		h.insertRow(RunsInApp, first, 5151, 5151, startedLong()),
		h.insertRow(RunsInTerminal, second, 5252, 0, startedLong()),
	}

	h.e.reconcile(t.Context()) // returns while both are still being verified

	for i, dir := range []string{first, second} {
		holder, held := h.locks.Holder(dir)
		require.True(t, held, "run %d's clone is held while it is verified", i)
		assert.Equal(t, clonelock.KindOrphan, holder.Kind)
		assert.Equal(t, ids[i], holder.RunID)
		assert.NotNil(t, h.e.liveRun(ids[i]), "run %d is registered", i)

		run, err := h.s.Run(t.Context(), ids[i])
		require.NoError(t, err)
		assert.False(t, run.CanStop, "nothing of it runs to stop")
	}

	assert.False(t, h.e.Active(), "verifying holds up no quit")
	assert.False(t, h.e.InUse("mage-x"), "nor an install")

	_, err := h.e.Start(t.Context(), Spec{
		Kind: KindScript, Dir: first, Argv: []string{"/usr/bin/true"}, Repo: testRepo(),
	})
	require.ErrorIs(t, err, clonelock.ErrBusy, "no run starts in a clone being verified")

	close(v.let)

	for _, id := range ids {
		assert.Equal(t, StatusSucceeded, h.ended(id).Status)
	}
}

func TestAnUnsettledEndIsLeftForTheNextStart(t *testing.T) {
	t.Parallel()

	var db *sql.DB

	h := newHarness(t, func(c *Config) { db = c.DB })
	procs := newFakeProcs()

	// A run in the app whose Finish can't learn what it needs before the app quits.
	spec := h.spec(testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute})
	spec.Finish = heldVerifier{let: make(chan struct{})}.Verify(Record{}).Finish
	id := h.start(spec)
	h.waitReady(id)

	h.e.StopAll()

	rec := h.record(id)
	assert.Equal(t, StatusRunning, rec.Status, "its record is left as going")
	assert.Nil(t, h.e.liveRun(id))

	_, held := h.locks.Holder(spec.Dir)
	assert.False(t, held, "its clone is let go")

	// The next start settles it through its kind's verifier.
	next := NewEngine(Config{DB: db, Locks: clonelock.New(), LogDir: h.e.files.dir, Logger: h.logs.Logger()})
	t.Cleanup(next.StopAll)
	procs.install(next)

	v := newFakeVerifier()
	next.RegisterVerifier(KindScript, v)
	next.reconcile(t.Context())

	require.Eventually(t, func() bool { return next.liveRun(id) == nil }, wait, poll)

	rec, err := next.store.get(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, StatusSucceeded, rec.Status)
	assert.Contains(t, v.output.String(), "ready", "its log is read again")

	_, ended, _ := v.seen()
	assert.Equal(t, []Ending{{Interrupted: appClosed}}, ended)
}

func TestAShutdownGivesUpOnVerifyingAtOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		h := newHarness(t)
		procs := newFakeProcs()
		procs.install(h.e)
		h.e.RegisterVerifier(KindScript, heldVerifier{let: make(chan struct{})})

		id := h.insertRow(RunsInApp, h.clone(), 5353, 5353, startedLong())
		h.e.reconcile(t.Context())

		begun := time.Now()

		h.e.StopAll()

		assert.Zero(t, time.Since(begun), "nothing runs, so no stop is waited for")
		assert.Equal(t, StatusRunning, h.record(id).Status, "the next start verifies it again")
		assert.Nil(t, h.e.liveRun(id))
	})
}

func TestReconcileAsksOnlyTheRunsKindsVerifier(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	procs := newFakeProcs()
	procs.install(h.e)

	v := newFakeVerifier()
	h.e.RegisterVerifier(KindRelease, v)

	id := h.insertRow(RunsInApp, h.clone(), 4848, 4848, startedLong())

	h.e.reconcile(t.Context())

	rec := h.record(id)
	assert.Equal(t, StatusInterrupted, rec.Status, "a script has no verifier: the engine's outcome stands")
	assert.Equal(t, appClosed, rec.Summary)

	asked, _, _ := v.seen()
	assert.Empty(t, asked)
}

func TestAQuitMidReconcileLeavesTheRestForTheNextStart(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.e.RegisterVerifier(KindScript, heldVerifier{let: make(chan struct{})})

	dir := h.clone()
	id := h.insertRow(RunsInApp, dir, 0, 0, startedLong())

	recs, err := h.e.leftBehind(t.Context())
	require.NoError(t, err)
	require.Len(t, recs, 1)

	ended := h.e.resumeAll(t.Context(), recs)
	require.Len(t, ended, 1, "registered, its end not yet worked out")

	h.e.StopAll() // the app quits before it is settled

	h.e.settle(t.Context(), ended[0])

	assert.Nil(t, h.e.liveRun(id))
	assert.Equal(t, StatusRunning, h.record(id).Status, "the next start settles it")

	_, held := h.locks.Holder(dir)
	assert.False(t, held, "its clone is let go")

	l := h.e.liveOf(recs[0])
	assert.Nil(t, h.e.endedRun(l, Ending{Interrupted: appClosed}), "nothing is registered once the engine stopped")
}
