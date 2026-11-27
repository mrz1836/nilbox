package inbox

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
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

// countingRefresher plans no tries: its failure is not one a later try may clear.
func (r *countingRefresher) retryAt() time.Time { return time.Time{} }

func (r *countingRefresher) onRetry(func()) {}

// plannedRefresher answers each refresh with the next of errs, nil once they run out,
// counting them as they start, and plans the tries of the failed ones as the Service
// does, on the time.Now of the test's synctest bubble. Each refresh takes took, none
// when zero.
type plannedRefresher struct {
	mu    sync.Mutex
	errs  []error
	took  time.Duration
	count atomic.Int32
	plan  retries
}

func (r *plannedRefresher) Refresh(context.Context) (Snapshot, error) {
	r.count.Add(1)

	time.Sleep(r.took)

	r.mu.Lock()

	var err error
	if len(r.errs) > 0 {
		err, r.errs = r.errs[0], r.errs[1:]
	}

	r.mu.Unlock()

	r.plan.ended(time.Now(), err)

	return Snapshot{}, err
}

func (r *plannedRefresher) retryAt() time.Time { return r.plan.next() }

func (r *plannedRefresher) onRetry(wake func()) { r.plan.onPlanned(wake) }

// errBadGateway is GitHub answering a refresh with a bad gateway, which a later try may
// well not meet.
var errBadGateway = &github.StatusError{StatusCode: http.StatusBadGateway}

// runScheduler runs s until the test's end, which waits for it to stop.
func runScheduler(t *testing.T, s *Scheduler) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		s.Run(ctx)
	}()

	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func TestSchedulerTriesAFailedRefreshAgainSoonerAndSoonerLess(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := &plannedRefresher{errs: []error{errBadGateway, errBadGateway, errBadGateway}}
		runScheduler(t, NewScheduler(r, nil, nil))

		synctest.Wait()
		assert.Equal(t, int32(1), r.count.Load(), "the first refresh fails")

		time.Sleep(retryFirst - time.Second)
		synctest.Wait()
		assert.Equal(t, int32(1), r.count.Load(), "not tried again before its try is due")

		time.Sleep(2 * time.Second) // 16 s in: the second try, at 15 s, fails too
		synctest.Wait()
		assert.Equal(t, int32(2), r.count.Load(), "tried again 15 seconds after it failed")

		time.Sleep(30 * time.Second) // 46 s in: the third, at 45 s, fails too
		synctest.Wait()
		assert.Equal(t, int32(3), r.count.Load(), "then 30 seconds after the second failed")

		time.Sleep(time.Minute) // 106 s in: the fourth, at 105 s, succeeds
		synctest.Wait()
		assert.Equal(t, int32(4), r.count.Load(), "then a minute after the third")

		time.Sleep(DefaultInterval - 2*time.Second)
		synctest.Wait()
		assert.Equal(t, int32(4), r.count.Load(), "once one succeeded, the interval again")

		time.Sleep(2 * time.Second)
		synctest.Wait()
		assert.Equal(t, int32(5), r.count.Load())
	})
}

// A refresh can take minutes before it fails, as one in flight while the Mac sleeps does
// until the connection it waited on is reset on waking. Its try is due retryFirst after
// it failed, and the scheduler starts it then, not retryFirst plus the refresh's length.
func TestSchedulerTriesASlowFailedRefreshAgainFromItsEnd(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		const took = 3 * time.Minute

		r := &plannedRefresher{errs: []error{errBadGateway}, took: took}
		runScheduler(t, NewScheduler(r, nil, nil))

		time.Sleep(took + time.Second)
		synctest.Wait()
		assert.Equal(t, int32(1), r.count.Load(), "the first refresh fails three minutes in")

		time.Sleep(retryFirst - 2*time.Second)
		synctest.Wait()
		assert.Equal(t, int32(1), r.count.Load(), "not tried again before its try is due")

		time.Sleep(2 * time.Second)
		synctest.Wait()
		assert.Equal(t, int32(2), r.count.Load(), "tried again 15 seconds after it failed, not after it started")
	})
}

