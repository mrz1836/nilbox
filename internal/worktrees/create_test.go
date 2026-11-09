package worktrees

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/worklog"
)

// mainBranch is the default branch of the test repositories.
const mainBranch = "main"

// remoteFixture is a clone under the clone root whose origin is a bare repository that
// stands in for GitHub: it has main, and pull request 7's head under refs/pull/7/head,
// one commit past main.
type remoteFixture struct {
	clone  string
	bare   string
	prHead string
	main   string
}

// newRemoteFixture makes the bare remote and clones it under the env's clone root.
func newRemoteFixture(t *testing.T, e *env) remoteFixture {
	t.Helper()

	src := filepath.Join(e.home, "src")
	testutil.InitRepo(t, src)
	require.NoError(t, os.WriteFile(filepath.Join(src, "README.md"), []byte("example\n"), 0o600))
	testutil.RunGit(t, src, "add", "README.md")
	testutil.RunGit(t, src, "commit", "-q", "-m", "init")
	main := strings.TrimSpace(testutil.RunGit(t, src, "rev-parse", "HEAD"))

	testutil.RunGit(t, src, "switch", "-q", "-c", "contributor-fix")
	require.NoError(t, os.WriteFile(filepath.Join(src, "fix.txt"), []byte("fix\n"), 0o600))
	testutil.RunGit(t, src, "add", "fix.txt")
	testutil.RunGit(t, src, "commit", "-q", "-m", "fix")
	prHead := strings.TrimSpace(testutil.RunGit(t, src, "rev-parse", "HEAD"))
	testutil.RunGit(t, src, "switch", "-q", mainBranch)

	bare := filepath.Join(e.home, "remote.git")
	testutil.RunGit(t, e.home, "clone", "-q", "--bare", src, bare)
	testutil.RunGit(t, bare, "update-ref", "refs/pull/7/head", prHead)
	testutil.RunGit(t, bare, "update-ref", "-d", "refs/heads/contributor-fix")

	clone := filepath.Join(e.root, "example-repo")
	testutil.RunGit(t, e.home, "clone", "-q", bare, clone)

	return remoteFixture{clone: clone, bare: bare, prHead: prHead, main: main}
}

// request asks for kind #number's worktree in the fixture's clone.
func (f remoteFixture) request(kind item.Kind, number int) EnsureRequest {
	return EnsureRequest{Clone: f.clone, Owner: "acme-org", Name: "example-repo", Kind: kind, Number: number}
}

// maker returns a Maker over the env's real git, recording into records.
func (e *env) maker(records *fakeLaunches) (*Maker, *Service) {
	svc := e.linked(linkDeps{launches: records})

	return NewMaker(svc), svc
}

// fakeRunning is a fixed list of running processes.
type fakeRunning struct {
	list []procs.Process
	err  error
}

func (f fakeRunning) Processes(context.Context) ([]procs.Process, error) {
	return f.list, f.err
}

func TestEnsureCreatesAPullRequestsWorktreeAtItsHead(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	records := &fakeLaunches{}
	m, _ := e.maker(records)

	planned, err := m.Plan(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)
	assert.Equal(t, Ensured{
		Path: ManagedPath(f.clone, "pr-7"), Branch: "worktree-pr-7", Action: ActionCreate,
		Base: "the pull request's head",
	}, planned)
	assert.NoDirExists(t, ManagedRoot(f.clone), "a plan changes nothing")
	assert.NotContains(t, branches(t, f.clone), "worktree-pr-7")

	got, err := m.Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)

	want := filepath.Join(e.root, "."+brand.Slug, "worktrees", "example-repo", "pr-7")
	assert.Equal(t, resolvedPath(want), resolvedPath(got.Path), "beside the clone, under the clone root's hidden managed directory")
	assert.Equal(t, ActionCreate, got.Action)
	assert.Equal(t, "worktree-pr-7", got.Branch)
	assert.Equal(t, f.prHead, got.Head, "at the pull request's head")
	assert.FileExists(t, filepath.Join(want, "fix.txt"))
	assert.Contains(t, gitWorktreePaths(t, f.clone), resolvedPath(want))
	assert.Empty(t, strings.TrimSpace(testutil.RunGit(t, f.clone, "status", "--porcelain")),
		"the clone's own checkout is untouched")

	require.Len(t, records.all(), 1)
	r := records.all()[0]
	assert.Equal(t, worklog.Record{
		ClonePath: f.clone, Worktree: "pr-7", Path: got.Path, Branch: "worktree-pr-7", Owner: "acme-org",
		Name: "example-repo", Kind: item.KindPR, Number: 7, CreatedAt: listedAt(),
	}, r)

	again, err := m.Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)
	assert.Equal(t, ActionResume, again.Action, "a second launch resumes it")
	assert.Equal(t, got.Path, again.Path)
	assert.Zero(t, again.Dirty)

	dirty(t, want)

	resumed, err := m.Plan(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)
	assert.Equal(t, ActionResume, resumed.Action)
	assert.Equal(t, 2, resumed.Dirty, "a plan says what is uncommitted there")
}

