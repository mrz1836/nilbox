package scriptrun

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// failedCall is one call of Config.Failed: what it was told, and the status the run's
// record held then.
type failedCall struct {
	f      Failure
	stored Status
}

// failures collects Config.Failed's calls; h is the harness whose records it reads.
type failures struct {
	h     *harness
	mu    sync.Mutex
	calls []failedCall
}

// hook is Config.Failed, which reads the run's record as it is called.
func (f *failures) hook(ctx context.Context, got Failure) {
	rec, _ := f.h.e.store.get(ctx, got.ID)

	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, failedCall{f: got, stored: rec.Status})
}

// all returns the calls so far.
func (f *failures) all() []failedCall {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]failedCall(nil), f.calls...)
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

func TestAFailedRunIsToldOnceItsEndIsKept(t *testing.T) {
	t.Parallel()

	h, got := failingHarness(t)

	id := h.start(h.spec(testutil.FakeRun{Stderr: "boom\n", Exit: 3}))
	h.ended(id)

	assert.Equal(t, []failedCall{{f: Failure{
		ID: id, Kind: KindScript, Repo: testRepo(), Title: "A script failed in acme-org/sdk: Lint",
		Body: "It exited with status 3.",
	}, stored: StatusFailed}}, got.all(), "once, with its end already kept")

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

	assert.Equal(t, []failedCall{{f: Failure{
		ID: id, Kind: KindChecks, Repo: testRepo(), Title: "Checks failed in acme-org/sdk: Checks · all files",
		Body: "Timed out after 300ms.",
	}, stored: StatusFailed}}, got.all())
}

func TestARunThatCantStartIsToldAsFailed(t *testing.T) {
	t.Parallel()

	h, got := failingHarness(t)

	spec := h.spec(testutil.FakeRun{})
	spec.Argv = []string{filepath.Join(t.TempDir(), "missing")}

	id, err := h.e.Start(t.Context(), spec)
	require.Error(t, err)
	h.ended(id)

	calls := got.all()
	require.Len(t, calls, 1)
	assert.Equal(t, id, calls[0].f.ID)
	assert.True(t, strings.HasPrefix(calls[0].f.Body, "It couldn't start: "), calls[0].f.Body)
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

	assert.Equal(t, []failedCall{{f: Failure{
		ID: id, Kind: KindUpdate, Title: "A tool's update failed in ~/notes: Update magex",
		Body: "It exited with status 1.",
	}, stored: StatusFailed}}, got.all(), "no repository for a folder that isn't one")
}

func TestNothingIsToldOfARunThatSucceeds(t *testing.T) {
	t.Parallel()

	h, got := failingHarness(t)

	assert.Equal(t, StatusSucceeded, h.ended(h.start(h.spec(testutil.FakeRun{}))).Status)
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

	assert.Empty(t, got.all(), "the user stopped them")
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
	h.e.StopAll()

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
	assert.Equal(t, StatusRunning, h.record(id).Status)
	assert.Empty(t, got.all())
}

// failingVerifier settles a run from before a restart as failed.
type failingVerifier struct{}

func (failingVerifier) Verify(Record) Verification {
	return Verification{Finish: func(context.Context, Ending, Outcome) Outcome {
		return Outcome{Status: StatusFailed, Summary: "The tag isn't on GitHub."}
	}}
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

	assert.Equal(t, []failedCall{{f: Failure{
		ID: id, Kind: KindScript, Repo: testRepo(), Title: "A script failed in acme-org/sdk: Lint",
		Body: "The tag isn't on GitHub.",
	}, stored: StatusFailed}}, got.all())
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

	assert.Equal(t, []failedCall{{f: Failure{
		ID: failed, Kind: KindScript, Repo: testRepo(), Title: "A script failed in acme-org/sdk: Lint",
		Body: "It exited with status 2.",
	}, stored: StatusFailed}}, got.all(), "a run in the terminal that failed while the app was away")
}

func TestFailureTitlesNameEachKind(t *testing.T) {
	t.Parallel()

	where := "acme-org/example-repo"

	tests := []struct {
		kind  Kind
		title string
		want  string
	}{
		{KindRelease, "Release v1.4.3", "A release failed in acme-org/example-repo: Release v1.4.3"},
		{KindHooks, "Install the hook", "A hook change failed in acme-org/example-repo: Install the hook"},
		{KindChecks, "Check whitespace", "Checks failed in acme-org/example-repo: Check whitespace"},
		{KindScript, "lint", "A script failed in acme-org/example-repo: lint"},
		{KindUpdate, "Update magex", "A tool's update failed in acme-org/example-repo: Update magex"},
		{KindPull, "Pull main", "A pull failed in acme-org/example-repo: Pull main"},
		{"other", "Something", "A run failed in acme-org/example-repo: Something"},
		{KindScript, "", "A script failed in acme-org/example-repo"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, failureTitle(tt.kind, tt.title, where))
	}
}
