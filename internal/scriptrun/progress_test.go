//go:build unix

package scriptrun

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// rebaseRisk is what a kind says while stopping it could leave a rebase in progress.
const rebaseRisk = "Stopping now could leave a rebase in progress."

// fakeKind is a kind's output reader and progress: it reads "step <name>", "working
// <name>", "refuse <why>", "confirm <question>", and "allow" lines, and a test can set its
// progress outright.
type fakeKind struct {
	mu      sync.Mutex
	pending []byte
	now     Progress
}

// Write reads p's whole lines.
func (k *fakeKind) Write(p []byte) (int, error) {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.pending = append(k.pending, p...)

	for {
		i := bytes.IndexByte(k.pending, '\n')
		if i < 0 {
			return len(p), nil
		}

		k.read(string(k.pending[:i]))
		k.pending = k.pending[i+1:]
	}
}

// Progress is the kind's Spec.Progress.
func (k *fakeKind) Progress() Progress {
	k.mu.Lock()
	defer k.mu.Unlock()

	return k.now
}

// set sets the progress outright.
func (k *fakeKind) set(p Progress) {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.now = p
}

// read reads one line.
func (k *fakeKind) read(line string) {
	word, rest, _ := strings.Cut(line, " ")

	switch word {
	case "step":
		k.now.Step, k.now.Working = rest, ""
	case "working":
		k.now.Working = rest
	case "refuse":
		k.now.StopRefused, k.now.StopConfirm = rest, ""
	case "confirm":
		k.now.StopRefused, k.now.StopConfirm = "", rest
	case "allow":
		k.now.StopRefused, k.now.StopConfirm = "", ""
	}
}

// withKind returns spec read by a fresh fake kind.
func withKind(spec Spec) (Spec, *fakeKind) {
	k := &fakeKind{}
	spec.Output, spec.Progress = k, k.Progress

	return spec, k
}

// run returns the run as the window reads it.
func (h *harness) run(id int64) Run {
	h.t.Helper()

	got, err := h.s.Run(h.t.Context(), id)
	require.NoError(h.t, err)

	return got
}

// waitProgress waits for the window to read the run's progress as want.
func (h *harness) waitProgress(id int64, want Progress) {
	h.t.Helper()

	require.EventuallyWithT(h.t, func(c *assert.CollectT) {
		got, err := h.s.Run(h.t.Context(), id)
		require.NoError(c, err)
		assert.Equal(c, want, got.Progress)
	}, wait, poll, "run %d reads as %+v", id, want)
}

func TestStopIsRefusedWhileTheKindSaysSo(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	spec, kind := withKind(h.spec(testutil.FakeRun{
		Stdout: "step onBranch\nworking updated\nrefuse " + rebaseRisk + "\n", Sleep: time.Minute,
	}))
	id := h.start(spec)

	refused := Progress{Step: "onBranch", Working: "updated", StopRefused: rebaseRisk}
	h.waitProgress(id, refused)
	assert.False(t, h.run(id).CanStop, "Stop is off while the kind refuses it")

	err := h.s.Stop(t.Context(), id)
	require.ErrorIs(t, err, ErrStopRefused)
	assert.Equal(t, rebaseRisk, uierr.Message(err), "the kind's own words")

	assert.Equal(t, StatusRunning, h.record(id).Status, "a refused Stop leaves the run going")
	assert.False(t, h.e.liveRun(id).stopped.Load(), "and doesn't mark it stopped")

	kind.set(Progress{Step: "updated", StopConfirm: "Stopping now can leave v1.4.3 on this Mac."})

	run := h.run(id)
	assert.True(t, run.CanStop, "the kind lets Stop through again")
	assert.Equal(t, "Stopping now can leave v1.4.3 on this Mac.", run.Progress.StopConfirm,
		"and says what the window asks first")

	require.NoError(t, h.s.Stop(t.Context(), id))

	rec := h.ended(id)
	assert.Equal(t, StatusStopped, rec.Status)
	require.NotNil(t, rec.ExitCode)
	assert.Equal(t, 130, *rec.ExitCode, "SIGINT ended it")
	assert.Equal(t, Progress{}, h.run(id).Progress, "a run that ended has no progress")
}