func TestEnsureStartsAnIssuesWorktreeFromTheDefaultBranch(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	m, _ := e.maker(&fakeLaunches{})

	got, err := m.Ensure(t.Context(), f.request(item.KindIssue, 12))
	require.NoError(t, err)
	assert.Equal(t, ActionCreate, got.Action)
	assert.Equal(t, "origin/main", got.Base, "found from the remote's HEAD")
	assert.Equal(t, f.main, got.Head)
	assert.Equal(t, resolvedPath(ManagedPath(f.clone, "issue-12")), resolvedPath(got.Path))
	assert.Empty(t, got.Notice)

	upstream := testutil.RunGit(t, got.Path, "for-each-ref", "--format=%(upstream)", "refs/heads/worktree-issue-12")
	assert.Empty(t, strings.TrimSpace(upstream), "the new branch tracks nothing, so a push never lands on main")

	// GitHub's default branch wins when the launch knows it.
	req := f.request(item.KindIssue, 13)
	req.DefaultBranch = mainBranch
	got, err = m.Ensure(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, "origin/main", got.Base)
}

func TestEnsureStartsFromTheLastFetchWhenTheRemoteIsUnreachable(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	m, _ := e.maker(&fakeLaunches{})

	require.NoError(t, os.Rename(f.bare, f.bare+".offline"))

	req := f.request(item.KindIssue, 3)
	req.DefaultBranch = mainBranch

	got, err := m.Ensure(t.Context(), req)
	require.NoError(t, err, "an issue's worktree still starts offline")
	assert.Equal(t, f.main, got.Head)
	assert.Contains(t, got.Notice, "Couldn't fetch origin/main")

	_, err = m.Ensure(t.Context(), f.request(item.KindPR, 7))
	require.Error(t, err)
	assert.Equal(t, "Couldn't fetch pull request #7 from origin.", uierr.Message(err),
		"a pull request's head can only come from GitHub")
	require.ErrorContains(t, err, "fetch pull request #7 from origin: git:")
	assert.NoDirExists(t, ManagedPath(f.clone, "pr-7"))
}

func TestEnsureAdoptsALegacyClaudeWorktree(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	legacy, head := agentWorktree(t, f.clone, "pr-7")
	records := &fakeLaunches{}
	m, _ := e.maker(records)

	got, err := m.Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)
	assert.Equal(t, ActionAdopt, got.Action, "the work Claude Code started there is kept")
	assert.Equal(t, resolvedPath(legacy), resolvedPath(got.Path))
	assert.Equal(t, head, got.Head)
	assert.NoDirExists(t, ManagedPath(f.clone, "pr-7"))

	require.Len(t, records.all(), 1)
	assert.Equal(t, got.Path, records.all()[0].Path, "recorded where it is")

	again, err := m.Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)
	assert.Equal(t, ActionResume, again.Action, "once recorded, it is resumed")
}

