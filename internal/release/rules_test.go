package release

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/reporules"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// sdkDir is sdk's clone in the rig.
const sdkDir = "/Users/example/projects/sdk"

// errPrefsLocked is rules that can't be read.
var errPrefsLocked = errors.New("database is locked")

// fakeRules answers the pinned and muted repositories as the test sets them.
type fakeRules struct {
	mu    sync.Mutex
	rules reporules.Rules
	err   error
}

func (f *fakeRules) RepoRules(context.Context) (reporules.Rules, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.rules, f.err
}

// set has the rules pin pinned and mute muted.
func (f *fakeRules) set(pinned, muted []string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.rules = reporules.Rules{Pinned: pinned, Muted: muted}
}

// rulesRig is the rig with rules the test sets, keeping its list in memory.
func rulesRig(pinned, muted []string) (*rig, *fakeRules, *testutil.Logs) {
	r, logs := newRig(), &testutil.Logs{}
	rules := &fakeRules{}
	rules.set(pinned, muted)
	r.svc = NewService(Config{
		GitHub: r.github, Clones: r.clones, Checker: r.checker, Home: "/Users/example", Clock: r.clock,
		Events: r.events, Keeper: &memoryKeeper{}, Rules: rules, Logger: logs.Logger(),
	})

	return r, rules, logs
}

// askedAbout lists every repository the status reads asked about.
func (f *fakeGitHub) askedAbout() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []string

	for _, names := range f.asked {
		for _, n := range names {
			out = append(out, n.String())
		}
	}

	return out
}

// lastHeld returns the row of name the service holds, as GitHub last said of it.
func lastHeld(t *testing.T, s *Service, name repo.Name) Candidate {
	t.Helper()

	s.mu.Lock()
	defer s.mu.Unlock()

	for _, c := range s.last.Repos {
		if c.origin.Key() == name.Key() {
			return c
		}
	}

	require.FailNow(t, "not held", name.String())

	return Candidate{}
}

func TestAMutedRepositoryIsListedApartAndNeverAskedAbout(t *testing.T) {
	t.Parallel()

	r, _, _ := rulesRig(nil, []string{"acme-org/web", "acme-org/fresh"})

	got, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"acme-org/sdk unreleased", "acme-org/cli upToDate",
		"acme-org/docs cantRelease", "acme-org/gone cantRelease", "acme-org/old cantRelease",
		"acme-org/fresh muted", "acme-org/web muted",
	}, groups(got), "the muted repositories last, by name")

	assert.NotContains(t, r.github.askedAbout(), "acme-org/web")
	assert.NotContains(t, r.github.askedAbout(), "acme-org/fresh")

	for _, reqs := range r.github.compared {
		for _, req := range reqs {
			assert.NotEqual(t, name("web"), req.Repo, "nor compared")
		}
	}

	assert.Equal(t, []string{sdkDir}, r.checker.asked(), "nor is its clone read")

	web := rowOf(t, got, "acme-org/web")
	assert.Equal(t, "~/projects/web", web.Dir)
	assert.NotNil(t, web.Commits, "never a null list")
	assert.Nil(t, web.Clone)
	assert.Empty(t, web.Version)
}

func TestAMutedOrgMutesEveryRepositoryButOnePinnedItself(t *testing.T) {
	t.Parallel()

	r, _, _ := rulesRig([]string{"acme-org/cli"}, []string{"acme-org"})

	got, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	assert.Equal(t, []string{
		"acme-org/cli upToDate",
		"acme-org/docs muted", "acme-org/fresh muted", "acme-org/gone muted", "acme-org/old muted",
		"acme-org/sdk muted", "acme-org/web muted",
	}, groups(got))
	assert.Equal(t, []string{"acme-org/cli"}, r.github.askedAbout(), "GitHub is asked about cli alone")
}

func TestEveryRepositoryMutedAsksGitHubNothing(t *testing.T) {
	t.Parallel()

	r, _, _ := rulesRig(nil, []string{"acme-org"})

	got, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.Zero(t, r.github.calls())
	assert.Len(t, got.Repos, 7)
	assert.Equal(t, testNow().Format("2006-01-02T15:04:05Z07:00"), got.CheckedAt)
}

func TestPinnedRepositoriesComeFirstInTheirGroup(t *testing.T) {
	t.Parallel()

	r, _, _ := rulesRig([]string{"acme-org/sdk", "acme-org/old"}, nil)

	got, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"acme-org/sdk unreleased", "acme-org/web unreleased", "acme-org/cli upToDate", "acme-org/fresh noReleases",
		"acme-org/old cantRelease", "acme-org/docs cantRelease", "acme-org/gone cantRelease",
	}, groups(got))
}

func TestMutingAListedRepositoryRegroupsItWithoutAskingGitHub(t *testing.T) {
	t.Parallel()

	r, rules, _ := rulesRig(nil, nil)
	r.refresherOn()

	first, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	require.Equal(t, "acme-org/web unreleased", groups(first)[0])

	before := r.events.Count()

	rules.set([]string{"acme-org/cli"}, []string{"acme-org/web"})
	NewRefresher(r.svc).RulesChanged()
	assert.Equal(t, []event.Event{event.ReleasesUpdated{}}, r.events.All()[before:], "the page hears to ask again")

	muted, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.Equal(t, []string{
		"acme-org/sdk unreleased", "acme-org/cli upToDate", "acme-org/fresh noReleases",
		"acme-org/docs cantRelease", "acme-org/gone cantRelease", "acme-org/old cantRelease", "acme-org/web muted",
	}, groups(muted))
	assert.False(t, muted.Refreshing)
	assert.Equal(t, 1, r.github.calls(), "muting reads nothing")
	assert.Equal(t, GroupUnreleased, lastHeld(t, r.svc, name("web")).Group, "what GitHub said of it is kept")

	rules.set(nil, nil)

	back, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.Equal(t, groups(first), groups(back), "unmuting shows it as it was read")
	assert.Equal(t, 1, rowOf(t, back, "acme-org/web").Ahead)
	assert.Equal(t, 1, r.github.calls(), "and reads nothing either")
	assert.False(t, r.askedPatch())
}

