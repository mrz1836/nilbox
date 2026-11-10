package shellenv_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/shellenv"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// errKilled and errExit stand for a shell the time limit killed and one that failed.
var (
	errKilled = errors.New("signal: killed")
	errExit   = errors.New("exit status 1")
)

// TestMain fails the run when a test leaves a goroutine behind.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

// appEnv is the app's own environment in these tests: launchd's few variables.
func appEnv(shell string) func() []string {
	return func() []string {
		return []string{"HOME=/Users/example", "SHELL=" + shell, "PATH=/usr/bin:/bin:/usr/sbin:/sbin"}
	}
}

// shells writes a list of login shells and returns its path.
func shells(t *testing.T, listed ...string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "shells")
	testutil.WriteFile(t, path, "# valid login shells\n"+strings.Join(listed, "\n")+"\n")

	return path
}

// markerOf returns the marker the read asked the shell to print.
func markerOf(t *testing.T, argv []string) string {
	t.Helper()

	script := argv[len(argv)-1]
	marker, _, ok := strings.Cut(strings.TrimPrefix(script, "printf '%s' "), ";")
	require.True(t, ok, "the script prints a marker first: %q", script)

	return marker
}

// printing returns a starter that answers with before, the marker, body, the marker, and
// after, and records the argv it was given.
func printing(t *testing.T, before, body, after string, argv *[]string) shellenv.Starter {
	t.Helper()

	return func(_ context.Context, a, _ []string) ([]byte, error) {
		*argv = a
		m := markerOf(t, a)

		return []byte(before + m + body + m + after), nil
	}
}

func TestReadIgnoresWhatTheShellPrintsAroundTheMarkers(t *testing.T) {
	t.Parallel()

	var argv []string

	r := shellenv.New(shellenv.Config{
		Start:   printing(t, "Welcome back!\nLast login: today\n", "A=1\x00PATH=/opt/homebrew/bin:/usr/bin\x00", "bye\n", &argv),
		Environ: appEnv("/bin/zsh"), Shells: shells(t, "/bin/zsh"),
	})

	require.NoError(t, r.Read(t.Context()))

	env, ok := r.Env()
	assert.True(t, ok)
	assert.Equal(t, []string{"A=1", "PATH=/opt/homebrew/bin:/usr/bin"}, env)
	assert.Equal(t, "/opt/homebrew/bin:/usr/bin", r.Path())
	assert.Equal(t, []string{"/bin/zsh", "-i", "-l", "-c"}, argv[:4], "an interactive login shell")
	assert.Contains(t, argv[4], "/usr/bin/env -0", "values are NUL-separated")
}

func TestReadKeepsValuesWithNewlines(t *testing.T) {
	t.Parallel()

	var argv []string

	r := shellenv.New(shellenv.Config{
		Start:   printing(t, "", "MULTI=line one\nline two\x00EMPTY=\x00=no-name\x00NOEQUALS\x00", "", &argv),
		Environ: appEnv("/bin/zsh"), Shells: shells(t, "/bin/zsh"),
	})

	require.NoError(t, r.Read(t.Context()))

	env, _ := r.Env()
	assert.Equal(t, []string{"MULTI=line one\nline two", "EMPTY="}, env, "a value with a newline survives; nameless entries don't")
}

func TestReadEachTimeUsesANewMarker(t *testing.T) {
	t.Parallel()

	var markers []string

	r := shellenv.New(shellenv.Config{
		Start: func(_ context.Context, argv, _ []string) ([]byte, error) {
			m := markerOf(t, argv)
			markers = append(markers, m)

			return []byte(m + "A=1\x00" + m), nil
		},
		Environ: appEnv("/bin/zsh"), Shells: shells(t, "/bin/zsh"),
	})

	require.NoError(t, r.Read(t.Context()))
	require.NoError(t, r.Read(t.Context()))
	require.Len(t, markers, 2)
	assert.NotEqual(t, markers[0], markers[1])
	assert.GreaterOrEqual(t, len(markers[0]), 32)
}

func TestReadTimesOutAndFallsBack(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := shellenv.New(shellenv.Config{
			Start: func(ctx context.Context, _, _ []string) ([]byte, error) {
				<-ctx.Done()

				return nil, errKilled
			},
			Environ: appEnv("/bin/zsh"), Shells: shells(t, "/bin/zsh"), Home: "/Users/example",
		})

		start := time.Now()
		err := r.Read(t.Context())

		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Equal(t, shellenv.DefaultTimeout, time.Since(start), "the read gives up after its time limit")

		select {
		case <-r.Ready():
		default:
			t.Fatal("Ready closes once the first read ends, even a failed one")
		}

		env, ok := r.Env()
		assert.False(t, ok)
		assert.Contains(t, env, "HOME=/Users/example", "the app's own environment")
		assert.Equal(t, execpath.FallbackPath("/Users/example", "/usr/bin:/bin:/usr/sbin:/sbin"), shellenv.Lookup(env, "PATH"),
			"with the well-known tool folders first on PATH")
	})
}

func TestReadFailsWithoutMarkersOrVariables(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		out  func(marker string) string
		want error
	}{
		{"no output", func(string) string { return "" }, shellenv.ErrNoMarkers},
		{"one marker", func(m string) string { return "zsh: bad option\n" + m + "A=1" }, shellenv.ErrNoMarkers},
		{"nothing between", func(m string) string { return m + m }, shellenv.ErrNoVariables},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := shellenv.New(shellenv.Config{
				Start: func(_ context.Context, argv, _ []string) ([]byte, error) {
					return []byte(tt.out(markerOf(t, argv))), nil
				},
				Environ: appEnv("/bin/zsh"), Shells: shells(t, "/bin/zsh"),
			})

			require.ErrorIs(t, r.Read(t.Context()), tt.want)

			_, ok := r.Env()
			assert.False(t, ok)
		})
	}
}

