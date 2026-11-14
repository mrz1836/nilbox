package release

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/scriptrun/scriptruntest"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// runWait bounds how long a test waits for a run to end, and runPoll how often it looks.
const (
	runWait = 30 * time.Second
	runPoll = 10 * time.Millisecond
)

// sdkRepo is the repository the release rig's clone holds, as a request names it, and
// majorVersion the major its v1.4.2 leads to.
const (
	sdkRepo      = "acme-org/sdk"
	majorVersion = "v2.0.0"
)

// rigOptions shape a release rig: base is the newest version, tagged on the first commit,
// and module, when set, is go.mod's module path, committed with the second.
type rigOptions struct {
	base   string
	module string

	// terminal gives the runner a terminal (releaseRig.term) for the runs in it.
	terminal bool
}

// releaseRig is a release end to end: a clone of a local bare origin, isolated from the
// user's git config, on master one commit past base (a copy of sdkFixtures' fixture for
// base); GitHub, the clone's origin, and ssh as
// fakes; a fake magex that replays a recording; and the real runner over a fresh database,
// with the release verifier registered. The fake magex only prints what the real one did,
// so after does to origin and the clone what the real one would have, just before the
// release is verified.
type releaseRig struct {
	t      *testing.T
	root   string
	env    []string
	origin string
	clone  string
	first  string
	second string

	github  *fakeGitHub
	logs    *testutil.Logs
	svc     *Service
	after   *afterGit
	notices *fakeNotifier
	events  *testutil.Events

	// runner is the real runner, whose history and logs the next start reads; locks,
	// engine, and runs are its clone locks, its Engine, and its Service.
	runner *scriptruntest.Harness
	locks  *clonelock.Set
	engine *scriptrun.Engine
	runs   *scriptrun.Service

	// term is the terminal the runs in it open in, with rigOptions.terminal, or nil.
	term *scriptruntest.Terminal

	tool     ToolFacts
	magexEnv string
}

// newReleaseRig builds a release rig with magex 1.30.2 replaying nothing, or skips the test
// without git.
func newReleaseRig(t *testing.T, opts rigOptions) *releaseRig {
	t.Helper()

	r := &releaseRig{t: t, env: testutil.Git(t), root: testutil.RealTempDir(t), logs: &testutil.Logs{}}
	r.origin, r.clone = filepath.Join(r.root, "origin.git"), filepath.Join(r.root, "sdk")

	c := sdkFixtures.Copy(t, sdkFixture{clone: "sdk", base: opts.base, module: opts.module}, r.root)
	r.first, r.second = c.first, c.second

	r.github = &fakeGitHub{statuses: map[string]RepoStatus{name("sdk").Key(): {
		Permission: "WRITE", DefaultBranch: branchMaster, HeadOID: r.second, Checks: ChecksSuccess,
	}}}

	var withTerm []scriptruntest.Option

	if opts.terminal {
		r.term = scriptruntest.NewTerminal(t)
		withTerm = append(withTerm, scriptruntest.WithTerminal(r.term))
	}

	r.runner = scriptruntest.New(t, r.root, r.logs.Logger(), withTerm...)
	r.locks, r.engine, r.runs = r.runner.Locks, r.runner.Engine, r.runner.Runs
	r.notices, r.events = &fakeNotifier{}, &testutil.Events{}
	r.magex("1.30.2", testutil.FakeRun{})

	r.svc = NewService(Config{
		GitHub: r.github, Clones: &fakeClones{root: r.root, list: []Clone{{Dir: r.clone, Origin: name("sdk"), OnHub: true}}},
		Git:     gitread.New(gitread.Config{Env: func() []string { return r.env }}),
		Origins: fakeOrigins{name: name("sdk"), onGitHub: true}, Tools: rigTools{r: r}, Locks: r.locks,
		Env:    func() ([]string, bool) { return append(slices.Clone(r.env), r.magexEnv), true },
		Runner: (&sshFake{}).runner, Runs: r.engine, Places: fakePlaces{where: Where{RunsIn: integrationprefs.RunsInApp}},
		History: r.runs, Notifier: r.notices, Home: r.root, Events: r.events, Logger: r.logs.Logger(),
	})
	r.after = &afterGit{verifyGit: r.svc.git}
	r.svc.git = r.after
	r.engine.RegisterVerifier(scriptrun.KindRelease, NewVerifier(r.svc))

	return r
}

// magex makes the copy of magex in use a fake at version that does what run says.
func (r *releaseRig) magex(version string, run testutil.FakeRun) {
	r.t.Helper()

	path, env := testutil.FakeTool(r.t, "magex", run)
	def := integrations.MageX()

	r.tool = ToolFactsOf(def, integrations.Tool{
		ID: def.ID, On: true, State: integrations.StateFound,
		Copy: integrations.Copy{Path: path, Runs: true, Version: integrations.Version{Raw: version}},
	})
	r.magexEnv = env
}

// replay is a fake magex run that prints the recording called name, every line in the
// order it arrived, then the extra lines, and exits as it did.
func (r *releaseRig) replay(name string, extra ...string) testutil.FakeRun {
	r.t.Helper()

	rec := loadRecording(r.t, name)

	return testutil.FakeRun{Stdout: string(rec.log("\n")) + strings.Join(extra, ""), Exit: rec.Exit}
}

// prepare prepares the rig's release, failing the test when it can't.
func (r *releaseRig) prepare(req PrepareRequest) Offer {
	r.t.Helper()

	req.Repo = sdkRepo

	o, err := r.svc.Prepare(r.t.Context(), req)
	require.NoError(r.t, err)

	return o
}

