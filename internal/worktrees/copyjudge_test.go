package worktrees

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/worklog"
)

// ownAgentBranch is the agent's own branch in the copy rigs.
const ownAgentBranch = ownBranchPrefix + "agent-1"

// merge has pull request #199 merged at head, as GitHub reports it by number and by its
// branch.
func (r *copyRig) merge(head string) {
	r.gh.set(exampleName, 199, headPR(199, ItemMerged, head, parentBranch, ownRepo).State)
	r.gh.setHead(parentBranch, headPR(199, ItemMerged, head, parentBranch, ownRepo))
}

// squashAndDelete merges feat/parent into main as one new commit, as a squash merge does,
// and deletes it from the remote, as the clone's next fetch with pruning finds.
func (r *copyRig) squashAndDelete(t *testing.T) {
	t.Helper()

	for _, name := range []string{"parent.txt", "agent-0.txt", "agent-1.txt"} {
		testutil.WriteFile(t, filepath.Join(r.repo, name), name+"\n")
	}

	testutil.RunGit(t, r.repo, "add", ".")
	testutil.RunGit(t, r.repo, "commit", "-q", "-m", "Describe it (#199)")
	pushed(t, r.repo, mainBranch, headOf(t, r.repo))
	testutil.RunGit(t, r.repo, "update-ref", "-d", "refs/remotes/origin/"+parentBranch)
}

// agentBranch returns the row's own branch, the agent's, which it must offer.
func agentBranch(t *testing.T, wt Worktree) Branch {
	t.Helper()

	i := slices.IndexFunc(wt.Branches, func(b Branch) bool { return b.Name == ownAgentBranch })
	require.GreaterOrEqual(t, i, 0, "no branch %s among %+v", ownAgentBranch, wt.Branches)

	return wt.Branches[i]
}

func TestACopiedWorktreeIsOnGitHubWhileItsPullRequestIsOpen(t *testing.T) {
	t.Parallel()

	r, _, _ := copiedRig(t)

	row := r.list(t)
	assert.Equal(t, 2, row.Unpushed)
	assert.True(t, row.OnGitHub, "every change is on GitHub, on the pull request's branch")
	assert.False(t, row.Copies.Recorded)
	assert.Equal(t, StageActive, row.Stage)
	assert.False(t, row.CanCleanUp)
	assert.True(t, row.CanRemove, "a removal is safe to offer")
	assert.False(t, autoCleanable(row))

	b := agentBranch(t, row)
	assert.False(t, b.Deletable, "its branch stays while the pull request is open")
	assert.Equal(t, "its commits are on GitHub only as copies, in pull request #199, which hasn't merged with them", b.Reason)
}

func TestACopiedWorktreeIsCleanedUpOnceItsPullRequestMerges(t *testing.T) {
	t.Parallel()

	r, _, tip := copiedRig(t)
	r.merge(tip)

	row := r.list(t)
	assert.True(t, row.OnGitHub)
	assert.Equal(t, StageFinished, row.Stage)
	assert.True(t, row.CanCleanUp)
	assert.True(t, autoCleanable(row), "the record's pull request merged with every copy")
	assert.True(t, agentBranch(t, row).Deletable)

	sweeper := r.e.linked(linkDeps{gh: r.gh, copies: r.log, auto: true})
	sweeper.run = r.run

	report := sweeper.Sweep(t.Context())
	require.Empty(t, report.Failed)
	require.Len(t, report.Cleaned, 1)
	assert.True(t, samePath(r.agent, report.Cleaned[0].Path))
	assert.Equal(t, []string{ownAgentBranch}, report.Cleaned[0].DeletedBranches)
	assert.NoDirExists(t, r.agent)
	assert.DirExists(t, r.parent, "the worktree made by hand on the pull request's branch is left for its user")
	assert.NotContains(t, branches(t, r.repo), ownAgentBranch)
	assert.Empty(t, r.records(t), "and the record goes with it")
}

