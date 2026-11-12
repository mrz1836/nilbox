package scriptrun

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// failedCall is one call of Config.Failed: what it was told, with Started left out, and,
// as it was called, the status the run's record held, whether Started was the record's
// start, and whether the run's clone was still held.
type failedCall struct {
	f         Failure
	stored    Status
	sameStart bool
	held      bool
}

// failures collects Config.Failed's calls; h is the harness whose records it reads.
type failures struct {
	h     *harness
	mu    sync.Mutex
	calls []failedCall
}

// hook is Config.Failed, which reads the run's record and its clone's lock as it is
// called.
func (f *failures) hook(ctx context.Context, got Failure) {
	rec, _ := f.h.e.store.get(ctx, got.ID)
	_, held := f.h.locks.Holder(rec.Dir)
	sameStart := got.Started.Equal(rec.StartedAt)
	got.Started = time.Time{}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, failedCall{f: got, stored: rec.Status, sameStart: sameStart, held: held})
}

// all returns the calls so far.
func (f *failures) all() []failedCall {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]failedCall(nil), f.calls...)
}

// told is the call Config.Failed gets for f, once the run's end is kept and its clone free.
func told(f Failure) failedCall {
	return failedCall{f: f, stored: StatusFailed, sameStart: true}
}

// failingHarness returns a harness whose engine tells the returned failures of each run
// that failed; edit changes the config first.
func failingHarness(t *testing.T, edit ...func(*Config)) (*harness, *failures) {
	t.Helper()

	got := &failures{}
	h := newHarness(t, append([]func(*Config){func(cfg *Config) { cfg.Failed = got.hook }}, edit...)...)
	got.h = h

	return h, got
}

// settle waits for every run's follower to end, as the app's quit does, so each run that
// ended has told what it tells. Nothing starts after it.
func (h *harness) settle() {
	h.e.StopAll()
}

func TestAFailedRunIsToldOnceItsEndIsKeptAndItsCloneFree(t *testing.T) {
	t.Parallel()

	h, got := failingHarness(t)

	id := h.start(h.spec(testutil.FakeRun{Stderr: "boom\n", Exit: 3}))
	h.ended(id)
	h.settle()

	assert.Equal(t, []failedCall{told(Failure{
		ID: id, Repo: testRepo(), Title: "A script failed: Lint", Body: "It exited with status 3.",
	})}, got.all(), "once, with its end kept and its clone free, and the repository beside the title")

	// Its end recorded again, as a second follower or the next start might, changes nothing
	// and tells nothing.
	h.e.record(t.Context(), h.e.liveOf(h.record(id)), nil, Outcome{Status: StatusFailed, Summary: "Again."}, 0)
	h.e.reconcile(t.Context())

	assert.Len(t, got.all(), 1, "a run is told of once")
	assert.Equal(t, "It exited with status 3.", h.record(id).Summary)
}

func TestARunPastItsLimitIsToldAsTimedOut(t *testing.T) {
	t.Parallel()

	h, got := failingHarness(t)

	spec := h.spec(testutil.FakeRun{Sleep: time.Minute})
	spec.Kind, spec.Title, spec.Timeout = KindChecks, "Checks · all files", 300*time.Millisecond

	id := h.start(spec)
	h.ended(id)
	h.settle()

	assert.Equal(t, []failedCall{told(Failure{
		ID: id, Repo: testRepo(), Title: "Checks failed: Checks · all files", Body: "Timed out after 300ms.",
	})}, got.all())
}

func TestARunThatCantStartIsntTold(t *testing.T) {
	t.Parallel()

	h, got := failingHarness(t)

	spec := h.spec(testutil.FakeRun{})
	spec.Argv = []string{filepath.Join(t.TempDir(), "missing")}

	id, err := h.e.Start(t.Context(), spec)
	require.Error(t, err, "the start's error tells whoever asked")
	assert.Equal(t, StatusFailed, h.ended(id).Status)

	h.settle()
	assert.Empty(t, got.all())
}

