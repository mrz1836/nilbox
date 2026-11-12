package worktrees

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/branchrules"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/worklog"
)

var (
	errFakeGitHub   = errors.New("fake: GitHub is unreachable")
	errFakeLaunches = errors.New("fake: launches unavailable")
)

// mergedAt is when the fake pull requests merged.
func mergedAt() time.Time { return time.Date(2026, 11, 6, 18, 0, 0, 0, time.UTC) }

// fakeGitHub answers ItemStates from a table keyed by item.Ref.Key, as the client does,
// and records each call.
type fakeGitHub struct {
	mu     sync.Mutex
	states map[string]ItemState
	err    error
	calls  [][]item.Ref

	// merged answers MergedPullRequests, which records each call's queries in asked.
	merged map[BranchKey][]MergedPR
	asked  [][]BranchQuery
}

func (f *fakeGitHub) MergedPullRequests(_ context.Context, queries []BranchQuery) (map[BranchKey][]MergedPR, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.asked = append(f.asked, queries)

	return f.merged, f.err
}

func (f *fakeGitHub) ItemStates(_ context.Context, refs []item.Ref) (map[string]ItemState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.calls = append(f.calls, refs)

	return f.states, f.err
}

// set records an item's state under acme-org/<name>.
func (f *fakeGitHub) set(name string, number int, state ItemState) {
	if f.states == nil {
		f.states = make(map[string]ItemState)
	}

	f.states[refKey("acme-org", name, number)] = state
}

// refKey is the key of owner/name#number.
func refKey(owner, name string, number int) string {
	return item.Ref{Repo: repo.Name{Owner: owner, Repo: name}, Number: number}.Key()
}

// refKeys lists the keys of refs.
func refKeys(refs []item.Ref) []string {
	keys := make([]string, 0, len(refs))
	for _, ref := range refs {
		keys = append(keys, ref.Key())
	}

	return keys
}

// fakeLaunches is an in-memory worktree record.
type fakeLaunches struct {
	mu        sync.Mutex
	records   []worklog.Record
	forgotten []string
	recorded  []string
	listErr   error
	forgetErr error
	recordErr error
}

func (f *fakeLaunches) Worktrees(context.Context) ([]worklog.Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]worklog.Record(nil), f.records...), f.listErr
}

func (f *fakeLaunches) Record(_ context.Context, r worklog.Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.recordErr != nil {
		return f.recordErr
	}

	f.recorded = append(f.recorded, filepath.Base(r.ClonePath)+"/"+r.Worktree)

	for i, old := range f.records {
		if old.ClonePath == r.ClonePath && old.Worktree == r.Worktree {
			r.CreatedAt, r.Agent, r.LastAgentAt, r.Launches = old.CreatedAt, old.Agent, old.LastAgentAt, old.Launches
			f.records[i] = r

			return nil
		}
	}

	f.records = append(f.records, r)

	return nil
}

func (f *fakeLaunches) Forget(_ context.Context, clonePath, worktree string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.forgetErr != nil {
		return f.forgetErr
	}

	f.forgotten = append(f.forgotten, filepath.Base(clonePath)+"/"+worktree)

	kept := f.records[:0]
	for _, r := range f.records {
		if r.ClonePath != clonePath || r.Worktree != worktree {
			kept = append(kept, r)
		}
	}

	f.records = kept

	return nil
}

// all returns a copy of the records.
func (f *fakeLaunches) all() []worklog.Record {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]worklog.Record(nil), f.records...)
}

// linkDeps are the optional dependencies of a linking Service.
type linkDeps struct {
	rules    *branchrules.Rules
	gh       *fakeGitHub
	launches *fakeLaunches
	history  History
	events   *testutil.Events
	auto     bool
	procs    processTable
	running  procs.Lister

	// guard, when set, runs the Service under gitread's guards, as core does.
	guard Guard
}