func TestASquashMergeAndTheBranchsDeletionKeepTheCopiesOnGitHub(t *testing.T) {
	t.Parallel()

	r, _, tip := copiedRig(t)
	r.list(t)
	require.Len(t, r.records(t), 1)

	r.merge(tip)
	r.squashAndDelete(t)

	before := len(r.run.Calls())

	row := r.list(t)
	assert.Equal(t, 2, row.Unpushed)
	assert.True(t, row.OnGitHub, "the merged pull request's head has every copy")
	assert.Equal(t, Copies{Ref: "origin/" + parentBranch, Branch: parentBranch, Count: 2, Identical: 2, Recorded: true, PR: 199, Gone: true}, row.Copies)
	assert.Equal(t, LinkCopy, row.LinkedBy, "linked by its record, its branch gone")
	require.NotNil(t, row.Item)
	assert.Equal(t, 199, row.Item.Number)
	assert.Equal(t, StageFinished, row.Stage)
	assert.True(t, autoCleanable(row))
	assert.True(t, agentBranch(t, row).Deletable)

	coverage := 0

	for _, argv := range r.run.Calls()[before:] {
		if startsWith(gitWords(argv), callUnpushed+" "+copyUnpushedCmd()[4]+" --ignore-missing") {
			coverage++
		}
	}

	assert.Equal(t, 1, coverage, "one log tells which commits the record covers")

	// A listing in which nothing changed runs no git to tell, and asks GitHub about the
	// pull request alone.
	before = len(r.run.Calls())
	calls, withHeads := r.gh.requests()

	assert.True(t, r.list(t).OnGitHub)

	for _, argv := range r.run.Calls()[before:] {
		sub := gitWords(argv)
		assert.False(t, startsWith(sub, callUnpushed) || startsWith(sub, callCandidates) || startsWith(sub, callSides) ||
			startsWith(sub, "merge-base"), "ran %v", sub)
	}

	after, afterHeads := r.gh.requests()
	assert.Equal(t, calls+1, after)
	assert.Equal(t, withHeads, afterHeads)
	assert.Equal(t, []string{refKey(exampleRepo().Owner, exampleName, 199)}, refKeys(r.gh.calls[len(r.gh.calls)-1]))
}

func TestACopiedWorktreeBranchedFromThePushedBranchStaysCoveredAfterASquash(t *testing.T) {
	t.Parallel()

	r := newCopyRig(t, nil)
	start := headOf(t, r.parent)
	pushed(t, r.repo, parentBranch, start)

	agent := filepath.Join(r.repo, ".claude", "worktrees", "agent-2")
	testutil.RunGit(t, r.repo, "worktree", "add", "-q", "-b", ownBranchPrefix+"agent-2", agent, start)
	work := commitAt(t, agent, "agent-0.txt", "agent-0.txt\n", "Add agent-0.txt", authoredAt)

	// The parent goes on before it copies the agent's commit.
	commitAt(t, r.parent, "parent-2.txt", "parent-2\n", "Go on", authoredAt+50)
	tip := copyOnto(t, r.parent, work)
	r.publish(t, tip)

	snap := listOf(t, r.svc)
	assert.Equal(t, 1, mustRow(t, snap, agent).Unpushed, "the commit it started from is on the branch")

	r.merge(tip)
	r.squashAndDelete(t)

	row := mustRow(t, listOf(t, r.svc), agent)
	assert.Equal(t, 2, row.Unpushed, "the commit it started from is on no branch now, as main has the squash")
	assert.True(t, row.OnGitHub, "but the merged pull request's head has it, and a copy of the other")
	assert.True(t, row.Copies.Recorded)
	assert.True(t, autoCleanable(row))
}

func TestACopiedWorktreeThatStartedFromAnotherDeletedBranchIsntCovered(t *testing.T) {
	t.Parallel()

	r := newCopyRig(t, nil)

	other := addWorktree(t, r.repo, filepath.Join(r.e.home, "wt", "other"), "feat/other")
	base := commitAt(t, other, "other.txt", "other\n", "Start another", authoredAt-500)
	pushed(t, r.repo, "feat/other", base)

	agent := filepath.Join(r.repo, ".claude", "worktrees", "agent-2")
	testutil.RunGit(t, r.repo, "worktree", "add", "-q", "-b", ownBranchPrefix+"agent-2", agent, base)
	work := commitAt(t, agent, "agent-0.txt", "agent-0.txt\n", "Add agent-0.txt", authoredAt)

	tip := copyOnto(t, r.parent, work)
	r.publish(t, tip)
	require.True(t, mustRow(t, listOf(t, r.svc), agent).OnGitHub)

	// The branch it started from is deleted without merging, and the pull request merges.
	testutil.RunGit(t, r.repo, "worktree", "remove", "--force", other)
	testutil.RunGit(t, r.repo, "update-ref", "-d", "refs/remotes/origin/feat/other")
	r.merge(tip)
	r.squashAndDelete(t)

	row := mustRow(t, listOf(t, r.svc), agent)
	assert.Equal(t, 2, row.Unpushed)
	assert.False(t, row.OnGitHub, "the commit it started from is on GitHub no more, and the record never vouched for it")
	assert.Nil(t, row.Item, "a record that vouches for less than the worktree holds links nothing")
	assert.False(t, autoCleanable(row))
}

