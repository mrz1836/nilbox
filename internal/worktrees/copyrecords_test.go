package worktrees

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/worklog"
)

// copiedRig is a copyRig whose agent's two commits were copied onto feat/parent, pushed,
// whose pull request #199 is open. It returns the agent's commits, oldest first, and the
// branch's commit.
func copiedRig(t *testing.T) (r *copyRig, oids []string, tip string) {
	t.Helper()

	r = newCopyRig(t, nil)
	oids = r.work(t, 2)
	tip = copyOnto(t, r.parent, oids...)
	r.publish(t, tip)

	return r, oids, tip
}

// publish pushes feat/parent at tip, the head of pull request #199, which is open.
func (r *copyRig) publish(t *testing.T, tip string) {
	t.Helper()

	pushed(t, r.repo, parentBranch, tip)
	r.gh.setHead(parentBranch, headPR(199, ItemOpen, tip, parentBranch, ownRepo))
	r.gh.set(exampleName, 199, headPR(199, ItemOpen, tip, parentBranch, ownRepo).State)
}

// records reads the rig's copy records.
func (r *copyRig) records(t *testing.T) []worklog.Copy {
	t.Helper()

	got, err := r.log.Copies(t.Context())
	require.NoError(t, err)

	return got
}

func TestListRecordsThePullRequestAWorktreesCopiesWentInto(t *testing.T) {
	t.Parallel()

	r, oids, tip := copiedRig(t)

	r.list(t)
	assert.Equal(t, []worklog.Copy{{
		ClonePath: paths.Resolved(r.repo), WorktreePath: paths.Resolved(r.agent), Head: oids[1],
		Ref: "refs/remotes/origin/" + parentBranch, Owner: exampleRepo().Owner, Name: exampleName, Number: 199, CopyTip: tip,
		Commits: []string{oids[1], oids[0]}, CreatedAt: listedAt(), UpdatedAt: listedAt(),
	}}, r.records(t))

	r.clock.Advance(time.Hour)
	r.list(t)
	assert.Equal(t, listedAt(), r.records(t)[0].UpdatedAt, "a listing that finds the same writes nothing")
}

func TestListMovesTheCopyTipOnWithTheBranch(t *testing.T) {
	t.Parallel()

	r, oids, _ := copiedRig(t)
	r.list(t)

	tip := commitAt(t, r.parent, "more.txt", "more\n", "Add more", authoredAt+100)
	r.publish(t, tip)
	r.clock.Advance(time.Hour)
	r.list(t)

	got := r.records(t)
	require.Len(t, got, 1)
	assert.Equal(t, tip, got[0].CopyTip, "the branch still holds every copy")
	assert.Equal(t, oids[1], got[0].Head)
	assert.Equal(t, listedAt(), got[0].CreatedAt)
	assert.Equal(t, listedAt().Add(time.Hour), got[0].UpdatedAt)

	// The branch force-pushed without the copies: the record keeps the last tip that held them.
	testutil.RunGit(t, r.parent, "reset", "-q", "--hard", r.main)
	bare := commitAt(t, r.parent, "other.txt", "other\n", "Start over", authoredAt+200)
	r.publish(t, bare)
	r.list(t)

	got = r.records(t)
	require.Len(t, got, 1)
	assert.Equal(t, tip, got[0].CopyTip)
}

func TestListForgetsACopyRecordOnceItsWorktreeMovesOn(t *testing.T) {
	t.Parallel()

	r, _, _ := copiedRig(t)
	r.list(t)
	require.Len(t, r.records(t), 1)

	commitAt(t, r.agent, "later.txt", "later\n", "Add later", authoredAt+300)
	r.list(t)
	assert.Empty(t, r.records(t), "the record describes another HEAD, and the new one isn't copied")
}

func TestListForgetsTheCopyRecordOfAWorktreeThatIsGone(t *testing.T) {
	t.Parallel()

	r, _, _ := copiedRig(t)
	r.list(t)

	testutil.RunGit(t, r.repo, "worktree", "remove", "--force", r.agent)
	listOf(t, r.svc)
	assert.Empty(t, r.records(t))
}

func TestRemovingAWorktreeForgetsItsCopyRecord(t *testing.T) {
	t.Parallel()

	r, _, _ := copiedRig(t)
	require.True(t, r.list(t).CanRemove)

	_, err := r.svc.Remove(t.Context(), r.repo, r.agent, nil)
	require.NoError(t, err)
	assert.Empty(t, r.records(t))
}