// linked builds a Service over the env that links worktrees through deps. Agent locks
// are judged against deps.procs, which by default says every PID is dead.
func (e *env) linked(deps linkDeps) *Service {
	cfg := Config{
		Settings: fakeSettings{root: cloneRoot, auto: deps.auto, rules: deps.rules},
		Git:      e.git,
		Finder:   execpath.Search{},
		Home:     e.home,
		Clock:    testutil.NewClock(listedAt()),
		Logger:   slog.New(slog.DiscardHandler),
	}

	if deps.gh != nil {
		cfg.GitHub = deps.gh
	}

	if deps.launches != nil {
		cfg.Records = deps.launches
	}

	if deps.running != nil {
		cfg.Procs = deps.running
	}

	if deps.events != nil {
		cfg.Events = deps.events
	}

	if deps.history != nil {
		cfg.History = deps.history
	}

	if deps.guard != nil {
		cfg.Guard = deps.guard
	}

	svc := NewService(cfg)

	svc.zone = time.UTC

	svc.procs = fakeProcesses{}
	if deps.procs != nil {
		svc.procs = deps.procs
	}

	return svc
}

// newGitHubRepo creates a clone under the root whose origin is github.com/acme-org/<name>.
func (e *env) newGitHubRepo(t *testing.T, name string) string {
	t.Helper()

	dir := e.newRepo(t, name)
	testutil.RunGit(t, dir, "remote", "add", "origin", "https://github.com/acme-org/"+name+".git")

	return dir
}

// agentWorktree adds the agent's worktree <name> under the clone's .claude/worktrees on its
// worktree-<name> branch, with one commit of its own, and returns its path and HEAD.
func agentWorktree(t *testing.T, repo, name string) (string, string) {
	t.Helper()

	path := addWorktree(t, repo, filepath.Join(repo, ".claude", "worktrees", name), ownBranchPrefix+name)
	require.NoError(t, os.WriteFile(filepath.Join(path, "fix.txt"), []byte(name+"\n"), 0o600))
	testutil.RunGit(t, path, "add", "fix.txt")
	testutil.RunGit(t, path, "commit", "-q", "-m", "fix "+name)

	return path, strings.TrimSpace(testutil.RunGit(t, path, "rev-parse", "HEAD"))
}

// pushed records head on a remote-tracking branch, as a push and fetch would.
func pushed(t *testing.T, repo, branch, head string) {
	t.Helper()

	testutil.RunGit(t, repo, "update-ref", "refs/remotes/origin/"+branch, head)
}

// pr is a pull request's state on GitHub.
func pr(state ItemStatus, head string) ItemState {
	s := ItemState{PullRequest: true, Title: "Fix things", URL: "https://github.com/acme-org/example-repo/pull/1", State: state, HeadOID: head}
	if state != ItemOpen {
		s.ClosedAt = mergedAt()
	}

	return s
}

// closedIssue is a closed issue's state on GitHub.
func closedIssue() ItemState {
	return ItemState{
		Title: "It breaks", URL: "https://github.com/acme-org/example-repo/issues/1", State: ItemClosed,
		ClosedAt: mergedAt(),
	}
}

// launchOf is the record of a launch in the worktree Claude Code made under the clone's
// .claude/worktrees for kind and number, as one kept before the app made worktrees.
func launchOf(repo, name string, kind item.Kind, number int) worklog.Record {
	return worklog.Record{
		ClonePath: repo, Worktree: name, Branch: ownBranchPrefix + name, Owner: "acme-org", Name: filepath.Base(repo),
		Kind: kind, Number: number, Agent: agent.Claude, CreatedAt: listedAt(), LastAgentAt: listedAt(), Launches: 1,
	}
}