func TestAFailedRunInAFolderNamesTheFolder(t *testing.T) {
	t.Parallel()

	home := testutil.RealTempDir(t)
	h, got := failingHarness(t, func(cfg *Config) { cfg.Home = home })

	dir := filepath.Join(home, "notes")
	require.NoError(t, os.Mkdir(dir, 0o700))

	spec := h.spec(testutil.FakeRun{Exit: 1})
	spec.Kind, spec.Title, spec.Repo, spec.Dir = KindUpdate, "Update magex", repo.Name{}, dir

	id := h.start(spec)
	h.ended(id)
	h.settle()

	assert.Equal(t, []failedCall{told(Failure{
		ID: id, Title: "A tool's update failed in ~/notes: Update magex", Body: "It exited with status 1.",
	})}, got.all(), "no repository for a folder that isn't one")
}

func TestNothingIsToldOfARunThatSucceeds(t *testing.T) {
	t.Parallel()

	h, got := failingHarness(t)

	assert.Equal(t, StatusSucceeded, h.ended(h.start(h.spec(testutil.FakeRun{}))).Status)
	h.settle()
	assert.Empty(t, got.all())
}

func TestNothingIsToldOfARunTheUserStopped(t *testing.T) {
	t.Parallel()

	h, got := failingHarness(t)

	plain := h.start(h.spec(testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute}))
	h.waitReady(plain)
	require.NoError(t, h.s.Stop(t.Context(), plain))
	assert.Equal(t, StatusStopped, h.ended(plain).Status)

	// Even when its kind makes a failure of the end.
	spec := h.spec(testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute})
	spec.Finish = func(context.Context, Ending, Outcome) Outcome {
		return Outcome{Status: StatusFailed, Summary: "Half done."}
	}

	judged := h.start(spec)
	h.waitReady(judged)
	require.NoError(t, h.s.Stop(t.Context(), judged))
	assert.Equal(t, StatusFailed, h.ended(judged).Status)

	h.settle()
	assert.Empty(t, got.all(), "the user stopped them")
}

func TestNothingIsToldOfARunASignalEnded(t *testing.T) {
	t.Parallel()

	h, got := failingHarness(t)

	for _, code := range []int{129, 130, 143} {
		rec := h.ended(h.start(h.spec(testutil.FakeRun{Exit: code})))
		assert.Equal(t, StatusFailed, rec.Status, "its record says how it ended, status %d", code)
	}

	h.settle()
	assert.Empty(t, got.all(), "a hang-up, Control-C, or SIGTERM: someone stopped it")
	assert.False(t, stoppedBySignal(1))
	assert.False(t, stoppedBySignal(137), "SIGKILL is the system's, as when memory runs out")
}

func TestNothingIsToldOfARunTheQuitStopped(t *testing.T) {
	t.Parallel()

	h, got := failingHarness(t)

	spec := h.spec(testutil.FakeRun{Stdout: readyLine, Sleep: time.Minute})
	spec.Finish = func(context.Context, Ending, Outcome) Outcome {
		return Outcome{Status: StatusFailed, Summary: "Half done."}
	}

	id := h.start(spec)
	h.waitReady(id)
	h.settle()

	assert.Equal(t, StatusFailed, h.record(id).Status, "its kind made a failure of the end")
	assert.Empty(t, got.all(), "but the quit stopped it")
}

