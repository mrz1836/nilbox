//go:build unix

package prompts

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// pipeWait bounds how long an import may take to refuse a named pipe before the test calls
// it hung: one that refuses the pipe returns at once.
const pipeWait = 5 * time.Second

func TestPreviewImportRefusesANamedPipe(t *testing.T) {
	t.Parallel()

	s, _, _ := keeping(t)

	dir := t.TempDir()

	pipe := filepath.Join(dir, "prompts.json")
	require.NoError(t, syscall.Mkfifo(pipe, 0o600))

	linked := filepath.Join(dir, "linked.json")
	require.NoError(t, os.Symlink(pipe, linked))

	for _, path := range []string{pipe, linked} {
		done := make(chan error, 1)

		go func() {
			_, err := s.PreviewImport(t.Context(), ImportSource{Kind: SourceFile, Path: path})
			done <- err
		}()

		select {
		case err := <-done:
			require.ErrorIs(t, err, ErrNotBundle, filepath.Base(path))
			require.ErrorIs(t, err, paths.ErrNotRegular, filepath.Base(path))
			assert.Equal(t, "The file isn't a "+brand.Name+" prompts file: read the file: not a regular file.",
				uierr.Message(err), "what the window shows")
		case <-time.After(pipeWait):
			require.FailNow(t, "an import waited on a named pipe", filepath.Base(path))
		}
	}
}
