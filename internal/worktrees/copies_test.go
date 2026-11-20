package worktrees

import (
	"context"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/worklog"
)

// The words that start each of a comparison's git reads, as gitCalls counts them.
const (
	callUnpushed   = "log --no-show-signature --no-mailmap --max-count=10000"
	callCandidates = "log --no-show-signature --no-mailmap --no-merges --source"
	callSides      = "rev-list --left-right"
	parentBranch   = "feat/parent"
)

// authoredAt is when the rigs' agent commits were authored, a while before they are copied.
const authoredAt = int64(1780000000)

// copyRig is a clone of acme-org/example-repo whose main is pushed, with an agent's worktree
// under its .claude/worktrees on worktree-agent-1, and a worktree made by hand on
// feat/parent, which the agent's commits are copied onto and pushed from. Its Service keeps
// copy records in a store of its own, whose clock the test moves.
type copyRig struct {
	e      *env
	repo   string
	agent  string
	parent string
	main   string
	gh     *fakeGitHub
	run    *testutil.Runner
	svc    *Service
	log    *worklog.Store
	clock  *testutil.Clock
}

// newCopyRig makes a copyRig whose Service runs git through a recording runner, which
// answers the commands intercept takes itself.
func newCopyRig(t *testing.T, intercept func(dir string, sub []string) (reply, bool)) *copyRig {
	t.Helper()

	e := newEnv(t)
	r := &copyRig{e: e, repo: e.newGitHubRepo(t, exampleName), gh: &fakeGitHub{}}
	r.main = strings.TrimSpace(testutil.RunGit(t, r.repo, "rev-parse", "HEAD"))
	pushed(t, r.repo, mainBranch, r.main)
	testutil.RunGit(t, r.repo, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/"+mainBranch)

	r.agent = addWorktree(t, r.repo, filepath.Join(r.repo, ".claude", "worktrees", "agent-1"), ownBranchPrefix+"agent-1")

	// The parent's own work comes first, so a copy never comes out the very commit it copies,
	// as one made in the same second on the same commit would.
	r.parent = addWorktree(t, r.repo, filepath.Join(e.home, "wt", "parent"), parentBranch)
	commitAt(t, r.parent, "parent.txt", "parent\n", "Start the parent", authoredAt-1000)

	r.run = &testutil.Runner{Script: func(ctx context.Context, argv []string) ([]byte, []byte, error) {
		dir := ""
		if len(argv) > 2 && argv[1] == "-C" {
			dir = argv[2]
		}

		if intercept != nil {
			if answer, ok := intercept(dir, gitWords(argv)); ok {
				return []byte(answer.stdout), []byte(answer.stderr), answer.err
			}
		}

		return e.git.Run(ctx, argv[0], argv[1:]...)
	}}

	r.clock = testutil.NewClock(listedAt())
	r.log = worklog.New(testutil.DB(t), r.clock)
	r.svc = e.linked(linkDeps{gh: r.gh, copies: r.log})
	r.svc.run = r.run

	return r
}

// commitAt commits content to file in the worktree at dir, with subject as its message,
// authored at, and returns the new commit.
func commitAt(t *testing.T, dir, file, content, subject string, at int64) string {
	t.Helper()

	testutil.WriteFile(t, filepath.Join(dir, file), content)
	testutil.RunGit(t, dir, "add", file)
	testutil.RunGit(t, dir, "commit", "-q", "-m", subject, "--date", "@"+strconv.FormatInt(at, 10)+" +0000")

	return headOf(t, dir)
}

// headOf is the commit the worktree at dir has checked out.
func headOf(t *testing.T, dir string) string {
	t.Helper()

	return strings.TrimSpace(testutil.RunGit(t, dir, "rev-parse", "HEAD"))
}

// work makes n commits in the agent's worktree, each adding a file of its own, and returns
// them, oldest first.
func (r *copyRig) work(t *testing.T, n int) []string {
	t.Helper()

	oids := make([]string, 0, n)
	for i := range n {
		name := "agent-" + strconv.Itoa(i) + ".txt"
		oids = append(oids, commitAt(t, r.agent, name, name+"\n", "Add "+name, authoredAt+int64(i)))
	}

	return oids
}

// copyOnto cherry-picks oids onto the worktree at dir, in turn, and returns its new HEAD.
func copyOnto(t *testing.T, dir string, oids ...string) string {
	t.Helper()

	for _, oid := range oids {
		testutil.RunGit(t, dir, "cherry-pick", "--allow-empty", oid)
	}

	return headOf(t, dir)
}

// list lists the rig's worktrees and returns the agent's row.
func (r *copyRig) list(t *testing.T) Worktree {
	t.Helper()

	snap, err := r.svc.List(t.Context())
	require.NoError(t, err)

	return mustRow(t, snap, r.agent)
}

// copies counts the comparison's git reads the rig ran so far.
func (r *copyRig) copies() map[string]int {
	return gitCalls(r.run, callUnpushed, callCandidates, callSides)
}

func TestListSeesCommitsCherryPickedOntoAPushedBranch(t *testing.T) {
	t.Parallel()

	r := newCopyRig(t, nil)
	oids := r.work(t, 2)
	head := copyOnto(t, r.parent, oids...)
	pushed(t, r.repo, parentBranch, head)
	r.gh.setHead(parentBranch, headPR(199, ItemOpen, head, parentBranch, ownRepo))
	r.gh.set(exampleName, 199, headPR(199, ItemOpen, head, parentBranch, ownRepo).State)

	row := r.list(t)
	assert.Equal(t, Copies{Ref: "origin/" + parentBranch, Branch: parentBranch, Count: 2, Identical: 2, PR: 199}, row.Copies)
	assert.Equal(t, 2, row.Unpushed, "the count by commit ID is kept as it is")
	assert.Equal(t, LinkCopy, row.LinkedBy)
	require.NotNil(t, row.Item)
	assert.Equal(t, 199, row.Item.Number)
	assert.False(t, row.Item.Launched)
	assert.Equal(t, map[string]int{callUnpushed: 1, callCandidates: 1, callSides: 1}, r.copies(),
		"one log of the commits not on GitHub, one of the pushed ones, and one comparison")

	parent := mustRow(t, listOf(t, r.svc), r.parent)
	assert.Equal(t, LinkBranch, parent.LinkedBy, "the branch they were copied onto links by itself")
	assert.Zero(t, parent.Copies, "and has nothing to compare")
}

func TestListSeesCommitsCopiedOntoAStackOfBranches(t *testing.T) {
	t.Parallel()

	r := newCopyRig(t, nil)
	oids := r.work(t, 3)

	// feat/parent holds the copies of the first two, and feat/stacked, built on it, those and
	// the third's: the log credits the two shared copies to feat/parent alone.
	lower := copyOnto(t, r.parent, oids[0], oids[1])
	pushed(t, r.repo, parentBranch, lower)

	stacked := addWorktree(t, r.repo, filepath.Join(r.e.home, "wt", "stacked"), "feat/stacked")
	testutil.RunGit(t, stacked, "reset", "-q", "--hard", lower)
	upper := copyOnto(t, stacked, oids[2])
	pushed(t, r.repo, "feat/stacked", upper)

	credits := testutil.RunGit(t, r.repo, "log", "--source", "--format=%H %S", "--remotes", "--not", "refs/remotes/origin/"+mainBranch)
	require.Contains(t, credits, lower+" origin/"+parentBranch, "the stack's shared copies are credited to the lower branch")
	require.Contains(t, credits, upper+" origin/feat/stacked")

	r.gh.setHead(parentBranch, headPR(199, ItemOpen, lower, parentBranch, ownRepo))
	r.gh.setHead("feat/stacked", headPR(200, ItemOpen, upper, "feat/stacked", ownRepo))
	r.gh.set(exampleName, 199, headPR(199, ItemOpen, lower, parentBranch, ownRepo).State)
	r.gh.set(exampleName, 200, headPR(200, ItemOpen, upper, "feat/stacked", ownRepo).State)

	row := r.list(t)
	assert.Equal(t, Copies{Ref: "origin/feat/stacked", Branch: "feat/stacked", Count: 3, Identical: 3, PR: 200}, row.Copies,
		"every copy on the upper branch counts there, wherever the log credited it")
	assert.Equal(t, LinkCopy, row.LinkedBy)
	require.NotNil(t, row.Item)
	assert.Equal(t, 200, row.Item.Number, "linked to the pull request of the branch that holds them all")
	assert.Equal(t, 2, r.copies()[callSides], "the lower branch, credited with more, is compared first")

	again := r.list(t)
	assert.Equal(t, row.Copies, again.Copies)
	assert.Equal(t, map[string]int{callUnpushed: 1, callCandidates: 1, callSides: 2}, r.copies(),
		"and a listing after which nothing changed runs no git to compare")
}

func TestListCountsCopiesTheLogCreditsToARemotesHead(t *testing.T) {
	t.Parallel()

	r := newCopyRig(t, nil)
	testutil.RunGit(t, r.repo, "remote", "add", "upstream", "https://github.com/upstream-org/example-repo.git")
	oids := r.work(t, 3)

	// upstream's default branch, which no copy is looked for on, holds the copies of the
	// first two, and feat/stacked, built on it, those and the third's: the log credits the two
	// to upstream's HEAD, which names that branch.
	lower := copyOnto(t, r.parent, oids[0], oids[1])
	testutil.RunGit(t, r.repo, "update-ref", "refs/remotes/upstream/main", lower)
	testutil.RunGit(t, r.repo, "symbolic-ref", "refs/remotes/upstream/HEAD", "refs/remotes/upstream/main")

	stacked := addWorktree(t, r.repo, filepath.Join(r.e.home, "wt", "stacked"), "feat/stacked")
	testutil.RunGit(t, stacked, "reset", "-q", "--hard", lower)
	upper := copyOnto(t, stacked, oids[2])
	pushed(t, r.repo, "feat/stacked", upper)

	credits := testutil.RunGit(t, r.repo, "log", "--source", "--format=%H %S", "--remotes", "--not", "refs/remotes/origin/"+mainBranch)
	require.Contains(t, credits, lower+" upstream/HEAD")

	r.gh.setHead("feat/stacked", headPR(200, ItemOpen, upper, "feat/stacked", ownRepo))

	row := r.list(t)
	assert.Equal(t, Copies{Ref: "origin/feat/stacked", Branch: "feat/stacked", Count: 3, Identical: 3, PR: 200}, row.Copies,
		"the copies the log credits to a ref no copy is looked for on still count on a branch that holds them")
	assert.Equal(t, LinkCopy, row.LinkedBy)
	assert.Equal(t, 1, r.copies()[callSides], "and only the pushed branch is compared with")
}

// listOf lists svc's worktrees.
func listOf(t *testing.T, svc *Service) Snapshot {
	t.Helper()

	snap, err := svc.List(t.Context())
	require.NoError(t, err)

	return snap
}

func TestListSeesCommitsRebasedOntoAMovedBase(t *testing.T) {
	t.Parallel()

	r := newCopyRig(t, nil)

	// A file of many lines, which the agent changes at its top, and main, meanwhile, at its
	// bottom: the rebased copy holds other files, but the same change.
	lines := make([]string, 30)
	for i := range lines {
		lines[i] = "line " + strconv.Itoa(i)
	}

	base := commitAt(t, r.repo, "notes.txt", strings.Join(lines, "\n")+"\n", "Add the notes", authoredAt-100)
	pushed(t, r.repo, mainBranch, base)
	testutil.RunGit(t, r.agent, "reset", "-q", "--hard", base)

	lines[0] = "the agent's line"
	work := commitAt(t, r.agent, "notes.txt", strings.Join(lines, "\n")+"\n", "Change the top", authoredAt)

	lines[0] = "line 0"
	lines[29] = "main's line"
	moved := commitAt(t, r.repo, "notes.txt", strings.Join(lines, "\n")+"\n", "Change the bottom", authoredAt+50)
	pushed(t, r.repo, mainBranch, moved)

	testutil.RunGit(t, r.parent, "reset", "-q", "--hard", moved)
	head := copyOnto(t, r.parent, work)
	pushed(t, r.repo, parentBranch, head)

	row := r.list(t)
	assert.Equal(t, Copies{Ref: "origin/" + parentBranch, Branch: parentBranch, Count: 1, Identical: 1}, row.Copies,
		"the same change, though the files around it differ")
}

func TestListTellsAnEditedCopyApart(t *testing.T) {
	t.Parallel()

	r := newCopyRig(t, nil)
	oids := r.work(t, 2)
	copyOnto(t, r.parent, oids...)

	// The second copy amended: the same author, time, and subject, another change.
	testutil.WriteFile(t, filepath.Join(r.parent, "agent-1.txt"), "edited\n")
	testutil.RunGit(t, r.parent, "commit", "-q", "-a", "--amend", "--no-edit")
	head := headOf(t, r.parent)
	pushed(t, r.repo, parentBranch, head)
	r.gh.setHead(parentBranch, headPR(199, ItemOpen, head, parentBranch, ownRepo))

	row := r.list(t)
	assert.Equal(t, Copies{Ref: "origin/" + parentBranch, Branch: parentBranch, Count: 2, Identical: 1, Edited: 1, PR: 199}, row.Copies)
	assert.Nil(t, row.Item, "an edited copy is no copy: the worktree isn't linked by it")
	assert.Empty(t, row.LinkedBy)
}

func TestListCountsAWorktreeCopiedInPart(t *testing.T) {
	t.Parallel()

	r := newCopyRig(t, nil)
	oids := r.work(t, 3)
	head := copyOnto(t, r.parent, oids[0])
	pushed(t, r.repo, parentBranch, head)

	row := r.list(t)
	assert.Equal(t, Copies{Ref: "origin/" + parentBranch, Branch: parentBranch, Count: 3, Identical: 1, None: 2}, row.Copies)
	assert.Nil(t, row.Item)
}

func TestListLeavesCopiesSpreadOverTwoBranchesUncounted(t *testing.T) {
	t.Parallel()

	r := newCopyRig(t, nil)
	oids := r.work(t, 2)
	pushed(t, r.repo, parentBranch, copyOnto(t, r.parent, oids[0]))

	other := addWorktree(t, r.repo, filepath.Join(r.e.home, "wt", "other"), "feat/other")
	pushed(t, r.repo, "feat/other", copyOnto(t, other, oids[1]))

	row := r.list(t)
	assert.Equal(t, 2, row.Copies.Count)
	assert.Equal(t, 1, row.Copies.Identical)
	assert.Contains(t, row.Copies.Note, "no one branch has them all")
	assert.False(t, row.Copies.full())
	assert.Equal(t, 2, r.copies()[callSides], "both branches were compared")
}

func TestListComparesNoMerge(t *testing.T) {
	t.Parallel()

	r := newCopyRig(t, nil)
	oids := r.work(t, 1)

	side := addWorktree(t, r.repo, filepath.Join(r.e.home, "wt", "side"), "side")
	commitAt(t, side, "side.txt", "side\n", "Add the side", authoredAt)
	testutil.RunGit(t, r.agent, "merge", "-q", "--no-ff", "-m", "Merge side", "side")

	pushed(t, r.repo, parentBranch, copyOnto(t, r.parent, oids...))

	row := r.list(t)
	assert.Equal(t, 3, row.Copies.Count)
	assert.Contains(t, row.Copies.Note, "merge")
	assert.Zero(t, row.Copies.Identical)
	assert.Zero(t, r.copies()[callSides], "nothing is compared")
}

func TestListComparesADetachedWorktree(t *testing.T) {
	t.Parallel()

	r := newCopyRig(t, nil)
	oids := r.work(t, 2)
	testutil.RunGit(t, r.agent, "switch", "-q", "--detach")

	head := copyOnto(t, r.parent, oids...)
	pushed(t, r.repo, parentBranch, head)
	r.gh.setHead(parentBranch, headPR(199, ItemOpen, head, parentBranch, ownRepo))

	row := r.list(t)
	assert.True(t, row.Detached)
	assert.True(t, row.Copies.full())
	assert.Equal(t, LinkCopy, row.LinkedBy)
}

func TestListComparesAtMostTwoHundredCommits(t *testing.T) {
	t.Parallel()

	var agent string

	r := newCopyRig(t, func(dir string, sub []string) (reply, bool) {
		if paths.Same(dir, agent) && startsWith(sub, "rev-list --count") {
			return reply{stdout: "201\n"}, true
		}

		return reply{}, false
	})
	agent = r.agent
	oids := r.work(t, 1)
	pushed(t, r.repo, parentBranch, copyOnto(t, r.parent, oids...))

	row := r.list(t)
	assert.Equal(t, 201, row.Unpushed)
	assert.Equal(t, 201, row.Copies.Count)
	assert.Contains(t, row.Copies.Note, "more than 200")
	assert.Equal(t, map[string]int{}, r.copies(), "no git is run for it")
}

func TestListLeavesALinkToAForksBranchOfTheSameNameOut(t *testing.T) {
	t.Parallel()

	r := newCopyRig(t, nil)
	oids := r.work(t, 1)
	head := copyOnto(t, r.parent, oids...)
	pushed(t, r.repo, parentBranch, head)
	r.gh.setHead(parentBranch, headPR(201, ItemOpen, head, parentBranch, "octo-fork/example-repo"))

	row := r.list(t)
	assert.True(t, row.Copies.full(), "fully copied onto the branch")
	assert.Nil(t, row.Item, "but a fork's pull request of the branch's name is not its")
}

func TestListComparesOnlyBranchesPushedToGitHub(t *testing.T) {
	t.Parallel()

	r := newCopyRig(t, nil)
	testutil.RunGit(t, r.repo, "remote", "add", "mirror", "https://gitlab.example.com/acme-org/example-repo.git")

	oids := r.work(t, 1)
	head := copyOnto(t, r.parent, oids...)
	testutil.RunGit(t, r.repo, "update-ref", "refs/remotes/mirror/"+parentBranch, head)
	pushed(t, r.repo, "feat/unrelated", r.main)

	row := r.list(t)
	assert.Equal(t, Copies{Count: 1, None: 1}, row.Copies, "a copy on a remote that isn't GitHub isn't looked at")
	assert.Zero(t, r.copies()[callSides])

	// Merged into the default branch by a rebase: the default branch isn't compared with.
	pushed(t, r.repo, mainBranch, head)

	row = r.list(t)
	assert.Equal(t, 1, row.Unpushed, "main has the copy, not the commit")
	assert.Equal(t, Copies{Count: 1, None: 1}, row.Copies)
	assert.Zero(t, r.copies()[callSides])
}

func TestListLeavesWhatGitCantCompareUncopied(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		answer reply
		want   string
	}{
		{
			name:   "a missing object",
			answer: reply{stderr: "fatal: missing blob object 'abc1234'", err: exitError{code: 128}},
			want:   "Couldn't compare its commits with the pushed branches. Missing blob object 'abc1234'.",
		},
		{
			name:   "a warning",
			answer: reply{stdout: ">" + headSHA + "\n", stderr: "warning: something odd about " + headSHA},
			want:   "Couldn't compare its commits with the pushed branches. Warning: something odd about " + headSHA + ".",
		},
		{
			name:   "a commit left out",
			answer: reply{stdout: ">" + headSHA + "\n"},
			want:   "Couldn't compare its commits with the pushed branches.",
		},
		{
			name:   "an answer git never gives",
			answer: reply{stdout: "=" + headSHA + "\n"},
			want:   "Couldn't compare its commits with the pushed branches.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newCopyRig(t, func(_ string, sub []string) (reply, bool) {
				return tt.answer, startsWith(sub, callSides)
			})
			oids := r.work(t, 1)
			head := copyOnto(t, r.parent, oids...)
			pushed(t, r.repo, parentBranch, head)
			r.gh.setHead(parentBranch, headPR(199, ItemOpen, head, parentBranch, ownRepo))

			row := r.list(t)
			assert.Equal(t, Copies{Count: 1, Note: tt.want}, row.Copies)
			assert.Nil(t, row.Item)

			r.list(t)
			assert.Equal(t, 1, r.copies()[callSides], "what git answered is kept until something changes")
		})
	}
}

