package testutil

import (
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestAwaitFile(t *testing.T) {
	t.Parallel()

	t.Run("returns once the file appears", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "proceed")

		synctest.Test(t, func(t *testing.T) {
			go func() {
				time.Sleep(time.Second)
				assert.NoError(t, os.WriteFile(path, nil, 0o600))
			}()

			start := time.Now()

			assert.True(t, awaitFile(path, time.Minute))
			assert.GreaterOrEqual(t, time.Since(start), time.Second)
			assert.LessOrEqual(t, time.Since(start), time.Second+awaitPoll, "within one look of it appearing")
		})
	})

	t.Run("gives up at its limit", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "never")

		synctest.Test(t, func(t *testing.T) {
			start := time.Now()

			assert.False(t, awaitFile(path, time.Minute))
			assert.Equal(t, time.Minute, time.Since(start))
		})
	})

	t.Run("without a file, waits all of its limit", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			start := time.Now()

			assert.False(t, awaitFile("", time.Minute))
			assert.Equal(t, time.Minute, time.Since(start))
		})
	})
}
