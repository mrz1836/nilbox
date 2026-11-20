package worktrees

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/worklog"
)

var errFakeRefs = errors.New("fake: the refs can't be read")

// unpushedLogWords start the log of the commits not on GitHub, as gitCalls counts it.
const unpushedLogWords = "log --no-show-signature --no-mailmap --max-count=200"

// detailCalls are the git runs Details may make, as gitCalls counts them.
func detailCalls() []string {
	return []string{"worktree list", "status", "rev-list", unpushedLogWords}
}

// countingBase is a BaseReader that counts each of its reads, then fails it with the error
// set for it, or makes it with the BaseReader it holds.
type countingBase struct {
	BaseReader

	originErr, trackingErr, betweenErr error
	origin, tracking, between          atomic.Int32
}

func (c *countingBase) OriginHead(ctx context.Context, dir string) (string, error) {
	c.origin.Add(1)

	if c.originErr != nil {
		return "", c.originErr
	}

	return c.BaseReader.OriginHead(ctx, dir)
}

func (c *countingBase) TrackingBranch(ctx context.Context, dir, branch string) (string, error) {
	c.tracking.Add(1)

	if c.trackingErr != nil {
		return "", c.trackingErr
	}

	return c.BaseReader.TrackingBranch(ctx, dir, branch)
}

func (c *countingBase) CommitsBetween(ctx context.Context, dir, from, to string) (int, error) {
	c.between.Add(1)

	if c.betweenErr != nil {
		return 0, c.betweenErr
	}

	return c.BaseReader.CommitsBetween(ctx, dir, from, to)
}

// reads are how many times OriginHead, TrackingBranch, and CommitsBetween were read.
func (c *countingBase) reads() []int32 {
	return []int32{c.origin.Load(), c.tracking.Load(), c.between.Load()}
}

// detailsRig is a clone, alpha, whose main is on origin as the clone last fetched it
// (origin/main, which origin/HEAD names), with a linked worktree on its own branch, and
// a Service whose git counts its runs, answering those intercept takes itself.
type detailsRig struct {
	e        *env
	repo     string
	worktree string
	run      *testutil.Runner
	base     *countingBase
	launches *fakeLaunches
	events   *testutil.Events
	svc      *Service
}

