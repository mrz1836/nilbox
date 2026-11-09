package worktrees

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/testutil"
)

const cloneRoot = "~/projects"

var (
	errFakeSettings = errors.New("fake: settings unavailable")
	errFakeFind     = errors.New("fake: git not found")
)

// listedAt is the fixed time snapshots are stamped with.
func listedAt() time.Time { return time.Date(2026, 11, 5, 9, 30, 0, 0, time.UTC) }

// fakeSettings serves a clone root and the automatic cleanup setting.
type fakeSettings struct {
	root string
	auto bool
	err  error
}

func (f fakeSettings) Get(context.Context) (SweepSettings, error) {
	return SweepSettings{CloneRoot: f.root, AutoCleanup: f.auto}, f.err
}

// reply is a scripted git answer.
type reply struct {
	stdout string
	stderr string
	err    error
}

// gitWords returns a git run's words after its leading -C <dir> and -c settings.
func gitWords(argv []string) []string {
	sub := argv[1:]
	if len(sub) >= 2 && sub[0] == "-C" {
		sub = sub[2:]
	}

	for len(sub) >= 2 && sub[0] == "-c" {
		sub = sub[2:]
	}

	return sub
}

// scriptedGit answers the git commands intercept accepts itself, matching them by their
// words after any leading -C <dir> and -c settings, and runs the rest with realGit.
func scriptedGit(realGit execpath.Runner, intercept func(sub []string) (reply, bool)) *testutil.Runner {
	return &testutil.Runner{Script: func(ctx context.Context, argv []string) ([]byte, []byte, error) {
		sub := gitWords(argv)

		if answer, ok := intercept(sub); ok {
			return []byte(answer.stdout), []byte(answer.stderr), answer.err
		}

		return realGit.Run(ctx, argv[0], argv[1:]...)
	}}
}

// startsWith reports whether sub begins with the space-separated words of prefix.
func startsWith(sub []string, prefix string) bool {
	words := strings.Fields(prefix)

	return len(sub) >= len(words) && strings.Join(sub[:len(words)], " ") == prefix
}

// env is a temp home with a clone root and a Service over it.
type env struct {
	home string
	root string
	svc  *Service

	// git runs real git isolated from the user's and the system's configuration.
	git execpath.Exec
}

// newEnv creates home/projects and builds a Service using real git, isolated through
// the runner's environment rather than the process's, so tests can run in parallel.
func newEnv(t testing.TB) *env {
	t.Helper()

	testutil.RequireGit(t)

	home := t.TempDir()
	root := filepath.Join(home, "projects")
	require.NoError(t, os.MkdirAll(root, 0o700))

	e := &env{home: home, root: root, git: execpath.Exec{Env: testutil.GitEnv(home)}}
	e.svc = e.service(e.git)

	return e
}

// service builds a Service over the env with the given git runner.
func (e *env) service(run execpath.Runner) *Service {
	return NewService(Config{
		Settings: fakeSettings{root: cloneRoot},
		Git:      run,
		Finder:   execpath.Search{},
		Home:     e.home,
		Clock:    testutil.NewClock(listedAt()),
	})
}

// scripted builds a Service whose git runner answers some commands itself.
func (e *env) scripted(intercept func(sub []string) (reply, bool)) *Service {
	return e.service(scriptedGit(e.git, intercept))
}

