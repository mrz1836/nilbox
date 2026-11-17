package launcher

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/profiles"
)

// Codex in the background: planned and fenced as Claude Code is, its fence on its command
// line rather than in a settings file, never trusted in the user's config, and taken over
// with `codex resume`.

// codexThread is a Codex run's thread, as its stream named it.
const codexThread = "01a1e670-d359-7720-8a38-a2402fc62c90"

// testProgram is the app's own executable, which a fence hook runs.
const testProgram = "/Applications/App.app/Contents/MacOS/app"

// repoDefaults are launch defaults a test sets directly.
type repoDefaults []launchdefaults.Defaults

func (d repoDefaults) All(context.Context) ([]launchdefaults.Defaults, error) {
	return d, nil
}

// codexConfig is the default Codex profile's config file, which a trust grant writes.
func (h *launchHarness) codexConfig() string {
	return filepath.Join(h.home, ".codex", "config.toml")
}

func TestACodexBackgroundRunIsFencedOnItsCommandLine(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.paths.AgentRuns = t.TempDir()

	req := withPreset(h.trustedPR, fence.EditTestPush)
	req.Agent = agent.Codex

	bp, hold := h.planBackground(t, req)
	require.Nil(t, hold)
	require.Equal(t, agent.Codex, bp.Plan.Agent)
	assert.Equal(t, []string{"exec", "--json", "--ignore-user-config", "--ignore-rules"}, bp.Plan.Args[:4])
	assert.NotContains(t, bp.Plan.Args, "--dangerously-bypass-hook-trust", "no hook before its fence is worked out")
	assert.True(t, strings.HasPrefix(bp.Command, "codex exec --json"), bp.Command)

	made := t.TempDir()
	fenced, run, err := NewBackgrounds(h.service()).Fenced(bp, FenceIn{
		Worktree: made, Program: testProgram, Env: map[string]string{"HOME": h.home},
	})
	require.NoError(t, err)

	args := strings.Join(fenced.Plan.Args, " ")
	assert.Contains(t, args, "--sandbox workspace-write")
	assert.Contains(t, args, "--dangerously-bypass-hook-trust")
	assert.Contains(t, args, "'"+testProgram+"' '--fence-hook-v1="+bp.Folder+"'", "its hook is the app in its hook mode")
	assert.Contains(t, args, "sandbox_workspace_write.network_access=true", "the one push needs the network")
	assert.Contains(t, args, "--output-schema "+filepath.Join(bp.Folder, agent.ReportSchemaName))
	assert.True(t, run.Fence.Policy.ShellReads, "Codex reads through its shell")
	assert.Equal(t, []string{"--", "-"}, fenced.Plan.Args[len(fenced.Plan.Args)-2:], "the prompt on stdin")

	names := make([]string, 0, len(run.Files))
	for _, f := range run.Files {
		names = append(names, f.Name)
	}

	assert.Equal(t, []string{agent.FenceSettingsName, agent.ReportSchemaName}, names)

	require.NoError(t, run.Write(bp.Folder))
	assert.FileExists(t, filepath.Join(bp.Folder, agent.ReportSchemaName))

	require.NoError(t, NewBackgrounds(h.service()).Trust(t.Context(), fenced))
	assert.NoFileExists(t, h.codexConfig(), "a Codex run reads no trust, so none is granted: a branch's config never loads")
}

func TestSwitchingARepositorysLaunchDefaultToCodexMovesItsInheritRoutines(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.paths.AgentRuns = t.TempDir()
	req := withPreset(h.trustedPR, fence.EditTest)

	bp, _ := h.planBackground(t, req)
	assert.Equal(t, agent.Claude, bp.Plan.Agent, "Inherit: the app's default agent")

	h.cfg.Defaults = repoDefaults{{Owner: "acme-org", Name: exampleRepo, Agent: agent.Codex}}

	bp, _ = h.planBackground(t, req)
	assert.Equal(t, agent.Codex, bp.Plan.Agent, "Inherit resolves as a click does: the repository's launch default")
	assert.Equal(t, "Codex", bp.Resolution.Agent.Name)
	assert.Equal(t, "exec", bp.Plan.Args[0])

	h.cfg.Defaults = repoDefaults{{Owner: "acme-org", Agent: agent.Codex}}

	bp, _ = h.planBackground(t, req)
	assert.Equal(t, agent.Codex, bp.Plan.Agent, "or the org's")

	_, _, err := NewBackgrounds(h.service()).Plan(t.Context(), withPreset(h.trustedPR, fence.GitHubTasks))
	require.ErrorIs(t, err, ErrFenceNotKept, "a preset Codex can't keep never runs weaker")
}