func newDetailsRig(t *testing.T, intercept func(sub []string) (reply, bool)) *detailsRig {
	t.Helper()

	e := newEnv(t)
	repo := e.newRepo(t, "alpha")
	pushed(t, repo, mainBranch, strings.TrimSpace(testutil.RunGit(t, repo, "rev-parse", "HEAD")))
	testutil.RunGit(t, repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")

	r := &detailsRig{
		e: e, repo: repo, worktree: addWorktree(t, repo, filepath.Join(e.home, "wt", "feature"), "feature"),
		base: &countingBase{BaseReader: e.guardOf()}, launches: &fakeLaunches{}, events: &testutil.Events{},
	}

	if intercept == nil {
		intercept = func([]string) (reply, bool) { return reply{}, false }
	}

	r.svc = e.linked(linkDeps{launches: r.launches, base: r.base, events: r.events})
	r.run = scriptedGit(e.git, intercept)
	r.svc.run = r.run

	return r
}

// details reads the rig's worktree's details, which must not fail.
func (r *detailsRig) details(t *testing.T) Details {
	t.Helper()

	d, err := r.svc.Details(t.Context(), r.repo, r.worktree)
	require.NoError(t, err)

	return d
}

// writeFiles writes n untracked files into dir, named so they sort as written.
func writeFiles(t *testing.T, dir string, n int) {
	t.Helper()

	for i := range n {
		testutil.WriteFile(t, filepath.Join(dir, fmt.Sprintf("notes-%03d.txt", i)), "draft\n")
	}
}

func TestDetailsOfACleanWorktreeOnGitHubReadOnlyWhatTheyNeed(t *testing.T) {
	t.Parallel()

	r := newDetailsRig(t, nil)

	assert.Equal(t, Details{
		Files: []FileEntry{}, Commits: []UnpushedCommit{}, Base: "origin/main", Errors: []string{},
	}, r.details(t), "nothing changed, nothing unpushed, and level with origin/main")
	assert.Equal(t, map[string]int{"worktree list": 1, "status": 1, "rev-list": 1}, gitCalls(r.run, detailCalls()...),
		"a status and a count, and no log, as no commit isn't on GitHub")
	assert.Equal(t, []int32{1, 1, 1}, r.base.reads())
	assert.Empty(t, r.events.All(), "Details tells the window nothing")
}

func TestDetailsOfABusyWorktree(t *testing.T) {
	t.Parallel()

	r := newDetailsRig(t, nil)

	first := testutil.Commit(t, r.worktree, "a.go", "package a\n")
	second := testutil.Commit(t, r.worktree, "b.go", "package b\n")
	writeFiles(t, r.worktree, 15)

	// Origin's main moves on three commits, as the clone last fetched it.
	var moved string
	for _, name := range []string{"x.md", "y.md", "z.md"} {
		moved = testutil.Commit(t, r.repo, name, name+"\n")
	}

	pushed(t, r.repo, mainBranch, moved)

	r.launches.records = []worklog.Record{{
		ClonePath: r.repo, Worktree: "feature", Path: r.worktree, Branch: "feature", Kind: item.KindPR, Number: 3,
		CreatedAt: listedAt(),
	}}

	d := r.details(t)

	assert.Len(t, d.Files, 15, "every one, as the status lists them now")
	assert.Equal(t, FileEntry{Code: "??", Path: "notes-000.txt"}, d.Files[0])
	assert.True(t, slices.IsSortedFunc(d.Files, func(a, b FileEntry) int { return strings.Compare(a.Path, b.Path) }))
	assert.Equal(t, 15, d.FileCount)

	require.Len(t, d.Commits, 2)
	assert.Equal(t, []string{second + " b.go", first + " a.go"},
		[]string{d.Commits[0].OID + " " + d.Commits[0].Subject, d.Commits[1].OID + " " + d.Commits[1].Subject}, "newest first")
	assert.Positive(t, d.Commits[0].Unix)
	assert.Equal(t, 2, d.Unpushed)

	assert.Equal(t, "origin/main", d.Base)
	assert.Equal(t, 3, d.Behind)
	assert.Equal(t, "2026-11-05T09:30:00Z", d.CreatedAt, "from the worktree's record")
	assert.Empty(t, d.Errors)
	assert.NotNil(t, d.Errors)

	assert.Equal(t, map[string]int{"worktree list": 1, "status": 1, "rev-list": 1, unpushedLogWords: 1},
		gitCalls(r.run, detailCalls()...))
	assert.Empty(t, r.events.All())
}

func TestDetailsListTheFilesAsTheyAreNow(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		files, listed int
	}{
		{files: 0}, {files: 3, listed: 3}, {files: 12, listed: 12}, {files: 13, listed: 13}, {files: 205, listed: 200},
	} {
		t.Run(fmt.Sprint(tt.files), func(t *testing.T) {
			t.Parallel()

			r := newDetailsRig(t, nil)
			writeFiles(t, r.worktree, tt.files)

			d := r.details(t)

			assert.Equal(t, tt.files, d.FileCount)
			assert.Len(t, d.Files, tt.listed, "every one by path, at most 200")
			assert.NotNil(t, d.Files)

			if tt.listed > 0 {
				assert.Equal(t, fmt.Sprintf("notes-%03d.txt", tt.listed-1), d.Files[tt.listed-1].Path)
			}
		})
	}
}

func TestDetailsReadTheFilesAgainSinceTheListing(t *testing.T) {
	t.Parallel()

	r := newDetailsRig(t, nil)
	writeFiles(t, r.worktree, 15)

	snap, err := r.svc.List(t.Context())
	require.NoError(t, err)

	row := mustRow(t, snap, r.worktree)
	require.Equal(t, 15, row.DirtyCount)
	require.Len(t, row.Files, 12)

	// The user commits five of them.
	testutil.RunGit(t, r.worktree, "add", "notes-000.txt", "notes-001.txt", "notes-002.txt", "notes-003.txt",
		"notes-004.txt")
	testutil.RunGit(t, r.worktree, "commit", "-q", "-m", "notes")

	d := r.details(t)
	assert.Equal(t, 10, d.FileCount)
	require.Len(t, d.Files, 10, "the files there now, not the listing's")
	assert.Equal(t, FileEntry{Code: "??", Path: "notes-005.txt"}, d.Files[0])

	// And then adds ten more.
	for i := range 10 {
		testutil.WriteFile(t, filepath.Join(r.worktree, fmt.Sprintf("more-%02d.txt", i)), "draft\n")
	}

	d = r.details(t)
	assert.Equal(t, 20, d.FileCount)
	require.Len(t, d.Files, 20)
	assert.Equal(t, FileEntry{Code: "??", Path: "more-00.txt"}, d.Files[0])
}

