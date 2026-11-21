package worktrees

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The task the tests ask for, the day its worktree is named for, and that worktree's own
// branch.
const (
	depsTask   = "deps-20261121"
	depsBranch = "worktree-deps-20261121"
)

// task asks for a task's worktree in the fixture's clone.
func (f remoteFixture) task() EnsureRequest {
	return EnsureRequest{Clone: f.clone, Owner: exampleRepo().Owner, Name: exampleName, Task: depsTask}
}

func TestEnsureMakesATasksWorktreeFromTheDefaultBranch(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	records := &fakeLaunches{}
	m, _ := e.maker(records)

	planned, err := m.Plan(t.Context(), f.task())
	require.NoError(t, err)
	assert.Equal(t, Ensured{
		Path: ManagedPath(f.clone, depsTask), Branch: depsBranch, Action: ActionCreate, Base: "origin/main",
		Task: depsTask, DefaultBranch: mainBranch,
	}, planned, "the default branch as the clone last learned it from origin's HEAD")
	assert.NoDirExists(t, ManagedRoot(f.clone), "a plan changes nothing")
	assert.NotContains(t, branches(t, f.clone), depsBranch)

	merged := f.advanceMain(t, e, "merged.txt")

	got, err := m.Ensure(t.Context(), f.task())
	require.NoError(t, err)
	assert.Equal(t, ActionCreate, got.Action)
	assert.Equal(t, resolvedPath(ManagedPath(f.clone, depsTask)), resolvedPath(got.Path), "beside the clone, as an item's")
	assert.Equal(t, depsBranch, got.Branch)
	assert.Equal(t, depsTask, got.Task)
	assert.Equal(t, "origin/main", got.Base)
	assert.Equal(t, mainBranch, got.DefaultBranch)
	assert.Equal(t, merged, got.Head, "at the default branch as just fetched")
	assert.Empty(t, got.Notice)
	assert.Empty(t, records.all(), "a task's worktree is recorded nowhere")

	upstream := testutil.RunGit(t, got.Path, "for-each-ref", "--format=%(upstream)", "refs/heads/"+depsBranch)
	assert.Empty(t, strings.TrimSpace(upstream), "the new branch tracks nothing, so a push never lands on main")
	assert.Equal(t, f.main, refAt(t, f.clone, "refs/heads/main"), "the clone's own branch is left alone")
	assert.Empty(t, strings.TrimSpace(testutil.RunGit(t, f.clone, "status", "--porcelain")), "and so is its checkout")
}

// TestATasksWorktreeIsMadeUnderGitreadsGuards makes a task's worktree as core does, under
// gitread's guards, in a clone whose own hooks folder and file system monitor are planted:
// it is made with neither running, every command on the clone runs one git, and a clone
// whose own config names a program a checkout would run is refused before git writes a
// file, as for an item's worktree.
func TestATasksWorktreeIsMadeUnderGitreadsGuards(t *testing.T) {
	t.Parallel()

	r := newGuardRig(t)
	markers := []string{
		r.plants.PlantHooks(r.f.clone, "post-checkout"),
		r.plants.Plant(r.f.clone, testutil.Planting{Name: "evil-fsmonitor", Settings: [][2]string{{"core.fsmonitor", "%s"}}}),
	}

	got, err := r.maker().Ensure(t.Context(), r.f.task())
	require.NoError(t, err)
	assert.Equal(t, depsTask, got.Task)
	assert.Equal(t, r.f.main, got.Head)

	for _, marker := range markers {
		assert.NoFileExists(t, marker, "making a task's worktree ran a program of the clone's own")
	}

	bin, err := r.guard().GitFinder().Find("git")
	require.NoError(t, err)

	for _, name := range r.names.all() {
		assert.Equal(t, bin, name)
	}

	planted := refusedPlantings()[0]
	marker := r.plants.Plant(r.f.clone, planted)

	_, err = r.maker().Ensure(t.Context(), r.f.task())
	require.ErrorIs(t, err, gitread.ErrCloneConfig)
	assert.Contains(t, uierr.Message(err), "This clone's own git config sets "+planted.Keys()[0])
	assert.NoFileExists(t, marker)
	assert.Len(t, worktreeList(t, r.f.clone), 2, "the clone and the first task's worktree alone")
}

