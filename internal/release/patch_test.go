package release

import (
	"context"
	"slices"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/repo"
)

// webDir is web's clone in the rig.
const webDir = "/Users/example/projects/web"

// releaseWeb has GitHub answer as it does once web's v0.9.2 is released, on its newest
// commit.
func (f *fakeGitHub) releaseWeb() {
	f.mu.Lock()
	defer f.mu.Unlock()

	web := f.statuses[name("web").Key()]
	web.Tags = append([]Tag{{Name: "v0.9.2", Commit: "w2"}}, web.Tags...)
	f.statuses[name("web").Key()] = web
}

// askedAt returns the repositories the status read i asked about.
func (f *fakeGitHub) askedAt(i int) []repo.Name {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.asked[i])
}

// patchBehind does the patch a release asked the Refresher for, as the Refresher would,
// and fails the test when none was asked.
func (r *rig) patchBehind(ctx context.Context, t *testing.T) {
	t.Helper()

	select {
	case <-r.svc.wakePatch:
	default:
		require.FailNow(t, "no release asked for a patch")
	}

	r.svc.patchReleased(ctx)
}

// askedPatch reports whether a release asked the Refresher for a patch, taking the ask.
func (r *rig) askedPatch() bool {
	select {
	case <-r.svc.wakePatch:
		return true
	default:
		return false
	}
}

// running reports whether s's Refresher has marked itself running.
func running(s *Service) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.background
}

// servedNow returns the last list as Candidates serves it while it is fresh, without
// reading anything.
func servedNow(s *Service) Candidates {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.served()
}

// rowOf returns the row of the repository named repoText in list.
func rowOf(t *testing.T, list Candidates, repoText string) Candidate {
	t.Helper()

	i := slices.IndexFunc(list.Repos, func(c Candidate) bool { return c.Repo == repoText })
	require.GreaterOrEqual(t, i, 0, "%s is listed", repoText)

	return list.Repos[i]
}

// progressOf is how far a read of the list has got, as the window hears it.
func progressOf(phase event.ReleasesPhase, done, total int) event.Event {
	return event.ReleasesProgress{Phase: phase, Done: done, Total: total}
}

func TestAReleasePatchesItsRepositoryAlone(t *testing.T) {
	t.Parallel()

	keeper := &memoryKeeper{}
	r, _ := newKeptRig(keeper)
	r.svc.progressEvery = 0
	r.refresherOn()

	first, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	require.Equal(t, []string{"/Users/example/projects/sdk", webDir}, r.checker.asked())

	r.clock.Advance(time.Minute)
	r.github.releaseWeb()

	before := r.events.Count()

	r.svc.released(t.Context(), name("web"), webDir, Result{Kind: ResultReleased})
	assert.Equal(t, []event.Event{event.ReleasesUpdated{}}, r.events.All()[before:],
		"the window hears at once that web is read again")

	queued, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.True(t, queued.Refreshing, "the list reads as refreshing until the patch lands")
	assert.Equal(t, 1, r.github.calls(), "and is served as it is meanwhile")

	r.patchBehind(t.Context(), t)

	assert.Equal(t, []event.Event{
		event.ReleasesUpdated{},
		progressOf(event.ReleasesAskingGitHub, 0, 1), progressOf(event.ReleasesAskingGitHub, 1, 1),
		event.ReleasesUpdated{},
	}, r.events.All()[before:], "then how far the patch has got, and then that it landed")

	assert.Equal(t, 2, r.github.calls())
	assert.Equal(t, []repo.Name{name("web")}, r.github.askedAt(1), "GitHub is asked about the released repository alone")
	assert.Len(t, r.github.compared, 1, "web's new version is on its newest commit, so there is nothing to compare")
	assert.Equal(t, []string{"/Users/example/projects/sdk", webDir}, r.checker.asked(),
		"no clone is read again: web is up to date now")
	assert.Equal(t, 1, r.clones.scans(), "nor is the clone root scanned again")

	got, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.False(t, got.Refreshing)
	assert.Equal(t, 2, r.github.calls(), "the patched list is served without reading GitHub again")
	assert.Equal(t, []string{
		"acme-org/sdk unreleased", "acme-org/cli upToDate", "acme-org/web upToDate", "acme-org/fresh noReleases",
		"acme-org/docs cantRelease", "acme-org/gone cantRelease", "acme-org/old cantRelease",
	}, groups(got), "web moves among the up to date, in order")
	assert.Equal(t, first.CheckedAt, got.CheckedAt, "the list is as old as it was")
	assert.Equal(t, rowOf(t, first, "acme-org/sdk"), rowOf(t, got, "acme-org/sdk"), "the rest of the list is as it was")

	web := rowOf(t, got, "acme-org/web")
	assert.Equal(t, "v0.9.2", web.Version)
	assert.Equal(t, "~/projects/web", web.Dir)
	assert.Nil(t, web.Clone, "an up to date clone isn't read")
	assert.NotNil(t, web.Commits)

	raw, found, err := keeper.Get(t.Context(), keptKey)
	require.NoError(t, err)
	require.True(t, found)

	kept, _, ok, err := decodeKept(raw)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, groups(got), groups(kept), "the patched list is kept for the next start")

	r.clock.Advance(FreshFor - time.Minute)

	_, err = r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.Equal(t, 3, r.github.calls(), "five minutes after the list was read, not patched, it is read again")
}

