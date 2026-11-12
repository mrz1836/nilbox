package scriptrun

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestStartRecordsARunThatSucceeds(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	spec := h.spec(testutil.FakeRun{Stdout: "hello\n"})
	spec.Intent = map[string]any{"target": "lint"}
	spec.Tool = "mage-x"

	id := h.start(spec)
	rec := h.ended(id)

	assert.Equal(t, StatusSucceeded, rec.Status)
	require.NotNil(t, rec.ExitCode)
	assert.Zero(t, *rec.ExitCode)
	assert.Equal(t, "Finished.", rec.Summary)
	assert.Equal(t, KindScript, rec.Kind)
	assert.Equal(t, RunsInApp, rec.RunsIn)
	assert.Equal(t, testRepo(), rec.Repo)
	assert.Equal(t, spec.Dir, rec.Dir)
	assert.Equal(t, spec.Argv, rec.Argv)
	assert.Equal(t, textutil.ShellWord(spec.Argv[0]), rec.Command)
	assert.Positive(t, rec.PID)
	assert.Equal(t, rec.PID, rec.PGID, "the program leads its own group")
	assert.False(t, rec.ProcStartedAt.IsZero(), "its start time tells it from a reused PID")
	assert.Equal(t, "mage-x", rec.Result.Tool)
	assert.Equal(t, map[string]any{"target": "lint"}, rec.Result.Intent, "the intent written at the start is kept")
	assert.Equal(t, int64(len("hello\n")), rec.LogBytes)
	assert.False(t, rec.FinishedAt.IsZero())
	assert.Equal(t, "hello\n", h.logText(id))
	assert.Positive(t, h.changes.Load(), "the window hears that the runs changed")

	_, held := h.locks.Holder(spec.Dir)
	assert.False(t, held, "the clone is free once the run ends")
	assert.Empty(t, h.e.slots, "its slot is free")
}

func TestStartRecordsAFailingExitStatus(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	rec := h.ended(h.start(h.spec(testutil.FakeRun{Stderr: "boom\n", Exit: 3})))

	assert.Equal(t, StatusFailed, rec.Status)
	require.NotNil(t, rec.ExitCode)
	assert.Equal(t, 3, *rec.ExitCode)
	assert.Equal(t, "It exited with status 3.", rec.Summary)
}

func TestTheLogIsPrivate(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	id := h.start(h.spec(testutil.FakeRun{Stdout: "x"}))
	h.ended(id)

	dir, err := filepath.Abs(h.e.files.dir)
	require.NoError(t, err)

	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.Equal(t, "drwx------", info.Mode().String(), "only the user reads the log folder")

	info, err = os.Stat(h.e.files.log(id))
	require.NoError(t, err)
	assert.Equal(t, "-rw-------", info.Mode().String(), "only the user reads a log")
}

func TestTheReaderGetsTheWholeLogAndFinishDecides(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	out := &collector{}
	lines := strings.Repeat("a line of output\n", 5000)

	var gotEnd Ending

	spec := h.spec(testutil.FakeRun{Stdout: lines, Exit: 0})
	spec.Output = out
	spec.Finish = func(_ context.Context, end Ending, def Outcome) Outcome {
		gotEnd = end

		assert.Equal(t, StatusSucceeded, def.Status, "Finish is given the engine's outcome")
		assert.Equal(t, lines, out.String(), "the reader has read everything by the time Finish runs")

		return Outcome{
			Status: StatusFailed, Warnings: []string{"WARN one"}, Unrecognized: []string{"odd"}, UnrecognizedCount: 7,
			Detail: map[string]any{"tag": "v1.2.3"},
		}
	}

	rec := h.ended(h.start(spec))

	assert.Equal(t, lines, out.String())
	require.NotNil(t, gotEnd.ExitCode)
	assert.Zero(t, *gotEnd.ExitCode)
	assert.Equal(t, StatusFailed, rec.Status, "the kind's verification decides, not the exit code alone")
	assert.Equal(t, "Finished.", rec.Summary, "an outcome without a summary keeps the engine's")
	assert.Equal(t, []string{"WARN one"}, rec.Result.Warnings)
	assert.Equal(t, []string{"odd"}, rec.Result.Unrecognized)
	assert.Equal(t, 7, rec.Result.UnrecognizedCount)
	assert.Equal(t, map[string]any{"tag": "v1.2.3"}, rec.Result.Outcome)
}