// codexRun sets the harness up with a Codex run's ended session on item, in its worktree,
// under preset, with the run's folder holding its fence's settings and policy.
func (h *launchHarness) codexRun(t *testing.T, itemID int64, preset fence.Preset) (*fakeTakeOvers, fence.Policy) {
	t.Helper()

	_, _ = h.launch(t, LaunchRequest{ItemID: itemID, Agent: agent.Codex})
	first := h.ledger.all()[0]

	runDir := filepath.Join(h.paths.AgentRuns, testSessionID)

	built, err := fence.Build(fence.Spec{
		Preset: preset, Worktree: first.Dir, GitDir: h.clone + "/.git/worktrees/pr-7", CommonDir: h.clone + "/.git",
		Repo: "acme-org/" + exampleRepo, Number: 7, Branch: "feature-branch", Remote: "origin", RunDir: runDir, Home: h.home,
		ShellReads: true,
	})
	require.NoError(t, err)
	require.NoError(t, fence.WriteRun(runDir, built, fence.File{Name: agent.FenceSettingsName, Data: []byte("{}\n")}))

	h.ledger.resumable = map[string]ResumableSession{testSessionID: {
		ID: first.ID, AgentSessionID: codexThread, Agent: agent.Codex, Profile: profiles.DefaultName, ItemID: itemID,
		Dir: first.Dir, Worktree: first.Worktree, Live: true, RunID: 42, Fence: &Fence{Preset: preset, RunDir: runDir},
	}}

	takes := &fakeTakeOvers{ledger: h.ledger, sessionID: testSessionID}
	h.cfg.TakeOvers = takes
	h.cfg.NewID = func() string { return takenOverID }

	require.NoError(t, os.MkdirAll(filepath.Dir(h.codexConfig()), 0o700))
	require.NoError(t, os.WriteFile(h.codexConfig(), []byte("model = \"gpt-6-sol\"\n"), 0o600),
		"start from a profile that trusts nothing")

	return takes, built.Policy
}

func TestTakingOverACodexRunResumesItsThreadUnderItsSandbox(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	takes, policy := h.codexRun(t, h.trustedPR, fence.EditTestPush)

	res, err := h.service().TakeOver(t.Context(), TakeOverRequest{SessionID: testSessionID})
	require.NoError(t, err)
	require.Nil(t, res.Hold)
	assert.Equal(t, []int64{42}, takes.runs())
	assert.Equal(t, agent.Codex, res.Agent)

	script := scriptOf(t, res)
	assert.Contains(t, script, quoted("resume", codexThread), "the thread its stream named")
	assert.Contains(t, script, quoted("--sandbox", "workspace-write"), "a resume keeps no sandbox of its own")
	assert.Contains(t, script, quoted("--ask-for-approval", "on-request"), "what the sandbox refuses is asked")
	assert.Contains(t, script, policy.GitDirs[0], "its commits still work, from the policy its folder keeps")
	assert.Contains(t, script, "no longer unattended", "the run's note persists, so the first message says so")
	assert.NotContains(t, script, "network_access", "the network is off: a push asks first")
	assert.NotContains(t, script, "--dangerously-bypass-hook-trust", "no hook bypass in the terminal")
	assert.NotContains(t, script, "--dangerously-bypass-approvals-and-sandbox", "the profile's full access isn't taken")

	config, err := os.ReadFile(h.codexConfig())
	require.NoError(t, err)
	assert.NotContains(t, string(config), "trust_level", "nothing is trusted for it: Codex asks the user, who answers now")

	started := h.ledger.all()
	assert.Equal(t, codexThread, started[len(started)-1].AgentSessionID, "it carries the run's thread on")
}

func TestTakingOverACodexRunWithTheProfilesPermissionsStillSaysSomeoneAnswers(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.codexRun(t, h.trustedPR, fence.ReadOnly)

	res, err := h.service().TakeOver(t.Context(), TakeOverRequest{SessionID: testSessionID, Profile: true})
	require.NoError(t, err)

	script := scriptOf(t, res)
	assert.Contains(t, script, "--dangerously-bypass-approvals-and-sandbox", "the profile's own full access, ticked")
	assert.Contains(t, script, "no longer unattended")

	config, err := os.ReadFile(h.codexConfig())
	require.NoError(t, err)
	assert.Contains(t, string(config), "trust_level", "a click's permissions trust the folder, as a click does")

	args := strings.Fields(script)
	assert.False(t, slices.Contains(args, "--ignore-user-config"), "an interactive resume reads the user's config")
}