func TestEnsureAttachesALeftBehindBranch(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	m, _ := e.maker(&fakeLaunches{})

	first, err := m.Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(first.Path, "more.txt"), []byte("more\n"), 0o600))
	testutil.RunGit(t, first.Path, "add", "more.txt")
	testutil.RunGit(t, first.Path, "commit", "-q", "-m", "more")
	work := strings.TrimSpace(testutil.RunGit(t, first.Path, "rev-parse", "HEAD"))

	// The worktree is removed outside the app; its branch stays.
	testutil.RunGit(t, f.clone, "worktree", "remove", first.Path)

	planned, err := m.Plan(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)
	assert.Equal(t, ActionAttach, planned.Action)

	got, err := m.Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)
	assert.Equal(t, ActionAttach, got.Action)
	assert.Equal(t, work, got.Head, "the commits on the branch are kept, never reset to the pull request's head")
}

func TestEnsureRecoversFromADirectoryDeletedOutOfBand(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	records := &fakeLaunches{}
	m, svc := e.maker(records)

	first, err := m.Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)

	require.NoError(t, os.RemoveAll(first.Path), "git still lists it, as prunable")

	got, err := m.Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)
	assert.Equal(t, ActionAttach, got.Action, "the stale entry is pruned and the branch attached again")
	assert.DirExists(t, got.Path)

	// A listing finds every record's worktree there.
	snap, err := svc.List(t.Context())
	require.NoError(t, err)
	row := mustRow(t, snap, got.Path)
	assert.True(t, row.Managed)
	assert.False(t, row.Legacy)
	assert.True(t, row.OwnBranch)
	require.Len(t, records.all(), 1)
}

func TestEnsureRefusesAPathTakenBySomethingElse(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	m, _ := e.maker(&fakeLaunches{})

	taken := ManagedPath(f.clone, "pr-7")
	require.NoError(t, os.MkdirAll(taken, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(taken, "notes.txt"), []byte("mine\n"), 0o600))

	_, err := m.Ensure(t.Context(), f.request(item.KindPR, 7))
	require.ErrorIs(t, err, ErrPathTaken)
	assert.Contains(t, uierr.Message(err), "is in the way of the worktree")
	assert.FileExists(t, filepath.Join(taken, "notes.txt"), "nothing is ever deleted")
}

func TestConcurrentEnsuresOnOneClone(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	records := &fakeLaunches{}
	m, _ := e.maker(records)

	var wg sync.WaitGroup

	results := make([]Ensured, 6)
	errs := make([]error, 6)

	for i := range results {
		wg.Go(func() {
			kind, number := item.KindPR, 7
			if i%2 == 1 {
				kind, number = item.KindIssue, 20+i
			}

			results[i], errs[i] = m.Ensure(t.Context(), f.request(kind, number))
		})
	}

	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, i)
	}

	for i := 0; i < len(results); i += 2 {
		assert.Equal(t, results[0].Path, results[i].Path, "every launch of one item gets its one worktree")
	}

	assert.Len(t, gitWorktreePaths(t, f.clone), 5, "the clone, pull request 7's worktree, and three issues'")
}

func TestEnsureRunsNoHookInThePullRequestsFiles(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	m, _ := e.maker(&fakeLaunches{})

	// A post-checkout hook in the clone runs with the new worktree, which holds the pull
	// request's files, as its working directory: one that runs a tool from the checkout
	// would run the pull request's code.
	marker := filepath.Join(e.home, "hook-ran")
	hook := filepath.Join(f.clone, ".git", "hooks", "post-checkout")
	require.NoError(t, os.WriteFile(hook, []byte("#!/bin/sh\npwd > '"+marker+"'\n"), 0o700)) //nolint:gosec // the hook must be executable

	_, err := m.Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)
	assert.NoFileExists(t, marker, "making the worktree runs no hook")

	_, err = m.Ensure(t.Context(), f.request(item.KindIssue, 9))
	require.NoError(t, err)
	assert.NoFileExists(t, marker, "nor does making an issue's")

	// The hook is real: git runs it for a worktree added by hand.
	testutil.RunGit(t, f.clone, "worktree", "add", "-q", "--detach", filepath.Join(e.home, "by-hand"), "HEAD")
	assert.FileExists(t, marker)
}