func TestListComparesAgainOnlyWhenSomethingChanged(t *testing.T) {
	t.Parallel()

	r := newCopyRig(t, nil)
	oids := r.work(t, 2)
	head := copyOnto(t, r.parent, oids...)
	pushed(t, r.repo, parentBranch, head)
	r.gh.setHead(parentBranch, headPR(199, ItemOpen, head, parentBranch, ownRepo))
	r.gh.set(exampleName, 199, headPR(199, ItemOpen, head, parentBranch, ownRepo).State)

	first := r.list(t)
	require.True(t, first.Copies.full())

	second := r.list(t)
	assert.Equal(t, first.Copies, second.Copies)
	assert.Equal(t, LinkCopy, second.LinkedBy)
	assert.Equal(t, map[string]int{callUnpushed: 1, callCandidates: 1, callSides: 1}, r.copies(),
		"a listing in which nothing changed runs no git to compare")

	calls, withHeads := r.gh.requests()
	assert.Equal(t, 2, calls)
	assert.Equal(t, 1, withHeads, "and asks GitHub about no branch again, only its pull request's state")
	assert.Equal(t, []string{refKey(exampleRepo().Owner, exampleName, 199)}, refKeys(r.gh.calls[1]))

	// A fetch that moves any pushed branch compares again.
	pushed(t, r.repo, "feat/elsewhere", r.main)
	r.list(t)
	assert.Equal(t, 2, r.copies()[callUnpushed])

	// A worktree that is gone is forgotten.
	testutil.RunGit(t, r.repo, "worktree", "remove", "--force", r.agent)
	listOf(t, r.svc)
	kept := r.svc.copies.has(paths.Resolved(r.agent))
	assert.False(t, kept)
}

