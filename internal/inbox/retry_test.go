package inbox

import (
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
)

func TestRetryWait(t *testing.T) {
	t.Parallel()

	tests := []struct {
		failures int
		after    time.Duration
		want     time.Duration
	}{
		{1, 0, 15 * time.Second},
		{2, 0, 30 * time.Second},
		{3, 0, time.Minute},
		{4, 0, 2 * time.Minute},
		{5, 0, 4 * time.Minute},
		{6, 0, 5 * time.Minute},
		{100, 0, 5 * time.Minute},
		{0, 0, 15 * time.Second},
		{1, 90 * time.Second, 90 * time.Second},
		{6, 10 * time.Minute, 10 * time.Minute},
		{3, 10 * time.Second, time.Minute},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, retryWait(tt.failures, tt.after), "failures %d, after %s", tt.failures, tt.after)
	}
}

func TestRefreshPlansATryOfAFailureALaterTryMayClear(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	var wakes atomic.Int32

	h.svc.onRetry(func() { wakes.Add(1) })

	h.client.inboxErr = &github.StatusError{StatusCode: http.StatusInternalServerError}

	snap, err := h.svc.Refresh(t.Context())
	require.Error(t, err)
	assert.Equal(t, RefreshRetry{At: "2026-01-10T12:00:15Z", Failures: 1}, snap.Retry, "tried again in 15 seconds")
	assert.NotEmpty(t, snap.LastError, "the error is kept, for the window to show beside the try")
	assert.Equal(t, int32(1), wakes.Load(), "the scheduler is woken to the try")
	assert.Equal(t, h.clock.Now().Add(retryFirst), h.svc.retryAt())

	h.clock.Advance(retryFirst)

	snap, err = h.svc.Refresh(t.Context())
	require.Error(t, err)
	assert.Equal(t, RefreshRetry{At: "2026-01-10T12:00:45Z", Failures: 2}, snap.Retry, "then in 30")
	assert.Equal(t, int32(2), wakes.Load())

	again, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Equal(t, snap.Retry, again.Retry, "every snapshot read says it")

	h.client.inboxErr = nil
	h.client.inbox = fetched(nil, nil, nil)

	snap, err = h.svc.Refresh(t.Context())
	require.NoError(t, err)
	assert.Equal(t, RefreshRetry{}, snap.Retry, "a refresh that succeeds ends the plan")
	assert.Empty(t, snap.LastError)
	assert.True(t, h.svc.retryAt().IsZero())

	h.client.inboxErr = errFakeBroken

	snap, err = h.svc.Refresh(t.Context())
	require.Error(t, err)
	assert.Equal(t, RefreshRetry{}, snap.Retry, "a failure no later try clears plans nothing")
	assert.NotEmpty(t, snap.LastError, "and the window shows it")
	assert.Equal(t, int32(2), wakes.Load(), "nor wakes the scheduler")
}

func TestRefreshHonorsTheWaitGitHubAskedFor(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inboxErr = &github.StatusError{StatusCode: http.StatusTooManyRequests, RetryAfter: 2 * time.Minute}

	snap, err := h.svc.Refresh(t.Context())
	require.Error(t, err)
	assert.Equal(t, RefreshRetry{At: "2026-01-10T12:02:00Z", Failures: 1}, snap.Retry)
}
