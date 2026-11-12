//go:build unix

package release

import (
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoSmallFileReadWaitsOnANamedPipe(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, goModFile), 0o600))
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, gpgAgentConf), 0o600))
	require.NoError(t, syscall.Mkfifo(filepath.Join(dir, "signing.pub"), 0o600))

	type read struct {
		module, key string
		window      bool
	}

	done := make(chan read, 1)

	go func() {
		done <- read{
			module: modulePath(dir),
			key:    readPublicKey(filepath.Join(dir, "signing.pub")),
			window: windowPinentry(dir),
		}
	}()

	select {
	case r := <-done:
		assert.Equal(t, read{}, r, "a pipe is read as no file")
	case <-time.After(10 * time.Second):
		require.FailNow(t, "a read of go.mod or the signing files waited on a named pipe")
	}
}
