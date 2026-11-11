package release

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// errGitHubDown is a GitHub that can't be reached.
var errGitHubDown = errors.New("github is down")

// testNow is when the tests' clock starts.
func testNow() time.Time {
	return time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
}

// name returns acme-org/<repo>.
func name(r string) repo.Name {
	return repo.Name{Owner: "acme-org", Repo: r}
}

// fakeGitHub answers from fixed statuses and comparisons, counting the calls.
type fakeGitHub struct {
	mu       sync.Mutex
	statuses map[string]RepoStatus
	compares map[string]Compare
	err      error
	asked    [][]repo.Name
	compared [][]CompareRequest

	// batch is how many repositories each answer it tells of holds, as the client's
	// batches do; 0 tells of them all at once.
	batch int

	// asking, when set, is called as each status is asked for.
	asking func()
}

func (f *fakeGitHub) ReleaseStatus(
	_ context.Context, repos []repo.Name, answered func(n int),
) (map[string]RepoStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.asked = append(f.asked, slices.Clone(repos))

	if f.asking != nil {
		f.asking()
	}

	if f.err != nil {
		return nil, f.err
	}

	out := map[string]RepoStatus{}

	for _, r := range repos {
		if st, ok := f.statuses[r.Key()]; ok {
			out[r.Key()] = st
		}
	}

	f.tell(len(repos), answered)

	return out, nil
}

func (f *fakeGitHub) ReleaseCompare(
	_ context.Context, reqs []CompareRequest, answered func(n int),
) (map[string]Compare, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.compared = append(f.compared, slices.Clone(reqs))

	out := map[string]Compare{}

	for _, r := range reqs {
		if c, ok := f.compares[r.Repo.Key()]; ok {
			out[r.Repo.Key()] = c
		}
	}

	f.tell(len(reqs), answered)

	return out, nil
}

// tell tells answered of n answers, batch at a time. The caller holds mu.
func (f *fakeGitHub) tell(n int, answered func(int)) {
	if answered == nil {
		return
	}

	size := f.batch
	if size <= 0 {
		size = max(n, 1)
	}

	for done := 0; done < n; done += size {
		answered(min(size, n-done))
	}
}

func (f *fakeGitHub) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return len(f.asked)
}

func (f *fakeGitHub) fail(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.err = err
}

// fakeClones lists fixed clones under a clone root that can change, counting the scans.
type fakeClones struct {
	mu      sync.Mutex
	root    string
	rootErr error
	list    []Clone
	err     error
	scanned int
}

func (f *fakeClones) Root(context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.root == "" {
		return "/Users/example/projects", f.rootErr
	}

	return f.root, f.rootErr
}

func (f *fakeClones) Clones(context.Context, string) ([]Clone, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.scanned++

	return f.list, f.err
}

// scans returns how often the clone root was scanned.
func (f *fakeClones) scans() int {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.scanned
}

func (f *fakeClones) moveTo(root string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.root = root
}

// fakeChecker answers every clone's state with its folder as the branch, recording the
// clones it was asked about.
type fakeChecker struct {
	mu   sync.Mutex
	dirs []string
}

func (f *fakeChecker) CloneState(_ context.Context, dir, branch, remoteHead string, tags []Tag) CloneState {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.dirs = append(f.dirs, dir)

	return CloneState{Branch: branch + "@" + remoteHead, Dirty: len(tags), LocalOnly: []string{}}
}

func (f *fakeChecker) asked() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Sorted(slices.Values(f.dirs))
}

// rig is a Service over fakes.
type rig struct {
	clones  *fakeClones
	github  *fakeGitHub
	checker *fakeChecker
	clock   *testutil.Clock
	events  *testutil.Events
	svc     *Service
}

