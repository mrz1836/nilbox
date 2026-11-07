package worktrees

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchlog"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// sweepFixture is a clone with one agent worktree of each kind the sweep tells apart.
type sweepFixture struct {
	repo     string
	gh       *fakeGitHub
	launches *fakeLaunches

	launchedPR, commitLinked, nameOnly, unlaunchedIssue, launchedIssue, changed, open string
}

func newSweepFixture(t *testing.T, e *env) *sweepFixture {
	t.Helper()

	f := &sweepFixture{repo: e.newGitHubRepo(t, "example-repo"), gh: &fakeGitHub{}, launches: &fakeLaunches{}}

	var head string

	// Launched by the app, and its pull request merged: cleaned up.
	f.launchedPR, head = agentWorktree(t, f.repo, "pr-1")
	pushed(t, f.repo, "one", head)
	f.gh.set("example-repo", 1, pr(github.StateMerged, "a different head"))
	f.launches.records = append(f.launches.records, launchOf(f.repo, "pr-1", item.KindPR, 1))

	// Not launched by the app, but HEAD is the merged pull request's head: cleaned up.
	f.commitLinked, head = agentWorktree(t, f.repo, "pr-2")
	f.gh.set("example-repo", 2, pr(github.StateMerged, head))

	// Not launched, and linked by its name alone: finished, but left for the user.
	f.nameOnly, head = agentWorktree(t, f.repo, "pr-3")
	pushed(t, f.repo, "three", head)
	f.gh.set("example-repo", 3, pr(github.StateMerged, "a different head"))

	// An issue not launched by the app: left for the user.
	f.unlaunchedIssue, head = agentWorktree(t, f.repo, "issue-4")
	pushed(t, f.repo, "four", head)
	f.gh.set("example-repo", 4, closedIssue())

	// An issue the app launched, now closed: cleaned up.
	f.launchedIssue, head = agentWorktree(t, f.repo, "issue-5")
	pushed(t, f.repo, "five", head)
	f.gh.set("example-repo", 5, closedIssue())
	f.launches.records = append(f.launches.records, launchOf(f.repo, "issue-5", item.KindIssue, 5))

	// Merged, but it holds uncommitted changes: needs a look.
	f.changed, head = agentWorktree(t, f.repo, "pr-6")
	pushed(t, f.repo, "six", head)
	dirty(t, f.changed)
	f.gh.set("example-repo", 6, pr(github.StateMerged, head))
	f.launches.records = append(f.launches.records, launchOf(f.repo, "pr-6", item.KindPR, 6))

	// Still open: nothing to do.
	f.open, head = agentWorktree(t, f.repo, "pr-7")
	pushed(t, f.repo, "seven", head)
	f.gh.set("example-repo", 7, pr(github.StateOpen, head))

	return f
}

func TestSweepCleansUpMergedAndClosedWork(t *testing.T) {
	e := newEnv(t)
	f := newSweepFixture(t, e)
	notifier := &testutil.Notifier{}
	svc := e.linked(linkDeps{gh: f.gh, launches: f.launches, notifier: notifier, auto: true})

	assert.Zero(t, svc.LastSweep().Seq, "no sweep yet")

	report := svc.Sweep(t.Context())

	assert.Equal(t, 1, report.Seq)
	assert.True(t, report.Enabled)
	assert.Empty(t, report.Error)
	assert.Equal(t, listedAt().Format(time.RFC3339), report.At)
	assert.Empty(t, report.Failed)
	assert.Equal(t, 3, report.Leftover, "pr-3 and issue-4 are finished but left for the user, and pr-6 needs a look")
	assert.Equal(t, 4, report.WorktreeCount, "the four agent worktrees the sweep kept")

	cleaned := make([]string, 0, len(report.Cleaned))
	for _, r := range report.Cleaned {
		cleaned = append(cleaned, r.Item)
		assert.True(t, r.Removed)
	}

	assert.ElementsMatch(t, []string{"acme-org/example-repo#1", "acme-org/example-repo#2", "acme-org/example-repo#5"}, cleaned)

	paths := gitWorktreePaths(t, f.repo)
	for _, gone := range []string{f.launchedPR, f.commitLinked, f.launchedIssue} {
		assert.NotContains(t, paths, resolvedPath(gone))
	}

	for _, kept := range []string{f.nameOnly, f.unlaunchedIssue, f.changed, f.open} {
		assert.Contains(t, paths, resolvedPath(kept))
	}

	assert.NotContains(t, branches(t, f.repo), "worktree-pr-1")
	assert.ElementsMatch(t, []string{"example-repo/pr-1", "example-repo/issue-5"}, f.launches.forgotten)
	assert.Equal(t, 1, notifier.Calls())
	assert.Equal(t, report, svc.LastSweep())

	again := svc.Sweep(t.Context())
	assert.Equal(t, 2, again.Seq)
	assert.Empty(t, again.Cleaned)
	assert.Equal(t, 3, again.Leftover)
	assert.Equal(t, 1, notifier.Calls(), "nothing changed, so nobody is told")
}