// requestFor is the release the sheet would start from o.
func requestFor(o Offer) StartRequest {
	return StartRequest{
		Repo: sdkRepo, Branch: o.Branch, Bump: o.Bump, Version: o.Version, Head: o.Head, Original: o.Current,
	}
}

// release prepares a patch and starts it, failing the test when it can't start.
func (r *releaseRig) release() int64 {
	r.t.Helper()

	return r.releaseIn("")
}

// releaseIn prepares a patch to run in runsIn, "" for the setting, and starts it there,
// failing the test when it can't start.
func (r *releaseRig) releaseIn(runsIn integrationprefs.RunsIn) int64 {
	r.t.Helper()

	o := r.prepare(PrepareRequest{RunsIn: runsIn})
	require.True(r.t, o.Ready, "%+v", o.Checks)

	req := requestFor(o)
	req.RunsIn = runsIn

	id, err := r.svc.Start(r.t.Context(), req)
	require.NoError(r.t, err)
	require.Positive(r.t, id)

	return id
}

// ended waits for the run to end and let go of its clone, and returns it.
func (r *releaseRig) ended(id int64) scriptrun.Run {
	r.t.Helper()

	run := r.runner.Ended(id)
	require.NoError(r.t, r.after.failure(), "what magex would have done")

	return run
}

// waitFor waits for the run's log to hold text.
func (r *releaseRig) waitFor(id int64, text string) {
	r.t.Helper()

	require.Eventually(r.t, func() bool {
		got, err := r.runs.Output(r.t.Context(), id, 0, 0)

		return err == nil && strings.Contains(got.Text, text)
	}, runWait, runPoll, "run %d prints %q", id, text)
}

// runCount returns how many runs the history holds.
func (r *releaseRig) runCount() int {
	r.t.Helper()

	all, err := r.runs.Runs(r.t.Context(), scriptrun.Filter{})
	require.NoError(r.t, err)

	return len(all)
}

// git runs git in the rig's clone, isolated as the rig's own commands are, from any
// goroutine: the release's Finish runs after on the runner's.
func (r *releaseRig) git(ctx context.Context, args ...string) error {
	return gitIn(ctx, r.env, r.clone, args...)
}

// gitIn runs git with args in dir with env.
func gitIn(ctx context.Context, env []string, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...) //nolint:gosec // test fixture; argv only, no shell
	cmd.Dir, cmd.Env = dir, env

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, out)
	}

	return nil
}

// tagged is what magex does when it tags at commit and, with sent, pushes the tag.
func (r *releaseRig) tagged(tag, commit string, sent bool) func(context.Context) error {
	return func(ctx context.Context) error {
		if err := r.git(ctx, "tag", "-a", tag, "-m", "GitHubRelease "+tag, commit); err != nil {
			return err
		}

		if !sent {
			return nil
		}

		return r.git(ctx, "push", "-q", "origin", "refs/tags/"+tag)
	}
}

// testingT is what a helper needs of a test: the test itself, or the *assert.CollectT an
// EventuallyWithT condition is given, in which a failure fails only that look.
type testingT interface {
	require.TestingT
	Helper()
}

// resultOf reads a release run's result.
func resultOf(t testingT, run scriptrun.Run) Result {
	t.Helper()

	data, err := json.Marshal(run.Result.Outcome)
	require.NoError(t, err)

	var res Result
	require.NoError(t, json.Unmarshal(data, &res))

	return res
}

// fakeNotifier keeps the notices of releases that reached GitHub, and of those their
// workflow published or failed, with whether each of the latter came with its context
// ended.
type fakeNotifier struct {
	mu        sync.Mutex
	told      []Notice
	published []Notice
	failed    []Notice
	ctxErrs   []error
}

func (f *fakeNotifier) Released(_ context.Context, n Notice) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.told = append(f.told, n)
}

func (f *fakeNotifier) Published(ctx context.Context, n Notice) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.published = append(f.published, n)
	f.ctxErrs = append(f.ctxErrs, ctx.Err())
}

func (f *fakeNotifier) WorkflowFailed(ctx context.Context, n Notice) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.failed = append(f.failed, n)
	f.ctxErrs = append(f.ctxErrs, ctx.Err())
}

// endedContexts returns, for each notice of a release published or failed, its context's
// error as it was told: nil while it went on.
func (f *fakeNotifier) endedContexts() []error {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.ctxErrs)
}

// all returns the notices of releases that reached GitHub told so far.
func (f *fakeNotifier) all() []Notice {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.told)
}

// followed returns the notices of releases published, and of failed workflows, told so
// far.
func (f *fakeNotifier) followed() (published, failed []Notice) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.published), slices.Clone(f.failed)
}

// rigTools is magex as the rig has it now.
type rigTools struct {
	r *releaseRig
}

func (t rigTools) ReleaseTool(context.Context) ToolFacts { return t.r.tool }

// fakePlaces says runs go where where says.
type fakePlaces struct {
	where Where
	err   error
}

func (f fakePlaces) Where(context.Context) (Where, error) { return f.where, f.err }

// afterGit is the git a release is verified with, which first runs then once, before its
// first read: what magex would have done to origin and the clone.
type afterGit struct {
	verifyGit

	mu   sync.Mutex
	then func(context.Context) error
	ran  bool
	err  error
}

func (a *afterGit) RemoteTagCommit(ctx context.Context, dir, tag string) (string, error) {
	a.first(ctx)

	return a.verifyGit.RemoteTagCommit(ctx, dir, tag)
}

