package launcher

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// userSettings are variables of the user's own beside the agents' session variables, which
// a launch passes through.
func userSettings() []string {
	return []string{"CLAUDE_CODE_MAX_OUTPUT_TOKENS", "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "CODEX_CA_CERTIFICATE"}
}

// inheritedEnv is this process's environment as a terminal app took it on from a shell
// inside another agent session: every agent's session variables set to "inherited", and
// each of the user's settings to "kept".
func inheritedEnv() []string {
	markers, settings := agent.SessionEnv(), userSettings()
	named := slices.Concat(markers, settings)

	env := slices.DeleteFunc(os.Environ(), func(e string) bool {
		name, _, _ := strings.Cut(e, "=")

		return slices.Contains(named, name)
	})

	for _, name := range markers {
		env = append(env, name+"=inherited")
	}

	for _, name := range settings {
		env = append(env, name+"=kept")
	}

	return env
}

// TestALaunchScriptStartsTheAgentAsNoSessionsChild runs a launch script, each agent's, in
// an environment a terminal app took on from a shell inside another agent session, as
// macOS's open hands an app it starts: the agent it starts gets none of any session's
// variables, and every setting of the user's beside them.
func TestALaunchScriptStartsTheAgentAsNoSessionsChild(t *testing.T) {
	t.Parallel()

	markers, settings := agent.SessionEnv(), userSettings()

	var want strings.Builder
	for _, name := range markers {
		want.WriteString(name + "=\n")
	}

	for _, name := range settings {
		want.WriteString(name + "=kept\n")
	}

	for _, req := range []Request{baseRequest(), codexRequest()} {
		t.Run(string(req.Agent), func(t *testing.T) {
			t.Parallel()

			bin, fake := testutil.FakeTool(t, string(req.Agent), testutil.FakeRun{ReportEnv: slices.Concat(markers, settings)})
			req.Dir, req.BinaryPath = t.TempDir(), bin

			plan, err := BuildPlan(req)
			require.NoError(t, err)

			files, err := WriteLaunchFiles(testPaths(t), plan, "Review", fixedNow(), nil)
			require.NoError(t, err)

			script, err := os.ReadFile(files.Script)
			require.NoError(t, err)

			for _, name := range markers {
				assert.Contains(t, string(script), "\nunset "+name+"\n")
			}

			for _, name := range settings {
				assert.NotContains(t, string(script), "unset "+name+"\n", "the user's own setting is never unset")
			}

			out, err := runSh(t, append(inheritedEnv(), fake), files.Script)
			require.NoError(t, err)
			assert.Equal(t, want.String(), string(out),
				"no session's variable reaches the agent, whichever agent set it, and the user's settings do")
		})
	}
}