func TestDetailsReadTheRecordTheListingReads(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	launches := &fakeLaunches{}
	svc := e.linked(linkDeps{gh: &fakeGitHub{}, launches: launches, base: e.guardOf()})

	pr, err := NewMaker(svc).Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)

	// An older record names the same folder without the commit the worktree was made at.
	made := launches.all()
	require.Len(t, made, 1)
	require.True(t, gitread.IsCommitID(made[0].FetchedHead))

	older := made[0]
	older.FetchedHead, older.CreatedAt = "", listedAt().Add(-time.Hour)
	launches.records = []worklog.Record{older, made[0]}

	d, err := svc.Details(t.Context(), f.clone, pr.Path)
	require.NoError(t, err)

	snap, err := svc.List(t.Context())
	require.NoError(t, err)

	row := mustRow(t, snap, pr.Path)
	assert.Equal(t, 0, row.Unpushed, "the listing reads the newer record, which holds the commit fetched from GitHub")
	assert.Equal(t, row.Unpushed, d.Unpushed, "and so does Details")
	assert.Empty(t, d.Commits)
	assert.Equal(t, "2026-11-05T09:30:00Z", d.CreatedAt, "the newer record's")
}

func TestRecordsByDir(t *testing.T) {
	t.Parallel()

	const (
		dir   = "/srv/projects/example-repo/.claude/worktrees/pr-7"
		other = "/srv/projects/example-repo/.claude/worktrees/pr-8"
	)

	record := func(path, fetched string, hour int) worklog.Record {
		return worklog.Record{
			ClonePath: repoDir, Path: path, FetchedHead: fetched, CreatedAt: listedAt().Add(time.Duration(hour) * time.Hour),
		}
	}

	for _, tt := range []struct {
		name    string
		records []worklog.Record
		want    worklog.Record
	}{
		{name: "one record", records: []worklog.Record{record(dir, headSHA, 0)}, want: record(dir, headSHA, 0)},
		{
			name:    "the newer holds the commit fetched",
			records: []worklog.Record{record(dir, "", 0), record(dir, headSHA, 1)}, want: record(dir, headSHA, 1),
		},
		{
			name:    "the older holds the commit fetched",
			records: []worklog.Record{record(dir, headSHA, 0), record(dir, "", 1)}, want: record(dir, headSHA, 0),
		},
		{
			name:    "both hold one",
			records: []worklog.Record{record(dir, otherSHA, 0), record(dir, headSHA, 1)}, want: record(dir, headSHA, 1),
		},
		{
			name:    "neither holds one",
			records: []worklog.Record{record(dir, "", 0), record(dir, "main", 1)}, want: record(dir, "main", 1),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			records := append(slices.Clone(tt.records), record(other, otherSHA, 2))
			byDir := recordsByDir(records)

			assert.Equal(t, tt.want, byDir[resolvedPath(dir)])
			assert.Equal(t, record(other, otherSHA, 2), byDir[resolvedPath(other)])

			wantHeads := map[string]string{resolvedPath(other): otherSHA}
			if gitread.IsCommitID(tt.want.FetchedHead) {
				wantHeads[resolvedPath(dir)] = tt.want.FetchedHead
			}

			assert.Equal(t, wantHeads, fetchedHeads(records), "the listing's commits fetched come from the same records")
		})
	}
}

