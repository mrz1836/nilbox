package periodic_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/periodic"
)

// interval is how often the job runs in these tests. They run in a synctest bubble,
// whose clock moves only when every goroutine in it is blocked, so a realistic interval
// takes no real time and every count is exact.
const interval = time.Minute

var errFakeJob = errors.New("fake: the job broke")

// logBuffer is a log sink safe for concurrent writers.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()

	return b.buf.String()
}

// logger writes every level to logs.
func logger(logs *logBuffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// start runs fn every interval in the background, inside the caller's synctest bubble,
// and waits until the first run has settled; stop cancels Run and waits for it to return.
func start(t *testing.T, fn func(context.Context) error, log *slog.Logger) (stop func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		periodic.Run(ctx, interval, "fake job", fn, log)
	}()

	synctest.Wait()

	return func() {
		cancel()
		<-done
	}
}

// after moves the bubble's clock on by d and waits until every run that started has
// settled.
func after(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

func TestRunRunsAtOnceAndOnEveryTick(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int32

		stop := start(t, func(context.Context) error {
			runs.Add(1)

			return nil
		}, nil)

		assert.Equal(t, int32(1), runs.Load(), "the first run starts at once")

		after(3*interval + time.Second)
		assert.Equal(t, int32(4), runs.Load(), "one run on each of the three ticks")

		stop()
		after(10 * interval)
		assert.Equal(t, int32(4), runs.Load(), "nothing runs once Run has returned")
	})
}

func TestRunStartsWithoutWaitingForATick(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		began := time.Now()
		ran := make(chan time.Time, 1)

		stop := start(t, func(context.Context) error {
			ran <- time.Now()

			return nil
		}, nil)
		defer stop()

		select {
		case at := <-ran:
			assert.Equal(t, began, at, "the first run starts at once, before any tick")
		default:
			t.Fatal("the first run did not start at once")
		}
	})
}

func TestRunLogsFailuresAndPanicsAndKeepsGoing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int32

		logs := &logBuffer{}
		stop := start(t, func(context.Context) error {
			switch runs.Add(1) {
			case 1:
				return errFakeJob
			case 2:
				panic("the job exploded")
			default:
				return nil
			}
		}, logger(logs))

		after(2*interval + time.Second)
		stop()

		assert.Equal(t, int32(3), runs.Load(), "a failure and a panic do not stop the runs")

		out := logs.String()
		assert.Contains(t, out, "fake job failed")
		assert.Contains(t, out, errFakeJob.Error())
		assert.Contains(t, out, "fake job panicked")
		assert.Contains(t, out, "the job exploded")
	})
}

func TestRunWaitsForTheRunInFlightAndDoesNotLogItsCancellation(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		sawCancel := make(chan struct{})

		logs := &logBuffer{}
		stop := start(t, func(ctx context.Context) error {
			<-ctx.Done()
			close(sawCancel)

			return ctx.Err()
		}, logger(logs))

		stop()

		select {
		case <-sawCancel:
		default:
			t.Fatal("Run returned before the run in flight did")
		}

		assert.NotContains(t, logs.String(), "fake job failed", "a canceled run is not a failure")
	})
}

func TestRunSkipsTicksThatComeWhileRunning(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int32

		// The first run outlasts two ticks.
		const long = 2*interval + interval/2

		logs := &logBuffer{}
		stop := start(t, func(context.Context) error {
			if runs.Add(1) == 1 {
				time.Sleep(long)
			}

			return nil
		}, logger(logs))

		after(long + time.Second)
		assert.Equal(t, int32(1), runs.Load(), "a run that outlasts the interval is not followed by one straight away")
		assert.Contains(t, logs.String(), "fake job skipped", "the ticks during the long first run were skipped")

		after(interval / 2)
		assert.Equal(t, int32(2), runs.Load(), "the next tick runs the job again")

		stop()
	})
}