func (a *afterGit) LocalTagCommit(ctx context.Context, dir, tag string) (string, error) {
	a.first(ctx)

	return a.verifyGit.LocalTagCommit(ctx, dir, tag)
}

func (a *afterGit) CurrentBranch(ctx context.Context, dir string) (string, error) {
	a.first(ctx)

	return a.verifyGit.CurrentBranch(ctx, dir)
}

func (a *afterGit) InProgress(ctx context.Context, dir string) (gitread.Operation, error) {
	a.first(ctx)

	return a.verifyGit.InProgress(ctx, dir)
}

// first runs then, once.
func (a *afterGit) first(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.then != nil && !a.ran {
		a.ran, a.err = true, a.then(ctx)
	}
}

// set makes then run before the release is verified.
func (a *afterGit) set(then func(context.Context) error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.then = then
}

// failure returns what then failed with, if it did.
func (a *afterGit) failure() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	return a.err
}

func TestPrepareOffersTheRelease(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	o := r.prepare(PrepareRequest{})

	assert.True(t, o.Ready, "%+v", o.Checks)
	assert.Equal(t, sdkRepo, o.Repo)
	assert.Equal(t, recVersion, o.Version)
	assert.Equal(t, r.second, o.Head)
	assert.Equal(t, []string{
		r.tool.Path, "version:bump", "bump=patch", "branch=master", "push=true", "expect=v1.4.3", "json=true", "ff-only",
	}, o.Argv)
	assert.Equal(t, 1, r.github.calls())

	minor := r.prepare(PrepareRequest{Bump: BumpMinor, RunsIn: integrationprefs.RunsInTerminal})
	assert.Equal(t, "v1.5.0", minor.Version)
	assert.Equal(t, "Release v1.5.0 in your terminal", minor.Button)
	assert.Equal(t, 1, r.github.calls(), "a choice decides again on the facts the sheet was shown")

	r.prepare(PrepareRequest{Refresh: true})
	assert.Equal(t, 2, r.github.calls(), "Refresh reads them again")

	r.prepare(PrepareRequest{Branch: branchMaster})
	assert.Equal(t, 2, r.github.calls(), "the default branch, named, is the same branch")

	r.prepare(PrepareRequest{Branch: branchMain})
	assert.Equal(t, 3, r.github.calls(), "another branch has facts of its own")
}

func TestPrepareServesTheKeptFactsForAWhile(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	clk := testutil.NewClock(testNow())
	r.svc.clock = clk

	r.prepare(PrepareRequest{})

	release, _, ok := r.locks.TryLock(r.clone, clonelock.Holder{Kind: clonelock.KindHooks, Repo: sdkRepo})
	require.True(t, ok)

	busy, found := checkOf(r.prepare(PrepareRequest{}), CheckBusy)
	require.True(t, found, "kept facts read the clone's lock again")
	assert.Equal(t, "A hooks install is using acme-org/sdk right now.", busy.Text)
	assert.Equal(t, 1, r.github.calls())

	release()

	clk.Advance(FactsFor)
	r.prepare(PrepareRequest{})
	assert.Equal(t, 2, r.github.calls(), "facts FactsFor old are read again")
}

func TestPrepareFindsTheClone(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	ctx := t.Context()

	_, err := r.svc.Prepare(ctx, PrepareRequest{Repo: "acme-org/other"})
	require.ErrorIs(t, err, ErrNoClone)
	assert.Equal(t, "There's no clone of acme-org/other in ~.", uierr.Message(err))

	_, err = r.svc.Prepare(ctx, PrepareRequest{Repo: sdkRepo, Dir: "~/elsewhere"})
	require.ErrorIs(t, err, ErrNoClone, "the folder shown picks among the clones")

	o, err := r.svc.Prepare(ctx, PrepareRequest{Repo: sdkRepo, Dir: "~/sdk"})
	require.NoError(t, err)
	assert.True(t, o.Ready)

	_, err = r.svc.Candidates(ctx, false)
	require.NoError(t, err)

	o, err = r.svc.Prepare(ctx, PrepareRequest{Repo: sdkRepo, Dir: r.clone, Refresh: true})
	require.NoError(t, err, "the Release page's own clones serve too")
	assert.True(t, o.Ready)

	for _, bad := range []PrepareRequest{
		{Repo: "not a repository"}, {Repo: sdkRepo, Bump: "huge"}, {Repo: sdkRepo, RunsIn: "elsewhere"},
	} {
		_, err = r.svc.Prepare(ctx, bad)
		require.ErrorIs(t, err, ErrInvalidRequest, "%+v", bad)
	}
}

