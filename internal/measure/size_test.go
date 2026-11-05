package measure

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSizes(t *testing.T) {
	bundle := filepath.Join(t.TempDir(), "App.app")
	exe := filepath.Join(bundle, "Contents", "MacOS", "app")

	require.NoError(t, os.MkdirAll(filepath.Dir(exe), 0o700))
	require.NoError(t, os.WriteFile(exe, make([]byte, 16_400_000), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(bundle, "Contents", "Info.plist"), make([]byte, 100_000), 0o600))
	require.NoError(t, os.Symlink(exe, filepath.Join(bundle, "Contents", "link")))

	report, err := Sizes(bundle, exe)
	require.NoError(t, err)

	assert.Equal(t, int64(16_400_000), report.BinaryBytes)
	assert.Equal(t, int64(16_500_000), report.BundleBytes, "regular files only")
	assert.Equal(t, []string{
		"size.binary_bytes=16400000 binary_mb=16.4",
		"size.bundle_bytes=16500000 bundle_mb=16.5",
	}, report.Lines())
}

func TestSizesErrors(t *testing.T) {
	dir := t.TempDir()

	_, err := Sizes(dir, filepath.Join(dir, "missing"))
	require.Error(t, err)

	exe := filepath.Join(dir, "app")
	require.NoError(t, os.WriteFile(exe, []byte("x"), 0o600))

	_, err = Sizes(filepath.Join(dir, "missing.app"), exe)
	require.Error(t, err)
}
