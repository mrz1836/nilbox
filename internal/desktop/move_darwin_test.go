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

// macOS opens a marked app from a hidden copy unless the user moved it with Finder, which
// adds a flag to the mark; translocates reads the bundle's own mark alone, and changes
// nothing.
func TestTranslocatesReadsTheMarkOfTheBundlesOwnFolder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		mark string // "" for none
		want bool
	}{
		{name: "a download not moved yet", mark: quarantineMark, want: true},
		{name: "a download from a sandboxed browser", mark: "0083;6560a000;Safari;", want: true},
		{name: "moved with Finder", mark: "0181;6560a000;Safari;"},
		{name: "moved and opened", mark: "01c1;6560a000;Safari;"},
		{name: "moved, opened, and more", mark: "03c1;6560a000;Safari;"},
		{name: "no mark"},
		{name: "a mark macOS can't read", mark: "not a mark"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			bundle := filepath.Join(t.TempDir(), "Example.app")
			program := filepath.Join(bundle, "Contents", "MacOS", "example")
			require.NoError(t, os.MkdirAll(filepath.Dir(program), 0o700))
			require.NoError(t, os.WriteFile(program, []byte("a program"), 0o600))
			require.NoError(t, unix.Setxattr(program, quarantineName, []byte(quarantineMark), 0), "inside, it counts for nothing")

			if tt.mark != "" {
				require.NoError(t, unix.Setxattr(bundle, quarantineName, []byte(tt.mark), 0))
			}

			got, err := desktop.Translocates(bundle)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			if tt.mark != "" {
				mark := make([]byte, 64)
				n, err := unix.Getxattr(bundle, quarantineName, mark)
				require.NoError(t, err)
				assert.Equal(t, tt.mark, string(mark[:n]), "the mark is left as it was")
			}
		})
	}
}

func TestTranslocatesOfNoCopyFails(t *testing.T) {
	t.Parallel()

	_, err := desktop.Translocates(filepath.Join(t.TempDir(), "missing.app"))
	assert.Error(t, err)
}
