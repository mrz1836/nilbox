package hooks

import (
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// check runs the checks req asks for in the clone at dir, waits for them to end, and
// returns the run and its result.
func (r *rig) check(dir string, req ChecksRequest) (scriptrun.Run, ChecksResult) {
	r.t.Helper()

	req.Dir = r.shown(dir)

	id, err := r.svc.RunChecks(r.t.Context(), req)
	require.NoError(r.t, err)

	run := r.ended(id)

	res, err := r.svc.ChecksResult(r.t.Context(), id)
	require.NoError(r.t, err)

	return run, res
}

// checkStates returns each check's state in a result, by name.
func checkStates(res ChecksResult) map[Check]CheckState {
	out := map[Check]CheckState{}
	for _, c := range res.Checks {
		out[c.Name] = c.State
	}

	return out
}

func TestChecksCatchAChangeToAFileThatWasAlreadyDirty(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv, origin: "acme-org/web"})
	writeFile(t, dir, "README.md", "# web  \n", 0o600)
	writeFile(t, dir, "docs/setup.md", "setup\n", 0o600)
	testutil.RunGit(t, dir, "add", ".")
	testutil.RunGit(t, dir, "commit", "-q", "-m", "docs")

	// The user's own uncommitted edit, with trailing spaces of its own.
	writeFile(t, dir, "docs/setup.md", "setup\nmore  \n", 0o600)

	// whitespace fixes both; the fake can't stage, so docs/setup.md stays " M" and only
	// its contents tell it changed.
	path := r.tool("1.13.1", loadRecording(t, "run-whitespace-dirty-file").fakeRun(
		testutil.FakeFile{Path: "README.md", Content: "# web\n"},
		testutil.FakeFile{Path: "docs/setup.md", Content: "setup\nmore\n"},
	))

	run, res := r.check(dir, ChecksRequest{Scope: ScopeAll})

	assert.Equal(t, []string{path, "run", noColor, "--all-files"}, run.Argv)
	assert.Equal(t, "Checks · all files", run.Title)
	assert.Equal(t, scriptrun.KindChecks, run.Kind)
	assert.Equal(t, scriptrun.StatusFailed, run.Status)
	assert.Equal(t, "5 passed, 1 failed: whitespace.", run.Summary)
	assert.Empty(t, run.Result.Unrecognized)

	assert.True(t, res.ChangedKnown)
	assert.Equal(t, []ChangedFile{
		{Path: "README.md", By: []Check{}},
		{Path: "docs/setup.md", WasDirty: true, By: []Check{}},
	}, res.Changed)
	assert.Equal(t, CheckFailed, checkStates(res)[CheckWhitespace])
	assert.Equal(t, 5, res.Passed)
	assert.Equal(t, 1, res.Failed)
	assert.Equal(t, 6, res.Files)
	assert.Equal(t, "acme-org/web", res.Name)
	assert.Equal(t, "~/projects/web", res.Dir)
}

func TestChecksThatPass(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	r.tool("1.13.1", loadRecording(t, "run-pass").fakeRun())

	before := r.events.Count()

	run, res := r.check(dir, ChecksRequest{Scope: ScopeAll})

	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, "All 6 checks passed on 6 files in 2.6 s.", res.Message)
	assert.Empty(t, res.Changed)
	assert.Empty(t, res.Installs)
	assert.Empty(t, res.Errors)

	// Once its end is recorded, the page folds its note, and the row shows the run.
	require.Eventually(t, func() bool {
		v, ok, _ := r.memory.Get(t.Context(), checksRanKey)

		return ok && v == "true"
	}, runWait, runPoll)
	assert.Contains(t, r.events.All()[before:], event.Event(event.HooksUpdated{}))

	p := r.page(false)
	assert.Equal(t, ToolNote{Show: true}, p.ToolNote)
	assert.Equal(t, LastRun{ID: run.ID, Status: scriptrun.StatusSucceeded, Summary: res.Message, FinishedAt: run.FinishedAt},
		row(t, p, dir).LastChecks)
}

func TestChecksOnStagedFilesWithNothingStaged(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	path := r.tool("1.13.1", loadRecording(t, "run-no-files").fakeRun())

	run, res := r.check(dir, ChecksRequest{Scope: ScopeStaged})

	assert.Equal(t, []string{path, "run", noColor}, run.Argv)
	assert.Equal(t, "Checks · staged files", run.Title)
	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.True(t, res.NoFiles)
	assert.Equal(t, "No files to check.", res.Message)
}

