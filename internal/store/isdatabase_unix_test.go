//go:build unix

package store_test

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/store"
)

func TestIsDatabaseRefusesANamedPipe(t *testing.T) {
	t.Parallel()

	pipe := filepath.Join(t.TempDir(), "app.db")
	require.NoError(t, syscall.Mkfifo(pipe, 0o600))

	done := make(chan error, 1)

	go func() {
		_, err := store.IsDatabase(pipe)
		done <- err
	}()

	select {
	case err := <-done:
		require.ErrorIs(t, err, paths.ErrNotRegular)
	case <-time.After(pipeWait):
		require.FailNow(t, "looking for the header waited on a named pipe")
	}
}