func TestEnsureRefusesAPullRequestHeadThatMovedAfterItsCheck(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	records := &fakeLaunches{}
	m, _ := e.maker(records)

	// The launch checked the files of another commit than the head the fetch finds now.
	req := f.request(item.KindPR, 7)
	req.Head = f.main

	_, err := m.Ensure(t.Context(), req)
	require.ErrorIs(t, err, ErrHeadMoved)
	assert.Equal(t, "Pull request #7 changed while it was opening, so its new commits weren't checked. "+
		"Open it again to check them.", uierr.Message(err))
	assert.NotContains(t, branches(t, f.clone), "worktree-pr-7", "the unchecked head is not left for a later launch")
	assert.NoDirExists(t, ManagedPath(f.clone, "pr-7"))
	assert.Empty(t, records.all())

	req.Head = f.prHead
	got, err := m.Ensure(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, ActionCreate, got.Action)
	assert.Equal(t, f.prHead, got.Head, "made at the head that was checked")
}

func TestEnsureChecksItsRequest(t *testing.T) {
	t.Parallel()

	m, _ := newEnv(t).maker(&fakeLaunches{})
	ok := EnsureRequest{Clone: "/srv/clone", Owner: "acme-org", Name: "example-repo", Kind: item.KindPR, Number: 1}

	for name, edit := range map[string]func(*EnsureRequest){
		"relative clone":       func(r *EnsureRequest) { r.Clone = "clone" },
		"no kind":              func(r *EnsureRequest) { r.Kind = "" },
		"no number":            func(r *EnsureRequest) { r.Number = 0 },
		"no repository":        func(r *EnsureRequest) { r.Owner = "" },
		"an option":            func(r *EnsureRequest) { r.Remote = "--upload-pack=x" },
		"a head no commit has": func(r *EnsureRequest) { r.Head = "main" },
		"an issue's head": func(r *EnsureRequest) {
			r.Kind, r.Head = item.KindIssue, "fc1c3975011e66491785ace891c893e4fc716587"
		},
	} {
		req := ok
		edit(&req)

		_, err := m.Ensure(t.Context(), req)
		require.ErrorIs(t, err, ErrInvalidRequest, name)

		_, err = m.Plan(t.Context(), req)
		require.ErrorIs(t, err, ErrInvalidRequest, name)
	}
}

func TestListReconcilesRecordsWithGit(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	records := &fakeLaunches{}
	m, svc := e.maker(records)

	pr, err := m.Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)

	issue, err := m.Ensure(t.Context(), f.request(item.KindIssue, 9))
	require.NoError(t, err)

	// One worktree moves; the other is removed outside the app.
	moved := filepath.Join(e.home, "elsewhere", "pr-7")
	require.NoError(t, os.MkdirAll(filepath.Dir(moved), 0o700))
	testutil.RunGit(t, f.clone, "worktree", "move", pr.Path, moved)
	testutil.RunGit(t, f.clone, "worktree", "remove", issue.Path)

	// A record of a clone that is gone is dropped too.
	records.records = append(records.records, launchOf(filepath.Join(e.root, "deleted-repo"), "pr-1", item.KindPR, 1))

	snap, err := svc.List(t.Context())
	require.NoError(t, err)

	all := records.all()
	require.Len(t, all, 1, "only the moved worktree keeps a record")
	assert.Equal(t, "pr-7", all[0].Worktree)
	assert.Equal(t, resolvedPath(moved), resolvedPath(all[0].Path), "the record follows the move")
	assert.ElementsMatch(t, []string{"example-repo/issue-9", "deleted-repo/pr-1"}, records.forgotten)

	row := mustRow(t, snap, moved)
	assert.True(t, row.Managed, "a recorded worktree is the app's wherever it is")
	assert.True(t, row.OwnBranch)
}