func TestStartReleases(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	testutil.RunGit(t, r.clone, "checkout", "-q", "-b", recFeature)
	r.magex("1.30.2", r.replay("patch-feature-behind"))
	r.after.set(r.tagged(recVersion, r.second, true))

	run := r.ended(r.release())

	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, "v1.4.3 is on GitHub.", run.Summary)
	assert.Equal(t, scriptrun.KindRelease, run.Kind)
	assert.Equal(t, "Release v1.4.3", run.Title)
	assert.Equal(t, scriptrun.RunsInApp, run.RunsIn)
	assert.Equal(t, string(integrationprefs.MageX), run.Result.Tool)
	assert.Equal(t, map[string]any{
		"version": recVersion, "bump": "patch", "branch": "master", "original": recFeature, "head": r.second,
		"fastForward": true,
	}, run.Result.Intent, "magex 1.30.2 is told ff-only")
	assert.Empty(t, run.Result.Warnings, "a version tag not on HEAD is why it released, not a warning")

	logged, err := r.runs.Output(t.Context(), run.ID, 0, 0)
	require.NoError(t, err)
	assert.Contains(t, logged.Text, "Current version tag is not on HEAD", "the log keeps the line")

	res := resultOf(t, run)
	assert.Equal(t, Result{
		Kind: ResultReleased, Version: recVersion, Tag: recVersion, Commit: r.second, Head: r.second, Tagged: true,
		Branch: recFeature, Original: recFeature, Back: true,
		TagURL:      "https://github.com/acme-org/sdk/releases/tag/v1.4.3",
		WorkflowURL: "https://github.com/acme-org/sdk/actions?query=branch%3Av1.4.3",
		Commands:    []string{},
	}, res)

	assert.Equal(t, 1, r.github.calls(), "the release checked again with the sheet's GitHub facts")

	_, err = r.svc.Prepare(t.Context(), PrepareRequest{Repo: sdkRepo})
	require.NoError(t, err)
	assert.Equal(t, 2, r.github.calls(), "a release ended: the sheet reads the clone again")
}

// releaseInTerminal starts a patch in the terminal with magex replaying recording, held
// open until the test writes the file it returns or closes the terminal, and waits for
// the engine to see its shell. The terminal's shell starts with the fake magex's entry, as the user's login
// shell would hold the real one's PATH.
func (r *releaseRig) releaseInTerminal(recording string) (int64, string) {
	r.t.Helper()

	proceed := filepath.Join(r.t.TempDir(), "proceed")
	run := r.replay(recording)
	run.Sleep, run.WaitFor = time.Minute, proceed

	r.magex("1.30.2", run)
	r.term.Env = []string{r.magexEnv}

	id := r.releaseIn(integrationprefs.RunsInTerminal)
	r.runner.AwaitShell(id)

	return id, proceed
}

func TestAReleaseInTheTerminalIsVerifiedAsOneInTheApp(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion, terminal: true})
	testutil.RunGit(t, r.clone, "checkout", "-q", "-b", recFeature)
	r.after.set(r.tagged(recVersion, r.second, true))

	id, proceed := r.releaseInTerminal("patch-feature-behind")

	opened := r.term.Opened()
	require.Len(t, opened, 1, "the release opened in the terminal")
	assert.Equal(t, r.clone, opened[0].Dir)
	assert.Equal(t, r.tool.Path, opened[0].Argv[0], "magex runs there itself")
	assert.Contains(t, opened[0].Argv, "push=true")

	holder, held := r.locks.Holder(r.clone)
	require.True(t, held, "the clone is held while the terminal runs the release")
	assert.Equal(t, clonelock.KindRelease, holder.Kind)

	going, err := r.runs.Run(t.Context(), id)
	require.NoError(t, err)
	assert.True(t, going.Status.Live(), "Runs shows it going")
	assert.Equal(t, scriptrun.RunsInTerminal, going.RunsIn, "in the terminal")

	testutil.WriteFile(t, proceed, "") // magex ends, as it would in the user's terminal

	run := r.ended(id)

	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, "v1.4.3 is on GitHub.", run.Summary)
	assert.Equal(t, scriptrun.RunsInTerminal, run.RunsIn)
	assert.Equal(t, Result{
		Kind: ResultReleased, Version: recVersion, Tag: recVersion, Commit: r.second, Head: r.second, Tagged: true,
		Branch: recFeature, Original: recFeature, Back: true,
		TagURL:      "https://github.com/acme-org/sdk/releases/tag/v1.4.3",
		WorkflowURL: "https://github.com/acme-org/sdk/actions?query=branch%3Av1.4.3",
		Commands:    []string{},
	}, resultOf(t, run), "verified as a release in the app is")
	assert.Len(t, r.notices.all(), 1, "and told the same way")
}

func TestAReleaseWhoseTerminalClosedIsCheckedForItsTagAndBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sent    bool
		status  scriptrun.Status
		summary string
		kind    ResultKind
		told    int
	}{
		{
			name: "its tag reached GitHub", sent: true, status: scriptrun.StatusSucceeded,
			summary: "v1.4.3 is on GitHub.", kind: ResultReleased, told: 1,
		},
		{
			name: "its tag stayed on this Mac", status: scriptrun.StatusInterrupted,
			summary: "v1.4.3 is tagged on this Mac but isn't on GitHub.", kind: ResultNotPushed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newReleaseRig(t, rigOptions{base: shownVersion, terminal: true})
			testutil.RunGit(t, r.clone, "checkout", "-q", "-b", recFeature)

			// The user closes the terminal once magex tagged, and maybe pushed: magex is hung
			// up where it is, and its shell ends with no exit status.
			r.after.set(r.tagged(recVersion, r.second, tt.sent))

			id, _ := r.releaseInTerminal("sigterm-after-tag")

			// The tab closes mid-run, not mid-start: once magex has said all it did, which is
			// how the release knows the tag is its own.
			rec := loadRecording(t, "sigterm-after-tag")
			r.waitFor(id, rec.Lines[len(rec.Lines)-1].Text)
			require.NoError(t, r.term.Close())

			run := r.ended(id)

			res := resultOf(t, run)
			assert.Equal(t, tt.status, run.Status)
			assert.Equal(t, tt.summary, run.Summary)
			assert.Equal(t, tt.kind, res.Kind)
			assert.Equal(t, recVersion, res.Tag)
			assert.True(t, res.Tagged, "its log says magex made the tag")
			assert.Equal(t, recFeature, res.Branch, "it says where the clone is")
			assert.True(t, res.Back)
			assert.Equal(t, gitread.OperationNone, res.InProgress)
			assert.Len(t, r.notices.all(), tt.told)

			_, held := r.locks.Holder(r.clone)
			assert.False(t, held, "the clone is free once the shell is gone")
		})
	}
}

