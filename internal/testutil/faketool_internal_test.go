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

			awaitFile(path, time.Minute)
			assert.GreaterOrEqual(t, time.Since(start), time.Second)
			assert.LessOrEqual(t, time.Since(start), time.Second+awaitPoll, "within one look of it appearing")
		})
	})

	t.Run("gives up at its limit", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "never")

		synctest.Test(t, func(t *testing.T) {
			start := time.Now()

			awaitFile(path, time.Minute)
			assert.Equal(t, time.Minute, time.Since(start))
		})
	})

	t.Run("without a file, waits all of its limit", func(t *testing.T) {
		t.Parallel()

		synctest.Test(t, func(t *testing.T) {
			start := time.Now()

			awaitFile("", time.Minute)
			assert.Equal(t, time.Minute, time.Since(start))
		})
	})
}

func TestAFakeRunWaitsForItsFileEvenWithoutASleep(t *testing.T) {
	t.Parallel()

	assert.Equal(t, waitForLimit, FakeRun{WaitFor: "proceed"}.waitLimit(), "no Sleep: the default bound")
	assert.Equal(t, 5*time.Second, FakeRun{WaitFor: "proceed", Sleep: 5 * time.Second}.waitLimit(), "Sleep bounds it")
	assert.Equal(t, 5*time.Second, FakeRun{Sleep: 5 * time.Second}.waitLimit(), "no file: the Sleep")
	assert.Zero(t, FakeRun{}.waitLimit(), "neither: it exits at once")
}
