package notify

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shownBatches records each batch a gathering shows.
type shownBatches struct {
	mu      sync.Mutex
	batches [][]Notification
}

func (s *shownBatches) show(_ context.Context, rows []Notification) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.batches = append(s.batches, rows)
}

func (s *shownBatches) all() [][]Notification {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([][]Notification(nil), s.batches...)
}

// failedRow is the recorded notification of failed run id.
func failedRow(id int64) Notification {
	return Notification{ID: id, Kind: KindRunFailed, Title: "A script failed: lint", RunID: id}
}

func TestAGatheringShowsABurstAsOneBatch(t *testing.T) {
	t.Parallel()

	// The failures come a tenth of a second apart, all inside the window the first began.
	for _, n := range []int{1, 3, 4, 9} {
		synctest.Test(t, func(t *testing.T) {
			shown := &shownBatches{}
			g := newGathering(shown.show)

			for i := range n {
				g.add(failedRow(int64(i + 1)))
				time.Sleep(100 * time.Millisecond)
			}

			synctest.Wait()
			assert.Empty(t, shown.all(), "nothing shows before the window ends")

			time.Sleep(GatherWindow)
			synctest.Wait()

			batches := shown.all()
			require.Len(t, batches, 1, "%d failures show together", n)
			require.Len(t, batches[0], n)

			banners := Banners(batches[0], nil)

			if n > MaxBanners {
				require.Len(t, banners, 1, "%d failures sum up in one banner", n)
				assert.Equal(t, "summary", banners[0].Thread)
			} else {
				assert.Len(t, banners, n, "one banner each")
			}
		})
	}
}

func TestAGatheringStartsAnotherBatchOnceItShowed(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		shown := &shownBatches{}
		g := newGathering(shown.show)

		g.add(failedRow(1))
		time.Sleep(GatherWindow + time.Second)
		g.add(failedRow(2))
		g.add(failedRow(3))
		time.Sleep(GatherWindow + time.Second)
		synctest.Wait()

		assert.Equal(t, [][]Notification{{failedRow(1)}, {failedRow(2), failedRow(3)}}, shown.all())
	})
}

func TestAStoppedGatheringShowsNothingMore(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		shown := &shownBatches{}
		g := newGathering(shown.show)

		g.add(failedRow(1))
		g.add(failedRow(2))
		g.stop()
		g.add(failedRow(3))

		time.Sleep(2 * GatherWindow)
		synctest.Wait()
		g.stop()

		assert.Empty(t, shown.all(), "what it held is dropped, and nothing more is held")
	})
}

func TestStoppingAGatheringWaitsForABatchShowing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		release := make(chan struct{})
		showing := make(chan struct{})
		shown := &shownBatches{}

		g := newGathering(func(ctx context.Context, rows []Notification) {
			close(showing)
			<-release
			shown.show(ctx, rows)
		})

		g.add(failedRow(1))
		time.Sleep(GatherWindow)
		<-showing

		stopped := make(chan struct{})

		go func() {
			g.stop()
			close(stopped)
		}()

		synctest.Wait()

		select {
		case <-stopped:
			require.Fail(t, "stop returned while a batch was still showing")
		default:
		}

		close(release)
		<-stopped

		assert.Equal(t, [][]Notification{{failedRow(1)}}, shown.all())
	})
}