func TestChecksTurnedOffInTheRepository(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("off", cloneOpts{config: coreEnv + "ENABLE_GO_PRE_COMMIT=false\n"})
	r.tool("1.13.1", loadRecording(t, "run-disabled").fakeRun())

	run, res := r.check(dir, ChecksRequest{Scope: ScopeAll})

	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.True(t, res.Disabled)
	assert.Equal(t, "go-pre-commit is turned off in this repository (ENABLE_GO_PRE_COMMIT=false).", res.Message)
}

func TestOneCheck(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	path := r.tool("1.13.1", loadRecording(t, "run-one-check").fakeRun())

	run, res := r.check(dir, ChecksRequest{Scope: ScopeOne, Check: CheckWhitespace})

	assert.Equal(t, []string{path, "run", "whitespace", noColor, "--all-files"}, run.Argv)
	assert.Equal(t, "Check whitespace", run.Title)
	assert.Equal(t, "1 failed: whitespace.", res.Message)
	assert.Equal(t, map[Check]CheckState{
		CheckEOF: CheckNotRun, CheckFumpt: CheckNotRun, CheckGitleaks: CheckNotRun, CheckLint: CheckNotRun,
		CheckModTidy: CheckNotRun, CheckWhitespace: CheckFailed,
	}, checkStates(res))
	assert.Equal(t, CheckResult{
		Name: CheckWhitespace, State: CheckFailed, DurationMs: 6, Error: "whitespace issues found", Detail: []string{"whitespace issues found"},
		ChangedFiles: []string{},
	}, res.Checks[0], "the checks that ran come first")

	// The checks a run of all leaves out because the repository turns them off say so.
	r.tool("1.13.1", loadRecording(t, "run-staged").fakeRun())

	_, res = r.check(dir, ChecksRequest{Scope: ScopeStaged})
	assert.Equal(t, CheckPassed, checkStates(res)[CheckGitleaks], "the recording's config turns gitleaks on")

	r.tool("1.13.1", testutil.FakeRun{Stdout: "✓ eof check passed (1ms)\n✓ 1 passed on 6 file(s) in 1ms\n"})

	_, res = r.check(dir, ChecksRequest{Scope: ScopeAll})
	assert.Equal(t, CheckOff, checkStates(res)[CheckGitleaks], "coreEnv leaves gitleaks off")
	assert.Equal(t, CheckNotRun, checkStates(res)[CheckLint])
	assert.Equal(t, "The check passed on 6 files in 1 ms.", res.Message)
}

func TestChecksReadTheJSONReportWhenTheCopyHasIt(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	writeFile(t, dir, "README.md", "# web  \n", 0o600)
	testutil.RunGit(t, dir, "add", "README.md")
	testutil.RunGit(t, dir, "commit", "-q", "-m", "spaces")

	path := r.tool("1.14.1", testutil.FakeRun{Stdout: report, Exit: 1, Files: []testutil.FakeFile{
		{Path: "README.md", Content: "# web\n"},
	}})

	run, res := r.check(dir, ChecksRequest{Scope: ScopeAll})

	assert.Equal(t, []string{path, "run", noColor, "--all-files", "--json"}, run.Argv)
	assert.Equal(t, scriptrun.StatusFailed, run.Status)
	assert.Equal(t, []ChangedFile{{Path: "README.md", By: []Check{CheckWhitespace}}}, res.Changed)
	assert.Equal(t, CheckSkipped, checkStates(res)[CheckLint])
	assert.Equal(t, 1, res.Skipped)
	assert.Equal(t, "1 passed, 1 failed: whitespace.", res.Message)
	assert.Empty(t, run.Result.Unrecognized)
}

func TestChecksThatCouldntRun(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	r.tool("1.13.1", testutil.FakeRun{Stderr: "Error: failed to load configuration: bad value\n", Exit: 1})

	run, res := r.check(dir, ChecksRequest{Scope: ScopeAll})

	assert.Equal(t, scriptrun.StatusFailed, run.Status)
	assert.Equal(t, []string{"failed to load configuration: bad value"}, res.Errors)
	assert.Equal(t, "go-pre-commit couldn't run the checks: failed to load configuration: bad value.", res.Message)

	r.tool("1.13.1", testutil.FakeRun{})

	_, res = r.check(dir, ChecksRequest{Scope: ScopeAll})
	assert.Equal(t, "The checks ended without saying how they came out.", res.Message)
}

