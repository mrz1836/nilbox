package inbox

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
)

// countingRefresher counts its refreshes and fails the first one.
type countingRefresher struct {
	count atomic.Int32
}

func (r *countingRefresher) Refresh(context.Context) (Snapshot, error) {
	if r.count.Add(1) == 1 {
		return Snapshot{}, errFakeBroken
	}

	return Snapshot{}, nil
}

// syncBuffer is a log sink safe for concurrent writers.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

func TestSchedulerRefreshesAndLogsUntilCanceled(t *testing.T) {
	t.Parallel()

	// The bubble's clock moves only when every goroutine in it is blocked, so the real
	// interval takes no real time and the count of refreshes is exact.
	synctest.Test(t, func(t *testing.T) {
		r := &countingRefresher{}
		logs := &syncBuffer{}
		s := NewScheduler(r, slog.New(slog.NewTextHandler(logs, nil)))

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			s.Run(ctx)
		}()

		synctest.Wait()
		assert.Equal(t, int32(1), r.count.Load(), "the inbox is refreshed at once")

		time.Sleep(2*DefaultInterval + time.Second)
		synctest.Wait()
		assert.Equal(t, int32(3), r.count.Load(), "and again on every tick, after a failure too")

		cancel()
		<-done

		assert.Contains(t, logs.String(), "inbox refresh failed")
		assert.Contains(t, logs.String(), errFakeBroken.Error())
	})
}

func TestNewSchedulerDefaults(t *testing.T) {
	t.Parallel()

	s := NewScheduler(&countingRefresher{}, nil)

	assert.Equal(t, DefaultInterval, s.interval)
	assert.Equal(t, 5*time.Minute, DefaultInterval)
	assert.Nil(t, s.logger, "periodic.Run discards a nil logger's entries")
}