func TestListLinksAndStagesAgentWorktrees(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	repo := e.newGitHubRepo(t, "example-repo")
	gh := &fakeGitHub{}
	alive := os.Getpid()

	finished, finishedHead := agentWorktree(t, repo, "pr-1")
	pushed(t, repo, "feat/one", finishedHead)
	gh.set("example-repo", 1, pr(ItemMerged, finishedHead))

	unpushed, _ := agentWorktree(t, repo, "pr-2")

	gh.set("example-repo", 2, pr(ItemMerged, "0000000000000000000000000000000000000002"))

	prHead, prHeadOID := agentWorktree(t, repo, "pr-3")
	gh.set("example-repo", 3, pr(ItemMerged, prHeadOID))

	open, openHead := agentWorktree(t, repo, "pr-4")
	pushed(t, repo, "feat/four", openHead)
	gh.set("example-repo", 4, pr(ItemOpen, openHead))

	changed, changedHead := agentWorktree(t, repo, "issue-5")
	pushed(t, repo, "feat/five", changedHead)
	dirty(t, changed)
	gh.set("example-repo", 5, closedIssue())

	inUse, inUseHead := agentWorktree(t, repo, "pr-6")
	pushed(t, repo, "feat/six", inUseHead)
	lock(t, repo, inUse, "claude session pr-6 (pid "+strconv.Itoa(alive)+")")
	gh.set("example-repo", 6, pr(ItemMerged, inUseHead))

	stale, staleHead := agentWorktree(t, repo, "pr-7")
	pushed(t, repo, "feat/seven", staleHead)
	lock(t, repo, stale, "claude session pr-7 (pid 999999)")
	gh.set("example-repo", 7, pr(ItemMerged, staleHead))

	byHand, byHandHead := agentWorktree(t, repo, "pr-8")
	pushed(t, repo, "feat/eight", byHandHead)
	lock(t, repo, byHand, "keep this")
	gh.set("example-repo", 8, pr(ItemMerged, byHandHead))

	unknown, _ := agentWorktree(t, repo, "pr-9") // GitHub does not know it
	named, _ := agentWorktree(t, repo, "bright-running-fox")
	elsewhere := addWorktree(t, repo, filepath.Join(e.home, "wt", "pr-10"), "pr-10")

	gh.set("example-repo", 10, pr(ItemMerged, ""))

	svc := e.linked(linkDeps{gh: gh, procs: fakeProcesses{alive: {state: livenessAlive}}})

	snap, err := svc.List(t.Context())
	require.NoError(t, err)
	assert.Empty(t, snap.LinkError)
	assert.Equal(t, 3, snap.FinishedCount, "pr-1, pr-3, and pr-7")
	assert.Equal(t, 2, snap.ReviewCount, "pr-2 and issue-5")

	require.Len(t, gh.calls, 1, "one request for every worktree")
	assert.Len(t, gh.calls[0], 9, "pr-1 to pr-9 and issue-5, by their names; not the others")

	row := mustRow(t, snap, finished)
	require.NotNil(t, row.Item)
	assert.Equal(t, LinkedItem{
		Kind: item.KindPR, Owner: "acme-org", Name: "example-repo", Number: 1, Title: "Fix things",
		URL: "https://github.com/acme-org/example-repo/pull/1", State: ItemMerged, ClosedAt: "2026-11-06T18:00:00Z",
		HeadOID: finishedHead,
	}, *row.Item)
	assert.Equal(t, 0, row.Unpushed)
	assert.True(t, row.OnGitHub)
	assert.True(t, row.OwnBranch)
	assert.Equal(t, finishedHead, row.Head)
	assert.Equal(t, StageFinished, row.Stage)
	assert.True(t, row.CanCleanUp)
	assert.True(t, row.CanRemove)

	row = mustRow(t, snap, unpushed)
	assert.Equal(t, 1, row.Unpushed)
	assert.False(t, row.OnGitHub)
	assert.Equal(t, StageNeedsReview, row.Stage)
	assert.False(t, row.CanCleanUp)

	row = mustRow(t, snap, prHead)
	assert.Equal(t, 1, row.Unpushed, "its remote branch is gone")
	assert.True(t, row.OnGitHub, "but HEAD is the pull request's head, which GitHub keeps")
	assert.Equal(t, StageFinished, row.Stage)

	row = mustRow(t, snap, open)
	assert.Equal(t, ItemOpen, row.Item.State)
	assert.Empty(t, row.Item.ClosedAt)
	assert.Equal(t, StageActive, row.Stage)
	assert.False(t, row.CanCleanUp)

	row = mustRow(t, snap, changed)
	assert.Equal(t, item.KindIssue, row.Item.Kind)
	assert.Equal(t, ItemClosed, row.Item.State)
	assert.Equal(t, StageNeedsReview, row.Stage, "done, but it has uncommitted changes")

	row = mustRow(t, snap, inUse)
	assert.True(t, row.AgentLock)
	assert.False(t, row.StaleLock)
	assert.False(t, row.CanUnlock)
	assert.Equal(t, StageInUse, row.Stage)
	assert.False(t, row.CanCleanUp)

	row = mustRow(t, snap, stale)
	assert.True(t, row.AgentLock)
	assert.True(t, row.StaleLock)
	assert.True(t, row.CanUnlock)
	assert.False(t, row.CanRemove, "a locked worktree is never removed as is")
	assert.Equal(t, StageFinished, row.Stage)
	assert.True(t, row.CanCleanUp, "CleanUp releases the ended session's lock first")

	row = mustRow(t, snap, byHand)
	assert.False(t, row.AgentLock)
	assert.Empty(t, row.Stage, "a lock set by hand is left alone")
	assert.False(t, row.CanCleanUp)
	assert.False(t, row.CanUnlock)

	assert.Nil(t, mustRow(t, snap, unknown).Item)
	assert.Empty(t, mustRow(t, snap, unknown).Stage)
	assert.Nil(t, mustRow(t, snap, named).Item, "a name that is not an item's")

	row = mustRow(t, snap, elsewhere)
	assert.Nil(t, row.Item, "only worktrees under .claude/worktrees are linked")
	assert.False(t, row.OwnBranch)

	main := snap.Repos[0].Worktrees[0]
	assert.True(t, main.Main)
	assert.Equal(t, -1, main.Unpushed, "the main worktree's commits are not counted")
	assert.Empty(t, main.Stage)
}

