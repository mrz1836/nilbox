package worktrees

import (
	"fmt"
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
	"github.com/mrz1836/nilbox/internal/branchrules"
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/worklog"
)

// pr7Branch is the own branch of pull request 7's worktree.
const pr7Branch = "worktree-pr-7"

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

// remoteCommits are the commits a remote fixture's build makes: main's head, and pull
// request 7's.
type remoteCommits struct {
	main   string
	prHead string
}

// remoteRepos are the repositories newRemoteFixture starts from, built once for the test
// binary by buildRemote and copied into each env that asks for them.
var remoteRepos = testutil.NewGitTemplate(buildRemote) //nolint:gochecknoglobals // built once for the whole test binary

// buildRemote builds, in the folder an env's home takes the place of, src, whose main
// holds README.md and whose contributor-fix is one commit past it; remote.git, its bare
// clone, which holds contributor-fix's head only as refs/pull/7/head; and the clone of
// remote.git under the clone root, projects/example-repo.
func buildRemote(b *testutil.GitBuild) remoteCommits {
	src := filepath.Join(b.Root, "src")
	b.Git(".", "-c", "init.defaultBranch="+mainBranch, "init", "-q", src)
	main := b.Commit("src", "README.md", "example\n")

	b.Git("src", "switch", "-q", "-c", "contributor-fix")
	prHead := b.Commit("src", "fix.txt", "fix\n")
	b.Git("src", "switch", "-q", mainBranch)

	bare := filepath.Join(b.Root, "remote.git")
	b.Git(".", "clone", "-q", "--bare", src, bare)
	b.Git("remote.git", "update-ref", "refs/pull/7/head", prHead)
	b.Git("remote.git", "update-ref", "-d", "refs/heads/contributor-fix")

	b.Git(".", "clone", "-q", bare, filepath.Join(b.Root, "projects", "example-repo"))

	return remoteCommits{main: main, prHead: prHead}
}

// newRemoteFixture copies the bare remote, and its clone under the env's clone root,
// into the env's home.
func newRemoteFixture(t *testing.T, e *env) remoteFixture {
	t.Helper()

	return e.remoteFixture(remoteRepos.Copy(t, e.home))
}

