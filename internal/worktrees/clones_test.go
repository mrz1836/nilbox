package worktrees

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
	"github.com/mrz1836/nilbox/internal/worklog"
)

// busySentence is what a click on a clone releaseRun holds tells the user.
const busySentence = "A release is using acme-org/example-repo right now."

// releaseRun is a release using a clone, as a run would hold it.
func releaseRun() clonelock.Holder {
	return clonelock.Holder{Kind: clonelock.KindRelease, Repo: "acme-org/example-repo", Title: "Release v1.4.3", RunID: 7}
}

// busyWith holds clone in svc's clone locks for h until the test ends, and returns the
// release, which may be called sooner.
func busyWith(t *testing.T, svc *Service, clone string, h clonelock.Holder) func() {
	t.Helper()

	release, _, ok := svc.clones.TryLock(clone, h)
	require.True(t, ok, "the clone is free")
	t.Cleanup(release)

	return release
}

// requireBusy checks that err is a busy clone, worded for the user as busySentence.
func requireBusy(t *testing.T, err error) {
	t.Helper()

	require.ErrorIs(t, err, clonelock.ErrBusy)
	assert.Equal(t, busySentence, uierr.Message(err), "the click names what is using the clone")
}

func TestClickRemovalNamesHolder(t *testing.T) {
	t.Parallel()

	// plain is a clone with one clean linked worktree.
	plain := func(t *testing.T) (*env, string, string) {
		t.Helper()

		e := newEnv(t)
		alpha := e.newRepo(t, "alpha")

		return e, alpha, addWorktree(t, alpha, filepath.Join(e.home, "wt", "feature"), "feature")
	}

	t.Run("remove", func(t *testing.T) {
		t.Parallel()

		e, alpha, wt := plain(t)
		busyWith(t, e.svc, alpha, releaseRun())

		_, err := e.svc.Remove(t.Context(), alpha, wt, []string{"feature"})
		requireBusy(t, err)
		assert.Contains(t, gitWorktreePaths(t, alpha), resolvedPath(wt), "nothing is removed")
		assert.Contains(t, branches(t, alpha), "feature")
	})

	t.Run("force remove", func(t *testing.T) {
		t.Parallel()

		e, alpha, wt := plain(t)
		dirty(t, wt)
		busyWith(t, e.svc, alpha, releaseRun())

		_, err := e.svc.ForceRemove(t.Context(), alpha, wt, nil)
		requireBusy(t, err)
		assert.Contains(t, gitWorktreePaths(t, alpha), resolvedPath(wt))
	})

	t.Run("remove all clean", func(t *testing.T) {
		t.Parallel()

		e, alpha, wt := plain(t)
		picks := cleanPicks(t, e.svc)
		require.Len(t, picks, 1)
		busyWith(t, e.svc, alpha, releaseRun())

		results, err := e.svc.RemoveAllClean(t.Context(), picks)
		require.NoError(t, err)
		require.Len(t, results, 1)
		assert.False(t, results[0].Removed)
		assert.Equal(t, busySentence, results[0].Reason)
		assert.Contains(t, gitWorktreePaths(t, alpha), resolvedPath(wt))
	})

	t.Run("prune", func(t *testing.T) {
		t.Parallel()

		e, alpha, _ := plain(t)
		busyWith(t, e.svc, alpha, releaseRun())

		requireBusy(t, e.svc.Prune(t.Context(), alpha))
	})

	t.Run("unlock", func(t *testing.T) {
		t.Parallel()

		e, alpha, wt := plain(t)
		busyWith(t, e.svc, alpha, releaseRun())

		requireBusy(t, e.svc.Unlock(t.Context(), alpha, wt))
	})

	t.Run("clean up", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		f := newSweepFixture(t, e)
		svc := e.linked(linkDeps{gh: f.gh, launches: f.launches})
		busyWith(t, svc, f.repo, releaseRun())

		_, err := svc.CleanUp(t.Context(), f.repo, f.launchedPR, nil)
		requireBusy(t, err)
		assert.Contains(t, gitWorktreePaths(t, f.repo), resolvedPath(f.launchedPR))

		f.gh.mu.Lock()
		defer f.gh.mu.Unlock()

		assert.Empty(t, f.gh.calls, "a busy clone is refused before GitHub is asked about it")
	})

	t.Run("clean up finished", func(t *testing.T) {
		t.Parallel()

		e := newEnv(t)
		f := newSweepFixture(t, e)
		svc := e.linked(linkDeps{gh: f.gh, launches: f.launches})
		picks := finishedPicks(t, svc)
		require.NotEmpty(t, picks)
		busyWith(t, svc, f.repo, releaseRun())

		results, err := svc.CleanUpFinished(t.Context(), picks)
		require.NoError(t, err)
		require.Len(t, results, len(picks))

		for _, res := range results {
			assert.False(t, res.Removed)
			assert.Equal(t, busySentence, res.Reason)
			assert.Contains(t, gitWorktreePaths(t, f.repo), resolvedPath(res.Path))
		}
	})

	t.Run("delete branches", func(t *testing.T) {
		t.Parallel()

		f := newMergedFixture(t)
		busyWith(t, f.svc, f.clone, releaseRun())

		got, err := f.svc.DeleteBranches(t.Context(), []BranchPick{{RepoPath: f.clone, Name: "feature-x", Head: f.heads["feature-x"]}})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.False(t, got[0].Deleted)
		assert.Equal(t, busySentence, got[0].Reason)
		assert.Contains(t, branches(t, f.clone), "feature-x")
	})
}

