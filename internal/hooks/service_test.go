package hooks

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// everyState builds a clone in every state S3 recorded, and one shared hooks folder used
// by a clone with a config and one without, and answers each clone's status as the real
// go-pre-commit did. It returns the clones' folders by name.
func everyState(r *rig) map[string]string {
	shared := filepath.Join(r.home, ".githooks")
	husky9 := ".husky/_"

	dirs := map[string]string{
		"api":     r.clone("api", cloneOpts{config: coreEnv, origin: "acme-org/api"}),
		"cli":     r.clone("cli", cloneOpts{config: coreEnv, hook: oldHook, origin: "acme-org/cli"}),
		"web":     r.clone("web", cloneOpts{config: coreEnv, hook: currentHook, origin: "acme-org/web"}),
		"sdk":     r.clone("sdk", cloneOpts{config: coreEnv, hook: huskyHook, origin: "acme-org/sdk"}),
		"mode":    r.clone("mode", cloneOpts{config: coreEnv, hook: currentHook, mode: 0o644}),
		"off":     r.clone("off", cloneOpts{config: coreEnv + "ENABLE_GO_PRE_COMMIT=false\n", hook: currentHook}),
		"msg":     r.clone("msg", cloneOpts{legacy: coreEnv, hook: currentHook, hooks: map[string]string{hookCommitMsg: currentHook}}),
		"ops":     r.clone("ops", cloneOpts{config: coreEnv, hooksPath: shared, origin: "acme-org/ops"}),
		"husky9":  r.clone("husky9", cloneOpts{config: coreEnv, hooksPath: husky9, hook: "#!/usr/bin/env sh\n. \"$(dirname \"$0\")/h\"\n"}),
		"plain":   r.clone("plain", cloneOpts{}),
		"plain-2": r.clone("plain-2", cloneOpts{hooksPath: shared}),
	}

	return dirs
}

func TestReposShowsEveryState(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dirs := everyState(r)
	p := r.page(false)

	assert.Equal(t, ToolStatus{Version: "1.13.1", Path: p.Tool.Path, State: integrations.StateFound, Usable: true}, p.Tool)
	assert.Equal(t, "~/projects", p.Root)
	assert.Empty(t, p.Error)
	assert.NotEmpty(t, p.CheckedAt)
	assert.Equal(t, []Clone{{Name: "plain", Dir: "~/projects/plain"}, {Name: "plain-2", Dir: "~/projects/plain-2"}}, p.NotSetUp)

	type want struct {
		group     Group
		state     State
		action    Action
		otherTool string
	}

	for name, w := range map[string]want{
		"api":    {GroupAttention, StateMissing, ActionInstall, ""},
		"cli":    {GroupAttention, StateOutdated, ActionUpdate, ""},
		"web":    {GroupReady, StateReady, "", ""},
		"sdk":    {GroupAttention, StateOther, ActionReplace, toolHusky},
		"mode":   {GroupAttention, StateNotExecutable, ActionReinstall, ""},
		"off":    {GroupOff, StateReady, "", ""},
		"msg":    {GroupReady, StateReady, "", ""},
		"ops":    {GroupAttention, StateMissing, ActionInstall, ""},
		"husky9": {GroupAttention, StateOther, ActionReplace, toolHusky},
	} {
		got := row(t, p, dirs[name])
		assert.Equal(t, want{got.Group, got.State, got.Action, got.OtherTool}, w, name)
		assert.Empty(t, got.Error, name)
		assert.Equal(t, "1.14.0", got.CIVersion, name)
	}

	web := row(t, p, dirs["web"])
	assert.Equal(t, "acme-org/web", web.Repo)
	assert.Equal(t, "acme-org/web", web.Name)
	assert.Equal(t, "~/projects/web", web.Dir)
	assert.Equal(t, "~/projects/web/.git/hooks", web.HooksDir)
	assert.False(t, web.Shared)
	assert.Len(t, web.Checks, len(Checks()))

	mode := row(t, p, dirs["mode"])
	assert.Empty(t, mode.Repo, "a clone with no GitHub origin is named by its folder")
	assert.Equal(t, "mode", mode.Name)

	assert.Equal(t, []string{hookCommitMsg}, row(t, p, dirs["msg"]).AlsoInstalled)

	ops := row(t, p, dirs["ops"])
	assert.True(t, ops.Shared)
	assert.Equal(t, 2, ops.SharedWith, "a clone without a config shares the folder too")
	assert.Equal(t, "~/.githooks", ops.HooksDir)

	husky9 := row(t, p, dirs["husky9"])
	assert.False(t, husky9.Shared, "a hooks folder inside the work tree is the clone's own")
	assert.Equal(t, "~/projects/husky9/.husky/_", husky9.HooksDir)

	groups := make([]Group, 0, len(p.Repos))
	for _, rr := range p.Repos {
		groups = append(groups, rr.Group)
	}

	assert.True(t, slices.IsSortedFunc(groups, func(a, b Group) int { return groupRank(a) - groupRank(b) }), "%v", groups)
	assert.Positive(t, r.events.Count())
}

