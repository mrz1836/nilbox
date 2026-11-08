package flight_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/mrz1836/nilbox/internal/flight"
)

// TestMain fails the run when a test leaves a goroutine behind.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// waitTimeout bounds every wait in these tests.
const waitTimeout = 5 * time.Second

var errFake = errors.New("fake: the call failed")

// gate is a call that blocks until released, counting how often it started.
type gate struct {
	started chan struct{}
	release chan struct{}
	calls   atomic.Int32
}

func newGate() *gate {
	return &gate{started: make(chan struct{}, 8), release: make(chan struct{})}
}

// call counts itself, says it started, and returns n once released, or ctx's error.
func (g *gate) call(n int) func(context.Context) (int, error) {
	return func(ctx context.Context) (int, error) {
		g.calls.Add(1)

		g.started <- struct{}{}

		select {
		case <-g.release:
			return n, nil
		case <-ctx.Done():
			return 0, ctx.Err()
		}
	}
}

// waitStarted waits for one call to start.
func (g *gate) waitStarted(t *testing.T) {
	t.Helper()

	select {
	case <-g.started:
	case <-time.After(waitTimeout):
		require.FailNow(t, "the call never started")
	}
}

// result is what one Do returned.
type result struct {
	n   int
	err error
}

// doAsync runs Do in the background and returns where its result arrives.
func doAsync(ctx context.Context, g *flight.Group[int], fn func(context.Context) (int, error)) <-chan result {
	out := make(chan result, 1)

	go func() {
		n, err := g.Do(ctx, "key", fn)
		out <- result{n, err}
	}()

	return out
}

// receive waits for a result.
func receive(t *testing.T, ch <-chan result) result {
	t.Helper()

	select {
	case r := <-ch:
		return r
	case <-time.After(waitTimeout):
		require.FailNow(t, "Do never returned")

		return result{}
	}
}

// The tests that start a second caller run in a synctest bubble: synctest.Wait returns
// once every goroutine in it is blocked, which is when the second caller has joined the
// call in flight, so no pause has to stand in for that.

func TestCallersJoinTheCallInFlight(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var g flight.Group[int]

		calls := newGate()
		first := doAsync(t.Context(), &g, calls.call(1))
		calls.waitStarted(t)

		second := doAsync(t.Context(), &g, calls.call(2))

		synctest.Wait()
		close(calls.release)

		assert.Equal(t, result{n: 1}, receive(t, first))
		assert.Equal(t, result{n: 1}, receive(t, second), "the second caller shares the first call's result")
		assert.Equal(t, int32(1), calls.calls.Load())
	})
}

func TestACallAfterTheLastRunsAgain(t *testing.T) {
	t.Parallel()

	var (
		g     flight.Group[int]
		calls atomic.Int32
	)

	count := func(context.Context) (int, error) { return int(calls.Add(1)), nil }

	for want := 1; want <= 2; want++ {
		n, err := g.Do(t.Context(), "key", count)
		require.NoError(t, err)
		assert.Equal(t, want, n)
	}
}

func TestACallerStopsWaitingWhenItsContextEnds(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var g flight.Group[int]

		calls := newGate()
		first := doAsync(t.Context(), &g, calls.call(1))
		calls.waitStarted(t)

		ctx, cancel := context.WithCancel(t.Context())
		second := doAsync(ctx, &g, calls.call(2))

		synctest.Wait()
		cancel()

		require.ErrorIs(t, receive(t, second).err, context.Canceled)

		close(calls.release)
		assert.Equal(t, result{n: 1}, receive(t, first), "the call goes on for the caller that started it")
	})
}

func TestAWaitingCallerStartsAgainWhenTheFirstCallerLeaves(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var g flight.Group[int]

		calls := newGate()
		ctx, cancel := context.WithCancel(t.Context())
		first := doAsync(ctx, &g, calls.call(1))
		calls.waitStarted(t)

		second := doAsync(t.Context(), &g, calls.call(2))

		synctest.Wait()
		cancel()

		require.ErrorIs(t, receive(t, first).err, context.Canceled)

		calls.waitStarted(t)
		close(calls.release)

		assert.Equal(t, result{n: 2}, receive(t, second), "the second caller ran the call under its own context")
		assert.Equal(t, int32(2), calls.calls.Load())
	})
}

func TestAFailureIsSharedWithEveryCaller(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var g flight.Group[int]

		started := make(chan struct{})
		release := make(chan struct{})
		fail := func(context.Context) (int, error) {
			close(started)
			<-release

			return 0, errFake
		}

		first := doAsync(t.Context(), &g, fail)

		<-started

		second := doAsync(t.Context(), &g, fail)

		synctest.Wait()
		close(release)

		require.ErrorIs(t, receive(t, first).err, errFake)
		require.ErrorIs(t, receive(t, second).err, errFake)
	})
}

func TestAPanicReachesEveryCaller(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var g flight.Group[int]

		started := make(chan struct{})
		release := make(chan struct{})
		boom := func(context.Context) (int, error) {
			close(started)
			<-release

			panic(errFake)
		}

		var wg sync.WaitGroup

		caught := make(chan any, 2)

		catch := func() {
			defer wg.Done()
			defer func() { caught <- recover() }()

			_, _ = g.Do(t.Context(), "key", boom)
		}

		wg.Add(1)

		go catch()

		<-started
		wg.Add(1)

		go catch()

		synctest.Wait()
		close(release)
		wg.Wait()
		close(caught)

		require.Len(t, caught, 2)

		for v := range caught {
			var p *flight.PanicError

			require.ErrorAs(t, v.(error), &p) //nolint:forcetypeassert // a panic from Do is always a *PanicError
			require.ErrorIs(t, p, errFake)
			assert.Contains(t, p.Error(), errFake.Error())
			assert.Contains(t, string(p.Stack), "flight_test")
		}
	})
}

func TestPanicErrorWithAValueThatIsNotAnError(t *testing.T) {
	t.Parallel()

	p := &flight.PanicError{Value: "broken", Stack: []byte("stack")}

	require.NoError(t, p.Unwrap())
	assert.Equal(t, "broken\n\nstack", p.Error())
}

func TestACallerWhoseContextEndedStartsNothing(t *testing.T) {
	t.Parallel()

	var g flight.Group[int]

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := g.Do(ctx, "key", func(context.Context) (int, error) {
		require.FailNow(t, "the call never starts")

		return 0, nil
	})
	require.ErrorIs(t, err, context.Canceled)
}