func TestACopiedWorktreeWhosePullRequestLostItsCopiesIsntOnGitHub(t *testing.T) {
	t.Parallel()

	for name, head := range map[string]func(r *copyRig) string{
		"a head that doesn't have the copies": func(r *copyRig) string { return r.main },
		"a head the clone doesn't have":       func(*copyRig) string { return otherSHA },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r, _, _ := copiedRig(t)
			r.list(t)

			r.merge(head(r))
			r.squashAndDelete(t)

			row := r.list(t)
			assert.False(t, row.OnGitHub)
			assert.False(t, row.Copies.Recorded)
			assert.Equal(t, StageNeedsReview, row.Stage, "done on GitHub, but its work isn't")
			assert.False(t, autoCleanable(row))
			assert.False(t, agentBranch(t, row).Deletable)
		})
	}
}

func TestACopyRecordWhoseCoverageGitCantTellCountsForNothing(t *testing.T) {
	t.Parallel()

	r := newCopyRig(t, func(_ string, sub []string) (reply, bool) {
		return reply{stderr: "fatal: bad object", err: exitError{code: 128}},
			startsWith(sub, callUnpushed) && slices.Contains(sub, "--ignore-missing")
	})
	oids := r.work(t, 2)
	tip := copyOnto(t, r.parent, oids...)
	r.publish(t, tip)
	r.list(t)

	r.merge(tip)
	r.squashAndDelete(t)

	row := r.list(t)
	assert.False(t, row.OnGitHub)
	assert.Equal(t, StageNone, row.Stage, "its record links nothing it can't vouch for")
}

func TestACopyRecordNeedsItsPullRequestFromGitHub(t *testing.T) {
	t.Parallel()

	r, _, tip := copiedRig(t)
	r.list(t)

	r.merge(tip)
	r.squashAndDelete(t)
	r.gh.err = errFakeGitHub

	snap := listOf(t, r.svc)
	assert.NotEmpty(t, snap.LinkError)

	row := mustRow(t, snap, r.agent)
	assert.False(t, row.OnGitHub, "without GitHub's word the record vouches for nothing")
	assert.Nil(t, row.Item)
}

// forcePushed has feat/parent force-pushed to a commit without the copies, its pull request
// #199's head on GitHub at head, in state.
func (r *copyRig) forcePushed(t *testing.T, state ItemStatus, head string) {
	t.Helper()

	testutil.RunGit(t, r.parent, "reset", "-q", "--hard", r.main)
	bare := commitAt(t, r.parent, "other.txt", "other\n", "Start over", authoredAt+200)
	pushed(t, r.repo, parentBranch, bare)

	r.gh.set(exampleName, 199, headPR(199, state, head, parentBranch, ownRepo).State)
	r.gh.setHead(parentBranch, headPR(199, state, head, parentBranch, ownRepo))
}

func TestACopyRecordVouchesOnceItsPullRequestMergedWhateverBecameOfItsBranch(t *testing.T) {
	t.Parallel()

	r, _, tip := copiedRig(t)
	r.list(t)

	// Merged with every copy, then force-pushed without them: the branch is there.
	r.forcePushed(t, ItemMerged, tip)

	row := r.list(t)
	assert.True(t, row.OnGitHub, "the merged pull request's head has every copy")
	assert.Equal(t, Copies{Ref: "origin/" + parentBranch, Branch: parentBranch, Count: 2, Identical: 2, Recorded: true, PR: 199}, row.Copies,
		"recorded, though its branch isn't gone")
	assert.Equal(t, LinkCopy, row.LinkedBy)
	require.NotNil(t, row.Item)
	assert.Equal(t, 199, row.Item.Number)
	assert.Equal(t, StageFinished, row.Stage)
	assert.True(t, autoCleanable(row))
	assert.True(t, agentBranch(t, row).Deletable)
}