func TestReposAsksStatusOnceForTheSameBytes(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("web", cloneOpts{config: coreEnv, hook: currentHook})
	app := r.clone("app", cloneOpts{config: coreEnv, hook: currentHook})
	cli := r.clone("cli", cloneOpts{config: coreEnv, hook: oldHook})

	first := r.page(false)
	assert.Equal(t, StateReady, row(t, first, app).State)
	assert.Equal(t, StateOutdated, row(t, first, cli).State)

	asked := len(r.status.runs())
	assert.Positive(t, asked)
	assert.LessOrEqual(t, asked, 3, "at most once per bytes, and the clones are scanned at once")

	again := r.page(true)
	assert.Equal(t, first.Repos, again.Repos)
	assert.Len(t, r.status.runs(), asked, "a refresh judges bytes already judged without asking again")

	call := r.status.runs()[0]
	assert.Equal(t, []string{"go-pre-commit", "status", noColor}, append([]string{filepath.Base(call.argv[0])}, call.argv[1:]...))
}

func TestStatusJudgesTheHookInAScratchRepositoryNeverInTheClone(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	cli := r.clone("cli", cloneOpts{config: coreEnv + "PATH=/srv/evil/bin\n", hook: oldHook})

	assert.Equal(t, StateOutdated, row(t, r.page(false), cli).State)

	calls := r.status.runs()
	require.Len(t, calls, 1)
	assert.Equal(t, oldHook, calls[0].hook, "the scratch repository holds the clone's hook")
	assert.Equal(t, r.scratch, filepath.Dir(calls[0].dir), "status ran in the scratch folder, not in the clone")
	assert.True(t, strings.HasPrefix(filepath.Base(calls[0].dir), scratchPrefix))
	assert.NoDirExists(t, calls[0].dir, "the scratch repository is removed once status ends")

	entries, err := os.ReadDir(r.scratch)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestAScratchRepositoryIsOneGitKnows(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	// A global core.hooksPath, which would send status to another folder.
	global := filepath.Join(r.home, "global.gitconfig")
	writeFile(t, r.home, "global.gitconfig", "[core]\n\thooksPath = /srv/elsewhere\n", 0o600)

	env := slices.DeleteFunc(slices.Clone(r.env), func(kv string) bool { return strings.HasPrefix(kv, "GIT_CONFIG_GLOBAL=") })
	git := gitread.New(gitread.Config{Env: func() []string { return append(env, "GIT_CONFIG_GLOBAL="+global) }})

	dir, err := r.svc.scratchRepo([]byte(currentHook))
	require.NoError(t, err)

	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	hp, err := git.HookPaths(t.Context(), dir)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, ".git", "hooks"), hp.Hooks, "its own hooksPath wins over the global one")
	assert.Equal(t, currentHook, readHookText(t, dir))
	assert.True(t, readConfig(dir).present, "go-pre-commit's status needs a config")

	// A scratch folder that can't be made says so.
	writeFile(t, r.home, "blocked", "a file", 0o600)
	r.svc.scratch = filepath.Join(r.home, "blocked", "scratch")

	_, err = r.svc.scratchRepo([]byte(currentHook))
	require.Error(t, err)
}

func TestReposReadsStatusAsJSONWhenTheCopyCan(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	cli := r.clone("cli", cloneOpts{config: coreEnv, hook: oldHook})
	web := r.clone("web", cloneOpts{config: coreEnv, hook: currentHook})
	r.tool("1.14.1", testutil.FakeRun{})
	r.status.answerJSON()

	p := r.page(false)
	assert.Equal(t, StateOutdated, row(t, p, cli).State)
	assert.Equal(t, StateReady, row(t, p, web).State)
	assert.Equal(t, "--json", r.status.runs()[0].argv[3])
}

