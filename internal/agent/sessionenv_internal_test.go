package agent

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEachAgentDeclaresWhatItsSessionsSetForTheirChildren(t *testing.T) {
	t.Parallel()

	claudes := []string{
		"CLAUDECODE", "CLAUDE_CODE_CHILD_SESSION", "CLAUDE_CODE_CHILD_NOTES_DIR", "CLAUDE_CODE_ENTRYPOINT",
		"CLAUDE_CODE_EXECPATH", "CLAUDE_CODE_MESSAGING_SOCKET", "CLAUDE_CODE_MESSAGING_TOKEN",
		"CLAUDE_CODE_SESSION_ATTENDED", "CLAUDE_CODE_SESSION_ID", "CLAUDE_EFFORT", "CLAUDE_PID", "CLAUDE_TMPDIR",
	}
	codexes := []string{"CODEX_SANDBOX", "CODEX_SANDBOX_NETWORK_DISABLED", "CODEX_THREAD_ID"}

	assert.Equal(t, claudes, claude{}.sessionEnv())
	assert.Equal(t, codexes, codex{}.sessionEnv())
	assert.Equal(t, slices.Concat(claudes, codexes), SessionEnv(), "every agent's, the default's first")
}

func TestABackgroundRunDropsEveryVariableItsAgentsSessionsSet(t *testing.T) {
	t.Parallel()

	for _, s := range specs() {
		id := s.info().ID

		markers := s.sessionEnv()
		require.NotEmpty(t, markers, id)

		env := make([]string, 0, len(markers))
		for _, name := range markers {
			env = append(env, name+"=1")
		}

		assert.Empty(t, HeadlessEnv(id, env), "%s's own drops (dropEnv) cover what its sessions set", id)

		if h, ok := s.(headlessSpec); ok {
			for _, name := range markers {
				assert.NotContains(t, h.keptEnv(), name, "%s keeps no session's variable", id)
			}
		}
	}
}