func TestListComparesEveryWorktreeOfAClonesInOneLog(t *testing.T) {
	t.Parallel()

	r := newCopyRig(t, nil)
	oids := r.work(t, 1)

	second := addWorktree(t, r.repo, filepath.Join(r.repo, ".claude", "worktrees", "agent-2"), ownBranchPrefix+"agent-2")
	more := commitAt(t, second, "second.txt", "second\n", "Add the second", authoredAt+10)

	head := copyOnto(t, r.parent, oids[0], more)
	pushed(t, r.repo, parentBranch, head)

	snap := listOf(t, r.svc)
	assert.True(t, mustRow(t, snap, r.agent).Copies.full())
	assert.True(t, mustRow(t, snap, second).Copies.full())
	assert.Equal(t, map[string]int{callUnpushed: 1, callCandidates: 1, callSides: 2}, r.copies(),
		"one log of each kind for the clone, and a comparison for each worktree")
}

func TestListDropsWhatTheCopyCacheKeepsForClonesItNoLongerLists(t *testing.T) {
	t.Parallel()

	r, _, _ := copiedRig(t)
	r.list(t)

	// A clone that was listed once, since gone from the clone root.
	gone := filepath.Join(r.e.home, "projects", "gone-repo")
	r.svc.copies.keep(gone, filepath.Join(gone, "wt"), copyKey{head: headSHA}, copyResult{})
	r.svc.copies.keepCoverage(gone, filepath.Join(gone, "wt"), coverKey{}, true)

	r.list(t)

	kept := r.svc.copies.has(filepath.Join(gone, "wt"))
	assert.False(t, kept, "a listing of every clone drops what it keeps for one it didn't list")

	_, kept = r.svc.copies.coverage(filepath.Join(gone, "wt"), coverKey{})
	assert.False(t, kept)

	kept = r.svc.copies.has(paths.Resolved(r.agent))
	assert.True(t, kept, "and keeps what it keeps for the clones it listed")

	// A listing of the one clone alone drops nothing of the others'.
	r.svc.copies.keep(gone, filepath.Join(gone, "wt"), copyKey{head: headSHA}, copyResult{})

	_, err := r.svc.Details(t.Context(), r.repo, r.agent)
	require.NoError(t, err)

	kept = r.svc.copies.has(filepath.Join(gone, "wt"))
	assert.True(t, kept)
}

