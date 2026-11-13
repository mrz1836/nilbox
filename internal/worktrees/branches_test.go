package worktrees

import (
	"context"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/branchrules"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// exampleRepo is the GitHub repository the merged-branch tests' clone tracks.
func exampleRepo() repo.Name { return repo.Name{Owner: "acme-org", Repo: "example-repo"} }

// branchAt makes branch point at a new commit on top of base, without checking it out,
// and returns the commit.
func branchAt(t *testing.T, clone, branch, base string) string {
	t.Helper()

	commit := commitOn(t, clone, base, "work on "+branch)
	testutil.RunGit(t, clone, "update-ref", "refs/heads/"+branch, commit)

	return commit
}

// commitOn makes a commit on top of base that no branch points at, and returns it.
func commitOn(t *testing.T, clone, base, message string) string {
	t.Helper()

	tree := strings.TrimSpace(testutil.RunGit(t, clone, "rev-parse", base+"^{tree}"))

	return strings.TrimSpace(testutil.RunGit(t, clone, "commit-tree", tree, "-p", base, "-m", message))
}

// mergedFixture is a clone whose branches cover every case of the cleanup of merged
// branches, and a GitHub that knows which merged.
type mergedFixture struct {
	e     *env
	clone string
	svc   *Service
	gh    *fakeGitHub
	heads map[string]string
}

func newMergedFixture(t *testing.T) mergedFixture {
	t.Helper()

	e := newEnv(t)
	clone := e.newGitHubRepo(t, "example-repo")
	main := strings.TrimSpace(testutil.RunGit(t, clone, "rev-parse", "HEAD"))
	heads := map[string]string{"main": main}

	// develop is the default branch, as origin's HEAD says; it is never offered.
	heads["develop"] = branchAt(t, clone, "develop", main)
	pushed(t, clone, "develop", heads["develop"])
	testutil.RunGit(t, clone, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/develop")

	// At the merged head; behind it; with work beyond it; checked out; pushed.
	heads["feature-x"] = branchAt(t, clone, "feature-x", main)
	heads["behind"] = branchAt(t, clone, "behind", main)
	behindHead := commitOn(t, clone, heads["behind"], "the pull request's last commit")
	heads["extra-work"] = branchAt(t, clone, "extra-work", main)
	heads["busy"] = branchAt(t, clone, "busy", main)
	testutil.RunGit(t, clone, "worktree", "add", "-q", filepath.Join(e.home, "busy"), "busy")
	heads["pushed-x"] = branchAt(t, clone, "pushed-x", main)
	pushed(t, clone, "pushed-x", heads["pushed-x"])

	// The clone's own checkout: current shares main's files, so moving HEAD to it changes
	// none.
	heads["current"] = branchAt(t, clone, "current", main)
	testutil.RunGit(t, clone, "symbolic-ref", "HEAD", "refs/heads/current")

	// Protected, never merged, and a worktree's own branch left after its worktree went.
	heads["release/1"] = branchAt(t, clone, "release/1", main)
	heads["never-merged"] = branchAt(t, clone, "never-merged", main)
	heads["worktree-pr-9"] = branchAt(t, clone, "worktree-pr-9", main)

	key := func(branch string) BranchKey { return BranchKey{Repo: exampleRepo().Key(), Branch: branch} }
	merged := func(n int, head string) []MergedPR {
		return []MergedPR{{
			Number: n, Title: "Change " + head[:7], URL: "https://github.com/acme-org/example-repo/pull/x",
			MergedAt: mergedAt(), HeadOID: head,
		}}
	}

	gh := &fakeGitHub{merged: map[BranchKey][]MergedPR{
		key("feature-x"):  merged(184, heads["feature-x"]),
		key("behind"):     merged(185, behindHead),
		key("extra-work"): merged(186, strings.Repeat("1", 40)),
		key("busy"):       merged(187, heads["busy"]),
		key("pushed-x"):   merged(188, strings.Repeat("2", 40)),
		key("current"):    merged(189, heads["current"]),
		key("develop"):    merged(1, heads["develop"]),
		key("release/1"):  merged(2, heads["release/1"]),
	}}
	gh.set("example-repo", 9, pr(ItemMerged, heads["worktree-pr-9"]))

	rules := branchrules.Rules{Own: true, PullRequest: true, Merged: true, Protected: []string{"main", "release/*"}}
	svc := e.linked(linkDeps{gh: gh, rules: &rules})

	return mergedFixture{e: e, clone: clone, svc: svc, gh: gh, heads: heads}
}

func TestMergedBranchesFindsEachBranchWhosePullRequestMerged(t *testing.T) {
	t.Parallel()

	f := newMergedFixture(t)

	got, err := f.svc.MergedBranches(t.Context())
	require.NoError(t, err)

	type row struct {
		name      string
		number    int
		deletable bool
		unpushed  int
		reason    string
	}

	rows := make([]row, 0, len(got))
	for _, mb := range got {
		assert.Equal(t, f.clone, mb.RepoPath)
		assert.Equal(t, "~/projects/example-repo", mb.DisplayRepo)
		assert.Equal(t, BranchMerged, mb.Branch.Kind)
		assert.Equal(t, f.heads[mb.Branch.Name], mb.Branch.Head)
		assert.Equal(t, "acme-org/example-repo", mb.PullRequest.Repo)
		assert.Equal(t, "2026-11-06T18:00:00Z", mb.PullRequest.MergedAt)

		rows = append(rows, row{mb.Branch.Name, mb.PullRequest.Number, mb.Branch.Deletable, mb.Unpushed, mb.Branch.Reason})
	}

	assert.Equal(t, []row{
		{"behind", 185, true, 0, ""},
		{"busy", 187, false, 0, "it is checked out in ~/busy"},
		{"current", 189, false, 0, "it is the clone's current branch"},
		{"extra-work", 186, false, 1, "it has 1 commit that isn't in pull request #186 or on GitHub"},
		{"feature-x", 184, true, 0, ""},
		{"pushed-x", 188, true, 0, ""},
		{"worktree-pr-9", 9, true, 0, ""},
	}, rows, "the default branch, a protected one, and one never merged are left out")

	require.Len(t, f.gh.asked, 1, "one question to GitHub for the clone's branches")
	require.Len(t, f.gh.asked[0], 1)
	assert.Equal(t, exampleRepo(), f.gh.asked[0][0].Repo)
	assert.ElementsMatch(t, []string{"behind", "busy", "current", "extra-work", "feature-x", "never-merged", "pushed-x"},
		f.gh.asked[0][0].Branches, "a worktree-pr-<n> branch is asked about by its number")
}

func TestMergedBranchesNeedsGitHub(t *testing.T) {
	t.Parallel()

	f := newMergedFixture(t)

	_, err := f.e.linked(linkDeps{}).MergedBranches(t.Context())
	require.ErrorIs(t, err, ErrGitHubUnavailable)

	f.gh.err = errFakeGitHub

	_, err = f.svc.MergedBranches(t.Context())
	require.ErrorIs(t, err, ErrGitHubUnavailable)
	require.ErrorIs(t, err, errFakeGitHub)
}

func TestDeleteBranchesChecksEachAgain(t *testing.T) {
	t.Parallel()

	f := newMergedFixture(t)
	pick := func(name string) BranchPick { return BranchPick{RepoPath: f.clone, Name: name, Head: f.heads[name]} }

	moved := pick("behind")
	moved.Head = f.heads["feature-x"]

	got, err := f.svc.DeleteBranches(t.Context(), []BranchPick{
		pick("feature-x"),
		pick("worktree-pr-9"),
		moved,
		pick("release/1"),
		pick("develop"),
		pick("busy"),
		pick("current"),
		pick("feature-x"),
		{RepoPath: f.clone, Name: "gone", Head: f.heads["main"]},
		{RepoPath: f.clone, Name: "-x", Head: f.heads["main"]},
		{RepoPath: f.e.home, Name: "feature-x", Head: f.heads["feature-x"]},
	})
	require.NoError(t, err)

	outcomes := make([]string, 0, len(got))
	for _, res := range got {
		outcome := res.Reason
		if res.Deleted {
			outcome = "deleted"
		}

		outcomes = append(outcomes, res.RepoPath+" "+res.Name+": "+outcome)
	}

	assert.Equal(t, []string{
		f.clone + " feature-x: deleted",
		f.clone + " worktree-pr-9: deleted",
		f.clone + " behind: " + ErrBranchMoved.Error(),
		f.clone + " release/1: the branch is protected: it is protected in Settings",
		f.clone + " develop: the branch is protected: it is the repository's default branch",
		f.clone + " busy: " + ErrBranchInUse.Error(),
		f.clone + " current: " + ErrBranchInUse.Error(),
		f.clone + " feature-x: " + ErrBranchGone.Error(),
		f.clone + " gone: " + ErrBranchGone.Error(),
		f.clone + " -x: " + reasonOf(ErrInvalidRequest) + `: "-x" at "` + f.heads["main"] + `"`,
		f.e.home + " feature-x: " + reasonOf(ErrOutsideCloneRoot) + ": ~",
	}, outcomes, "each outcome in the order picked, a second pick of a deleted branch finding it gone")

	left := branches(t, f.clone)
	assert.NotContains(t, left, "feature-x")
	assert.NotContains(t, left, "worktree-pr-9")

	for _, kept := range []string{"behind", "release/1", "develop", "busy", "current", "main"} {
		assert.Contains(t, left, kept)
	}

	assert.Equal(t, f.heads["feature-x"], got[0].Head, "the result keeps the commit that restores the branch")
	assert.Equal(t, "~/projects/example-repo", got[0].DisplayRepo)
}

func TestDeleteBranchesDeletesABranchWithWorkNotOnGitHubAtTheCommitShown(t *testing.T) {
	t.Parallel()

	f := newMergedFixture(t)

	listed, err := f.svc.MergedBranches(t.Context())
	require.NoError(t, err)

	i := slices.IndexFunc(listed, func(mb MergedBranch) bool { return mb.Branch.Name == "extra-work" })
	require.GreaterOrEqual(t, i, 0)
	require.False(t, listed[i].Branch.Deletable)
	require.Equal(t, 1, listed[i].Unpushed, "kept only for its commit not on GitHub, so the user may delete it anyway")

	// Work added since it was shown is never lost: the pick is pinned to the commit shown.
	moved := commitOn(t, f.clone, f.heads["extra-work"], "more work")
	testutil.RunGit(t, f.clone, "update-ref", "refs/heads/extra-work", moved)

	pick := BranchPick{RepoPath: f.clone, Name: "extra-work", Head: listed[i].Branch.Head}
	got, err := f.svc.DeleteBranches(t.Context(), []BranchPick{pick})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.False(t, got[0].Deleted)
	assert.Equal(t, ErrBranchMoved.Error(), got[0].Reason)

	// Back at the commit shown, the user's confirmed pick deletes it, and names that commit.
	testutil.RunGit(t, f.clone, "update-ref", "refs/heads/extra-work", listed[i].Branch.Head)

	got, err = f.svc.DeleteBranches(t.Context(), []BranchPick{pick})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.True(t, got[0].Deleted, got[0].Reason)
	assert.Equal(t, listed[i].Branch.Head, got[0].Head, "the commit that brings it back")
	assert.NotContains(t, branches(t, f.clone), "extra-work")
}

func TestDeleteBranchesStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	f := newMergedFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	got, err := f.svc.DeleteBranches(ctx, []BranchPick{{RepoPath: f.clone, Name: "feature-x", Head: f.heads["feature-x"]}})
	require.ErrorIs(t, err, context.Canceled)
	require.Len(t, got, 1)
	assert.False(t, got[0].Deleted)
	assert.Equal(t, reasonOf(context.Canceled), got[0].Reason)
	assert.Contains(t, branches(t, f.clone), "feature-x")
}