func TestStartReleasedANewerCommit(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	elsewhere := filepath.Join(r.root, "elsewhere")
	testutil.RunGit(t, r.root, "clone", "-q", r.origin, elsewhere)
	testutil.WriteFileMode(t, filepath.Join(elsewhere, "three.txt"), "three", 0o600)

	r.magex("1.30.2", r.replay("patch-feature-behind"))
	r.after.set(func(ctx context.Context) error {
		// A commit landed after the run's checks, and magex pulled and tagged it.
		for _, args := range [][]string{
			{"add", "three.txt"},
			{"commit", "-q", "-m", "three"},
			{"push", "-q", "origin", "HEAD:refs/heads/master"},
		} {
			if err := gitIn(ctx, r.env, elsewhere, args...); err != nil {
				return err
			}
		}

		for _, args := range [][]string{
			{"fetch", "-q", "origin", branchMaster},
			{"tag", "-a", recVersion, "-m", recVersion, "FETCH_HEAD"},
			{"push", "-q", "origin", "refs/tags/v1.4.3"},
		} {
			if err := r.git(ctx, args...); err != nil {
				return err
			}
		}

		return nil
	})

	run := r.ended(r.release())
	res := resultOf(t, run)

	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, ResultReleased, res.Kind)
	assert.True(t, res.Newer, "the tag is on a newer commit than the one the sheet showed")
	assert.NotEqual(t, r.second, res.Commit)
	assert.Equal(t, r.second, res.Head)
}

func TestStartReleasedAnotherVersionWithoutExpect(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	r.magex("1.29.5", r.replay("version-moved"))
	r.after.set(func(ctx context.Context) error {
		// v1.4.3 was released between the run's checks and magex's own fetch, so magex,
		// which can't be told which version to make, made v1.4.4.
		if err := r.tagged(recVersion, r.first, true)(ctx); err != nil {
			return err
		}

		return r.tagged("v1.4.4", r.second, true)(ctx)
	})

	id := r.release()
	run := r.ended(id)
	res := resultOf(t, run)

	assert.NotContains(t, strings.Join(run.Argv, " "), "expect=")
	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, "Released v1.4.4. v1.4.3 appeared on GitHub meanwhile.", run.Summary)
	assert.Equal(t, ResultReleased, res.Kind)
	assert.Equal(t, "v1.4.4", res.Tag)
	assert.True(t, res.Moved)

	told := r.notices.all()
	require.Len(t, told, 1)
	assert.Equal(t, "v1.4.4 is on GitHub", told[0].Title, "the version magex made")
	assert.Equal(t, "Tagged "+r.second[:shortOIDLen]+" on master. v1.4.3 appeared on GitHub meanwhile.", told[0].Body)
}

func TestASkippedPushIsntARelease(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	r.magex("1.29.5", r.replay("origin-unreachable-nobranch"))
	r.after.set(r.tagged(recVersion, r.second, false))

	run := r.ended(r.release())
	res := resultOf(t, run)

	require.NotNil(t, run.ExitCode)
	assert.Zero(t, *run.ExitCode, "magex 1.29.5 exits 0 after skipping the push")
	assert.Equal(t, scriptrun.StatusFailed, run.Status)
	assert.Equal(t, "v1.4.3 is tagged on this Mac but isn't on GitHub.", run.Summary)
	assert.Equal(t, ResultNotPushed, res.Kind)
	assert.True(t, res.Tagged)
	assert.Empty(t, res.Commit)
	assert.Contains(t, res.PushWarning, "Git remote validation failed")
	assert.Equal(t, []string{"git push origin v1.4.3", "git tag -d v1.4.3"}, res.Commands)
	assert.Nil(t, res.Failure, "nothing failed: magex skipped the push")
	assert.Empty(t, res.TagURL)
}

func TestARefusedTagIsNotPushed(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: "v0.0.1"})
	r.magex("1.29.5", r.replay("ruleset-refused"))
	r.after.set(r.tagged("v0.0.2", r.second, false))

	run := r.ended(r.release())
	res := resultOf(t, run)

	assert.Equal(t, scriptrun.StatusFailed, run.Status)
	assert.Equal(t, "v0.0.2 is tagged on this Mac but isn't on GitHub.", run.Summary)
	assert.Equal(t, ResultNotPushed, res.Kind)
	require.NotNil(t, res.Failure)
	assert.Equal(t, FailureRefused, res.Failure.Kind)
	assert.Equal(t, "GitHub refused the tag v0.0.2: Cannot create ref due to creations being restricted.",
		res.Failure.Heading)
}

func TestAFailedReleaseSaysWhy(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	r.magex("1.29.5", r.replay("signing-gpg-fails"))

	run := r.ended(r.release())
	res := resultOf(t, run)

	assert.Equal(t, scriptrun.StatusFailed, run.Status)
	assert.Equal(t, "Git couldn't sign the tag here. Release in your terminal, where signing can ask you.", run.Summary)
	assert.Equal(t, ResultFailed, res.Kind)
	assert.False(t, res.Tagged)
	require.NotNil(t, res.Failure)
	assert.Equal(t, FailureSigning, res.Failure.Kind)
	assert.Equal(t, branchMaster, res.Branch)
	assert.True(t, res.Back)
}

