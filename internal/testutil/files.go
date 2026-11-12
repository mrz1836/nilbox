package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
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

// WriteFileMode writes content to path, creating its directory, and leaves the file with
// exactly mode: it is set again after the write, so neither the umask nor the mode of a
// file already at path changes it.
func WriteFileMode(t testing.TB, path, content string, mode os.FileMode) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), mode))
	require.NoError(t, os.Chmod(path, mode))
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

// AgentHome returns a fresh home directory holding dirs, made as MakeAgentDirs makes them.
func AgentHome(t testing.TB, dirs ...string) string {
	t.Helper()

	home := t.TempDir()
	MakeAgentDirs(t, home, dirs...)

	return home
}

// MakeAgentDirs creates each of dirs, with its parents, under home. A top-level one named
// like a named profile's config directory of some coding agent, such as .claude_<name>,
// .claude-<name>, or .codex_<name>, also holds the agent's first config marker (Claude's
// state file, or Codex's sign-in), as the agent writes it on its first run with the
// directory, so profile discovery lists it.
func MakeAgentDirs(t testing.TB, home string, dirs ...string) {
	t.Helper()

	for _, dir := range dirs {
		path := filepath.Join(home, dir)
		require.NoError(t, os.MkdirAll(path, 0o700))

		if marker, ok := profileMarker(dir); ok {
			WriteFile(t, filepath.Join(path, marker), "{}")
		}
	}
}

// ClaudeHome returns a fresh home directory holding dirs, as AgentHome does.
func ClaudeHome(t testing.TB, dirs ...string) string {
	t.Helper()

	return AgentHome(t, dirs...)
}

// MakeClaudeDirs creates each of dirs under home, as MakeAgentDirs does.
func MakeClaudeDirs(t testing.TB, home string, dirs ...string) {
	t.Helper()

	MakeAgentDirs(t, home, dirs...)
}

// profileMarker returns the first config marker of the agent whose named profile's config
// directory dir, relative to a home directory, is named like.
func profileMarker(dir string) (string, bool) {
	if strings.ContainsRune(dir, '/') {
		return "", false
	}

	for _, a := range agent.All() {
		if strings.HasPrefix(dir, a.ConfigDir+"_") || strings.HasPrefix(dir, a.ConfigDir+"-") {
			return a.Markers[0].Name, true
		}
	}

	return "", false
}
