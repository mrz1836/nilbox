package hooks

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/gitread"
)

// stuckGit is git whose read of where a clone's hooks are waits until its context ends, as
// one on a disk that stopped answering does, noting that it was let go.
type stuckGit struct {
	Git

	entered chan struct{}
	once    sync.Once
	letGo   atomic.Bool
}

func (g *stuckGit) HookPaths(ctx context.Context, _ string) (gitread.HookPaths, error) {
	g.once.Do(func() { close(g.entered) })

	<-ctx.Done()
	g.letGo.Store(true)

	return gitread.HookPaths{}, ctx.Err()
}

// waitFor waits for ch to close, failing the test after runWait.
func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(runWait):
		require.FailNow(t, what)
	}
}

func TestTheAppsQuitEndsAScanAndWaitsForIt(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("api", cloneOpts{config: coreEnv})

	git := &stuckGit{Git: r.git, entered: make(chan struct{})}
	r.svc.git = git

	life, quit := context.WithCancel(t.Context())
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)

		NewScans(r.svc).Run(life)
	}()

	scanned := make(chan error, 1)

	go func() {
		_, err := r.svc.Repos(t.Context(), false)
		scanned <- err
	}()

	waitFor(t, git.entered, "the scan never reached the clone")

	// The window's call is still waiting, and would until ScanTimeout: the quit ends the
	// scan, and the worker returns only once it has.
	quit()
	waitFor(t, stopped, "the worker never returned")
	assert.True(t, git.letGo.Load(), "the scan was ended before the worker returned")

	select {
	case err := <-scanned:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(runWait):
		require.FailNow(t, "the window's call never returned")
	}

	r.svc.mu.Lock()
	has := r.svc.has
	r.svc.mu.Unlock()
	assert.False(t, has, "nothing a scan cut short read is kept")

	// Once the app is quitting, no scan starts.
	_, err := r.svc.Repos(t.Context(), true)
	require.ErrorIs(t, err, errQuitting)
	assert.Equal(t, 1, r.clones.listings())
}

func TestScansReturnsAtOnceWithNoScanUnderWay(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("api", cloneOpts{config: coreEnv})
	r.page(false) // a scan that ended before the quit holds nothing up

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	NewScans(r.svc).Run(ctx)

	_, err := r.svc.Repos(t.Context(), true)
	require.ErrorIs(t, err, errQuitting)
}