func TestACopyRecordOfAnOpenPullRequestNeedsTheCopiesOnItsBranch(t *testing.T) {
	t.Parallel()

	r, _, tip := copiedRig(t)
	r.list(t)

	// The branch was force-pushed without the copies, though GitHub still says the open
	// pull request's head has them.
	r.forcePushed(t, ItemOpen, tip)

	row := r.list(t)
	assert.False(t, row.OnGitHub, "an open pull request counts only the copies on its branch")
	assert.False(t, row.Copies.Recorded)
	require.Len(t, r.records(t), 1, "the record still describes the worktree")
	assert.False(t, agentBranch(t, row).Deletable)
}

func TestAutoCleanableKeepsCopiedWorkToItsMergedPullRequest(t *testing.T) {
	t.Parallel()

	record := &worklog.Copy{Owner: exampleRepo().Owner, Name: exampleName, Number: 199}
	merged := func(number int) *LinkedItem {
		return &LinkedItem{Kind: item.KindPR, Owner: "Acme-Org", Name: exampleName, Number: number, State: ItemMerged}
	}
	copied := func(change func(*Worktree)) Worktree {
		wt := Worktree{
			CanCleanUp: true, Managed: true, Unpushed: 2, Head: headSHA, Item: merged(199), LinkedBy: LinkCopy,
			copies: copyState{record: record, onGitHub: true, merged: true},
		}
		change(&wt)

		return wt
	}

	assert.True(t, autoCleanable(copied(func(*Worktree) {})))

	for name, change := range map[string]func(*Worktree){
		"not merged with every copy":       func(wt *Worktree) { wt.copies.merged = false },
		"linked to another pull request":   func(wt *Worktree) { wt.Item = merged(200) },
		"its pull request open":            func(wt *Worktree) { wt.Item.State = ItemOpen },
		"made by hand":                     func(wt *Worktree) { wt.Managed = false },
		"no record":                        func(wt *Worktree) { wt.copies.record = nil },
		"linked by name, never launched":   func(wt *Worktree) { wt.LinkedBy = LinkName },
		"linked by branch, never launched": func(wt *Worktree) { wt.LinkedBy = LinkBranch },
		"by name, launched, copies unmerged": func(wt *Worktree) {
			wt.LinkedBy, wt.Item.Launched, wt.copies.merged = LinkName, true, false
		},
	} {
		assert.False(t, autoCleanable(copied(change)), name)
	}

	assert.True(t, autoCleanable(copied(func(wt *Worktree) { wt.LinkedBy, wt.Item.Launched = LinkName, true })),
		"a launched worktree whose copies went into a merged pull request")
	assert.False(t, autoCleanable(copied(func(wt *Worktree) { wt.Unpushed, wt.copies = 0, copyState{} })),
		"a link by copies always needs its record's merged pull request")
}

func TestHeadSafetyOnlyTrustsCopiesInAMergedPullRequest(t *testing.T) {
	t.Parallel()

	open := &LinkedItem{Kind: item.KindPR, Number: 199, State: ItemOpen}

	assert.Equal(t, branchSafety{onGitHub: true}, headSafety(&Worktree{Unpushed: 0}))
	assert.Equal(t, branchSafety{onGitHub: true}, headSafety(&Worktree{Unpushed: 2, copies: copyState{merged: true}}))
	assert.Equal(t, branchSafety{why: "its commits are on GitHub only as copies, in pull request #199, which hasn't merged with them"},
		headSafety(&Worktree{Unpushed: 2, copies: copyState{onGitHub: true, pr: open}}))
	assert.Equal(t, branchSafety{why: "its commits are on GitHub only as copies, which no merged pull request has yet"},
		headSafety(&Worktree{Unpushed: 2, copies: copyState{onGitHub: true}}))
	assert.Equal(t, branchSafety{why: "2 commits on it aren't on GitHub"}, headSafety(&Worktree{Unpushed: 2}))
	assert.True(t, strings.HasPrefix(headSafety(&Worktree{Unpushed: -1}).why, "commits on it"))
}