func TestARunReadAsItsEndIsRecordedHasNoProgress(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	spec, _ := withKind(h.spec(testutil.FakeRun{Stdout: "step updated\nconfirm Stopping now can leave v1.4.3 on this Mac.\n"}))

	type seen struct {
		run  Run
		err  error
		held bool
	}

	got := make(chan seen, 1)

	// Recorded is told once the end is recorded, before the engine lets go of the run: a
	// window reading the run then reads an ended record of a run still registered.
	spec.Recorded = func(ctx context.Context, id int64, _ Outcome) {
		run, err := h.s.Run(ctx, id)
		got <- seen{run: run, err: err, held: h.e.liveRun(id) != nil}
	}

	h.ended(h.start(spec))

	read := received(t, got, "Recorded is told")
	require.NoError(t, read.err)
	require.True(t, read.held, "the engine still held the run")
	assert.Equal(t, StatusSucceeded, read.run.Status)
	assert.Equal(t, Progress{}, read.run.Progress, "a run that ended has no progress")
	assert.False(t, read.run.CanStop, "nor a Stop")
}

func TestARunWhoseEndIsRecordedNoLongerGoes(t *testing.T) {
	t.Parallel()

	for _, kind := range []Kind{KindScript, KindUpdate} {
		t.Run(string(kind), func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			proceed := filepath.Join(t.TempDir(), "proceed")
			spec, _ := withKind(h.spec(testutil.FakeRun{Stdout: "step updated\n", WaitFor: proceed}))
			spec.Kind, spec.Tool = kind, "mage-x"

			type gap struct {
				held     bool
				stop     error
				stopped  bool
				active   bool
				inUse    bool
				updating int64
				stale    Run
				listed   []Run
				listErr  error
			}

			got := make(chan gap, 1)

			var going atomic.Pointer[Record]

			// Recorded is told in the gap: the run's end is recorded, and the engine still
			// holds it and its clone.
			spec.Recorded = func(ctx context.Context, id int64, _ Outcome) {
				var g gap

				l := h.e.liveRun(id)
				g.held = l != nil
				g.stop = h.s.Stop(ctx, id)
				g.stopped = l != nil && l.stopped.Load()
				g.active, g.inUse, g.updating = h.e.Active(), h.e.InUse("mage-x"), h.e.Updating("mage-x")
				g.stale = h.s.view(*going.Load())
				g.listed, g.listErr = h.s.Runs(ctx, Filter{})

				got <- g
			}

			id := h.start(spec)
			h.waitProgress(id, Progress{Step: "updated"})

			rec := h.record(id)
			require.Equal(t, StatusRunning, rec.Status)
			going.Store(&rec)

			var updating int64
			if kind == KindUpdate {
				updating = id
			}

			assert.True(t, h.e.Active(), "while it goes, it keeps the app from quitting")
			assert.Equal(t, kind != KindUpdate, h.e.InUse("mage-x"), "uses its tool, unless it updates it")
			assert.Equal(t, updating, h.e.Updating("mage-x"))
			assert.True(t, h.s.view(rec).CanStop)

			testutil.WriteFile(t, proceed, "")
			h.ended(id)

			read := received(t, got, "Recorded is told")
			require.True(t, read.held, "the engine still held the run")
			require.ErrorIs(t, read.stop, ErrNotRunning, "a Stop finds it ended")
			assert.Equal(t, "It has already ended.", uierr.Message(read.stop))
			assert.False(t, read.stopped, "and never marks it stopped")
			assert.False(t, read.active, "it keeps nothing from quitting")
			assert.False(t, read.inUse, "uses no tool")
			assert.Zero(t, read.updating, "and updates none")
			assert.Equal(t, Progress{}, read.stale.Progress, "a record read while it went shows no progress now")
			assert.False(t, read.stale.CanStop, "nor a Stop")

			require.NoError(t, read.listErr)
			require.Len(t, read.listed, 1)
			assert.Equal(t, Progress{}, read.listed[0].Progress, "nor does the list")
			assert.False(t, read.listed[0].CanStop)
		})
	}
}

// received returns what ch gives, failing the test when it gives nothing within wait.
func received[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()

	select {
	case v := <-ch:
		return v
	case <-time.After(wait):
		require.FailNow(t, what+": nothing came")

		var none T

		return none
	}
}