func TestEnsureGivesEachTaskTheSameDayAWorktreeOfItsOwn(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	m, _ := e.maker(&fakeLaunches{})

	first, err := m.Ensure(t.Context(), f.task())
	require.NoError(t, err)
	assert.Equal(t, depsTask, first.Task)

	work := testutil.Commit(t, first.Path, "go.mod", "module example\n")

	second, err := m.Ensure(t.Context(), f.task())
	require.NoError(t, err)
	assert.Equal(t, ActionCreate, second.Action, "never resumed")
	assert.Equal(t, depsTask+"-2", second.Task)
	assert.Equal(t, "worktree-"+depsTask+"-2", second.Branch)
	assert.Equal(t, resolvedPath(ManagedPath(f.clone, depsTask+"-2")), resolvedPath(second.Path))
	assert.Equal(t, f.main, second.Head, "from the default branch, never from the first one's work")

	planned, err := m.Plan(t.Context(), f.task())
	require.NoError(t, err)
	assert.Equal(t, depsTask+"-3", planned.Task, "a plan names the next free one")

	// The first worktree is removed outside the app, its branch and its work kept: the
	// next task never attaches it.
	testutil.RunGit(t, f.clone, "worktree", "remove", first.Path)
	require.Contains(t, branches(t, f.clone), depsBranch)

	third, err := m.Ensure(t.Context(), f.task())
	require.NoError(t, err)
	assert.Equal(t, ActionCreate, third.Action)
	assert.Equal(t, depsTask+"-3", third.Task, "a name whose branch is left is passed over")
	assert.Equal(t, f.main, third.Head)
	assert.Equal(t, work, refAt(t, f.clone, "refs/heads/"+depsBranch), "and the branch keeps its work")
}

func TestConcurrentTasksEachGetAWorktreeOfTheirOwn(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	m, _ := e.maker(&fakeLaunches{})

	var wg sync.WaitGroup

	results := make([]Ensured, 3)
	errs := make([]error, 3)

	for i := range results {
		wg.Go(func() { results[i], errs[i] = m.Ensure(t.Context(), f.task()) })
	}

	wg.Wait()

	names := make([]string, 0, len(results))

	for i, got := range results {
		require.NoError(t, errs[i], i)
		assert.Equal(t, ActionCreate, got.Action, i)

		names = append(names, got.Task)
	}

	assert.ElementsMatch(t, []string{depsTask, depsTask + "-2", depsTask + "-3"}, names,
		"no two launches share one, as two quick clicks would if they shared a call")
	assert.Len(t, gitWorktreePaths(t, f.clone), 4, "the clone and three worktrees")
}

func TestEnsurePassesOverATaskNameAnythingHas(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	m, _ := e.maker(&fakeLaunches{})

	// A remote-tracking branch of the first name, left from a task pushed and merged
	// before: the new worktree must never be related to that pull request. One of the
	// second name on another remote counts too, and something not git's has the third's
	// folder.
	testutil.RunGit(t, f.clone, "update-ref", "refs/remotes/origin/"+depsBranch, f.main)
	testutil.RunGit(t, f.clone, "update-ref", "refs/remotes/fork/"+depsBranch+"-2", f.main)

	taken := ManagedPath(f.clone, depsTask+"-3")
	require.NoError(t, os.MkdirAll(taken, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(taken, "notes.txt"), []byte("mine\n"), 0o600))

	planned, err := m.Plan(t.Context(), f.task())
	require.NoError(t, err)
	assert.Equal(t, depsTask+"-4", planned.Task)

	got, err := m.Ensure(t.Context(), f.task())
	require.NoError(t, err)
	assert.Equal(t, depsTask+"-4", got.Task)
	assert.Equal(t, ActionCreate, got.Action)
	assert.FileExists(t, filepath.Join(taken, "notes.txt"), "nothing is ever deleted")

	// A worktree whose folder went out of band still holds its name until git forgets it.
	require.NoError(t, os.RemoveAll(got.Path))

	next, err := m.Ensure(t.Context(), f.task())
	require.NoError(t, err)
	assert.Equal(t, depsTask+"-5", next.Task)
}

func TestEnsureSaysWhenEveryTaskNameIsTaken(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	m, _ := e.maker(&fakeLaunches{})

	for n := 1; n <= maxTaskNames; n++ {
		require.NoError(t, os.MkdirAll(ManagedPath(f.clone, taskName(depsTask, n)), 0o700))
	}

	_, err := m.Ensure(t.Context(), f.task())
	require.ErrorIs(t, err, ErrNoTaskName)
	assert.Equal(t, "Every name for today's worktree of ~/projects/example-repo, deps-20261121 to deps-20261121-99, is "+
		"taken. Remove some of them on the Worktrees tab, then launch again.", uierr.Message(err))
}