// newRig returns a Service over five clones: sdk with two unreleased commits, web
// unreleased too and more recently, cli up to date, fresh with no version, and an archived
// repository; plus a clone without a GitHub origin.
func newRig() *rig {
	r := &rig{
		github: &fakeGitHub{
			statuses: map[string]RepoStatus{
				name("sdk").Key(): {
					Permission: "WRITE", DefaultBranch: "master", HeadOID: "s3", HeadAt: testNow().Add(-time.Hour),
					Headline: "Fix retries on 502 (#45)", Checks: ChecksSuccess,
					Tags: []Tag{{Name: "v1.4.2", Commit: "s1"}, {Name: "v1.4.10-rc.1", Commit: "s0"}, {Name: "release-7", Commit: "s2"}},
				},
				name("web").Key(): {
					Permission: "ADMIN", DefaultBranch: "main", HeadOID: "w2", HeadAt: testNow().Add(-time.Minute),
					Tags: []Tag{{Name: "v0.9.1", Commit: "w1"}},
				},
				name("cli").Key(): {
					Permission: "MAINTAIN", DefaultBranch: "master", HeadOID: "c1", HeadAt: testNow().Add(-48 * time.Hour),
					Tags: []Tag{{Name: "v2.3.0", Commit: "c1"}},
				},
				name("fresh").Key(): {Permission: "WRITE", DefaultBranch: "main", HeadOID: "f1", HeadAt: testNow()},
				name("old").Key():   {Archived: true, Permission: "ADMIN", DefaultBranch: "master", HeadOID: "o1"},
				name("docs").Key():  {Permission: "READ", DefaultBranch: "main", HeadOID: "d1"},
			},
			compares: map[string]Compare{
				name("sdk").Key(): {Ahead: 2, Commits: []Commit{
					{OID: "s2", Headline: "Older", At: testNow().Add(-2 * time.Hour)},
					{OID: "s3", Headline: "Fix retries on 502 (#45)", At: testNow().Add(-time.Hour)},
				}},
				name("web").Key(): {Ahead: 1, Commits: []Commit{{OID: "w2", Headline: "Bump vite", At: testNow().Add(-time.Minute)}}},
			},
		},
		checker: &fakeChecker{},
		clock:   testutil.NewClock(testNow()),
		events:  &testutil.Events{},
	}

	clones := &fakeClones{list: []Clone{
		{Dir: "/Users/example/projects/sdk", Origin: name("sdk"), OnHub: true},
		{Dir: "/Users/example/projects/web", Origin: name("web"), OnHub: true},
		{Dir: "/Users/example/projects/cli", Origin: name("cli"), OnHub: true},
		{Dir: "/Users/example/projects/fresh", Origin: name("fresh"), OnHub: true},
		{Dir: "/Users/example/projects/old", Origin: name("old"), OnHub: true},
		{Dir: "/Users/example/projects/docs", Origin: name("docs"), OnHub: true},
		{Dir: "/Users/example/projects/gone", Origin: name("gone"), OnHub: true},
		{Dir: "/Users/example/projects/notes"},
	}}

	r.clones = clones
	r.svc = NewService(Config{
		GitHub: r.github, Clones: clones, Checker: r.checker, Home: "/Users/example", Clock: r.clock, Events: r.events,
	})

	return r
}

// updates counts the event.ReleasesUpdated among events, leaving out the progress.
func updates(events *testutil.Events) int {
	n := 0

	for _, e := range events.All() {
		if _, ok := e.(event.ReleasesUpdated); ok {
			n++
		}
	}

	return n
}

// groups returns each listed repository's name and group, in order.
func groups(list Candidates) []string {
	out := make([]string, 0, len(list.Repos))
	for _, c := range list.Repos {
		out = append(out, c.Repo+" "+string(c.Group))
	}

	return out
}