func TestUnknownLinesReachTheResult(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	r.magex("1.30.2", r.replay("patch-feature-behind", "A line no magex printed before\n"))
	r.after.set(r.tagged(recVersion, r.second, true))

	run := r.ended(r.release())

	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Contains(t, run.Result.Unrecognized, "A line no magex printed before")
	assert.Equal(t, len(run.Result.Unrecognized), run.Result.UnrecognizedCount)
}

func TestExpectAndJSONGoOnlyToAMagexThatHasThem(t *testing.T) {
	t.Parallel()

	for version, extra := range map[string][]string{
		"1.29.5": {},
		"1.30.0": {"expect=v1.4.3"},
		"1.30.1": {"expect=v1.4.3", "json=true"},
		"1.30.2": {"expect=v1.4.3", "json=true", "ff-only"},
	} {
		t.Run(version, func(t *testing.T) {
			t.Parallel()

			r := newReleaseRig(t, rigOptions{base: shownVersion})
			r.magex(version, testutil.FakeRun{})

			run := r.ended(r.release())

			want := append([]string{r.tool.Path, "version:bump", "bump=patch", "branch=master", "push=true"}, extra...)
			assert.Equal(t, want, run.Argv)
		})
	}
}

func TestStartRechecksUnderLock(t *testing.T) {
	t.Parallel()

	t.Run("a concurrent start is busy", func(t *testing.T) {
		t.Parallel()

		r := newReleaseRig(t, rigOptions{base: shownVersion})
		r.magex("1.30.2", testutil.FakeRun{Sleep: time.Minute})

		o := r.prepare(PrepareRequest{})

		var (
			wg   sync.WaitGroup
			ids  = make([]int64, 2)
			errs = make([]error, 2)
		)

		for i := range 2 {
			wg.Go(func() { ids[i], errs[i] = r.svc.Start(t.Context(), requestFor(o)) })
		}

		wg.Wait()

		started := slices.IndexFunc(errs, func(err error) bool { return err == nil })
		require.GreaterOrEqual(t, started, 0, "one of them starts: %v", errs)

		refused := errs[1-started]
		require.ErrorIs(t, refused, clonelock.ErrBusy, "the other finds the clone held")
		assert.Equal(t, "A release is using acme-org/sdk right now.", uierr.Message(refused))
		assert.Equal(t, 1, r.runCount())
	})

	t.Run("a fact that changed blocks", func(t *testing.T) {
		t.Parallel()

		r := newReleaseRig(t, rigOptions{base: shownVersion})

		var underLock []bool

		r.svc.gatherer.origins = fakeOrigins{name: name("sdk"), onGitHub: true, asking: func() {
			holder, held := r.locks.Holder(r.clone)
			underLock = append(underLock, held && holder.Kind == clonelock.KindRelease)
		}}

		o := r.prepare(PrepareRequest{})
		require.True(t, o.Ready)

		testutil.WriteFileMode(t, filepath.Join(r.clone, "notes.txt"), "uncommitted", 0o600)

		_, err := r.svc.Start(t.Context(), requestFor(o))
		require.ErrorIs(t, err, ErrNotReady)
		assert.Equal(t, "1 uncommitted change: notes.txt. magex won't tag with uncommitted work. Commit or stash it.",
			uierr.Message(err))
		assert.Equal(t, []bool{false, true}, underLock, "Start checks again holding the clone's lock")
		assert.Zero(t, r.runCount(), "nothing ran, nor was recorded")

		_, held := r.locks.Holder(r.clone)
		assert.False(t, held, "the lock is let go")
	})
}

func TestAVersionThatMovedSendsYouBackToTheSheet(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	o := r.prepare(PrepareRequest{})
	require.Equal(t, recVersion, o.Version)

	// Someone released v1.4.3 meanwhile.
	testutil.RunGit(t, r.clone, "tag", "-a", recVersion, "-m", recVersion, r.first)
	testutil.RunGit(t, r.clone, "push", "-q", "origin", "refs/tags/v1.4.3")

	_, err := r.svc.Start(t.Context(), requestFor(o))
	require.ErrorIs(t, err, ErrVersionMoved)
	assert.Equal(t, "v1.4.3 was released a moment ago. The next patch is v1.4.4.", uierr.Message(err))
	assert.Zero(t, r.runCount())

	again := r.prepare(PrepareRequest{})
	assert.Equal(t, "v1.4.4", again.Version, "the sheet reads the new version")
}

func TestAMajorTheSheetShouldNotHaveStartedIsRejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		version string
		module  string
		typed   string
		text    string
	}{
		{
			name: "without the typed version", version: "1.30.2", typed: "v2.0",
			text: "Type v2.0.0 to confirm the major release.",
		},
		{
			name: "with a magex lacking expect=", version: "1.29.5", typed: majorVersion,
			text: "A major release needs magex 1.30.0 or later, which can be told which version to make.",
		},
		{
			name: "without the module path box ticked", version: "1.30.2", module: "example.com/sdk", typed: majorVersion,
			text: "go.mod says module example.com/sdk. Go can't install v2.0.0 until it says example.com/sdk/v2.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newReleaseRig(t, rigOptions{base: shownVersion, module: tt.module})
			r.magex(tt.version, testutil.FakeRun{})

			req := StartRequest{Repo: sdkRepo, Bump: BumpMajor, Version: majorVersion, Typed: tt.typed}

			_, err := r.svc.Start(t.Context(), req)
			require.ErrorIs(t, err, ErrRejected)
			assert.Equal(t, tt.text, uierr.Message(err))
			assert.Contains(t, r.logs.String(), "this is a bug")
			assert.Zero(t, r.runCount())
		})
	}
}

