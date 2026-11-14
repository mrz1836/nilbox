package release

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/repo"
)

// setChecks has GitHub answer that the newest commit of acme-org/<repoName> has checks.
func (f *fakeGitHub) setChecks(repoName string, checks Checks) {
	f.mu.Lock()
	defer f.mu.Unlock()

	status := f.statuses[name(repoName).Key()]
	status.Checks = checks
	f.statuses[name(repoName).Key()] = status
}

// checksRig is the rig with web's newest commit's checks running, its list kept in
// memory and read under rules the test sets, telling the window how far each read has got
// at once, so a read that told would show.
func checksRig() (*rig, *fakeRules, *memoryKeeper) {
	r := newRig()
	rules, keeper := &fakeRules{}, &memoryKeeper{}

	r.github.setChecks("web", ChecksPending)
	r.svc = NewService(Config{
		GitHub: r.github, Clones: r.clones, Checker: r.checker, Home: "/Users/example", Clock: r.clock,
		Events: r.events, Keeper: keeper, Rules: rules,
	})
	r.svc.progressEvery = 0

	return r, rules, keeper
}

// keptRow returns the row of repoText in the list keeper holds.
func keptRow(t *testing.T, keeper *memoryKeeper, repoText string) Candidate {
	t.Helper()

	raw, found, err := keeper.Get(t.Context(), keptKey)
	require.NoError(t, err)
	require.True(t, found)

	kept, _, ok, err := decodeKept(raw)
	require.NoError(t, err)
	require.True(t, ok)

	return rowOf(t, kept, repoText)
}

func TestFollowChecksReadsOnlyTheRunningRows(t *testing.T) {
	t.Parallel()

	r, rules, keeper := checksRig()
	r.github.setChecks("fresh", ChecksExpected)

	first, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	require.Equal(t, ChecksPending, rowOf(t, first, "acme-org/web").Checks)
	require.Equal(t, ChecksSuccess, rowOf(t, first, "acme-org/sdk").Checks)

	// fresh's checks run too, but it is muted once the list is read.
	rules.set(nil, []string{"acme-org/fresh"})

	r.clock.Advance(FollowChecksInterval)
	r.github.setChecks("web", ChecksSuccess)

	refreshing := false
	r.github.asking = func() { refreshing = refreshing || servedNow(r.svc).Refreshing }
	before := r.events.Count()

	require.NoError(t, r.svc.followChecks(t.Context()))

	assert.Equal(t, 2, r.github.calls())
	assert.Equal(t, []repo.Name{name("web")}, r.github.askedAt(1),
		"GitHub is asked about the repository whose checks run alone, never the muted one")
	assert.Equal(t, []event.Event{event.ReleasesUpdated{}}, r.events.All()[before:],
		"the window hears once that the row changed, and never how far the read got")
	assert.False(t, refreshing, "the list never reads as refreshing")

	got, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.Equal(t, 2, r.github.calls(), "the followed list is served without reading GitHub again")
	assert.False(t, got.Refreshing)
	assert.Equal(t, ChecksSuccess, rowOf(t, got, "acme-org/web").Checks)
	assert.Equal(t, first.CheckedAt, got.CheckedAt, "the list is as old as it was")
	assert.Equal(t, rowOf(t, first, "acme-org/sdk"), rowOf(t, got, "acme-org/sdk"), "the rest is as it was")
	assert.Equal(t, ChecksSuccess, keptRow(t, keeper, "acme-org/web").Checks, "and kept for the next start")

	r.clock.Advance(FollowChecksInterval)
	require.NoError(t, r.svc.followChecks(t.Context()))
	assert.Equal(t, 2, r.github.calls(), "once its checks ended, a round asks GitHub nothing")
}

func TestFollowChecksTellsTheWindowOnlyWhenARowChanged(t *testing.T) {
	t.Parallel()

	r, _, keeper := checksRig()

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	require.NoError(t, keeper.Set(t.Context(), keptKey, "as kept"))

	before := r.events.Count()

	for range 3 {
		r.clock.Advance(FollowChecksInterval)
		require.NoError(t, r.svc.followChecks(t.Context()))
	}

	assert.Equal(t, 4, r.github.calls(), "each round reads web again while its checks run")
	assert.Equal(t, before, r.events.Count(), "but tells the window nothing while nothing changed")

	raw, _, err := keeper.Get(t.Context(), keptKey)
	require.NoError(t, err)
	assert.Equal(t, "as kept", raw, "nor keeps the list again")

	r.clock.Advance(FollowChecksInterval)
	r.github.setChecks("web", ChecksFailure)
	require.NoError(t, r.svc.followChecks(t.Context()))

	assert.Equal(t, []event.Event{event.ReleasesUpdated{}}, r.events.All()[before:])
	assert.Equal(t, ChecksFailure, keptRow(t, keeper, "acme-org/web").Checks)
}