func TestAReleasePatchesEveryCloneOfItsRepository(t *testing.T) {
	t.Parallel()

	copied := repo.Name{Owner: "Acme-Org", Repo: "SDK"}

	r := newRig()
	r.svc.progressEvery = 0
	r.clones.list = append(r.clones.list, Clone{Dir: "/Users/example/projects/sdk-copy", Origin: copied, OnHub: true})
	r.refresherOn()

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	// sdk's v1.5.0 is released, and a commit lands on master meanwhile.
	sdk := r.github.statuses[name("sdk").Key()]
	sdk.HeadOID, sdk.HeadAt = "s4", testNow()
	sdk.Tags = append([]Tag{{Name: "v1.5.0", Commit: "s3"}}, sdk.Tags...)
	r.github.statuses[name("sdk").Key()] = sdk
	r.github.compares[name("sdk").Key()] = Compare{Ahead: 1, Commits: []Commit{{OID: "s4", Headline: "Later", At: testNow()}}}

	before := r.events.Count()

	r.svc.released(t.Context(), copied, "/Users/example/projects/sdk-copy", Result{Kind: ResultReleased})
	r.patchBehind(t.Context(), t)

	assert.Equal(t, []event.Event{
		event.ReleasesUpdated{},
		progressOf(event.ReleasesAskingGitHub, 0, 1), progressOf(event.ReleasesAskingGitHub, 1, 1),
		progressOf(event.ReleasesComparing, 0, 1), progressOf(event.ReleasesComparing, 1, 1),
		progressOf(event.ReleasesCheckingClones, 0, 2), progressOf(event.ReleasesCheckingClones, 1, 2),
		progressOf(event.ReleasesCheckingClones, 2, 2),
		event.ReleasesUpdated{},
	}, r.events.All()[before:], "the patch tells each round of its read, then lands")

	asked := r.github.askedAt(1)
	require.Len(t, asked, 1, "two clones of one repository ask about it once")
	assert.Equal(t, name("sdk").Key(), asked[0].Key())

	require.Len(t, r.github.compared, 2)
	require.Len(t, r.github.compared[1], 1)
	assert.Equal(t, name("sdk").Key(), r.github.compared[1][0].Repo.Key())
	assert.Equal(t, "v1.5.0", r.github.compared[1][0].Tag)

	assert.Equal(t, []string{
		"/Users/example/projects/sdk", "/Users/example/projects/sdk",
		"/Users/example/projects/sdk-copy", "/Users/example/projects/sdk-copy", webDir,
	}, r.checker.asked(), "both of sdk's clones are read again, and only they are")

	got, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.Equal(t, 2, r.github.calls())

	for _, c := range got.Repos[:2] {
		assert.Equal(t, name("sdk").Key(), c.origin.Key(), "sdk's newer commit lists it first now")
		assert.Equal(t, "v1.5.0", c.Version, c.Dir)
		assert.Equal(t, 1, c.Ahead, c.Dir)
		require.NotNil(t, c.Clone, c.Dir)
		assert.Equal(t, "master@s4", c.Clone.Branch, "%s: read against GitHub's newest commit", c.Dir)
	}

	assert.Equal(t, "acme-org/web unreleased", groups(got)[2])
}