// remoteFixture is the remote fixture a copy of buildRemote's repositories into the env's
// home holds, whose commits are c.
func (e *env) remoteFixture(c remoteCommits) remoteFixture {
	return remoteFixture{
		clone: filepath.Join(e.root, "example-repo"), bare: filepath.Join(e.home, "remote.git"),
		prHead: c.prHead, main: c.main,
	}
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

func TestEnsureCreatesAPullRequestsWorktreeAtItsHead(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	records := &fakeLaunches{}
	m, _ := e.maker(records)

	planned, err := m.Plan(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)
	assert.Equal(t, Ensured{
		Path: ManagedPath(f.clone, "pr-7"), Branch: pr7Branch, Action: ActionCreate,
		Base: "the pull request's head",
	}, planned)
	assert.NoDirExists(t, ManagedRoot(f.clone), "a plan changes nothing")
	assert.NotContains(t, branches(t, f.clone), pr7Branch)

	got, err := m.Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)

	want := filepath.Join(e.root, "."+brand.Slug, "worktrees", "example-repo", "pr-7")
	assert.Equal(t, resolvedPath(want), resolvedPath(got.Path), "beside the clone, under the clone root's hidden managed directory")
	assert.Equal(t, ActionCreate, got.Action)
	assert.Equal(t, pr7Branch, got.Branch)
	assert.Equal(t, f.prHead, got.Head, "at the pull request's head")
	assert.FileExists(t, filepath.Join(want, "fix.txt"))
	assert.Contains(t, gitWorktreePaths(t, f.clone), resolvedPath(want))
	assert.Empty(t, strings.TrimSpace(testutil.RunGit(t, f.clone, "status", "--porcelain")),
		"the clone's own checkout is untouched")

	require.Len(t, records.all(), 1)
	r := records.all()[0]
	assert.Equal(t, worklog.Record{
		ClonePath: f.clone, Worktree: "pr-7", Path: got.Path, Branch: pr7Branch, Owner: "acme-org",
		Name: "example-repo", Kind: item.KindPR, Number: 7, CreatedAt: listedAt(), FetchedHead: f.prHead,
	}, r, "with the head it fetched from GitHub")

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
	records := &fakeLaunches{}
	m, _ := e.maker(records)

	got, err := m.Ensure(t.Context(), f.request(item.KindIssue, 12))
	require.NoError(t, err)
	assert.Equal(t, ActionCreate, got.Action)
	assert.Equal(t, "origin/main", got.Base, "found from the remote's HEAD")
	assert.Equal(t, f.main, got.Head)
	assert.Equal(t, resolvedPath(ManagedPath(f.clone, "issue-12")), resolvedPath(got.Path))
	assert.Empty(t, got.Notice)
	require.Len(t, records.all(), 1)
	assert.Equal(t, f.main, records.all()[0].FetchedHead, "recorded with the commit it started from")

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

// advanceMain adds a commit of file to the remote's main, as a merge on GitHub does, and
// returns it.
func (f remoteFixture) advanceMain(t *testing.T, e *env, file string) string {
	t.Helper()

	src := filepath.Join(e.home, "src")
	head := testutil.Commit(t, src, file, file+"\n")
	testutil.RunGit(t, src, "push", "-q", f.bare, mainBranch)

	return head
}

// refAt is the commit ref points at in dir.
func refAt(t *testing.T, dir, ref string) string {
	t.Helper()

	return strings.TrimSpace(testutil.RunGit(t, dir, "rev-parse", ref))
}

func TestEnsureFetchesAPullRequestsBase(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	m, _ := e.maker(&fakeLaunches{})

	req := f.request(item.KindPR, 7)
	req.Base = mainBranch

	merged := f.advanceMain(t, e, "merged.txt")

	planned, err := m.Plan(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, ActionCreate, planned.Action)
	assert.Equal(t, f.main, refAt(t, f.clone, "refs/remotes/origin/main"), "a plan asks no remote")

	got, err := m.Ensure(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, ActionCreate, got.Action)
	assert.Empty(t, got.Notice)
	assert.Equal(t, f.prHead, got.Head, "the worktree stays at the pull request's head")
	assert.Equal(t, merged, refAt(t, f.clone, "refs/remotes/origin/main"), "with its base as GitHub has it")
	assert.Equal(t, merged, refAt(t, got.Path, "origin/main"), "which the worktree shares")

	again := f.advanceMain(t, e, "again.txt")

	got, err = m.Ensure(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, ActionResume, got.Action)
	assert.Empty(t, got.Notice)
	assert.Equal(t, again, refAt(t, f.clone, "refs/remotes/origin/main"), "a resumed worktree fetches it again")

	assert.Equal(t, f.main, refAt(t, f.clone, "refs/heads/main"), "the clone's own branch is left alone")
	assert.Empty(t, strings.TrimSpace(testutil.RunGit(t, f.clone, "status", "--porcelain")),
		"and so is its checkout")
	assert.Empty(t, strings.TrimSpace(testutil.RunGit(t, got.Path, "status", "--porcelain")),
		"and the worktree's")
}

func TestEnsureSaysWhenItCouldNotFetchTheBase(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	m, _ := e.maker(&fakeLaunches{})

	req := f.request(item.KindPR, 7)
	req.Base = "release"

	got, err := m.Ensure(t.Context(), req)
	require.NoError(t, err, "a base that can't be fetched fails no launch")
	assert.Equal(t, ActionCreate, got.Action)
	assert.Equal(t, f.prHead, got.Head)
	assert.Contains(t, got.Notice, "Couldn't fetch origin/release, the branch the pull request merges into:")

	require.NoError(t, os.Rename(f.bare, f.bare+".offline"))

	req.Base = mainBranch

	got, err = m.Ensure(t.Context(), req)
	require.NoError(t, err)
	assert.Equal(t, ActionResume, got.Action)
	assert.Contains(t, got.Notice, "Couldn't fetch origin/main, so the worktree has the last fetched copy:")
}

func TestEnsureFetchesNoBaseItShouldNot(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	m, _ := e.maker(&fakeLaunches{})

	_, err := m.Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)

	// Offline, a fetch would say so in the notice.
	require.NoError(t, os.Rename(f.bare, f.bare+".offline"))

	for _, base := range []string{"", "-main", "main..x", "refs/heads/main:refs/heads/x", "main lock"} {
		req := f.request(item.KindPR, 7)
		req.Base = base

		got, ensureErr := m.Ensure(t.Context(), req)
		require.NoError(t, ensureErr, "%q", base)
		assert.Empty(t, got.Notice, "%q is no branch to fetch", base)
	}

	// An issue's worktree starts from its default branch, and has no base of its own.
	issue := f.request(item.KindIssue, 3)
	issue.DefaultBranch, issue.Base = mainBranch, "release"

	got, err := m.Ensure(t.Context(), issue)
	require.NoError(t, err)
	assert.NotContains(t, got.Notice, "release")
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
	assert.Empty(t, records.all()[0].FetchedHead, "its commits may be anyone's, so none counts as fetched")

	again, err := m.Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)
	assert.Equal(t, ActionResume, again.Action, "once recorded, it is resumed")
}

