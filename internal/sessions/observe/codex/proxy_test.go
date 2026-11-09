package codex

import (
	"bufio"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// helperProxyEnv, set to a mode, makes TestHelperProxy act as the proxy a test starts
	// rather than skip: "term" leaves on SIGTERM, writing it to helperMarkerEnv's file, and
	// "ignore" ignores SIGTERM, so only SIGKILL ends it.
	helperProxyEnv = "CODEX_OBSERVER_TEST_HELPER_PROXY"

	// helperMarkerEnv names the file the "term" helper writes the signal it heard to.
	helperMarkerEnv = "CODEX_OBSERVER_TEST_HELPER_MARKER"
)

// TestHelperProxy is not a test of its own: started by the proxy tests with helperProxyEnv
// set, it stands in for `codex app-server proxy` as installed with npm, whose launcher
// passes SIGTERM on to Codex but cannot pass on SIGKILL. It reads nothing, so only a
// signal ends it. Any other time it skips.
func TestHelperProxy(t *testing.T) {
	t.Parallel()

	mode := os.Getenv(helperProxyEnv)
	if mode == "" {
		t.Skip("a helper process for the proxy tests")
	}

	heard := make(chan os.Signal, 1)

	if mode == "ignore" {
		signal.Ignore(syscall.SIGTERM)
	} else {
		signal.Notify(heard, syscall.SIGTERM)
	}

	_, _ = os.Stdout.WriteString("ready\n")

	select {
	case sig := <-heard:
		_ = os.WriteFile(os.Getenv(helperMarkerEnv), []byte(sig.String()), 0o600) //nolint:gosec // the temp file the starting test chose

		os.Exit(0)
	case <-time.After(time.Minute):
		os.Exit(1)
	}
}

// startHelper starts TestHelperProxy in mode as a proxy's stream, and waits until it is
// ready for signals.
func startHelper(t *testing.T, mode, marker string) *procStream {
	t.Helper()

	self, err := os.Executable()
	require.NoError(t, err)

	cmd := exec.CommandContext(t.Context(), self, "-test.run=^TestHelperProxy$") //nolint:gosec // the test's own binary, fixed arguments

	cmd.Env = append(os.Environ(), helperProxyEnv+"="+mode, helperMarkerEnv+"="+marker)

	s, err := startStream(cmd)
	require.NoError(t, err)

	t.Cleanup(func() { _ = s.Close() })

	line, err := bufio.NewReader(s).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "ready\n", line)

	return s
}

func TestProxyStopsWithSIGTERMFirst(t *testing.T) {
	t.Parallel()

	marker := filepath.Join(t.TempDir(), "signal")
	s := startHelper(t, "term", marker)

	require.NoError(t, s.Close())

	got, err := os.ReadFile(marker) //nolint:gosec // the test's own temp file
	require.NoError(t, err, "the proxy heard SIGTERM, which npm's launcher passes on to Codex")
	assert.Equal(t, syscall.SIGTERM.String(), string(got))
	assert.NotNil(t, s.cmd.ProcessState, "the proxy was waited for")
}

func TestProxyIsKilledWhenSIGTERMDoesNotStopIt(t *testing.T) {
	t.Parallel()

	s := startHelper(t, "ignore", "")
	s.grace = 50 * time.Millisecond

	require.NoError(t, s.Close())
	assert.NotNil(t, s.cmd.ProcessState, "the proxy was killed after the grace, and waited for")
}