// linkedRig is a copiedRig whose agent's folder moved elsewhere, a link left where git knows
// it, and whose git removes such a folder: this one refuses to (rmdir: Not a directory), so
// the removal takes the link and the folder it points at, as one that doesn't would. It
// returns the copies' branch's commit, and where the folder is now.
func linkedRig(t *testing.T) (r *copyRig, tip, moved string) {
	t.Helper()

	r, _, tip = copiedRig(t)
	moved = filepath.Join(r.e.home, "elsewhere", "agent-1")
	require.NoError(t, os.MkdirAll(filepath.Dir(moved), 0o750))
	require.NoError(t, os.Rename(r.agent, moved))
	require.NoError(t, os.Symlink(moved, r.agent))

	link := r.agent
	r.svc.run = scriptedGit(r.e.git, func(sub []string) (reply, bool) {
		if !startsWith(sub, "worktree remove") {
			return reply{}, false
		}

		if err := errors.Join(os.Remove(link), os.RemoveAll(moved)); err != nil {
			return reply{stderr: "fatal: " + err.Error(), err: exitError{code: 128}}, true
		}

		return reply{}, true
	})

	return r, tip, moved
}

func TestRemovingAWorktreeWhoseFolderIsALinkForgetsItsCopyRecord(t *testing.T) {
	t.Parallel()

	r, _, moved := linkedRig(t)
	require.True(t, r.list(t).CanRemove)
	require.Len(t, r.records(t), 1)
	require.Equal(t, paths.Resolved(moved), r.records(t)[0].WorktreePath, "recorded where the folder is")

	_, err := r.svc.Remove(t.Context(), r.repo, r.agent, nil)
	require.NoError(t, err)
	assert.Empty(t, r.records(t), "forgotten where it was recorded, though the link resolves elsewhere now")
}

func TestCleaningUpAWorktreeWhoseFolderIsALinkForgetsItsCopyRecord(t *testing.T) {
	t.Parallel()

	r, tip, _ := linkedRig(t)
	r.gh.setHead(parentBranch, headPR(199, ItemMerged, tip, parentBranch, ownRepo))
	r.gh.set(exampleName, 199, headPR(199, ItemMerged, tip, parentBranch, ownRepo).State)

	row := r.list(t)
	require.True(t, row.CanCleanUp, row.Stage)
	require.Len(t, r.records(t), 1)

	_, err := r.svc.CleanUp(t.Context(), r.repo, r.agent, nil)
	require.NoError(t, err)
	assert.Empty(t, r.records(t))
}

func TestListKeepsTheCopyRecordsOfClonesItDidntList(t *testing.T) {
	t.Parallel()

	r, _, _ := copiedRig(t)
	head := headOf(t, r.agent)

	unplugged := worklog.Copy{
		ClonePath: filepath.Join(r.e.home, "drive", "projects", "example-repo"), Head: head,
		WorktreePath: filepath.Join(r.e.home, "drive", "projects", "example-repo", "wt"), Ref: "refs/remotes/origin/x",
		Owner: exampleRepo().Owner, Name: exampleName, Number: 7, CopyTip: head, Commits: []string{head},
	}
	gone := unplugged
	gone.ClonePath, gone.WorktreePath = filepath.Join(r.e.root, "gone"), filepath.Join(r.e.root, "gone", "wt")

	require.NoError(t, r.log.RecordCopy(t.Context(), unplugged))
	require.NoError(t, r.log.RecordCopy(t.Context(), gone))

	r.list(t)

	kept := make([]string, 0, 2)
	for _, c := range r.records(t) {
		kept = append(kept, c.WorktreePath)
	}

	assert.ElementsMatch(t, []string{unplugged.WorktreePath, paths.Resolved(r.agent)}, kept,
		"a clone on a drive not plugged in keeps its record; one whose folder is there but the clone gone loses it")
}

func TestListRecordsNoCopyItCantTieToAPullRequest(t *testing.T) {
	t.Parallel()

	tests := map[string]func(t *testing.T, r *copyRig, oids []string){
		"no pull request heads the branch": func(t *testing.T, r *copyRig, oids []string) {
			t.Helper()
			pushed(t, r.repo, parentBranch, copyOnto(t, r.parent, oids...))
		},
		"a fork's pull request has its name": func(t *testing.T, r *copyRig, oids []string) {
			t.Helper()

			tip := copyOnto(t, r.parent, oids...)
			pushed(t, r.repo, parentBranch, tip)
			r.gh.setHead(parentBranch, headPR(201, ItemOpen, tip, parentBranch, "octo-fork/example-repo"))
		},
		"only one commit was copied": func(t *testing.T, r *copyRig, oids []string) {
			t.Helper()
			r.publish(t, copyOnto(t, r.parent, oids[0]))
		},
		"GitHub couldn't be asked": func(t *testing.T, r *copyRig, oids []string) {
			t.Helper()
			r.publish(t, copyOnto(t, r.parent, oids...))
			r.gh.err = errFakeGitHub
		},
	}

	for name, arrange := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r := newCopyRig(t, nil)
			arrange(t, r, r.work(t, 2))

			_, err := r.svc.List(t.Context())
			require.NoError(t, err)
			assert.Empty(t, r.records(t))
		})
	}
}
