package execpath

import (
	"context"
	"fmt"
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

// requireNotOnPath fails the test when the fake tool is already on the inherited search
// path, which would let Find succeed without looking where the test means it to.
func requireNotOnPath(t *testing.T) {
	t.Helper()

	_, err := exec.LookPath(fakeTool)
	require.Error(t, err, "the search path must not already contain the fake tool")
}

func TestFindAugmentsGUIPath(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the fake tool is a POSIX script")
	}

	requireNotOnPath(t)

	extra := t.TempDir()
	want := writeExecutable(t, extra)

	got, err := Search{Home: t.TempDir(), Extra: []string{"", extra}}.Find(fakeTool)
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

	got, err := Search{Extra: []string{extra}}.Find(fakeTool)
	require.NoError(t, err)
	assert.Equal(t, want, got)
}

func TestFindHomeWellKnownDir(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the fake tool is a POSIX script")
	}

	requireNotOnPath(t)

	home := t.TempDir()
	want := writeExecutable(t, filepath.Join(home, ".local", "bin"))

	got, err := Search{Home: home}.Find(fakeTool)
	if runtime.GOOS == "darwin" {
		require.NoError(t, err)
		assert.Equal(t, want, got)

		return
	}

	require.ErrorIs(t, err, ErrNotFound)
}

func TestFindNotFoundNamesProgramAndDirs(t *testing.T) {
	t.Parallel()

	requireNotOnPath(t)

	extra := t.TempDir()

	_, err := Search{Extra: []string{extra}}.Find(fakeTool)
	require.ErrorIs(t, err, ErrNotFound)
	assert.Contains(t, err.Error(), fakeTool)
	assert.Contains(t, err.Error(), extra)
}

func TestFindSkipsNonExecutable(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("execute permission is a POSIX concept")
	}

	requireNotOnPath(t)

	extra := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(extra, fakeTool), []byte("data"), 0o600))

	_, err := Search{Extra: []string{extra}}.Find(fakeTool)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestWellKnownDirs(t *testing.T) {
	t.Parallel()

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

// writeScript creates the fake tool in a fresh directory, off the search path, as a shell
// script running body, and returns its absolute path.
func writeScript(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), fakeTool)
	testutil.WriteExecutable(t, path, "#!/bin/sh\n"+body+"\n")

	return path
}

func TestExecRun(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the fake tools are POSIX scripts")
	}

	testutil.RequireGit(t)

	tests := []struct {
		name string
		exec Exec
		argv []string

		// canceled runs the program under a context canceled beforehand.
		canceled bool

		// check asserts what the run returned.
		check func(t *testing.T, stdout, stderr []byte, err error)
	}{
		{
			name: "a program on the search path",
			argv: []string{"git", "--version"},
			check: func(t *testing.T, stdout, stderr []byte, err error) {
				t.Helper()

				require.NoError(t, err)
				assert.Contains(t, string(stdout), "git version")
				assert.Empty(t, stderr)
			},
		},
		{
			name: "an absolute path runs without a search",
			argv: []string{writeScript(t, "echo ran")},
			check: func(t *testing.T, stdout, _ []byte, err error) {
				t.Helper()

				require.NoError(t, err)
				assert.Equal(t, "ran\n", string(stdout))
			},
		},
		{
			name: "a non-zero exit keeps its status and output",
			argv: []string{"git", "-C", t.TempDir(), "rev-parse", "--git-dir"},
			check: func(t *testing.T, _, stderr []byte, err error) {
				t.Helper()

				var exitErr *exec.ExitError
				require.ErrorAs(t, err, &exitErr)
				assert.NotZero(t, exitErr.ExitCode())
				assert.Positive(t, ExitCode(err))
				assert.NotEmpty(t, stderr)
			},
		},
		{
			name: "a program found nowhere",
			exec: Exec{Search: Search{Extra: []string{t.TempDir()}}},
			argv: []string{fakeTool},
			check: func(t *testing.T, _, _ []byte, err error) {
				t.Helper()

				require.ErrorIs(t, err, ErrNotFound)
			},
		},
		{
			name:     "a canceled context",
			argv:     []string{"git", "--version"},
			canceled: true,
			check: func(t *testing.T, _, _ []byte, err error) {
				t.Helper()

				require.ErrorIs(t, err, context.Canceled)
			},
		},
		{
			name: "the given environment replaces the process's",
			exec: Exec{Env: []string{"EXAMPLE_SETTING=example-value"}},
			argv: []string{writeScript(t, `printf '%s|%s' "$EXAMPLE_SETTING" "$HOME"`)},
			check: func(t *testing.T, stdout, _ []byte, err error) {
				t.Helper()

				require.NoError(t, err)
				assert.Equal(t, "example-value|", string(stdout))
			},
		},
		{
			name: "no environment inherits the process's",
			argv: []string{writeScript(t, `printf '%s' "$HOME"`)},
			check: func(t *testing.T, stdout, _ []byte, err error) {
				t.Helper()

				require.NoError(t, err)
				assert.Equal(t, os.Getenv("HOME"), string(stdout))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			if tt.canceled {
				cancel()
			}

			stdout, stderr, err := tt.exec.Run(ctx, tt.argv[0], tt.argv[1:]...)
			tt.check(t, stdout, stderr, err)
		})
	}
}

// fakeExitError is a fake process exit with a status.
type fakeExitError int

func (e fakeExitError) Error() string { return fmt.Sprintf("exit status %d", int(e)) }

func (e fakeExitError) ExitCode() int { return int(e) }

func TestExitCode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "no error", want: -1},
		{name: "not an exit", err: ErrNotFound, want: -1},
		{name: "an exit", err: fakeExitError(128), want: 128},
		{name: "a wrapped exit", err: fmt.Errorf("run git: %w", fakeExitError(1)), want: 1},
		{name: "a clean exit", err: fakeExitError(0), want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, ExitCode(tt.err))
		})
	}
}

func TestFirstLineAndGitMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		out   string
		first string
		git   string
	}{
		{name: "nothing"},
		{name: "blank lines", out: " \n\n\t\n"},
		{name: "fatal", out: "\n  fatal: not a git repository\nmore", first: "fatal: not a git repository", git: "not a git repository"},
		{name: "error", out: "error: bad thing", first: "error: bad thing", git: "bad thing"},
		{name: "both prefixes", out: "fatal: error: twice", first: "fatal: error: twice", git: "twice"},
		{name: "no prefix", out: "LSOpenURLsWithRole() failed\nmore", first: "LSOpenURLsWithRole() failed", git: "LSOpenURLsWithRole() failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.first, FirstLine([]byte(tt.out)))
			assert.Equal(t, tt.git, GitMessage([]byte(tt.out)))
		})
	}
}
