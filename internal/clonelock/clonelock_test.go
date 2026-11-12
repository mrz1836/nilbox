package clonelock_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clonelock"
)

// TestMain fails the run when a test leaves a goroutine behind.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// releaseJob and worktreeJob hold a clone in these tests.
func releaseJob() clonelock.Holder {
	return clonelock.Holder{Kind: clonelock.KindRelease, Repo: "acme-org/sdk", Title: "Release v1.4.3", RunID: 7}
}

func worktreeJob() clonelock.Holder {
	return clonelock.Holder{Kind: clonelock.KindWorktree, Repo: "acme-org/sdk"}
}

// onlyWorktrees waits only for another worktree change.
func onlyWorktrees(h clonelock.Holder) bool { return h.Kind == clonelock.KindWorktree }

func TestTryLockReportsHolder(t *testing.T) {
	t.Parallel()

	var set clonelock.Set

	clone := t.TempDir()

	unlock, _, ok := set.TryLock(clone, releaseJob())
	require.True(t, ok, "a free clone is taken")

	_, held, ok := set.TryLock(clone, worktreeJob())
	assert.False(t, ok, "a held clone is refused")
	assert.Equal(t, releaseJob(), held, "naming the job that holds it")

	got, busy := set.Holder(clone)
	assert.True(t, busy)
	assert.Equal(t, releaseJob(), got)

	other, _, ok := set.TryLock(t.TempDir(), worktreeJob())
	require.True(t, ok, "each clone has its own lock")
	other()

	unlock()
	unlock() // a second release does nothing

	_, busy = set.Holder(clone)
	assert.False(t, busy, "released")

	again, _, ok := set.TryLock(clone, worktreeJob())
	require.True(t, ok, "taken again once released")
	again()
}

func TestLockHonorsCtx(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		set := clonelock.New()
		clone := t.TempDir()

		unlock, _, ok := set.TryLock(clone, releaseJob())
		require.True(t, ok)

		ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
		defer cancel()

		start := time.Now()
		_, err := set.Lock(ctx, clone, worktreeJob())

		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, time.Minute, time.Since(start), "it waited until ctx ended, and no longer")

		got, _ := set.Holder(clone)
		assert.Equal(t, releaseJob(), got, "the holder kept the clone")

		ended, endNow := context.WithCancel(t.Context())
		endNow()
		unlock()

		_, err = set.Lock(ended, clone, worktreeJob())
		require.ErrorIs(t, err, context.Canceled, "an ended ctx fails even on a free clone")

		_, busy := set.Holder(clone)
		assert.False(t, busy)
	})
}

func TestLockWaitsForTheHolderToLetGo(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		set := clonelock.New()
		clone := t.TempDir()

		unlock, _, ok := set.TryLock(clone, releaseJob())
		require.True(t, ok)

		got := make(chan error, 1)

		go func() {
			next, err := set.Lock(t.Context(), clone, worktreeJob())
			if err == nil {
				next()
			}

			got <- err
		}()

		synctest.Wait()

		select {
		case err := <-got:
			require.Failf(t, "Lock returned while the clone was held", "%v", err)
		default:
		}

		unlock()
		require.NoError(t, <-got, "the waiter takes the clone once it is let go")
	})
}

func TestLockWaitingForFailsFastOnOtherHolders(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		set := clonelock.New()
		clone := t.TempDir()

		unlock, _, ok := set.TryLock(clone, releaseJob())
		require.True(t, ok)

		_, err := set.LockWaitingFor(t.Context(), clone, worktreeJob(), onlyWorktrees)
		require.ErrorIs(t, err, clonelock.ErrBusy, "a release isn't waited for")

		busy, ok := errors.AsType[*clonelock.BusyError](err)
		require.True(t, ok)
		assert.Equal(t, releaseJob(), busy.Holder)
		assert.Equal(t, "A release is using acme-org/sdk right now.", err.Error())

		unlock()

		maker, _, ok := set.TryLock(clone, worktreeJob())
		require.True(t, ok)

		got := make(chan error, 1)

		go func() {
			next, lockErr := set.LockWaitingFor(t.Context(), clone, worktreeJob(), onlyWorktrees)
			if lockErr == nil {
				next()
			}

			got <- lockErr
		}()

		synctest.Wait()

		select {
		case err := <-got:
			require.Failf(t, "another worktree change is waited for, but it returned", "%v", err)
		default:
		}

		maker()
		require.NoError(t, <-got)
	})
}