func TestEnsureAttachesALeftBehindBranch(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	records := &fakeLaunches{}
	m, _ := e.maker(records)

	first, err := m.Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)

	work := testutil.Commit(t, first.Path, "more.txt", "more\n")

	// The worktree is removed outside the app; its branch stays.
	testutil.RunGit(t, f.clone, "worktree", "remove", first.Path)

	planned, err := m.Plan(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)
	assert.Equal(t, ActionAttach, planned.Action)

	got, err := m.Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)
	assert.Equal(t, ActionAttach, got.Action)
	assert.Equal(t, work, got.Head, "the commits on the branch are kept, never reset to the pull request's head")
	require.Len(t, records.all(), 1)
	assert.Equal(t, f.prHead, records.all()[0].FetchedHead,
		"the head it was made at stays, never the branch's tip, which holds work of the user's own")
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
	assert.NotContains(t, branches(t, f.clone), pr7Branch, "the unchecked head is not left for a later launch")
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
		"a remote with ..":     func(r *EnsureRequest) { r.Remote = "up..stream" },
		"a remote too long":    func(r *EnsureRequest) { r.Remote = strings.Repeat("a", 256) },
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

// TestEnsureRefusesARemoteGitreadWouldRefuse holds a request's remote to gitread's rule
// for a remote's name, so a name gitread's guards would refuse later, one with ".." or
// longer than 255 bytes, is refused before anything runs, in the request's own words.
func TestEnsureRefusesARemoteGitreadWouldRefuse(t *testing.T) {
	t.Parallel()

	m, _ := newEnv(t).maker(&fakeLaunches{})
	ok := EnsureRequest{Clone: "/srv/clone", Owner: "acme-org", Name: "example-repo", Kind: item.KindPR, Number: 1}

	for _, remote := range []string{"up..stream", "..", strings.Repeat("a", 256)} {
		req := ok
		req.Remote = remote

		_, err := m.Ensure(t.Context(), req)
		require.EqualError(t, err, fmt.Sprintf("%v: the remote %q", ErrInvalidRequest, remote))
	}

	req := ok
	req.Remote = strings.Repeat("a", 255)

	_, err := m.Plan(t.Context(), req)
	require.NotErrorIs(t, err, ErrInvalidRequest, "a name of 255 bytes is a remote's name")
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

	running := &testutil.Lister[procs.Process]{List: []procs.Process{
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

	err = testutil.ErrOf(svc.Remove(t.Context(), f.clone, got.Path, nil))
	require.ErrorIs(t, err, ErrInUse)
	require.ErrorContains(t, err, "codex (pid 4242)")
	assert.DirExists(t, got.Path)

	unknown := e.linked(linkDeps{launches: records, running: &testutil.Lister[procs.Process]{Err: procs.ErrUnsupported}})

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

	err = testutil.ErrOf(unknown.ForceRemove(t.Context(), f.clone, got.Path, nil))
	require.ErrorIs(t, err, ErrSessionsUnknown)
	assert.Contains(t, uierr.Message(err), "won't discard its changes")
	assert.DirExists(t, got.Path)

	require.Error(t, testutil.ErrOf(unknown.Remove(t.Context(), f.clone, got.Path, nil)), "git refuses a plain removal of a dirty worktree")
	testutil.RunGit(t, got.Path, "checkout", "--", "README.md")
	require.NoError(t, os.Remove(filepath.Join(got.Path, "notes.txt")))

	require.NoError(t, testutil.ErrOf(unknown.Remove(t.Context(), f.clone, got.Path, nil)),
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

func TestRemoveOffersAndTakesTheBranchesPicked(t *testing.T) {
	t.Parallel()

	f, svc, issue, pr := removeFixture(t)

	snap, err := svc.List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []Branch{{Name: "worktree-issue-9", Head: f.main, Kind: BranchOwn, Deletable: true}},
		mustRow(t, snap, issue.Path).Branches, "an open issue's branch with no commits of its own may go")
	assert.Equal(t, []Branch{{Name: pr7Branch, Head: f.prHead, Kind: BranchOwn, Deletable: true}},
		mustRow(t, snap, pr.Path).Branches, "an open pull request's branch at its head, which GitHub keeps, may go")

	res, err := svc.Remove(t.Context(), f.clone, issue.Path, []string{"worktree-issue-9"})
	require.NoError(t, err)
	assert.True(t, res.Removed)
	assert.Equal(t, []string{"worktree-issue-9"}, res.DeletedBranches)
	assert.Empty(t, res.Reason)
	assert.Equal(t, "acme-org/example-repo#9", res.Item)
	assert.NoDirExists(t, issue.Path)
	assert.NotContains(t, branches(t, f.clone), "worktree-issue-9", "the next launch starts afresh")

	// A branch the user leaves unticked stays.
	res, err = svc.Remove(t.Context(), f.clone, pr.Path, nil)
	require.NoError(t, err)
	assert.True(t, res.Removed)
	assert.Empty(t, res.DeletedBranches)
	assert.Empty(t, res.Reason)
	assert.Contains(t, branches(t, f.clone), pr7Branch)
}

func TestRemoveKeepsABranchWithCommitsNotOnGitHub(t *testing.T) {
	t.Parallel()

	f, svc, issue, _ := removeFixture(t)

	work := testutil.Commit(t, issue.Path, "fix.go", "package fix\n")

	snap, err := svc.List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []Branch{{Name: "worktree-issue-9", Head: work, Kind: BranchOwn, Reason: "1 commit on it isn't on GitHub"}},
		mustRow(t, snap, issue.Path).Branches)

	// Even picked, it stays.
	res, err := svc.Remove(t.Context(), f.clone, issue.Path, []string{"worktree-issue-9"})
	require.NoError(t, err)
	assert.True(t, res.Removed)
	assert.Empty(t, res.DeletedBranches)
	assert.Equal(t, "kept worktree-issue-9: 1 commit on it isn't on GitHub", res.Reason)
	assert.Contains(t, branches(t, f.clone), "worktree-issue-9")

	// The item's next launch picks the work up again.
	again, err := NewMaker(svc).Ensure(t.Context(), f.request(item.KindIssue, 9))
	require.NoError(t, err)
	assert.Equal(t, ActionAttach, again.Action)
	assert.Equal(t, work, again.Head)
}

func TestRemoveTakesThePullRequestsOwnBranchToo(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	gh := &fakeGitHub{}
	gh.set("example-repo", 7, ItemState{
		PullRequest: true, Title: "Add the widget", State: ItemMerged, ClosedAt: mergedAt(), HeadOID: f.prHead,
		HeadRef: "contributor-fix",
	})
	svc := e.linked(linkDeps{gh: gh, launches: &fakeLaunches{}})

	pr, err := NewMaker(svc).Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)

	// The session ran gh pr checkout, which checks out the pull request's own branch.
	testutil.RunGit(t, pr.Path, "switch", "-q", "-c", "contributor-fix")

	snap, err := svc.List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []Branch{
		{Name: pr7Branch, Head: f.prHead, Kind: BranchOwn, Deletable: true},
		{Name: "contributor-fix", Head: f.prHead, Kind: BranchPullRequest, Deletable: true},
	}, mustRow(t, snap, pr.Path).Branches, "the worktree's own branch, left beside it, and the pull request's")

	res, err := svc.CleanUp(t.Context(), f.clone, pr.Path, []string{pr7Branch, "contributor-fix"})
	require.NoError(t, err)
	assert.Equal(t, []string{pr7Branch, "contributor-fix"}, res.DeletedBranches)
	assert.NotContains(t, branches(t, f.clone), pr7Branch)
	assert.NotContains(t, branches(t, f.clone), "contributor-fix")
}