func TestListKeepsTheRecordOfAWorktreeMadeWhileItListed(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	records := &fakeLaunches{}
	m, _ := e.maker(records)

	got, err := m.Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)

	// The listing read the clone's worktrees just before a launch added pr-7's, so its
	// read has the clone alone, while the record the launch then wrote is there.
	var reads atomic.Int32

	run := scriptedGit(e.git, func(sub []string) (reply, bool) {
		if startsWith(sub, "worktree list") && reads.Add(1) == 1 {
			return reply{stdout: "worktree " + f.clone + "\x00HEAD " + f.main + "\x00branch refs/heads/main\x00\x00"}, true
		}

		return reply{}, false
	})
	svc := NewService(Config{
		Settings: fakeSettings{root: cloneRoot}, Git: run, Finder: execpath.Search{}, Home: e.home,
		Clock: testutil.NewClock(listedAt()), Logger: slog.New(slog.DiscardHandler), Records: records,
	})

	_, err = svc.List(t.Context())
	require.NoError(t, err)
	require.Greater(t, reads.Load(), int32(1), "git is asked again before a record goes")
	assert.Empty(t, records.forgotten, "the launch's record stays")
	require.Len(t, records.all(), 1)
	assert.Equal(t, got.Path, records.all()[0].Path)
}

func TestListReportsSessionsAndRemovalRefusesThem(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	records := &fakeLaunches{}

	got, err := NewMaker(e.linked(linkDeps{launches: records})).Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)

	running := fakeRunning{list: []procs.Process{
		{PID: 4000, PPID: 1, Command: "iTerm2", Dir: "/"},
		{PID: 4242, PPID: 4000, Command: "codex", Dir: filepath.Join(got.Path, "src")},
		{PID: 4343, PPID: 4000, Command: "zsh", Dir: f.clone},
		// Codex's app-server daemon outlives the session that started it, keeping its
		// folder as its working directory: no session.
		{PID: 15337, PPID: 1, Command: "codex", Dir: got.Path},
	}}
	svc := e.linked(linkDeps{launches: records, running: running})

	snap, err := svc.List(t.Context())
	require.NoError(t, err)
	assert.False(t, snap.SessionsUnknown)

	row := mustRow(t, snap, got.Path)
	assert.Equal(t, []Session{{PID: 4242, Command: "codex"}}, row.InUse, "a session counts, an agent's background service does not")
	assert.Equal(t, StageInUse, row.Stage, "a session works in it")
	assert.False(t, row.CanRemove, "a worktree in use is never removed")

	err = errOf(svc.Remove(t.Context(), f.clone, got.Path))
	require.ErrorIs(t, err, ErrInUse)
	require.ErrorContains(t, err, "codex (pid 4242)")
	assert.DirExists(t, got.Path)

	unknown := e.linked(linkDeps{launches: records, running: fakeRunning{err: procs.ErrUnsupported}})

	snap, err = unknown.List(t.Context())
	require.NoError(t, err)
	assert.True(t, snap.SessionsUnknown)
	assert.Empty(t, mustRow(t, snap, got.Path).InUse)
	assert.NotNil(t, mustRow(t, snap, got.Path).InUse)

	// A force removal discards changes, and only the process list could tell that no
	// session is making them, so without it none is offered or done.
	dirty(t, got.Path)

	snap, err = unknown.List(t.Context())
	require.NoError(t, err)
	assert.False(t, mustRow(t, snap, got.Path).CanForceRemove)

	err = errOf(unknown.ForceRemove(t.Context(), f.clone, got.Path))
	require.ErrorIs(t, err, ErrSessionsUnknown)
	assert.Contains(t, uierr.Message(err), "won't discard its changes")
	assert.DirExists(t, got.Path)

	require.Error(t, errOf(unknown.Remove(t.Context(), f.clone, got.Path)), "git refuses a plain removal of a dirty worktree")
	testutil.RunGit(t, got.Path, "checkout", "--", "README.md")
	require.NoError(t, os.Remove(filepath.Join(got.Path, "notes.txt")))

	require.NoError(t, errOf(unknown.Remove(t.Context(), f.clone, got.Path)),
		"a plain removal the user asks for goes ahead when sessions can't be listed: git and the locks still guard it")
}