func TestSchedulerTriesAgainARefreshSomeoneElseRanThatFailed(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := &plannedRefresher{errs: []error{nil, errBadGateway}}
		runScheduler(t, NewScheduler(r, nil, nil))

		synctest.Wait()
		assert.Equal(t, int32(1), r.count.Load(), "the scheduler's own refresh succeeds")

		time.Sleep(time.Minute)

		// The user clicks Refresh a minute later, and GitHub fails it.
		_, err := r.Refresh(t.Context())
		require.ErrorIs(t, err, errBadGateway)

		time.Sleep(retryFirst - time.Second)
		synctest.Wait()
		assert.Equal(t, int32(2), r.count.Load(), "nothing before its try is due")

		time.Sleep(2 * time.Second)
		synctest.Wait()
		assert.Equal(t, int32(3), r.count.Load(), "the scheduler, woken, tries it 15 seconds after the click, not at its interval")
	})
}

func TestSchedulerLeavesAFailureNoLaterTryClearsToItsInterval(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := &plannedRefresher{errs: []error{errFakeBroken}}
		runScheduler(t, NewScheduler(r, nil, nil))

		time.Sleep(DefaultInterval - time.Second)
		synctest.Wait()
		assert.Equal(t, int32(1), r.count.Load(), "a failure only the user can clear waits out the interval")

		time.Sleep(2 * time.Second)
		synctest.Wait()
		assert.Equal(t, int32(2), r.count.Load())
	})
}

func TestSchedulerDue(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	now := time.Now()
	r := &plannedRefresher{}
	s := NewScheduler(r, func(context.Context) time.Duration { return 2 * time.Minute }, nil)

	assert.Equal(t, 2*time.Minute, s.due(ctx), "the interval, with no try planned")

	r.plan.ended(now, errBadGateway)
	assert.Equal(t, 2*time.Minute, s.due(ctx), "before its first refresh, which starts at once anyway")

	s.ended.Store(now.UnixNano())
	assert.Equal(t, retryFirst, s.due(ctx), "from the end of its own last refresh to the try")

	for range 4 {
		r.plan.ended(now, errBadGateway)
	}

	assert.Equal(t, 2*time.Minute, s.due(ctx), "never past the user's own interval")

	s.ended.Store(now.Add(time.Hour).UnixNano())
	assert.Equal(t, time.Nanosecond, s.due(ctx), "a try already due waits no more")

	r.plan.ended(now, nil)
	assert.Equal(t, 2*time.Minute, s.due(ctx), "a refresh that succeeded ends the plan")

	off := NewScheduler(r, func(context.Context) time.Duration { return 0 }, nil)
	off.ended.Store(now.UnixNano())
	r.plan.ended(now, errBadGateway)
	assert.Zero(t, off.due(ctx), "a scheduler turned off runs nothing, a planned try included")
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

	assert.Equal(t, DefaultInterval, s.job.Interval(t.Context()))
	assert.Equal(t, 5*time.Minute, DefaultInterval)

	// periodic.RunEvery discards a nil logger's entries: the first refresh fails, and the
	// scheduler goes on with no logger to tell.
	synctest.Test(t, func(t *testing.T) {
		r := &countingRefresher{}
		s := NewScheduler(r, nil, nil)

		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})

		go func() {
			defer close(done)

			s.Run(ctx)
		}()

		time.Sleep(DefaultInterval + time.Second)
		synctest.Wait()
		cancel()
		<-done

		assert.Equal(t, int32(2), r.count.Load(), "a failure with no logger stops nothing")
	})
}

func TestAZeroWorkerIgnoresAWake(t *testing.T) {
	t.Parallel()

	assert.NotPanics(t, (&Scheduler{}).Wake, "a scheduler never made ignores a wake")
	assert.NotPanics(t, (&ChecksFollower{}).Wake, "so does a checks follower")
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
