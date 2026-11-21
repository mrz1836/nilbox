//go:build unix

package update_test

import (
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/update"
)

// pipeLog makes the helper's log of oldPID, in the rig's temporary folder, a named pipe
// that nothing writes to.
func pipeLog(t *testing.T, r *rig) {
	t.Helper()

	require.NoError(t, syscall.Mkfifo(filepath.Join(r.cfg.TempDir, "wails-update-"+strconv.Itoa(oldPID)+".log"), 0o600))
}

// settleWithin settles r's engine, failing the test if that waits on a named pipe, and
// returns how long it says to wait before settling again.
func settleWithin(t *testing.T, r *rig) time.Duration {
	t.Helper()

	done := make(chan time.Duration, 1)

	go func() { done <- r.engine.Settle(t.Context()) }()

	select {
	case wait := <-done:
		return wait
	case <-time.After(pipeWait):
		require.FailNow(t, "settling waited on a named pipe")

		return 0
	}
}

func TestAHelperLogThatIsANamedPipeIsNoLog(t *testing.T) {
	t.Parallel()

	r, logs := loggedRig(t)
	pending(t, r, next)
	pipeLog(t, r)

	require.Zero(t, settleWithin(t, r))

	failed := r.status(t).InstallFailed
	assert.Equal(t, next, failed.Version)
	assert.Equal(t, "The update didn't install, and left no log saying why.", failed.Reason)
	assert.True(t, failed.Retryable)
	assert.Contains(t, logs.String(), "could not read the update's log")
}

func TestARecentInstallWhoseHelperLogIsANamedPipeWaits(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	record(t, r, running, next, time.Minute)
	pipeLog(t, r)

	wait := settleWithin(t, r)
	assert.InDelta(t, float64(update.HelperWindow-time.Minute), float64(wait), float64(time.Second),
		"a log that can't be read hasn't ended")
	assert.NotEmpty(t, r.memory.get(update.KeyPendingInstall), "the record stays")
	assert.Empty(t, r.status(t).InstallFailed.Version, "nothing is told yet")
}
