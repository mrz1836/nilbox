package launcher

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// missingRemote is the remote lookup of a clone of the orphan PR's repository.
const missingRemote = "remote.origin.url https://github.com/acme-org/missing-repo.git\n"

// cloneOfMissing makes a clone of the orphan PR's repository at dir, relative to home.
func (h *launchHarness) cloneOfMissing(t *testing.T, dir string) string {
	t.Helper()

	path := filepath.Join(h.home, dir)
	makeRepoDir(t, path)
	h.git.setRemotes(path, missingRemote)

	return path
}

// remember caches path as the clone of the repository named name.
func (h *launchHarness) remember(t *testing.T, name, path string) {
	t.Helper()

	_, err := h.pool.ExecContext(t.Context(), "UPDATE repos SET local_path = ? WHERE name = ?", path, name)
	require.NoError(t, err)
}

func TestCloneTargetAcrossLocations(t *testing.T) {
	t.Parallel()

	t.Run("a clone in another location", func(t *testing.T) {
		t.Parallel()

		h := newLaunchHarness(t)
		h.cloneOfMissing(t, "work/missing-repo")
		require.NoError(t, testutil.ErrOf(h.settings.AddCloneLocations(t.Context(), []string{"~/work"})))

		got, err := h.service().CloneTarget(t.Context(), h.orphanPR)
		require.NoError(t, err)
		assert.Equal(t, CloneTarget{Repo: "acme-org/missing-repo", Cloned: true, Dir: "~/work/missing-repo"}, got)

		res, _ := h.launch(t, LaunchRequest{ItemID: h.orphanPR})
		assert.Equal(t, "~/work/missing-repo", res.ClonePath, "a launch starts in it")
	})

	t.Run("new clones still go in the clone root", func(t *testing.T) {
		t.Parallel()

		h := newLaunchHarness(t)
		makeRepoDir(t, filepath.Join(h.home, "work", "other"))
		require.NoError(t, testutil.ErrOf(h.settings.AddCloneLocations(t.Context(), []string{"~/work"})))

		got, err := h.service().CloneTarget(t.Context(), h.orphanPR)
		require.NoError(t, err)
		assert.Equal(t, CloneTarget{Repo: "acme-org/missing-repo", Dir: "~/projects/missing-repo"}, got)
	})

	t.Run("a remembered clone outside every location", func(t *testing.T) {
		t.Parallel()

		h := newLaunchHarness(t)
		old := h.cloneOfMissing(t, "old/missing-repo")
		h.remember(t, "missing-repo", old)

		got, err := h.service().CloneTarget(t.Context(), h.orphanPR)
		require.NoError(t, err)
		assert.Equal(t, CloneTarget{Repo: "acme-org/missing-repo", Dir: "~/projects/missing-repo", Elsewhere: "~/old/missing-repo"},
			got, "it is offered, not used")

		_, err = h.service().Launch(t.Context(), LaunchRequest{ItemID: h.orphanPR})
		require.ErrorIs(t, err, localrepos.ErrCloneNotFound, "no launch starts where no listing or sweep looks")
	})
}

func TestLinkClone(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	exp := h.cloneOfMissing(t, "Desktop/missing-repo")
	makeRepoDir(t, filepath.Join(h.home, "Desktop", "other"))
	svc := h.service()

	got, err := svc.LinkClone(t.Context(), h.orphanPR, "~/Desktop/missing-repo")
	require.NoError(t, err)
	assert.Equal(t, LinkedClone{Dir: "~/Desktop/missing-repo", Location: localrepos.Preview{
		Path: "~/Desktop/missing-repo", Kind: localrepos.KindClone, Repo: "acme-org/missing-repo",
		Inside: []localrepos.Suggestion{}, Replaces: []string{}, Parent: localrepos.Suggestion{Path: "~/Desktop", Clones: 1},
	}}, got, "its folder, which holds another clone, is offered beside it")
	assert.Equal(t, exp, h.cachedPath(t, "missing-repo"), "it is remembered")

	_, err = svc.Launch(t.Context(), LaunchRequest{ItemID: h.orphanPR})
	require.ErrorIs(t, err, localrepos.ErrCloneNotFound, "it is used only once a location holds it")

	require.NoError(t, testutil.ErrOf(h.settings.AddCloneLocations(t.Context(), []string{got.Dir})))

	res, _ := h.launch(t, LaunchRequest{ItemID: h.orphanPR})
	assert.Equal(t, "~/Desktop/missing-repo", res.ClonePath)
}

func TestLinkCloneRefuses(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	makeRepoDir(t, filepath.Join(h.home, "other"))
	h.git.setRemotes(filepath.Join(h.home, "other"), "remote.origin.url https://github.com/acme-org/other.git\n")
	makeRepoDir(t, filepath.Join(h.home, "no-remote"))
	h.git.setRemotes(filepath.Join(h.home, "no-remote"), "")
	makeRepoDir(t, filepath.Join(h.home, "fork"))
	h.git.setRemotes(filepath.Join(h.home, "fork"), "remote.mine.url https://github.com/octo-user/missing-repo.git\n")
	require.NoError(t, os.MkdirAll(filepath.Join(h.home, "plain"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(h.home, ".hidden"), 0o700))

	tests := []struct {
		dir, want string
	}{
		{dir: "~/plain", want: "~/plain isn't a clone of acme-org/missing-repo."},
		{dir: "~/other", want: "~/other isn't a clone of acme-org/missing-repo; its origin is acme-org/other."},
		{dir: "~/no-remote", want: "~/no-remote isn't a clone of acme-org/missing-repo; it has no remote on GitHub."},
		{
			dir:  "~/fork",
			want: "~/fork isn't a clone of acme-org/missing-repo; its remote mine is octo-user/missing-repo.",
		},
		{dir: "~/.hidden", want: "~/.hidden is hidden, and the app never looks in hidden folders."},
		{dir: "relative", want: "Choose the clone's folder."},
	}

	for _, tt := range tests {
		_, err := h.service().LinkClone(t.Context(), h.orphanPR, tt.dir)
		require.Error(t, err, tt.dir)
		assert.Equal(t, tt.want, uierr.Message(err), tt.dir)
	}

	assert.Empty(t, h.cachedPath(t, "missing-repo"), "nothing refused is remembered")
}