func TestProgressIsToldWhenItChanges(t *testing.T) {
	t.Parallel()

	counts := struct {
		mu                  sync.Mutex
		progressed, changed int
		ids                 []int64
	}{}

	h := newHarness(t, func(cfg *Config) {
		cfg.Changed = func() { counts.mu.Lock(); counts.changed++; counts.mu.Unlock() }
		cfg.Progressed = func(id int64) {
			counts.mu.Lock()
			counts.progressed++
			counts.ids = append(counts.ids, id)
			counts.mu.Unlock()
		}
	})

	spec, _ := withKind(h.spec(testutil.FakeRun{Stdout: "noise\nstep one\nworking two\n", Sleep: time.Minute}))
	id := h.start(spec)

	h.waitProgress(id, Progress{Step: "one", Working: "two"})

	counts.mu.Lock()
	told, starts, ids := counts.progressed, counts.changed, slices.Clone(counts.ids)
	counts.mu.Unlock()

	assert.GreaterOrEqual(t, told, 1, "Progressed hears the new step")
	assert.Equal(t, slices.Repeat([]int64{id}, told), ids, "with the run's ID")
	assert.LessOrEqual(t, told, 2, "once per change it read, at most")
	assert.Equal(t, 1, starts, "Changed hears only that the run started")

	require.NoError(t, h.s.Stop(t.Context(), id))
	h.ended(id)
}

func TestNoteProgressTellsOnlyAChange(t *testing.T) {
	t.Parallel()

	told := 0
	e := NewEngine(Config{Progressed: func(id int64) {
		assert.Equal(t, int64(7), id)

		told++
	}})
	kind := &fakeKind{}
	l := &live{id: 7, progress: kind.Progress}

	e.noteProgress(l)
	assert.Zero(t, told, "no progress yet is no change")

	kind.set(Progress{Step: "onBranch"})
	e.noteProgress(l)
	e.noteProgress(l)
	assert.Equal(t, 1, told, "a change is told once")

	kind.set(Progress{Step: "onBranch", StopRefused: rebaseRisk})
	e.noteProgress(l)
	assert.Equal(t, 2, told, "Stop turning off is a change")

	e.noteProgress(&live{})
	assert.Equal(t, 2, told, "a kind without progress tells nothing")

	quiet := NewEngine(Config{})
	quiet.noteProgress(l)
	kind.set(Progress{})
	quiet.noteProgress(l)
	assert.Equal(t, Progress{}, l.shown, "without Progressed, the change is still noted")
}

func TestAKindWithoutProgressStopsAsBefore(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	spec := h.spec(testutil.FakeRun{Stdout: readyLine + "step one\n", Sleep: time.Minute})
	spec.Output = &collector{}
	id := h.start(spec)
	h.waitReady(id)

	run := h.run(id)
	assert.Equal(t, Progress{}, run.Progress, "no progress without a kind that reads one")
	assert.True(t, run.CanStop)

	require.NoError(t, h.s.Stop(t.Context(), id))
	assert.Equal(t, StatusStopped, h.ended(id).Status)
}

func TestARunInTheTerminalShowsItsStepsButNotStop(t *testing.T) {
	t.Parallel()

	h := terminalHarness(t, newFakeTerminal(t))
	spec, _ := withKind(h.terminalSpec(testutil.FakeRun{
		Stdout: "step onBranch\nrefuse " + rebaseRisk + "\n", Sleep: time.Minute,
	}))
	id := h.start(spec)

	h.waitProgress(id, Progress{Step: "onBranch"})

	run := h.run(id)
	assert.False(t, run.CanStop, "a run in the terminal stops there")

	err := h.s.Stop(t.Context(), id)
	require.ErrorIs(t, err, ErrNoStop, "the terminal's own answer comes first")
}

func TestStopIsRefusedForAnOrphanedRunWhileItsKindSaysSo(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	procs := newFakeProcs()
	procs.install(h.e)
	procs.run(4848, startedLong())

	kind := &fakeKind{}
	h.e.RegisterVerifier(KindScript, progressVerifier{kind: kind})

	id := h.insertRow(RunsInApp, h.clone(), 4848, 4848, startedLong())
	h.writeLog(id, "working updated\nrefuse "+rebaseRisk+"\n")

	h.e.reconcile(t.Context())
	require.Equal(t, StatusOrphaned, h.record(id).Status)

	h.waitProgress(id, Progress{Working: "updated", StopRefused: rebaseRisk})

	err := h.s.Stop(t.Context(), id)
	require.ErrorIs(t, err, ErrStopRefused)
	assert.Equal(t, rebaseRisk, uierr.Message(err))
	assert.Empty(t, procs.sent(), "no signal went to its group")

	kind.set(Progress{})
	require.NoError(t, h.s.Stop(t.Context(), id))
	require.Eventually(t, func() bool { return len(procs.sent()) > 0 }, wait, poll)
	assert.Equal(t, syscall.SIGINT, procs.sent()[0])

	procs.end(4848)
	assert.Equal(t, StatusStopped, h.ended(id).Status)
}

