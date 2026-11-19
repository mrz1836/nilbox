package worktrees

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// TestTheWorktreeReadsReachNoRemote plants, in a clone, what makes git fetch an object it
// lacks from a promisor remote reached through a command (an ext:: URL the clone's own
// config allows), and a mailmap blob it lacks: listing the clone's worktrees, which reads
// every worktree's last commit, and an ancestor's check of a commit it lacks fetch
// nothing. A plain git log and merge-base run the command.
func TestTheWorktreeReadsReachNoRemote(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newSweepFixture(t, e)
	marker := filepath.Join(t.TempDir(), "fetched")
	missing := strings.Repeat("0123456789", 4)

	for _, kv := range [][2]string{
		{"core.repositoryformatversion", "1"},
		{"extensions.partialClone", "evil"},
		{"remote.evil.url", "ext::sh -c touch% " + marker},
		{"remote.evil.promisor", "true"},
		{"protocol.ext.allow", "always"},
		{"mailmap.blob", missing},
	} {
		testutil.RunGit(t, f.repo, "config", kv[0], kv[1])
	}

	svc := e.linked(linkDeps{gh: f.gh, launches: f.launches, guard: e.guardOf()})

	_, err := svc.List(t.Context())
	require.NoError(t, err)

	g, err := svc.git()
	require.NoError(t, err)

	head := strings.TrimSpace(testutil.RunGit(t, f.repo, "rev-parse", "HEAD"))
	assert.False(t, g.isAncestor(t.Context(), f.repo, head, missing), "a commit the clone lacks isn't its ancestor")
	assert.NoFileExists(t, marker, "nothing was fetched")

	plain := execpath.Exec{Env: testutil.GitEnv(e.home), Dir: f.repo}

	_, _, err = plain.Run(t.Context(), "git", "log", "-1", "--format=%ct")
	t.Logf("a plain log: %v", err)
	assert.FileExists(t, marker, "a plain log fetches the mailmap through the clone's own command")

	require.NoError(t, os.Remove(marker))

	_, _, err = plain.Run(t.Context(), "git", "merge-base", "--is-ancestor", head, missing)
	t.Logf("a plain merge-base: %v", err)
	assert.FileExists(t, marker, "and a plain merge-base the commit it lacks")
}

// TestRemoveWeighsTheWorktreesOwnConfig plants, in a worktree's own config.worktree, a
// clean filter for every file of the worktree, whose files' stat data changed: removing it,
// which runs a status inside it, is refused with the filter named, as its listing says, and
// runs nothing. A plain git worktree remove runs the filter.
func TestRemoveWeighsTheWorktreesOwnConfig(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newSweepFixture(t, e)
	marker := filepath.Join(t.TempDir(), "clean")

	require.NoError(t, os.WriteFile(filepath.Join(f.open, ".gitattributes"), []byte("* filter=x\n"), 0o600))
	testutil.RunGit(t, f.open, "add", ".gitattributes")
	testutil.RunGit(t, f.open, "commit", "-q", "-m", "attributes")
	testutil.RunGit(t, f.repo, "config", "extensions.worktreeConfig", "true")
	testutil.RunGit(t, f.open, "config", "--worktree", "filter.x.clean", "touch '"+marker+"'; cat")
	touchAll(t, f.open)

	svc := e.linked(linkDeps{gh: f.gh, launches: f.launches, guard: e.guardOf()})

	snap, err := svc.List(t.Context())
	require.NoError(t, err)

	row := mustRow(t, snap, f.open)
	assert.Contains(t, row.Error, "sets filter.x.clean,")
	assert.False(t, row.CanRemove, "a worktree whose status can't be read is never offered a removal")

	_, err = svc.Remove(t.Context(), f.repo, f.open, nil)
	require.Error(t, err)
	assert.Contains(t, uierr.Message(err), "sets filter.x.clean,")
	assert.DirExists(t, f.open)
	assert.NoFileExists(t, marker)

	testutil.RunGit(t, f.repo, "worktree", "remove", f.open)
	assert.FileExists(t, marker, "a plain removal runs the worktree's own filter")
}

// manyFilesSettings are git's many-files mode and each setting it turns on, each written in
// a case of its own, as git takes a key by any case.
func manyFilesSettings() [][2]string {
	return [][2]string{
		{"FEATURE.ManyFiles", "true"}, {"Index.Version", "4"}, {"index.SKIPHASH", "true"}, {"Core.UntrackedCache", "true"},
	}
}

// TestPruneInACloneInGitsManyFilesMode prunes, under gitread's guards, a clone whose own
// config turns on git's many-files mode, as users of large repositories do, or one of the
// settings it turns on: the gone worktree's entry is cleared, and the clone's other
// worktree's status is read and it is removed, as in any clone.
func TestPruneInACloneInGitsManyFilesMode(t *testing.T) {
	t.Parallel()

	for _, kv := range manyFilesSettings() {
		t.Run(kv[0], func(t *testing.T) {
			t.Parallel()

			e := newEnv(t)
			alpha := e.newRepo(t, "alpha")
			testutil.RunGit(t, alpha, "config", kv[0], kv[1])

			wt := filepath.Join(e.home, "wt")
			gone := addWorktree(t, alpha, filepath.Join(wt, "gone"), "gone")
			clean := addWorktree(t, alpha, filepath.Join(wt, "clean"), "clean")
			require.NoError(t, os.RemoveAll(gone))

			svc := e.linked(linkDeps{guard: e.guardOf()})

			snap, err := svc.List(t.Context())
			require.NoError(t, err)
			assert.Empty(t, mustRow(t, snap, clean).Error, "its status is read")

			require.NoError(t, svc.Prune(t.Context(), alpha))
			assert.Equal(t, []string{resolvedPath(alpha), resolvedPath(clean)}, gitWorktreePaths(t, alpha))

			require.NoError(t, testutil.ErrOf(svc.Remove(t.Context(), alpha, clean, nil)))
			assert.NoDirExists(t, clean)
		})
	}
}