func TestNothingIsToldOfAnEndNotKept(t *testing.T) {
	t.Parallel()

	var db *sql.DB

	h, got := failingHarness(t, func(cfg *Config) { db = cfg.DB })

	_, err := db.ExecContext(t.Context(), `CREATE TRIGGER refuse_finish BEFORE UPDATE ON script_runs
		WHEN NEW.finished_at IS NOT NULL BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)
	require.NoError(t, err)

	id := h.start(h.spec(testutil.FakeRun{Exit: 3}))

	require.Eventually(t, func() bool { return h.e.liveRun(id) == nil }, wait, poll, "run %d ends", id)
	h.settle()
	assert.Equal(t, StatusRunning, h.record(id).Status, "the next start settles it, and tells then")
	assert.Empty(t, got.all())
}

func TestNothingIsToldOfAnUnsettledEnd(t *testing.T) {
	t.Parallel()

	h, got := failingHarness(t)

	spec := h.spec(testutil.FakeRun{Exit: 3})
	spec.Finish = func(context.Context, Ending, Outcome) Outcome { return Outcome{Unsettled: true} }

	id := h.start(spec)

	require.Eventually(t, func() bool { return h.e.liveRun(id) == nil }, wait, poll, "run %d is let go", id)
	h.settle()
	assert.Equal(t, StatusRunning, h.record(id).Status)
	assert.Empty(t, got.all())
}

// failingVerifier settles a run from before a restart as failed.
type failingVerifier struct{}

func (failingVerifier) Verify(Record) Verification {
	return Verification{Callbacks: Callbacks{Finish: func(context.Context, Ending, Outcome) Outcome {
		return Outcome{Status: StatusFailed, Summary: "The tag isn't on GitHub."}
	}}}
}

func TestARunAVerifierSettlesAsFailedIsTold(t *testing.T) {
	t.Parallel()

	h, got := failingHarness(t)
	procs := newFakeProcs()
	procs.install(h.e)
	h.e.RegisterVerifier(KindScript, failingVerifier{})

	id := h.insertRow(RunsInApp, h.clone(), 5151, 5151, startedLong())
	h.e.reconcile(t.Context())
	h.ended(id)
	h.settle()

	assert.Equal(t, []failedCall{told(Failure{
		ID: id, Repo: testRepo(), Title: "A script failed: Lint", Body: "The tag isn't on GitHub.",
	})}, got.all())
}

func TestRunsFromBeforeARestartAreToldOfOnlyWhenTheyFailed(t *testing.T) {
	t.Parallel()

	h, got := failingHarness(t)
	procs := newFakeProcs()
	procs.install(h.e)

	interrupted := h.insertRow(RunsInApp, h.clone(), 5252, 5252, startedLong())
	failed := h.insertRow(RunsInTerminal, h.clone(), 5353, 5353, startedLong())
	succeeded := h.insertRow(RunsInTerminal, h.clone(), 5454, 5454, startedLong())

	require.NoError(t, h.e.files.ensureDir())
	testutil.WriteFile(t, h.e.files.exit(failed), "2\n")
	testutil.WriteFile(t, h.e.files.exit(succeeded), "0\n")

	h.e.reconcile(t.Context())

	assert.Equal(t, StatusInterrupted, h.ended(interrupted).Status)
	assert.Equal(t, StatusSucceeded, h.ended(succeeded).Status)
	assert.Equal(t, StatusFailed, h.ended(failed).Status)

	h.settle()
	assert.Equal(t, []failedCall{told(Failure{
		ID: failed, Repo: testRepo(), Title: "A script failed: Lint", Body: "It exited with status 2.",
	})}, got.all(), "a run in the terminal that failed while the app was away")
}

func TestFailureTitlesNameEachKind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		kind   Kind
		title  string
		folder string
		want   string
	}{
		{KindRelease, "Release v1.4.3", "", "A release failed: Release v1.4.3"},
		{KindHooks, "Install the hook", "", "A hook change failed: Install the hook"},
		{KindChecks, "Check whitespace", "", "Checks failed: Check whitespace"},
		{KindScript, "lint", "", "A script failed: lint"},
		{KindUpdate, "Update magex", "~/go/bin", "A tool's update failed in ~/go/bin: Update magex"},
		{KindPull, "Pull main", "", "A pull failed: Pull main"},
		{"other", "Something", "", "A run failed: Something"},
		{KindScript, "", "~/projects/notes", "A script failed in ~/projects/notes"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, failureTitle(tt.kind, tt.title, tt.folder))
	}
}