func TestListLinksByLaunchRecord(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	repo := e.newGitHubRepo(t, "example-repo")
	testutil.RunGit(t, repo, "remote", "add", "upstream", "https://github.com/upstream-org/example-repo.git")

	// The app launched it on upstream's issue 12, though the name says pr-3.
	path, head := agentWorktree(t, repo, "pr-3")
	pushed(t, repo, "feat/x", head)

	gh := &fakeGitHub{}
	gh.states = map[string]ItemState{refKey("upstream-org", "example-repo", 12): closedIssue()}

	record := launchOf(repo, "pr-3", item.KindIssue, 12)
	record.Owner = "Upstream-Org"
	launches := &fakeLaunches{records: []worklog.Record{record}}

	snap, err := e.linked(linkDeps{gh: gh, launches: launches}).List(t.Context())
	require.NoError(t, err)

	require.Len(t, gh.calls, 1)
	assert.Equal(t, []string{"upstream-org/example-repo#12"}, refKeys(gh.calls[0]),
		"the recorded item is asked for, matched whatever its casing")

	row := mustRow(t, snap, path)
	require.NotNil(t, row.Item)
	assert.Equal(t, item.KindIssue, row.Item.Kind)
	assert.Equal(t, 12, row.Item.Number)
	assert.True(t, row.Item.Launched)
	assert.Equal(t, StageFinished, row.Stage)
}

func TestListPicksTheRightRemote(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	repo := e.newGitHubRepo(t, "example-repo")
	testutil.RunGit(t, repo, "remote", "add", "upstream", "https://github.com/upstream-org/example-repo.git")
	testutil.RunGit(t, repo, "remote", "add", "mirror", "https://gitlab.example.com/acme-org/example-repo.git")

	byHead, head := agentWorktree(t, repo, "pr-1")
	byKind, _ := agentWorktree(t, repo, "issue-2")

	upstream := func(n int) string { return refKey("upstream-org", "example-repo", n) }
	origin := func(n int) string { return refKey("acme-org", "example-repo", n) }

	gh := &fakeGitHub{states: map[string]ItemState{
		origin(1):   pr(ItemOpen, "other"),
		upstream(1): pr(ItemMerged, head),
		origin(2):   pr(ItemOpen, "x"),
		upstream(2): closedIssue(),
	}}

	snap, err := e.linked(linkDeps{gh: gh}).List(t.Context())
	require.NoError(t, err)

	require.Len(t, gh.calls, 1)
	assert.ElementsMatch(t, []string{origin(1), upstream(1), origin(2), upstream(2)}, refKeys(gh.calls[0]),
		"origin and upstream are asked; the remote that is not on GitHub is not")

	row := mustRow(t, snap, byHead)
	assert.Equal(t, "upstream-org", row.Item.Owner, "the pull request whose head is HEAD wins over origin's")

	row = mustRow(t, snap, byKind)
	assert.Equal(t, "upstream-org", row.Item.Owner, "origin's #2 is a pull request, not an issue")
}

