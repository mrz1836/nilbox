//go:build integration

package integrations

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/shellenv"
)

// TestIntegrationDetectsTheInstalledTools reads this machine's login shell and looks for
// the real tools, running only their --version, and logs what it found and how long it
// took: the number docs/performance.md gives for detection. It changes nothing.
func TestIntegrationDetectsTheInstalledTools(t *testing.T) { //nolint:paralleltest // times the machine's own shell and tools
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	reader := shellenv.New(shellenv.Config{Home: home})

	start := time.Now()
	readErr := reader.Read(t.Context())
	t.Logf("shell read: %v (error: %v)", time.Since(start).Round(time.Millisecond), readErr)

	env, _ := reader.Env()

	for run := range 3 {
		start = time.Now()
		got := detectAll(t.Context(), Definitions(), nil, dirsFrom(env, home), env, home, execRun)
		took := time.Since(start).Round(time.Millisecond)

		for _, def := range Definitions() {
			d := got[string(def.ID)]
			t.Logf("run %d: %s found=%v version=%s source=%s others=%d", run+1, def.Binary, d.has,
				d.selected.Version.Raw, d.selected.Source, len(d.others))
		}

		t.Logf("run %d: detection took %v", run+1, took)
	}
}
