package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestWhatHoldsAStartKeepsTheNameOfWhatItCheckedAsItIsSpelled(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)
	// The terminal chosen is iTerm2, and this build opens Terminal alone.
	r.deps.registry = terminal.NewRegistry(&scriptOpener{id: terminal.TerminalApp})

	_, words, err := holdsOf(r).Hold(t.Context(), r.job(r.pr, "acme-org/example-repo", spec.AgentStep{}))
	require.NoError(t, err)
	assert.Equal(t, "Not ready on this Mac: iTerm2 isn't one this Mac can open. "+
		"Choose another terminal in Settings → Launching.", words, "iTerm2 is spelled as it spells itself")

	gh := newReadyChecks(agentDeps{finder: testutil.Finder{Paths: map[string]string{}}}).tool("gh")
	assert.Equal(t, "gh isn't on this Mac. Run `brew install gh`, then Check again.", gh.Words(),
		"a program is named as it is typed")
}