func TestListWithoutGitHub(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	repo := e.newGitHubRepo(t, "example-repo")
	path, head := agentWorktree(t, repo, "pr-1")
	pushed(t, repo, "feat/one", head)

	snap, err := e.linked(linkDeps{}).List(t.Context())
	require.NoError(t, err)
	assert.Nil(t, mustRow(t, snap, path).Item, "nothing is linked without GitHub")
	assert.Zero(t, snap.FinishedCount)

	gh := &fakeGitHub{err: errFakeGitHub}
	launches := &fakeLaunches{listErr: errFakeLaunches}

	snap, err = e.linked(linkDeps{gh: gh, launches: launches}).List(t.Context())
	require.NoError(t, err)
	assert.Contains(t, snap.LinkError, "Could not read the pull requests and issues from GitHub")
	assert.Contains(t, snap.LinkError, errFakeGitHub.Error())
	assert.Nil(t, mustRow(t, snap, path).Item)
	assert.True(t, mustRow(t, snap, path).OnGitHub, "the commit is still known to be pushed")
}

func TestUnlock(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	repo := e.newRepo(t, "example-repo")
	alive := os.Getpid()
	svc := e.linked(linkDeps{procs: fakeProcesses{alive: {state: livenessAlive}}})

	stale := addWorktree(t, repo, filepath.Join(e.home, "wt", "stale"), "stale")
	lock(t, repo, stale, "claude session stale (pid 999999 start Fri Nov  6 17:14:26 2026)")

	running := addWorktree(t, repo, filepath.Join(e.home, "wt", "running"), "running")
	lock(t, repo, running, "claude session running (pid "+strconv.Itoa(alive)+")")

	byHand := addWorktree(t, repo, filepath.Join(e.home, "wt", "by-hand"), "by-hand")
	lock(t, repo, byHand, "do not touch")

	free := addWorktree(t, repo, filepath.Join(e.home, "wt", "free"), "free")

	require.NoError(t, svc.Unlock(t.Context(), repo, stale))
	assert.False(t, mustRow(t, e.list(t), stale).Locked)

	require.ErrorIs(t, svc.Unlock(t.Context(), repo, running), ErrSessionRunning)
	require.ErrorIs(t, svc.Unlock(t.Context(), repo, byHand), ErrHandLock)
	require.ErrorIs(t, svc.Unlock(t.Context(), repo, free), ErrNotLocked)
	require.ErrorIs(t, svc.Unlock(t.Context(), repo, repo), ErrMainWorktree)
	require.ErrorIs(t, svc.Unlock(t.Context(), repo, filepath.Join(e.home, "nowhere")), ErrNotAWorktree)
	require.ErrorIs(t, svc.Unlock(t.Context(), e.home, stale), ErrOutsideCloneRoot)

	snap := e.list(t)
	assert.True(t, mustRow(t, snap, running).Locked)
	assert.True(t, mustRow(t, snap, byHand).Locked)
}

func TestCleanUpRemovesAFinishedWorktreeAndItsBranch(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	repo := e.newGitHubRepo(t, "example-repo")
	path, head := agentWorktree(t, repo, "pr-184")
	pushed(t, repo, "feat/locktimes", head)
	lock(t, repo, path, "claude session pr-184 (pid 999999 start Fri Nov  6 17:14:26 2026)")

	gh := &fakeGitHub{}
	gh.set("example-repo", 184, pr(ItemMerged, head))

	launches := &fakeLaunches{records: []worklog.Record{launchOf(repo, "pr-184", item.KindPR, 184)}}
	svc := e.linked(linkDeps{gh: gh, launches: launches})

	res, err := svc.CleanUp(t.Context(), repo, path, []string{"worktree-pr-184"})
	require.NoError(t, err)
	assert.Equal(t, RemoveResult{
		RepoPath: repo, Path: mustResolve(t, path), DisplayPath: "~/projects/example-repo/.claude/worktrees/pr-184",
		Removed: true, DeletedBranches: []string{"worktree-pr-184"}, Item: "acme-org/example-repo#184",
	}, normalizeResult(res))

	assert.NotContains(t, gitWorktreePaths(t, repo), resolvedPath(path))
	assert.NotContains(t, branches(t, repo), "worktree-pr-184")
	assert.Equal(t, []string{"example-repo/pr-184"}, launches.forgotten)
	assert.Equal(t, head, strings.TrimSpace(testutil.RunGit(t, repo, "rev-parse", "refs/remotes/origin/feat/locktimes")),
		"nothing on the remote side is touched")
}