func TestTheListReadsAsRefreshingWhileAPatchRuns(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r, held := newHeldRig(t)
		r.refresherOn()
		r.github.releaseWeb()

		r.svc.released(t.Context(), name("web"), webDir, Result{Kind: ResultReleased})

		queued, err := r.svc.Candidates(t.Context(), false)
		require.NoError(t, err)
		assert.True(t, queued.Refreshing, "a patch is queued")
		assert.Contains(t, groups(queued), "acme-org/web unreleased", "and the list is served as it is meanwhile")

		require.True(t, r.askedPatch())

		patched := make(chan struct{})

		go func() {
			defer close(patched)

			r.svc.patchReleased(t.Context())
		}()

		<-held.holding

		underWay, err := r.svc.Candidates(t.Context(), false)
		require.NoError(t, err)
		assert.True(t, underWay.Refreshing, "a patch is under way")

		close(held.hold)
		<-patched

		landed, err := r.svc.Candidates(t.Context(), false)
		require.NoError(t, err)
		assert.False(t, landed.Refreshing, "the patch landed")
		assert.Contains(t, groups(landed), "acme-org/web upToDate")
		assert.Equal(t, 2, r.github.calls())

		// A release whose repository the list doesn't hold can't be patched.
		r.svc.released(t.Context(), name("other"), "/Users/example/projects/other", Result{Kind: ResultReleased})
		assert.True(t, servedNow(r.svc).Refreshing)

		require.True(t, r.askedPatch())
		r.svc.patchReleased(t.Context())
		assert.False(t, servedNow(r.svc).Refreshing, "the list went out of date, and no read runs behind it")
		assert.Equal(t, 2, r.github.calls())
	})
}

func TestAReleaseWithoutTheRefresherPutsTheListOutOfDate(t *testing.T) {
	t.Parallel()

	r := newRig()

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	r.svc.released(t.Context(), name("web"), webDir, Result{Kind: ResultReleased})
	assert.False(t, r.askedPatch(), "nothing would patch the list")
	assert.Equal(t, 2, updates(r.events), "the window hears at once")
	assert.False(t, servedNow(r.svc).Refreshing, "and no read runs behind the list")

	_, err = r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.Equal(t, 2, r.github.calls(), "the list is read again")
	assert.Len(t, r.github.askedAt(1), 7, "in full")
	assert.Equal(t, 2, r.clones.scans())
}

func TestAListThatCantBePatchedGoesOutOfDate(t *testing.T) {
	t.Parallel()

	read := func(t *testing.T, r *rig) {
		t.Helper()

		_, err := r.svc.Candidates(t.Context(), false)
		require.NoError(t, err)
	}

	tests := []struct {
		name     string
		before   func(t *testing.T, r *rig)
		after    func(r *rig)
		released repo.Name
		asks     int
		progress []event.Event
		log      string
	}{
		{name: "no list yet", before: func(*testing.T, *rig) {}, released: name("web")},
		{
			name: "a list from another clone root",
			before: func(t *testing.T, r *rig) {
				t.Helper()
				read(t, r)
				r.clones.moveTo("/Users/example/work")
			},
			after:    func(r *rig) { r.clones.moveTo("/Users/example/projects") },
			released: name("web"),
		},
		{
			name: "a list over five minutes old",
			before: func(t *testing.T, r *rig) {
				t.Helper()
				read(t, r)
				r.clock.Advance(FreshFor)
			},
			released: name("web"),
		},
		{name: "a repository the list doesn't hold", before: read, released: name("other")},
		{
			name: "GitHub failing",
			before: func(t *testing.T, r *rig) {
				t.Helper()
				read(t, r)
				r.github.fail(errGitHubDown)
			},
			after:    func(r *rig) { r.github.fail(nil) },
			released: name("web"), asks: 1,
			progress: []event.Event{progressOf(event.ReleasesAskingGitHub, 0, 1)},
			log:      "couldn't read a released repository again",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r, logs := newKeptRig(nil)
			r.svc.progressEvery = 0
			r.refresherOn()
			tt.before(t, r)

			asked, before := r.github.calls(), r.events.Count()

			r.svc.released(t.Context(), tt.released, webDir, Result{Kind: ResultReleased})
			assert.True(t, servedNow(r.svc).Refreshing, "the list reads as refreshing while the patch is queued")

			r.patchBehind(t.Context(), t)

			if tt.after != nil {
				tt.after(r)
			}

			want := append([]event.Event{event.ReleasesUpdated{}}, tt.progress...)
			assert.Equal(t, append(want, event.ReleasesUpdated{}), r.events.All()[before:],
				"the window hears of the release at once, and that the list went out of date")
			assert.Equal(t, asked+tt.asks, r.github.calls(), "what the patch asked GitHub")
			assert.False(t, servedNow(r.svc).Refreshing, "nothing reads behind the list now")

			if tt.log == "" {
				assert.Empty(t, logs.String())
			} else {
				assert.Contains(t, logs.String(), tt.log)
			}

			scans := r.clones.scans()

			got, err := r.svc.Candidates(t.Context(), false)
			require.NoError(t, err)
			assert.Equal(t, scans+1, r.clones.scans(), "the next read reads the list again in full")
			assert.Contains(t, groups(got), "acme-org/web unreleased")
		})
	}
}