func TestRemoveNeverTakesAProtectedBranch(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	rules := branchrules.Rules{Own: true, PullRequest: true, Protected: []string{"worktree-issue-*"}}
	svc := e.linked(linkDeps{rules: &rules, launches: &fakeLaunches{}})

	issue, err := NewMaker(svc).Ensure(t.Context(), f.request(item.KindIssue, 9))
	require.NoError(t, err)

	snap, err := svc.List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []Branch{{Name: "worktree-issue-9", Head: f.main, Kind: BranchOwn, Reason: "it is protected in Settings"}},
		mustRow(t, snap, issue.Path).Branches)

	res, err := svc.Remove(t.Context(), f.clone, issue.Path, []string{"worktree-issue-9"})
	require.NoError(t, err)
	assert.Empty(t, res.DeletedBranches)
	assert.Contains(t, branches(t, f.clone), "worktree-issue-9")
}

func TestForceRemoveAndRemoveAllCleanTakeThePickedBranches(t *testing.T) {
	t.Parallel()

	f, svc, issue, pr := removeFixture(t)

	dirty(t, issue.Path)

	res, err := svc.ForceRemove(t.Context(), f.clone, issue.Path, []string{"worktree-issue-9"})
	require.NoError(t, err)
	assert.Equal(t, []string{"worktree-issue-9"}, res.DeletedBranches, "the changes discarded, nothing on the branch is lost")

	results, err := svc.RemoveAllClean(t.Context(), cleanPicks(t, svc))
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, resolvedPath(pr.Path), resolvedPath(results[0].Path))
	assert.True(t, results[0].Removed)
	assert.Equal(t, []string{pr7Branch}, results[0].DeletedBranches)
	assert.Equal(t, []string{"main"}, branches(t, f.clone), "only the clone's own branch is left")

	// A pick that is no longer there is reported, not removed.
	results, err = svc.RemoveAllClean(t.Context(), []Pick{{RepoPath: f.clone, Path: pr.Path}})
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].Removed)
	assert.NotEmpty(t, results[0].Reason)
}

