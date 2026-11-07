package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// WriteFile writes content to path with mode 0600, creating its directory.
func WriteFile(t testing.TB, path, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// WriteExecutable writes content to path with mode 0700, creating its directory, so the
// test can run it.
func WriteExecutable(t testing.TB, path, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o700)) //nolint:gosec // a test executable must be runnable
}

// RealTempDir returns a new temporary directory with its symbolic links resolved. On
// macOS the temporary directory sits under one, and code that resolves a path, as an agent
// does with its working directory, sees the resolved form.
func RealTempDir(t testing.TB) string {
	t.Helper()

	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	return dir
}

// SkipAsRoot skips a test that relies on file permissions, which root ignores.
func SkipAsRoot(t testing.TB) {
	t.Helper()

	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
}

// ClaudeHome returns a fresh home directory holding dirs, made as MakeClaudeDirs makes
// them.
func ClaudeHome(t testing.TB, dirs ...string) string {
	t.Helper()

	home := t.TempDir()
	MakeClaudeDirs(t, home, dirs...)

	return home
}

// MakeClaudeDirs creates each of dirs, with its parents, under home. A top-level one
// named like a named agent profile's config directory, .claude_<name> or .claude-<name>,
// also holds the state file the agent writes on its first run with it, so profile
// discovery lists it.
func MakeClaudeDirs(t testing.TB, home string, dirs ...string) {
	t.Helper()

	for _, dir := range dirs {
		path := filepath.Join(home, dir)
		require.NoError(t, os.MkdirAll(path, 0o700))

		if isProfileDir(dir) {
			WriteFile(t, filepath.Join(path, ".claude.json"), "{}")
		}
	}
}

// isProfileDir reports whether dir, relative to a home directory, is named like a named
// agent profile's config directory.
func isProfileDir(dir string) bool {
	return !strings.ContainsRune(dir, '/') && (strings.HasPrefix(dir, ".claude_") || strings.HasPrefix(dir, ".claude-"))
}