func TestAKeptListIsntPatched(t *testing.T) {
	t.Parallel()

	keeper := &memoryKeeper{}
	past, _ := newKeptRig(keeper)

	_, err := past.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	r, _ := newKeptRig(keeper)
	r.refresherOn()

	kept, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	require.True(t, kept.Kept, "the list kept from the past start is shown")

	// As fresh as a list can be, so only its being kept stops the patch.
	r.svc.mu.Lock()
	r.svc.tried = r.clock.Now()
	r.svc.mu.Unlock()

	r.github.releaseWeb()

	raw, _, err := keeper.Get(t.Context(), keptKey)
	require.NoError(t, err)

	before := r.events.Count()

	r.svc.released(t.Context(), name("web"), webDir, Result{Kind: ResultReleased})
	r.patchBehind(t.Context(), t)

	assert.Zero(t, r.github.calls(), "GitHub isn't asked: a fresh row would sit among the past start's")
	assert.Equal(t, []event.Event{event.ReleasesUpdated{}, event.ReleasesUpdated{}}, r.events.All()[before:])

	again, _, err := keeper.Get(t.Context(), keptKey)
	require.NoError(t, err)
	assert.Equal(t, raw, again, "nothing new is kept")

	got, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.True(t, got.Kept, "the list went out of date, and the read behind it is still to come")
	assert.Contains(t, groups(got), "acme-org/web unreleased")

	r.readBehind(t.Context(), t)

	read, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.False(t, read.Kept)
	assert.Contains(t, groups(read), "acme-org/web upToDate", "the read behind the page lists it as released")
	assert.Equal(t, 1, r.github.calls())
}

func TestAListWhoseLastReadFailedIsntPatched(t *testing.T) {
	t.Parallel()

	r := newRig()
	r.refresherOn()

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	r.github.fail(errGitHubDown)

	failed, err := r.svc.Candidates(t.Context(), true)
	require.NoError(t, err)
	require.NotEmpty(t, failed.Error, "the list from before is shown, with why it couldn't be read again")

	// GitHub answers again, and the list was asked for a moment ago: only the failure stops
	// the patch.
	r.github.fail(nil)
	r.github.releaseWeb()

	before := r.events.Count()

	r.svc.released(t.Context(), name("web"), webDir, Result{Kind: ResultReleased})
	r.patchBehind(t.Context(), t)

	assert.Equal(t, 2, r.github.calls(), "GitHub isn't asked: a fresh row would sit among an older list")
	assert.Equal(t, []event.Event{event.ReleasesUpdated{}, event.ReleasesUpdated{}}, r.events.All()[before:])

	got, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.Equal(t, 3, r.github.calls(), "the list went out of date, so it is read again in full")
	assert.Empty(t, got.Error)
	assert.Contains(t, groups(got), "acme-org/web upToDate")
}