// removeFixture is a clone with an open issue's worktree and an open pull request's,
// both made by the app, and a service that links them to the items through a GitHub that
// knows the pull request's head.
func removeFixture(t *testing.T) (f remoteFixture, svc *Service, issue, pr Ensured) {
	t.Helper()

	e := newEnv(t)
	f = newRemoteFixture(t, e)
	gh := &fakeGitHub{}
	gh.set("example-repo", 9, ItemState{Title: "Fix the thing", State: ItemOpen})
	gh.set("example-repo", 7, ItemState{PullRequest: true, Title: "Add the widget", State: ItemOpen, HeadOID: f.prHead})
	svc = e.linked(linkDeps{gh: gh, launches: &fakeLaunches{}})
	m := NewMaker(svc)

	issue, err := m.Ensure(t.Context(), f.request(item.KindIssue, 9))
	require.NoError(t, err)

	pr, err = m.Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)

	return f, svc, issue, pr
}

func TestRemoveTakesTheBranchWhenNothingOnItWouldBeLost(t *testing.T) {
	t.Parallel()

	f, svc, issue, pr := removeFixture(t)

	// An open issue's worktree with no commits of its own: its branch is the default
	// branch's commit, which GitHub has.
	res, err := svc.Remove(t.Context(), f.clone, issue.Path)
	require.NoError(t, err)
	assert.True(t, res.Removed)
	assert.Equal(t, "worktree-issue-9", res.DeletedBranch)
	assert.Empty(t, res.Reason)
	assert.Equal(t, "acme-org/example-repo#9", res.Item)
	assert.NoDirExists(t, issue.Path)
	assert.NotContains(t, branches(t, f.clone), "worktree-issue-9", "the next launch starts afresh")

	// An open pull request's worktree at its head, which GitHub has as the pull request's.
	res, err = svc.Remove(t.Context(), f.clone, pr.Path)
	require.NoError(t, err)
	assert.Equal(t, "worktree-pr-7", res.DeletedBranch)
	assert.NotContains(t, branches(t, f.clone), "worktree-pr-7")
}

func TestRemoveKeepsABranchWithCommitsNotOnGitHub(t *testing.T) {
	t.Parallel()

	f, svc, issue, _ := removeFixture(t)

	require.NoError(t, os.WriteFile(filepath.Join(issue.Path, "fix.go"), []byte("package fix\n"), 0o600))
	testutil.RunGit(t, issue.Path, "add", "fix.go")
	testutil.RunGit(t, issue.Path, "commit", "-q", "-m", "fix the thing")
	work := strings.TrimSpace(testutil.RunGit(t, issue.Path, "rev-parse", "HEAD"))

	res, err := svc.Remove(t.Context(), f.clone, issue.Path)
	require.NoError(t, err)
	assert.True(t, res.Removed)
	assert.Empty(t, res.DeletedBranch)
	assert.Equal(t, "kept the branch worktree-issue-9, which has 1 commit not on GitHub", res.Reason)
	assert.Contains(t, branches(t, f.clone), "worktree-issue-9")

	// The item's next launch picks the work up again.
	again, err := NewMaker(svc).Ensure(t.Context(), f.request(item.KindIssue, 9))
	require.NoError(t, err)
	assert.Equal(t, ActionAttach, again.Action)
	assert.Equal(t, work, again.Head)
}

func TestForceRemoveAndRemoveAllCleanTakeTheBranchToo(t *testing.T) {
	t.Parallel()

	f, svc, issue, pr := removeFixture(t)

	dirty(t, issue.Path)

	res, err := svc.ForceRemove(t.Context(), f.clone, issue.Path)
	require.NoError(t, err)
	assert.Equal(t, "worktree-issue-9", res.DeletedBranch, "the changes discarded, nothing on the branch is lost")

	results, err := svc.RemoveAllClean(t.Context())
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, pr.Path, results[0].Path)
	assert.True(t, results[0].Removed)
	assert.Equal(t, "worktree-pr-7", results[0].DeletedBranch)
	assert.Empty(t, branches(t, f.clone)[1:], "only the clone's own branch is left")
}

func TestRemoveKeepsThePullRequestsBranchWithoutGitHub(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	svc := e.linked(linkDeps{gh: &fakeGitHub{err: errFakeGitHub}, launches: &fakeLaunches{}})

	pr, err := NewMaker(svc).Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)

	// Its head is on GitHub only as the pull request's, which only GitHub can say.
	res, err := svc.Remove(t.Context(), f.clone, pr.Path)
	require.NoError(t, err)
	assert.True(t, res.Removed)
	assert.Equal(t, "kept the branch worktree-pr-7, which has 1 commit not on GitHub", res.Reason)
	assert.Contains(t, branches(t, f.clone), "worktree-pr-7")
}