// TestPruneStillRefusesWhatManyFilesModeDoesnt prunes a clone in git's many-files mode
// whose own config also names a pager, a program: the prune is refused before git runs,
// naming the pager alone, and the gone worktree's entry stays.
func TestPruneStillRefusesWhatManyFilesModeDoesnt(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	alpha := e.newRepo(t, "alpha")
	gone := addWorktree(t, alpha, filepath.Join(e.home, "wt", "gone"), "gone")
	require.NoError(t, os.RemoveAll(gone))
	testutil.RunGit(t, alpha, "config", "feature.manyFiles", "true")
	testutil.RunGit(t, alpha, "config", "core.pager", "touch /nonexistent/owned")

	err := e.linked(linkDeps{guard: e.guardOf()}).Prune(t.Context(), alpha)
	require.ErrorIs(t, err, gitread.ErrCloneConfig)
	assert.Equal(t, "This clone's own git config sets core.pager, which isn't run with here: a clone's own config "+
		"may hold only what git and Git LFS write there. Set it for this folder in your global git config, with an "+
		"includeIf, instead.", uierr.Message(err))
	assert.Len(t, gitWorktreePaths(t, alpha), 2, "the gone worktree's entry stays")
}

// TestManyFilesModeRunsNoMonitorOrHookOfTheClonesOwn lists, prunes, and removes the
// worktrees of a clone in git's many-files mode, whose untracked cache a status reads,
// and whose own config also names a file system monitor and a hooks folder, which every
// guarded command sets again, with the stat data of every file changed: neither runs. A
// plain status runs both.
func TestManyFilesModeRunsNoMonitorOrHookOfTheClonesOwn(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	plants := testutil.NewPlanter(t, e.home)
	alpha := e.newRepo(t, "alpha")
	wt := filepath.Join(e.home, "wt")
	gone := addWorktree(t, alpha, filepath.Join(wt, "gone"), "gone")
	clean := addWorktree(t, alpha, filepath.Join(wt, "clean"), "clean")
	require.NoError(t, os.RemoveAll(gone))

	testutil.RunGit(t, alpha, "config", "feature.manyFiles", "true")

	markers := map[string]string{
		"file system monitor": plants.Plant(alpha, testutil.Planting{
			Name: "evil-fsmonitor", Settings: [][2]string{{"core.fsmonitor", "%s"}},
		}),
		"hooks folder": plants.PlantHooks(alpha, "post-index-change"),
	}

	touchAll(t, alpha)
	touchAll(t, clean)

	svc := e.linked(linkDeps{guard: gitread.New(gitread.Config{Env: plants.Environ})})

	snap, err := svc.List(t.Context())
	require.NoError(t, err)
	assert.Empty(t, mustRow(t, snap, clean).Error, "its status is read")
	require.NoError(t, svc.Prune(t.Context(), alpha))
	require.NoError(t, testutil.ErrOf(svc.Remove(t.Context(), alpha, clean, nil)))

	for name, marker := range markers {
		assert.NoFileExists(t, marker, "a guarded command ran the clone's own %s", name)
	}

	_, _, err = execpath.Exec{Env: plants.Environ(), Dir: alpha}.Run(t.Context(), "git", "status", "--porcelain")
	require.NoError(t, err)

	for name, marker := range markers {
		assert.FileExists(t, marker, "a plain status runs the clone's own %s", name)
	}
}

// touchAll changes the stat data of every file directly in dir, so a status reads them all
// again.
func touchAll(t *testing.T, dir string) {
	t.Helper()

	later := time.Now().Add(time.Hour)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	for _, entry := range entries {
		if !entry.IsDir() {
			require.NoError(t, os.Chtimes(filepath.Join(dir, entry.Name()), later, later))
		}
	}
}

// slowGuard is a Guard whose LocalSetup of one folder waits until released, counting every
// LocalSetup.
type slowGuard struct {
	Guard

	slow    string
	release chan struct{}
	calls   *atomic.Int32
}

func (s slowGuard) LocalSetup(_ context.Context, dir string) (gitread.Local, error) {
	s.calls.Add(1)

	if dir == s.slow {
		<-s.release
	}

	return gitread.Local{}, nil
}

func TestLocalSetupsRunSideBySideOncePerFolder(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		guard := slowGuard{slow: "/srv/slow", release: make(chan struct{}), calls: &atomic.Int32{}}
		g := git{guard: guard, locals: &localCache{byID: map[string]localResult{}}}

		var wg sync.WaitGroup

		for range 3 {
			wg.Go(func() {
				_, err := g.localFor(t.Context(), "/srv/slow")
				assert.NoError(t, err)
			})
		}

		synctest.Wait()

		_, err := g.localFor(t.Context(), "/srv/fast")
		require.NoError(t, err, "another folder's waits for no other's")

		close(guard.release)
		wg.Wait()

		assert.Equal(t, int32(2), guard.calls.Load(), "one LocalSetup for each folder")
	})
}