func TestCopyCacheStartsOverWhenFull(t *testing.T) {
	t.Parallel()

	c := newCopyCache()

	for i := range worktreesMax {
		path := "/wt/" + strconv.Itoa(i)
		c.keep("/clone", path, copyKey{}, copyResult{})
		c.keepCoverage("/clone", path, coverKey{}, true)
	}

	c.keep("/clone", "/wt/0", copyKey{head: headSHA}, copyResult{})
	assert.Len(t, c.entries, worktreesMax, "a worktree kept again takes no more room")

	c.keep("/clone", "/wt/more", copyKey{}, copyResult{})
	c.keepCoverage("/clone", "/wt/more", coverKey{}, true)
	assert.Len(t, c.entries, 1, "a full cache starts over")
	assert.Len(t, c.covers, 1)
}

// markedRig is a copyRig whose agent made three commits: the first copied as it is onto
// feat/parent, the second copied and edited, and the third not copied. It returns the
// agent's commits and their copies, oldest first.
func markedRig(t *testing.T) (r *copyRig, oids, copies []string) {
	t.Helper()

	r = newCopyRig(t, nil)
	oids = r.work(t, 3)

	same := copyOnto(t, r.parent, oids[0])
	copyOnto(t, r.parent, oids[1])
	testutil.WriteFile(t, filepath.Join(r.parent, "agent-1.txt"), "edited\n")
	testutil.RunGit(t, r.parent, "commit", "-q", "-a", "--amend", "--no-edit")
	edited := headOf(t, r.parent)
	pushed(t, r.repo, parentBranch, edited)

	return r, oids, []string{same, edited}
}