func TestCleanUpKeepsABranchThatIsNotItsOwn(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	repo := e.newGitHubRepo(t, "example-repo")
	path := addWorktree(t, repo, filepath.Join(repo, ".claude", "worktrees", "pr-5"), "feat/five")
	head := strings.TrimSpace(testutil.RunGit(t, path, "rev-parse", "HEAD"))
	pushed(t, repo, "feat/five", head)

	gh := &fakeGitHub{}
	gh.set("example-repo", 5, pr(ItemMerged, head))

	res, err := e.linked(linkDeps{gh: gh}).CleanUp(t.Context(), repo, path, []string{"feat/five"})
	require.NoError(t, err)
	assert.True(t, res.Removed)
	assert.Empty(t, res.DeletedBranches)
	assert.Equal(t, "kept feat/five: it is not this worktree's to delete", res.Reason,
		"a branch the worktree does not offer is never deleted, whatever is asked")
	assert.Contains(t, branches(t, repo), "feat/five")
}

func TestCleanUpRefusesWhatIsNotFinished(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	repo := e.newGitHubRepo(t, "example-repo")
	alive := os.Getpid()
	gh := &fakeGitHub{}

	open, openHead := agentWorktree(t, repo, "pr-1")
	pushed(t, repo, "a", openHead)
	gh.set("example-repo", 1, pr(ItemOpen, openHead))

	running, runningHead := agentWorktree(t, repo, "pr-2")
	pushed(t, repo, "b", runningHead)
	lock(t, repo, running, "claude session pr-2 (pid "+strconv.Itoa(alive)+")")
	gh.set("example-repo", 2, pr(ItemMerged, runningHead))

	unpushed, _ := agentWorktree(t, repo, "pr-3")

	gh.set("example-repo", 3, pr(ItemMerged, "elsewhere"))

	changed, changedHead := agentWorktree(t, repo, "pr-4")
	pushed(t, repo, "d", changedHead)
	dirty(t, changed)
	gh.set("example-repo", 4, pr(ItemMerged, changedHead))

	byHand, byHandHead := agentWorktree(t, repo, "pr-6")
	pushed(t, repo, "f", byHandHead)
	lock(t, repo, byHand, "mine")
	gh.set("example-repo", 6, pr(ItemMerged, byHandHead))

	plain, _ := agentWorktree(t, repo, "scratch")

	svc := e.linked(linkDeps{gh: gh, procs: fakeProcesses{alive: {state: livenessAlive}}})

	_, err := svc.CleanUp(t.Context(), repo, repo, nil)
	require.ErrorIs(t, err, ErrMainWorktree)

	_, err = svc.CleanUp(t.Context(), repo, filepath.Join(e.home, "nowhere"), nil)
	require.ErrorIs(t, err, ErrNotAWorktree)

	_, err = svc.CleanUp(t.Context(), e.home, open, nil)
	require.ErrorIs(t, err, ErrOutsideCloneRoot)

	offline := e.linked(linkDeps{gh: &fakeGitHub{err: errFakeGitHub}, procs: fakeProcesses{alive: {state: livenessAlive}}})

	_, err = offline.CleanUp(t.Context(), repo, open, nil)
	require.ErrorIs(t, err, ErrGitHubUnavailable)

	tests := []struct {
		name string
		path string
		want string
	}{
		{name: "open", path: open, want: "still open"},
		{name: "a session is running", path: running, want: "session is still running"},
		{name: "commits not on GitHub", path: unpushed, want: "not on GitHub"},
		{name: "uncommitted changes", path: changed, want: "uncommitted changes"},
		{name: "locked by hand", path: byHand, want: "it is locked"},
		{name: "not linked", path: plain, want: "not linked"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := svc.CleanUp(t.Context(), repo, tt.path, nil)
			require.ErrorIs(t, err, ErrNotFinished)
			assert.Contains(t, err.Error(), tt.want)
			assert.Contains(t, gitWorktreePaths(t, repo), resolvedPath(tt.path), "nothing is removed")
		})
	}
}