func TestCandidatesGroupTheClones(t *testing.T) {
	t.Parallel()

	r := newRig()

	got, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"acme-org/web unreleased", "acme-org/sdk unreleased", "acme-org/cli upToDate", "acme-org/fresh noReleases",
		"acme-org/docs cantRelease", "acme-org/gone cantRelease", "acme-org/old cantRelease",
	}, groups(got), "unreleased first, the most recent commit on top; the rest by name")
	assert.Equal(t, 1, got.WithoutGitHub, "the clone without a GitHub origin is counted, not listed")
	assert.Equal(t, testNow().Format(time.RFC3339), got.CheckedAt)
	assert.Empty(t, got.Error)

	sdk := got.Repos[1]
	assert.Equal(t, "v1.4.10-rc.1", sdk.Version, "the highest version among the tags, not the newest tag")
	assert.Equal(t, 2, sdk.Ahead)
	assert.Equal(t, []string{"s3", "s2"}, []string{sdk.Commits[0].OID, sdk.Commits[1].OID}, "newest first")
	assert.Equal(t, "~/projects/sdk", sdk.Dir)
	assert.Equal(t, "Fix retries on 502 (#45)", sdk.Headline)
	assert.Equal(t, ChecksSuccess, sdk.Checks)
	assert.Equal(t, testNow().Add(-time.Hour).Format(time.RFC3339), sdk.HeadAt)
	require.NotNil(t, sdk.Clone, "an unreleased clone's state is read")
	assert.Equal(t, "master@s3", sdk.Clone.Branch, "against its default branch and GitHub's newest commit")

	reasons := map[string]string{}

	for _, c := range got.Repos {
		if c.Group == GroupCantRelease {
			reasons[c.Repo] = c.Reason
		}

		if c.Group != GroupUnreleased {
			assert.Nil(t, c.Clone, "%s: only unreleased clones are read", c.Repo)
		}

		assert.NotNil(t, c.Commits, "%s: never a null list", c.Repo)
	}

	assert.Equal(t, map[string]string{
		"acme-org/docs": "You can only read it.",
		"acme-org/gone": "GitHub doesn't show this repository to you.",
		"acme-org/old":  "It's archived.",
	}, reasons)

	assert.Equal(t, []string{"/Users/example/projects/sdk", "/Users/example/projects/web"}, r.checker.asked())
	assert.Equal(t, 1, updates(r.events), "the window hears the list landed")
}

func TestCandidatesCompareOnlyWhatHasChanges(t *testing.T) {
	t.Parallel()

	r := newRig()

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	require.Len(t, r.github.compared, 1)
	assert.ElementsMatch(t, []CompareRequest{
		{Repo: name("sdk"), Tag: "v1.4.10-rc.1", Branch: "master"},
		{Repo: name("web"), Tag: "v0.9.1", Branch: "main"},
	}, r.github.compared[0], "cli's tag is on its newest commit, and fresh has no version")
}

func TestCandidatesWithoutAComparisonStayUnreleased(t *testing.T) {
	t.Parallel()

	r := newRig()
	delete(r.github.compares, name("web").Key())

	got, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	web := got.Repos[slices.IndexFunc(got.Repos, func(c Candidate) bool { return c.Repo == "acme-org/web" })]
	assert.Equal(t, GroupUnreleased, web.Group, "a tag off its newest commit may have changes since")
	assert.Zero(t, web.Ahead)
}

func TestCandidatesAheadByNothingAreUpToDate(t *testing.T) {
	t.Parallel()

	r := newRig()
	r.github.compares[name("web").Key()] = Compare{}

	got, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.Contains(t, groups(got), "acme-org/web upToDate")
}

func TestCandidatesAreKeptUntilTheUserReadsThemAgain(t *testing.T) {
	t.Parallel()

	r := newRig()

	for range 3 {
		_, err := r.svc.Candidates(t.Context(), false)
		require.NoError(t, err)
	}

	assert.Equal(t, 1, r.github.calls(), "the list is served as it is")

	r.clock.Advance(6 * time.Hour)

	old, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.Equal(t, 1, r.github.calls(), "however old: the whole list is read again only when the user asks")
	assert.False(t, old.Refreshing)

	_, err = r.svc.Candidates(t.Context(), true)
	require.NoError(t, err)
	assert.Equal(t, 2, r.github.calls(), "Read GitHub again asks again")
	assert.Equal(t, 2, updates(r.events), "each refresh that lands tells the window")
}

func TestAFailedRefreshKeepsTheLastList(t *testing.T) {
	t.Parallel()

	r := newRig()

	first, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	r.github.fail(errGitHubDown)

	kept, err := r.svc.Candidates(t.Context(), true)
	require.NoError(t, err, "the last list is still shown")
	assert.Equal(t, groups(first), groups(kept))
	assert.Equal(t, first.CheckedAt, kept.CheckedAt, "with the time it was read, so its age shows")
	assert.Equal(t, "Couldn't read your repositories from GitHub.", kept.Error)
	assert.Equal(t, 2, updates(r.events), "a refresh that failed tells the window too")

	r.clock.Advance(time.Hour)

	again, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.Equal(t, kept.Error, again.Error, "the failure stays until a refresh succeeds")
	assert.Equal(t, 2, r.github.calls(), "and a read after it asks nothing, until the user reads again")

	r.github.fail(nil)

	healed, err := r.svc.Candidates(t.Context(), true)
	require.NoError(t, err)
	assert.Empty(t, healed.Error)
}

