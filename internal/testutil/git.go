package testutil

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// GitName and GitEmail are the identity every isolated git command commits as.
const (
	GitName  = "Octo User"
	GitEmail = "octo-user@example.com"
)

// RequireGit skips the test when git is not installed and returns its path.
func RequireGit(t testing.TB) string {
	t.Helper()

	path, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed")
	}

	return path
}

// GitEnv returns an environment for git that reads none of the user's or the system's
// configuration: home is HOME, and XDG_CONFIG_HOME is under it, so no personal ignore or
// attributes file applies either, and commits are made as GitName and GitEmail. Every
// GIT_ variable inherited from the test process is dropped, so a test run from a git hook,
// which sets GIT_DIR and GIT_INDEX_FILE, cannot reach the repository the hook runs in.
//
// It is meant for cmd.Env, or execpath.Exec's Env when the code under test runs git, so a
// test can isolate git without changing the process environment and still run in parallel.
func GitEnv(home string) []string {
	inherited := os.Environ()
	env := make([]string, 0, len(inherited)+9)

	for _, entry := range inherited {
		key, _, _ := strings.Cut(entry, "=")
		if key != "HOME" && key != "XDG_CONFIG_HOME" && !strings.HasPrefix(key, "GIT_") {
			env = append(env, entry)
		}
	}

	return append(env,
		"HOME="+home,
		"XDG_CONFIG_HOME="+filepath.Join(home, ".config"),
		"GIT_CONFIG_NOSYSTEM=1",
		"GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME="+GitName,
		"GIT_AUTHOR_EMAIL="+GitEmail,
		"GIT_COMMITTER_NAME="+GitName,
		"GIT_COMMITTER_EMAIL="+GitEmail,
	)
}

// Git skips the test when git is not installed, and otherwise returns a GitEnv over a
// fresh home directory.
func Git(t testing.TB) []string {
	t.Helper()

	RequireGit(t)

	return GitEnv(t.TempDir())
}

// RunGit runs git with args in dir, isolated as Git describes, and returns what it wrote
// to standard output. A failure fails the test with what git wrote to standard error.
func RunGit(t testing.TB, dir string, args ...string) string {
	t.Helper()

	var stdout, stderr bytes.Buffer

	cmd := exec.CommandContext(t.Context(), "git", args...) //nolint:gosec // test fixture setup; argv only, no shell
	cmd.Dir = dir
	cmd.Env = Git(t)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	require.NoError(t, cmd.Run(), "git %s: %s", strings.Join(args, " "), stderr.String())

	return stdout.String()
}

// Commit writes content to the file name in the repository at dir, creating its
// directory, adds it, and commits it with name as the message, isolated as Git
// describes. It returns the new commit.
func Commit(t testing.TB, dir, name, content string) string {
	t.Helper()

	WriteFile(t, filepath.Join(dir, name), content)
	RunGit(t, dir, "add", name)
	RunGit(t, dir, "commit", "-q", "-m", name)

	return strings.TrimSpace(RunGit(t, dir, "rev-parse", "HEAD"))
}

// InitRepo creates dir, when it is missing, as a new git repository whose first branch
// is main.
func InitRepo(t testing.TB, dir string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(dir, 0o700))
	RunGit(t, dir, "-c", "init.defaultBranch=main", "init", "-q")
}