func TestABackgroundServiceLeftInAWorktreeIsNoSession(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	records := &fakeLaunches{}

	got, err := NewMaker(e.linked(linkDeps{launches: records})).Ensure(t.Context(), f.request(item.KindIssue, 202))
	require.NoError(t, err)

	// The session ended; Codex's app-server daemon, which launchd now runs, and its
	// worker are still running from the worktree.
	daemon := []procs.Process{
		{PID: 15337, PPID: 1, Command: "codex", Dir: got.Path},
		{PID: 22072, PPID: 15337, Command: "codex", Dir: got.Path},
	}
	svc := e.linked(linkDeps{launches: records, running: fakeRunning{list: daemon}})

	snap, err := svc.List(t.Context())
	require.NoError(t, err)

	row := mustRow(t, snap, got.Path)
	assert.Empty(t, row.InUse)
	assert.True(t, row.CanRemove)

	// An agent an editor runs in the worktree, with the editor as its parent, is a
	// session, terminal or not.
	editor := append(slices.Clone(daemon),
		procs.Process{PID: 83937, PPID: 1, Command: "Code Helper (Plugin)", Dir: "/"},
		procs.Process{PID: 87508, PPID: 83937, Command: "codex", Dir: got.Path},
	)
	inEditor := e.linked(linkDeps{launches: records, running: fakeRunning{list: editor}})

	snap, err = inEditor.List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []Session{{PID: 87508, Command: "codex"}}, mustRow(t, snap, got.Path).InUse)
	require.ErrorIs(t, errOf(inEditor.Remove(t.Context(), f.clone, got.Path)), ErrInUse)

	require.NoError(t, errOf(svc.Remove(t.Context(), f.clone, got.Path)))
	assert.NoDirExists(t, got.Path)
}

func TestSweepLeavesWhatItCannotTellIsUnused(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newSweepFixture(t, e)
	svc := e.linked(linkDeps{gh: f.gh, launches: f.launches, auto: true, running: fakeRunning{err: procs.ErrUnsupported}})

	report := svc.Sweep(t.Context())
	assert.Empty(t, report.Cleaned, "without the running processes the sweep never guesses")
	assert.Empty(t, report.Failed)
	assert.Equal(t, 6, report.Leftover)

	busy := e.linked(linkDeps{gh: f.gh, launches: f.launches, auto: true, running: fakeRunning{list: []procs.Process{
		{PID: 99, PPID: 98, Command: "claude", Dir: f.launchedPR},
	}}})

	report = busy.Sweep(t.Context())

	cleaned := make([]string, 0, len(report.Cleaned))
	for _, r := range report.Cleaned {
		cleaned = append(cleaned, r.Item)
	}

	assert.NotContains(t, cleaned, "acme-org/example-repo#1", "a worktree a session works in stays")
	assert.Contains(t, gitWorktreePaths(t, f.repo), resolvedPath(f.launchedPR))
}

func TestRecordedRowsShowTheirSessions(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	r := launchOf(f.clone, "pr-7", item.KindPR, 7)
	r.Agent = agent.Codex
	r.Launches = 3
	_, _ = agentWorktree(t, f.clone, "pr-7")

	svc := e.linked(linkDeps{launches: &fakeLaunches{records: []worklog.Record{r}}})

	snap, err := svc.List(t.Context())
	require.NoError(t, err)

	row := mustRow(t, snap, filepath.Join(f.clone, ".claude", "worktrees", "pr-7"))
	assert.True(t, row.Managed)
	assert.True(t, row.Legacy, "Claude Code made it")
	assert.Equal(t, agent.Codex, row.Agent)
	assert.Equal(t, 3, row.Launches)
	assert.Equal(t, "2026-11-05T09:30:00Z", row.LastLaunchedAt)
}