// refsCall is the words that start the read of a clone's refs, as startsWith matches them.
func refsCall() string {
	return strings.Join(refsArgs()[:2], " ")
}

// marksOf maps each commit of d to its mark and its copy.
func marksOf(d Details) map[string]string {
	out := map[string]string{}
	for _, c := range d.Commits {
		out[c.OID] = string(c.Copy) + " " + c.CopyOID
	}

	return out
}

func TestDetailsMarkEachCommitsCopyAsTheListingFoundIt(t *testing.T) {
	t.Parallel()

	r, oids, copies := markedRig(t)

	row := r.list(t)
	assert.Equal(t, Copies{Ref: "origin/" + parentBranch, Branch: parentBranch, Count: 3, Identical: 1, Edited: 1, None: 1}, row.Copies)

	before := len(r.run.Calls())

	d, err := r.svc.Details(t.Context(), r.repo, r.agent)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		oids[0]: "identical " + copies[0], oids[1]: "edited " + copies[1], oids[2]: " ",
	}, marksOf(d))

	var refsReads, compares, remotes int

	for _, argv := range r.run.Calls()[before:] {
		sub := gitWords(argv)

		switch {
		case startsWith(sub, refsCall()):
			refsReads++
		case startsWith(sub, callUnpushed), startsWith(sub, callCandidates), startsWith(sub, callSides):
			compares++
		case startsWith(sub, "config"):
			remotes++
		}
	}

	assert.Equal(t, []int{1, 0, 1}, []int{refsReads, compares, remotes},
		"one read of the refs and one of the remotes say nothing changed since the listing, which compared them")
}