func TestSweepOnlyCountsWhenAutomaticCleanupIsOff(t *testing.T) {
	e := newEnv(t)
	f := newSweepFixture(t, e)
	svc := e.linked(linkDeps{gh: f.gh, launches: f.launches})

	report := svc.Sweep(t.Context())
	assert.False(t, report.Enabled)
	assert.Empty(t, report.Cleaned)
	assert.Equal(t, 6, report.Leftover, "every finished worktree, and pr-6")
	assert.Equal(t, 7, report.WorktreeCount)
	assert.Len(t, gitWorktreePaths(t, f.repo), 8, "the main worktree and all seven agent worktrees stay")
}

func TestSweepCountsTheWorktreesOfEveryClone(t *testing.T) {
	e := newEnv(t)
	plain := e.newRepo(t, "plain")
	feature := addWorktree(t, plain, filepath.Join(e.home, "wt", "feature"), "feature")
	notifier := &testutil.Notifier{}
	svc := e.linked(linkDeps{gh: &fakeGitHub{}, notifier: notifier, auto: true})

	report := svc.Sweep(t.Context())
	assert.Empty(t, report.Error)
	assert.Equal(t, 1, report.WorktreeCount, "a clone without agent worktrees still counts, though git never runs in it")
	assert.Equal(t, 1, notifier.Calls(), "the first count is news")

	svc.Sweep(t.Context())
	assert.Equal(t, 1, notifier.Calls(), "the same count is not")

	addWorktree(t, plain, filepath.Join(e.home, "wt", "second"), "second")
	assert.Equal(t, 2, svc.Sweep(t.Context()).WorktreeCount)
	assert.Equal(t, 2, notifier.Calls(), "a new worktree is news")

	testutil.RunGit(t, plain, "worktree", "remove", feature)
	assert.Equal(t, 1, svc.Sweep(t.Context()).WorktreeCount)
	assert.Equal(t, 3, notifier.Calls(), "so is one removed")
}

func TestCountWorktrees(t *testing.T) {
	e := newEnv(t)
	wt := filepath.Join(e.home, "wt")

	// Two linked worktrees outside the root, one of them gone from disk but still
	// registered, as git lists it until pruned.
	alpha := e.newRepo(t, "alpha")
	addWorktree(t, alpha, filepath.Join(wt, "alpha-one"), "one")
	require.NoError(t, os.RemoveAll(addWorktree(t, alpha, filepath.Join(wt, "alpha-gone"), "gone")))

	// A linked worktree under the root counts with its clone, once, and so does the clone
	// reached through a symlink.
	addWorktree(t, alpha, filepath.Join(e.root, "alpha-wt"), "alpha-wt")
	require.NoError(t, os.Symlink(alpha, filepath.Join(e.root, "alpha-link")))

	// A registration without a gitdir file, which git does not list.
	require.NoError(t, os.MkdirAll(filepath.Join(alpha, ".git", "worktrees", "half-made"), 0o700))

	// A linked worktree under the root of a repository outside it brings that
	// repository's worktrees, as a listing shows them.
	outside := filepath.Join(e.home, "outside")
	testutil.InitRepo(t, outside)
	testutil.RunGit(t, outside, "commit", "-q", "--allow-empty", "-m", "init")
	addWorktree(t, outside, filepath.Join(e.root, "outside-wt"), "outside-wt")
	addWorktree(t, outside, filepath.Join(wt, "outside-two"), "outside-two")

	// Not counted: a clone without linked worktrees, a hidden clone, a directory that is
	// not a clone, and a .git file that points nowhere.
	e.newRepo(t, "beta")
	addWorktree(t, e.newRepo(t, ".hidden"), filepath.Join(wt, "hidden-wt"), "hidden")
	require.NoError(t, os.MkdirAll(filepath.Join(e.root, "notes"), 0o700))

	broken := filepath.Join(e.root, "broken")
	require.NoError(t, os.MkdirAll(broken, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(broken, ".git"), []byte("not a pointer\n"), 0o600))

	assert.Equal(t, 5, countWorktrees(e.root), "alpha's three and outside's two")
	assert.Equal(t, e.list(t).WorktreeCount, countWorktrees(e.root), "as a listing counts them")
	assert.Zero(t, countWorktrees(filepath.Join(e.root, "missing")))
}

