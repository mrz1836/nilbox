package clock_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clock"
)

func TestSleepWaitsOrEndsWithItsContext(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		start := time.Now()

		require.NoError(t, clock.Sleep(t.Context(), time.Minute))
		assert.Equal(t, time.Minute, time.Since(start), "it waits the whole minute")

		ctx, cancel := context.WithCancel(t.Context())

		go func() {
			time.Sleep(10 * time.Second)
			cancel()
		}()

		start = time.Now()

		require.ErrorIs(t, clock.Sleep(ctx, time.Hour), context.Canceled)
		assert.Equal(t, 10*time.Second, time.Since(start), "it returns as its context ends")
	})
}