func TestEnsureMakesATasksWorktreeOnlyFromTheClonesOwnFolder(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	m, _ := e.maker(&fakeLaunches{})

	linked := addWorktree(t, f.clone, filepath.Join(e.root, "example-repo-feature"), "feature")

	req := f.task()
	req.Clone = linked

	for _, run := range []func() (Ensured, error){
		func() (Ensured, error) { return m.Plan(t.Context(), req) },
		func() (Ensured, error) { return m.Ensure(t.Context(), req) },
	} {
		_, err := run()
		require.ErrorIs(t, err, ErrNotMainWorktree)
		assert.Equal(t, "~/projects/example-repo-feature is a linked worktree, not a clone of its own, so no worktree is "+
			"made from it.", uierr.Message(err))
	}

	assert.NoDirExists(t, ManagedRoot(linked))
	assert.NoDirExists(t, ManagedRoot(f.clone))
}

func TestEnsureChecksATasksRequest(t *testing.T) {
	t.Parallel()

	m, _ := newEnv(t).maker(&fakeLaunches{})
	ok := EnsureRequest{Clone: "/srv/clone", Owner: exampleRepo().Owner, Name: exampleName, Task: depsTask}

	for name, edit := range map[string]func(*EnsureRequest){
		"a day too short":      func(r *EnsureRequest) { r.Task = "deps-2026112" },
		"a name already taken": func(r *EnsureRequest) { r.Task = depsTask + "-2" },
		"an item's name":       func(r *EnsureRequest) { r.Task = "pr-20261121" },
		"a path":               func(r *EnsureRequest) { r.Task = "../" + depsTask },
		"a folder below":       func(r *EnsureRequest) { r.Task = depsTask + "/x" },
		"an option":            func(r *EnsureRequest) { r.Task = "--" + depsTask },
		"a kind too":           func(r *EnsureRequest) { r.Kind = item.KindIssue },
		"a number too":         func(r *EnsureRequest) { r.Number = 7 },
		"a base":               func(r *EnsureRequest) { r.Base = mainBranch },
		"a head": func(r *EnsureRequest) {
			r.Head = "fc1c3975011e66491785ace891c893e4fc716587"
		},
		"no repository": func(r *EnsureRequest) { r.Name = "" },
		"a bad remote":  func(r *EnsureRequest) { r.Remote = "up..stream" },
	} {
		req := ok
		edit(&req)

		_, err := m.Ensure(t.Context(), req)
		require.ErrorIs(t, err, ErrInvalidRequest, name)

		_, err = m.Plan(t.Context(), req)
		require.ErrorIs(t, err, ErrInvalidRequest, name)
	}
}

func TestATasksWorktreeIsRelatedToItsPullRequestOncePushed(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	gh := &fakeGitHub{}
	svc := e.linked(linkDeps{gh: gh, launches: &fakeLaunches{}})

	got, err := NewMaker(svc).Ensure(t.Context(), f.task())
	require.NoError(t, err)

	// The fixture's origin stands in for GitHub; the listing links only through a remote
	// on GitHub.
	testutil.RunGit(t, f.clone, "remote", "set-url", "origin", "https://github.com/acme-org/example-repo.git")

	row := listRow(t, svc, got.Path)
	assert.True(t, row.Managed)
	assert.True(t, row.OwnBranch)
	assert.Nil(t, row.Item, "not pushed yet, so related to nothing")
	assert.Equal(t, StageNone, row.Stage)
	assert.False(t, autoCleanable(row), "and the sweep leaves it alone")

	// The session commits, pushes the branch under its own name, and opens a pull request.
	head := testutil.Commit(t, got.Path, "go.sum", "upgraded\n")
	pushed(t, f.clone, depsBranch, head)
	gh.setHead(depsBranch, headPR(12, ItemOpen, head, depsBranch, ownRepo))

	row = listRow(t, svc, got.Path)
	require.NotNil(t, row.Item)
	assert.Equal(t, LinkBranch, row.LinkedBy, "related through its branch, as pushed")
	assert.Equal(t, 12, row.Item.Number)
	assert.Equal(t, StageActive, row.Stage)
	assert.False(t, autoCleanable(row), "never while its pull request is open")

	gh.set(exampleName, 12, headPR(12, ItemMerged, head, depsBranch, ownRepo).State)

	row = listRow(t, svc, got.Path)
	require.NotNil(t, row.Item)
	assert.Equal(t, ItemMerged, row.Item.State)
	assert.Equal(t, StageFinished, row.Stage)
	assert.True(t, autoCleanable(row), "the app made it, and its HEAD is the merged pull request's head")
}

// listRow lists svc's worktrees and returns the row of the worktree at path.
func listRow(t *testing.T, svc *Service, path string) Worktree {
	t.Helper()

	snap, err := svc.List(t.Context())
	require.NoError(t, err)
	require.Empty(t, snap.LinkError)

	return mustRow(t, snap, path)
}