func TestUnmutingARepositoryNeverReadReadsItAlone(t *testing.T) {
	t.Parallel()

	r, rules, _ := rulesRig(nil, []string{"acme-org/web"})
	r.svc.progressEvery = 0
	r.refresherOn()

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	rules.set(nil, nil)

	before := r.events.Count()

	queued, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.True(t, queued.Refreshing, "the list reads as refreshing until web is read")
	assert.Equal(t, "acme-org/web muted", groups(queued)[len(queued.Repos)-1], "meanwhile it stays where it was")
	assert.Equal(t, []event.Event{event.ReleasesUpdated{}}, r.events.All()[before:])

	again, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.True(t, again.Refreshing)
	assert.Equal(t, 1, r.github.calls())

	r.patchBehind(t.Context(), t)
	assert.False(t, r.askedPatch(), "asked for once, however often the page asks for the list")

	assert.Equal(t, []repo.Name{name("web")}, r.github.askedAt(1), "GitHub is asked about web alone")
	assert.Equal(t, []string{sdkDir, webDir}, r.checker.asked())

	got, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.False(t, got.Refreshing)
	assert.Equal(t, []string{
		"acme-org/web unreleased", "acme-org/sdk unreleased", "acme-org/cli upToDate", "acme-org/fresh noReleases",
		"acme-org/docs cantRelease", "acme-org/gone cantRelease", "acme-org/old cantRelease",
	}, groups(got))
	assert.Equal(t, 2, r.github.calls())
}

func TestUnmutingWithoutTheRefresherReadsTheListAgain(t *testing.T) {
	t.Parallel()

	r, rules, _ := rulesRig(nil, []string{"acme-org/web"})

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	rules.set(nil, nil)

	got, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.Equal(t, 2, r.github.calls(), "the list can't be patched, so it is read again in full")
	assert.Contains(t, r.github.askedAbout(), "acme-org/web")
	assert.Contains(t, groups(got), "acme-org/web unreleased")
}

func TestAMergeIntoAMutedRepositoryAsksNothing(t *testing.T) {
	t.Parallel()

	r, _, _ := rulesRig(nil, []string{"acme-org/web"})
	r.refresherOn()

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	NewRefresher(r.svc).Merged(name("web"))
	assert.False(t, r.askedPatch())

	NewRefresher(r.svc).Merged(name("sdk"))
	assert.True(t, r.askedPatch(), "a repository that isn't muted is read again")
}

func TestAReleaseInAMutedRepositoryLeavesItsRowUnread(t *testing.T) {
	t.Parallel()

	r, rules, _ := rulesRig(nil, nil)
	r.refresherOn()

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	rules.set(nil, []string{"acme-org/web"})

	_, err = r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	r.svc.released(t.Context(), name("web"), webDir, Result{Kind: ResultReleased})
	r.patchBehind(t.Context(), t)
	assert.Equal(t, 1, r.github.calls(), "a muted repository isn't asked about")
	assert.Equal(t, GroupMuted, lastHeld(t, r.svc, name("web")).Group, "its row before the release is dropped")

	rules.set(nil, nil)

	_, err = r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	r.patchBehind(t.Context(), t)
	assert.Equal(t, []repo.Name{name("web")}, r.github.askedAt(1), "it is read once it is unmuted")
}

func TestRulesThatCantBeReadLeaveTheLastOnes(t *testing.T) {
	t.Parallel()

	r, rules, logs := rulesRig(nil, []string{"acme-org/web"})

	first, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	rules.mu.Lock()
	rules.err = errPrefsLocked
	rules.mu.Unlock()

	again, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.Equal(t, groups(first), groups(again))
	assert.Contains(t, logs.String(), "couldn't read the pinned and muted repositories")
	assert.Equal(t, 1, r.github.calls())
}

func TestAKeptListOfTheFirstShapeIsntRead(t *testing.T) {
	t.Parallel()

	r, _ := newKeptRig(&memoryKeeper{})

	first, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	raw, err := encodeKept(first, "/Users/example/projects")
	require.NoError(t, err)
	require.Contains(t, raw, `"version":2`)

	older := strings.Replace(raw, `"version":2`, `"version":1`, 1)

	next, logs := newKeptRig(&memoryKeeper{values: map[string]string{keptKey: older}})
	next.refresherOn()

	got, err := next.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.False(t, got.Kept, "a list kept before the muted group is read from GitHub again")
	assert.Equal(t, 1, next.github.calls())
	assert.Empty(t, logs.String())
}

func TestAKeptListHoldsTheMutedGroup(t *testing.T) {
	t.Parallel()

	r, _, _ := rulesRig(nil, []string{"acme-org/web"})

	first, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	raw, err := encodeKept(first, "/Users/example/projects")
	require.NoError(t, err)

	list, root, ok, err := decodeKept(raw)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "/Users/example/projects", root)
	assert.Equal(t, groups(first), groups(list))
	assert.Equal(t, name("web"), rowOf(t, list, "acme-org/web").origin)
}