func TestFollowChecksPicksUpEveryFieldOfTheRowsItReads(t *testing.T) {
	t.Parallel()

	r, _, _ := checksRig()

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	// Someone pushed to web while its checks ran: a new head, and a commit more to release.
	r.github.mu.Lock()
	web := r.github.statuses[name("web").Key()]
	web.HeadOID, web.Headline, web.Checks = "w3", "Add a banner", ChecksSuccess
	r.github.statuses[name("web").Key()] = web
	r.github.compares[name("web").Key()] = Compare{Ahead: 2, Commits: []Commit{
		{OID: "w2", Headline: "Bump vite", At: testNow().Add(-time.Minute)},
		{OID: "w3", Headline: "Add a banner", At: testNow()},
	}}
	r.github.mu.Unlock()

	r.clock.Advance(FollowChecksInterval)
	require.NoError(t, r.svc.followChecks(t.Context()))

	got := rowOf(t, servedNow(r.svc), "acme-org/web")
	assert.Equal(t, ChecksSuccess, got.Checks)
	assert.Equal(t, "Add a banner", got.Headline)
	assert.Equal(t, 2, got.Ahead)
	require.NotNil(t, got.Clone)
	assert.Equal(t, "main@w3", got.Clone.Branch, "the clone is read against the new head")
}

func TestFollowChecksAsksNothingWithNothingToFollow(t *testing.T) {
	t.Parallel()

	for name, setup := range map[string]func(t *testing.T, r *rig){
		"no list read yet": func(*testing.T, *rig) {},
		"no checks running": func(t *testing.T, r *rig) {
			t.Helper()
			r.github.setChecks("web", ChecksSuccess)

			_, err := r.svc.Candidates(t.Context(), false)
			require.NoError(t, err)
		},
		"a list out of date": func(t *testing.T, r *rig) {
			t.Helper()

			_, err := r.svc.Candidates(t.Context(), false)
			require.NoError(t, err)

			r.svc.mu.Lock()
			r.svc.outOfDate()
			r.svc.mu.Unlock()
		},
		"a last read that failed": func(t *testing.T, r *rig) {
			t.Helper()

			_, err := r.svc.Candidates(t.Context(), false)
			require.NoError(t, err)

			r.github.fail(errGitHubDown)

			failed, err := r.svc.Candidates(t.Context(), true)
			require.NoError(t, err)
			require.NotEmpty(t, failed.Error)

			r.github.fail(nil)
		},
		"a list from other locations": func(t *testing.T, r *rig) {
			t.Helper()

			_, err := r.svc.Candidates(t.Context(), false)
			require.NoError(t, err)

			r.clones.moveTo("/Users/example/work")
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			r, _, _ := checksRig()
			setup(t, r)

			calls, events := r.github.calls(), r.events.Count()

			r.clock.Advance(FollowChecksInterval)
			require.NoError(t, r.svc.followChecks(t.Context()))
			assert.Equal(t, calls, r.github.calls(), "GitHub is asked nothing")
			assert.Equal(t, events, r.events.Count(), "and the window told nothing")
		})
	}
}

func TestFollowChecksLeavesAListKeptFromAPastStart(t *testing.T) {
	t.Parallel()

	first, _, keeper := checksRig()

	_, err := first.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	next, _ := newKeptRig(keeper)
	next.svc.restore(t.Context())
	require.True(t, servedNow(next.svc).Kept)
	require.Equal(t, ChecksPending, rowOf(t, servedNow(next.svc), "acme-org/web").Checks)

	next.clock.Advance(FollowChecksInterval)
	require.NoError(t, next.svc.followChecks(t.Context()))
	assert.Zero(t, next.github.calls(), "a kept list is read again in full, not followed")
}

func TestFollowChecksLeavesARowJustRead(t *testing.T) {
	t.Parallel()

	r, _, _ := checksRig()
	r.refresherOn()

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	require.NoError(t, r.svc.followChecks(t.Context()))
	assert.Equal(t, 1, r.github.calls(), "the refresh just read it")

	r.clock.Advance(followChecksFresh - time.Second)
	require.NoError(t, r.svc.followChecks(t.Context()))
	assert.Equal(t, 1, r.github.calls(), "nor a moment later")

	r.clock.Advance(time.Second)
	require.NoError(t, r.svc.followChecks(t.Context()))
	assert.Equal(t, 2, r.github.calls(), "but once it is no longer fresh")

	r.clock.Advance(time.Minute)
	r.github.releaseWeb()
	r.svc.released(t.Context(), name("web"), webDir, Result{Kind: ResultReleased})
	r.patchBehind(t.Context(), t)
	require.Equal(t, 3, r.github.calls())

	require.NoError(t, r.svc.followChecks(t.Context()))
	assert.Equal(t, 3, r.github.calls(), "a patch that just read it leaves it too")
}

func TestFollowChecksLeavesAnOldCommitsChecks(t *testing.T) {
	t.Parallel()

	r, _, _ := checksRig()
	r.github.setChecks("cli", ChecksExpected) // its newest commit is two days old

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	r.clock.Advance(FollowChecksInterval)
	require.NoError(t, r.svc.followChecks(t.Context()))
	assert.Equal(t, []repo.Name{name("web")}, r.github.askedAt(1), "a check never reported isn't followed for good")

	r.clock.Advance(followChecksFor)
	require.NoError(t, r.svc.followChecks(t.Context()))
	assert.Equal(t, 2, r.github.calls(), "and neither is web's, once its commit is as old")

	_, err = r.svc.Candidates(t.Context(), true)
	require.NoError(t, err)
	assert.Equal(t, 3, r.github.calls(), "Read GitHub again still reads them")
}

