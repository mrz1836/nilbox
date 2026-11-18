package pushwatch_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/pushwatch"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// The commits the tests move branches between.
const (
	commitA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	commitB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	commitC = "cccccccccccccccccccccccccccccccccccccccc"
)

// errBroken is a failure the fakes answer with.
var errBroken = errors.New("the database is locked")

// exampleRepo is the repository the tests' pull requests are in.
func exampleRepo() repo.Name {
	return repo.Name{Owner: "acme-org", Repo: "example-repo"}
}

// fakeTargets answers with the pull requests set, and counts the reads.
type fakeTargets struct {
	mu    sync.Mutex
	list  []pushwatch.Target
	err   error
	reads int
}

func (f *fakeTargets) PushTargets(context.Context) ([]pushwatch.Target, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.reads++
	if f.err != nil {
		return nil, f.err
	}

	return slices.Clone(f.list), nil
}

// set replaces the pull requests.
func (f *fakeTargets) set(list ...pushwatch.Target) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.list = list
}

// setHead has GitHub report head for the pull request with id.
func (f *fakeTargets) setHead(id int64, head string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for i := range f.list {
		if f.list[i].ItemID == id {
			f.list[i].Head = head
		}
	}
}

// fail makes every read fail with err, or succeed again with nil.
func (f *fakeTargets) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.err = err
}

// count returns how many reads were made.
func (f *fakeTargets) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.reads
}

// fakeClones lists the clones added.
type fakeClones struct {
	mu   sync.Mutex
	dirs []string
}

func (f *fakeClones) CloneDirs(context.Context) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.dirs), nil
}

// fakeOrigins answers with each clone's origin, and counts the reads of each.
type fakeOrigins struct {
	mu      sync.Mutex
	origins map[string]repo.Name
	reads   map[string]int
}

func (f *fakeOrigins) Origin(_ context.Context, dir string) (repo.Name, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.reads[dir]++
	name, ok := f.origins[dir]

	return name, ok, nil
}

// count returns how many times the origin of the clone at dir was read.
func (f *fakeOrigins) count(dir string) int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.reads[dir]
}

// fakeRereader records the items asked to be read again.
type fakeRereader struct {
	mu  sync.Mutex
	ids []int64
}

func (f *fakeRereader) Reread(id int64) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.ids = append(f.ids, id)
}

// seen returns the items asked for, in order.
func (f *fakeRereader) seen() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.ids)
}

// fakeSwitch is the user's switch.
type fakeSwitch struct {
	mu  sync.Mutex
	on  bool
	err error
}

func (f *fakeSwitch) WatchPushes(context.Context) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.on, f.err
}

// flip turns the switch on or off.
func (f *fakeSwitch) flip(on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.on = on
}

// fakeGit answers rev-parse of origin's branches in reftable clones from refs, by clone
// and ref, and records each command.
type fakeGit struct {
	mu    sync.Mutex
	refs  map[string]string
	calls [][]string
}

func (f *fakeGit) Run(_ context.Context, dir string, args ...string) (stdout, stderr []byte, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, append([]string{dir}, args...))

	if commit, ok := f.refs[dir+" "+args[len(args)-1]]; ok {
		return []byte(commit + "\n"), nil, nil
	}

	return nil, nil, errBroken
}

// set has origin's branch in the clone at dir be at commit.
func (f *fakeGit) set(dir, branch, commit string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.refs[dir+" refs/remotes/origin/"+branch] = commit
}

// count returns how many commands ran.
func (f *fakeGit) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.calls)
}

// commands returns each command run, its folder first, in order.
func (f *fakeGit) commands() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.calls)
}

// rig is a Watcher's fakes over clones on disk.
type rig struct {
	root    string
	targets *fakeTargets
	clones  *fakeClones
	origins *fakeOrigins
	reread  *fakeRereader
	toggle  *fakeSwitch
	git     *fakeGit
	logs    *testutil.Logs
}

// newRig returns fakes with the switch on, no pull request, and no clone.
func newRig(t *testing.T) *rig {
	t.Helper()

	return &rig{
		root:    t.TempDir(),
		targets: &fakeTargets{},
		clones:  &fakeClones{},
		origins: &fakeOrigins{origins: map[string]repo.Name{}, reads: map[string]int{}},
		reread:  &fakeRereader{},
		toggle:  &fakeSwitch{on: true},
		git:     &fakeGit{refs: map[string]string{}},
		logs:    &testutil.Logs{},
	}
}

