package manifests

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waitLimit bounds how long a test waits for a goroutine to get somewhere.
const waitLimit = 30 * time.Second

// waitFor waits for ch to close, failing the test after waitLimit.
func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()

	select {
	case <-ch:
	case <-time.After(waitLimit):
		require.FailNow(t, what)
	}
}

func TestTheAppsQuitEndsAReadAndWaitsForIt(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("api", moduleRepo(commitID(1), daysAgo(10), daysAgo(5)))

	var (
		once  sync.Once
		letGo atomic.Bool
	)

	entered := make(chan struct{})

	// The clone's tree waits until its read ends, as one on a disk that stopped answering
	// does, noting that it was let go.
	r.git.treeHook = func(ctx context.Context) {
		once.Do(func() { close(entered) })
		<-ctx.Done()
		letGo.Store(true)
	}

	life, quit := context.WithCancel(t.Context())
	stopped := make(chan struct{})

	go func() {
		defer close(stopped)

		NewScans(r.svc).Run(life)
	}()

	listed := make(chan error, 1)

	go func() {
		_, err := r.svc.List(t.Context(), false)
		listed <- err
	}()

	waitFor(t, entered, "the read never reached the clone")

	// The window's call is still waiting, and would until ScanTimeout: the quit ends the
	// read, and the worker returns only once it has.
	quit()
	waitFor(t, stopped, "the worker never returned")
	assert.True(t, letGo.Load(), "the read was ended before the worker returned")

	select {
	case err := <-listed:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(waitLimit):
		require.FailNow(t, "the window's call never returned")
	}

	r.svc.mu.Lock()
	has, kept := r.svc.has, len(r.svc.kept)
	r.svc.mu.Unlock()

	assert.False(t, has, "no page a read cut short read is kept")
	assert.Zero(t, kept, "nor any clone it was reading")

	_, err := r.svc.List(t.Context(), true)
	require.ErrorIs(t, err, errQuitting, "once the app is quitting, no read starts")
	assert.Equal(t, 1, r.clones.Reads().Dirs)
}
