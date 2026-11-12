package hooks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// refuseEnds makes the history refuse to record any run's end, as when the app goes just
// before it would, so a run that ends stays recorded as going for the next start; what it
// returns lets ends be recorded again.
func (r *rig) refuseEnds() func() {
	r.t.Helper()

	_, err := r.runner.DB.ExecContext(r.t.Context(), `CREATE TRIGGER refuse_finish BEFORE UPDATE ON script_runs
		WHEN NEW.finished_at IS NOT NULL BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)
	require.NoError(r.t, err)

	return func() {
		_, err := r.runner.DB.ExecContext(r.t.Context(), `DROP TRIGGER refuse_finish`)
		require.NoError(r.t, err)
	}
}

// leftGoing waits for the run id to end with its end unrecorded: it let go of the clone at
// dir and told the window, which it does whether its end was recorded or not, so its
// record still says it goes, as one the app restarted across does. told is how many events
// the service had emitted before the run started. It returns the run as the window sees it.
func (r *rig) leftGoing(id int64, dir string, told int) scriptrun.Run {
	r.t.Helper()

	r.runner.Await(func() bool {
		_, held := r.locks.Holder(dir)

		return !held && r.events.Count() > told
	}, "run %d lets go of its clone", id)

	runs, err := r.runs.Runs(r.t.Context(), scriptrun.Filter{})
	require.NoError(r.t, err)

	i := slices.IndexFunc(runs, func(run scriptrun.Run) bool { return run.ID == id })
	require.GreaterOrEqual(r.t, i, 0, "run %d is listed", id)
	require.True(r.t, runs[i].Status.Live(), "run %d is still recorded as going", id)

	return runs[i]
}

// restart starts the runner again over the same history and logs, with the hooks Verifier
// registered, as the next start of the app does, and returns the run id once it is settled.
func (r *rig) restart(id int64) scriptrun.Run {
	r.t.Helper()

	verify := NewVerifier(r.svc)

	return r.runner.Restart(id, map[scriptrun.Kind]scriptrun.Verifier{
		scriptrun.KindHooks: verify, scriptrun.KindChecks: verify,
	})
}

// keeps reports whether the run id keeps anything beside its record.
func (r *rig) keeps(id int64) bool {
	r.t.Helper()

	var raw json.RawMessage

	found, err := r.engine.Kept(id, &raw)
	require.NoError(r.t, err)

	return found
}

// keptFiles returns the kept states in the runner's log folder.
func (r *rig) keptFiles() []string {
	r.t.Helper()

	names, err := filepath.Glob(filepath.Join(r.runner.LogDir, "*.kept"))
	require.NoError(r.t, err)

	return names
}

// keptRuns stands in for the runner's kept states: what each run kept, by its ID.
type keptRuns struct {
	Runs

	kept map[int64]any
}

func (k keptRuns) Kept(id int64, into any) (bool, error) {
	v, ok := k.kept[id]
	if !ok {
		return false, nil
	}

	data, err := json.Marshal(v)
	if err != nil {
		return false, err
	}

	return true, json.Unmarshal(data, into)
}

// endingGit is git whose read of a clone's status ends the context of the Finish that
// asked, as the app's quit does: after the read when after is set, else before it, which
// then fails as a read the quit cut short does.
type endingGit struct {
	Git

	cancel context.CancelFunc
	after  bool
}

func (g endingGit) Entries(ctx context.Context, dir string) ([]gitread.Entry, error) {
	if !g.after {
		g.cancel()

		return nil, ctx.Err()
	}

	entries, err := g.Git.Entries(ctx, dir)
	g.cancel()

	return entries, err
}

// actionIntent is a hooks action's intent as its record holds it, decoded as plain JSON.
func actionIntent(action string) map[string]any {
	return map[string]any{"action": action, "confirm": ""}
}

// checksIntent is a checks run's intent as its record holds it, decoded as plain JSON.
func checksIntent(scope, check string) map[string]any {
	return map[string]any{"scope": scope, "check": check}
}

// checksRecord is the record of a checks run of every file in the clone at dir, as the next
// start reads it.
func checksRecord(id int64, dir string) scriptrun.Record {
	return scriptrun.Record{
		ID: id, Kind: scriptrun.KindChecks, Dir: dir, RunsIn: scriptrun.RunsInApp,
		Result: scriptrun.Result{Intent: checksIntent("all", "")},
	}
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

	going := r.leftGoing(id, dir, told)
	assert.Equal(t, actionIntent("replace"), going.Result.Intent,
		"what its Before read stays out of what the window reads")
	assert.True(t, r.keeps(id), "it is kept beside the record instead")
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
	assert.Equal(t, actionIntent("replace"), run.Result.Intent)
	assert.False(t, r.keeps(id), "what it kept goes once its end is recorded")
	assert.Empty(t, watch.told(t), "the page is told once the run has let go of its clone, its end kept")
}

func TestAChecksRunCutOffByARestartNamesTheFilesItChanged(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv, origin: "acme-org/web"})
	testutil.WriteFileMode(t, filepath.Join(dir, "README.md"), "# web  \n", 0o600)
	testutil.WriteFileMode(t, filepath.Join(dir, "docs/setup.md"), "setup\n", 0o600)
	testutil.RunGit(t, dir, "add", ".")
	testutil.RunGit(t, dir, "commit", "-q", "-m", "docs")

	// The user's own uncommitted edit, which whitespace's fix mixes its own into.
	testutil.WriteFileMode(t, filepath.Join(dir, "docs/setup.md"), "setup\nmore  \n", 0o600)

	r.tool("1.13.1", loadRecording(t, "run-whitespace-dirty-file").fakeRun(
		testutil.FakeFile{Path: "README.md", Content: "# web\n"},
		testutil.FakeFile{Path: "docs/setup.md", Content: "setup\nmore\n"},
	))

	allow := r.refuseEnds()
	told := r.events.Count()

	id, err := r.svc.RunChecks(t.Context(), ChecksRequest{Dir: r.shown(dir), Scope: ScopeAll})
	require.NoError(t, err)

	going := r.leftGoing(id, dir, told)
	assert.Equal(t, checksIntent("all", ""), going.Result.Intent,
		"no snapshot in what the window reads while the run goes")
	assert.True(t, r.keeps(id), "the snapshot is kept beside the record instead")
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
	assert.Equal(t, checksIntent("all", ""), run.Result.Intent, "nor in the record of its end")
	assert.False(t, r.keeps(id))
	assert.Empty(t, r.keptFiles())

	require.Eventually(t, func() bool {
		v, ok, _ := r.memory.Get(t.Context(), checksRanKey)

		return ok && v == "true"
	}, runWait, runPoll, "a checks run ended")
}

func TestAChecksRunCutOffByARestartUsesTheConfigItRanWith(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	r.tool("1.13.1", testutil.FakeRun{Stdout: "✓ eof check passed (1ms)\n✓ 1 passed on 6 file(s) in 1ms\n"})

	allow := r.refuseEnds()
	told := r.events.Count()

	id, err := r.svc.RunChecks(t.Context(), ChecksRequest{Dir: r.shown(dir), Scope: ScopeAll})
	require.NoError(t, err)
	r.leftGoing(id, dir, told)
	allow()

	// The config changes while the app is away: gitleaks on, lint off.
	testutil.WriteFileMode(t, filepath.Join(dir, ".github/env/99-local.env"),
		"GO_PRE_COMMIT_ENABLE_GITLEAKS=true\nGO_PRE_COMMIT_ENABLE_LINT=false\n", 0o600)

	r.restart(id)

	res, err := r.svc.ChecksResult(t.Context(), id)
	require.NoError(t, err)

	assert.Equal(t, CheckOff, checkStates(res)[CheckGitleaks], "off in the config the run ran with")
	assert.Equal(t, CheckNotRun, checkStates(res)[CheckLint], "on in the config the run ran with")
}

func TestWhatARunsBeforeReadStaysOutOfItsRecord(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	testutil.WriteFileMode(t, filepath.Join(dir, "notes.txt"), "draft\n", 0o600)
	r.tool("1.13.1", loadRecording(t, "run-pass").fakeRun())

	// A run that ends as it should.
	run, _ := r.check(dir, ChecksRequest{Scope: ScopeOne, Check: CheckLint})
	assert.Equal(t, checksIntent("one", "lint"), run.Result.Intent)
	assert.False(t, r.keeps(run.ID))

	// Runs whose start fails once their Before ran, the program gone.
	r.tools.set(factsAt(filepath.Join(t.TempDir(), "gone", "go-pre-commit"), "1.13.1"), "")

	id, err := r.svc.RunChecks(t.Context(), ChecksRequest{Dir: r.shown(dir), Scope: ScopeAll})
	require.Error(t, err)
	require.Positive(t, id)

	failed := r.ended(id)
	assert.Equal(t, scriptrun.StatusFailed, failed.Status)
	assert.Equal(t, checksIntent("all", ""), failed.Result.Intent)

	id, err = r.svc.Install(t.Context(), ActionRequest{Dir: r.shown(dir)})
	require.Error(t, err)
	require.Positive(t, id)

	failed = r.ended(id)
	assert.Equal(t, actionIntent("install"), failed.Result.Intent)

	assert.Empty(t, r.keptFiles(), "nothing is left beside any record")
}

func TestARunFromBeforeTheRestartThatKeptNothingSaysSo(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	v := NewVerifier(r.svc)
	zero := 0

	// One that kept nothing, as from before runs kept their state: the checks are read from
	// the log, the files they changed can't be.
	checks := v.Verify(checksRecord(7, dir))
	require.NotNil(t, checks.Finish)

	_, err := checks.Output.Write([]byte(loadRecording(t, "run-pass").Log("\n")))
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
		Result: scriptrun.Result{Intent: actionIntent("install")},
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
	r.svc.runs = keptRuns{Runs: r.engine, kept: map[int64]any{
		7: keptChecks{Checks: readConfig(dir).settings(), Snapshot: keepSnapshot(snapshot{err: errFake})},
	}}
	zero := 0

	out := NewVerifier(r.svc).Verify(checksRecord(7, dir)).Finish(t.Context(), scriptrun.Ending{ExitCode: &zero},
		scriptrun.Outcome{Status: scriptrun.StatusSucceeded})
	res, ok := out.Detail.(ChecksResult)
	require.True(t, ok)

	assert.False(t, res.ChangedKnown)
	assert.Equal(t, "Fake: failed.", res.ChangedError, "as its own Finish would have said")
}

func TestAChecksRunWithTooManyDirtyFilesToHashIsStillComparedAfterARestart(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	testutil.WriteFileMode(t, filepath.Join(dir, "notes.txt"), "draft\n", 0o600)

	// As for a clone with more than maxHashed dirty files: sizes and times, not contents.
	before := r.svc.read(t.Context(), dir, func(int) bool { return false })
	kept := keepSnapshot(before)
	require.False(t, kept.Hashed)
	require.Len(t, kept.Files, len(before.entries), "kept however many files it holds")

	r.svc.runs = keptRuns{Runs: r.engine, kept: map[int64]any{
		7: keptChecks{Checks: readConfig(dir).settings(), Snapshot: kept},
	}}

	testutil.WriteFileMode(t, filepath.Join(dir, "notes.txt"), "draft, longer now\n", 0o600)

	zero := 0
	out := NewVerifier(r.svc).Verify(checksRecord(7, dir)).Finish(t.Context(), scriptrun.Ending{ExitCode: &zero},
		scriptrun.Outcome{Status: scriptrun.StatusSucceeded})
	res, ok := out.Detail.(ChecksResult)
	require.True(t, ok)

	assert.True(t, res.ChangedKnown)
	assert.Equal(t, []ChangedFile{{Path: "notes.txt", WasDirty: true, By: []Check{}}}, res.Changed)
}

func TestAChecksRunsFinishIsKeptUnlessTheQuitCutsAReadShort(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name    string
		after   bool
		settled bool
	}{
		{name: "the quit comes after git's status was read", after: true, settled: true},
		{name: "the quit cuts the read of git's status short", after: false, settled: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newRig(t)
			dir := r.clone("web", cloneOpts{config: coreEnv})
			testutil.WriteFileMode(t, filepath.Join(dir, "notes.txt"), "draft\n", 0o600)

			r.svc.runs = keptRuns{Runs: r.engine, kept: map[int64]any{
				7: keptChecks{Checks: readConfig(dir).settings(), Snapshot: keepSnapshot(r.svc.take(t.Context(), dir))},
			}}

			testutil.WriteFileMode(t, filepath.Join(dir, "notes.txt"), "draft, fixed\n", 0o600)

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			r.svc.git = endingGit{Git: r.git, cancel: cancel, after: tt.after}

			zero := 0
			out := NewVerifier(r.svc).Verify(checksRecord(7, dir)).Finish(ctx, scriptrun.Ending{ExitCode: &zero},
				scriptrun.Outcome{Status: scriptrun.StatusSucceeded})

			if !tt.settled {
				assert.True(t, out.Unsettled, "left for the next start, which reads the clone again")

				return
			}

			require.False(t, out.Unsettled, "a Finish that read all it needed stands, however late the quit")

			res, ok := out.Detail.(ChecksResult)
			require.True(t, ok)
			assert.Equal(t, []ChangedFile{{Path: "notes.txt", WasDirty: true, By: []Check{}}}, res.Changed)
		})
	}
}

func TestAnActionsFinishIsKeptUnlessTheQuitCutsItsStatusShort(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name     string
		answers  bool
		settled  bool
		expected ActionOutcome
	}{
		{name: "the quit comes after status answered", answers: true, settled: true, expected: OutcomeCurrent},
		{name: "the quit cuts status short", answers: false, settled: false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newRig(t)
			dir := r.clone("api", cloneOpts{config: coreEnv, hook: currentHook})

			// A reinstall that left the hook as it was, so status must judge the bytes.
			r.svc.runs = keptRuns{Runs: r.engine, kept: map[int64]any{
				9: keptAction{Tool: keepTool(r.tools.HooksTool(t.Context())), Hooks: keepHooks(r.svc.look(t.Context(), dir))},
			}}

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			r.svc.runner = func(env []string, at string) execpath.Runner {
				return &testutil.Runner{Script: func(sctx context.Context, argv []string) ([]byte, []byte, error) {
					if !tt.answers {
						cancel()

						return nil, nil, sctx.Err()
					}

					stdout, stderr, err := r.status.runner(env, at).Run(sctx, argv[0], argv[1:]...)

					cancel()

					return stdout, stderr, err
				}}
			}

			zero := 0
			out := NewVerifier(r.svc).Verify(scriptrun.Record{
				ID: 9, Kind: scriptrun.KindHooks, Dir: dir,
				Result: scriptrun.Result{Intent: actionIntent("reinstall")},
			}).Finish(ctx, scriptrun.Ending{ExitCode: &zero}, scriptrun.Outcome{Status: scriptrun.StatusSucceeded})

			if !tt.settled {
				assert.True(t, out.Unsettled, "left for the next start, which asks status again")

				return
			}

			require.False(t, out.Unsettled)

			res, ok := out.Detail.(ActionResult)
			require.True(t, ok)
			assert.Equal(t, tt.expected, res.Outcome)
		})
	}
}

func TestAnActionAfterARestartIsJudgedWithTheCopyThatRan(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("api", cloneOpts{config: coreEnv})
	ran := r.tool("1.14.0", testutil.FakeRun{}) // 1.14.0 fails an install that exits non-zero
	before := r.svc.look(t.Context(), dir)
	facts := r.tools.HooksTool(t.Context())

	// It wrote its hook, then exited 1; meanwhile another copy came into use.
	testutil.WriteFileMode(t, filepath.Join(dir, ".git", "hooks", hookPreCommit), currentHook, 0o755)
	r.tool("1.13.1", testutil.FakeRun{})
	require.NotEqual(t, ran, r.tools.HooksTool(t.Context()).Path)

	r.svc.runs = keptRuns{Runs: r.engine, kept: map[int64]any{
		3: keptAction{Tool: keepTool(facts), Hooks: keepHooks(before)},
		4: keptAction{Tool: keepTool(facts), Hooks: keepHooks(r.svc.look(t.Context(), dir))},
	}}

	one := 1
	v := NewVerifier(r.svc)
	record := func(id int64, action string) scriptrun.Record {
		return scriptrun.Record{
			ID: id, Kind: scriptrun.KindHooks, Dir: dir,
			Result: scriptrun.Result{Intent: actionIntent(action)},
		}
	}

	out := v.Verify(record(3, "install")).Finish(t.Context(), scriptrun.Ending{ExitCode: &one},
		scriptrun.Outcome{Status: scriptrun.StatusFailed})
	res, ok := out.Detail.(ActionResult)
	require.True(t, ok)
	assert.Equal(t, OutcomeFailed, res.Outcome, "the exit status counts, as the copy that ran says")

	// A reinstall that left the hook as it was: only the copy that ran could say whether
	// those bytes are current, and it's no longer the one in use.
	out = v.Verify(record(4, "reinstall")).Finish(t.Context(), scriptrun.Ending{ExitCode: new(int)},
		scriptrun.Outcome{Status: scriptrun.StatusSucceeded})
	res, ok = out.Detail.(ActionResult)
	require.True(t, ok)
	assert.Equal(t, OutcomeUnverified, res.Outcome)
	assert.Equal(t, "Couldn't read what go-pre-commit did to the hooks in api: the copy of go-pre-commit that ran "+
		"isn't the one in use now, so it can't say whether the hook it left is current.", res.Message)
	assert.Empty(t, r.status.runs(), "no other copy's status stands in for it")
}

func TestVerifyLeavesARunItCantReadToTheRunner(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	v := NewVerifier(r.svc)

	for _, rec := range []scriptrun.Record{
		{Kind: scriptrun.KindChecks, Dir: dir, Result: scriptrun.Result{Intent: "not an intent"}},
		{Kind: scriptrun.KindChecks, Dir: dir, Result: scriptrun.Result{Intent: checksIntent("everything", "")}},
		{Kind: scriptrun.KindHooks, Dir: dir, Result: scriptrun.Result{Intent: []int{1}}},
		{Kind: scriptrun.KindHooks, Dir: dir, Result: scriptrun.Result{Intent: actionIntent("paint")}},
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
		checksRecord(1, dir),
		{Kind: scriptrun.KindHooks, Dir: dir, Result: scriptrun.Result{Intent: ActionIntent{Action: ActionInstall}}},
	} {
		out := v.Verify(rec).Finish(ctx, end, scriptrun.Outcome{Status: scriptrun.StatusInterrupted})
		assert.True(t, out.Unsettled, "%s: left for the next start", rec.Kind)
	}
}

func TestAKeptSnapshotReadsBackAsItWasTaken(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	testutil.WriteFileMode(t, filepath.Join(dir, "notes.txt"), "draft\n", 0o600)
	require.NoError(t, os.Remove(filepath.Join(dir, "README.md")))

	for _, hashed := range []bool{true, false} {
		snap := r.svc.read(t.Context(), dir, func(int) bool { return hashed })

		data, err := json.Marshal(keepSnapshot(snap))
		require.NoError(t, err)

		var kept keptSnapshot
		require.NoError(t, json.Unmarshal(data, &kept))

		back := kept.snapshot()
		assert.Equal(t, snap.hashed, back.hashed)
		assert.Equal(t, snap.sums, back.sums)
		assert.Empty(t, changedFiles(snap, back), "nothing reads as changed against itself")
	}
}
