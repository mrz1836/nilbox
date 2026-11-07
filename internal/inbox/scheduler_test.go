package inbox

import (
	"bytes"
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// waitTimeout bounds every wait in these tests.
const waitTimeout = 5 * time.Second

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

	r := &countingRefresher{}
	logs := &syncBuffer{}
	s := NewScheduler(r, slog.New(slog.NewTextHandler(logs, nil)))
	s.interval = time.Millisecond

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})

	go func() {
		defer close(done)

		s.Run(ctx)
	}()

	require.Eventually(t, func() bool { return r.count.Load() >= 3 }, waitTimeout, time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(waitTimeout):
		t.Fatal("the scheduler did not stop")
	}

	assert.Contains(t, logs.String(), "inbox refresh failed")
	assert.Contains(t, logs.String(), errFakeBroken.Error())
}

func TestNewSchedulerDefaults(t *testing.T) {
	t.Parallel()

	s := NewScheduler(&countingRefresher{}, nil)

	assert.Equal(t, DefaultInterval, s.interval)
	assert.Equal(t, 5*time.Minute, DefaultInterval)
	assert.Nil(t, s.logger, "periodic.Run discards a nil logger's entries")
}
