package worktrees

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// headPR is pull request number of acme-org/example-repo, in state at head, whose head
// branch is branch in the repository headRepo (owner/name).
func headPR(number int, state ItemStatus, head, branch, headRepo string) HeadPR {
	s := pr(state, head)
	s.HeadRef = branch

	return HeadPR{Number: number, State: s, HeadRepo: headRepo}
}

// The repository the test clones' origin points at, as GitHub names it, its name, and the
// branch the rigs' worktrees are pushed on.
const (
	ownRepo      = "acme-org/example-repo"
	exampleName  = "example-repo"
	pushedBranch = "feat/x"
)

// branchRig is a clone of acme-org/example-repo with a worktree on feat/x, pushed, and a
// Service whose clock the test moves.
type branchRig struct {
	e        *env
	repo     string
	path     string
	head     string
	gh       *fakeGitHub
	launches *fakeLaunches
	clock    *testutil.Clock
	svc      *Service
}

// newBranchRig makes a branchRig whose worktree is at dir under the home folder, made by
// hand, or under the clone's .claude/worktrees with managed.
func newBranchRig(t *testing.T, managed bool) *branchRig {
	t.Helper()

	e := newEnv(t)
	r := &branchRig{e: e, repo: e.newGitHubRepo(t, exampleName), gh: &fakeGitHub{}, launches: &fakeLaunches{}}

	dir := filepath.Join(e.home, "wt", "feature")
	if managed {
		dir = filepath.Join(r.repo, ".claude", "worktrees", "feature")
	}

	r.path = addWorktree(t, r.repo, dir, pushedBranch)
	r.head = testutil.Commit(t, r.path, "x.txt", "x\n")
	pushed(t, r.repo, pushedBranch, r.head)

	r.clock = testutil.NewClock(listedAt())
	r.svc = e.linked(linkDeps{gh: r.gh, launches: r.launches})
	r.svc.clock = r.clock

	return r
}

// list lists the rig's worktrees and returns the worktree's row.
func (r *branchRig) list(t *testing.T) Worktree {
	t.Helper()

	snap, err := r.svc.List(t.Context())
	require.NoError(t, err)
	require.Empty(t, snap.LinkError)

	return mustRow(t, snap, r.path)
}

func TestListLinksAWorktreeToThePullRequestItsBranchHeads(t *testing.T) {
	t.Parallel()

	r := newBranchRig(t, false)
	r.gh.setHead(pushedBranch,
		headPR(201, ItemOpen, otherSHA, pushedBranch, "octo-fork/example-repo"),
		headPR(7, ItemMerged, r.head, pushedBranch, ownRepo),
	)

	row := r.list(t)
	require.NotNil(t, row.Item, "the newest pull request from the repository itself, not the fork's")
	assert.Equal(t, LinkBranch, row.LinkedBy)
	assert.Equal(t, LinkedItem{
		Kind: item.KindPR, Owner: exampleRepo().Owner, Name: exampleName, Number: 7, Title: "Fix things",
		URL: "https://github.com/acme-org/example-repo/pull/1", State: ItemMerged, ClosedAt: "2026-11-06T18:00:00Z",
		HeadOID: r.head, HeadRef: pushedBranch,
	}, *row.Item)
	assert.Equal(t, StageFinished, row.Stage, "merged, clean, and every commit pushed")
	assert.True(t, row.CanCleanUp)
	assert.False(t, autoCleanable(row), "a worktree made by hand and linked by its branch is cleaned up by hand alone")

	require.Len(t, r.gh.heads, 1)
	assert.Equal(t, []HeadQuery{{Repo: exampleRepo(), Branch: pushedBranch}}, r.gh.heads[0], "asked about in origin's repository")
	assert.Empty(t, r.gh.calls[0], "and nothing else")

	assert.Empty(t, r.launches.all(), "a worktree linked by its branch is never recorded as the item's own")
	assert.Empty(t, row.Branches, "nor does it offer its pull request's branch")
}

