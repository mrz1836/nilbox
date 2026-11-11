package scriptrun

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// recordedCall is one call of a spec's Recorded: the run, the outcome, and the status its
// record held then.
type recordedCall struct {
	id     int64
	status Status
	stored Status
}

// recordings collects a spec's Recorded calls.
type recordings struct {
	h     *harness
	mu    sync.Mutex
	calls []recordedCall
}

// hook is a spec's Recorded, which reads the run's record as it is called: a record it
// can't read holds no status.
func (r *recordings) hook(ctx context.Context, id int64, out Outcome) {
	rec, _ := r.h.e.store.get(ctx, id)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.calls = append(r.calls, recordedCall{id: id, status: out.Status, stored: rec.Status})
}

// all returns the calls so far.
func (r *recordings) all() []recordedCall {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]recordedCall(nil), r.calls...)
}

func TestRecordedIsToldOnceTheEndIsKept(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	got := &recordings{h: h}

	spec := h.spec(testutil.FakeRun{Exit: 3})
	spec.Recorded = got.hook

	id := h.start(spec)
	h.ended(id)

	assert.Equal(t, []recordedCall{{id: id, status: StatusFailed, stored: StatusFailed}}, got.all(),
		"once, with the outcome its record already holds")

	err := h.e.store.finish(t.Context(), finished{id: id, status: StatusSucceeded, at: time.Now()})
	require.ErrorIs(t, err, errNotRecorded, "a run that ended already isn't recorded again")
	assert.Equal(t, StatusFailed, h.record(id).Status)
}

func TestRecordedIsntToldWhenTheRecordFails(t *testing.T) {
	t.Parallel()

	var db *sql.DB

	h := newHarness(t, func(cfg *Config) { db = cfg.DB })
	got := &recordings{h: h}

	_, err := db.ExecContext(t.Context(), `CREATE TRIGGER refuse_finish BEFORE UPDATE ON script_runs
		WHEN NEW.finished_at IS NOT NULL BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)
	require.NoError(t, err)

	spec := h.spec(testutil.FakeRun{})
	spec.Recorded = got.hook

	id := h.start(spec)

	require.Eventually(t, func() bool { return h.e.liveRun(id) == nil }, wait, poll, "run %d ends", id)
	assert.Equal(t, StatusRunning, h.record(id).Status, "its end couldn't be recorded: the next start settles it")
	assert.Empty(t, got.all(), "nothing is told of an end not kept")
	assert.Contains(t, h.logs.String(), "couldn't record how a run ended")
}

func TestRecordedIsntToldForAnUnsettledEnd(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	got := &recordings{h: h}

	spec := h.spec(testutil.FakeRun{})
	spec.Recorded = got.hook
	spec.Finish = func(context.Context, Ending, Outcome) Outcome { return Outcome{Unsettled: true} }

	id := h.start(spec)

	require.Eventually(t, func() bool { return h.e.liveRun(id) == nil }, wait, poll, "run %d is let go", id)
	assert.Equal(t, StatusRunning, h.record(id).Status)
	assert.Empty(t, got.all())
}

func TestAVerifierGivesARunItsRecordedAgain(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	procs := newFakeProcs()
	procs.install(h.e)

	got := &recordings{h: h}
	h.e.RegisterVerifier(KindScript, recordedVerifier{hook: got.hook})

	id := h.insertRow(RunsInApp, h.clone(), 5151, 5151, startedLong())
	h.e.reconcile(t.Context())
	h.ended(id)

	assert.Equal(t, []recordedCall{{id: id, status: StatusInterrupted, stored: StatusInterrupted}}, got.all())
}

// recordedVerifier gives a run from before a restart only its Recorded.
type recordedVerifier struct {
	hook func(ctx context.Context, id int64, out Outcome)
}

func (v recordedVerifier) Verify(Record) Verification {
	return Verification{Recorded: v.hook}
}

// releasedCall is one call of a spec's Released: the run, the status its record held
// then, and whether its clone was held.
type releasedCall struct {
	id     int64
	stored Status
	held   bool
}

func TestReleasedIsToldOnceTheCloneIsFreeAndTheEndKept(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	dir := h.clone()

	var (
		mu    sync.Mutex
		calls []releasedCall
	)

	spec := h.spec(testutil.FakeRun{Exit: 3})
	spec.Dir = dir
	spec.Released = func(id int64) {
		rec, _ := h.e.store.get(t.Context(), id)
		_, held := h.locks.Holder(dir)

		mu.Lock()
		defer mu.Unlock()

		calls = append(calls, releasedCall{id: id, stored: rec.Status, held: held})
	}

	id := h.start(spec)
	h.ended(id)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()

		return len(calls) == 1
	}, wait, poll)

	mu.Lock()
	defer mu.Unlock()

	assert.Equal(t, []releasedCall{{id: id, stored: StatusFailed}}, calls, "once, with the clone free and the end kept")
}

func TestReleasedIsToldEvenWhenTheRecordFails(t *testing.T) {
	t.Parallel()

	var db *sql.DB

	h := newHarness(t, func(cfg *Config) { db = cfg.DB })

	_, err := db.ExecContext(t.Context(), `CREATE TRIGGER refuse_finish BEFORE UPDATE ON script_runs
		WHEN NEW.finished_at IS NOT NULL BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)
	require.NoError(t, err)

	released := make(chan int64, 1)

	spec := h.spec(testutil.FakeRun{})
	spec.Released = func(id int64) { released <- id }

	id := h.start(spec)

	select {
	case got := <-released:
		assert.Equal(t, id, got, "the clone is free, so a page reads it again")
	case <-time.After(wait):
		require.Fail(t, "Released was never told")
	}
}

func TestReleasedIsntToldForAnUnsettledEnd(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	released := make(chan int64, 1)

	spec := h.spec(testutil.FakeRun{})
	spec.Released = func(id int64) { released <- id }
	spec.Finish = func(context.Context, Ending, Outcome) Outcome { return Outcome{Unsettled: true} }

	id := h.start(spec)

	require.Eventually(t, func() bool { return h.e.liveRun(id) == nil }, wait, poll, "run %d is let go", id)
	assert.Empty(t, released, "a run left going hasn't ended")
}