func TestChecksWhenGitCantReadTheClone(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	r.svc.git = failingGit{Git: r.git, dir: dir}
	r.tool("1.13.1", loadRecording(t, "run-pass").fakeRun())

	_, res := r.check(dir, ChecksRequest{Scope: ScopeAll})

	assert.False(t, res.ChangedKnown)
	assert.Equal(t, "Fake: failed.", res.ChangedError)
	assert.Empty(t, res.Changed)
}

func TestChecksRefusedBeforeTheyStart(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	plain := r.clone("plain", cloneOpts{})
	ctx := t.Context()

	for _, req := range []ChecksRequest{
		{Dir: r.shown(dir), Scope: "everything"},
		{Dir: r.shown(dir), Scope: ScopeOne, Check: "spelling"},
		{Dir: r.shown(dir), Scope: ScopeAll, Check: CheckLint},
		{Dir: r.shown(dir), Scope: ScopeAll, RunsIn: "nowhere"},
		{Dir: "relative", Scope: ScopeAll},
	} {
		_, err := r.svc.RunChecks(ctx, req)
		require.ErrorIs(t, err, ErrInvalidRequest, "%+v", req)
	}

	_, err := r.svc.RunChecks(ctx, ChecksRequest{Dir: r.shown(plain), Scope: ScopeAll})
	require.ErrorIs(t, err, ErrNotSetUp)

	r.svc.places = fakePlaces{err: errFake}
	_, err = r.svc.RunChecks(ctx, ChecksRequest{Dir: r.shown(dir), Scope: ScopeAll})
	require.ErrorIs(t, err, errFake)

	r.tools.set(ToolFacts{}, "")
	_, err = r.svc.RunChecks(ctx, ChecksRequest{Dir: r.shown(dir), Scope: ScopeAll, RunsIn: scriptrun.RunsInApp})
	require.ErrorIs(t, err, ErrNoTool)

	bare := NewService(Config{Clones: r.clones, Git: r.git})
	_, err = bare.RunChecks(ctx, ChecksRequest{Dir: r.shown(dir), Scope: ScopeAll})
	require.ErrorIs(t, err, ErrNotSetUp)

	_, err = bare.ChecksResult(ctx, 1)
	require.ErrorIs(t, err, ErrNotSetUp)
}

func TestRunAgainRunsTheSameChecks(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	r.tool("1.13.1", loadRecording(t, "run-one-check").fakeRun())

	first, _ := r.check(dir, ChecksRequest{Scope: ScopeOne, Check: CheckWhitespace})

	id, err := r.runs.RunAgain(t.Context(), first.ID)
	require.NoError(t, err)

	again := r.ended(id)
	assert.Equal(t, first.Argv, again.Argv)

	res, err := r.svc.ChecksResult(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, ScopeOne, res.Scope)
	assert.Equal(t, CheckWhitespace, res.Check)

	_, err = r.svc.ActionResult(t.Context(), id)
	require.ErrorIs(t, err, ErrNoResult)
	assert.Equal(t, "This run has no hooks result.", uierr.Message(err))
}

func TestRerunRefusesWhatItCantWorkOut(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	rr := NewRerunner(r.svc)

	_, err := rr.Rerun(t.Context(), scriptrun.Record{Kind: scriptrun.KindScript, Dir: dir})
	require.ErrorIs(t, err, ErrInvalidRequest)

	_, err = rr.Rerun(t.Context(), scriptrun.Record{
		Kind: scriptrun.KindChecks, Dir: dir, Result: scriptrun.Result{Intent: "not an intent"},
	})
	require.ErrorIs(t, err, ErrInvalidRequest)
	require.ErrorContains(t, err, "invalid hooks request: the run's intent: json: cannot unmarshal string")

	_, err = rr.Rerun(t.Context(), scriptrun.Record{
		Kind: scriptrun.KindHooks, Dir: dir, Result: scriptrun.Result{Intent: []int{1}},
	})
	require.ErrorIs(t, err, ErrInvalidRequest)
	require.ErrorContains(t, err, "invalid hooks request: the run's intent: json: cannot unmarshal array")

	_, err = rr.Rerun(t.Context(), scriptrun.Record{Kind: scriptrun.KindHooks, Dir: "/srv/gone"})
	require.ErrorIs(t, err, ErrNoClone)
}