func TestDetailsLeaveTheCommitsFetchedFromGitHubOut(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	gh := &fakeGitHub{}
	gh.set("example-repo", 7, ItemState{PullRequest: true, Title: "Add the widget", State: ItemOpen, HeadOID: f.prHead})

	launches := &fakeLaunches{}
	svc := e.linked(linkDeps{gh: gh, launches: launches, base: e.guardOf()})

	pr, err := NewMaker(svc).Ensure(t.Context(), f.request(item.KindPR, 7))
	require.NoError(t, err)

	// Someone pushes to the pull request after the launch: its head moves past the
	// worktree's, and no remote-tracking branch has the commits the worktree was made at.
	src := filepath.Join(e.home, "src")
	testutil.RunGit(t, src, "switch", "-q", "contributor-fix")
	testutil.Commit(t, src, "docs.md", "docs\n")
	testutil.RunGit(t, f.bare, "fetch", "-q", src, "+contributor-fix:refs/pull/7/head")

	d, err := svc.Details(t.Context(), f.clone, pr.Path)
	require.NoError(t, err)
	assert.Equal(t, Details{
		Files: []FileEntry{}, Commits: []UnpushedCommit{}, Base: "origin/main", Behind: 0,
		CreatedAt: "2026-11-05T09:30:00Z", Errors: []string{},
	}, d, "the commits it was made at came from GitHub")

	// A commit made in the worktree is the user's own.
	work := testutil.Commit(t, pr.Path, "fix.go", "package fix\n")

	d, err = svc.Details(t.Context(), f.clone, pr.Path)
	require.NoError(t, err)
	assert.Equal(t, 1, d.Unpushed)
	require.Len(t, d.Commits, 1)
	assert.Equal(t, work, d.Commits[0].OID)
	assert.Equal(t, "fix.go", d.Commits[0].Subject)

	// Without the record of the commit it was made at, the pull request's own looks like
	// the user's too.
	launches.forgetFetched()

	d, err = svc.Details(t.Context(), f.clone, pr.Path)
	require.NoError(t, err)
	assert.Equal(t, 2, d.Unpushed)
	require.Len(t, d.Commits, 2)
	assert.Equal(t, []string{work, f.prHead}, []string{d.Commits[0].OID, d.Commits[1].OID})
}