func TestAWordedGitHubFailureKeepsItsWords(t *testing.T) {
	t.Parallel()

	r := newRig()
	r.github.fail(uierr.New("Install the GitHub CLI (gh) to sign in.", errGitHubDown))

	_, err := r.svc.Candidates(t.Context(), false)
	assert.Equal(t, "Install the GitHub CLI (gh) to sign in.", uierr.Message(err))
}

func TestAChangedCloneRootIsReadAgain(t *testing.T) {
	t.Parallel()

	r := newRig()

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	r.clones.moveTo("/Users/example/work")

	_, err = r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.Equal(t, 2, r.github.calls(), "a list from another clone root isn't served")

	r.github.fail(errGitHubDown)
	r.clones.moveTo("/Users/example/elsewhere")

	_, err = r.svc.Candidates(t.Context(), false)
	require.ErrorIs(t, err, errGitHubDown, "nor kept when the new root's refresh fails")
}

func TestARefreshCutShortKeepsNothing(t *testing.T) {
	t.Parallel()

	r := newRig()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := r.svc.Candidates(ctx, false)
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, r.events.All(), "nothing was stored")

	before := r.github.calls()

	got, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.NotEmpty(t, got.Repos)
	assert.Equal(t, before+1, r.github.calls(), "the next read asks again")
}

func TestAnUnknownPermissionDoesntBlock(t *testing.T) {
	t.Parallel()

	c := candidateFor(clone{dir: "/x", origin: name("sdk")}, RepoStatus{DefaultBranch: "main", HeadOID: "a"}, true, Compare{}, false)
	assert.Equal(t, GroupNoReleases, c.Group, "GitHub not saying isn't read-only")
}

func TestAFirstRefreshThatFailsIsAnError(t *testing.T) {
	t.Parallel()

	r := newRig()
	r.github.fail(ErrSignedOut)

	_, err := r.svc.Candidates(t.Context(), false)
	require.ErrorIs(t, err, ErrSignedOut)
	assert.Equal(t, "Sign in to GitHub first, so the Release page can read your repositories.", uierr.Message(err))

	clonesFail := NewService(Config{GitHub: &fakeGitHub{}, Clones: &fakeClones{err: errGitHubDown}})
	_, err = clonesFail.Candidates(t.Context(), false)
	require.ErrorIs(t, err, errGitHubDown)
}

func TestCandidatesWithNoClonesAskNothing(t *testing.T) {
	t.Parallel()

	gh := &fakeGitHub{}
	svc := NewService(Config{
		GitHub: gh, Clones: &fakeClones{list: []Clone{{Dir: "/Users/example/projects/notes"}}}, Clock: testutil.NewClock(testNow()),
	})

	got, err := svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.Empty(t, got.Repos)
	assert.NotNil(t, got.Repos)
	assert.Equal(t, 1, got.WithoutGitHub)
	assert.Zero(t, gh.calls(), "no request without a clone on GitHub")
}

func TestTwoClonesOfOneRepositoryAskOnce(t *testing.T) {
	t.Parallel()

	gh := &fakeGitHub{statuses: map[string]RepoStatus{
		name("sdk").Key(): {Permission: "WRITE", DefaultBranch: "master", HeadOID: "s1", Tags: []Tag{{Name: "v1.0.0", Commit: "s1"}}},
	}}
	svc := NewService(Config{GitHub: gh, Clones: &fakeClones{list: []Clone{
		{Dir: "/Users/example/projects/sdk", Origin: name("sdk"), OnHub: true},
		{Dir: "/Users/example/projects/sdk-copy", Origin: repo.Name{Owner: "Acme-Org", Repo: "SDK"}, OnHub: true},
	}}})

	got, err := svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.Len(t, got.Repos, 2, "each clone is listed")
	require.Len(t, gh.asked, 1)
	assert.Len(t, gh.asked[0], 1, "the repository is asked about once")
}

func TestARepositoryWithNoCommitsCantRelease(t *testing.T) {
	t.Parallel()

	c := candidateFor(clone{dir: "/x", origin: name("empty")}, RepoStatus{Permission: "WRITE"}, true, Compare{}, false)
	assert.Equal(t, GroupCantRelease, c.Group)
	assert.Equal(t, "It has no commits on GitHub yet.", c.Reason)
	assert.Empty(t, c.HeadAt)
}