func TestChecksStatus(t *testing.T) {
	t.Parallel()

	zero, one := 0, 1

	for _, c := range []struct {
		ok     bool
		end    scriptrun.Ending
		engine scriptrun.Outcome
		want   scriptrun.Status
		said   string
	}{
		{true, scriptrun.Ending{ExitCode: &zero}, scriptrun.Outcome{Status: scriptrun.StatusSucceeded}, scriptrun.StatusSucceeded, "mine"},
		{false, scriptrun.Ending{ExitCode: &one}, scriptrun.Outcome{Status: scriptrun.StatusFailed}, scriptrun.StatusFailed, "mine"},
		{true, scriptrun.Ending{TimedOut: true}, scriptrun.Outcome{Status: scriptrun.StatusFailed, Summary: "late"}, scriptrun.StatusFailed, "late"},
		{true, scriptrun.Ending{Stopped: true}, scriptrun.Outcome{Status: scriptrun.StatusStopped, Summary: "Stopped."}, scriptrun.StatusStopped, "Stopped."},
		{true, scriptrun.Ending{}, scriptrun.Outcome{Status: scriptrun.StatusInterrupted, Summary: "gone"}, scriptrun.StatusInterrupted, "gone"},
	} {
		status, said := checksStatus(c.ok, "mine", c.end, c.engine)
		assert.Equal(t, c.want, status)
		assert.Equal(t, c.said, said)
	}
}

func TestSecondsText(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "350 ms", secondsText(350))
	assert.Equal(t, "2.6 s", secondsText(2600))
	assert.Equal(t, "38 s", secondsText(37600))
	assert.Equal(t, "1 file", plural(1, "file"))
	assert.Equal(t, "Checks · all files", checksTitle(ChecksIntent{Scope: ScopeAll}))
	assert.True(t, ScopeStaged.Valid())
	assert.False(t, Scope("x").Valid())
	assert.Equal(t, len(Checks()), checkRank("plugin"))
}

// releaseWatch is an event sink that notes, as each HooksUpdated arrives, whether the
// clone is held and whether any run is still going, as the window would find them when it
// reads the page again.
type releaseWatch struct {
	r   *rig
	dir string

	mu    sync.Mutex
	saw   []string
	count int
}

func (w *releaseWatch) Emit(e event.Event) {
	if _, ok := e.(event.HooksUpdated); !ok {
		return
	}

	_, held := w.r.locks.Holder(w.dir)

	runs, err := w.r.runs.Runs(w.r.t.Context(), scriptrun.Filter{})
	live := err != nil || slices.ContainsFunc(runs, func(run scriptrun.Run) bool { return run.Status.Live() })

	w.mu.Lock()
	defer w.mu.Unlock()

	w.count++

	if held || live {
		w.saw = append(w.saw, fmt.Sprintf("held %t, live %t", held, live))
	}
}

// told waits for a HooksUpdated, and returns what any of them found still going.
func (w *releaseWatch) told(t *testing.T) []string {
	t.Helper()

	require.Eventually(t, func() bool {
		w.mu.Lock()
		defer w.mu.Unlock()

		return w.count > 0
	}, runWait, runPoll, "the window is told")

	w.mu.Lock()
	defer w.mu.Unlock()

	return slices.Clone(w.saw)
}

func TestTheWindowIsToldOnlyOnceARunLetsGoOfItsClone(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv})
	r.page(false)

	watch := &releaseWatch{r: r, dir: dir}
	r.svc.events = watch

	r.tool("1.13.1", writesHook(t, 0o755))
	_, res := r.act(ActionInstall, dir, ActionRequest{})
	assert.Equal(t, OutcomeInstalled, res.Outcome)
	assert.Empty(t, watch.told(t), "an action tells the window once its run is recorded and its clone free")

	watch = &releaseWatch{r: r, dir: dir}
	r.svc.events = watch

	r.tool("1.13.1", loadRecording(t, "run-pass").fakeRun())
	r.check(dir, ChecksRequest{Scope: ScopeAll})
	assert.Empty(t, watch.told(t), "so does a checks run")

	assert.Equal(t, StateReady, row(t, r.page(false), dir).State)
}
