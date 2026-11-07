package execpath

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// fakeTool is the name of the executable the tests create; nothing real is called that.
const fakeTool = "example-fake-tool"

// writeExecutable creates the fake tool as an executable file in dir and returns its path.
func writeExecutable(t *testing.T, dir string) string {
	t.Helper()

	path := filepath.Join(dir, fakeTool)
	testutil.WriteExecutable(t, path, "#!/bin/sh\nexit 0\n")

	return path
}

func TestFindAugmentsGUIPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake tool is a POSIX script")
	}

	t.Setenv("PATH", "/usr/bin:/bin")

	extra := t.TempDir()
	want := writeExecutable(t, extra)

	_, err := exec.LookPath(fakeTool)
	require.Error(t, err, "the minimal PATH must not already contain the fake tool")

	got, err := Finder{Home: t.TempDir(), Extra: []string{"", extra}}.Find(fakeTool)
	require.NoError(t, err)
	assert.Equal(t, want, got)
	assert.True(t, filepath.IsAbs(got))
}

func TestFindPrefersSearchPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake tool is a POSIX script")
	}

	onPath := t.TempDir()
	want := writeExecutable(t, onPath)
	extra := t.TempDir()
	writeExecutable(t, extra)

	t.Setenv("PATH", onPath)

	got, err := Finder{Extra: []string{extra}}.Find(fakeTool)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestFindHomeWellKnownDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake tool is a POSIX script")
	}

	t.Setenv("PATH", "/usr/bin:/bin")

	home := t.TempDir()
	want := writeExecutable(t, filepath.Join(home, ".local", "bin"))

	got, err := Finder{Home: home}.Find(fakeTool)
	if runtime.GOOS == "darwin" {
		require.NoError(t, err)
		assert.Equal(t, want, got)

		return
	}

	require.ErrorIs(t, err, ErrNotFound)
}

func TestFindNotFoundNamesProgramAndDirs(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")

	extra := t.TempDir()

	_, err := Finder{Extra: []string{extra}}.Find(fakeTool)
	require.ErrorIs(t, err, ErrNotFound)
	assert.Contains(t, err.Error(), fakeTool)
	assert.Contains(t, err.Error(), extra)
}

func TestFindSkipsNonExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("execute permission is a POSIX concept")
	}

	t.Setenv("PATH", "/usr/bin:/bin")

	extra := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(extra, fakeTool), []byte("data"), 0o600))

	_, err := Finder{Extra: []string{extra}}.Find(fakeTool)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestWellKnownDirs(t *testing.T) {
	home := filepath.Join(string(filepath.Separator)+"home", "octo-user")

	switch runtime.GOOS {
	case "darwin":
		assert.Equal(t, []string{
			"/opt/homebrew/bin",
			"/usr/local/bin",
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, "bin"),
			filepath.Join(home, "go", "bin"),
		}, wellKnownDirs(home))
		assert.Equal(t, []string{"/opt/homebrew/bin", "/usr/local/bin"}, wellKnownDirs(""))
	default:
		assert.Empty(t, wellKnownDirs(home))
	}
}

func TestRunnerRun(t *testing.T) {
	testutil.RequireGit(t)

	runner := Runner{Finder: Finder{}}

	stdout, stderr, err := runner.Run(t.Context(), "git", "--version")
	require.NoError(t, err)
	assert.Contains(t, string(stdout), "git version")
	assert.Empty(t, stderr)
}

func TestRunnerRunAbsolutePath(t *testing.T) {
	abs, err := filepath.Abs(testutil.RequireGit(t))
	require.NoError(t, err)

	t.Setenv("PATH", "")

	stdout, _, err := Runner{}.Run(t.Context(), abs, "--version")
	require.NoError(t, err)
	assert.Contains(t, string(stdout), "git version")
}

func TestRunnerRunExitError(t *testing.T) {
	testutil.RequireGit(t)

	_, stderr, err := Runner{}.Run(t.Context(), "git", "-C", t.TempDir(), "rev-parse", "--git-dir")
	require.Error(t, err)

	var exitErr *exec.ExitError
	require.ErrorAs(t, err, &exitErr)
	assert.NotZero(t, exitErr.ExitCode())
	assert.NotEmpty(t, stderr)
}

func TestRunnerRunNotFound(t *testing.T) {
	t.Setenv("PATH", "/usr/bin:/bin")

	_, _, err := Runner{Finder: Finder{Extra: []string{t.TempDir()}}}.Run(t.Context(), fakeTool)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestRunnerRunCanceled(t *testing.T) {
	testutil.RequireGit(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, _, err := Runner{}.Run(ctx, "git", "--version")
	require.ErrorIs(t, err, context.Canceled)
}
