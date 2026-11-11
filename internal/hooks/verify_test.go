package hooks

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// refuseEnds makes the history refuse to record any run's end, as when the app goes just
// before it would, so a run that ends stays recorded as going for the next start; what it
// returns lets ends be recorded again.
func (r *rig) refuseEnds() func() {
	r.t.Helper()

	_, err := r.db.ExecContext(r.t.Context(), `CREATE TRIGGER refuse_finish BEFORE UPDATE ON script_runs
		WHEN NEW.finished_at IS NOT NULL BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)
	require.NoError(r.t, err)

	return func() {
		_, err := r.db.ExecContext(r.t.Context(), `DROP TRIGGER refuse_finish`)
		require.NoError(r.t, err)
	}
}

// leftGoing waits for the run id to end with its end unrecorded: it let go of the clone at
// dir and told the window, which it does whether its end was recorded or not, so its
// record still says it goes, as one the app restarted across does. told is how many events
// the service had emitted before the run started.
func (r *rig) leftGoing(id int64, dir string, told int) {
	r.t.Helper()

	require.Eventually(r.t, func() bool {
		_, held := r.locks.Holder(dir)

		return !held && r.events.Count() > told
	}, runWait, runPoll, "run %d lets go of its clone", id)

	run, err := r.runs.Run(r.t.Context(), id)
	require.NoError(r.t, err)
	require.True(r.t, run.Status.Live(), "run %d is still recorded as going", id)
}

// restart starts the runner again over the same history and logs, with the hooks Verifier
// registered, as the next start of the app does, and returns the run id once it is settled.
func (r *rig) restart(id int64) scriptrun.Run {
	r.t.Helper()

	next := scriptrun.NewEngine(scriptrun.Config{
		DB: r.db, Locks: r.locks, LogDir: r.logDir, Home: r.home, Logger: r.logs.Logger(),
	})
	verify := NewVerifier(r.svc)
	next.RegisterVerifier(scriptrun.KindHooks, verify)
	next.RegisterVerifier(scriptrun.KindChecks, verify)

	ctx, cancel := context.WithCancel(r.t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		next.Run(ctx)
	}()

	runs := scriptrun.NewService(next)

	var run scriptrun.Run

	require.Eventually(r.t, func() bool {
		got, err := runs.Run(r.t.Context(), id)
		run = got

		return err == nil && !got.Status.Live()
	}, runWait, runPoll, "run %d is settled on the next start", id)

	cancel()
	<-done

	return run
}

// checksIntentOf reads the intent the run id's record holds now.
func (r *rig) checksIntentOf(id int64) ChecksIntent {
	r.t.Helper()

	run, err := r.runs.Run(r.t.Context(), id)
	require.NoError(r.t, err)

	var in ChecksIntent
	require.NoError(r.t, decodeIntent(run.Result.Intent, &in))

	return in
}

func TestAHooksActionCutOffByARestartIsJudgedOnTheNextStart(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("sdk", cloneOpts{config: coreEnv, hook: huskyHook, origin: "acme-org/sdk"})
	r.tool("1.13.1", loadRecording(t, "install-force-over-other").fakeRun(
		testutil.FakeFile{Path: filepath.Join(".git", "hooks", "pre-commit.backup.64138"), From: hookInClone},
		testutil.FakeFile{Path: hookInClone, Content: currentHook, Mode: 0o755},
	))

	allow := r.refuseEnds()
	told := r.events.Count()

	id, err := r.svc.Replace(t.Context(), ActionRequest{Dir: r.shown(dir)})
	require.NoError(t, err)
	r.leftGoing(id, dir, told)
	allow()

	watch := &releaseWatch{r: r, dir: dir}
	r.svc.events = watch

	run := r.restart(id)

	res, err := r.svc.ActionResult(t.Context(), id)
	require.NoError(t, err)

	assert.Equal(t, scriptrun.StatusSucceeded, run.Status, "the hooks show it done, though its end went unseen")
	assert.Equal(t, OutcomeReplaced, res.Outcome)
	assert.Equal(t, toolHusky, res.OtherTool, "named from the hook its Before kept, gone from its place since")
	assert.Equal(t, "pre-commit.backup.64138", res.Backup, "told apart from the backups its Before kept")
	assert.Equal(t, res.Message, run.Summary)
	assert.Equal(t, "~/projects/sdk/.git/hooks", res.HooksDir)
	assert.Empty(t, watch.told(t), "the page is told once the run has let go of its clone, its end kept")
}

func TestAChecksRunCutOffByARestartNamesTheFilesItChanged(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv, origin: "acme-org/web"})
	writeFile(t, dir, "README.md", "# web  \n", 0o600)
	writeFile(t, dir, "docs/setup.md", "setup\n", 0o600)
	testutil.RunGit(t, dir, "add", ".")
	testutil.RunGit(t, dir, "commit", "-q", "-m", "docs")

	// The user's own uncommitted edit, which whitespace's fix mixes its own into.
	writeFile(t, dir, "docs/setup.md", "setup\nmore  \n", 0o600)

	r.tool("1.13.1", loadRecording(t, "run-whitespace-dirty-file").fakeRun(
		testutil.FakeFile{Path: "README.md", Content: "# web\n"},
		testutil.FakeFile{Path: "docs/setup.md", Content: "setup\nmore\n"},
	))

	allow := r.refuseEnds()
	told := r.events.Count()

	id, err := r.svc.RunChecks(t.Context(), ChecksRequest{Dir: r.shown(dir), Scope: ScopeAll})
	require.NoError(t, err)
	r.leftGoing(id, dir, told)

	kept := r.checksIntentOf(id)
	require.NotNil(t, kept.Before, "the record keeps the snapshot while the run goes")
	require.Len(t, kept.Before.Files, 1)
	assert.Equal(t, "docs/setup.md", kept.Before.Files[0].Path)
	allow()

	run := r.restart(id)

	res, err := r.svc.ChecksResult(t.Context(), id)
	require.NoError(t, err)

	assert.Equal(t, scriptrun.StatusInterrupted, run.Status, "its end went unseen")
	assert.Equal(t, "5 passed, 1 failed: whitespace.", res.Message, "its whole log is read again")
	assert.True(t, res.ChangedKnown, "the clone now is compared with the snapshot kept")
	assert.Equal(t, []ChangedFile{
		{Path: "README.md", By: []Check{}},
		{Path: "docs/setup.md", WasDirty: true, By: []Check{}},
	}, res.Changed)

	require.Eventually(t, func() bool {
		v, ok, _ := r.memory.Get(t.Context(), checksRanKey)

		return ok && v == "true"
	}, runWait, runPoll, "a checks run ended")
}

func TestAChecksRunsRecordDropsItsSnapshotOnceItEnds(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	writeFile(t, dir, "notes.txt", "draft\n", 0o600)
	r.tool("1.13.1", loadRecording(t, "run-pass").fakeRun())

	run, _ := r.check(dir, ChecksRequest{Scope: ScopeOne, Check: CheckLint})

	assert.Equal(t, ChecksIntent{Scope: ScopeOne, Check: CheckLint}, r.checksIntentOf(run.ID),
		"the end's record keeps what was asked alone")
}

func TestARunFromBeforeTheRestartWithoutWhatItsBeforeReadSaysSo(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	v := NewVerifier(r.svc)
	zero := 0

	// Recorded before the snapshot was kept: the checks are read from the log, the files
	// they changed can't be.
	checks := v.Verify(scriptrun.Record{
		ID: 7, Kind: scriptrun.KindChecks, Dir: dir,
		Result: scriptrun.Result{Intent: map[string]any{"scope": "all", "check": ""}},
	})
	require.NotNil(t, checks.Finish)

	_, err := checks.Output.Write([]byte(loadRecording(t, "run-pass").log()))
	require.NoError(t, err)

	out := checks.Finish(t.Context(), scriptrun.Ending{ExitCode: &zero}, scriptrun.Outcome{Status: scriptrun.StatusSucceeded})
	res, ok := out.Detail.(ChecksResult)
	require.True(t, ok)

	assert.Equal(t, scriptrun.StatusSucceeded, out.Status)
	assert.Equal(t, "All 6 checks passed on 6 files in 2.6 s.", res.Message)
	assert.False(t, res.ChangedKnown)
	assert.Equal(t, brand.Name+" restarted while they ran, without a record of the clone from before them.",
		res.ChangedError)
	assert.Empty(t, res.Changed)

	// So with an action: what the hooks were before it is gone, so what it did can't be told.
	action := v.Verify(scriptrun.Record{
		ID: 8, Kind: scriptrun.KindHooks, Dir: dir,
		Result: scriptrun.Result{Intent: map[string]any{"action": "install", "confirm": ""}},
	})
	require.NotNil(t, action.Finish)

	out = action.Finish(t.Context(), scriptrun.Ending{Interrupted: "gone"},
		scriptrun.Outcome{Status: scriptrun.StatusInterrupted, Summary: "gone"})
	got, ok := out.Detail.(ActionResult)
	require.True(t, ok)

	assert.Equal(t, scriptrun.StatusInterrupted, out.Status)
	assert.Equal(t, "gone", out.Summary)
	assert.Equal(t, OutcomeUnverified, got.Outcome)
	assert.Equal(t, "Couldn't read what go-pre-commit did to the hooks in web: "+brand.Name+
		" restarted while it ran, without a record of the hooks from before it.", got.Message)
}

func TestAChecksRunWhoseBeforeCouldntReadTheCloneSaysWhyAfterARestart(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	zero := 0

	checks := NewVerifier(r.svc).Verify(scriptrun.Record{
		Kind: scriptrun.KindChecks, Dir: dir,
		Result: scriptrun.Result{Intent: ChecksIntent{
			Scope: ScopeAll, Before: keepSnapshot(snapshot{err: errFake}),
		}},
	})

	out := checks.Finish(t.Context(), scriptrun.Ending{ExitCode: &zero}, scriptrun.Outcome{Status: scriptrun.StatusSucceeded})
	res, ok := out.Detail.(ChecksResult)
	require.True(t, ok)

	assert.False(t, res.ChangedKnown)
	assert.Equal(t, "Fake: failed.", res.ChangedError, "as its own Finish would have said")
}

func TestVerifyLeavesARunItCantReadToTheRunner(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	v := NewVerifier(r.svc)

	for _, rec := range []scriptrun.Record{
		{Kind: scriptrun.KindChecks, Dir: dir, Result: scriptrun.Result{Intent: "not an intent"}},
		{Kind: scriptrun.KindChecks, Dir: dir, Result: scriptrun.Result{Intent: map[string]any{"scope": "everything"}}},
		{Kind: scriptrun.KindHooks, Dir: dir, Result: scriptrun.Result{Intent: []int{1}}},
		{Kind: scriptrun.KindHooks, Dir: dir, Result: scriptrun.Result{Intent: map[string]any{"action": "paint"}}},
		{Kind: scriptrun.KindScript, Dir: dir},
	} {
		got := v.Verify(rec)
		assert.Nil(t, got.Output, "%+v", rec)
		assert.Nil(t, got.Finish, "%+v", rec)
	}

	assert.Contains(t, r.logs.String(), "can't be checked")
}

func TestAVerifiedRunWaitsForTheShellUntilTheAppQuits(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	r.svc.ready = func() <-chan struct{} { return make(chan struct{}) } // never read

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	v := NewVerifier(r.svc)
	end := scriptrun.Ending{Interrupted: "gone"}

	for _, rec := range []scriptrun.Record{
		{Kind: scriptrun.KindChecks, Dir: dir, Result: scriptrun.Result{Intent: ChecksIntent{Scope: ScopeAll}}},
		{Kind: scriptrun.KindHooks, Dir: dir, Result: scriptrun.Result{Intent: ActionIntent{Action: ActionInstall}}},
	} {
		out := v.Verify(rec).Finish(ctx, end, scriptrun.Outcome{Status: scriptrun.StatusInterrupted})
		assert.True(t, out.Unsettled, "%s: left for the next start", rec.Kind)
	}
}
