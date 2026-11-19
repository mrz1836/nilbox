package core

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/launcher"
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/statusprefs"
	"github.com/mrz1836/nilbox/internal/svcstatus"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestARoutinesPreviewIsTheLaunchersWithTheRoutinesChoicesNamed(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)
	previews := runPreviews{deps: r.deps}

	got, err := previews.PreviewRun(t.Context(), autopilot.RunRequest{
		ItemID: r.pr, Routine: "Heal the build", Trigger: catalog.EventChecksFailed, Attempt: 1,
		Step: spec.AgentStep{
			Agent: claudeID, Profile: workProfile, Model: "opus", Prompt: spec.PromptRef{Starter: prompts.StarterQuickLook},
			Where: catalog.WhereTerminal, Worktree: new(false),
		},
	})
	require.NoError(t, err)

	assert.Equal(t, "Claude", got.Agent)
	assert.Equal(t, workProfile, got.Profile)
	assert.Equal(t, "opus", got.Model)
	assert.NotEmpty(t, got.PromptName, "the starter is found by its key")
	assert.Equal(t, "~/projects/example-repo", got.Dir)
	assert.Equal(t, "iTerm2", got.Terminal)
	assert.Contains(t, got.Command, "opus")
	assert.Nil(t, got.Worktree, "a run in the clone has no worktree to show")
	assert.Equal(t, "routine", got.Sources["agent"], "what the routine chose is the routine's, not the launch menu's")
	assert.Equal(t, "routine", got.Sources["profile"])
	assert.Equal(t, "routine", got.Sources["model"])
	assert.Equal(t, "routine", got.Sources["prompt"])
	assert.NotEqual(t, "routine", got.Sources["effort"], "what it left out is inherited from where a click's would be")
	assert.Empty(t, r.opener.opened(), "a preview opens nothing")

	inWorktree, err := previews.PreviewRun(t.Context(), autopilot.RunRequest{
		ItemID: r.pr, Step: spec.AgentStep{Where: catalog.WhereTerminal, Worktree: new(true)},
	})
	require.NoError(t, err)
	require.NotNil(t, inWorktree.Worktree, "a run in a worktree shows the worktree it would use")
	assert.Equal(t, "pr-7", inWorktree.Worktree.Name)
	assert.Equal(t, "~/worktrees/pr-7", inWorktree.Worktree.Path)
	assert.Empty(t, r.maker.asked(), "a preview makes no worktree")
}

func TestARoutinesPreviewSaysWhatKeepsItFromPlanning(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)
	previews := runPreviews{deps: r.deps}

	_, err := previews.PreviewRun(t.Context(), autopilot.RunRequest{
		ItemID: r.pr, Step: spec.AgentStep{Where: catalog.WhereBackground},
	})
	require.ErrorIs(t, err, autopilot.ErrNoAgentStep)
	assert.Equal(t, "This copy can't run an agent in the background.", uierr.Message(err),
		"a copy with no background runner previews no background step")

	_, err = previews.PreviewRun(t.Context(), autopilot.RunRequest{
		ItemID: r.pr, Step: spec.AgentStep{Where: catalog.WhereTerminal, Prompt: spec.PromptRef{Nickname: "Gone"}},
	})
	require.ErrorIs(t, err, prompts.ErrPromptNotFound, "a prompt the library lost is the preview's error")

	_, err = previews.PreviewRun(t.Context(), autopilot.RunRequest{
		ItemID: r.orphan, Step: spec.AgentStep{Where: catalog.WhereTerminal},
	})
	require.Error(t, err, "a repository with no clone has nowhere to plan the run")
}

// terminalApp has the rig's readiness check Terminal, which this Mac always has, as the
// terminal launches open in.
func terminalApp(t *testing.T, r *routineRig) {
	t.Helper()

	_, err := r.settings.SetTerminal(t.Context(), terminal.TerminalApp)
	require.NoError(t, err)

	r.deps.registry = terminal.NewRegistry(&scriptOpener{id: terminal.TerminalApp})
}

// holdsOf are the rig's holds, its checks over the rig's own deps.
func holdsOf(r *routineRig) agentHolds {
	return agentHolds{deps: r.deps, checks: newReadyChecks(r.deps)}
}

func TestAStartThisMacIsReadyForGoesThrough(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)
	terminalApp(t, r)

	blocker, words, err := holdsOf(r).Hold(t.Context(), r.job(r.pr, "acme-org/example-repo", spec.AgentStep{
		Agent: claudeID, Profile: workProfile,
	}))
	require.NoError(t, err)
	assert.Empty(t, blocker, "its agent, profile, terminal, and clone are all there")
	assert.Empty(t, words)

	blocker, _, err = holdsOf(r).Hold(t.Context(), autopilot.Job{Step: spec.Step{Pin: &spec.Empty{}}})
	require.NoError(t, err)
	assert.Empty(t, blocker, "a step with no agent has nothing on this Mac to wait for")
}