// progressVerifier gives a run from before a restart the fake kind as its reader and
// progress.
type progressVerifier struct {
	kind *fakeKind
}

func (v progressVerifier) Verify(Record) Verification {
	return Verification{Output: v.kind, Progress: v.kind.Progress}
}

func TestQuitWaitsForAKindToLetItStop(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	procs := newFakeProcs()
	procs.install(h.e)
	procs.run(5050, startedLong())

	kind := &fakeKind{}
	kind.set(Progress{Working: "updated", StopRefused: rebaseRisk})
	h.e.RegisterVerifier(KindScript, progressVerifier{kind: kind})

	id := h.insertRow(RunsInApp, h.clone(), 5050, 5050, startedLong())
	h.e.reconcile(t.Context())
	require.Equal(t, StatusOrphaned, h.record(id).Status)

	var signaledWhileRefused atomic.Bool

	h.e.signal = func(pgid int, sig syscall.Signal) error {
		if kind.Progress().StopRefused != "" {
			signaledWhileRefused.Store(true)
		}

		if sig == syscall.SIGTERM {
			procs.end(pgid)
		}

		return procs.send(pgid, sig)
	}

	allowed := make(chan struct{})

	go func() {
		defer close(allowed)

		// The pull ends once the quit has begun.
		assert.Eventually(t, h.e.isClosed, wait, poll)
		kind.set(Progress{Step: "updated"})
	}()

	h.e.StopAll()
	<-allowed

	assert.False(t, signaledWhileRefused.Load(), "no signal while its kind refused")
	assert.Equal(t, []syscall.Signal{syscall.SIGTERM}, procs.sent(), "SIGTERM once the pull ended, and it was enough")

	rec := h.record(id)
	assert.Equal(t, StatusStopped, rec.Status)
	assert.Equal(t, "Stopped as "+brand.Name+" quit.", rec.Summary)
}

func TestQuitLeavesARunItsKindHoldsBack(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	spec, _ := withKind(h.spec(testutil.FakeRun{
		Stdout: "working updated\nrefuse " + rebaseRisk + "\n", Sleep: time.Minute,
	}))
	id := h.start(spec)
	h.waitProgress(id, Progress{Working: "updated", StopRefused: rebaseRisk})

	proc := h.e.liveRun(id).proc

	t.Cleanup(func() {
		proc.Stop(fastLadder())
		_, _ = proc.Wait()
	})

	h.e.StopAll()

	select {
	case <-proc.Done():
		assert.Fail(t, "the run was stopped mid-pull")
	default:
	}

	assert.Nil(t, h.e.liveRun(id), "the app let it go")
	assert.Equal(t, StatusRunning, h.record(id).Status, "still recorded as going, for the next start to follow")
	assert.Contains(t, h.logs.String(), "left a run going as the app quit")
	assert.Contains(t, h.logs.String(), rebaseRisk)
}

func TestQuitStopsARunThatEndsWhileHeldBackAsUsual(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.e.quitKill = wait // time enough for it to end on its own while held back

	// The tool runs until the quit has begun, so it is seen held back however long it takes
	// to start, and it ends while the quit still holds it back.
	proceed := filepath.Join(t.TempDir(), "proceed")
	spec, _ := withKind(h.spec(testutil.FakeRun{
		Stdout: "working updated\nrefuse " + rebaseRisk + "\n", WaitFor: proceed, Exit: 1,
	}))
	id := h.start(spec)
	h.waitProgress(id, Progress{Working: "updated", StopRefused: rebaseRisk})

	let := make(chan struct{})

	go func() {
		defer close(let)

		if assert.Eventually(t, h.e.isClosed, wait, poll, "the quit begins") {
			assert.NoError(t, os.WriteFile(proceed, nil, 0o600), "the tool ends")
		}
	}()

	h.e.StopAll()
	<-let

	rec := h.record(id)
	assert.Equal(t, StatusFailed, rec.Status, "it ended on its own, unsignaled, and its end is recorded")
	assert.NotContains(t, h.logs.String(), "left a run going")
}