func TestSweepSkipsBusyClone(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newSweepFixture(t, e)
	svc := e.linked(linkDeps{gh: f.gh, launches: f.launches, auto: true})
	logs := &testutil.Logs{}
	svc.logger = logs.Logger()
	release := busyWith(t, svc, f.repo, releaseRun())

	report := svc.Sweep(t.Context())

	assert.Empty(t, report.Cleaned, "nothing is cleaned up in a clone a run is using")
	assert.Empty(t, report.Failed, "and nothing failed: the next sweep tries again")
	assert.Empty(t, report.Error)
	assert.Equal(t, 6, report.Leftover, "the three it would have cleaned up are left, with the three it always leaves")

	paths := gitWorktreePaths(t, f.repo)
	for _, kept := range []string{f.launchedPR, f.commitLinked, f.launchedIssue} {
		assert.Contains(t, paths, resolvedPath(kept))
	}

	assert.Equal(t, 1, strings.Count(logs.String(), "the sweep skipped a clone another job is using"), "once for the clone")
	assert.Contains(t, logs.String(), "job=release")

	release()

	again := svc.Sweep(t.Context())
	assert.Len(t, again.Cleaned, 3, "once the run ends, the next sweep cleans up")
	assert.Empty(t, again.Failed)
}

func TestForgetStaleSkipsBusyClone(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	repo := e.newGitHubRepo(t, "example-repo")
	gone, _ := agentWorktree(t, repo, "pr-2")
	testutil.RunGit(t, repo, "worktree", "remove", gone)

	launches := &fakeLaunches{records: []worklog.Record{launchOf(repo, "pr-2", item.KindPR, 2)}}
	svc := e.linked(linkDeps{gh: &fakeGitHub{}, launches: launches})
	release := busyWith(t, svc, repo, releaseRun())

	_, err := svc.List(t.Context())
	require.NoError(t, err, "a listing never waits on a busy clone")
	assert.Empty(t, launches.forgotten, "the record stays for the next listing")
	assert.Len(t, launches.all(), 1)

	release()

	_, err = svc.List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{"example-repo/pr-2"}, launches.forgotten, "the next listing forgets it")
}

func TestMakerFailsFastOnRunHolder(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	records := &fakeLaunches{}
	m, svc := e.maker(records)
	checks := clonelock.Holder{Kind: clonelock.KindChecks, Repo: "acme-org/example-repo", RunID: 3}
	busyWith(t, svc, f.clone, checks)

	// Ensure's own limit is minutes; a Maker that waited would end with this ctx instead.
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()

	_, err := m.Ensure(ctx, f.request(item.KindPR, 7))
	require.ErrorIs(t, err, clonelock.ErrBusy, "a launch never waits behind a run")
	assert.Equal(t, "A checks run is using acme-org/example-repo right now.", uierr.Message(err))
	assert.NoDirExists(t, ManagedRoot(f.clone), "nothing is made")
	assert.NotContains(t, branches(t, f.clone), pr7Branch, "nothing is fetched")
	assert.Empty(t, records.all())
}

func TestMakerWaitsForMaker(t *testing.T) {
	t.Parallel()

	e := newEnv(t)
	f := newRemoteFixture(t, e)
	records := &fakeLaunches{}
	m, svc := e.maker(records)

	type outcome struct {
		got Ensured
		err error
	}

	synctest.Test(t, func(t *testing.T) {
		// Another launch is making a worktree in the same clone.
		release := busyWith(t, svc, f.clone, svc.worktreeHolder(f.clone))
		done := make(chan outcome, 1)

		go func() {
			got, err := m.Ensure(t.Context(), f.request(item.KindPR, 7))
			done <- outcome{got, err}
		}()

		// Ensure runs no git before it has the clone, so once everything in the bubble is
		// blocked, it is blocked on the clone's lock.
		synctest.Wait()

		select {
		case got := <-done:
			require.Failf(t, "Ensure returned while another launch held the clone", "%v", got.err)
		default:
		}

		release()

		got := <-done
		require.NoError(t, got.err, "it waited for the other launch, then made its worktree")
		assert.Equal(t, ActionCreate, got.got.Action)
		assert.Contains(t, gitWorktreePaths(t, f.clone), resolvedPath(got.got.Path))
	})
}

func TestHoldCloneWaitsOnlyForWorktreeChanges(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		svc := NewService(Config{})
		clone := t.TempDir()

		// A run fails a removal at once, naming the run.
		run := busyWith(t, svc, clone, releaseRun())

		_, err := svc.holdClone(t.Context(), clone)
		requireBusy(t, err)
		run()

		// A listing forgetting a record holds the clone a moment: a removal waits for it.
		listing := busyWith(t, svc, clone, svc.worktreeHolder(clone))
		got := make(chan error, 1)

		go func() {
			release, holdErr := svc.holdClone(t.Context(), clone)
			if holdErr == nil {
				held, _ := svc.clones.Holder(clone)
				assert.Equal(t, clonelock.Holder{Kind: clonelock.KindWorktree, Repo: clone}, held, "named by its folder")
				release()
			}

			got <- holdErr
		}()

		synctest.Wait()

		select {
		case holdErr := <-got:
			require.Failf(t, "holdClone returned while another worktree change held the clone", "%v", holdErr)
		default:
		}

		listing()
		require.NoError(t, <-got)

		// A wait that outlasts its ctx says so in a sentence, not as a bare deadline.
		slow := busyWith(t, svc, clone, svc.worktreeHolder(clone))
		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)

		defer cancel()

		_, err = svc.holdClone(ctx, clone)
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, "Another worktree change in "+clone+" took too long. Try again in a moment.", uierr.Message(err))
		slow()
	})
}