func TestARefreshAfterAReleaseClearsItsPatch(t *testing.T) {
	t.Parallel()

	r := newRig()
	r.refresherOn()

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	r.github.releaseWeb()
	r.svc.released(t.Context(), name("web"), webDir, Result{Kind: ResultReleased})

	got, err := r.svc.Candidates(t.Context(), true)
	require.NoError(t, err)
	assert.Contains(t, groups(got), "acme-org/web upToDate", "Read GitHub again, after the release, reads web too")
	assert.Equal(t, 2, r.github.calls())
	assert.Len(t, r.github.askedAt(1), 7, "about the whole list, once")

	before := r.events.Count()

	r.patchBehind(t.Context(), t)

	assert.Equal(t, 2, r.github.calls(), "so the patch asks GitHub nothing")
	assert.Equal(t, []event.Event{event.ReleasesUpdated{}}, r.events.All()[before:],
		"and ends at once, telling the window")
	assert.False(t, servedNow(r.svc).Refreshing)

	served, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.False(t, served.Refreshing)
	assert.Contains(t, groups(served), "acme-org/web upToDate")
	assert.Equal(t, 2, r.github.calls(), "and the list stays fresh")
}

// heldGitHub is a GitHub whose next status read, once armed, answers at once, as GitHub
// was then, but returns only once hold is closed, as a read still going would.
type heldGitHub struct {
	*fakeGitHub

	armed   atomic.Bool
	holding chan struct{}
	hold    chan struct{}
}

func (h *heldGitHub) ReleaseStatus(
	ctx context.Context, repos []repo.Name, answered func(n int),
) (map[string]RepoStatus, error) {
	out, err := h.fakeGitHub.ReleaseStatus(ctx, repos, answered)

	if h.armed.CompareAndSwap(true, false) {
		close(h.holding)
		<-h.hold
	}

	return out, err
}

// newHeldRig returns a rig over a held GitHub, with its list read once and GitHub armed:
// the next read of the list is held once it has asked GitHub, while a release ends.
func newHeldRig(t *testing.T) (*rig, *heldGitHub) {
	t.Helper()

	r := newRig()
	held := &heldGitHub{fakeGitHub: r.github, holding: make(chan struct{}), hold: make(chan struct{})}
	r.svc = NewService(Config{
		GitHub: held, Clones: r.clones, Checker: r.checker, Home: "/Users/example", Clock: r.clock, Events: r.events,
	})

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	held.armed.Store(true)

	return r, held
}

// readAgain starts Read GitHub again, and returns where its list arrives once it has
// asked GitHub about every repository, before anything changed there.
func readAgain(t *testing.T, r *rig, held *heldGitHub) <-chan Candidates {
	t.Helper()

	out := make(chan Candidates, 1)

	go func() {
		got, _ := r.svc.Candidates(t.Context(), true)
		out <- got
	}()

	<-held.holding

	return out
}

func TestAPatchWaitsForARefreshUnderWay(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r, held := newHeldRig(t)
		r.refresherOn()

		refreshed := readAgain(t, r, held)

		r.github.releaseWeb()
		r.svc.released(t.Context(), name("web"), webDir, Result{Kind: ResultReleased})
		require.True(t, r.askedPatch())

		patched := make(chan struct{})

		go func() {
			defer close(patched)

			r.svc.patchReleased(t.Context())
		}()

		synctest.Wait()
		assert.Equal(t, 2, r.github.calls(), "the patch waits for the refresh under way")

		close(held.hold)

		old := <-refreshed
		assert.Contains(t, groups(old), "acme-org/web unreleased", "the refresh asked GitHub before the release")

		<-patched

		assert.Equal(t, []repo.Name{name("web")}, r.github.askedAt(2),
			"then the patch reads web again: the refresh began before the release, so it doesn't count")

		got, err := r.svc.Candidates(t.Context(), false)
		require.NoError(t, err)
		assert.Contains(t, groups(got), "acme-org/web upToDate", "and lands over the refresh")
		assert.False(t, got.Refreshing)
		assert.Equal(t, 3, r.github.calls())

		all := r.events.All()
		assert.Equal(t, event.ReleasesUpdated{}, all[len(all)-1], "the window hears the patch landed last")
	})
}

func TestARefreshUnderWayWhenTheListGoesOutOfDateLandsOutOfDate(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r, held := newHeldRig(t)

		refreshed := readAgain(t, r, held)

		r.github.releaseWeb()
		r.svc.released(t.Context(), name("web"), webDir, Result{Kind: ResultReleased})

		close(held.hold)
		<-refreshed

		got, err := r.svc.Candidates(t.Context(), false)
		require.NoError(t, err)
		assert.Equal(t, 3, r.github.calls(), "the refresh asked GitHub before the release, so the list is read again")
		assert.Contains(t, groups(got), "acme-org/web upToDate")
	})
}