// watcher returns a Watcher over the rig's fakes.
func (r *rig) watcher() *pushwatch.Watcher {
	return pushwatch.NewWatcher(pushwatch.Config{
		Targets: r.targets, Clones: r.clones, Origins: r.origins, Rereader: r.reread, Switch: r.toggle, Git: r.git,
		Logger: r.logs.Logger(),
	})
}

// addClone makes a clone named name in the clone root whose origin is origin, with a
// config file and origin's refs as git keeps them, lists it, and returns its folder.
func (r *rig) addClone(t *testing.T, name string, origin repo.Name) string {
	t.Helper()

	dir := filepath.Join(r.root, name)
	testutil.WriteFile(t, filepath.Join(dir, ".git", "config"), "[core]\n")
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git", "refs", "remotes", "origin"), 0o700))

	r.list(dir)

	r.origins.mu.Lock()
	r.origins.origins[dir] = origin
	r.origins.mu.Unlock()

	return dir
}

// list adds dir to the clones listed.
func (r *rig) list(dir string) {
	r.clones.mu.Lock()
	defer r.clones.mu.Unlock()

	r.clones.dirs = append(r.clones.dirs, dir)
}

// setRef moves origin's branch in the clone at dir to commit, as git does: a lock file
// written and renamed over the ref.
func setRef(t *testing.T, dir, branch, commit string) {
	t.Helper()

	path := filepath.Join(dir, ".git", "refs", "remotes", "origin", filepath.FromSlash(branch))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	testutil.WriteFile(t, path+".lock", commit+"\n")
	require.NoError(t, os.Rename(path+".lock", path))
}

// packRef takes origin's branch's loose ref in the clone at dir away, as git pack-refs
// does.
func packRef(t *testing.T, dir, branch string) {
	t.Helper()

	require.NoError(t, os.Remove(filepath.Join(dir, ".git", "refs", "remotes", "origin", filepath.FromSlash(branch))))
}

// target is the pull request with id on branch in example-repo, at head.
func target(id int64, branch, head string) pushwatch.Target {
	return pushwatch.Target{ItemID: id, Repo: exampleRepo(), Branch: branch, Head: head}
}

// running runs w in the test's synctest bubble until the test ends, and waits for its
// first pass.
func running(t *testing.T, w *pushwatch.Watcher) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		w.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	synctest.Wait()
}

// after lets d pass in the test's bubble and waits for the Watcher to settle.
func after(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

// noticed is how long a move takes to be read at most: a Tick to be seen, Settle to read.
const noticed = pushwatch.DefaultTick + pushwatch.DefaultSettle

func TestAPushReadsItsPullRequestAgainOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		dir := r.addClone(t, "example-repo", exampleRepo())
		setRef(t, dir, "feat/watch", commitA)
		r.targets.set(target(7, "feat/watch", commitA))

		running(t, r.watcher())

		setRef(t, dir, "feat/watch", commitB)
		after(pushwatch.DefaultTick)
		assert.Empty(t, r.reread.seen(), "GitHub is given a moment to take the push in")

		after(pushwatch.DefaultSettle)
		assert.Equal(t, []int64{7}, r.reread.seen(), "read again once the push settled")

		r.targets.setHead(7, commitB)
		after(10 * time.Minute)
		assert.Equal(t, []int64{7}, r.reread.seen(), "once GitHub reports the commit, nothing more is read")
	})
}

func TestAPushGitHubHasNotTakenInIsReadThreeTimesAtMost(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		dir := r.addClone(t, "example-repo", exampleRepo())
		setRef(t, dir, "main-fix", commitA)
		r.targets.set(target(7, "main-fix", commitA))

		running(t, r.watcher())

		setRef(t, dir, "main-fix", commitB)
		after(noticed)
		assert.Len(t, r.reread.seen(), 1)

		after(10 * time.Second)
		assert.Len(t, r.reread.seen(), 2, "read again while GitHub reports the old head")

		after(30 * time.Second)
		assert.Len(t, r.reread.seen(), 3)

		after(time.Hour)
		assert.Equal(t, []int64{7, 7, 7}, r.reread.seen(), "then the refresh catches it up")
	})
}