func TestAllowedCreateCommands(t *testing.T) {
	t.Parallel()

	for args, want := range map[string]bool{
		"fetch --no-tags origin refs/pull/7/head:refs/heads/worktree-pr-7":                     true,
		"fetch --no-tags upstream refs/pull/7/head:refs/heads/worktree-pr-8":                   false,
		"fetch --no-tags origin refs/pull/07/head:refs/heads/worktree-pr-7":                    false,
		"fetch --no-tags origin +refs/pull/7/head:refs/heads/worktree-pr-7":                    false,
		"fetch --no-tags origin refs/pull/7/head:refs/heads/main":                              false,
		"fetch --no-tags origin +refs/heads/main:refs/remotes/origin/main":                     true,
		"fetch --no-tags origin +refs/heads/main:refs/remotes/upstream/main":                   false,
		"fetch --no-tags origin +refs/heads/main:refs/heads/main":                              false,
		"fetch --no-tags --upload-pack=x refs/pull/7/head:refs/heads/worktree-pr-7":            false,
		"fetch origin refs/pull/7/head:refs/heads/worktree-pr-7":                               false,
		"worktree add /srv/wt/pr-7 worktree-pr-7":                                              true,
		"worktree add /srv/wt/pr-7 main":                                                       false,
		"worktree add relative worktree-pr-7":                                                  false,
		"worktree add --force /srv/wt/pr-7":                                                    false,
		"worktree add --no-track -b worktree-issue-3 /srv/wt/issue-3 refs/remotes/origin/main": true,
		"worktree add --no-track -b main /srv/wt/issue-3 refs/remotes/origin/main":             false,
		"worktree add --no-track -b worktree-issue-3 /srv/wt/issue-3 refs/heads/main":          false,
		"worktree add --no-track -b worktree-issue-3 /srv/wt/issue-3 refs/remotes/origin/../x": false,
		"rev-parse --verify --quiet refs/heads/worktree-pr-7":                                  true,
		"rev-parse --verify --quiet refs/remotes/origin/main":                                  true,
		"rev-parse --verify --quiet refs/heads/main":                                           false,
		"symbolic-ref --quiet refs/remotes/origin/HEAD":                                        true,
		"symbolic-ref --quiet refs/remotes/-x/HEAD":                                            false,
		"symbolic-ref HEAD refs/heads/main":                                                    false,
		"ls-remote --symref origin HEAD":                                                       true,
		"ls-remote --symref --upload-pack=x HEAD":                                              false,
	} {
		assert.Equal(t, want, allowed(strings.Fields(args)), args)
	}
}

func TestValidBranch(t *testing.T) {
	t.Parallel()

	for _, ok := range []string{mainBranch, "release/2026", "dev-1.x", "trunk_2"} {
		assert.True(t, validBranch(ok), ok)
	}

	for _, bad := range []string{
		"", "-x", "a..b", "a b", "a~1", "a^", "a:b", "a?", "a*", "a[", `a\b`, "a/", "a.", "a//b", ".hidden",
		"a/.b", "x.lock", "a@{1}", "@", "tab\there", strings.Repeat("a", 256),
	} {
		assert.False(t, validBranch(bad), bad)
	}
}

func TestParseSymref(t *testing.T) {
	t.Parallel()

	got, ok := parseSymref("ref: refs/heads/trunk\tHEAD\n0123456789abcdef0123456789abcdef01234567\tHEAD\n")
	assert.True(t, ok)
	assert.Equal(t, "trunk", got)

	for _, out := range []string{"", "0123\tHEAD\n", "ref: refs/heads/-x\tHEAD\n", "ref: refs/tags/v1\tHEAD\n"} {
		_, ok = parseSymref(out)
		assert.False(t, ok, out)
	}
}

func TestManagedRoot(t *testing.T) {
	t.Parallel()

	assert.Equal(t, filepath.FromSlash("/srv/projects/."+brand.Slug+"/worktrees/example-repo"), ManagedRoot("/srv/projects/example-repo/"))
	assert.Equal(t, filepath.FromSlash("/srv/projects/."+brand.Slug+"/worktrees/example-repo/issue-4"),
		ManagedPath("/srv/projects/example-repo", "issue-4"))
}