func TestSweepReportsWhatItCouldNotRemove(t *testing.T) {
	e := newEnv(t)
	repo := e.newGitHubRepo(t, "example-repo")
	path, head := agentWorktree(t, repo, "pr-1")
	gh := &fakeGitHub{}
	gh.set("example-repo", 1, pr(github.StateMerged, head))

	// git reports the worktree's commit as of the listing; a commit made by the time it
	// is removed keeps it.
	svc := e.linked(linkDeps{gh: gh, auto: true})
	svc.run = scriptedGit(func(sub []string) (reply, bool) {
		if startsWith(sub, "worktree remove") {
			return reply{stderr: "fatal: '" + path + "' contains modified or untracked files, use --force to delete it", err: exitError{code: 128}}, true
		}

		return reply{}, false
	})

	report := svc.Sweep(t.Context())
	require.Len(t, report.Failed, 1)
	assert.Equal(t, reasonDirty, report.Failed[0].Reason)
	assert.Equal(t, 1, report.Leftover, "a worktree the sweep could not remove still needs the user")
}

func TestSweepForgetsLaunchesWhoseWorktreeIsGone(t *testing.T) {
	e := newEnv(t)
	repo := e.newGitHubRepo(t, "example-repo")
	kept, head := agentWorktree(t, repo, "pr-1")
	pushed(t, repo, "one", head)
	removed, _ := agentWorktree(t, repo, "pr-2")
	testutil.RunGit(t, repo, "worktree", "remove", removed)

	elsewhere := filepath.Join(e.home, "elsewhere", "example-repo")
	require.NoError(t, os.MkdirAll(elsewhere, 0o700))

	launches := &fakeLaunches{records: []launchlog.Launch{
		launchOf(repo, "pr-1", item.KindPR, 1),
		launchOf(repo, "pr-2", item.KindPR, 2),
		launchOf(filepath.Join(e.root, "deleted-clone"), "pr-3", item.KindPR, 3),
		launchOf(elsewhere, "pr-4", item.KindPR, 4),
	}}

	gh := &fakeGitHub{}
	gh.set("example-repo", 1, pr(github.StateOpen, head))

	e.linked(linkDeps{gh: gh, launches: launches, auto: true}).Sweep(t.Context())

	assert.ElementsMatch(t, []string{"example-repo/pr-2", "deleted-clone/pr-3"}, launches.forgotten,
		"a live worktree and a clone outside the root keep their records")
	assert.Contains(t, gitWorktreePaths(t, repo), resolvedPath(kept))
}

func TestSweepForgetsLaunchesWhenNoCloneIsLeft(t *testing.T) {
	e := newEnv(t)
	launches := &fakeLaunches{records: []launchlog.Launch{launchOf(filepath.Join(e.root, "gone"), "pr-1", item.KindPR, 1)}}

	report := e.linked(linkDeps{gh: &fakeGitHub{}, launches: launches, auto: true}).Sweep(t.Context())
	assert.Empty(t, report.Error)
	assert.Zero(t, report.Leftover)
	assert.Equal(t, []string{"gone/pr-1"}, launches.forgotten)

	launches = &fakeLaunches{records: []launchlog.Launch{launchOf(filepath.Join(e.root, "gone"), "pr-1", item.KindPR, 1)}, forgetErr: errFakeLaunches}
	e.linked(linkDeps{gh: &fakeGitHub{}, launches: launches}).Sweep(t.Context())
	assert.Empty(t, launches.forgotten, "a failure to forget is only logged")
}