func TestTheRefresherPatchesAReleasedRepository(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newRig()
		r.svc.progressEvery = 0

		ctx, stop := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			NewRefresher(r.svc).Run(ctx)
		}()

		synctest.Wait()
		require.True(t, running(r.svc))

		_, err := r.svc.Candidates(t.Context(), false)
		require.NoError(t, err)

		r.github.releaseWeb()

		before := r.events.Count()

		r.svc.released(t.Context(), name("web"), webDir, Result{Kind: ResultReleased})

		synctest.Wait()
		assert.Equal(t, []repo.Name{name("web")}, r.github.askedAt(1), "the Refresher read web again")
		assert.Equal(t, []event.Event{
			event.ReleasesUpdated{},
			progressOf(event.ReleasesAskingGitHub, 0, 1), progressOf(event.ReleasesAskingGitHub, 1, 1),
			event.ReleasesUpdated{},
		}, r.events.All()[before:])

		got, err := r.svc.Candidates(t.Context(), false)
		require.NoError(t, err)
		assert.Contains(t, groups(got), "acme-org/web upToDate")
		assert.False(t, got.Refreshing)
		assert.Equal(t, 2, r.github.calls())

		stop()
		<-done

		before = r.events.Count()

		r.svc.released(t.Context(), name("web"), webDir, Result{Kind: ResultReleased})
		assert.Equal(t, []event.Event{event.ReleasesUpdated{}}, r.events.All()[before:],
			"once it stops, a release puts the list out of date at once")
		assert.False(t, servedNow(r.svc).Refreshing)

		_, err = r.svc.Candidates(t.Context(), false)
		require.NoError(t, err)
		assert.Equal(t, 2, r.clones.scans(), "and the next read reads it again in full")
	})
}

func TestARefresherThatStopsLeavesItsPatchesOutOfDate(t *testing.T) {
	t.Parallel()

	r := newRig()
	r.refresherOn()

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	before := r.events.Count()

	r.svc.released(t.Context(), name("web"), webDir, Result{Kind: ResultReleased})
	require.True(t, servedNow(r.svc).Refreshing, "a patch is queued")

	// The app quits before the Refresher takes the patch up.
	require.True(t, r.askedPatch())

	ended, stop := context.WithCancel(t.Context())
	stop()

	NewRefresher(r.svc).Run(ended)

	assert.Equal(t, []event.Event{event.ReleasesUpdated{}, event.ReleasesUpdated{}}, r.events.All()[before:],
		"the window hears the release, then that the list went out of date")
	assert.False(t, servedNow(r.svc).Refreshing, "nothing reads behind the list now")
	assert.Equal(t, 1, r.github.calls(), "and nothing asked GitHub")

	_, err = r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.Equal(t, 2, r.clones.scans(), "the next read reads the list again in full")
}

func TestAReleaseEndingPatchesTheReleasePage(t *testing.T) {
	t.Parallel()

	r := newReleaseRig(t, rigOptions{base: shownVersion})
	r.magex("1.29.5", r.replay("signing-gpg-fails"))
	r.svc.progressEvery = 0

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		NewRefresher(r.svc).Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	require.Eventually(t, func() bool { return running(r.svc) }, runWait, runPoll)

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	before := r.events.Count()

	r.ended(r.release())

	require.Eventually(t, func() bool { return updates(r.events) == 3 }, runWait, runPoll, "the patch lands")
	assert.Equal(t, []event.Event{
		event.ReleasesUpdated{},
		progressOf(event.ReleasesAskingGitHub, 0, 1), progressOf(event.ReleasesAskingGitHub, 1, 1),
		event.ReleasesUpdated{},
	}, r.events.All()[before:], "the page hears the release at once, how far the patch has got, and that it landed")
	assert.Equal(t, 3, r.github.calls(), "the list, the sheet, and the patch")
	assert.Equal(t, []repo.Name{name("sdk")}, r.github.askedAt(2), "the patch asks about the released repository")

	got, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.False(t, got.Refreshing)
	assert.Len(t, r.events.All()[before:], 4, "the page reads the patched list, not the whole list again")
	assert.Equal(t, 3, r.github.calls())
}
