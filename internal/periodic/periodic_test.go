package periodic_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/periodic"
)

// waitTimeout bounds every wait in these tests.
const waitTimeout = 5 * time.Second

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

// start runs fn every interval in the background; stop cancels it and waits for Run to
// return.
func start(t *testing.T, interval time.Duration, fn func(context.Context) error, log *slog.Logger) (stop func()) {
	t.Helper()

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		periodic.Run(ctx, interval, "fake job", fn, log)
	}()

	stop = func() {
		cancel()

		select {
		case <-done:
		case <-time.After(waitTimeout):
			t.Fatal("Run did not return once canceled")
		}
	}

	t.Cleanup(stop)

	return stop
}

func TestRunRunsAtOnceAndOnEveryTick(t *testing.T) {
	t.Parallel()

	var runs atomic.Int32

	stop := start(t, time.Millisecond, func(context.Context) error {
		runs.Add(1)

		return nil
	}, nil)

	require.Eventually(t, func() bool { return runs.Load() >= 3 }, waitTimeout, time.Millisecond)
	stop()

	after := runs.Load()

	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, after, runs.Load(), "nothing runs once Run has returned")
}

func TestRunStartsWithoutWaitingForATick(t *testing.T) {
	t.Parallel()

	ran := make(chan struct{}, 1)

	start(t, time.Hour, func(context.Context) error {
		ran <- struct{}{}

		return nil
	}, nil)

	select {
	case <-ran:
	case <-time.After(waitTimeout):
		t.Fatal("the first run did not start at once")
	}
}

func TestRunLogsFailuresAndPanicsAndKeepsGoing(t *testing.T) {
	t.Parallel()

	var runs atomic.Int32

	logs := &logBuffer{}
	stop := start(t, time.Millisecond, func(context.Context) error {
		switch runs.Add(1) {
		case 1:
			return errFakeJob
		case 2:
			panic("the job exploded")
		default:
			return nil
		}
	}, logger(logs))

	require.Eventually(t, func() bool { return runs.Load() >= 3 }, waitTimeout, time.Millisecond)
	stop()

	out := logs.String()
	assert.Contains(t, out, "fake job failed")
	assert.Contains(t, out, errFakeJob.Error())
	assert.Contains(t, out, "fake job panicked")
	assert.Contains(t, out, "the job exploded")
}

func TestRunWaitsForTheRunInFlightAndDoesNotLogItsCancellation(t *testing.T) {
	t.Parallel()

	started := make(chan struct{})
	sawCancel := make(chan struct{})

	logs := &logBuffer{}
	stop := start(t, time.Hour, func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		close(sawCancel)

		return ctx.Err()
	}, logger(logs))

	<-started
	stop()

	select {
	case <-sawCancel:
	default:
		t.Fatal("Run returned before the run in flight did")
	}

	assert.NotContains(t, logs.String(), "fake job failed", "a canceled run is not a failure")
}

func TestRunSkipsTicksThatComeWhileRunning(t *testing.T) {
	t.Parallel()

	var runs atomic.Int32

	logs := &logBuffer{}
	stop := start(t, time.Millisecond, func(context.Context) error {
		if runs.Add(1) == 1 {
			time.Sleep(20 * time.Millisecond)
		}

		return nil
	}, logger(logs))

	require.Eventually(t, func() bool { return runs.Load() >= 2 }, waitTimeout, time.Millisecond)
	stop()

	assert.Contains(t, logs.String(), "fake job skipped", "the ticks during the long first run were skipped")
}