func TestFollowChecksReadsFifteenARoundTheLongestUnreadFirst(t *testing.T) {
	t.Parallel()

	r, _, _ := checksRig()
	r.clones.list = nil

	all := make([]repo.Name, 0, 20)

	for i := range 20 {
		n := name(fmt.Sprintf("r%02d", i))
		all = append(all, n)
		r.clones.list = append(r.clones.list, Clone{Dir: "/Users/example/projects/" + n.Repo, Origin: n, OnHub: true})
		r.github.statuses[n.Key()] = RepoStatus{
			Permission: "WRITE", DefaultBranch: "main", HeadOID: "h", HeadAt: testNow().Add(-time.Minute),
			Checks: ChecksPending,
		}
	}

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	r.clock.Advance(FollowChecksInterval)
	require.NoError(t, r.svc.followChecks(t.Context()))
	assert.Equal(t, all[:followChecksMax], r.github.askedAt(1), "one batch's worth")

	r.clock.Advance(FollowChecksInterval)
	require.NoError(t, r.svc.followChecks(t.Context()))
	assert.Len(t, r.github.askedAt(2), followChecksMax)
	assert.Subset(t, r.github.askedAt(2), all[followChecksMax:], "the five left out come first")
	assert.Subset(t, r.github.askedAt(2), all[:10], "then the longest unread of the rest")
}

func TestFollowChecksThatFailsLeavesTheListCurrent(t *testing.T) {
	t.Parallel()

	r, _, _ := checksRig()

	first, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	r.clock.Advance(FollowChecksInterval)
	r.github.fail(errGitHubDown)
	before := r.events.Count()

	require.ErrorIs(t, r.svc.followChecks(t.Context()), errGitHubDown)
	assert.Equal(t, before, r.events.Count(), "the window hears nothing")

	served := servedNow(r.svc)
	assert.False(t, served.Refreshing)
	assert.Empty(t, served.Error, "the page shows no failure for a follow")
	assert.Equal(t, groups(first), groups(served))

	r.github.fail(nil)

	_, err = r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	assert.Equal(t, 2, r.github.calls(), "the list is still current, so it isn't read again in full")

	r.github.setChecks("web", ChecksSuccess)
	r.clock.Advance(FollowChecksInterval)
	require.NoError(t, r.svc.followChecks(t.Context()))
	assert.Equal(t, 3, r.github.calls(), "the next round tries again")
	assert.Equal(t, ChecksSuccess, rowOf(t, servedNow(r.svc), "acme-org/web").Checks)
}

func TestFollowChecksCutShortChangesNothing(t *testing.T) {
	t.Parallel()

	r, _, _ := checksRig()

	_, err := r.svc.Candidates(t.Context(), false)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())

	r.github.setChecks("web", ChecksSuccess)
	r.github.asking = cancel
	r.clock.Advance(FollowChecksInterval)
	before := r.events.Count()

	require.NoError(t, r.svc.followChecks(ctx), "the app quitting is no failure")
	assert.Equal(t, before, r.events.Count())
	assert.Equal(t, ChecksPending, rowOf(t, servedNow(r.svc), "acme-org/web").Checks)
}

// countingFollower counts its rounds.
type countingFollower struct {
	count atomic.Int32
}

func (f *countingFollower) follow(context.Context) error {
	f.count.Add(1)

	return nil
}

func TestChecksFollowerFollowsEveryIntervalUntilCanceled(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		f := &countingFollower{}
		follower := newChecksFollower(f.follow, nil, slog.New(slog.DiscardHandler))

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			follower.Run(ctx)
		}()

		synctest.Wait()
		assert.Equal(t, int32(1), f.count.Load())

		time.Sleep(2*FollowChecksInterval + time.Second)
		synctest.Wait()
		assert.Equal(t, int32(3), f.count.Load(), "every 45 seconds")

		cancel()
		<-done
	})

	assert.Equal(t, 45*time.Second, FollowChecksInterval)
	assert.Equal(t, FollowChecksInterval, NewChecksFollower(newRig().svc, nil, nil).every(t.Context()))
}

func TestChecksFollowerOffFollowsNothingUntilOn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var on atomic.Bool

		f := &countingFollower{}
		follower := newChecksFollower(f.follow, func(context.Context) time.Duration {
			if on.Load() {
				return 30 * time.Second
			}

			return 0
		}, nil)

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			follower.Run(ctx)
		}()

		time.Sleep(time.Hour)
		synctest.Wait()
		assert.Zero(t, f.count.Load(), "off follows nothing, not even at once")

		on.Store(true)
		follower.Wake()
		synctest.Wait()
		assert.Equal(t, int32(1), f.count.Load())

		time.Sleep(30*time.Second + time.Second)
		synctest.Wait()
		assert.Equal(t, int32(2), f.count.Load(), "then every 30 seconds, as the setting says")

		cancel()
		<-done
	})
}