func TestAStartWaitsForWhatThisMacLacks(t *testing.T) {
	t.Parallel()

	r := newRoutineRigWith(t, routineRigConfig{runs: claudeSays(`{"loggedIn": false}`, 1)})
	terminalApp(t, r)

	blocker, words, err := holdsOf(r).Hold(t.Context(), r.job(r.pr, "acme-org/example-repo", spec.AgentStep{
		Agent: claudeID, Profile: workProfile,
	}))
	require.NoError(t, err)
	assert.Equal(t, autopilot.BlockNotReady, blocker, "a profile signed out holds the start")
	assert.Equal(t, "Not ready on this Mac: Profile work (Claude) is signed out. "+
		"Run `CLAUDE_CONFIG_DIR=~/.claude_work claude auth login`, then Check again.", words, "with its fix")

	blocker, words, err = holdsOf(r).Hold(t.Context(), r.job(r.pr, "acme-org/example-repo", spec.AgentStep{
		Prompt: spec.PromptRef{Nickname: "Gone"},
	}))
	require.NoError(t, err)
	assert.Equal(t, autopilot.BlockNotReady, blocker)
	assert.Equal(t, "Not ready on this Mac: Its prompt, “Gone”, isn't in your library any more.", words,
		"a prompt the library lost holds it too, saying which")

	blocker, words, err = holdsOf(r).Hold(t.Context(), r.job(r.pr, "acme-org/example-repo", spec.AgentStep{
		Agent: claudeID, Profile: "nobody",
	}))
	require.NoError(t, err)
	assert.Equal(t, autopilot.BlockNotReady, blocker, "a profile this Mac doesn't have can't resolve")
	assert.Contains(t, words, "Not ready on this Mac: ")
}

func TestAStartInATerminalThisMacCantOpenWaits(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)
	// The terminal chosen is iTerm2, and this build opens Terminal alone.
	r.deps.registry = terminal.NewRegistry(&scriptOpener{id: terminal.TerminalApp})

	blocker, words, err := holdsOf(r).Hold(t.Context(), r.job(r.pr, "acme-org/example-repo", spec.AgentStep{}))
	require.NoError(t, err)
	assert.Equal(t, autopilot.BlockNotReady, blocker)
	assert.Contains(t, words, "isn't one this Mac can open. Choose another terminal in Settings → Launching.")

	background := r.job(r.pr, "acme-org/example-repo", spec.AgentStep{})
	background.Step.Agent.Where = catalog.WhereBackground

	blocker, _, err = holdsOf(r).Hold(t.Context(), background)
	require.NoError(t, err)
	assert.Empty(t, blocker, "a run in the background needs no terminal")
}

func TestABackgroundStartWaitsForAnAgentTooOldForIt(t *testing.T) {
	t.Parallel()

	r := newRoutineRigWith(t, routineRigConfig{runs: map[string]testutil.FakeRun{claudeID: {
		Stdout: "1.0.0 (Claude Code)\n" + `{"loggedIn": true, "authMethod": "claude.ai"}`,
	}}})

	h, err := agent.HeadlessOf(agent.Claude)
	require.NoError(t, err)

	background := r.job(r.pr, "acme-org/example-repo", spec.AgentStep{Agent: claudeID})
	background.Step.Agent.Where = catalog.WhereBackground

	blocker, words, err := holdsOf(r).Hold(t.Context(), background)
	require.NoError(t, err)
	assert.Equal(t, autopilot.BlockNotReady, blocker)
	assert.Equal(t, "Not ready on this Mac: Claude 1.0.0 is older than "+h.MinVersion+
		", which runs agents in the background. Update it, then Check again.", words)

	terminalApp(t, r)

	blocker, _, err = holdsOf(r).Hold(t.Context(), r.job(r.pr, "acme-org/example-repo", spec.AgentStep{Agent: claudeID}))
	require.NoError(t, err)
	assert.Empty(t, blocker, "in the terminal, any version runs")
}

func TestAStartWaitsOutItsProvidersOutageThenItsClone(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)
	terminalApp(t, r)

	rules := svcstatus.Rules{Every: 30 * time.Minute, Watch: statusprefs.Defaults()}
	r.deps.status = svcstatus.NewService(svcstatus.Config{
		Fetcher: claudeDown{}, Rules: func(context.Context) svcstatus.Rules { return rules },
	})
	r.deps.status.CheckNow(t.Context())

	held := holdsOf(r)
	held.prefs = holdPrefs(true)

	blocker, words, err := held.Hold(t.Context(), r.job(r.pr, "acme-org/example-repo", spec.AgentStep{Agent: claudeID}))
	require.NoError(t, err)
	assert.Equal(t, autopilot.BlockOutage, blocker, "Claude Code's outage queues its start")
	assert.Equal(t, "Queued: Claude reports a major outage; starts once it's back.", words)

	blocker, _, err = held.Hold(t.Context(), r.job(r.pr, "acme-org/example-repo", spec.AgentStep{Agent: string(agent.Codex)}))
	require.NoError(t, err)
	assert.NotEqual(t, autopilot.BlockOutage, blocker, "another provider's agent isn't held by it")
}

func TestAStartOnARepositoryWithNoCloneWaitsForOne(t *testing.T) {
	t.Parallel()

	r := newRoutineRig(t)
	terminalApp(t, r)

	blocker, words, err := holdsOf(r).Hold(t.Context(), r.job(r.orphan, "acme-org/no-clone", spec.AgentStep{}))
	require.NoError(t, err)
	assert.Equal(t, autopilot.BlockNoClone, blocker)
	assert.Equal(t, "No local clone of acme-org/no-clone.", words)
}

// The tests above hold these to the launcher's own types.
var _ launcher.RunFolders = busyFolders{}