func TestDetailsSayNothingOfABaseTheCloneDoesntName(t *testing.T) {
	t.Parallel()

	r := newDetailsRig(t, nil)
	testutil.RunGit(t, r.repo, "symbolic-ref", "--delete", "refs/remotes/origin/HEAD")

	d := r.details(t)
	assert.Empty(t, d.Base)
	assert.Equal(t, -1, d.Behind)
	assert.Empty(t, d.Errors)
	assert.Equal(t, []int32{1, 0, 0}, r.base.reads(), "no branch to look for")

	// Origin's main is gone, though origin/HEAD names it.
	testutil.RunGit(t, r.repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	testutil.RunGit(t, r.repo, "update-ref", "-d", "refs/remotes/origin/main")

	d = r.details(t)
	assert.Equal(t, "origin/main", d.Base)
	assert.Equal(t, -1, d.Behind)
	assert.Empty(t, d.Errors)
	assert.Equal(t, []int32{2, 1, 0}, r.base.reads(), "no commit to count from")

	// Without a BaseReader, nothing is read of it.
	r.svc.base = nil

	d = r.details(t)
	assert.Empty(t, d.Base)
	assert.Equal(t, -1, d.Behind)
	assert.Empty(t, d.Errors)
	assert.Equal(t, []int32{2, 1, 0}, r.base.reads())
}

// refuse answers a git run whose words start with any of prefixes with git's refusal,
// saying "fatal: unreadable index".
func refuse(prefixes ...string) func(sub []string) (reply, bool) {
	return func(sub []string) (reply, bool) {
		for _, prefix := range prefixes {
			if startsWith(sub, prefix) {
				return reply{stderr: "fatal: unreadable index\n", err: exitError{code: 128}}, true
			}
		}

		return reply{}, false
	}
}

func TestDetailsFailSoft(t *testing.T) {
	t.Parallel()

	refsMessage := textutil.Sentence(gitread.Message(errFakeRefs))

	t.Run("each read that fails is a sentence, in the drawer's order", func(t *testing.T) {
		t.Parallel()

		r := newDetailsRig(t, refuse("status", "rev-list"))
		r.base.originErr = errFakeRefs

		assert.Equal(t, Details{
			Files: []FileEntry{}, FileCount: -1, Commits: []UnpushedCommit{}, Unpushed: -1, Behind: -1,
			Errors: []string{
				"Couldn't read the changed files. Unreadable index.",
				"Couldn't read the commits not on GitHub. Unreadable index.",
				"Couldn't read origin's default branch. " + refsMessage,
			},
		}, r.details(t))
		assert.Zero(t, gitCalls(r.run, unpushedLogWords)[unpushedLogWords], "nothing to list without a count")
	})

	t.Run("a count with no list keeps the count", func(t *testing.T) {
		t.Parallel()

		r := newDetailsRig(t, refuse(unpushedLogWords))
		testutil.Commit(t, r.worktree, "a.go", "package a\n")

		d := r.details(t)
		assert.Equal(t, 1, d.Unpushed)
		assert.Empty(t, d.Commits)
		assert.NotNil(t, d.Commits)
		assert.Equal(t, []string{"Couldn't read the commits not on GitHub. Unreadable index."}, d.Errors)
	})

	t.Run("a list git prints wrong", func(t *testing.T) {
		t.Parallel()

		r := newDetailsRig(t, func(sub []string) (reply, bool) {
			return reply{stdout: "something else\n"}, startsWith(sub, unpushedLogWords)
		})
		testutil.Commit(t, r.worktree, "a.go", "package a\n")

		d := r.details(t)
		assert.Equal(t, 1, d.Unpushed)
		assert.Equal(t, []string{`Couldn't read the commits not on GitHub. Unexpected commit list "something else".`}, d.Errors)
	})

	t.Run("origin's main can't be read", func(t *testing.T) {
		t.Parallel()

		r := newDetailsRig(t, nil)
		r.base.trackingErr = errFakeRefs

		d := r.details(t)
		assert.Equal(t, "origin/main", d.Base)
		assert.Equal(t, -1, d.Behind)
		assert.Equal(t, []string{"Couldn't read where origin/main is. " + refsMessage}, d.Errors)
	})

	t.Run("the commits behind origin's main can't be counted", func(t *testing.T) {
		t.Parallel()

		r := newDetailsRig(t, nil)
		r.base.betweenErr = errFakeRefs

		d := r.details(t)
		assert.Equal(t, "origin/main", d.Base)
		assert.Equal(t, -1, d.Behind)
		assert.Equal(t, []string{"Couldn't read how far behind origin/main the worktree is. " + refsMessage}, d.Errors)
	})

	t.Run("the guards refuse the status", func(t *testing.T) {
		t.Parallel()

		r := newDetailsRig(t, nil)
		guard := failingGuard{env: func() []string { return testutil.GitEnv(r.e.home) }, err: errFakeRefs, calls: &atomic.Int32{}}
		r.svc.guard = guard

		d := r.details(t)
		assert.Equal(t, -1, d.FileCount)
		assert.Equal(t, []string{"Couldn't read the changed files. " + refsMessage}, d.Errors,
			"in the guards' words, as the row's error says it")
		assert.Equal(t, int32(1), guard.calls.Load(), "the status alone needs the clone's setup; the reads run without")
		assert.Equal(t, 0, d.Unpushed)
	})
}

func TestDetailsRefuseARequestAboutAnythingButALinkedWorktree(t *testing.T) {
	t.Parallel()

	r := newDetailsRig(t, nil)
	ctx := t.Context()

	outside := filepath.Join(r.e.home, "outside")
	testutil.InitRepo(t, outside)

	elsewhere := filepath.Join(r.e.home, "elsewhere")
	require.NoError(t, os.MkdirAll(elsewhere, 0o700))

	gone := addWorktree(t, r.repo, filepath.Join(r.e.home, "wt", "gone"), "gone")
	require.NoError(t, os.RemoveAll(gone))

	for _, tt := range []struct {
		name           string
		repoPath, path string
		want           error
	}{
		{name: "a clone outside the repository locations", repoPath: outside, path: outside, want: ErrOutsideCloneRoot},
		{name: "the main worktree", repoPath: r.repo, path: r.repo, want: ErrMainWorktree},
		{name: "a folder that is no worktree of the clone", repoPath: r.repo, path: elsewhere, want: ErrNotAWorktree},
		{name: "a worktree whose folder is gone", repoPath: r.repo, path: gone, want: ErrPrunable},
	} {
		_, err := r.svc.Details(ctx, tt.repoPath, tt.path)
		require.ErrorIs(t, err, tt.want, tt.name)
	}

	assert.Empty(t, gitCalls(r.run, "status", "rev-list"), "nothing is read for a request it refuses")
	assert.Equal(t, []int32{0, 0, 0}, r.base.reads())
}

func TestDetailsStopWhenCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	r := newDetailsRig(t, func(sub []string) (reply, bool) {
		if !startsWith(sub, "status") {
			return reply{}, false
		}

		cancel()

		return reply{err: context.Canceled}, true
	})

	_, err := r.svc.Details(ctx, r.repo, r.worktree)
	require.ErrorIs(t, err, context.Canceled)
}