func TestReadKeepsTheLastGoodEnvironment(t *testing.T) {
	t.Parallel()

	fail := false
	r := shellenv.New(shellenv.Config{
		Start: func(_ context.Context, argv, _ []string) ([]byte, error) {
			if fail {
				return nil, errExit
			}

			m := markerOf(t, argv)

			return []byte(m + "PATH=/opt/homebrew/bin\x00" + m), nil
		},
		Environ: appEnv("/bin/zsh"), Shells: shells(t, "/bin/zsh"),
	})

	require.NoError(t, r.Read(t.Context()))

	fail = true

	require.Error(t, r.Read(t.Context()))

	env, ok := r.Env()
	assert.True(t, ok, "a failed read again keeps what the last good one found")
	assert.Equal(t, []string{"PATH=/opt/homebrew/bin"}, env)
}

func TestEnvBeforeTheFirstReadIsTheFallback(t *testing.T) {
	t.Parallel()

	r := shellenv.New(shellenv.Config{Environ: appEnv("/bin/zsh"), Home: "/Users/example"})

	env, ok := r.Env()
	assert.False(t, ok)
	assert.Equal(t, execpath.FallbackPath("/Users/example", "/usr/bin:/bin:/usr/sbin:/sbin"), shellenv.Lookup(env, "PATH"))

	select {
	case <-r.Ready():
		t.Fatal("Ready closes only once a read ends")
	default:
	}
}

func TestShellFallsBackToZsh(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, shell string
		listed      []string
		want        string
	}{
		{"listed", "/bin/bash", []string{"/bin/zsh", "/bin/bash"}, "/bin/bash"},
		{"empty", "", []string{"/bin/zsh", "/bin/bash"}, "/bin/zsh"},
		{"not listed", "/opt/example/sh", []string{"/bin/zsh", "/bin/bash"}, "/bin/zsh"},
		{"relative", "bash", []string{"bash"}, "/bin/zsh"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var argv []string

			r := shellenv.New(shellenv.Config{
				Start: printing(t, "", "A=1\x00", "", &argv), Environ: appEnv(tt.shell), Shells: shells(t, tt.listed...),
			})

			require.NoError(t, r.Read(t.Context()))
			assert.Equal(t, tt.want, argv[0])
		})
	}

	var argv []string

	r := shellenv.New(shellenv.Config{
		Start: printing(t, "", "A=1\x00", "", &argv), Environ: appEnv("/bin/bash"), Shells: filepath.Join(t.TempDir(), "missing"),
	})

	require.NoError(t, r.Read(t.Context()))
	assert.Equal(t, "/bin/zsh", argv[0], "without a list of shells, $SHELL can't be trusted")
}

func TestLogNeverHoldsANameOrAValue(t *testing.T) {
	t.Parallel()

	logs := &testutil.Logs{}
	fail := false

	var mu sync.Mutex

	r := shellenv.New(shellenv.Config{
		Start: func(_ context.Context, argv, _ []string) ([]byte, error) {
			mu.Lock()
			defer mu.Unlock()

			if fail {
				return []byte("PRIVATE_PROJECT_TOKEN=very-secret-value"), errExit
			}

			m := markerOf(t, argv)

			return []byte(m + "PRIVATE_PROJECT_TOKEN=very-secret-value\x00OTHER_NAME=other\x00" + m), nil
		},
		Environ: appEnv("/bin/zsh"), Shells: shells(t, "/bin/zsh"), Logger: logs.Logger(),
	})

	require.NoError(t, r.Read(t.Context()))

	mu.Lock()
	fail = true
	mu.Unlock()

	require.Error(t, r.Read(t.Context()))

	text := logs.String()
	assert.Contains(t, text, "variables=2")
	assert.Contains(t, text, "duration=")
	assert.Contains(t, text, "exit status 1")

	for _, secret := range []string{"PRIVATE_PROJECT_TOKEN", "very-secret-value", "OTHER_NAME", "other\n"} {
		assert.NotContains(t, text, secret)
	}
}

func TestRunShellReadsARealShell(t *testing.T) {
	t.Parallel()

	sh := "/bin/sh"
	if _, err := os.Stat(sh); err != nil {
		t.Skip("no /bin/sh")
	}

	home := t.TempDir()
	r := shellenv.New(shellenv.Config{
		Environ: func() []string {
			// A fresh home, so no developer's startup file runs.
			return []string{"HOME=" + home, "SHELL=" + sh, "PATH=/usr/bin:/bin", "MULTI=one\ntwo"}
		},
		Shells: shells(t, sh),
	})

	require.NoError(t, r.Read(t.Context()))

	env, ok := r.Env()
	require.True(t, ok)
	assert.Equal(t, home, shellenv.Lookup(env, "HOME"))
	assert.Equal(t, "one\ntwo", shellenv.Lookup(env, "MULTI"))
}

func TestRunShellReportsAShellThatFails(t *testing.T) {
	t.Parallel()

	_, err := shellenv.RunShell(t.Context(), []string{"/bin/sh", "-c", "exit 3"}, []string{"PATH=/usr/bin:/bin"})
	require.Error(t, err)
	assert.Equal(t, 3, execpath.ExitCode(err))

	_, err = shellenv.RunShell(t.Context(), nil, nil)
	require.Error(t, err)

	_, err = shellenv.RunShell(t.Context(), []string{"relative-shell"}, nil)
	require.Error(t, err)
}