func TestAPullRequestThatMovesOnLeavesItsWorktreesCommitsOnGitHub(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	gh := &fakeGitHub{}
	gh.set("example-repo", 7, ItemState{PullRequest: true, Title: "Add the widget", State: ItemOpen, HeadOID: f.prHead})
	svc := e.linked(linkDeps{gh: gh, launches: &fakeLaunches{}})

	pr, err := NewMaker(svc).Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)

	// Someone pushes to the pull request after the launch: its head moves past the
	// worktree's, and no remote-tracking branch has the commits the worktree was made at.
	src := filepath.Join(e.home, "src")
	testutil.RunGit(t, src, "switch", "-q", "contributor-fix")
	moved := testutil.Commit(t, src, "docs.md", "docs\n")
	testutil.RunGit(t, f.bare, "fetch", "-q", src, "+contributor-fix:refs/pull/7/head")
	gh.set("example-repo", 7, ItemState{
		PullRequest: true, Title: "Add the widget", State: ItemOpen, HeadOID: moved, HeadRef: "contributor-fix",
	})

	snap, err := svc.List(t.Context())
	require.NoError(t, err)

	row := mustRow(t, snap, pr.Path)
	assert.Equal(t, 0, row.Unpushed, "the commits it was made at came from GitHub")
	assert.True(t, row.OnGitHub)
	assert.Equal(t, []Branch{{Name: pr7Branch, Head: f.prHead, Kind: BranchOwn, Deletable: true}}, row.Branches)

	// A session checks out the pull request's own branch, leaving the worktree's beside it.
	testutil.RunGit(t, pr.Path, "switch", "-q", "-c", "contributor-fix")

	snap, err = svc.List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []Branch{
		{Name: pr7Branch, Head: f.prHead, Kind: BranchOwn, Deletable: true},
		{Name: "contributor-fix", Head: f.prHead, Kind: BranchPullRequest, Deletable: true},
	}, mustRow(t, snap, pr.Path).Branches, "both at the head it was made at")

	// A commit made in the worktree is the user's own.
	work := testutil.Commit(t, pr.Path, "fix.go", "package fix\n")

	snap, err = svc.List(t.Context())
	require.NoError(t, err)

	row = mustRow(t, snap, pr.Path)
	assert.Equal(t, 1, row.Unpushed)
	assert.False(t, row.OnGitHub)
	assert.Equal(t, []Branch{
		{Name: pr7Branch, Head: f.prHead, Kind: BranchOwn, Deletable: true},
		{Name: "contributor-fix", Head: work, Kind: BranchPullRequest, Reason: "1 commit on it isn't on GitHub"},
	}, row.Branches)
}