func TestDetailsCompareTheCommitsWhenTheListingDidntForTheirState(t *testing.T) {
	t.Parallel()

	r, oids, copies := markedRig(t)

	d, err := r.svc.Details(t.Context(), r.repo, r.agent)
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		oids[0]: "identical " + copies[0], oids[1]: "edited " + copies[1], oids[2]: " ",
	}, marksOf(d))
	assert.Equal(t, map[string]int{callUnpushed: 1, callCandidates: 1, callSides: 1}, r.copies())

	assert.Equal(t, Copies{Ref: "origin/" + parentBranch, Branch: parentBranch, Count: 3, Identical: 1, Edited: 1, None: 1}, r.list(t).Copies)
	assert.Equal(t, 1, r.copies()[callSides], "and the listing after them finds what they found")
}

func TestDetailsMarkNothingWhenTheRefsCantBeRead(t *testing.T) {
	t.Parallel()

	r, _, _ := markedRig(t)
	r.list(t)

	r.svc.run = scriptedGit(r.e.git, func(sub []string) (reply, bool) {
		return reply{stderr: "fatal: bad ref", err: exitError{code: 128}}, startsWith(sub, refsCall())
	})

	d, err := r.svc.Details(t.Context(), r.repo, r.agent)
	require.NoError(t, err)
	require.Len(t, d.Commits, 3)

	for _, c := range d.Commits {
		assert.Empty(t, c.Copy, "a copy is never claimed without the refs that say where things stand")
		assert.Empty(t, c.CopyOID)
	}

	assert.Equal(t, CopyComparison{Note: "Couldn't compare its commits with the pushed branches."}, d.Copies,
		"not compared, which says nothing of whether the copies are there")
	assert.Empty(t, d.Errors, "and no read the drawer shows failed")
}