func TestAConfirmedMajorStarts(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion, module: "example.com/sdk"})
	r.magex("1.30.2", r.replay("major-confirm"))
	r.after.set(r.tagged(majorVersion, r.second, true))

	o := r.prepare(PrepareRequest{Bump: BumpMajor, Typed: majorVersion, AllowModulePath: true})
	require.True(t, o.Ready, "%+v", o.Checks)

	req := requestFor(o)
	req.Typed, req.AllowModulePath = " v2.0.0", true

	id, err := r.svc.Start(t.Context(), req)
	require.NoError(t, err)

	run := r.ended(id)
	assert.Equal(t, []string{
		r.tool.Path, "version:bump", "bump=major", "branch=master", "push=true", "expect=v2.0.0", "json=true", "ff-only",
		"major-confirm",
	}, run.Argv)
	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, "v2.0.0 is on GitHub.", run.Summary)
}

func TestALocalOnlyTagBlocks(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	testutil.RunGit(t, r.clone, "tag", recVersion)

	o := r.prepare(PrepareRequest{})
	assert.False(t, o.Ready)

	check, found := checkOf(o, CheckLocalOnly)
	require.True(t, found)
	assert.Equal(t, CheckBlock, check.State)

	_, err := r.svc.Start(t.Context(), requestFor(o))
	require.ErrorIs(t, err, ErrNotReady)
	assert.Equal(t, check.Text+".", uierr.Message(err))
	assert.Zero(t, r.runCount())
}

func TestStartRefusesWhatItCantServe(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	ctx := t.Context()

	for _, bad := range []StartRequest{
		{Repo: sdkRepo, Bump: BumpPatch, Version: "1.4"},
		{Repo: sdkRepo, Version: recVersion},
		{Repo: "nope", Bump: BumpPatch, Version: recVersion},
	} {
		_, err := r.svc.Start(ctx, bad)
		require.ErrorIs(t, err, ErrInvalidRequest, "%+v", bad)
	}

	_, err := r.svc.Start(ctx, StartRequest{Repo: "acme-org/other", Bump: BumpPatch, Version: recVersion})
	require.ErrorIs(t, err, ErrNoClone)

	bare := NewService(Config{GitHub: r.github, Clones: &fakeClones{}})

	_, err = bare.Prepare(ctx, PrepareRequest{Repo: sdkRepo})
	require.ErrorIs(t, err, ErrNotSetUp)

	_, err = bare.Start(ctx, StartRequest{Repo: sdkRepo, Bump: BumpPatch, Version: recVersion})
	require.ErrorIs(t, err, ErrNotSetUp)

	r.svc.places = fakePlaces{err: errGitHubDown}

	_, err = r.svc.Prepare(ctx, PrepareRequest{Repo: sdkRepo})
	require.ErrorIs(t, err, errGitHubDown, "where runs go couldn't be read")
}

func TestReconcileVerifiesAnInterruptedRelease(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		sent    bool
		status  scriptrun.Status
		summary string
		kind    ResultKind
		told    int
	}{
		{
			name: "its tag reached GitHub", sent: true, status: scriptrun.StatusSucceeded,
			summary: "v1.4.3 is on GitHub.", kind: ResultReleased, told: 1,
		},
		{
			name: "its tag stayed on this Mac", status: scriptrun.StatusInterrupted,
			summary: "v1.4.3 is tagged on this Mac but isn't on GitHub.", kind: ResultNotPushed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newReleaseRig(t, rigOptions{base: shownVersion})
			testutil.RunGit(t, r.clone, "checkout", "-q", "-b", recFeature)

			// A release the app started before it crashed: magex tagged, and maybe pushed.
			require.NoError(t, r.tagged(recVersion, r.second, tt.sent)(t.Context()))

			id := r.insertRelease(Intent{
				Version: recVersion, Bump: BumpPatch, Branch: branchMaster, Original: recFeature, Head: r.second,
			}, loadRecording(t, "sigterm-after-tag").log("\n"))

			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan struct{})

			go func() {
				defer close(done)

				r.engine.Run(ctx)
			}()

			run := r.ended(id)

			cancel()
			<-done

			res := resultOf(t, run)
			assert.Equal(t, tt.status, run.Status)
			assert.Equal(t, tt.summary, run.Summary)
			assert.Equal(t, tt.kind, res.Kind)
			assert.Equal(t, recFeature, res.Branch, "it says where the clone is")
			assert.True(t, res.Back)
			assert.Equal(t, gitread.OperationNone, res.InProgress)
			assert.True(t, res.Tagged, "its log, read again, says magex made the tag")
			assert.Len(t, r.notices.all(), tt.told, "a release verified on the next start is told then")
		})
	}
}

// insertRelease records a release in the app as going, as the app did before it crashed,
// with its intent and the log it wrote, and returns its ID.
func (r *releaseRig) insertRelease(in Intent, log []byte) int64 {
	r.t.Helper()

	result, err := json.Marshal(scriptrun.Result{
		Tool: string(integrationprefs.MageX), Intent: in, Warnings: []string{}, Unrecognized: []string{},
	})
	require.NoError(r.t, err)

	got, err := r.runner.DB.ExecContext(r.t.Context(), `INSERT INTO script_runs (kind, title, repo_owner, repo_name, dir,
		argv, command, runs_in, status, result, started_at) VALUES ('release', 'Release v1.4.3', 'acme-org', 'sdk', ?,
		'["/opt/tools/magex"]', 'magex', 'app', 'running', ?, ?)`, r.clone, string(result), time.Now().UTC())
	require.NoError(r.t, err)

	id, err := got.LastInsertId()
	require.NoError(r.t, err)

	require.NoError(r.t, os.MkdirAll(r.runner.LogDir, 0o700))
	require.NoError(r.t, os.WriteFile(filepath.Join(r.runner.LogDir, fmt.Sprintf("%d.log", id)), log, 0o600))

	return id
}