func TestARereadThatCaughtUpEndsTheRetries(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		dir := r.addClone(t, "example-repo", exampleRepo())
		setRef(t, dir, "feature", commitA)
		r.targets.set(target(7, "feature", commitA))

		w := r.watcher()
		running(t, w)

		setRef(t, dir, "feature", commitB)
		after(noticed)
		require.Len(t, r.reread.seen(), 1)

		r.targets.setHead(7, commitB)
		w.Wake()
		synctest.Wait()

		after(time.Hour)
		assert.Len(t, r.reread.seen(), 1, "the inbox's change woke the watch, which saw GitHub caught up")
	})
}

func TestTheFirstLookOnlyLearnsWhereABranchIs(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		dir := r.addClone(t, "example-repo", exampleRepo())
		setRef(t, dir, "feature", commitB)
		r.targets.set(target(7, "feature", commitA))

		running(t, r.watcher())
		after(time.Hour)

		assert.Empty(t, r.reread.seen(), "a branch ahead of GitHub before the watch began is the refresh's")
	})
}

func TestABranchMovedToTheHeadGitHubReportsIsNotRead(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		dir := r.addClone(t, "example-repo", exampleRepo())
		setRef(t, dir, "feature", commitA)
		r.targets.set(target(7, "feature", commitB))

		running(t, r.watcher())

		setRef(t, dir, "feature", commitB)
		after(time.Hour)
		assert.Empty(t, r.reread.seen(), "a fetch of what GitHub reported already costs nothing")
	})
}

func TestPackingABranchAwayIsNoMove(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		dir := r.addClone(t, "example-repo", exampleRepo())
		setRef(t, dir, "feature", commitA)
		r.targets.set(target(7, "feature", commitA))

		running(t, r.watcher())

		packRef(t, dir, "feature")
		after(time.Minute)
		setRef(t, dir, "feature", commitA)
		after(time.Minute)
		assert.Empty(t, r.reread.seen(), "a ref packed away, and written again where it was, never moved")

		setRef(t, dir, "feature", commitC)
		after(noticed)
		assert.Equal(t, []int64{7}, r.reread.seen())
	})
}

func TestABranchFirstSeenPackedIsReadOnceItMoves(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		dir := r.addClone(t, "example-repo", exampleRepo())
		r.targets.set(target(7, "feature", commitA))

		running(t, r.watcher())

		setRef(t, dir, "feature", commitB)
		after(noticed)
		assert.Equal(t, []int64{7}, r.reread.seen(), "a loose ref only a push writes")
	})
}

func TestEveryPullRequestOnAMovedBranchIsRead(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		dir := r.addClone(t, "example-repo", exampleRepo())
		setRef(t, dir, "shared", commitA)
		setRef(t, dir, "other", commitA)
		r.targets.set(target(7, "shared", commitA), target(8, "shared", commitA), target(9, "other", commitA))

		running(t, r.watcher())

		setRef(t, dir, "shared", commitB)
		after(noticed)
		assert.ElementsMatch(t, []int64{7, 8}, r.reread.seen(), "both pull requests from the branch, never another")
	})
}

func TestEveryCloneOfTheRepositoryIsWatched(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		first := r.addClone(t, "example-repo", exampleRepo())
		second := r.addClone(t, "example-repo-2", exampleRepo())
		other := r.addClone(t, "other-repo", repo.Name{Owner: "acme-org", Repo: "other-repo"})

		for _, dir := range []string{first, second, other} {
			setRef(t, dir, "feature", commitA)
		}

		r.targets.set(target(7, "feature", commitA))
		running(t, r.watcher())

		setRef(t, other, "feature", commitB)
		after(time.Minute)
		assert.Empty(t, r.reread.seen(), "another repository's branch of the same name is never this one")

		setRef(t, second, "feature", commitB)
		after(noticed)
		assert.Equal(t, []int64{7}, r.reread.seen())
	})
}

