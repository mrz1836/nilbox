package scriptruntest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// TestMain plays a fake tool when the test binary was started as one, and otherwise fails
// the run when a test leaves a goroutine behind.
func TestMain(m *testing.M) {
	testutil.FakeToolMain()
	goleak.VerifyTestMain(m)
}

// clone makes a clone called sdk under home, and returns its folder.
func clone(t *testing.T, home string) string {
	t.Helper()

	dir := filepath.Join(home, "sdk")
	require.NoError(t, os.MkdirAll(dir, 0o700))

	return dir
}

// start starts a script run in dir of a fake tool that plays run, with callbacks.
func start(t *testing.T, h *Harness, dir string, run testutil.FakeRun, callbacks scriptrun.Callbacks) int64 {
	t.Helper()

	path, env := testutil.FakeTool(t, "tool", run)

	id, err := h.Engine.Start(t.Context(), scriptrun.Spec{
		Callbacks: callbacks, Kind: scriptrun.KindScript, Title: "Lint", Source: "magex",
		Repo: repo.Name{Owner: "acme-org", Repo: "sdk"}, Dir: dir, Argv: []string{path}, Env: append(os.Environ(), env),
	})
	require.NoError(t, err)
	require.Positive(t, id)

	return id
}

// failingTest is a test the harness fails: it keeps what the harness said, and goes on
// where a real test would stop.
type failingTest struct {
	testing.TB

	mu     sync.Mutex
	said   []string
	failed bool
}

func (f *failingTest) Helper() {}

func (f *failingTest) Errorf(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.said = append(f.said, fmt.Sprintf(format, args...))
}

func (f *failingTest) FailNow() {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.failed = true
}

// failure returns whether the harness failed the test, and what it said.
func (f *failingTest) failure() (bool, string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.failed, strings.Join(f.said, "\n")
}

func TestEndedReturnsTheRunOnceItLetsGoOfItsClone(t *testing.T) {
	t.Parallel()

	home := testutil.RealTempDir(t)
	h := New(t, home, nil)
	dir := clone(t, home)

	var recordedHeld bool

	id := start(t, h, dir, testutil.FakeRun{Stdout: "ok\n"}, scriptrun.Callbacks{
		Recorded: func(context.Context, int64, scriptrun.Outcome) { _, recordedHeld = h.Locks.Holder(dir) },
	})

	run := h.Ended(id)
	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, "~/sdk", run.Dir)
	assert.True(t, recordedHeld, "the clone is still held once the end is recorded")

	_, held := h.Locks.Holder(dir)
	assert.False(t, held, "but not once Ended returns")
}

func TestEndedFailsTheTestWhenTheRunDoesntEndInTime(t *testing.T) {
	t.Parallel()

	t.Run("still going", func(t *testing.T) {
		t.Parallel()

		home := testutil.RealTempDir(t)
		dir := clone(t, home)
		ft := &failingTest{TB: t}
		h := New(ft, home, nil)
		h.wait = 50 * time.Millisecond

		id := start(t, h, dir, testutil.FakeRun{Sleep: time.Minute}, scriptrun.Callbacks{})
		h.Ended(id)

		failed, said := ft.failure()
		assert.True(t, failed)
		assert.Contains(t, said, fmt.Sprintf("run %d never ended: its record says it is running within 50ms", id))
	})

	t.Run("its clone still held", func(t *testing.T) {
		t.Parallel()

		home := testutil.RealTempDir(t)
		dir := clone(t, home)
		ft := &failingTest{TB: t}
		h := New(ft, home, nil)

		id := start(t, h, dir, testutil.FakeRun{}, scriptrun.Callbacks{})
		require.Equal(t, scriptrun.StatusSucceeded, h.Ended(id).Status)
		h.wait = 50 * time.Millisecond

		release, _, ok := h.Locks.TryLock(dir, clonelock.Holder{Kind: clonelock.KindHooks})
		require.True(t, ok)
		t.Cleanup(release)

		h.Ended(id)

		failed, said := ft.failure()
		assert.True(t, failed)
		assert.Contains(t, said, fmt.Sprintf("run %d never ended: it is recorded as succeeded, but a hooks run "+
			"still holds ~/sdk within 50ms", id))
	})

	t.Run("never recorded", func(t *testing.T) {
		t.Parallel()

		ft := &failingTest{TB: t}
		h := New(ft, testutil.RealTempDir(t), nil)
		h.wait = 50 * time.Millisecond

		h.Ended(99)

		failed, said := ft.failure()
		assert.True(t, failed)
		assert.Contains(t, said, "run 99 never ended: it can't be read: ")
	})
}