func TestRemoveTakesThePullRequestsBranchAtTheHeadItFetchedWithoutGitHub(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	svc := e.linked(linkDeps{gh: &fakeGitHub{err: errFakeGitHub}, launches: &fakeLaunches{}})

	pr, err := NewMaker(svc).Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)

	// No remote-tracking branch has its head, but the record says it came from GitHub.
	res, err := svc.Remove(t.Context(), f.clone, pr.Path, []string{pr7Branch})
	require.NoError(t, err)
	assert.True(t, res.Removed)
	assert.Equal(t, []string{pr7Branch}, res.DeletedBranches)
	assert.Empty(t, res.Reason)
	assert.NotContains(t, branches(t, f.clone), pr7Branch)
}

func TestRemoveKeepsThePullRequestsBranchWithoutGitHub(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	records := &fakeLaunches{}
	svc := e.linked(linkDeps{gh: &fakeGitHub{err: errFakeGitHub}, launches: records})

	pr, err := NewMaker(svc).Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)

	records.forgetFetched()

	// Recorded before the app kept the head it fetched, its head is on GitHub only as the
	// pull request's, which only GitHub can say.
	res, err := svc.Remove(t.Context(), f.clone, pr.Path, []string{pr7Branch})
	require.NoError(t, err)
	assert.True(t, res.Removed)
	assert.Equal(t, "kept worktree-pr-7: 1 commit on it isn't on GitHub", res.Reason)
	assert.Contains(t, branches(t, f.clone), pr7Branch)
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
	svc := e.linked(linkDeps{launches: records, running: &testutil.Lister[procs.Process]{List: daemon}})

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
	inEditor := e.linked(linkDeps{launches: records, running: &testutil.Lister[procs.Process]{List: editor}})

	snap, err = inEditor.List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []Session{{PID: 87508, Command: "codex"}}, mustRow(t, snap, got.Path).InUse)
	require.ErrorIs(t, testutil.ErrOf(inEditor.Remove(t.Context(), f.clone, got.Path, nil)), ErrInUse)

	require.NoError(t, testutil.ErrOf(svc.Remove(t.Context(), f.clone, got.Path, nil)))
	assert.NoDirExists(t, got.Path)
}