func TestReposWhenStatusCantJudgeTheHook(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("web", cloneOpts{config: coreEnv, hook: currentHook})

	for _, name := range []string{"status-no-config", "status-other", "status-not-installed"} {
		r.status.sayInstead(name)

		got := row(t, r.page(true), dir)
		assert.Equal(t, StateUnknown, got.State, name)
		assert.Equal(t, GroupReady, got.Group, "%s: nothing to do here that the page could offer", name)
	}

	assert.Contains(t, r.logs.String(), "go-pre-commit status failed")
	assert.Contains(t, r.logs.String(), "go-pre-commit status said nothing of the hook it was given")
}

func TestReposWithoutAUsableGoPreCommit(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	web := r.clone("web", cloneOpts{config: coreEnv, hook: currentHook})
	api := r.clone("api", cloneOpts{config: coreEnv})
	r.tools.set(ToolFacts{State: integrations.StateMissing, Off: true}, "")

	p := r.page(false)

	assert.False(t, p.Tool.Usable)
	assert.Equal(t, StateUnknown, row(t, p, web).State)
	assert.Equal(t, StateMissing, row(t, p, api).State)
	assert.Empty(t, r.status.runs())
	assert.Equal(t, ToolNote{}, p.ToolNote)
	assert.Equal(t, PathWarning{}, p.PathWarning)
}

func TestReposWithAClonesHooksFolderGitCantRead(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("api", cloneOpts{config: coreEnv})
	r.svc.git = failingGit{Git: r.git, dir: dir}

	got := row(t, r.page(false), dir)
	assert.Equal(t, StateUnreadable, got.State)
	assert.Equal(t, GroupAttention, got.Group)
	assert.Contains(t, got.Error, "Couldn't read where git runs this clone's hooks")
	assert.Empty(t, got.Action)
}

// failingGit is gitread failing for one clone.
type failingGit struct {
	*gitread.Git

	dir string
}

func (f failingGit) HookPaths(ctx context.Context, dir string) (gitread.HookPaths, error) {
	if dir == f.dir {
		return gitread.HookPaths{}, errFake
	}

	return f.Git.HookPaths(ctx, dir)
}

func (f failingGit) Entries(ctx context.Context, dir string) ([]gitread.Entry, error) {
	if dir == f.dir {
		return nil, errFake
	}

	return f.Git.Entries(ctx, dir)
}

func TestReposServesTheLastScanWhileItsFresh(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	clk := testutil.NewClock(time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC))
	r.svc.clock = clk
	dir := r.clone("api", cloneOpts{config: coreEnv})

	r.page(false)
	r.page(false)
	assert.Equal(t, 1, r.clones.listings(), "a fresh scan is served")

	r.page(true)
	assert.Equal(t, 2, r.clones.listings(), "refresh scans again")

	clk.Advance(FreshFor)
	r.page(false)
	assert.Equal(t, 3, r.clones.listings(), "an old scan is scanned again")

	r.tool("1.14.0", testutil.FakeRun{})
	r.page(false)
	assert.Equal(t, 4, r.clones.listings(), "another copy of go-pre-commit judges again")

	// A failed scan keeps the last page, with why.
	r.clones.setDirsErr(errFake)

	p := r.page(true)
	assert.Equal(t, "Couldn't list the clones in ~/projects.", p.Error)
	assert.Equal(t, StateMissing, row(t, p, dir).State)

	r.clones.setDirsErr(nil)
	assert.Empty(t, r.page(true).Error)
}

func TestReposFailsWithoutAScanToShow(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clones.setDirsErr(errFake)

	_, err := r.svc.Repos(t.Context(), false)
	require.ErrorIs(t, err, errFake)

	r.clones.mu.Lock()
	r.clones.rootErr = errFake
	r.clones.mu.Unlock()

	_, err = r.svc.Repos(t.Context(), false)
	require.ErrorIs(t, err, errFake)
}

func TestReposScanCutShortKeepsNothing(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("api", cloneOpts{config: coreEnv})

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := r.svc.Repos(ctx, false)
	require.ErrorIs(t, err, context.Canceled)

	r.svc.mu.Lock()
	has := r.svc.has
	r.svc.mu.Unlock()
	assert.False(t, has)
}