func TestABranchLinkedWorktreeTheAppMadeIsCleanedUpOnlyAtTheMergedHead(t *testing.T) {
	t.Parallel()

	r := newBranchRig(t, true)
	r.gh.setHead(pushedBranch, headPR(7, ItemMerged, r.head, pushedBranch, ownRepo))

	row := r.list(t)
	assert.Equal(t, LinkBranch, row.LinkedBy)
	assert.True(t, autoCleanable(row), "HEAD is the merged pull request's head")
	assert.Empty(t, r.launches.all(), "and still not recorded as the item's own")

	other := newBranchRig(t, true)
	other.gh.setHead(pushedBranch, headPR(8, ItemMerged, otherSHA, pushedBranch, ownRepo))

	row = other.list(t)
	assert.Equal(t, LinkBranch, row.LinkedBy)
	assert.Equal(t, StageFinished, row.Stage)
	assert.False(t, autoCleanable(row), "never by the branch's name alone")
}

func TestListLeavesAForksBranchOfTheSameNameUnlinked(t *testing.T) {
	t.Parallel()

	r := newBranchRig(t, false)
	r.gh.setHead(pushedBranch,
		headPR(201, ItemMerged, r.head, pushedBranch, "octo-fork/example-repo"),
		headPR(202, ItemMerged, r.head, pushedBranch, ""),
		headPR(203, ItemMerged, r.head, "feat/other", ownRepo),
	)
	r.gh.setHead("feat/other", headPR(204, ItemMerged, r.head, "feat/other", ownRepo))

	row := r.list(t)
	assert.Nil(t, row.Item, "a fork's, a deleted fork's, and another branch's pull requests never link")
	assert.Empty(t, row.LinkedBy)
	assert.Empty(t, row.Stage)
}

