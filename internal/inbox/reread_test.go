package inbox

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
)

var errGitHubDown = errors.New("GitHub is down")

// recordingReads records the items Rereads read. A read of an item in fails fails with its
// error, one in panics panics, and, while held is set, each read waits for it to close.
type recordingReads struct {
	mu     sync.Mutex
	ids    []int64
	fails  map[int64]error
	panics map[int64]bool
	held   chan struct{}
}

func (r *recordingReads) read(ctx context.Context, id int64) error {
	r.mu.Lock()
	r.ids = append(r.ids, id)
	held, err, panics := r.held, r.fails[id], r.panics[id]
	r.mu.Unlock()

	if held != nil {
		select {
		case <-held:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if panics {
		panic(fmt.Sprintf("the read of %d broke", id))
	}

	return err
}

func (r *recordingReads) seen() []int64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	return slices.Clone(r.ids)
}

// running runs rr in the test's synctest bubble until the test ends, and returns what
// cancels it and where it says it returned.
func running(t *testing.T, rr *Rereads) (context.CancelFunc, <-chan struct{}) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		rr.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})

	return cancel, done
}

func TestRereadsReadWhatWasAskedBeforeRunOnceEach(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		reads := &recordingReads{}
		rr := newRereads(reads.read, nil)

		rr.Reread(3)
		rr.Reread(1)
		rr.Reread(3)
		synctest.Wait()
		assert.Empty(t, reads.seen(), "nothing is read before Run")

		running(t, rr)
		synctest.Wait()
		assert.Equal(t, []int64{1, 3}, reads.seen(), "in the order of their IDs, each once")
	})
}

func TestRereadsCoalesceWhatWaitsForTheReadInFlight(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		reads := &recordingReads{held: make(chan struct{})}
		rr := newRereads(reads.read, nil)
		running(t, rr)

		rr.Reread(1)
		synctest.Wait()
		require.Equal(t, []int64{1}, reads.seen(), "the read of 1 waits")

		rr.Reread(2)
		rr.Reread(2)
		rr.Reread(1)
		rr.Reread(2)
		synctest.Wait()
		assert.Equal(t, []int64{1}, reads.seen(), "one read at a time")

		close(reads.held)
		synctest.Wait()
		assert.Equal(t, []int64{1, 1, 2}, reads.seen(),
			"1 again, since it was asked for once its read had started, and 2 once")
	})
}

func TestRereadsKeepGoingPastAFailure(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		reads := &recordingReads{
			fails: map[int64]error{
				1: errGitHubDown,
				3: fmt.Errorf("refresh the item: %w (id 3)", item.ErrNotFound),
			},
			panics: map[int64]bool{2: true},
		}
		logs := &testutil.Logs{}
		rr := newRereads(reads.read, logs.Logger())

		for _, id := range []int64{4, 3, 2, 1} {
			rr.Reread(id)
		}

		running(t, rr)
		synctest.Wait()

		assert.Equal(t, []int64{1, 2, 3, 4}, reads.seen(), "every read runs")

		got := logs.String()
		assert.Contains(t, got, `level=WARN msg="read an item a session worked on again failed" item=1 error="GitHub is down"`)
		assert.Contains(t, got, `level=ERROR msg="read an item a session worked on again panicked" item=2`)
		assert.Contains(t, got, `level=DEBUG msg="read an item a session worked on again: it is no longer stored" item=3`)
		assert.NotContains(t, got, "item=4")
	})
}

func TestRereadsReturnOnCancel(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		reads := &recordingReads{held: make(chan struct{})}
		rr := newRereads(reads.read, nil)
		cancel, done := running(t, rr)

		rr.Reread(1)
		rr.Reread(2)
		synctest.Wait()
		rr.Reread(3)

		cancel()
		<-done
		assert.Equal(t, []int64{1}, reads.seen(), "the read in flight ends with Run, and nothing after it starts")
	})
}

func TestNewRereadsReadsTheItemAgain(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	h.client.items = map[string]*github.ItemUpdate{nodeID(1): open(h, 1, asking([]string{}, []string{}))}

	require.NoError(t, NewRereads(h.svc, nil).read(t.Context(), id))

	assert.Equal(t, []string{nodeID(1)}, h.client.itemIDs, "one read of that item alone")
	assert.Equal(t, [3]bool{false, false, true}, h.reasons(t, 1), "a review GitHub no longer asks leaves Review requested")
	require.ErrorIs(t, NewRereads(h.svc, nil).read(t.Context(), 9999), item.ErrNotFound)
}