func TestAwaitLooksAgainAsTheRunsChange(t *testing.T) {
	t.Parallel()

	home := testutil.RealTempDir(t)
	h := New(t, home, nil)
	dir := clone(t, home)

	released := make(chan int64, 1)
	id := start(t, h, dir, testutil.FakeRun{}, scriptrun.Callbacks{Released: func(ran int64) { released <- ran }})

	h.Await(func() bool { return len(released) == 1 }, "run %d lets go of its clone", id)
	assert.Equal(t, id, <-released)

	ft := &failingTest{TB: t}
	failing := New(ft, home, nil)
	failing.wait = 20 * time.Millisecond

	failing.Await(func() bool { return false }, "the %s holds", "sky")

	failed, said := ft.failure()
	assert.True(t, failed)
	assert.Contains(t, said, "the condition never held within 20ms")
	assert.Contains(t, said, "the sky holds")
}

func TestAWaitThatRunsOutSaysWhyItMostLikelyDid(t *testing.T) {
	t.Parallel()

	// awaited fails a wait of 20ms on cond, and returns what the harness said.
	awaited := func(t *testing.T, cond func(h *Harness) bool) string {
		t.Helper()

		ft := &failingTest{TB: t}
		h := New(ft, testutil.RealTempDir(t), nil)
		h.wait = 20 * time.Millisecond

		h.Await(func() bool { return cond(h) })

		failed, said := ft.failure()
		require.True(t, failed)

		return said
	}

	t.Run("no change told", func(t *testing.T) {
		t.Parallel()

		said := awaited(t, func(*Harness) bool { return false })
		assert.Contains(t, said, "the condition never held within 20ms; the engine told no change while it waited, "+
			"so it looked only once")
	})

	t.Run("still not holding after the last change", func(t *testing.T) {
		t.Parallel()

		looks := 0
		said := awaited(t, func(h *Harness) bool {
			if looks++; looks == 1 {
				h.changes.tell() // one change, which wakes the wait to look again
			}

			return false
		})
		assert.Contains(t, said, "the condition never held within 20ms; it still didn't hold after the last of the 1 "+
			"changes the engine told, ")
		assert.Contains(t, said, " into the wait")
	})

	t.Run("holding with no change told since", func(t *testing.T) {
		t.Parallel()

		looks := 0
		said := awaited(t, func(*Harness) bool {
			looks++

			return looks > 1 // it comes to hold, but nothing tells the wait to look again
		})
		assert.Contains(t, said, "the condition never held within 20ms; it holds now, but the engine told no change "+
			"once it did, so the wait never looked again")
	})
}

// verified is a Verifier whose runs say they were verified on the next start.
type verified struct{}

func (verified) Verify(scriptrun.Record) scriptrun.Verification {
	return scriptrun.Verification{Callbacks: scriptrun.Callbacks{
		Finish: func(_ context.Context, _ scriptrun.Ending, out scriptrun.Outcome) scriptrun.Outcome {
			out.Summary = "Verified on the next start."

			return out
		},
	}}
}

func TestRestartSettlesARunLeftGoing(t *testing.T) {
	t.Parallel()

	home := testutil.RealTempDir(t)
	h := New(t, home, (&testutil.Logs{}).Logger())
	dir := clone(t, home)

	_, err := h.DB.ExecContext(t.Context(), `CREATE TRIGGER refuse_finish BEFORE UPDATE ON script_runs
		WHEN NEW.finished_at IS NOT NULL BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)
	require.NoError(t, err)

	id := start(t, h, dir, testutil.FakeRun{Stdout: "ok\n"}, scriptrun.Callbacks{})

	h.Await(func() bool {
		_, held := h.Locks.Holder(dir)

		return !held
	}, "run %d lets go of its clone", id)

	going, err := h.Runs.Run(t.Context(), id)
	require.NoError(t, err)
	require.True(t, going.Status.Live(), "its end went unrecorded")

	_, err = h.DB.ExecContext(t.Context(), `DROP TRIGGER refuse_finish`)
	require.NoError(t, err)

	run := h.Restart(id, map[scriptrun.Kind]scriptrun.Verifier{scriptrun.KindScript: verified{}})
	assert.Equal(t, scriptrun.StatusInterrupted, run.Status, "the next start found it gone")
	assert.Equal(t, "Verified on the next start.", run.Summary, "with the Verifier registered")

	_, held := h.Locks.Holder(dir)
	assert.False(t, held, "the next start let go of it")

	again := h.Next(nil)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	again.Run(ctx)

	settled, err := h.Runs.Run(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, run.Summary, settled.Summary, "a start after that leaves it as it was")
}
