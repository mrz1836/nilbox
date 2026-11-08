package launcher

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

var (
	errGHNotAllowed = errors.New("gh command not allowed")
	errFakeClone    = errors.New("exit status 1")
)

// fakeGH stands in for the GitHub CLI. It accepts only `gh repo clone owner/name <dir>`
// and, unless told to fail, creates dir as a git working tree whose origin is the
// repository, telling git about it, the way a real clone would look on disk. It never
// touches the network.
type fakeGH struct {
	git *fakeGit

	// stderr and err make every clone fail with them.
	stderr string
	err    error

	mu    sync.Mutex
	calls [][]string
}

// Run records the argv and fakes the clone.
func (g *fakeGH) Run(_ context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	argv := append([]string{name}, args...)
	g.calls = append(g.calls, argv)

	if len(argv) != 5 || argv[0] != "gh" || argv[1] != "repo" || argv[2] != "clone" || !filepath.IsAbs(argv[4]) {
		return nil, nil, fmt.Errorf("%w: %q", errGHNotAllowed, argv)
	}

	if g.err != nil {
		return nil, []byte(g.stderr), g.err
	}

	dir := argv[4]
	if err = os.MkdirAll(filepath.Join(dir, ".git"), 0o700); err != nil {
		return nil, nil, err
	}

	g.git.setRemotes(dir, "remote.origin.url https://github.com/"+argv[3]+".git\n")

	return nil, []byte("Cloning into '" + filepath.Base(dir) + "'...\n"), nil
}

// recorded returns a copy of every argv run so far.
func (g *fakeGH) recorded() [][]string {
	g.mu.Lock()
	defer g.mu.Unlock()

	return slices.Clone(g.calls)
}

// setRemotes makes the remote lookup in dir print out.
func (g *fakeGit) setRemotes(dir, out string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.remotes[dir] = out
}

// withGH gives the harness a fake GitHub CLI.
func (h *launchHarness) withGH() *fakeGH {
	gh := &fakeGH{git: h.git}
	h.cfg.GH = gh

	return gh
}

func TestCloneTarget(t *testing.T) {
	t.Run("a repository with a clone", func(t *testing.T) {
		h := newLaunchHarness(t)
		gh := h.withGH()

		got, err := h.service().CloneTarget(t.Context(), h.trustedPR)
		require.NoError(t, err)
		assert.Equal(t, CloneTarget{Repo: "acme-org/example-repo", Cloned: true, Dir: "~/projects/example-repo"}, got)
		assert.Empty(t, gh.recorded())
	})

	t.Run("a repository without one", func(t *testing.T) {
		h := newLaunchHarness(t)
		gh := h.withGH()

		got, err := h.service().CloneTarget(t.Context(), h.orphanPR)
		require.NoError(t, err)
		assert.Equal(t, CloneTarget{Repo: "acme-org/missing-repo", Dir: "~/projects/missing-repo"}, got)
		assert.Empty(t, gh.recorded(), "finding the target clones nothing")
		assert.NoDirExists(t, filepath.Join(h.root, "missing-repo"))
	})

	t.Run("a target already taken", func(t *testing.T) {
		h := newLaunchHarness(t)
		require.NoError(t, os.Mkdir(filepath.Join(h.root, "missing-repo"), 0o700))

		got, err := h.service().CloneTarget(t.Context(), h.orphanPR)
		require.NoError(t, err)
		assert.Equal(t, CloneTarget{Repo: "acme-org/missing-repo", Dir: "~/projects/missing-repo", Taken: true}, got)
	})

	t.Run("a target taken by a dangling symlink", func(t *testing.T) {
		h := newLaunchHarness(t)
		require.NoError(t, os.Symlink(filepath.Join(h.home, "gone"), filepath.Join(h.root, "missing-repo")))

		got, err := h.service().CloneTarget(t.Context(), h.orphanPR)
		require.NoError(t, err)
		assert.True(t, got.Taken)
	})

	t.Run("the configured clone root", func(t *testing.T) {
		h := newLaunchHarness(t)
		other := filepath.Join(h.home, "code")
		require.NoError(t, os.Mkdir(other, 0o700))
		require.NoError(t, testutil.ErrOf(h.settings.SetCloneRoot(t.Context(), "~/code")))

		got, err := h.service().CloneTarget(t.Context(), h.orphanPR)
		require.NoError(t, err)
		assert.Equal(t, "~/code/missing-repo", got.Dir)
	})
}