func TestListLinksByBranchOnlyWhatWasPushedToGitHub(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	clone := e.newGitHubRepo(t, exampleName)
	testutil.RunGit(t, clone, "remote", "add", "upstream", "https://github.com/upstream-org/example-repo.git")
	testutil.RunGit(t, clone, "remote", "add", "mirror", "https://gitlab.example.com/acme-org/example-repo.git")

	// Not pushed anywhere.
	local := addWorktree(t, clone, filepath.Join(e.home, "wt", "local"), "feat/local")

	// Pushed to upstream alone, and following it.
	followed := addWorktree(t, clone, filepath.Join(e.home, "wt", "followed"), "feat/followed")
	followedHead := testutil.Commit(t, followed, "f.txt", "f\n")
	testutil.RunGit(t, clone, "update-ref", "refs/remotes/upstream/feat/followed", followedHead)
	testutil.RunGit(t, clone, "update-ref", "refs/remotes/origin/feat/followed", followedHead)
	testutil.RunGit(t, clone, "config", "branch.feat/followed.remote", "upstream")
	testutil.RunGit(t, clone, "config", "branch.feat/followed.merge", "refs/heads/feat/followed")

	// Made from upstream's release branch, which it follows, and never pushed itself.
	based := addWorktree(t, clone, filepath.Join(e.home, "wt", "based"), "feat/based")
	testutil.RunGit(t, clone, "update-ref", "refs/remotes/upstream/release", followedHead)
	testutil.RunGit(t, clone, "config", "branch.feat/based.remote", "upstream")
	testutil.RunGit(t, clone, "config", "branch.feat/based.merge", "refs/heads/release")

	// Pushed to the mirror alone, which isn't GitHub.
	mirrored := addWorktree(t, clone, filepath.Join(e.home, "wt", "mirrored"), "feat/mirrored")
	testutil.RunGit(t, clone, "update-ref", "refs/remotes/mirror/feat/mirrored", followedHead)

	// The repository's default branch, as origin's HEAD says.
	develop := addWorktree(t, clone, filepath.Join(e.home, "wt", "develop"), "develop")
	pushed(t, clone, "develop", followedHead)
	testutil.RunGit(t, clone, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/develop")

	// Detached at a pushed commit.
	detached := filepath.Join(e.home, "wt", "detached")
	testutil.RunGit(t, clone, "worktree", "add", "-q", "--detach", detached, followedHead)

	gh := &fakeGitHub{}
	svc := e.linked(linkDeps{gh: gh})

	snap, err := svc.List(t.Context())
	require.NoError(t, err)

	require.Len(t, gh.heads, 1)
	assert.Equal(t, []HeadQuery{{Repo: repo.Name{Owner: "upstream-org", Repo: exampleName}, Branch: "feat/followed"}},
		gh.heads[0], "the followed remote's branch is asked about first, and only branches pushed to GitHub")

	for _, path := range []string{local, based, mirrored, develop, detached} {
		assert.Nil(t, mustRow(t, snap, path).Item, path)
	}
}

func TestListAsksAboutABranchOnceUntilItMoves(t *testing.T) {
	t.Parallel()

	r := newBranchRig(t, false)
	r.gh.setHead(pushedBranch, headPR(7, ItemOpen, r.head, pushedBranch, ownRepo))

	row := r.list(t)
	assert.Equal(t, StageActive, row.Stage)

	r.gh.set(exampleName, 7, headPR(7, ItemMerged, r.head, pushedBranch, ownRepo).State)

	row = r.list(t)
	assert.Equal(t, LinkBranch, row.LinkedBy)
	assert.Equal(t, ItemMerged, row.Item.State, "its state is read again by number")
	assert.Equal(t, StageFinished, row.Stage)

	calls, withHeads := r.gh.requests()
	assert.Equal(t, 2, calls)
	assert.Equal(t, 1, withHeads, "the branch was asked about once")
	assert.Equal(t, []string{refKey(exampleRepo().Owner, exampleName, 7)}, refKeys(r.gh.calls[1]))

	// A push moves the branch: it is asked about again.
	head := testutil.Commit(t, r.path, "y.txt", "y\n")
	pushed(t, r.repo, pushedBranch, head)

	r.list(t)

	_, withHeads = r.gh.requests()
	assert.Equal(t, 2, withHeads)
}

func TestListAsksAgainAboutABranchWithNoPullRequestOnlyNowAndThen(t *testing.T) {
	t.Parallel()

	r := newBranchRig(t, false)

	assert.Nil(t, r.list(t).Item)
	assert.Nil(t, r.list(t).Item)

	calls, _ := r.gh.requests()
	assert.Equal(t, 1, calls, "a branch no pull request heads is not asked about again at once")

	r.clock.Advance(headRecheck)
	r.gh.setHead(pushedBranch, headPR(7, ItemOpen, r.head, pushedBranch, ownRepo))

	row := r.list(t)
	require.NotNil(t, row.Item, "but is once a while has passed, so a pull request opened since is found")
	assert.Equal(t, 7, row.Item.Number)

	calls, withHeads := r.gh.requests()
	assert.Equal(t, 2, calls)
	assert.Equal(t, 2, withHeads)
}

func TestListAsksAgainAboutABranchThatMovedPastItsDonePullRequest(t *testing.T) {
	t.Parallel()

	r := newBranchRig(t, false)
	r.gh.setHead(pushedBranch, headPR(7, ItemMerged, otherSHA, pushedBranch, ownRepo))

	assert.Equal(t, 7, r.list(t).Item.Number)

	r.gh.set(exampleName, 7, headPR(7, ItemMerged, otherSHA, pushedBranch, ownRepo).State)
	r.gh.setHead(pushedBranch,
		headPR(9, ItemOpen, r.head, pushedBranch, ownRepo), headPR(7, ItemMerged, otherSHA, pushedBranch, ownRepo))

	assert.Equal(t, 7, r.list(t).Item.Number, "as last said, until a while has passed")

	r.clock.Advance(headRecheck + time.Second)

	row := r.list(t)
	assert.Equal(t, 9, row.Item.Number, "the newer pull request on the reused branch")
	assert.Equal(t, StageActive, row.Stage)

	_, withHeads := r.gh.requests()
	assert.Equal(t, 2, withHeads)
}

func TestListPrefersTheOpenPullRequestABranchHeads(t *testing.T) {
	t.Parallel()

	r := newBranchRig(t, false)
	r.gh.setHead(pushedBranch,
		headPR(7, ItemMerged, r.head, pushedBranch, ownRepo),
		headPR(8, ItemClosed, r.head, pushedBranch, ownRepo),
		headPR(9, ItemOpen, r.head, pushedBranch, ownRepo),
	)

	row := r.list(t)
	require.NotNil(t, row.Item)
	assert.Equal(t, 9, row.Item.Number, "the open one, though an older one was updated since")
	assert.Equal(t, StageActive, row.Stage)
}

func TestListAsksAgainAboutABranchReusedAtTheHeadOfItsDonePullRequest(t *testing.T) {
	t.Parallel()

	r := newBranchRig(t, false)
	r.gh.setHead(pushedBranch, headPR(7, ItemMerged, r.head, pushedBranch, ownRepo))
	r.gh.set(exampleName, 7, headPR(7, ItemMerged, r.head, pushedBranch, ownRepo).State)

	assert.Equal(t, 7, r.list(t).Item.Number)

	// A pull request opened again from the branch, with nothing pushed since.
	r.gh.setHead(pushedBranch,
		headPR(9, ItemOpen, r.head, pushedBranch, ownRepo), headPR(7, ItemMerged, r.head, pushedBranch, ownRepo))

	assert.Equal(t, 7, r.list(t).Item.Number, "as last said, until a while has passed")

	r.clock.Advance(headRecheck)

	row := r.list(t)
	assert.Equal(t, 9, row.Item.Number)
	assert.Equal(t, StageActive, row.Stage)

	_, withHeads := r.gh.requests()
	assert.Equal(t, 2, withHeads)
}

func TestListForgetsABranchsPullRequestGitHubNoLongerKnows(t *testing.T) {
	t.Parallel()

	r := newBranchRig(t, false)
	r.gh.setHead(pushedBranch, headPR(7, ItemOpen, r.head, pushedBranch, ownRepo))

	assert.NotNil(t, r.list(t).Item)

	// Its number now answers as another branch's pull request, as it never could: the
	// link is dropped and the branch asked about again.
	r.gh.set(exampleName, 7, headPR(7, ItemOpen, r.head, "feat/other", ownRepo).State)
	r.gh.setHead(pushedBranch)

	assert.Nil(t, r.list(t).Item)
	assert.Nil(t, r.list(t).Item)
	assert.Nil(t, r.list(t).Item)

	_, withHeads := r.gh.requests()
	assert.Equal(t, 2, withHeads, "asked about again once, then kept as heading none")
}

func TestListKeepsNoBranchAnswerWhenGitHubFails(t *testing.T) {
	t.Parallel()

	r := newBranchRig(t, false)
	r.gh.err = errFakeGitHub

	snap, err := r.svc.List(t.Context())
	require.NoError(t, err)
	assert.NotEmpty(t, snap.LinkError)
	assert.Nil(t, mustRow(t, snap, r.path).Item)

	r.gh.err = nil
	r.gh.setHead(pushedBranch, headPR(7, ItemOpen, r.head, pushedBranch, ownRepo))

	assert.Equal(t, 7, r.list(t).Item.Number, "the next listing asks again")
}

func TestListReadsAClonesRefsOnceForLinkingAndItsBranches(t *testing.T) {
	t.Parallel()

	r := newBranchRig(t, true)
	r.gh.setHead(pushedBranch, headPR(7, ItemMerged, r.head, pushedBranch, ownRepo))

	run := scriptedGit(r.e.git, func([]string) (reply, bool) { return reply{}, false })
	r.svc.run = run

	row := r.list(t)
	assert.Equal(t, LinkBranch, row.LinkedBy)
	assert.Equal(t, map[string]int{"for-each-ref": 1}, gitCalls(run, "for-each-ref"),
		"one read of the refs serves the link and the branches a removal offers")
}

func TestBranchAskSkipsWorktreesItCannotLink(t *testing.T) {
	t.Parallel()

	remotes := map[string]repo.Name{"origin": exampleRepo()}
	refs := withOwners(cloneRefs{
		tracking: map[string]string{"refs/remotes/origin/feat/x": headSHA},
		defaults: map[string]string{originRemote: mainBranch},
	}, []string{originRemote})

	_, ok := branchAsk(&Worktree{Branch: pushedBranch}, remotes, refs)
	assert.True(t, ok)

	for name, wt := range map[string]*Worktree{
		"the main worktree": {Branch: pushedBranch, Main: true},
		"prunable":          {Branch: pushedBranch, Prunable: true},
		"detached":          {Branch: pushedBranch, Detached: true},
		"bad name":          {Branch: "-x"},
		"default branch":    {Branch: mainBranch},
	} {
		_, ok = branchAsk(wt, remotes, refs)
		assert.False(t, ok, name)
	}

	_, ok = branchAsk(&Worktree{Branch: pushedBranch}, map[string]repo.Name{}, refs)
	assert.False(t, ok, "a clone with no GitHub remote links nothing by branch")
}

func TestWithOwnersTellsARemoteWhoseNameHoldsASlash(t *testing.T) {
	t.Parallel()

	refs := withOwners(cloneRefs{tracking: map[string]string{
		"refs/remotes/origin/feat/x":        headSHA,
		"refs/remotes/origin/mirror/feat/x": headSHA,
		"refs/remotes/origin/mirror":        headSHA,
		"refs/remotes/stranger/feat/x":      headSHA,
	}}, []string{"origin/mirror", "origin"})

	for ref, want := range map[string]remoteBranch{
		"refs/remotes/origin/feat/x":        {remote: "origin", branch: "feat/x"},
		"refs/remotes/origin/mirror/feat/x": {remote: "origin/mirror", branch: "feat/x"},
		"refs/remotes/origin/mirror":        {remote: "origin", branch: "mirror"},
	} {
		remote, branch, ok := refs.owner(ref)
		assert.True(t, ok, ref)
		assert.Equal(t, want, remoteBranch{remote: remote, branch: branch}, ref)
	}

	_, _, ok := refs.owner("refs/remotes/stranger/feat/x")
	assert.False(t, ok, "a remote the clone doesn't name owns nothing")

	_, _, ok = refs.owner("refs/remotes/origin/gone")
	assert.False(t, ok, "nor is a branch the clone hasn't anyone's")
}

// addSlashedMirror gives the clone at repo a remote that isn't on GitHub named
// origin/mirror, as its config may name one though git remote add refuses it beside origin.
func addSlashedMirror(t *testing.T, repo string) {
	t.Helper()

	testutil.RunGit(t, repo, "config", "remote.origin/mirror.url", "https://gitlab.example.com/acme-org/example-repo.git")
	testutil.RunGit(t, repo, "config", "remote.origin/mirror.fetch", "+refs/heads/*:refs/remotes/origin/mirror/*")
}

func TestListLinksNothingByABranchPushedOnlyToARemoteNamedLikeOrigins(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	clone := e.newGitHubRepo(t, exampleName)
	addSlashedMirror(t, clone)

	path := addWorktree(t, clone, filepath.Join(e.home, "wt", "mirrored"), "mirror/feat/y")
	head := testutil.Commit(t, path, "y.txt", "y\n")
	testutil.RunGit(t, clone, "update-ref", "refs/remotes/origin/mirror/feat/y", head)
	testutil.RunGit(t, clone, "config", "branch.mirror/feat/y.remote", "origin/mirror")
	testutil.RunGit(t, clone, "config", "branch.mirror/feat/y.merge", "refs/heads/feat/y")

	gh := &fakeGitHub{}
	gh.setHead("mirror/feat/y", headPR(7, ItemMerged, head, "mirror/feat/y", ownRepo))

	snap, err := e.linked(linkDeps{gh: gh}).List(t.Context())
	require.NoError(t, err)
	assert.Nil(t, mustRow(t, snap, path).Item, "refs/remotes/origin/mirror/feat/y is the mirror's feat/y, not origin's")
	assert.Empty(t, gh.heads, "and GitHub isn't asked about it")
}

func TestHeadCacheStartsOverWhenFull(t *testing.T) {
	t.Parallel()

	c := newHeadCache()

	for i := range headCacheMax + 1 {
		c.learn(headKey{tip: string(rune('a'+i%26)) + string(rune(i))}, nil, listedAt())
	}

	assert.Len(t, c.entries, 1, "a full cache starts over")
}