func TestSymlinkedPathSharesLock(t *testing.T) {
	t.Parallel()

	var set clonelock.Set

	clone := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	require.NoError(t, os.Symlink(clone, link))

	unlock, _, ok := set.TryLock(link, releaseJob())
	require.True(t, ok)

	defer unlock()

	for _, path := range []string{clone, link, clone + string(filepath.Separator), filepath.Join(link, "sub", "..")} {
		_, held, taken := set.TryLock(path, worktreeJob())
		assert.False(t, taken, "%s names the same clone", path)
		assert.Equal(t, releaseJob(), held)
	}

	missing := filepath.Join(link, "not-cloned-yet")
	inside, _, ok := set.TryLock(missing, releaseJob())
	require.True(t, ok, "a folder inside the clone is a clone of its own")

	defer inside()

	_, _, ok = set.TryLock(filepath.Join(clone, "not-cloned-yet"), worktreeJob())
	assert.False(t, ok, "a missing path resolves through its nearest existing parent")
}

func TestLockKeepsJobsApart(t *testing.T) {
	t.Parallel()

	var (
		set    clonelock.Set
		inside atomic.Int32
		wrong  atomic.Bool
		wg     sync.WaitGroup
	)

	clone := t.TempDir()

	for range 16 {
		wg.Go(func() {
			if !lockAndLeave(t.Context(), &set, clone, &inside, 50) {
				wrong.Store(true)
			}
		})
	}

	wg.Wait()
	assert.False(t, wrong.Load(), "one job at a time holds a clone")
}

// lockAndLeave takes the clone and lets it go times times, counting the jobs inside in
// inside, and reports whether it was alone inside every time.
func lockAndLeave(ctx context.Context, set *clonelock.Set, clone string, inside *atomic.Int32, times int) bool {
	alone := true

	for range times {
		unlock, err := set.Lock(ctx, clone, worktreeJob())
		if err != nil {
			return false
		}

		if inside.Add(1) != 1 {
			alone = false
		}

		inside.Add(-1)
		unlock()
	}

	return alone
}

func TestSentence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		holder clonelock.Holder
		want   string
	}{
		{releaseJob(), "A release is using acme-org/sdk right now."},
		{clonelock.Holder{Kind: clonelock.KindHooks, Repo: "acme-org/sdk"}, "A hooks install is using acme-org/sdk right now."},
		{clonelock.Holder{Kind: clonelock.KindChecks, Repo: "acme-org/sdk"}, "A checks run is using acme-org/sdk right now."},
		{clonelock.Holder{Kind: clonelock.KindScript, Repo: "acme-org/sdk"}, "A script is using acme-org/sdk right now."},
		{worktreeJob(), "A worktree change is using acme-org/sdk right now."},
		{clonelock.Holder{Kind: clonelock.KindPull, Repo: "acme-org/sdk"}, "A pull is using acme-org/sdk right now."},
		{clonelock.Holder{Kind: clonelock.KindUpdate, Repo: "~/.local/bin"}, "A tool update is using ~/.local/bin right now."},
		{clonelock.Holder{Kind: clonelock.KindInstall, Repo: "~/.local/bin"}, "A tool install is using ~/.local/bin right now."},
		{clonelock.Holder{Kind: clonelock.KindOrphan, Repo: "acme-org/sdk"}, "A run from before " + brand.Name + " restarted is using acme-org/sdk right now."},
		{clonelock.Holder{Kind: "unknown"}, "Another job is using this clone right now."},
	}

	for _, tt := range tests {
		t.Run(string(tt.holder.Kind), func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, tt.holder.Sentence())
			assert.Equal(t, tt.want, (&clonelock.BusyError{Holder: tt.holder}).Error())
		})
	}
}