func TestReposScansAgainOnlyTheClonesThatChanged(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	api := r.clone("api", cloneOpts{config: coreEnv})
	web := r.clone("web", cloneOpts{config: coreEnv})

	assert.Equal(t, StateMissing, row(t, r.page(false), api).State)

	writeFile(t, filepath.Join(api, ".git", "hooks"), hookPreCommit, huskyHook, 0o755)
	writeFile(t, filepath.Join(web, ".git", "hooks"), hookPreCommit, huskyHook, 0o755)
	r.svc.markStale(api)

	p := r.page(false)
	assert.Equal(t, StateOther, row(t, p, api).State, "a clone marked stale is scanned again")
	assert.Equal(t, StateMissing, row(t, p, web).State, "the others are served from the last scan")
	assert.Equal(t, 1, r.clones.listings())
}

func TestReposReadsWhatHoldsEachCloneAndItsLastChecks(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("api", cloneOpts{config: coreEnv, origin: "acme-org/api"})

	release, _, ok := r.locks.TryLock(dir, clonelock.Holder{Kind: clonelock.KindRelease, Repo: "acme-org/api"})
	require.True(t, ok)

	assert.Equal(t, "A release is using acme-org/api right now.", row(t, r.page(false), dir).Busy)

	release()
	assert.Empty(t, row(t, r.page(false), dir).Busy)
	assert.Zero(t, row(t, r.page(false), dir).LastChecks.ID)
}

func TestReposToolNoteAndPathWarning(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("api", cloneOpts{config: coreEnv})

	p := r.page(false)
	assert.Equal(t, ToolNote{Show: true, Open: true}, p.ToolNote, "1.13.1 installs the checks' tools unchecked")
	assert.Equal(t, PathWarning{}, p.PathWarning, "the fake copy is on the PATH")

	require.NoError(t, r.memory.Set(t.Context(), checksRanKey, "true"))
	assert.Equal(t, ToolNote{Show: true}, r.page(false).ToolNote, "it folds once a checks run ended")

	r.tool("1.14.0", testutil.FakeRun{})
	assert.Equal(t, ToolNote{}, r.page(false).ToolNote, "1.14.0 verifies them")

	facts := r.tools.HooksTool(t.Context())
	facts.OnPath = false
	r.tools.set(facts, r.tools.envEntry())

	assert.Equal(t, PathWarning{Show: true, Dir: filepath.Dir(facts.Path)}, r.page(false).PathWarning)
}

func TestReposEmitsHooksUpdated(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.page(false)

	assert.Equal(t, []event.Event{event.HooksUpdated{}}, r.events.All())
}

func TestDisplayAndOrder(t *testing.T) {
	t.Parallel()

	api, err := repo.Parse("acme-org/api")
	require.NoError(t, err)

	assert.Equal(t, "example-repo", display(repo.Name{}, "/srv/projects/example-repo"))
	assert.Equal(t, "acme-org/api", display(api, "/srv/projects/api"))
	assert.Negative(t, compareNames("API", "api"), "case decides only between names otherwise equal")
	assert.Negative(t, compareNames("api", "Web"))
	assert.Empty(t, stamp(time.Time{}))
	assert.True(t, strings.HasPrefix(stamp(time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC)), "2026-11-10"))
}

func TestAGlobalHooksPathIsSharedByEveryClone(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	shared := filepath.Join(r.home, ".githooks")
	writeFile(t, r.home, "global.gitconfig", "[core]\n\thooksPath = "+shared+"\n", 0o600)
	r.env = append(slices.DeleteFunc(r.env, func(kv string) bool { return strings.HasPrefix(kv, "GIT_CONFIG_GLOBAL=") }),
		"GIT_CONFIG_GLOBAL="+filepath.Join(r.home, "global.gitconfig"))

	api := r.clone("api", cloneOpts{config: coreEnv})
	web := r.clone("web", cloneOpts{config: coreEnv})
	r.clone("notes", cloneOpts{})
	writeFile(t, shared, hookPreCommit, currentHook, 0o755)

	p := r.page(false)

	for _, dir := range []string{api, web} {
		got := row(t, p, dir)
		assert.True(t, got.Shared)
		assert.Equal(t, 3, got.SharedWith, "a clone without a config uses it too")
		assert.Equal(t, StateReady, got.State, "the global folder's hook is every clone's")
		assert.Equal(t, GroupReady, got.Group)
		assert.Equal(t, "~/.githooks", got.HooksDir)
	}
}