func TestCleanUpRowChecksAgainBeforeEachStep(t *testing.T) {
	t.Parallel()

	// Each case changes its repository, so each gets a clone root of its own.
	setup := func(t *testing.T) (*env, string, *Service, git) {
		t.Helper()

		e := newEnv(t)
		repo := e.newGitHubRepo(t, "example-repo")
		svc := e.linked(linkDeps{})

		g, err := svc.git()
		require.NoError(t, err)

		return e, repo, svc, g
	}

	t.Run("a worktree whose commit moved is kept", func(t *testing.T) {
		t.Parallel()

		_, repo, svc, g := setup(t)
		path, head := agentWorktree(t, repo, "pr-1")
		res, _ := svc.cleanUpRow(t.Context(), g, repo, Worktree{Path: path, DisplayPath: "pr-1", Head: head + "x"}, false, nil)
		assert.False(t, res.Removed)
		assert.Equal(t, ErrChanged.Error(), res.Reason)
		assert.Contains(t, gitWorktreePaths(t, repo), resolvedPath(path))
	})

	t.Run("a lock that changed is kept", func(t *testing.T) {
		t.Parallel()

		e, repo, svc, g := setup(t)
		path, head := agentWorktree(t, repo, "pr-2")
		lock(t, repo, path, "claude session pr-2 (pid 999998)")

		res, _ := svc.cleanUpRow(t.Context(), g, repo, Worktree{
			Path: path, Head: head, Locked: true, LockReason: "claude session pr-2 (pid 999999)",
		}, false, nil)
		assert.False(t, res.Removed)
		assert.Equal(t, ErrChanged.Error(), res.Reason)
		assert.True(t, mustRow(t, e.list(t), path).Locked, "the new lock stays")
	})

	t.Run("its own branch goes with it", func(t *testing.T) {
		t.Parallel()

		_, repo, svc, g := setup(t)
		path, head := agentWorktree(t, repo, "pr-3")
		res, _ := svc.cleanUpRow(t.Context(), g, repo, Worktree{
			Path: path, Head: head, Branch: "worktree-pr-3", OwnBranch: true,
			Branches: []Branch{{Name: "worktree-pr-3", Head: head, Kind: BranchOwn, Deletable: true}},
		}, false, []string{"worktree-pr-3"})
		assert.True(t, res.Removed)
		assert.Equal(t, []string{"worktree-pr-3"}, res.DeletedBranches)
		assert.Empty(t, res.Reason)
		assert.NotContains(t, branches(t, repo), "worktree-pr-3")
	})

	t.Run("a branch checked out or moved is kept", func(t *testing.T) {
		t.Parallel()

		_, repo, svc, g := setup(t)
		other, otherHead := agentWorktree(t, repo, "pr-4")
		require.ErrorIs(t, svc.deleteBranchAt(t.Context(), g, repo, "worktree-pr-4", otherHead), ErrBranchInUse)

		testutil.RunGit(t, repo, "worktree", "remove", other)

		err := svc.deleteBranchAt(t.Context(), g, repo, "worktree-pr-4", strings.Repeat("1", 40))
		require.ErrorIs(t, err, errGit, "git refuses: the branch is not at the commit checked")

		for _, head := range []string{"", strings.Repeat("0", 40), strings.Repeat("0", 64)} {
			require.ErrorIs(t, svc.deleteBranchAt(t.Context(), g, repo, "worktree-pr-4", head), ErrChanged,
				"the null ID would turn git's comparison off")
		}

		assert.Contains(t, branches(t, repo), "worktree-pr-4")

		res, _ := svc.cleanUpRow(t.Context(), g, repo, Worktree{Path: other, Head: otherHead, Branch: "worktree-pr-4", OwnBranch: true}, false, nil)
		assert.False(t, res.Removed, "it is no longer a worktree")
	})

	t.Run("a removed worktree whose branch git keeps says why", func(t *testing.T) {
		t.Parallel()

		_, repo, svc, g := setup(t)
		path, head := agentWorktree(t, repo, "pr-5")
		res, _ := svc.cleanUpRow(t.Context(), g, repo, Worktree{
			Path: path, Head: head, Branch: "worktree-pr-5x", OwnBranch: true,
			Branches: []Branch{{Name: "worktree-pr-5x", Head: head, Kind: BranchOwn, Deletable: true}},
		}, false, []string{"worktree-pr-5x"})
		assert.True(t, res.Removed)
		assert.Empty(t, res.DeletedBranches)
		assert.Contains(t, res.Reason, "kept worktree-pr-5x")
	})
}

