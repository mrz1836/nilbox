package worktrees

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/worklog"
)

// ranIn returns the folders the git calls ran in, by their -C, resolved through links.
func ranIn(calls [][]string) map[string]bool {
	dirs := map[string]bool{}

	for _, argv := range calls {
		if len(argv) > 2 && argv[1] == "-C" {
			dirs[paths.Resolved(argv[2])] = true
		}
	}

	return dirs
}

func TestListRunsGitOnlyInTheClonesWorthListing(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	alpha := e.newGitHubRepo(t, "alpha")
	addWorktree(t, alpha, filepath.Join(e.home, "wt", "alpha-feature"), "feature")
	plain := e.newGitHubRepo(t, "plain")
	web := e.repoAt(t, filepath.Join(e.home, "work", "web"))

	recorded := e.newGitHubRepo(t, "recorded")
	gone, _ := agentWorktree(t, recorded, "pr-2")
	testutil.RunGit(t, recorded, "worktree", "remove", gone)

	launches := &fakeLaunches{records: []worklog.Record{launchOf(recorded, "pr-2", item.KindPR, 2)}}
	svc := e.linked(linkDeps{
		gh: &fakeGitHub{}, launches: launches,
		locations: []localrepos.Location{{Path: "~/work", Kind: localrepos.KindFolder}},
	})

	git := scriptedGit(e.git, func([]string) (reply, bool) { return reply{}, false })
	svc.run = git

	snap, err := svc.List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha"}, repoNames(snap))
	assert.Equal(t, []string{"recorded/pr-2"}, launches.forgotten,
		"a clone only a record names is still listed, and its record, whose worktree is gone, forgotten")

	ran := ranIn(git.Calls())
	assert.True(t, ran[paths.Resolved(alpha)], "a clone with a linked worktree is listed")
	assert.True(t, ran[paths.Resolved(recorded)], "a clone a record names is listed")
	assert.False(t, ran[paths.Resolved(plain)], "a clone with no worktrees and no record runs no git")
	assert.False(t, ran[paths.Resolved(web)], "nor does one in another location")

	assert.Equal(t, 1, snap.WorktreeCount)
	assert.Equal(t, snap.WorktreeCount, svc.Sweep(t.Context()).WorktreeCount, "the sweep's count agrees")
}

func TestWorthListing(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	alpha := e.newRepo(t, "alpha")
	alphaWT := addWorktree(t, alpha, filepath.Join(e.root, "alpha-wt"), "feature")
	plain := e.newRepo(t, "plain")
	named := e.newRepo(t, "named")
	broken := filepath.Join(e.root, "broken")
	testutil.WriteFile(t, filepath.Join(broken, ".git"), "gitdir: "+filepath.Join(e.home, "missing")+"\n")

	dirs := []string{alpha, alphaWT, broken, named, plain}
	records := []worklog.Record{{ClonePath: named + "/", Worktree: "pr-1"}}

	assert.Equal(t, []string{alpha, alphaWT, broken, named}, worthListing(dirs, records),
		"a clone with a linked worktree, a linked worktree itself, one whose git folder can't be found, and one "+
			"a record names, by its path once cleaned")
	assert.Equal(t, []string{alpha, alphaWT, broken}, worthListing(dirs, nil))

	common, linked, known := linkedWorktrees(alphaWT)
	assert.True(t, known)
	assert.Equal(t, 1, linked, "a linked worktree counts its repository's worktrees")
	assert.Equal(t, paths.Resolved(filepath.Join(alpha, ".git")), common)

	_, linked, known = linkedWorktrees(plain)
	assert.True(t, known)
	assert.Zero(t, linked)

	_, _, known = linkedWorktrees(broken)
	assert.False(t, known, "a git folder that isn't there is for git to explain")
}
