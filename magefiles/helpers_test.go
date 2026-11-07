//go:build mage

package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// isolatedPath points PATH at a fresh, empty directory and returns it, so a target finds
// only the fake tools a test writes there.
func isolatedPath(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv("PATH", dir)

	return dir
}

// fakeTool writes an executable shell script named name into dir that runs body, and
// returns its path. Scripts may rely only on shell builtins and absolute paths, because
// PATH may hold nothing but fake tools.
func fakeTool(t *testing.T, dir, name, body string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	writeExecutable(t, path, "#!/bin/sh\n"+body+"\n")

	return path
}

// fakeLoggedTool writes a fake tool named name into dir, as fakeTool does, that appends
// its arguments as one line to a fresh log before it runs body, and returns the log's
// path. The script holds the path in $log, so body can append lines of its own.
func fakeLoggedTool(t *testing.T, dir, name, body string) string {
	t.Helper()

	log := filepath.Join(t.TempDir(), name+".log")
	require.NotContains(t, log, "'", "the script single-quotes the log's path")

	fakeTool(t, dir, name, "log='"+log+"'\n"+`printf '%s\n' "$*" >> "$log"`+"\n"+body)

	return log
}

// writeExecutable writes content to path, creating its directory, with the execute bit set.
func writeExecutable(t *testing.T, path, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o700)) //nolint:gosec // a test tool must be executable
}

// writeFile writes content to path, creating its directory.
func writeFile(t *testing.T, path, content string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

// readFile returns the content of path, failing the test when it cannot be read.
func readFile(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path) //nolint:gosec // a path the test itself created
	require.NoError(t, err)

	return string(body)
}

// logLines returns the lines a fake tool appended to the log at path.
func logLines(t *testing.T, path string) []string {
	t.Helper()

	return strings.Split(strings.TrimSuffix(readFile(t, path), "\n"), "\n")
}

// inTempDir makes a fresh temporary directory the working directory and returns it.
func inTempDir(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Chdir(dir)

	return dir
}

// inRepoRoot makes the repository root the working directory, as it is when magex runs.
func inRepoRoot(t *testing.T) {
	t.Helper()

	t.Chdir("..")
}

// skipAsRoot skips a test that relies on file permissions, which root ignores.
func skipAsRoot(t *testing.T) {
	t.Helper()

	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
}

// requireTool skips the test when name is not on PATH.
func requireTool(t *testing.T, name string) string {
	t.Helper()

	path, err := exec.LookPath(name)
	if err != nil {
		t.Skip(name + " is not installed")
	}

	return path
}

// skipOnMacOS skips a test of the guard that stops macOS-only targets elsewhere.
func skipOnMacOS(t *testing.T) {
	t.Helper()

	if runtime.GOOS == "darwin" {
		t.Skip("covers the refusal on other systems")
	}
}

// onlyOnMacOS skips a test that needs macOS.
func onlyOnMacOS(t *testing.T) {
	t.Helper()

	if runtime.GOOS != "darwin" {
		t.Skip("needs macOS")
	}
}

// unsetenv removes key from the environment for the rest of the test.
func unsetenv(t *testing.T, key string) {
	t.Helper()

	t.Setenv(key, "") // restores the original value when the test ends
	require.NoError(t, os.Unsetenv(key))
}

// isolateGit keeps the user's git configuration, and any repository the test runs
// inside, out of git commands the test runs.
func isolateGit(t *testing.T) {
	t.Helper()

	requireTool(t, "git")

	for _, key := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_COMMON_DIR"} {
		unsetenv(t, key)
	}

	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Octo User")
	t.Setenv("GIT_AUTHOR_EMAIL", "octo-user@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Octo User")
	t.Setenv("GIT_COMMITTER_EMAIL", "octo-user@example.com")
}

// git runs git in the working directory and returns its trimmed output.
func git(t *testing.T, args ...string) string {
	t.Helper()

	out, err := exec.CommandContext(t.Context(), "git", args...).CombinedOutput() //nolint:gosec // test fixture setup with fixed arguments
	require.NoError(t, err, "git %s: %s", strings.Join(args, " "), out)

	return strings.TrimSpace(string(out))
}

// captureStdout runs fn and returns what it wrote to standard output.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	r, w, err := os.Pipe()
	require.NoError(t, err)

	original := os.Stdout
	os.Stdout = w

	defer func() { os.Stdout = original }()

	read := make(chan []byte)

	go func() {
		var buf bytes.Buffer

		_, _ = io.Copy(&buf, r)

		read <- buf.Bytes()
	}()

	fn()

	require.NoError(t, w.Close())

	out := <-read

	require.NoError(t, r.Close())

	return string(out)
}