func TestFinishCannotLeaveARunGoing(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	spec := h.spec(testutil.FakeRun{Exit: 2})
	spec.Finish = func(context.Context, Ending, Outcome) Outcome {
		return Outcome{Status: StatusRunning, Unrecognized: make([]string, MaxUnrecognized+5)}
	}

	rec := h.ended(h.start(spec))

	assert.Equal(t, StatusFailed, rec.Status, "an outcome can't say the run still goes")
	assert.Len(t, rec.Result.Unrecognized, MaxUnrecognized, "the result keeps at most 200 unrecognized lines")
	assert.Equal(t, MaxUnrecognized+5, rec.Result.UnrecognizedCount, "and counts them all")
}

func TestTheAppLogGetsOneLinePerRunAndNoOutput(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	spec := h.spec(testutil.FakeRun{Stdout: "secret-output\n", Exit: 4})
	spec.Env = append(spec.Env, "API_TOKEN=secret-env-value")
	spec.Tool = "mage-x"

	id := h.start(spec)
	h.ended(id)

	require.Eventually(t, func() bool { return strings.Contains(h.logs.String(), "a run ended") }, wait, poll)

	logged := h.logs.String()
	assert.Equal(t, 1, strings.Count(logged, "a run ended"))
	assert.Contains(t, logged, "level=WARN", "a non-zero exit is a warning")
	assert.Contains(t, logged, "kind=script")
	assert.Contains(t, logged, "repo=acme-org/sdk")
	assert.Contains(t, logged, "status=failed")
	assert.Contains(t, logged, "exit=4")
	assert.Contains(t, logged, "warnings=0")
	assert.Contains(t, logged, "unrecognized=0")
	assert.NotContains(t, logged, "secret-output", "the output never reaches the app log")
	assert.NotContains(t, logged, "secret-env-value", "nor does the environment")
}

func TestStopEndsARunWithTheLadder(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	spec := h.spec(testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute})
	id := h.start(spec)
	h.waitReady(id)

	run, err := h.s.Run(t.Context(), id)
	require.NoError(t, err)
	assert.True(t, run.CanStop)

	require.NoError(t, h.s.Stop(t.Context(), id))

	rec := h.ended(id)
	assert.Equal(t, StatusStopped, rec.Status)
	assert.Equal(t, "Stopped.", rec.Summary)
	require.NotNil(t, rec.ExitCode)
	assert.Equal(t, 130, *rec.ExitCode, "SIGINT ended it")

	err = h.s.Stop(t.Context(), id)
	require.ErrorIs(t, err, ErrNotRunning)
	assert.Equal(t, "It has already ended.", uierr.Message(err))

	require.ErrorIs(t, h.s.Stop(t.Context(), id+100), ErrNotFound)
}

func TestARunPastItsLimitIsStopped(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	spec := h.spec(testutil.FakeRun{Sleep: time.Minute})
	spec.Timeout = 300 * time.Millisecond

	rec := h.ended(h.start(spec))

	assert.Equal(t, StatusFailed, rec.Status)
	assert.Equal(t, "It ran past its limit of 300ms, so it was stopped.", rec.Summary)
}

func TestEachKindHasItsLimit(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 5*time.Minute, KindRelease.Timeout())
	assert.Equal(t, time.Minute, KindHooks.Timeout())
	assert.Equal(t, 30*time.Minute, KindChecks.Timeout())
	assert.Equal(t, 10*time.Minute, KindUpdate.Timeout())
	assert.Equal(t, 30*time.Minute, KindPull.Timeout())
	assert.Zero(t, KindScript.Timeout(), "a script has Stop instead")
	assert.Zero(t, Kind("other").Timeout())
	assert.True(t, KindUpdate.Valid())
	assert.True(t, KindPull.Valid())
	assert.Equal(t, clonelock.KindUpdate, KindUpdate.lockKind())
	assert.Equal(t, clonelock.KindPull, KindPull.lockKind())
	assert.Equal(t, clonelock.KindScript, Kind("other").lockKind())

	e := NewEngine(Config{})
	assert.Equal(t, 5*time.Minute, e.newLive(Spec{Kind: KindRelease, RunsIn: RunsInApp}, holds{}).timeout)
	assert.Zero(t, e.newLive(Spec{Kind: KindRelease, RunsIn: RunsInTerminal}, holds{}).timeout, "a terminal has none")
	assert.Zero(t, e.newLive(Spec{Kind: KindChecks, RunsIn: RunsInApp, Timeout: -1}, holds{}).timeout)
	assert.Equal(t, time.Second, e.newLive(Spec{Kind: KindHooks, RunsIn: RunsInApp, Timeout: time.Second}, holds{}).timeout)
	assert.Equal(t, "5 minutes", durationText(5*time.Minute))
	assert.Equal(t, "1 minute", durationText(time.Minute))
}