func TestSweepErrors(t *testing.T) {
	e := newEnv(t)
	repo := e.newGitHubRepo(t, "example-repo")
	agentWorktree(t, repo, "pr-1")

	notifier := &testutil.Notifier{}
	gh := &fakeGitHub{err: errFakeGitHub}
	svc := e.linked(linkDeps{gh: gh, notifier: notifier, auto: true})

	report := svc.Sweep(t.Context())
	assert.Contains(t, report.Error, errFakeGitHub.Error())
	assert.Empty(t, report.Cleaned)
	assert.Equal(t, 1, notifier.Calls(), "a new error is news")

	svc.settings = fakeSettings{err: errFakeSettings}

	report = svc.Sweep(t.Context())
	assert.Equal(t, errFakeSettings.Error(), report.Error)
	assert.NotEmpty(t, report.At)

	svc.settings = fakeSettings{root: cloneRoot, auto: true}
	svc.run = scriptedGit(func(sub []string) (reply, bool) {
		if startsWith(sub, "worktree list") {
			return reply{err: errFakeStart}, true
		}

		return reply{}, false
	})

	report = svc.Sweep(t.Context())
	assert.Contains(t, report.Error, errFakeStart.Error())
}

func TestSweepClones(t *testing.T) {
	e := newEnv(t)
	agent := e.newGitHubRepo(t, "agent")
	agentWorktree(t, agent, "pr-1")

	empty := e.newRepo(t, "empty")
	require.NoError(t, os.MkdirAll(filepath.Join(empty, ".claude", "worktrees"), 0o700))

	plain := e.newRepo(t, "plain")
	addWorktree(t, plain, filepath.Join(e.home, "wt", "plain"), "feature")

	recorded := e.newRepo(t, "recorded")

	hidden := filepath.Join(e.root, ".hidden")
	require.NoError(t, os.MkdirAll(filepath.Join(hidden, ".claude", "worktrees", "pr-1"), 0o700))

	notRepo := filepath.Join(e.root, "not-a-repo")
	require.NoError(t, os.MkdirAll(filepath.Join(notRepo, ".claude", "worktrees", "pr-1"), 0o700))

	require.NoError(t, os.WriteFile(filepath.Join(e.root, "file.txt"), []byte("x"), 0o600))

	launches := []launchlog.Launch{
		launchOf(recorded, "pr-9", item.KindPR, 9),
		launchOf(agent, "pr-1", item.KindPR, 1),
		launchOf(filepath.Join(e.home, "outside"), "pr-2", item.KindPR, 2),
	}

	svc := e.linked(linkDeps{})
	root, err := filepath.EvalSymlinks(e.root)
	require.NoError(t, err)

	clones := svc.sweepClones(t.Context(), e.root, launches)

	names := make([]string, 0, len(clones))
	for _, c := range clones {
		names = append(names, filepath.Base(c.Path))
	}

	assert.Equal(t, []string{"agent", "recorded"}, names,
		"only clones with agent worktrees or records, each once; not plain, empty, hidden, or not a repository")
	assert.Equal(t, map[string]repo.Name{"origin": {Owner: "acme-org", Repo: "agent"}}, clones[0].Remotes)
	assert.Empty(t, svc.sweepClones(t.Context(), filepath.Join(root, "missing"), nil))
}

func TestSweeperRunsUntilCanceled(t *testing.T) {
	e := newEnv(t)
	svc := e.linked(linkDeps{gh: &fakeGitHub{}})

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		NewSweeper(svc).Run(ctx, time.Millisecond)
	}()

	require.Eventually(t, func() bool { return svc.LastSweep().Seq >= 2 }, 5*time.Second, time.Millisecond,
		"it sweeps at once and then on every tick")

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the sweeper did not return once canceled")
	}
}

// panickingSettings panics when read.
type panickingSettings struct{}

func (panickingSettings) Get(context.Context) (settings.Settings, error) { panic("settings exploded") }

func TestSweeperSurvivesAPanicAndLogsFailures(t *testing.T) {
	var logs bytes.Buffer

	var mu sync.Mutex

	logger := slog.New(slog.NewTextHandler(lockedWriter{w: &logs, mu: &mu}, nil))
	svc := NewService(Config{Settings: panickingSettings{}, Git: execpath.Exec{}, Logger: logger})

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		NewSweeper(svc).Run(ctx, time.Millisecond)
	}()

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()

		return strings.Count(logs.String(), "worktree sweep panicked") >= 2
	}, 5*time.Second, time.Millisecond, "a panicking sweep is logged and the next one still runs")

	cancel()
	<-done

	svc = NewService(Config{Settings: fakeSettings{err: errFakeSettings}, Git: execpath.Exec{}, Logger: logger})
	require.EqualError(t, svc.sweepOnce(t.Context()), errFakeSettings.Error())
	assert.Equal(t, errFakeSettings.Error(), svc.LastSweep().Error)
}

// lockedWriter serializes writes to w.
type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.w.Write(p)
}