func TestALinkedWorktreeCountsAsItsClone(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		dir := r.addClone(t, "example-repo", exampleRepo())

		gitDir := filepath.Join(dir, ".git", "worktrees", "feature")
		testutil.WriteFile(t, filepath.Join(gitDir, "commondir"), "../..\n")

		linked := filepath.Join(r.root, "example-repo-feature")
		testutil.WriteFile(t, filepath.Join(linked, ".git"), "gitdir: "+gitDir+"\n")
		r.list(linked)

		setRef(t, dir, "feature", commitA)
		r.targets.set(target(7, "feature", commitA))
		running(t, r.watcher())

		setRef(t, dir, "feature", commitB)
		after(noticed)
		assert.Equal(t, []int64{7}, r.reread.seen(), "a push from the worktree moves the clone's ref, read once")
		assert.Equal(t, 1, r.origins.count(dir))
		assert.Zero(t, r.origins.count(linked), "one clone, one git folder, its origin read once")
	})
}

func TestAClonesOriginIsReadAgainOnlyOnceItsConfigChanged(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		dir := r.addClone(t, "example-repo", exampleRepo())
		setRef(t, dir, "feature", commitA)
		r.targets.set(target(7, "feature", commitA))

		running(t, r.watcher())
		after(3 * pushwatch.DefaultIndexEvery)
		assert.Equal(t, 1, r.origins.count(dir), "the clones are listed again, the origin read once")

		testutil.WriteFile(t, filepath.Join(dir, ".git", "config"), "[core]\n[remote \"origin\"]\n")
		after(pushwatch.DefaultIndexEvery)
		assert.Equal(t, 2, r.origins.count(dir), "a changed config is read again")

		added := r.addClone(t, "example-repo-2", exampleRepo())
		setRef(t, added, "feature", commitA)
		after(pushwatch.DefaultIndexEvery)

		setRef(t, added, "feature", commitB)
		after(noticed)
		assert.Equal(t, []int64{7}, r.reread.seen(), "a clone added is watched once the clones are listed again")
	})
}

func TestNothingIsLookedAtWithoutAPullRequestToWatch(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		dir := r.addClone(t, "example-repo", exampleRepo())

		running(t, r.watcher())
		after(10 * time.Minute)

		assert.Zero(t, r.origins.count(dir), "no clone is read while nothing is watched")
		assert.Equal(t, 11, r.targets.count(), "the pull requests are read again once a minute")
	})
}

func TestTheSwitchOffWatchesNothingUntilWoken(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		dir := r.addClone(t, "example-repo", exampleRepo())
		setRef(t, dir, "feature", commitA)
		r.targets.set(target(7, "feature", commitA))
		r.toggle.flip(false)

		w := r.watcher()
		running(t, w)

		setRef(t, dir, "feature", commitB)
		after(time.Hour)
		assert.Zero(t, r.targets.count(), "off, the pull requests aren't read")
		assert.Zero(t, r.origins.count(dir), "nor any clone")
		assert.Empty(t, r.reread.seen())

		r.toggle.flip(true)
		w.Wake()
		synctest.Wait()
		after(time.Minute)
		assert.Empty(t, r.reread.seen(), "a move made while off is the refresh's")

		setRef(t, dir, "feature", commitC)
		after(noticed)
		assert.Equal(t, []int64{7}, r.reread.seen())

		r.toggle.flip(false)
		w.Wake()
		synctest.Wait()

		setRef(t, dir, "feature", commitA)
		after(time.Hour)
		assert.Equal(t, []int64{7}, r.reread.seen(), "turned off, it stops at once")
	})
}

func TestASwitchThatCantBeReadWatches(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		dir := r.addClone(t, "example-repo", exampleRepo())
		setRef(t, dir, "feature", commitA)
		r.targets.set(target(7, "feature", commitA))
		r.toggle.mu.Lock()
		r.toggle.on, r.toggle.err = false, errBroken
		r.toggle.mu.Unlock()

		running(t, r.watcher())

		setRef(t, dir, "feature", commitB)
		after(noticed)
		assert.Equal(t, []int64{7}, r.reread.seen(), "its default, on")
		assert.Contains(t, r.logs.String(), "could not read whether to watch for pushes")
	})
}

func TestAWakeWatchesANewPullRequestAtOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		dir := r.addClone(t, "example-repo", exampleRepo())
		setRef(t, dir, "feature", commitA)

		w := r.watcher()
		running(t, w)

		r.targets.set(target(7, "feature", commitA))
		w.Wake()
		synctest.Wait()

		setRef(t, dir, "feature", commitB)
		after(noticed)
		assert.Equal(t, []int64{7}, r.reread.seen(), "listed by the inbox, it is watched before the minute is up")
	})
}

func TestAListThatCantBeReadKeepsTheLast(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		dir := r.addClone(t, "example-repo", exampleRepo())
		setRef(t, dir, "feature", commitA)
		r.targets.set(target(7, "feature", commitA))

		running(t, r.watcher())
		r.targets.fail(errBroken)
		after(2 * time.Minute)

		setRef(t, dir, "feature", commitB)
		after(noticed)
		assert.Equal(t, []int64{7}, r.reread.seen())
		assert.Contains(t, r.logs.String(), "could not read the pull requests to watch for pushes")
	})
}

func TestABranchThatCouldLeaveOriginsRefsIsNeverWatched(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		dir := r.addClone(t, "example-repo", exampleRepo())
		outside := filepath.Join(dir, ".git", "refs", "remotes", "evil")
		testutil.WriteFile(t, outside, commitA+"\n")
		r.targets.set(target(7, "../evil", commitA), pushwatch.Target{ItemID: 8, Branch: "feature"})

		running(t, r.watcher())

		testutil.WriteFile(t, outside+".lock", commitB+"\n")
		require.NoError(t, os.Rename(outside+".lock", outside))
		after(time.Hour)

		assert.Empty(t, r.reread.seen())
		assert.Zero(t, r.origins.count(dir), "nothing well-formed to watch, so no clone is read")
	})
}

func TestOnlyTheFirstPullRequestsAreWatched(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		dir := r.addClone(t, "example-repo", exampleRepo())

		list := make([]pushwatch.Target, 0, pushwatch.MaxTargets+1)
		for i := range pushwatch.MaxTargets + 1 {
			branch := "branch-" + strconv.Itoa(i)
			setRef(t, dir, branch, commitA)
			list = append(list, target(int64(i+1), branch, commitA))
		}

		r.targets.set(list...)
		running(t, r.watcher())

		setRef(t, dir, list[pushwatch.MaxTargets].Branch, commitB)
		setRef(t, dir, list[0].Branch, commitB)
		after(noticed)
		assert.Equal(t, []int64{1}, r.reread.seen(), "the one past the cap isn't watched")
	})
}

func TestAReftableCloneIsReadWithGitOnceItsTablesChange(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		dir := r.addClone(t, "example-repo", exampleRepo())
		tables := filepath.Join(dir, ".git", "reftable", "tables.list")
		testutil.WriteFile(t, tables, "0x01.ref\n")
		r.git.set(dir, "feature", commitA)
		r.targets.set(target(7, "feature", commitA))

		running(t, r.watcher())
		after(time.Minute)
		assert.Equal(t, 1, r.git.count(), "read once, then only once the tables change")

		r.git.set(dir, "feature", commitB)
		testutil.WriteFile(t, tables+".lock", "0x01.ref\n0x02.ref\n")
		require.NoError(t, os.Rename(tables+".lock", tables))
		after(noticed)

		assert.Equal(t, []int64{7}, r.reread.seen())

		revParse := []string{dir, "rev-parse", "--verify", "--quiet", "refs/remotes/origin/feature"}
		assert.Equal(t, [][]string{revParse, revParse}, r.git.commands(), "gitread's allowlisted read, in the clone")
	})
}

func TestAReftableCloneIsntWatchedWithoutGit(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		dir := r.addClone(t, "example-repo", exampleRepo())
		tables := filepath.Join(dir, ".git", "reftable", "tables.list")
		testutil.WriteFile(t, tables, "0x01.ref\n")
		r.targets.set(target(7, "feature", commitA))

		w := pushwatch.NewWatcher(pushwatch.Config{
			Targets: r.targets, Clones: r.clones, Origins: r.origins, Rereader: r.reread, Switch: r.toggle,
		})
		running(t, w)

		testutil.WriteFile(t, tables+".lock", "0x01.ref\n0x02.ref\n")
		require.NoError(t, os.Rename(tables+".lock", tables))
		after(time.Hour)

		assert.Empty(t, r.reread.seen())
	})
}
