//go:build darwin

package desktop_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"

	"github.com/mrz1836/nilbox/internal/desktop"
)

// macOS's quarantine mark, as a browser sets it on a download.
const (
	quarantineName = "com.apple.quarantine"
	quarantineMark = "0081;6560a000;Safari;"
)

func TestUnquarantineTakesTheMarkOffEveryFileOfTheCopyAlone(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	bundle := filepath.Join(root, "Example.app")
	program := filepath.Join(bundle, "Contents", "MacOS", "example")
	plain := filepath.Join(bundle, "Contents", "Resources", "plain.txt")
	link := filepath.Join(bundle, "Contents", "Frameworks", "Current")
	outside := filepath.Join(root, "outside.txt")

	require.NoError(t, os.MkdirAll(filepath.Dir(program), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Dir(plain), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Dir(link), 0o700))
	require.NoError(t, os.WriteFile(program, []byte("a program"), 0o600))
	require.NoError(t, os.WriteFile(plain, []byte("no mark"), 0o600))
	require.NoError(t, os.WriteFile(outside, []byte("outside the copy"), 0o600))
	require.NoError(t, os.Symlink(outside, link))

	for _, path := range []string{bundle, program, outside} {
		require.NoError(t, unix.Setxattr(path, quarantineName, []byte(quarantineMark), 0))
	}

	require.NoError(t, desktop.Unquarantine(bundle))

	for _, path := range []string{bundle, program, plain} {
		_, err := unix.Getxattr(path, quarantineName, nil)
		require.ErrorIs(t, err, unix.ENOATTR, path)
	}

	_, err := unix.Getxattr(outside, quarantineName, nil)
	require.NoError(t, err, "a link in the copy is never followed out of it")
}

func TestUnquarantineOfNoCopyFails(t *testing.T) {
	t.Parallel()

	assert.Error(t, desktop.Unquarantine(filepath.Join(t.TempDir(), "missing.app")))
}
