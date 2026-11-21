package periodic_test

import (
	"context"
	"log/slog"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/periodic"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// startJob runs j in the background, inside the caller's synctest bubble, as start runs a
// loop: it waits until the first run, if any, has settled, and stop cancels it and waits
// for Run to return.
func startJob(t *testing.T, j *periodic.Job) (stop func()) {
	t.Helper()

	return start(t, func(ctx context.Context, _ func(context.Context) error, _ *slog.Logger) {
		j.Run(ctx)
	}, nil, nil)
}

// counting returns a job's run that counts itself in runs.
func counting(runs *atomic.Int32) func(context.Context) error {
	return func(context.Context) error {
		runs.Add(1)

		return nil
	}
}

// fixed is an Every that always says interval.
func fixed(context.Context) time.Duration { return interval }

func TestAJobRunsAtOnceThenEveryIntervalUntilCanceled(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int32

		stop := startJob(t, periodic.NewJob(fixed, "fake job", counting(&runs), nil))
		assert.Equal(t, int32(1), runs.Load(), "the first run starts at once")

		after(2*interval + time.Second)
		assert.Equal(t, int32(3), runs.Load(), "then one in each interval")

		stop()
		after(10 * interval)
		assert.Equal(t, int32(3), runs.Load(), "nothing runs once Run has returned")
	})
}

func TestAJobWakesToANewInterval(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int32

		every := &setting{}
		every.set(30 * time.Minute)

		job := periodic.NewJob(every.get, "fake job", counting(&runs), nil)
		stop := startJob(t, job)

		after(5 * time.Minute)
		assert.Equal(t, int32(1), runs.Load(), "not yet: it is set to half an hour")

		every.set(2 * time.Minute)
		job.Wake()
		synctest.Wait()
		assert.Equal(t, int32(2), runs.Load(), "set shorter, a run already due starts at once")

		job.Wake()
		synctest.Wait()
		assert.Equal(t, int32(2), runs.Load(), "a wake with nothing due runs nothing")

		after(2*time.Minute + time.Second)
		assert.Equal(t, int32(3), runs.Load(), "then every two minutes")

		stop()
	})
}

func TestAJobOffRunsNothingUntilWokenOn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int32

		every := &setting{} // zero: off
		job := periodic.NewJob(every.get, "fake job", counting(&runs), nil)
		stop := startJob(t, job)

		after(time.Hour)
		assert.Zero(t, runs.Load(), "off runs nothing, not even at once")

		every.set(interval)
		job.Wake()
		synctest.Wait()
		assert.Equal(t, int32(1), runs.Load(), "on again, a run long due starts at once")

		stop()
	})
}

func TestAJobsWakeNeverBlocksAndCoalesces(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var asked atomic.Int32

		job := periodic.NewJob(func(context.Context) time.Duration {
			asked.Add(1)

			return 0 // off, so only a wake has it ask again
		}, "fake job", func(context.Context) error { return nil }, nil)

		// Nothing reads the wakes yet: none of them blocks, and they wait as one.
		job.Wake()
		job.Wake()
		job.Wake()

		stop := startJob(t, job)
		assert.Equal(t, int32(2), asked.Load(), "it asks as it starts, then once for the three wakes")

		stop()

		job.Wake()
		job.Wake()
		assert.Equal(t, int32(2), asked.Load(), "once Run has returned, a wake still never blocks")
	})
}

func TestAJobStopsWithItsContextOnceTheRunInFlightReturns(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		sawCancel := make(chan struct{})

		logs := &testutil.Logs{}
		stop := startJob(t, periodic.NewJob(fixed, "fake job", func(ctx context.Context) error {
			<-ctx.Done()
			close(sawCancel)

			return ctx.Err()
		}, logs.Logger()))

		stop()

		select {
		case <-sawCancel:
		default:
			require.FailNow(t, "Run returned before the run in flight did")
		}

		assert.NotContains(t, logs.String(), "fake job failed", "a canceled run is not a failure")
	})
}

func TestAJobLogsItsFailuresUnderItsName(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int32

		logs := &testutil.Logs{}
		stop := startJob(t, periodic.NewJob(fixed, "fake job", func(context.Context) error {
			if runs.Add(1) == 1 {
				return errFakeJob
			}

			panic("the job exploded")
		}, logs.Logger()))

		after(interval + time.Second)
		stop()

		assert.Equal(t, int32(2), runs.Load())

		out := logs.String()
		assert.Contains(t, out, "fake job failed")
		assert.Contains(t, out, errFakeJob.Error())
		assert.Contains(t, out, "fake job panicked")
		assert.Contains(t, out, "the job exploded")
	})
}

func TestAJobWithoutALoggerDiscardsItsFailures(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		var runs atomic.Int32

		stop := startJob(t, periodic.NewJob(fixed, "fake job", func(context.Context) error {
			if runs.Add(1) == 1 {
				return errFakeJob
			}

			panic("the job exploded")
		}, nil))

		after(2*interval + time.Second)
		stop()

		assert.Equal(t, int32(3), runs.Load(), "a failure and a panic logged nowhere do not stop the runs")
	})
}

func TestAJobsIntervalIsWhatItsEverySaysNow(t *testing.T) {
	t.Parallel()

	every := &setting{}
	every.set(interval)

	job := periodic.NewJob(every.get, "fake job", nil, nil)
	assert.Equal(t, interval, job.Interval(t.Context()))

	every.set(0)
	assert.Zero(t, job.Interval(t.Context()), "asked again each time")
}
