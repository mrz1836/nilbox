package budget

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// errOffline is a read that never reached GitHub.
var errOffline = errors.New("offline")

// fakeReader reads every budget into a tracker, as the GitHub client's observer does, and
// counts its reads.
type fakeReader struct {
	tracker *Tracker
	reads   atomic.Int32
	err     error
}

func (r *fakeReader) ReadRates(context.Context) error {
	r.reads.Add(1)

	if r.err != nil {
		return r.err
	}

	r.tracker.ObserveRate(graphql(120, reset), 0)
	r.tracker.ObserveRate(github.RateReading{Resource: github.ResourceCore, Limit: 5000, Remaining: 4900, Used: 100, ResetAt: reset}, 0)
	r.tracker.ObserveRate(github.RateReading{
		Resource: github.ResourceSearch, Limit: 30, Remaining: 30, ResetAt: start.Add(time.Minute),
	}, 0)

	return nil
}

func newTestService() (*Service, *fakeReader, *testutil.Clock) {
	tr, clk, _ := newTestTracker()
	reader := &fakeReader{tracker: tr}

	every := func(context.Context) time.Duration { return 5 * time.Minute }

	return NewService(Config{Tracker: tr, Reader: reader, Clock: clk, RefreshEvery: every}), reader, clk
}

func TestCheckReadsEveryBudget(t *testing.T) {
	t.Parallel()

	svc, reader, _ := newTestService()

	got, err := svc.Check(t.Context())
	require.NoError(t, err)

	assert.Equal(t, int32(1), reader.reads.Load())
	assert.Equal(t, 120, got.GraphQL.Used)
	assert.Equal(t, 100, got.Core.Used)
	assert.Equal(t, 30, got.Search.Limit)
	assert.Equal(t, 12, got.RefreshesPerHour)
}

func TestCheckSkipsTheReadWhileEveryBudgetIsFresh(t *testing.T) {
	t.Parallel()

	svc, reader, clk := newTestService()

	_, err := svc.Check(t.Context())
	require.NoError(t, err)

	clk.Advance(FreshFor - time.Second)

	_, err = svc.Check(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int32(1), reader.reads.Load(), "read moments ago")

	clk.Advance(2 * time.Second)

	_, err = svc.Check(t.Context())
	require.NoError(t, err)
	assert.Equal(t, int32(2), reader.reads.Load())
}

func TestAFailedCheckReturnsTheGaugeAsItWas(t *testing.T) {
	t.Parallel()

	svc, reader, _ := newTestService()
	svc.tracker.Refreshed(5, 31)

	reader.err = errOffline

	got, err := svc.Check(t.Context())
	require.ErrorIs(t, err, errOffline)
	assert.Contains(t, err.Error(), "read the API budget")
	assert.Equal(t, 31, got.Refresh.Items)
}

func TestGaugeWithoutARefreshInterval(t *testing.T) {
	t.Parallel()

	tr, _, _ := newTestTracker()

	assert.Zero(t, NewService(Config{Tracker: tr}).Gauge(t.Context()).RefreshesPerHour)

	off := func(context.Context) time.Duration { return 0 }
	assert.Zero(t, NewService(Config{Tracker: tr, RefreshEvery: off}).Gauge(t.Context()).RefreshesPerHour)
}

func TestGaugeFollowsTheRefreshIntervalAsSet(t *testing.T) {
	t.Parallel()

	tr, _, _ := newTestTracker()
	every := 5 * time.Minute
	svc := NewService(Config{Tracker: tr, RefreshEvery: func(context.Context) time.Duration { return every }})

	assert.Equal(t, 12, svc.Gauge(t.Context()).RefreshesPerHour)

	every = 15 * time.Minute

	assert.Equal(t, 4, svc.Gauge(t.Context()).RefreshesPerHour, "a changed setting shows at once")
}