func TestDetailsSayHowTheirComparisonWent(t *testing.T) {
	t.Parallel()

	r, _, _ := markedRig(t)
	r.list(t)

	d, err := r.svc.Details(t.Context(), r.repo, r.agent)
	require.NoError(t, err)
	assert.Equal(t, CopyComparison{Compared: true, Ref: "origin/" + parentBranch, Branch: parentBranch, Identical: 1, Edited: 1}, d.Copies)

	// The branch is deleted: compared again, nothing is on a pushed branch any more.
	testutil.RunGit(t, r.repo, "update-ref", "-d", "refs/remotes/origin/"+parentBranch)

	d, err = r.svc.Details(t.Context(), r.repo, r.agent)
	require.NoError(t, err)
	assert.Equal(t, CopyComparison{Compared: true}, d.Copies, "compared, with nothing found: no copy is pushed now")

	for _, c := range d.Commits {
		assert.Empty(t, c.Copy)
	}
}

func TestDetailsCompareAgainOnceTheRemotesChanged(t *testing.T) {
	t.Parallel()

	r, oids, _ := markedRig(t)
	require.Equal(t, 1, r.list(t).Copies.Identical)

	// origin now points somewhere that isn't GitHub, its refs as they were.
	testutil.RunGit(t, r.repo, "remote", "set-url", originRemote, "https://gitlab.example.com/acme-org/example-repo.git")

	d, err := r.svc.Details(t.Context(), r.repo, r.agent)
	require.NoError(t, err)
	assert.Equal(t, CopyComparison{Compared: true}, d.Copies,
		"compared again, as the listing's result was for other remotes: no branch of a GitHub remote holds a copy now")
	assert.Equal(t, map[string]string{oids[0]: " ", oids[1]: " ", oids[2]: " "}, marksOf(d))
}

func TestDetailsSayWhyTheirComparisonCouldntRun(t *testing.T) {
	t.Parallel()

	for name, tt := range map[string]struct {
		answer func(sub []string) (reply, bool)
		note   string
	}{
		"git refuses the comparison": {
			answer: func(sub []string) (reply, bool) {
				return reply{stderr: "fatal: missing blob object", err: exitError{code: 128}}, startsWith(sub, callSides)
			},
			note: "Couldn't compare its commits with the pushed branches. Missing blob object.",
		},
		"too many commits": {
			answer: func(sub []string) (reply, bool) { return reply{stdout: "201\n"}, startsWith(sub, "rev-list --count") },
			note:   tooManyToCompare(),
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r, _, _ := markedRig(t)
			r.svc.run = scriptedGit(r.e.git, tt.answer)

			d, err := r.svc.Details(t.Context(), r.repo, r.agent)
			require.NoError(t, err)
			assert.Equal(t, CopyComparison{Note: tt.note}, d.Copies)
		})
	}
}

func TestListComparesNoBranchOfARemoteNamedLikeOrigins(t *testing.T) {
	t.Parallel()

	r := newCopyRig(t, nil)
	addSlashedMirror(t, r.repo)

	oids := r.work(t, 1)
	head := copyOnto(t, r.parent, oids...)
	testutil.RunGit(t, r.repo, "update-ref", "refs/remotes/origin/mirror/"+parentBranch, head)
	pushed(t, r.repo, "feat/unrelated", r.main)

	row := r.list(t)
	assert.Equal(t, Copies{Count: 1, None: 1}, row.Copies,
		"refs/remotes/origin/mirror/feat/parent is the mirror's, which isn't GitHub, not origin's mirror/feat/parent")
	assert.False(t, row.OnGitHub)
	assert.Zero(t, r.copies()[callSides])
}