func TestABusyCloneNamesItsHolderAndNeverQueues(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	spec := h.spec(testutil.FakeRun{})

	release, _, ok := h.locks.TryLock(spec.Dir, clonelock.Holder{Kind: clonelock.KindRelease, Repo: "acme-org/sdk"})
	require.True(t, ok)

	defer release()

	before := false
	spec.Before = func(context.Context) ([]string, error) {
		before = true

		return nil, nil
	}

	begun := time.Now()
	_, err := h.e.Start(t.Context(), spec)

	var busy *clonelock.BusyError
	require.ErrorAs(t, err, &busy)
	require.ErrorIs(t, err, clonelock.ErrBusy)
	assert.Equal(t, clonelock.KindRelease, busy.Holder.Kind)
	assert.Equal(t, "A release is using acme-org/sdk right now.", uierr.Message(err))
	assert.Less(t, time.Since(begun), time.Second, "it fails at once, never waiting for the clone")
	assert.False(t, before, "nothing ran")
	assert.Zero(t, h.count(), "nothing was recorded")
	assert.Empty(t, h.e.slots, "no slot was taken")
}

func TestASecondRunOnACloneFailsWhileTheFirstGoes(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	first := h.spec(testutil.FakeRun{Sleep: time.Minute})
	first.Title = "Checks · all files"
	first.Kind = KindChecks
	id := h.start(first)

	holder, held := h.locks.Holder(first.Dir)
	require.True(t, held)
	assert.Equal(t, clonelock.KindChecks, holder.Kind)
	assert.Equal(t, "Checks · all files", holder.Title)
	assert.Equal(t, "acme-org/sdk", holder.Repo)

	second := h.spec(testutil.FakeRun{})
	second.Dir = first.Dir

	_, err := h.e.Start(t.Context(), second)
	require.ErrorIs(t, err, clonelock.ErrBusy)
	assert.Equal(t, "A checks run is using acme-org/sdk right now.", uierr.Message(err))

	require.NoError(t, h.s.Stop(t.Context(), id))
	h.ended(id)

	again := h.ended(h.start(second))
	assert.Equal(t, StatusSucceeded, again.Status, "the clone is free once the first run ends")
}

func TestBeforeRunsUnderTheLockAndCanEndTheStart(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	spec := h.spec(testutil.FakeRun{})

	spec.Before = func(context.Context) ([]string, error) {
		holder, held := h.locks.Holder(spec.Dir)
		assert.True(t, held, "Before runs under the clone's lock")
		assert.Equal(t, clonelock.KindScript, holder.Kind)
		assert.Len(t, h.e.slots, 1, "and holds a slot")

		return nil, errFacts
	}

	_, err := h.e.Start(t.Context(), spec)
	require.ErrorIs(t, err, errFacts)
	assert.Zero(t, h.count(), "nothing was recorded")

	_, held := h.locks.Holder(spec.Dir)
	assert.False(t, held)
	assert.Empty(t, h.e.slots)
}

func TestBeforeCanWorkTheCommandOut(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	spec := h.spec(testutil.FakeRun{Stdout: readyLine})
	argv := spec.Argv
	spec.Argv = nil

	spec.Before = func(context.Context) ([]string, error) { return argv, nil }

	rec := h.ended(h.start(spec))
	assert.Equal(t, StatusSucceeded, rec.Status)
	assert.Equal(t, argv, rec.Argv, "the command Before worked out is the one run and recorded")

	spec.Before = func(context.Context) ([]string, error) { return []string{"tool"}, nil }

	_, err := h.e.Start(t.Context(), spec)
	require.ErrorIs(t, err, ErrInvalidSpec, "a command Before gives must start with an absolute path")

	_, held := h.locks.Holder(spec.Dir)
	assert.False(t, held)
	assert.Empty(t, h.e.slots)

	spec.Before = nil

	_, err = h.e.Start(t.Context(), spec)
	require.ErrorIs(t, err, ErrInvalidSpec, "without Before, the command is needed up front")
}