func TestCloneThenLaunch(t *testing.T) {
	h := newLaunchHarness(t)
	gh := h.withGH()
	svc := h.service()
	target := filepath.Join(h.root, "missing-repo")

	_, err := svc.Launch(t.Context(), LaunchRequest{ItemID: h.orphanPR})
	require.ErrorIs(t, err, localrepos.ErrCloneNotFound, "a launch never clones on its own")
	assert.Empty(t, gh.recorded())

	got, err := svc.Clone(t.Context(), h.orphanPR)
	require.NoError(t, err)
	assert.Equal(t, CloneTarget{Repo: "acme-org/missing-repo", Cloned: true, Dir: "~/projects/missing-repo"}, got)

	assert.Equal(t, [][]string{{"gh", "repo", "clone", "acme-org/missing-repo", target}}, gh.recorded(),
		"one gh argv: the repository and an absolute target under the clone root")
	assert.Equal(t, target, h.cachedPath(t, "missing-repo"), "the new clone is cached")

	before := len(h.git.recorded())
	res, script := h.launch(t, LaunchRequest{ItemID: h.orphanPR})

	assert.Equal(t, "~/projects/missing-repo", res.ClonePath)
	assert.Contains(t, script, "cd -- "+shQuote(target)+" || exit 1\n")
	assert.Len(t, gh.recorded(), 1, "the launch runs no gh command")

	for _, argv := range h.git.recorded()[before:] {
		assert.True(t, allowedGitArgv(argv), "the launch ran a git command other than the remote lookup: %q", argv)
		assert.Equal(t, target, argv[2], "the launch only validates the cached clone")
	}
}

func TestCloneLeavesAnExistingCloneAlone(t *testing.T) {
	h := newLaunchHarness(t)
	gh := h.withGH()

	got, err := h.service().Clone(t.Context(), h.trustedPR)
	require.NoError(t, err)
	assert.Equal(t, CloneTarget{Repo: "acme-org/example-repo", Cloned: true, Dir: "~/projects/example-repo"}, got)
	assert.Empty(t, gh.recorded(), "a repository that has a clone is never cloned again")
}

func TestCloneRefusesAnExistingTarget(t *testing.T) {
	tests := []struct {
		name string
		make func(t *testing.T, target string)
	}{
		{
			name: "an empty directory",
			make: func(t *testing.T, target string) {
				t.Helper()
				require.NoError(t, os.Mkdir(target, 0o700))
			},
		},
		{
			name: "a clone of another repository",
			make: func(t *testing.T, target string) {
				t.Helper()
				makeRepoDir(t, target)
			},
		},
		{
			name: "a file",
			make: func(t *testing.T, target string) {
				t.Helper()
				require.NoError(t, os.WriteFile(target, []byte("notes"), 0o600))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newLaunchHarness(t)
			gh := h.withGH()
			target := filepath.Join(h.root, "missing-repo")
			tt.make(t, target)

			before, err := os.Lstat(target)
			require.NoError(t, err)

			_, err = h.service().Clone(t.Context(), h.orphanPR)
			require.ErrorIs(t, err, ErrCloneTargetExists)
			assert.Equal(t, "clone acme-org/missing-repo into ~/projects/missing-repo: it already exists", err.Error())

			assert.Empty(t, gh.recorded(), "gh never runs against an existing target")
			assert.Empty(t, h.cachedPath(t, "missing-repo"))

			after, err := os.Lstat(target)
			require.NoError(t, err)
			assert.Equal(t, before.ModTime(), after.ModTime(), "the target is left as it was")
		})
	}
}

