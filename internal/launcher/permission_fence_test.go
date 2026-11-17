package launcher

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/profiles"
)

// modeAcceptEdits is the profile mode the fence tests start from.
const modeAcceptEdits = "acceptEdits"

// testRunDir is a background run's own folder.
func testRunDir() string {
	return absPath("data", "agent-runs", "00000000-0000-4000-8000-000000000007")
}

func TestABackgroundRunGetsItsFencesAccess(t *testing.T) {
	t.Parallel()

	claude := lookupAgent(t, agent.Claude)
	run := testRunDir()

	tests := []struct {
		name     string
		preset   fence.Preset
		trusted  bool
		override bool
		mode     string
		plan     bool
		want     agent.Access
		summary  string
	}{
		{
			name: "a fixed preset ignores full access", preset: fence.EditTestPush, trusted: true, mode: profiles.PermissionFull,
			want: agent.FenceAccess(fence.EditTestPush, agent.Access{}).InRun(run), summary: "fenced: Edit, test, and push to the PR's branch",
		},
		{
			name: "a fixed preset ignores plan mode", preset: fence.EditTest, trusted: true, plan: true,
			want: agent.FenceAccess(fence.EditTest, agent.Access{}).InRun(run), summary: "fenced: Edit and test, no push",
		},
		{
			name: "a profile with no mode still works", preset: fence.GitHubTasks, trusted: true,
			want: agent.FenceAccess(fence.GitHubTasks, agent.Access{}).InRun(run), summary: "fenced: GitHub tasks only",
		},
		{
			name: "an outside contributor reads only, whatever the fence", preset: fence.EditTestPush, mode: profiles.PermissionFull,
			want:    agent.FenceAccess(fence.ReadOnly, agent.Access{}).InRun(run),
			summary: "fenced: Read only" + summaryOutside,
		},
		{
			name: "the profile's own preset is an outside contributor's read only too", preset: fence.Profile, mode: profiles.PermissionFull,
			want:    agent.FenceAccess(fence.ReadOnly, agent.Access{}).InRun(run),
			summary: "fenced: Read only" + summaryOutside,
		},
		{
			name: "the person's override lifts it to the fence", preset: fence.EditTest, override: true,
			want: agent.FenceAccess(fence.EditTest, agent.Access{}).InRun(run), summary: "fenced: Edit and test, no push",
		},
		{
			name: "the profile's own preset is a click's access", preset: fence.Profile, trusted: true, mode: profiles.PermissionFull,
			want:    agent.FenceAccess(fence.Profile, agent.Access{Kind: agent.AccessFull}).InRun(run),
			summary: "fenced: The profile's own permissions, " + summaryFull,
		},
		{
			name: "the profile's own preset keeps its mode", preset: fence.Profile, trusted: true, mode: modeAcceptEdits,
			want:    agent.FenceAccess(fence.Profile, agent.Access{Kind: agent.AccessMode, Mode: modeAcceptEdits}).InRun(run),
			summary: "fenced: The profile's own permissions, " + summaryModePrefix + modeAcceptEdits,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := fullProfile()
			p.PermissionMode, p.PlanMode = tt.mode, tt.plan

			got, err := resolvePermission(claude, tt.trusted, tt.override, p, &Fence{Preset: tt.preset, RunDir: run})
			require.NoError(t, err)
			assert.Equal(t, tt.want, got.access)
			assert.Equal(t, tt.summary, got.summary)
		})
	}
}

func TestAFenceItsAgentCantKeepIsRefused(t *testing.T) {
	t.Parallel()

	p := fullProfile()
	p.Agent, p.PermissionMode = agent.Codex, ""

	_, err := resolvePermission(lookupAgent(t, agent.Codex), true, false, p, &Fence{Preset: fence.GitHubTasks, RunDir: testRunDir()})
	require.ErrorIs(t, err, ErrFenceNotKept, "Codex can't keep GitHub tasks only")

	_, err = resolvePermission(lookupAgent(t, agent.Codex), true, false, p, &Fence{Preset: fence.EditTest, RunDir: testRunDir()})
	require.NoError(t, err, "but keeps Edit and test")

	_, err = resolvePermission(lookupAgent(t, agent.Claude), true, false, fullProfile(), &Fence{Preset: fence.EditTest})
	require.ErrorIs(t, err, ErrFenceNotKept, "a fence needs its run's folder")

	_, err = resolvePermission(lookupAgent(t, agent.Claude), true, false, fullProfile(), &Fence{Preset: "everything", RunDir: testRunDir()})
	require.ErrorIs(t, err, ErrFenceNotKept)

	bad := fullProfile()
	bad.PermissionMode = "yolo"

	_, err = resolvePermission(lookupAgent(t, agent.Claude), true, false, bad, &Fence{Preset: fence.EditTest, RunDir: testRunDir()})
	require.ErrorIs(t, err, ErrInvalidPermissionMode, "a profile's unknown mode is an error under a fence too")
}

func TestAFencedPlanTakesNoExtraDirectories(t *testing.T) {
	t.Parallel()

	req := baseRequest()
	req.Profile.AddDirs = []string{absPath("shared")}
	req.Fence = &Fence{Preset: fence.EditTestPush, RunDir: testRunDir()}

	plan, err := BuildPlan(req)
	require.NoError(t, err)

	for _, arg := range plan.Args {
		assert.NotContains(t, arg, "--add-dir", "a profile's --add-dir never reaches a background run")
	}

	assert.Contains(t, plan.Args, "--settings")
	assert.Contains(t, plan.Args, testRunDir()+"/"+agent.FenceSettingsName)
	assert.NotContains(t, plan.Args, "--dangerously-skip-permissions", "the profile's full access doesn't loosen it")
	assert.Equal(t, "fenced: Edit, test, and push to the PR's branch", plan.Permission)

	req.Profile.ExtraArgs = []string{"--plugin-url", "https://example.com/p.zip"}

	_, err = BuildPlan(req)
	require.ErrorIs(t, err, agent.ErrReservedFlag, "--plugin-url as an extra flag is refused")

	req.Profile.ExtraArgs = []string{"--setting-sources", "user,project"}

	_, err = BuildPlan(req)
	require.ErrorIs(t, err, agent.ErrReservedFlag)

	click := baseRequest()
	click.Profile.AddDirs = []string{absPath("shared")}
	click.Profile.ExtraArgs = []string{"--plugin-url", "https://example.com/p.zip"}

	plan, err = BuildPlan(click)
	require.NoError(t, err, "a click keeps today's lists")
	assert.Contains(t, plan.Args, "--add-dir="+absPath("shared"))
}