func TestAnOldWorktreeNeverKeepsTheOthersOfItsCloneFromBeingCompared(t *testing.T) {
	t.Parallel()

	// A worktree left for two months, whose window of pushed commits is too wide to read.
	const longAgo = authoredAt - 60*86400

	tooMany := strings.Repeat(otherSHA+"\x00origin/elsewhere\x001\x00octo-user@example.com\x00Old\n", copyCandidatesMax)
	oldSince := "--since=@" + strconv.FormatInt(longAgo-copyWindow, 10)

	r := newCopyRig(t, func(_ string, sub []string) (reply, bool) {
		return reply{stdout: tooMany}, startsWith(sub, callCandidates) && slices.Contains(sub, oldSince)
	})

	old := filepath.Join(r.repo, ".claude", "worktrees", "agent-old")
	testutil.RunGit(t, r.repo, "worktree", "add", "-q", "-b", ownBranchPrefix+"agent-old", old, r.main)
	commitAt(t, old, "old.txt", "old\n", "Add old", longAgo)

	oids := r.work(t, 2)
	r.publish(t, copyOnto(t, r.parent, oids...))

	snap := listOf(t, r.svc)
	assert.True(t, mustRow(t, snap, r.agent).Copies.full(), "the fresh worktree is compared all the same")

	got := mustRow(t, snap, old).Copies
	assert.Equal(t, 1, got.Count)
	assert.Equal(t, "Couldn't compare its commits with the pushed branches. Too many pushed commits to compare with.",
		got.Note, "only the old one, whose window was too wide, isn't")
	assert.Equal(t, 2, r.copies()[callCandidates], "the wide window's log, then the next one's")
}

func TestListComparesNothingInAClonePushedNowhere(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	alpha := e.newRepo(t, "alpha")
	feature := addWorktree(t, alpha, filepath.Join(alpha, ".claude", "worktrees", "feature"), "worktree-feature")
	testutil.Commit(t, feature, "x.txt", "x\n")

	run := scriptedGit(e.git, func([]string) (reply, bool) { return reply{}, false })

	snap, err := e.service(run).List(t.Context())
	require.NoError(t, err)
	assert.Zero(t, mustRow(t, snap, feature).Copies)
	assert.Equal(t, map[string]int{}, gitCalls(run, callUnpushed, callCandidates, callSides))
}

func TestReachFollowsParentsWithinTheSet(t *testing.T) {
	t.Parallel()

	set := map[string]copyCommit{
		"c": {oid: "c", parents: []string{"b"}},
		"b": {oid: "b", parents: []string{"a", "x"}},
		"a": {oid: "a", parents: []string{"pushed"}},
		"x": {oid: "x"},
		"d": {oid: "d", parents: []string{"a"}},
	}

	got := make([]string, 0, len(set))
	for _, c := range reach(set, "c") {
		got = append(got, c.oid)
	}

	assert.ElementsMatch(t, []string{"c", "b", "a", "x"}, got, "not d, which c doesn't reach, nor what the set lacks")
	assert.Empty(t, reach(set, "gone"))
}

func TestRankRefsKeepsTheBranchesWithTheMostLikelyCopies(t *testing.T) {
	t.Parallel()

	likely := map[string]map[string][]string{
		"refs/remotes/origin/b":    {"1": nil, "2": nil},
		"refs/remotes/origin/a":    {"1": nil, "2": nil},
		"refs/remotes/origin/c":    {"1": nil, "2": nil, "3": nil},
		"refs/remotes/origin/d":    {"1": nil},
		"refs/remotes/origin/HEAD": {"1": nil, "2": nil, "3": nil, "4": nil},
	}
	branches := map[string]bool{
		"refs/remotes/origin/a": true, "refs/remotes/origin/b": true, "refs/remotes/origin/c": true, "refs/remotes/origin/d": true,
	}

	assert.Equal(t, []string{"refs/remotes/origin/c", "refs/remotes/origin/a", "refs/remotes/origin/b"}, rankRefs(likely, branches),
		"a ref copies aren't looked for on, as a remote's HEAD, is never compared with")
}

func TestCompareFailureWordsWhatWentWrong(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Couldn't compare its commits with the pushed branches. Too many pushed commits to compare with.",
		compareFailure(errTooManyCandidates))
	assert.Equal(t, "Couldn't compare its commits with the pushed branches.", compareFailure(errFakeStart))

	w := &copyWork{wt: &Worktree{Unpushed: 1}}
	w.fail(t.Context(), &copyGitError{err: errFakeStart})
	assert.True(t, w.retry, "git that couldn't be run is tried again")

	w.fail(t.Context(), &copyGitError{err: exitError{code: 128}})
	assert.False(t, w.retry, "a refusal is kept")

	w.fail(t.Context(), &copyGitError{err: errGitWarned})
	assert.False(t, w.retry, "and so is a warning")
}