func TestAStaleRescanCutShortKeepsNothing(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("api", cloneOpts{config: coreEnv})
	assert.Equal(t, StateMissing, row(t, r.page(false), dir).State)

	writeFile(t, filepath.Join(dir, ".git", "hooks"), hookPreCommit, huskyHook, 0o755)
	r.svc.markStale(dir)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	require.ErrorIs(t, r.svc.scanStale(ctx, r.tools.HooksTool(t.Context())), context.Canceled)

	r.svc.mu.Lock()
	_, marked := r.svc.stale[paths.Resolved(dir)]
	kept := r.svc.repos[0].row.State
	r.svc.mu.Unlock()

	assert.True(t, marked, "the clone stays marked to scan again")
	assert.Equal(t, StateMissing, kept, "nothing a canceled rescan read is kept")
	assert.Equal(t, StateOther, row(t, r.page(false), dir).State, "the next read scans it again")
}

func TestAMarkSetDuringAScanStays(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	api := r.clone("api", cloneOpts{config: coreEnv})
	web := r.clone("web", cloneOpts{config: coreEnv})
	r.page(false)

	// One mark was there when the scan began; the other comes while it runs, after the
	// clones were listed, so the scan may have read the clone before the change.
	r.svc.markStale(api)
	r.clones.hook(func() { r.svc.markStale(web) }, nil)
	r.page(true)
	r.clones.hook(nil, nil)

	r.svc.mu.Lock()
	_, apiMarked := r.svc.stale[paths.Resolved(api)]
	_, webMarked := r.svc.stale[paths.Resolved(web)]
	r.svc.mu.Unlock()

	assert.False(t, apiMarked, "the scan read the clone marked before it began")
	assert.True(t, webMarked, "a mark set while it ran is kept for the next read")

	r.page(false)

	r.svc.mu.Lock()
	assert.Empty(t, r.svc.stale)
	r.svc.mu.Unlock()
}

func TestAStaleRescanReadsTheConfigAfresh(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("api", cloneOpts{config: coreEnv})
	assert.Equal(t, GroupAttention, row(t, r.page(false), dir).Group)

	writeFile(t, dir, ".github/env/99-local.env", "ENABLE_GO_PRE_COMMIT=false\n", 0o600)
	r.svc.markStale(dir)

	assert.Equal(t, GroupOff, row(t, r.page(false), dir).Group)
}

func TestConcurrentReadsRescanAStaleCloneOnce(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("api", cloneOpts{config: coreEnv})
	r.page(false)
	r.svc.markStale(dir)

	_, before := r.clones.counts()
	entered, gate := make(chan struct{}), make(chan struct{})

	var once sync.Once

	r.clones.hook(nil, func() {
		once.Do(func() { close(entered) })
		<-gate
	})

	pages := make(chan Page, 2)
	read := func() {
		p, err := r.svc.Repos(t.Context(), false)
		assert.NoError(t, err)

		pages <- p
	}

	go read()

	<-entered

	rootedBefore, _ := r.clones.counts()

	go read()

	// The second read has asked for the clone root, so it is on its way to the rescan the
	// first one runs.
	require.Eventually(t, func() bool { rooted, _ := r.clones.counts(); return rooted > rootedBefore }, runWait, runPoll)
	close(gate)

	for range 2 {
		assert.Equal(t, StateMissing, row(t, <-pages, dir).State)
	}

	_, after := r.clones.counts()
	assert.Equal(t, 1, after-before, "the stale clone's origin is read once")
}

// TestReposScanOutlivesTheCallerThatStartedIt checks that a window that stops waiting, as
// it does when it asks again, leaves the scan it started to finish and be kept, rather than
// throwing it away for the next caller to start over.
func TestReposScanOutlivesTheCallerThatStartedIt(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("api", cloneOpts{config: coreEnv})

	ctx, cancel := context.WithCancel(t.Context())

	r.clones.mu.Lock()
	r.clones.onDirs = cancel // the caller goes while the clones are listed
	r.clones.mu.Unlock()

	_, err := r.svc.Repos(ctx, false)
	require.ErrorIs(t, err, context.Canceled)

	r.clones.mu.Lock()
	r.clones.onDirs = nil
	r.clones.mu.Unlock()

	page, err := r.svc.Repos(t.Context(), false)
	require.NoError(t, err)
	require.Len(t, page.Repos, 1)

	r.clones.mu.Lock()
	listed := r.clones.listed
	r.clones.mu.Unlock()
	assert.Equal(t, 1, listed, "the first scan went on and was kept, so the clones were listed once")
}