// newRepo creates a repository under the clone root with one commit and returns its path.
func (e *env) newRepo(t testing.TB, name string) string {
	t.Helper()

	dir := filepath.Join(e.root, name)
	testutil.InitRepo(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README.md"), []byte("example\n"), 0o600))
	testutil.RunGit(t, dir, "add", "README.md")
	testutil.RunGit(t, dir, "commit", "-q", "-m", "init")

	return dir
}

// addWorktree adds a linked worktree on a new branch at path.
func addWorktree(t testing.TB, repo, path, branch string) string {
	t.Helper()

	testutil.RunGit(t, repo, argv("worktree add -q -b", branch, path)...)

	return path
}

// lock locks a worktree with a reason.
func lock(t *testing.T, repo, path, reason string) {
	t.Helper()

	testutil.RunGit(t, repo, append(argv("worktree lock --reason"), reason, path)...)
}

// dirty modifies a tracked file and adds an untracked one.
func dirty(t *testing.T, path string) {
	t.Helper()

	require.NoError(t, os.WriteFile(filepath.Join(path, "README.md"), []byte("changed\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(path, "notes.txt"), []byte("draft\n"), 0o600))
}

// errOf returns a removal's error, for a test that checks only that.
func errOf(_ RemoveResult, err error) error {
	return err
}

// branches lists a repository's local branches.
func branches(t *testing.T, repo string) []string {
	t.Helper()

	return strings.Fields(testutil.RunGit(t, repo, "branch", "--list", "--format=%(refname:short)"))
}

// gitWorktreePaths lists the resolved paths git knows as worktrees of repo.
func gitWorktreePaths(t *testing.T, repo string) []string {
	t.Helper()

	entries, err := ParsePorcelain([]byte(testutil.RunGit(t, repo, listArgs()...)))
	require.NoError(t, err)

	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, resolvedPath(entry.Path))
	}

	return paths
}

// list takes a snapshot and fails on error.
func (e *env) list(t *testing.T) Snapshot {
	t.Helper()

	snap, err := e.svc.List(t.Context())
	require.NoError(t, err)

	return snap
}

// findRepo returns the snapshot's repository named name.
func findRepo(snap Snapshot, name string) (Repo, bool) {
	for _, repo := range snap.Repos {
		if repo.Name == name {
			return repo, true
		}
	}

	return Repo{}, false
}

// findRow returns the snapshot's row for path.
func findRow(snap Snapshot, path string) (Worktree, bool) {
	for _, repo := range snap.Repos {
		for _, wt := range repo.Worktrees {
			if samePath(wt.Path, path) {
				return wt, true
			}
		}
	}

	return Worktree{}, false
}

// mustRow returns the snapshot's row for path and fails when it is missing.
func mustRow(t *testing.T, snap Snapshot, path string) Worktree {
	t.Helper()

	wt, ok := findRow(snap, path)
	require.True(t, ok, "no row for %s", path)

	return wt
}

// repoNames lists the snapshot's repository names in order.
func repoNames(snap Snapshot) []string {
	names := make([]string, 0, len(snap.Repos))
	for _, repo := range snap.Repos {
		names = append(names, repo.Name)
	}

	return names
}

func TestListScansAllReposUnderRoot(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	wt := filepath.Join(e.home, "wt")

	alpha := e.newRepo(t, "alpha")
	feature := addWorktree(t, alpha, filepath.Join(wt, "alpha-feature"), "feature")
	changed := addWorktree(t, alpha, filepath.Join(wt, "alpha-dirty"), "dirty-work")
	dirty(t, changed)
	testutil.RunGit(t, alpha, argv("worktree add -q --detach", filepath.Join(wt, "alpha-detached"))...)
	claude := addWorktree(t, alpha, filepath.Join(alpha, ".claude", "worktrees", "pr-7"), "worktree-pr-7")
	lock(t, alpha, claude, "claude session pr-7 (pid 1 start Wed Nov 5 09:00:00 2026)")
	stale := addWorktree(t, alpha, filepath.Join(wt, "alpha-stale"), "stale-session")
	lock(t, alpha, stale, "claude session stale (pid "+strconv.Itoa(deadPID(t))+" start earlier)")

	e.newRepo(t, "beta") // no linked worktrees: not listed

	gamma := e.newRepo(t, "gamma")
	addWorktree(t, gamma, filepath.Join(e.root, "gamma-wt"), "gamma-feature") // also scanned as a clone

	require.NoError(t, os.MkdirAll(filepath.Join(e.root, "notes"), 0o700))

	nested := filepath.Join(e.root, "group", "nested")
	require.NoError(t, os.MkdirAll(filepath.Dir(nested), 0o700))
	testutil.InitRepo(t, nested)
	require.NoError(t, os.WriteFile(filepath.Join(nested, "x"), []byte("x"), 0o600))
	testutil.RunGit(t, nested, "add", "x")
	testutil.RunGit(t, nested, "commit", "-q", "-m", "x")
	addWorktree(t, nested, filepath.Join(wt, "nested-wt"), "nested-feature") // two levels down: ignored

	broken := filepath.Join(e.root, "broken")
	require.NoError(t, os.MkdirAll(broken, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(broken, ".git"), []byte("gitdir: "+filepath.Join(e.home, "missing")+"\n"), 0o600))

	snap := e.list(t)

	assert.Equal(t, cloneRoot, snap.Root)
	assert.Equal(t, listedAt().Format(time.RFC3339), snap.ListedAt)
	assert.Equal(t, []string{"alpha", "broken", "gamma"}, repoNames(snap), "beta has no linked worktrees, nested is too deep, and gamma-wt folds into gamma")
	assert.Equal(t, 3, snap.CleanCount, "alpha-feature, alpha-detached, and gamma-wt are clean")
	assert.Equal(t, 6, snap.WorktreeCount, "alpha's five linked worktrees and gamma-wt, the main worktrees aside")
	assert.Equal(t, snap.WorktreeCount, countWorktrees(e.root), "the sweep's count, read without git, agrees")

	repo, ok := findRepo(snap, "alpha")
	require.True(t, ok)
	assert.Equal(t, "~/projects/alpha", repo.DisplayPath)
	assert.False(t, repo.HasStale)
	require.Len(t, repo.Worktrees, 6)

	main := repo.Worktrees[0]
	assert.True(t, main.Main)
	assert.Equal(t, "main", main.Branch)
	assert.False(t, main.CanRemove)
	assert.False(t, main.CanForceRemove)

	row := mustRow(t, snap, feature)
	assert.Equal(t, "~/wt/alpha-feature", row.DisplayPath)
	assert.Equal(t, "feature", row.Branch)
	assert.False(t, row.Dirty)
	assert.Positive(t, row.LastCommitUnix)
	assert.False(t, row.Managed)
	assert.True(t, row.CanRemove)
	assert.False(t, row.CanForceRemove)

	row = mustRow(t, snap, changed)
	assert.True(t, row.Dirty)
	assert.Equal(t, 2, row.DirtyCount, "one modified and one untracked file")
	assert.False(t, row.CanRemove)
	assert.True(t, row.CanForceRemove)

	row = mustRow(t, snap, filepath.Join(wt, "alpha-detached"))
	assert.True(t, row.Detached)
	assert.Empty(t, row.Branch)
	assert.True(t, row.CanRemove)

	row = mustRow(t, snap, claude)
	assert.True(t, row.Managed)
	assert.True(t, row.Locked)
	assert.Equal(t, "claude session pr-7 (pid 1 start Wed Nov 5 09:00:00 2026)", row.LockReason)
	assert.False(t, row.StaleLock, "PID 1 is always running")
	assert.False(t, row.CanRemove)
	assert.False(t, row.CanForceRemove)

	row = mustRow(t, snap, stale)
	assert.True(t, row.Locked)
	assert.True(t, row.StaleLock)
	assert.False(t, row.CanRemove)
	assert.False(t, row.CanForceRemove)

	repo, ok = findRepo(snap, "broken")
	require.True(t, ok)
	assert.NotEmpty(t, repo.Error)
	assert.Empty(t, repo.Worktrees)

	repo, ok = findRepo(snap, "gamma")
	require.True(t, ok)
	require.Len(t, repo.Worktrees, 2)
	assert.True(t, mustRow(t, snap, filepath.Join(e.root, "gamma-wt")).CanRemove)

	_, ok = findRow(snap, filepath.Join(wt, "nested-wt"))
	assert.False(t, ok)
}

func TestRemoveClean(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	alpha := e.newRepo(t, "alpha")
	feature := addWorktree(t, alpha, filepath.Join(e.home, "wt", "feature"), "feature")

	require.NoError(t, errOf(e.svc.Remove(t.Context(), alpha, feature)))

	assert.NoDirExists(t, feature)
	assert.NotContains(t, gitWorktreePaths(t, alpha), resolvedPath(feature))
	assert.Empty(t, e.list(t).Repos, "alpha has no linked worktrees left")
}

func TestRemoveRefusesDirty(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	alpha := e.newRepo(t, "alpha")
	changed := addWorktree(t, alpha, filepath.Join(e.home, "wt", "changed"), "changed")
	dirty(t, changed)

	err := errOf(e.svc.Remove(t.Context(), alpha, changed))
	require.ErrorIs(t, err, ErrRemovalRefused)

	var refused *RefusedError
	require.ErrorAs(t, err, &refused)
	assert.Equal(t, reasonDirty, refused.Reason)
	assert.Equal(t, "~/wt/changed", refused.DisplayPath)
	assert.Contains(t, err.Error(), "has uncommitted changes")

	assert.DirExists(t, changed)
	assert.FileExists(t, filepath.Join(changed, "notes.txt"))
	assert.True(t, mustRow(t, e.list(t), changed).CanForceRemove)
}

func TestForceRemoveDirty(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	alpha := e.newRepo(t, "alpha")
	changed := addWorktree(t, alpha, filepath.Join(e.home, "wt", "changed"), "changed")
	dirty(t, changed)
	clean := addWorktree(t, alpha, filepath.Join(e.home, "wt", "clean"), "clean")

	require.NoError(t, errOf(e.svc.ForceRemove(t.Context(), alpha, changed)))
	assert.NoDirExists(t, changed)

	require.ErrorIs(t, errOf(e.svc.ForceRemove(t.Context(), alpha, clean)), ErrNotDirty, "force is only for worktrees with changes")
	assert.DirExists(t, clean)
}

func TestLockedRefusedEvenWithForce(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	alpha := e.newRepo(t, "alpha")
	held := addWorktree(t, alpha, filepath.Join(e.home, "wt", "held"), "held")
	lock(t, alpha, held, "claude session held (pid 1 start now)")
	heldDirty := addWorktree(t, alpha, filepath.Join(e.home, "wt", "held-dirty"), "held-dirty")
	dirty(t, heldDirty)
	lock(t, alpha, heldDirty, "claude session held-dirty (pid 1 start now)")

	for _, path := range []string{held, heldDirty} {
		require.ErrorIs(t, errOf(e.svc.Remove(t.Context(), alpha, path)), ErrLocked)
		require.ErrorIs(t, errOf(e.svc.ForceRemove(t.Context(), alpha, path)), ErrLocked)
		assert.DirExists(t, path)
		assert.True(t, mustRow(t, e.list(t), path).Locked)
	}

	results, err := e.svc.RemoveAllClean(t.Context())
	require.NoError(t, err)
	assert.Empty(t, results)
	assert.DirExists(t, held)
}

func TestMainWorktreeRefused(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	alpha := e.newRepo(t, "alpha")
	addWorktree(t, alpha, filepath.Join(e.home, "wt", "feature"), "feature")

	require.ErrorIs(t, errOf(e.svc.Remove(t.Context(), alpha, alpha)), ErrMainWorktree)
	require.ErrorIs(t, errOf(e.svc.ForceRemove(t.Context(), alpha, alpha)), ErrMainWorktree)
	assert.FileExists(t, filepath.Join(alpha, "README.md"))

	other := filepath.Join(e.home, "elsewhere")
	require.NoError(t, os.MkdirAll(other, 0o700))
	require.ErrorIs(t, errOf(e.svc.Remove(t.Context(), alpha, other)), ErrNotAWorktree)

	outside := filepath.Join(e.home, "outside")
	testutil.InitRepo(t, outside)
	require.ErrorIs(t, errOf(e.svc.Remove(t.Context(), outside, outside)), ErrOutsideCloneRoot)
	require.ErrorIs(t, e.svc.Prune(t.Context(), outside), ErrOutsideCloneRoot)
	require.ErrorIs(t, errOf(e.svc.Remove(t.Context(), "projects/alpha", alpha)), ErrOutsideCloneRoot)
	require.ErrorIs(t, errOf(e.svc.Remove(t.Context(), filepath.Join(e.root, ".hidden"), alpha)), ErrOutsideCloneRoot)
}

func TestBranchesSurviveRemoval(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	alpha := e.newRepo(t, "alpha")
	wt := filepath.Join(e.home, "wt")
	clean := addWorktree(t, alpha, filepath.Join(wt, "clean"), "clean-work")
	changed := addWorktree(t, alpha, filepath.Join(wt, "changed"), "changed-work")
	dirty(t, changed)
	gone := addWorktree(t, alpha, filepath.Join(wt, "gone"), "gone-work")
	bulk := addWorktree(t, alpha, filepath.Join(wt, "bulk"), "bulk-work")

	before := branches(t, alpha)
	require.Len(t, before, 5)

	require.NoError(t, errOf(e.svc.Remove(t.Context(), alpha, clean)))
	require.NoError(t, errOf(e.svc.ForceRemove(t.Context(), alpha, changed)))
	require.NoError(t, os.RemoveAll(gone))
	require.NoError(t, e.svc.Prune(t.Context(), alpha))

	results, err := e.svc.RemoveAllClean(t.Context())
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.True(t, samePath(bulk, results[0].Path))

	assert.Equal(t, before, branches(t, alpha), "removing worktrees never deletes a branch")
	assert.Equal(t, []string{resolvedPath(alpha)}, gitWorktreePaths(t, alpha))
}

func TestPruneClearsStaleEntries(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	alpha := e.newRepo(t, "alpha")
	gone := addWorktree(t, alpha, filepath.Join(e.home, "wt", "gone"), "gone")
	require.NoError(t, os.RemoveAll(gone))

	snap := e.list(t)
	repo, ok := findRepo(snap, "alpha")
	require.True(t, ok)
	assert.True(t, repo.HasStale)

	row := mustRow(t, snap, gone)
	assert.True(t, row.Prunable)
	assert.NotEmpty(t, row.PrunableReason)
	assert.False(t, row.CanRemove)
	assert.False(t, row.CanForceRemove)
	assert.Zero(t, snap.CleanCount)

	require.ErrorIs(t, errOf(e.svc.ForceRemove(t.Context(), alpha, gone)), ErrPrunable)

	require.NoError(t, e.svc.Prune(t.Context(), alpha))

	assert.Equal(t, []string{resolvedPath(alpha)}, gitWorktreePaths(t, alpha))
	assert.Empty(t, e.list(t).Repos)
	assert.Contains(t, branches(t, alpha), "gone")
}

func TestRemoveAllClean(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	wt := filepath.Join(e.home, "wt")
	alpha := e.newRepo(t, "alpha")
	cleanA := addWorktree(t, alpha, filepath.Join(wt, "clean-a"), "clean-a")
	cleanB := addWorktree(t, alpha, filepath.Join(wt, "clean-b"), "clean-b")
	changed := addWorktree(t, alpha, filepath.Join(wt, "changed"), "changed")
	dirty(t, changed)
	held := addWorktree(t, alpha, filepath.Join(wt, "held"), "held")
	lock(t, alpha, held, "claude session held (pid 1 start now)")
	gamma := e.newRepo(t, "gamma")
	cleanC := addWorktree(t, gamma, filepath.Join(wt, "clean-c"), "clean-c")

	assert.Equal(t, 3, e.list(t).CleanCount)

	results, err := e.svc.RemoveAllClean(t.Context())
	require.NoError(t, err)
	require.Len(t, results, 3)

	for _, result := range results {
		assert.True(t, result.Removed, result.DisplayPath)
		assert.Empty(t, result.Reason)
		assert.True(t, strings.HasPrefix(result.DisplayPath, "~/wt/"), result.DisplayPath)
	}

	for _, path := range []string{cleanA, cleanB, cleanC} {
		assert.NoDirExists(t, path)
	}

	for _, path := range []string{alpha, changed, held, gamma} {
		assert.DirExists(t, path)
	}
}

func TestRemoveAllCleanReportsRefusals(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	alpha := e.newRepo(t, "alpha")
	feature := addWorktree(t, alpha, filepath.Join(e.home, "wt", "feature"), "feature")

	// The worktree changes after it was listed: git's own check refuses it.
	svc := e.scripted(func(sub []string) (reply, bool) {
		if startsWith(sub, "worktree remove") {
			return reply{
				stderr: "fatal: '" + feature + "' contains modified or untracked files, use --force to delete it\n",
				err:    exitError{code: 128},
			}, true
		}

		return reply{}, false
	})

	results, err := svc.RemoveAllClean(t.Context())
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.False(t, results[0].Removed)
	assert.Equal(t, reasonDirty, results[0].Reason)
	assert.DirExists(t, feature)
}

func TestServiceErrors(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	alpha := e.newRepo(t, "alpha")
	feature := addWorktree(t, alpha, filepath.Join(e.home, "wt", "feature"), "feature")
	ctx := t.Context()

	fail := func(prefix string) *Service {
		return e.scripted(func(sub []string) (reply, bool) {
			return reply{err: errFakeStart}, startsWith(sub, prefix)
		})
	}

	t.Run("git not found", func(t *testing.T) {
		t.Parallel()

		svc := NewService(Config{Settings: fakeSettings{root: cloneRoot}, Git: e.git, Finder: testutil.Finder{Err: errFakeFind}, Home: e.home})
		_, err := svc.List(ctx)
		require.ErrorIs(t, err, errFakeFind)
		require.ErrorIs(t, svc.Prune(ctx, alpha), errFakeFind)
	})

	t.Run("settings unavailable", func(t *testing.T) {
		t.Parallel()

		svc := NewService(Config{Settings: fakeSettings{err: errFakeSettings}, Git: e.git, Home: e.home})
		_, err := svc.List(ctx)
		require.ErrorIs(t, err, errFakeSettings)
	})

	t.Run("bad clone roots", func(t *testing.T) {
		t.Parallel()

		for _, root := range []string{"projects", "~/missing"} {
			svc := NewService(Config{Settings: fakeSettings{root: root}, Git: e.git, Home: e.home})
			_, err := svc.List(ctx)
			require.Error(t, err, root)
		}
	})

	t.Run("list cannot run git", func(t *testing.T) {
		t.Parallel()

		svc := fail("worktree list")
		_, err := svc.List(ctx)
		require.ErrorIs(t, err, errFakeStart)
		require.ErrorIs(t, errOf(svc.Remove(ctx, alpha, feature)), errFakeStart)

		_, err = svc.RemoveAllClean(ctx)
		require.ErrorIs(t, err, errFakeStart)
	})

	t.Run("status cannot run git", func(t *testing.T) {
		t.Parallel()

		svc := fail("status")
		_, err := svc.List(ctx)
		require.ErrorIs(t, err, errFakeStart)
		require.ErrorIs(t, errOf(svc.ForceRemove(ctx, alpha, feature)), errFakeStart)
	})

	t.Run("last commit cannot run git", func(t *testing.T) {
		t.Parallel()

		_, err := fail("log").List(ctx)
		require.ErrorIs(t, err, errFakeStart)
	})

	t.Run("remove and prune cannot run git", func(t *testing.T) {
		t.Parallel()

		err := errOf(fail("worktree remove").Remove(ctx, alpha, feature))
		require.ErrorIs(t, err, errFakeStart)
		require.NotErrorIs(t, err, ErrRemovalRefused)
		require.ErrorIs(t, fail("worktree prune").Prune(ctx, alpha), errFakeStart)
		assert.DirExists(t, feature)
	})

	t.Run("bulk remove cannot run git", func(t *testing.T) {
		t.Parallel()

		results, err := fail("worktree remove").RemoveAllClean(ctx)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Removed)
		assert.Contains(t, results[0].Reason, errFakeStart.Error())
	})

	t.Run("git refuses status and log", func(t *testing.T) {
		t.Parallel()

		svc := e.scripted(func(sub []string) (reply, bool) {
			if startsWith(sub, "status") || startsWith(sub, "log") {
				return reply{stderr: "fatal: unreadable index", err: exitError{code: 128}}, true
			}

			return reply{}, false
		})

		snap, err := svc.List(ctx)
		require.NoError(t, err)

		row := mustRow(t, snap, feature)
		assert.Contains(t, row.Error, "unreadable index")
		assert.Zero(t, row.LastCommitUnix)
		assert.True(t, row.CanRemove, "git's own check still guards a remove")
	})
}

// gitCalls counts the git runs a runner recorded by their first words after -C <dir>
// and the -c settings.
func gitCalls(run *testutil.Runner, prefixes ...string) map[string]int {
	counts := make(map[string]int, len(prefixes))

	for _, argv := range run.Calls() {
		sub := gitWords(argv)

		for _, prefix := range prefixes {
			if startsWith(sub, prefix) {
				counts[prefix]++
			}
		}
	}

	return counts
}

func TestListReadsAClonesCommitTimesInOneRun(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	alpha := e.newRepo(t, "alpha")
	first := addWorktree(t, alpha, filepath.Join(e.home, "wt", "first"), "first")
	second := addWorktree(t, alpha, filepath.Join(e.home, "wt", "second"), "second")

	run := scriptedGit(e.git, func([]string) (reply, bool) { return reply{}, false })

	snap, err := e.service(run).List(t.Context())
	require.NoError(t, err)

	want, err := strconv.ParseInt(strings.TrimSpace(testutil.RunGit(t, alpha, "log", "-1", "--format=%ct")), 10, 64)
	require.NoError(t, err)

	for _, path := range []string{alpha, first, second} {
		assert.Equal(t, want, mustRow(t, snap, path).LastCommitUnix, path)
	}

	assert.Equal(t, map[string]int{
		"worktree list": 1, "log --no-walk=unsorted": 1, "status": 3, "rev-list": 2,
	}, gitCalls(run, "worktree list", "log --no-walk=unsorted", "log -1", "status", "rev-list"),
		"one list and one commit-times run for the clone; a status for each worktree, and an unpushed count for each linked one")
}

func TestListReadsEachCommitTimeAloneWhenGitRefusesThemTogether(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	alpha := e.newRepo(t, "alpha")
	feature := addWorktree(t, alpha, filepath.Join(e.home, "wt", "feature"), "feature")

	run := scriptedGit(e.git, func(sub []string) (reply, bool) {
		if startsWith(sub, "log --no-walk=unsorted") {
			return reply{stderr: "fatal: bad object", err: exitError{code: 128}}, true
		}

		return reply{}, false
	})

	snap, err := e.service(run).List(t.Context())
	require.NoError(t, err)

	assert.Positive(t, mustRow(t, snap, alpha).LastCommitUnix)
	assert.Positive(t, mustRow(t, snap, feature).LastCommitUnix)
	assert.Equal(t, 2, gitCalls(run, "log -1")["log -1"], "each worktree's time is read on its own")
}

func TestResolvedPathThroughALink(t *testing.T) {
	t.Parallel()

	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(target, link))

	realDir := resolvedPath(target)
	require.NoError(t, os.MkdirAll(filepath.Join(target, "kept"), 0o700))

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "existing directory", path: filepath.Join(link, "kept"), want: filepath.Join(realDir, "kept")},
		{name: "deleted directory", path: filepath.Join(link, "gone"), want: filepath.Join(realDir, "gone")},
		{name: "deleted nested directory", path: filepath.Join(link, "a", "b"), want: filepath.Join(realDir, "a", "b")},
		{name: "unclean path", path: link + "/kept/../gone/", want: filepath.Join(realDir, "gone")},
		{name: "relative path", path: "no-such-dir/x", want: filepath.Join("no-such-dir", "x")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, resolvedPath(tt.path))
		})
	}

	assert.True(t, samePath(filepath.Join(link, "gone"), filepath.Join(realDir, "gone")))
	assert.False(t, samePath(filepath.Join(link, "gone"), filepath.Join(realDir, "kept")))
}