func TestSweepLeavesWhatItCannotTellIsUnused(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newSweepFixture(t, e)
	svc := e.linked(linkDeps{gh: f.gh, launches: f.launches, auto: true, running: &testutil.Lister[procs.Process]{Err: procs.ErrUnsupported}})

	report := svc.Sweep(t.Context())
	assert.Empty(t, report.Cleaned, "without the running processes the sweep never guesses")
	assert.Empty(t, report.Failed)
	assert.Equal(t, 6, report.Leftover)

	busy := e.linked(linkDeps{gh: f.gh, launches: f.launches, auto: true, running: &testutil.Lister[procs.Process]{List: []procs.Process{
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
		"fetch --no-tags --refmap= --upload-pack=git-upload-pack origin refs/pull/7/head:refs/heads/worktree-pr-7":   true,
		"fetch --no-tags --refmap= --upload-pack=git-upload-pack upstream refs/pull/7/head:refs/heads/worktree-pr-8": false,
		"fetch --no-tags --refmap= --upload-pack=git-upload-pack origin refs/pull/07/head:refs/heads/worktree-pr-7":  false,
		"fetch --no-tags --refmap= --upload-pack=git-upload-pack origin +refs/pull/7/head:refs/heads/worktree-pr-7":  false,
		"fetch --no-tags --refmap= --upload-pack=git-upload-pack origin refs/pull/7/head:refs/heads/main":            false,
		"fetch --no-tags --refmap= --upload-pack=git-upload-pack origin +refs/heads/main:refs/remotes/origin/main":   true,
		"fetch --no-tags --refmap= --upload-pack=git-upload-pack origin +refs/heads/main:refs/remotes/upstream/main": false,
		"fetch --no-tags --refmap= --upload-pack=git-upload-pack origin +refs/heads/main:refs/heads/main":            false,
		"fetch --no-tags --refmap= --upload-pack=x origin refs/pull/7/head:refs/heads/worktree-pr-7":                 false,
		"fetch --no-tags --upload-pack=git-upload-pack origin refs/pull/7/head:refs/heads/worktree-pr-7":             false,
		"fetch --no-tags origin refs/pull/7/head:refs/heads/worktree-pr-7":                                           false,
		"fetch origin refs/pull/7/head:refs/heads/worktree-pr-7":                                                     false,
		"worktree add /srv/wt/pr-7 worktree-pr-7":                                                                    true,
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
		"ls-remote --upload-pack=git-upload-pack --symref origin HEAD":                         true,
		"ls-remote --symref origin HEAD":                                                       false,
		"ls-remote --symref --upload-pack=x HEAD":                                              false,
		"ls-remote --upload-pack=git-upload-pack --symref up..stream HEAD":                     false,
		"rev-parse --verify --quiet refs/remotes/up..stream/main":                              false,
		"symbolic-ref --quiet refs/remotes/up..stream/HEAD":                                    false,
		"fetch --no-tags --refmap= --upload-pack=git-upload-pack up..stream " +
			"+refs/heads/main:refs/remotes/up..stream/main": false,
	} {
		assert.Equal(t, want, allowed(strings.Fields(args)), args)
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
