//go:build unix

package store_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/store/migrate"
)

// pipeWait bounds how long a restore may take to refuse a named pipe before the test calls
// it hung: one that refuses the pipe returns at once.
const pipeWait = 5 * time.Second

func TestRestoreBackupRefusesANamedPipeInTheBackupsPlace(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "app.db")
	setPref(t, path, "current")

	pipe := path + migrate.BackupInfix + "20260102T030405Z-v1"
	require.NoError(t, syscall.Mkfifo(pipe, 0o600))

	linked := path + migrate.BackupInfix + "20260102T030406Z-v1"
	require.NoError(t, os.Symlink(pipe, linked))

	for _, backup := range []string{pipe, linked} {
		done := make(chan error, 1)

		go func() {
			_, err := store.RestoreBackup(t.Context(), path, backup)
			done <- err
		}()

		select {
		case err := <-done:
			require.ErrorIs(t, err, store.ErrNotABackup)
			assert.Equal(t, "not a backup of the database: "+filepath.Base(backup)+" is not a file", err.Error())
		case <-time.After(pipeWait):
			require.FailNow(t, "a restore waited on a named pipe", filepath.Base(backup))
		}
	}

	assert.Equal(t, "current", prefIn(t, path), "nothing moved")
	assert.Empty(t, setAsideFiles(t, path), "nothing was set aside")
	assert.Empty(t, restoreCopies(t, path), "nothing was copied")
}
