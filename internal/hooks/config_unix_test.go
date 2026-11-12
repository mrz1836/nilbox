//go:build unix

package hooks

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/paths"
)

// fifo makes a named pipe at path, which nothing ever writes to: opening it to read the
// usual way waits for a writer forever.
func fifo(t *testing.T, path string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, syscall.Mkfifo(path, 0o600))
}

// inTime runs fn, failing the test when it doesn't return within runWait.
func inTime(t *testing.T, what string, fn func()) {
	t.Helper()

	done := make(chan struct{})

	go func() {
		defer close(done)

		fn()
	}()

	waitFor(t, done, what+" hung on a named pipe")
}

func TestNoReadOfAHookFileWaitsOnAPipe(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "pipe.env")
	fifo(t, path)

	inTime(t, "opening a pipe", func() {
		_, err := paths.OpenRegular(path)
		assert.ErrorIs(t, err, paths.ErrNotRegular)
	})

	inTime(t, "reading a hook that is a pipe", func() {
		h := readHook(path)
		assert.True(t, h.present)
		assert.ErrorIs(t, h.err, paths.ErrNotRegular)
	})

	inTime(t, "hashing a dirty file that is a pipe", func() {
		assert.Equal(t, "special", fileSum(filepath.Dir(path), filepath.Base(path), true))
	})
}

func TestReadConfigSkipsAPipeAndALinkToOne(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	pipe := filepath.Join(t.TempDir(), "elsewhere.env")
	fifo(t, pipe)

	writeFile(t, dir, ".github/env/00-core.env", coreEnv, 0o600)
	writeFile(t, dir, ".github/env/elsewhere/10-real.env", "GO_PRE_COMMIT_ENABLE_LINT=false\n", 0o600)
	fifo(t, filepath.Join(dir, ".github", "env", "20-pipe.env"))
	require.NoError(t, os.Symlink(pipe, filepath.Join(dir, ".github", "env", "30-linked-pipe.env")))
	require.NoError(t, os.Symlink(filepath.Join(dir, ".github", "env", "elsewhere", "10-real.env"),
		filepath.Join(dir, ".github", "env", "40-linked.env")))

	var cfg repoConfig

	inTime(t, "reading the config", func() { cfg = readConfig(dir) })

	require.True(t, cfg.present)
	assert.Equal(t, "1.14.0", cfg.ciVersion())
	assert.False(t, cfg.checkOn(CheckLint), "a link to a regular file is still followed")
	assert.Equal(t, []string{
		filepath.Join(dir, ".github", "env", "00-core.env"), filepath.Join(dir, ".github", "env", "40-linked.env"),
	}, modularFiles(filepath.Join(dir, ".github", "env")), "only regular files, or links to them, count")

	// A folder holding nothing but pipes is no config.
	only := t.TempDir()
	fifo(t, filepath.Join(only, ".github", "env", "00-core.env"))
	require.NoError(t, os.Symlink(pipe, filepath.Join(only, ".github", "env", "10-linked.env")))

	inTime(t, "reading a config of pipes", func() { cfg = readConfig(only) })
	assert.False(t, cfg.present)
}

func TestTheScanCompletesWithAPipeInTheConfig(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	dir := r.clone("api", cloneOpts{config: coreEnv})
	pipe := filepath.Join(t.TempDir(), "local.env")
	fifo(t, pipe)
	require.NoError(t, os.Symlink(pipe, filepath.Join(dir, ".github", "env", "99-local.env")))

	var (
		p   Page
		err error
	)

	// Well within ScanTimeout, which would otherwise end a scan stuck on the pipe.
	inTime(t, "the scan", func() { p, err = r.svc.Repos(t.Context(), false) })

	require.NoError(t, err)
	assert.Equal(t, StateMissing, row(t, p, dir).State)
	assert.Equal(t, GroupAttention, row(t, p, dir).Group)
}
