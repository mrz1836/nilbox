package worktrees

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/worklog"
)

// otherLocations is a folder location, ~/work, a clone location, ~/Desktop/exp, and a
// folder location that isn't there, ~/gone.
func otherLocations() []localrepos.Location {
	return []localrepos.Location{
		{Path: "~/work", Kind: localrepos.KindFolder},
		{Path: "~/Desktop/exp", Kind: localrepos.KindClone},
		{Path: "~/gone", Kind: localrepos.KindFolder},
	}
}

func TestListCoversEveryLocation(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	alpha := e.newRepo(t, "alpha")
	web := e.repoAt(t, filepath.Join(e.home, "work", "web"))
	exp := e.repoAt(t, filepath.Join(e.home, "Desktop", "exp"))

	for _, repo := range []string{alpha, web, exp} {
		addWorktree(t, repo, filepath.Join(e.home, "wt", filepath.Base(repo)), "feature")
	}

	svc := e.linked(linkDeps{locations: otherLocations()})

	snap, err := svc.List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "web", "exp"}, repoNames(snap), "the clone root's, then each location's in order")
	assert.Equal(t, []string{"~/projects", "~/work", "~/Desktop/exp", "~/gone"}, snap.Roots)
	assert.Equal(t, []string{"~/gone"}, snap.Unread, "a location that isn't there is named, and skipped")
	assert.Equal(t, 3, snap.WorktreeCount)

	report := svc.Sweep(t.Context())
	assert.Empty(t, report.Error)
	assert.Equal(t, 3, report.WorktreeCount, "the sweep counts every location's worktrees")

	repo, ok := findRepo(snap, "exp")
	require.True(t, ok)
	assert.Equal(t, "~/Desktop/exp", repo.DisplayPath)
}

func TestAWorktreeInALocationIsRemovedUntilTheLocationIs(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	web := e.repoAt(t, filepath.Join(e.home, "work", "web"))
	one := addWorktree(t, web, filepath.Join(e.home, "wt", "one"), "one")
	two := addWorktree(t, web, filepath.Join(e.home, "wt", "two"), "two")

	svc := e.linked(linkDeps{locations: otherLocations()})

	res, err := svc.Remove(t.Context(), web, one, nil)
	require.NoError(t, err)
	assert.True(t, res.Removed, "a clone in a folder location is one the app may change")

	// The user removes ~/work from the locations: its clone is left as it is, and the app no
	// longer touches it.
	without := e.linked(linkDeps{})

	_, err = without.Remove(t.Context(), web, two, nil)
	require.ErrorIs(t, err, ErrOutsideCloneRoot)
	assert.DirExists(t, two)
	assert.NotContains(t, repoNames(e.list(t)), "web")
}

func TestTheSweepKeepsTheRecordsOfALocationThatIsGone(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	unplugged := filepath.Join(e.home, "Volumes", "ext", "src", "api")
	deleted := filepath.Join(e.root, "deleted")
	removed := e.repoAt(t, filepath.Join(e.home, "work", "web"))

	launches := &fakeLaunches{records: []worklog.Record{
		launchOf(unplugged, "pr-1", item.KindPR, 1),
		launchOf(deleted, "pr-2", item.KindPR, 2),
		launchOf(removed, "pr-3", item.KindPR, 3),
	}}

	svc := e.linked(linkDeps{launches: launches, locations: []localrepos.Location{
		{Path: filepath.Join(e.home, "Volumes", "ext", "src"), Kind: localrepos.KindFolder},
	}})

	report := svc.Sweep(t.Context())
	assert.Empty(t, report.Error)
	assert.Equal(t, []string{"deleted/pr-2"}, launches.forgotten,
		"only a clone gone from a folder still there is forgotten: a drive that isn't plugged in, or a location "+
			"removed in Settings, keeps its records")
}

func TestCloneGone(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	clone := filepath.Join(root, "clone")
	require.NoError(t, os.MkdirAll(clone, 0o700))

	assert.False(t, cloneGone(clone), "it is there")
	assert.True(t, cloneGone(filepath.Join(root, "missing")), "its folder is there, and it isn't")
	assert.False(t, cloneGone(filepath.Join(root, "unplugged", "clone")), "its folder is gone too")

	testutil.WriteFile(t, filepath.Join(root, "file"), "x")
	assert.False(t, cloneGone(filepath.Join(root, "file", "clone")), "what holds it isn't a folder")
}

func TestManagedCloneDirsKeepsALinkedCloneLocation(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	root := filepath.Join(home, "projects")
	target := filepath.Join(home, "elsewhere", "exp")

	for _, dir := range []string{
		filepath.Join(root, "api", ".git"),
		filepath.Join(target, ".git"), filepath.Join(target, ".claude", "worktrees", "pr-1"),
	} {
		require.NoError(t, os.MkdirAll(dir, 0o700))
	}

	require.NoError(t, os.MkdirAll(ManagedRoot(filepath.Join(root, "api")), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(ManagedRoot(filepath.Join(root, "api")), "pr-1"), nil, 0o600))
	require.NoError(t, os.Symlink(target, filepath.Join(root, "exp-link")))
	require.NoError(t, os.Symlink(target, filepath.Join(home, "exp")))

	linked := filepath.Join(home, "exp")
	roots := rootsOf(t, root, localrepos.Location{Path: linked, Kind: localrepos.KindClone})

	found, err := roots.Clones(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Join(root, "api"), filepath.Join(root, "exp-link")}, found.Dirs,
		"the clone location is the clone root's link already")

	assert.Equal(t, []string{filepath.Join(root, "api")}, managedCloneDirs(roots, found.Dirs),
		"a clone a folder reaches through a link is left out")
	assert.Equal(t, []string{linked}, managedCloneDirs(roots, []string{linked}),
		"a clone location that is a link is kept: it is reached no other way")
}
