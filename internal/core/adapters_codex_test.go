package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/agentrun"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Codex as a background run's agent, and as the fallback of a Claude Code profile out of
// usage (Q16): its plan runs its own program, read by its own parser, through the same
// runner and the same chain.

// codexThread is the S6 success fixture's thread.
const codexThread = "01a1e66a-b48a-7f43-a547-901d3ca2bb33"

// codexPlan is a background plan of a fake codex playing run.
func (f *fakePlanner) codexPlan(t *testing.T, run testutil.FakeRun) *launcher.BackgroundPlan {
	t.Helper()

	path, env := testutil.FakeTool(t, "codex", run)
	name, value, _ := strings.Cut(env, "=")
	sid := textutil.NewUUID()

	return &launcher.BackgroundPlan{
		Plan:   launcher.Plan{Agent: agent.Codex, Binary: path, Env: map[string]string{name: value}},
		Prompt: "Review it.", SessionID: sid, Folder: filepath.Join(f.folders, sid), ReadOnly: true,
		Preset: fence.ReadOnly, Command: "codex exec --json - < prompt.md",
		Worktree: launcher.WorktreeRequest{Kind: item.KindPR, Number: 12},
	}
}

// codexChain is the chain rig with Codex's default profile running fake codex playing run.
func codexChain(t *testing.T, run testutil.FakeRun) (*agentRig, autopilot.Job) {
	t.Helper()

	r, job := chainRig(t)
	r.planner.profiles["codex/"+defaultProfile] = plannedProfile{
		agent: agent.Codex, profile: defaultProfile, bp: r.planner.codexPlan(t, run),
	}

	later := time.Now().Add(time.Hour)
	job.Resting = []autopilot.Resting{resting(claudeID, defaultProfile, later), resting(claudeID, workProfile, later)}

	return r, job
}

func TestAClaudeProfileOutOfUsageFallsBackToCodexInTheBackground(t *testing.T) {
	t.Parallel()

	r, job := codexChain(t, testutil.FakeRun{Stdout: agentFixture(t, "codex/success-structured.jsonl")})

	res, err := r.agents.Dispatch(t.Context(), job)
	require.NoError(t, err)
	require.True(t, res.Running, res.WaitWords)
	assert.Equal(t, "codex", res.Launched.Agent, "the run's row keeps the agent it ran with")
	assert.Equal(t, defaultProfile, res.Launched.Profile)

	end := r.engine.end(t, 21)
	assert.Equal(t, autopilot.RunDone, end.Outcome, "Codex's stream, read by its own parser")
	assert.Nil(t, end.Cost, "Codex reports tokens, never a cost")
	assert.Equal(t, int64(462), end.TokensOut)
}

func TestACodexRunOutOfUsageRestsItsProfileUntilItsRolloutsReset(t *testing.T) {
	t.Parallel()

	failed := `{"type":"turn.failed","error":{"message":"You've hit your usage limit."}}` + "\n"
	r, job := codexChain(t, testutil.FakeRun{Stdout: agentLines(t, "codex/success-structured.jsonl", 2) + failed, Exit: 1})

	day := filepath.Join(r.codexHome, ".codex", "sessions", "2026", "11", "16")
	require.NoError(t, os.MkdirAll(day, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(day, "rollout-2026-11-16T22-00-00-"+codexThread+".jsonl"), []byte(
		`{"type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":{"primary":{"used_percent":100,`+
			`"window_minutes":300,"resets_at":1794870000},"secondary":null,"rate_limit_reached_type":"rate_limit_reached"}}}`+
			"\n",
	), 0o600))

	res, err := r.agents.Dispatch(t.Context(), job)
	require.NoError(t, err)
	require.True(t, res.Running, res.WaitWords)

	end := r.engine.end(t, 21)
	assert.Equal(t, autopilot.RunOutOfUsage, end.Outcome, "out of usage, which the engine rests the profile for")
	require.NotNil(t, end.Limit)
	assert.Equal(t, "five_hour", end.Limit.Type, "which limit, from the thread's rollout")
	assert.Equal(t, time.Unix(1794870000, 0).UTC(), end.Limit.ResetsAt, "and when it resets")
}

func TestRunLimitsReadsOnlyCodexsRollout(t *testing.T) {
	t.Parallel()

	l := runLimits{}

	_, ok := l.Limit(t.Context(), agent.Claude, "", codexThread)
	assert.False(t, ok, "Claude Code's stream says its limit itself")

	_, ok = l.Limit(t.Context(), agent.Codex, "", "")
	assert.False(t, ok, "a run whose thread is unknown")

	var _ agentrun.LimitReader = l
}

func TestACodexRunsEnvironmentIsItsProfilesWithNoneOfCodexsOwn(t *testing.T) {
	t.Parallel()

	home := "/Users/example"
	base := []string{
		"PATH=/usr/bin", "CODEX_HOME=/Users/example/.codex_other", "CODEX_SANDBOX=seatbelt", "CODEX_THREAD_ID=x",
		"CODEX_API_KEY=k", "CODEX_CA_CERTIFICATE=/etc/ca.pem",
	}

	work := agentEnv(base, launcher.Plan{
		Agent: agent.Codex, Env: map[string]string{"CODEX_HOME": home + "/.codex_work"},
		Unset: []string{"OPENAI_API_KEY", "CODEX_API_KEY", "CODEX_ACCESS_TOKEN"},
	}, home)
	assert.ElementsMatch(t, []string{"PATH=/usr/bin", "CODEX_HOME=/Users/example/.codex_work", "CODEX_CA_CERTIFICATE=/etc/ca.pem"},
		work, "the profile's home, a proxy's certificate, and nothing else of Codex's own")

	def := agentEnv(base, launcher.Plan{Agent: agent.Codex, Unset: []string{"CODEX_HOME"}}, home)
	assert.NotContains(t, strings.Join(def, "\n"), "CODEX_HOME", "the default profile's home is Codex's own")
}
