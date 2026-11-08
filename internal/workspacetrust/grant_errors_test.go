package workspacetrust_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/workspacetrust"
)

// untrusted is a config whose clone entry still needs the trust answer, so a grant has to
// take the lock and write.
func untrusted(clone string) string {
	return `{"projects": {"` + clone + `": {"hasTrustDialogAccepted": false}}}`
}

func TestGrantStopsWhileAnotherProcessHoldsTheLock(t *testing.T) {
	t.Parallel()

	home := testutil.RealTempDir(t)
	clone := makeDir(t, filepath.Join(home, "example-repo"))
	file := filepath.Join(home, ".claude.json")
	testutil.WriteFile(t, file, untrusted(clone))
	makeDir(t, file+".lock")

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	changed, err := workspacetrust.Grant(ctx, file, clone)
	require.ErrorIs(t, err, context.Canceled)
	assert.False(t, changed)
	assert.DirExists(t, file+".lock", "another process's lock is left alone")

	data, err := os.ReadFile(file) //nolint:gosec // a test file under a temp dir
	require.NoError(t, err)
	assert.Equal(t, untrusted(clone), string(data))
}

func TestGrantReportsAConfigItCannotRead(t *testing.T) {
	t.Parallel()
	testutil.SkipAsRoot(t)

	home := testutil.RealTempDir(t)
	clone := makeDir(t, filepath.Join(home, "example-repo"))
	file := filepath.Join(home, ".claude.json")
	testutil.WriteFile(t, file, untrusted(clone))
	require.NoError(t, os.Chmod(file, 0o000))
	t.Cleanup(func() { _ = os.Chmod(file, 0o600) })

	changed, err := workspacetrust.Grant(t.Context(), file, clone)
	require.ErrorIs(t, err, os.ErrPermission)
	require.ErrorContains(t, err, "read "+file)
	assert.False(t, changed)
	assert.ElementsMatch(t, []string{".claude.json", "example-repo"}, dirNames(t, home), "the lock is released")
}

func TestGrantReportsAConfigItCannotReplace(t *testing.T) {
	t.Parallel()
	testutil.SkipAsRoot(t)

	home := testutil.RealTempDir(t)
	clone := makeDir(t, filepath.Join(home, "example-repo"))
	dotfiles := makeDir(t, filepath.Join(home, "dotfiles"))
	target := filepath.Join(dotfiles, "claude.json")
	file := filepath.Join(home, ".claude.json")

	testutil.WriteFile(t, target, untrusted(clone))
	require.NoError(t, os.Symlink(target, file))
	require.NoError(t, os.Chmod(dotfiles, 0o500))       //nolint:gosec // read-only, so nothing can be created in it
	t.Cleanup(func() { _ = os.Chmod(dotfiles, 0o700) }) //nolint:gosec // restoring the test's own directory

	changed, err := workspacetrust.Grant(t.Context(), file, clone)
	require.ErrorIs(t, err, os.ErrPermission)
	require.ErrorContains(t, err, "write "+file)
	assert.False(t, changed)

	data, err := os.ReadFile(target) //nolint:gosec // a test file under a temp dir
	require.NoError(t, err)
	assert.Equal(t, untrusted(clone), string(data), "the file it points at is left as it was")
	assert.Equal(t, []string{"claude.json"}, dirNames(t, dotfiles))
	assert.ElementsMatch(t, []string{".claude.json", "dotfiles", "example-repo"}, dirNames(t, home),
		"the lock is released")
}

func TestGrantReportsAConfigSymlinkLoop(t *testing.T) {
	t.Parallel()

	home := testutil.RealTempDir(t)
	clone := makeDir(t, filepath.Join(home, "example-repo"))
	file := filepath.Join(home, ".claude.json")
	require.NoError(t, os.Symlink(file, file))

	changed, err := workspacetrust.Grant(t.Context(), file, clone)
	require.ErrorContains(t, err, "resolve "+file)
	assert.False(t, changed)
}