// elsewhereTags is what someone else does: tag commit as tag in a clone of their own, and
// push it.
func (r *releaseRig) elsewhereTags(tag, commit string) func(context.Context) error {
	other := filepath.Join(r.root, "someone-else")
	testutil.RunGit(r.t, r.root, "clone", "-q", r.origin, other)

	return func(ctx context.Context) error {
		if err := gitIn(ctx, r.env, other, "tag", "-a", tag, "-m", tag, commit); err != nil {
			return err
		}

		return gitIn(ctx, r.env, other, "push", "-q", "origin", "refs/tags/"+tag)
	}
}

func TestATagOnGitHubAtAnotherCommitIsntARelease(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	r.magex("1.29.5", r.replay("remote-tag-other-commit"))

	theirs := r.elsewhereTags(recVersion, r.first)
	r.after.set(func(ctx context.Context) error {
		// Someone tagged v1.4.3 before magex's push; magex tagged its own here.
		if err := theirs(ctx); err != nil {
			return err
		}

		return r.tagged(recVersion, r.second, false)(ctx)
	})

	run := r.ended(r.release())
	res := resultOf(t, run)

	assert.Equal(t, scriptrun.StatusFailed, run.Status, "origin's v1.4.3 isn't this release's")
	assert.Equal(t, takenHeading, run.Summary)
	assert.Equal(t, ResultFailed, res.Kind)
	assert.True(t, res.Taken)
	assert.True(t, res.Tagged)
	assert.Empty(t, res.Commit)
	assert.Equal(t, []string{"git tag -d v1.4.3"}, res.Commands, "only undoing this release's own tag")
	require.NotNil(t, res.Failure)
	assert.Equal(t, FailureTaken, res.Failure.Kind)
	assert.Empty(t, res.TagURL)
}

func TestAReleaseStoppedBeforeItsTagIsntSomeoneElses(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	r.magex("1.30.2", testutil.FakeRun{Stdout: "21:59:47 [INFO] Using bump type: patch\n", Sleep: time.Minute})
	r.after.set(r.elsewhereTags(recVersion, r.second))

	id := r.release()
	r.waitFor(id, "Using bump type")
	require.NoError(t, r.runs.Stop(t.Context(), id))

	run := r.ended(id)
	res := resultOf(t, run)

	assert.Equal(t, scriptrun.StatusStopped, run.Status)
	assert.Equal(t, "Someone else released v1.4.3 meanwhile.", run.Summary)
	assert.Equal(t, ResultFailed, res.Kind, "the tag on its commit is someone else's: this run made none")
	assert.True(t, res.Taken)
	assert.False(t, res.Tagged)
	assert.Equal(t, []string{}, res.Commands)
}

func TestStartAsksGitHubOnlyWhenTheSheetsFactsAreOld(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	clk := testutil.NewClock(testNow())
	r.svc.clock = clk

	var underLock []bool

	r.github.asking = func() {
		_, held := r.locks.Holder(r.clone)
		underLock = append(underLock, held)
	}

	start := func(req StartRequest) {
		t.Helper()

		id, err := r.svc.Start(t.Context(), req)
		require.NoError(t, err)
		r.ended(id)
	}

	start(StartRequest{Repo: sdkRepo, Bump: BumpPatch, Version: recVersion})
	assert.Equal(t, []bool{true}, underLock, "with no sheet shown, it asks once, holding the clone")

	start(requestFor(r.prepare(PrepareRequest{})))
	assert.Equal(t, []bool{true, false}, underLock, "the sheet's facts serve a click at once")

	o := r.prepare(PrepareRequest{})

	clk.Advance(FactsFor)
	start(requestFor(o))
	assert.Equal(t, []bool{true, false, false, true}, underLock, "facts FactsFor old are asked for again")
}

func TestAReleaseCutOffAsTheAppQuitsIsVerifiedOnTheNextStart(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	rec := loadRecording(t, "patch-feature-behind")
	r.magex("1.30.2", testutil.FakeRun{Stdout: string(rec.log("\n")), Sleep: time.Minute})

	// origin doesn't answer before the app gives up on the release.
	r.after.set(func(ctx context.Context) error {
		<-ctx.Done()

		return nil
	})

	id := r.release()
	r.waitFor(id, "Tag pushed to remote")
	r.engine.StopAll()

	run, err := r.runs.Run(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, scriptrun.StatusRunning, run.Status, "it is left for the next start to verify")
	assert.Empty(t, r.notices.all(), "nothing is told of a release not verified")

	// magex had pushed the tag; the next start finds it.
	require.NoError(t, r.tagged(recVersion, r.second, true)(t.Context()))

	run = r.runner.Restart(id, map[scriptrun.Kind]scriptrun.Verifier{scriptrun.KindRelease: NewVerifier(r.svc)})

	assert.Equal(t, scriptrun.StatusSucceeded, run.Status)
	assert.Equal(t, "v1.4.3 is on GitHub.", run.Summary)
	assert.Equal(t, ResultReleased, resultOf(t, run).Kind)
	assert.Len(t, r.notices.all(), 1, "told once, by the start that verified it")
}
