package scriptrun

import (
	"context"
	"os"
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