func TestCloneFailures(t *testing.T) {
	t.Run("gh fails", func(t *testing.T) {
		h := newLaunchHarness(t)
		gh := h.withGH()
		gh.stderr, gh.err = "\nGraphQL: Could not resolve to a Repository with the name 'acme-org/missing-repo'.\n", errFakeClone

		_, err := h.service().Clone(t.Context(), h.orphanPR)
		require.ErrorIs(t, err, errFakeClone)
		assert.Equal(t, "clone acme-org/missing-repo: GraphQL: Could not resolve to a Repository with the name "+
			"'acme-org/missing-repo'.: exit status 1", err.Error())
		assert.Empty(t, h.cachedPath(t, "missing-repo"))
	})

	t.Run("gh fails silently", func(t *testing.T) {
		h := newLaunchHarness(t)
		gh := h.withGH()
		gh.err = errFakeClone

		_, err := h.service().Clone(t.Context(), h.orphanPR)
		require.ErrorIs(t, err, errFakeClone)
		assert.Equal(t, "clone acme-org/missing-repo: exit status 1", err.Error())
	})

	t.Run("git's own prefix is dropped", func(t *testing.T) {
		h := newLaunchHarness(t)
		gh := h.withGH()
		gh.stderr, gh.err = "fatal: could not create work tree dir: Permission denied\n", errFakeClone

		_, err := h.service().Clone(t.Context(), h.orphanPR)
		require.ErrorIs(t, err, errFakeClone)
		assert.Contains(t, err.Error(), ": could not create work tree dir: Permission denied:")
	})

	t.Run("no GH runner", func(t *testing.T) {
		h := newLaunchHarness(t)

		_, err := h.service().Clone(t.Context(), h.orphanPR)
		require.ErrorIs(t, err, ErrCloneUnavailable)
	})

	t.Run("unknown item", func(t *testing.T) {
		h := newLaunchHarness(t)
		gh := h.withGH()

		_, err := h.service().Clone(t.Context(), 9999)
		require.ErrorIs(t, err, item.ErrNotFound)
		assert.Empty(t, gh.recorded())
	})

	t.Run("unreadable settings", func(t *testing.T) {
		h := newLaunchHarness(t)
		h.cfg.Settings = failingSettings{}

		_, err := h.service().CloneTarget(t.Context(), h.orphanPR)
		require.ErrorIs(t, err, errFakeSettings)
	})

	t.Run("missing clone root", func(t *testing.T) {
		h := newLaunchHarness(t)
		gh := h.withGH()
		require.NoError(t, os.RemoveAll(h.root))

		_, err := h.service().Clone(t.Context(), h.orphanPR)
		require.ErrorIs(t, err, os.ErrNotExist)
		assert.Empty(t, gh.recorded(), "gh never creates the clone root")
	})
}

func TestCloneRefusesInvalidNames(t *testing.T) {
	for _, tt := range []struct{ owner, name string }{
		{owner: "acme-org", name: ".."},
		{owner: "acme-org", name: "../escape"},
		{owner: "-acme", name: "missing-repo"},
		{owner: "acme org", name: "missing-repo"},
	} {
		t.Run(tt.owner+"/"+tt.name, func(t *testing.T) {
			h := newLaunchHarness(t)
			gh := h.withGH()
			h.exec(t, "UPDATE repos SET owner = ?, name = ? WHERE name = ?",
				tt.owner, tt.name, "missing-repo")

			_, err := h.service().Clone(t.Context(), h.orphanPR)
			require.ErrorIs(t, err, repo.ErrInvalid)
			assert.Empty(t, gh.recorded())
		})
	}
}

func TestClonesRunOneAtATime(t *testing.T) {
	h := newLaunchHarness(t)
	gh := h.withGH()
	svc := h.service()

	var wg sync.WaitGroup

	errs := make([]error, 4)

	for i := range errs {
		wg.Go(func() {
			_, errs[i] = svc.Clone(t.Context(), h.orphanPR)
		})
	}

	wg.Wait()

	for _, err := range errs {
		require.NoError(t, err)
	}

	assert.Len(t, gh.recorded(), 1, "the first clone wins; the rest find it")
}
