package inbox

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/testutil"
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

func TestSchedulerRefreshesAndLogsUntilCanceled(t *testing.T) {
	t.Parallel()

	// The bubble's clock moves only when every goroutine in it is blocked, so the real
	// interval takes no real time and the count of refreshes is exact.
	synctest.Test(t, func(t *testing.T) {
		r := &countingRefresher{}
		logs := &testutil.Logs{}
		s := NewScheduler(r, nil, logs.Logger())

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

	s := NewScheduler(&countingRefresher{}, nil, nil)

	assert.Equal(t, DefaultInterval, s.every(t.Context()))
	assert.Equal(t, 5*time.Minute, DefaultInterval)
	assert.Nil(t, s.logger, "periodic.RunEvery discards a nil logger's entries")
}

func TestSchedulerFollowsTheSettingAndWakesToAChange(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var every atomic.Int64

		every.Store(int64(30 * time.Minute))

		r := &countingRefresher{}
		s := NewScheduler(r, func(context.Context) time.Duration { return time.Duration(every.Load()) }, nil)

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			s.Run(ctx)
		}()

		synctest.Wait()
		assert.Equal(t, int32(1), r.count.Load())

		time.Sleep(5 * time.Minute)
		synctest.Wait()
		assert.Equal(t, int32(1), r.count.Load(), "not yet: it is set to half an hour")

		every.Store(int64(2 * time.Minute))
		s.Wake()
		synctest.Wait()
		assert.Equal(t, int32(2), r.count.Load(), "set shorter, a refresh already due runs at once")

		s.Wake()
		s.Wake() // a second wake while one waits is dropped, never blocks
		synctest.Wait()
		assert.Equal(t, int32(2), r.count.Load(), "a wake with nothing due runs nothing")

		cancel()
		<-done
	})
}