func TestBeforeEnvReplacesTheEnvironmentUnderTheLock(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	spec := h.spec(testutil.FakeRun{})
	worked := []string{"PATH=/usr/bin", "GIT_TERMINAL_PROMPT=0"}

	spec.Before = func(context.Context) ([]string, error) { return nil, nil }
	spec.BeforeEnv = func() []string {
		_, held := h.locks.Holder(spec.Dir)
		assert.True(t, held, "it is asked under the clone's lock")

		return worked
	}

	held, err := h.e.hold(t.Context(), &spec)
	require.NoError(t, err)
	held.free()
	assert.Equal(t, worked, spec.Env, "the environment Before's work gave is the one the run gets")

	spec.Env = nil
	spec.Before = func(context.Context) ([]string, error) { return nil, errFacts }

	_, err = h.e.hold(t.Context(), &spec)
	require.ErrorIs(t, err, errFacts)
	assert.Nil(t, spec.Env, "a Before that fails gives no environment")
}

func TestTheSlotLimitHoldsAndAReleaseNeverWaits(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	ids := make([]int64, 0, Slots)

	for range Slots {
		spec := h.spec(testutil.FakeRun{Sleep: time.Minute})
		ids = append(ids, h.start(spec))
	}

	release := h.spec(testutil.FakeRun{})
	release.Kind = KindRelease

	begun := time.Now()
	_, err := h.e.Start(t.Context(), release)
	require.ErrorIs(t, err, ErrNoSlot)
	assert.Less(t, time.Since(begun), time.Second, "a release never waits for a slot")
	assert.Equal(t, "4 runs are going already, and a release never waits for one to end. Try again when one ends.",
		uierr.Message(err))

	_, held := h.locks.Holder(release.Dir)
	assert.False(t, held, "the release let go of its clone")

	short, cancel := context.WithTimeout(t.Context(), 300*time.Millisecond)
	defer cancel()

	_, err = h.e.Start(short, h.spec(testutil.FakeRun{}))
	require.ErrorIs(t, err, context.DeadlineExceeded, "another run waits for a slot until its context ends")
	assert.Equal(t, Slots, h.count(), "only the runs with a slot were recorded")

	waiting := h.spec(testutil.FakeRun{Stdout: "got a slot\n"})
	started := make(chan int64, 1)

	var wg sync.WaitGroup

	wg.Go(func() {
		id, startErr := h.e.Start(t.Context(), waiting)
		assert.NoError(t, startErr)

		started <- id
	})

	select {
	case <-started:
		require.FailNow(t, "a fifth run started while four went")
	case <-time.After(300 * time.Millisecond):
	}

	_, held = h.locks.Holder(waiting.Dir)
	assert.False(t, held, "a run waiting for a slot doesn't hold its clone")

	require.NoError(t, h.s.Stop(t.Context(), ids[0]))

	id := <-started

	wg.Wait()

	assert.Equal(t, StatusSucceeded, h.ended(id).Status, "the waiting run took the slot a run freed")
}

func TestTakeSlotWaitsInFakeTime(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		e := NewEngine(Config{})

		frees := make([]func(), 0, Slots)

		for range Slots {
			free, err := e.takeSlot(t.Context(), KindScript)
			require.NoError(t, err)

			frees = append(frees, free)
		}

		_, err := e.takeSlot(t.Context(), KindRelease)
		require.ErrorIs(t, err, ErrNoSlot)

		ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
		defer cancel()

		begun := time.Now()
		_, err = e.takeSlot(ctx, KindChecks)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, time.Hour, time.Since(begun), "it waited until its context ended")

		go func() {
			time.Sleep(time.Minute)
			frees[0]()
			frees[0]() // a second release frees nothing more
		}()

		begun = time.Now()
		free, err := e.takeSlot(t.Context(), KindHooks)
		require.NoError(t, err)
		assert.Equal(t, time.Minute, time.Since(begun), "it took the slot as soon as one was freed")

		synctest.Wait()
		assert.Len(t, e.slots, Slots)

		free()

		for _, f := range frees[1:] {
			f()
		}

		assert.Empty(t, e.slots)

		for range Slots {
			_, err = e.takeSlot(t.Context(), KindScript)
			require.NoError(t, err)
		}

		go e.StopAll()

		_, err = e.takeSlot(t.Context(), KindScript)
		require.ErrorIs(t, err, ErrClosed, "a run waiting for a slot gives up as the app quits")
	})
}