func TestCleanUpFinished(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	alpha := e.newGitHubRepo(t, "alpha")
	beta := e.newGitHubRepo(t, "beta")
	gh := &fakeGitHub{}

	one, oneHead := agentWorktree(t, alpha, "pr-1")
	pushed(t, alpha, "one", oneHead)
	gh.set("alpha", 1, pr(ItemMerged, oneHead))

	two, twoHead := agentWorktree(t, beta, "issue-2")
	pushed(t, beta, "two", twoHead)
	gh.set("beta", 2, closedIssue())

	open, openHead := agentWorktree(t, beta, "pr-3")
	pushed(t, beta, "three", openHead)
	gh.set("beta", 3, pr(ItemOpen, openHead))

	svc := e.linked(linkDeps{gh: gh})

	results, err := svc.CleanUpFinished(t.Context(), finishedPicks(t, svc))
	require.NoError(t, err)
	require.Len(t, results, 2)
	assert.True(t, results[0].Removed)
	assert.Equal(t, "acme-org/alpha#1", results[0].Item)
	assert.Equal(t, []string{"worktree-pr-1"}, results[0].DeletedBranches)
	assert.True(t, results[1].Removed)
	assert.Equal(t, "acme-org/beta#2", results[1].Item)

	assert.NotContains(t, gitWorktreePaths(t, alpha), resolvedPath(one))
	assert.NotContains(t, gitWorktreePaths(t, beta), resolvedPath(two))
	assert.Contains(t, gitWorktreePaths(t, beta), resolvedPath(open), "an open pull request's worktree stays")

	results, err = svc.CleanUpFinished(t.Context(), finishedPicks(t, svc))
	require.NoError(t, err)
	assert.Empty(t, results)

	gh.err = errFakeGitHub

	_, err = svc.CleanUpFinished(t.Context(), finishedPicks(t, svc))
	require.ErrorIs(t, err, ErrGitHubUnavailable)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = svc.CleanUpFinished(ctx, nil)
	require.Error(t, err)
}

// mustResolve resolves path's symlinks, as git reports worktree paths.
func mustResolve(t *testing.T, path string) string {
	t.Helper()

	resolved, err := filepath.EvalSymlinks(filepath.Dir(path))
	require.NoError(t, err)

	return filepath.Join(resolved, filepath.Base(path))
}

// normalizeResult resolves a result's path, which may sit behind a temp-dir symlink.
func normalizeResult(r RemoveResult) RemoveResult {
	r.Path = resolvedPath(r.Path)

	return r
}

// toldProgress returns the listing progress among events, in order.
func toldProgress(events *testutil.Events) []event.WorktreesProgress {
	var out []event.WorktreesProgress

	for _, e := range events.All() {
		if p, ok := e.(event.WorktreesProgress); ok {
			out = append(out, p)
		}
	}

	return out
}

func TestListTellsTheWindowHowFarItHasGot(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	repo := e.newGitHubRepo(t, "example-repo")
	e.newGitHubRepo(t, "other-repo")
	_, head := agentWorktree(t, repo, "pr-1")

	events := &testutil.Events{}
	gh := &fakeGitHub{states: map[string]ItemState{refKey("acme-org", "example-repo", 1): pr(ItemOpen, head)}}
	svc := e.linked(linkDeps{gh: gh, events: events})
	svc.progressEvery = 0

	_, err := svc.List(t.Context())
	require.NoError(t, err)

	listing := func(done int) event.WorktreesProgress {
		return event.WorktreesProgress{Phase: event.WorktreesListing, Done: done, Total: 2}
	}
	checking := func(done int) event.WorktreesProgress {
		return event.WorktreesProgress{Phase: event.WorktreesChecking, Done: done, Total: 3}
	}

	assert.Equal(t, []event.WorktreesProgress{
		{Phase: event.WorktreesFindingClones},
		listing(0), listing(1), listing(2),
		checking(0), checking(1), checking(2), checking(3),
		{Phase: event.WorktreesAskingGitHub, Total: 1},
		{Phase: event.WorktreesAskingGitHub, Done: 1, Total: 1},
	}, toldProgress(events), "the clones, then each one's worktrees, then each worktree, then GitHub")

	before := len(events.All())

	svc.Sweep(t.Context())

	for _, ev := range events.All()[before:] {
		assert.NotEqual(t, event.NameWorktreesProgress, ev.Name(), "a sweep's listing tells the window nothing")
	}
}