func TestInUseAndActive(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	assert.False(t, h.e.Active())
	assert.False(t, h.e.InUse("mage-x"))

	spec := h.spec(testutil.FakeRun{Sleep: time.Minute})
	spec.Tool = "mage-x"
	id := h.start(spec)

	assert.True(t, h.e.Active(), "a run goes in the app")
	assert.True(t, h.e.InUse("mage-x"))
	assert.False(t, h.e.InUse("go-pre-commit"))
	assert.False(t, h.e.InUse(""))

	require.NoError(t, h.s.Stop(t.Context(), id))
	h.ended(id)

	assert.False(t, h.e.Active())
	assert.False(t, h.e.InUse("mage-x"))
}

func TestAToolUpdatingItselfIsNoUseOfIt(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	spec := h.spec(testutil.FakeRun{Sleep: time.Minute})
	spec.Kind, spec.Tool = KindUpdate, "mage-x"
	id := h.start(spec)

	holder, held := h.locks.Holder(spec.Dir)
	assert.True(t, held, "the update holds its folder")
	assert.Equal(t, clonelock.KindUpdate, holder.Kind)
	assert.True(t, h.e.Active(), "the quit guard still asks while it goes")
	assert.False(t, h.e.InUse("mage-x"), "its own update doesn't keep a tool from being updated")
	assert.Equal(t, id, h.e.Updating("mage-x"), "but it says the tool is updating")
	assert.Zero(t, h.e.Updating("go-pre-commit"))
	assert.Zero(t, h.e.Updating(""))

	require.NoError(t, h.s.Stop(t.Context(), id))
	assert.Equal(t, StatusStopped, h.ended(id).Status)
	assert.Zero(t, h.e.Updating("mage-x"))
}

func TestShutdownStopsEveryRunWithinTheStopTimeout(t *testing.T) {
	t.Parallel()

	// desktopStopTimeout is the desktop shell's DefaultStopTimeout, which the shutdown
	// must fit inside before the database closes.
	const desktopStopTimeout = 5 * time.Second

	h := newHarness(t)
	h.e.quitKill, h.e.quitGrace = QuitKillAfter, quitGrace

	gentle := h.start(h.spec(testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute}))
	stubborn := h.start(h.spec(testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute, IgnoreSignals: true}))

	h.waitReady(gentle)
	h.waitReady(stubborn)

	begun := time.Now()

	h.e.StopAll()

	took := time.Since(begun)
	assert.Less(t, took, desktopStopTimeout, "the shutdown fits inside the shell's stop timeout")
	assert.GreaterOrEqual(t, took, QuitKillAfter, "a run that ignores SIGTERM gets SIGKILL after three seconds")

	for _, id := range []int64{gentle, stubborn} {
		rec := h.record(id)
		assert.Equal(t, StatusStopped, rec.Status, "run %d", id)
		assert.Equal(t, "Stopped as "+brand.Name+" quit.", rec.Summary)
	}

	assert.False(t, h.e.Active())

	_, err := h.e.Start(t.Context(), h.spec(testutil.FakeRun{}))
	require.ErrorIs(t, err, ErrClosed, "nothing starts once the app is quitting")
}

func TestShutdownCutsASlowFinishShort(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	spec := h.spec(testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute})
	spec.Finish = func(ctx context.Context, _ Ending, out Outcome) Outcome {
		<-ctx.Done() // a check that would take as long as it's let

		return out
	}

	id := h.start(spec)
	h.waitReady(id)

	begun := time.Now()

	h.e.StopAll()

	assert.Less(t, time.Since(begun), 5*time.Second, "a Finish that waits is cut short as the app quits")
	assert.Equal(t, StatusStopped, h.record(id).Status, "and the run is still recorded")
}

func TestTheWorkerStopsTheRunsWhenItsContextEnds(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	ctx, cancel := context.WithCancel(t.Context())

	var wg sync.WaitGroup

	wg.Go(func() { h.e.Run(ctx) })

	id := h.start(h.spec(testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute}))
	h.waitReady(id)

	cancel()
	wg.Wait()

	assert.Equal(t, StatusStopped, h.record(id).Status)
}

func TestStartRefusesWhatCantRun(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	good := h.spec(testutil.FakeRun{})

	for name, edit := range map[string]func(*Spec){
		"an unknown kind":           func(s *Spec) { s.Kind = "deploy" },
		"an unknown place":          func(s *Spec) { s.RunsIn = "cloud" },
		"a relative folder":         func(s *Spec) { s.Dir = "projects/sdk" },
		"no command":                func(s *Spec) { s.Argv = nil },
		"a relative program":        func(s *Spec) { s.Argv = []string{"magex", "lint"} },
		"an intent that isn't JSON": func(s *Spec) { s.Intent = func() {} },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			spec := good
			edit(&spec)

			_, err := h.e.Start(t.Context(), spec)
			require.ErrorIs(t, err, ErrInvalidSpec)
		})
	}

	t.Run("no log folder", func(t *testing.T) {
		t.Parallel()

		e := NewEngine(Config{DB: testutil.DB(t)})

		_, err := e.Start(t.Context(), good)
		require.ErrorIs(t, err, ErrInvalidSpec)
	})
}

func TestARunThatCantStartIsRecordedAsFailed(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	spec := h.spec(testutil.FakeRun{})
	spec.Argv = []string{filepath.Join(t.TempDir(), "missing")}

	id, err := h.e.Start(t.Context(), spec)
	require.Error(t, err)
	assert.Positive(t, id, "the failed run is in the history")
	assert.Equal(t, "The run couldn't start.", uierr.Message(err))

	rec := h.ended(id)
	assert.Equal(t, StatusFailed, rec.Status)
	assert.True(t, strings.HasPrefix(rec.Summary, "It couldn't start: "), rec.Summary)
	assert.Nil(t, rec.ExitCode)

	_, held := h.locks.Holder(spec.Dir)
	assert.False(t, held)
	assert.Empty(t, h.e.slots)
}

func TestAStaleLogIsReplaced(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	require.NoError(t, h.e.files.ensureDir())
	testutil.WriteFile(t, h.e.files.log(1), "a run the history forgot\n")

	id := h.start(h.spec(testutil.FakeRun{Stdout: "new\n"}))
	require.Equal(t, int64(1), id)
	h.ended(id)

	assert.Equal(t, "new\n", h.logText(id))
}

func TestDisplayCommandQuotesWhatAShellWouldRead(t *testing.T) {
	t.Parallel()

	const home = "/Users/example"

	assert.Equal(t, "/bin/magex version:bump bump=patch", DisplayCommand([]string{"/bin/magex", "version:bump", "bump=patch"}, home))
	assert.Equal(t, `/bin/zsh -c 'echo "$HOME" it'\''s'`, DisplayCommand([]string{"/bin/zsh", "-c", `echo "$HOME" it's`}, home))
	assert.Equal(t, "/bin/x ''", DisplayCommand([]string{"/bin/x", ""}, home))
	assert.Equal(t, "/bin/x 'café' '=y'", DisplayCommand([]string{"/bin/x", "café", "=y"}, home),
		"a word outside ASCII, or one zsh would read as a path, is quoted")
}

func TestDisplayCommandShowsTheProgramFromHome(t *testing.T) {
	t.Parallel()

	const home = "/Users/example"

	assert.Equal(t, "~/.local/bin/magex version:bump branch=master",
		DisplayCommand([]string{home + "/.local/bin/magex", "version:bump", "branch=master"}, home))
	assert.Equal(t, `~/'My Tools/magex' /Users/example/notes`,
		DisplayCommand([]string{home + "/My Tools/magex", home + "/notes"}, home),
		"~/ stays outside the quotes, and only the program is shortened")
	assert.Equal(t, "/Users/username/bin/magex", DisplayCommand([]string{"/Users/username/bin/magex"}, "/Users/user"),
		"a folder beside home, its name starting as home's does, isn't under it")
	assert.Equal(t, "/Users/example/bin/magex", DisplayCommand([]string{home + "/bin/magex"}, ""),
		"an unknown home shortens nothing")
}

func TestARunShowsItsProgramFromHomeAndRecordsItWhole(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	spec := h.spec(testutil.FakeRun{Stdout: "hello\n"})

	// The fake tool's folder stands in for home.
	h.e.home = filepath.Dir(spec.Argv[0])

	rec := h.ended(h.start(spec))

	assert.Equal(t, spec.Argv, rec.Argv, "the program's path is recorded whole")
	assert.Equal(t, "~/"+filepath.Base(spec.Argv[0]), rec.Command, "and shown from ~")
}

func TestSameStart(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

	assert.True(t, sameStart(at, at))
	assert.True(t, sameStart(at, at.Add(startSlack)), "Linux readings move a little")
	assert.False(t, sameStart(at, at.Add(startSlack+time.Millisecond)))
	assert.False(t, sameStart(at, time.Time{}), "an unreadable start matches nothing")
	assert.False(t, sameStart(time.Time{}, time.Time{}))
}
