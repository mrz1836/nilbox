package launcher

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// readOnly asks for a Read only background run on id.
func readOnly(id int64) BackgroundRequest {
	return withPreset(id, fence.ReadOnly)
}

// withPreset asks for a background run on id under preset.
func withPreset(id int64, preset fence.Preset) BackgroundRequest {
	return BackgroundRequest{
		ItemID: id, Preset: preset, BudgetUSD: 0.5,
		Schema: `{"type":"object"}`, SystemNote: "You run unattended.",
	}
}

// planBackground plans a background run, failing the test on an error.
func (h *launchHarness) planBackground(t *testing.T, req BackgroundRequest) (BackgroundPlan, *Hold) {
	t.Helper()

	bp, hold, err := NewBackgrounds(h.service()).Plan(t.Context(), req)
	require.NoError(t, err)

	return bp, hold
}

// fakeRunFolders names the app's own runs in each folder.
type fakeRunFolders struct {
	runs map[string][]RunningSession
}

func (f fakeRunFolders) RunsIn(_ context.Context, dir string) []RunningSession {
	for d, runs := range f.runs {
		if paths.Same(d, dir) {
			return runs
		}
	}

	return []RunningSession{}
}

// fencedRequest is baseRequest under the Read only fence, in a run's folder.
func fencedRequest() Request {
	req := baseRequest()
	req.SessionID, req.Fence = testSessionID, &Fence{Preset: fence.ReadOnly, RunDir: absPath("agent-runs", testSessionID)}

	return req
}

func TestPlanBackgroundRunsFencedInTheWorktreeWithNoTerminal(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	worktreeOff := false

	req := readOnly(h.trustedPR)
	req.Worktree = &worktreeOff

	bp, hold := h.planBackground(t, req)
	require.Nil(t, hold)

	wt := worktreeOf(WorktreeRequest{Clone: h.clone, Kind: "pr", Number: 7})
	assert.Equal(t, wt, bp.Plan.Dir, "a background run starts in the item's worktree, whatever the resolution says")
	assert.Equal(t, h.clone, bp.Clone.Dir)
	assert.Equal(t, filepath.Join(h.paths.AgentRuns, testSessionID), bp.Folder, "its folder is named by its session")
	assert.Equal(t, testSessionID, bp.SessionID)
	assert.Equal(t, fence.ReadOnly, bp.Preset)
	assert.True(t, bp.ReadOnly)
	assert.Empty(t, h.iterm.opened, "nothing opens in a terminal")
	assert.Empty(t, h.maker.ensures(), "the worktree is only planned")

	assert.Equal(t, []string{
		"-p", "--output-format", "stream-json", "--verbose", "--permission-prompts", "none",
		"--session-id=" + testSessionID, "--restricted", "--tools", "Read,Grep,Glob", "--permission-mode", "dontAsk",
		"--strict-mcp-config", "--settings", filepath.Join(bp.Folder, "settings.json"), "--max-budget-usd", "0.50",
		"--json-schema", `{"type":"object"}`, "--append-system-prompt", "You run unattended.",
		"--system-prompt-snapshot", "off",
	}, bp.Plan.Args, "the default profile's full access never reaches a background run: the fence's flags do")
	assert.Equal(t, "fenced: Read only", bp.Plan.Permission)
	assert.Equal(t, sessionEnv("CLAUDE_CONFIG_DIR", "ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"), bp.Plan.Unset,
		"the default profile unsets the config folder, no API key signs it in unless the user lets it, and no "+
			"session's variables make it another's child (AgentEnv)")
	assert.True(t, strings.HasSuffix(bp.Command, "< prompt.md"), bp.Command)
	assert.NotEmpty(t, bp.Prompt)
}

func TestFencedWorksTheFenceOutForTheWorktreeMade(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.paths.AgentRuns = t.TempDir()

	bp, hold := h.planBackground(t, withPreset(h.trustedPR, fence.EditTest))
	require.Nil(t, hold)

	made := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(made, "go.mod"), []byte("module example.com/x\n"), 0o600))

	fenced, run, err := NewBackgrounds(h.service()).Fenced(bp, FenceIn{
		Worktree: made, Program: "/Applications/App.app/Contents/MacOS/app", Env: map[string]string{"HOME": h.home},
		Deny: []string{"make release"},
	})
	require.NoError(t, err)

	assert.Equal(t, made, fenced.Plan.Dir, "the run starts in the worktree made")
	assert.Equal(t, fence.EditTest, run.Fence.Policy.Preset)
	assert.Equal(t, paths.Resolved(made), run.Fence.Policy.Worktree)
	assert.NotNil(t, run.Fence.Sandbox, "an edit preset runs its commands in the sandbox")
	assert.Contains(t, run.Fence.Policy.Tools, "go", "the worktree's tools are read from its files")
	assert.Contains(t, run.Fence.Policy.Deny, "make release", "the routine's own denials")

	note := fenced.Plan.Args[slices.Index(fenced.Plan.Args, "--append-system-prompt")+1]
	assert.True(t, strings.HasPrefix(note, "You run unattended.\n\nYour tools are fenced: Edit and test, no push."), note)
	assert.Contains(t, note, run.Fence.Policy.Canary.Command(), "the agent is told to check its sandbox first")

	require.NoError(t, run.Write(bp.Folder))

	var settings map[string]any

	body, err := os.ReadFile(filepath.Join(bp.Folder, agent.FenceSettingsName))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(body, &settings))
	assert.Equal(t, false, settings["includeCoAuthoredBy"], "attribution is off in the run's own settings")

	_, _, err = NewBackgrounds(h.service()).Fenced(bp, FenceIn{Worktree: "relative", Program: "/app"})
	require.ErrorIs(t, err, ErrUnfenced)
}

func TestBuildBackgroundLeavesOutTheProfilesExtraDirectories(t *testing.T) {
	t.Parallel()

	req := fencedRequest()
	req.Profile.AddDirs = []string{absPath("elsewhere")}

	plan, err := BuildBackground(req, background{})
	require.NoError(t, err)
	assert.False(t, slices.ContainsFunc(plan.Args, func(a string) bool { return strings.Contains(a, "elsewhere") }),
		"a profile's --add-dir never reaches a background run: %q", plan.Args)
	assert.NotContains(t, plan.Args, "--dangerously-skip-permissions", "the profile's full access doesn't loosen the fence")

	unfenced := fencedRequest()
	unfenced.Fence = nil

	_, err = BuildBackground(unfenced, background{})
	require.ErrorIs(t, err, ErrUnfenced, "a background run is always fenced")
}

func TestBuildBackgroundRefusesAPluginFlag(t *testing.T) {
	t.Parallel()

	for _, extra := range [][]string{
		{"--plugin-url=https://example.com/p.zip"},
		{"--plugin-dir", absPath("plugins")},
		{"--mcp-config", absPath("m.json")},
		{"--add-dir=" + absPath("x")},
		{"--max-budget-usd", "100"},
	} {
		req := fencedRequest()
		req.Profile.ExtraArgs = extra

		_, err := BuildBackground(req, background{})
		require.ErrorIs(t, err, ErrConflictingExtraArg, extra)
		require.ErrorIs(t, err, agent.ErrReservedFlag, extra)
	}
}

func TestBuildBackgroundRefusesWhatNoRunMayStartWith(t *testing.T) {
	t.Parallel()

	relative := fencedRequest()
	relative.Dir = "pr-7"

	_, err := BuildBackground(relative, background{})
	require.ErrorIs(t, err, ErrRelativePath)

	looser := fencedRequest()
	looser.Profile.ExtraArgs = []string{"--dangerously-skip-permissions"}

	_, err = BuildBackground(looser, background{})
	require.ErrorIs(t, err, ErrConflictingExtraArg, "a profile's flag never loosens the fence")

	nul := fencedRequest()
	nul.Dir = absPath("pr-7\x00elsewhere")

	_, err = BuildBackground(nul, background{})
	require.ErrorIs(t, err, ErrNULByte)
}

func TestPlanBackgroundOnAnOutsideContributorsItemReadsOnly(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)

	req := withPreset(h.outsidePR, fence.EditTestPush)
	req.FullAccessOverride = true

	bp, hold := h.planBackground(t, req)
	require.Nil(t, hold)
	assert.Equal(t, fence.ReadOnly, bp.Preset, "an automatic run never lifts the outside-contributor rule")
	assert.Equal(t, "fenced: Read only (outside contributor)", bp.Plan.Permission)
}

func TestPlanBackgroundWaitsForARunOrASessionInTheWorktree(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	wt := worktreeOf(WorktreeRequest{Clone: h.clone, Kind: "pr", Number: 7})

	h.cfg.RunFolders = fakeRunFolders{runs: map[string][]RunningSession{
		wt: {{SessionID: "run-session", Agent: agent.Claude, Background: true}},
	}}

	_, hold := h.planBackground(t, readOnly(h.trustedPR))
	require.NotNil(t, hold)
	assert.Equal(t, HoldRunning, hold.Reason, "an agent working in the worktree holds the run back")

	h.cfg.RunFolders = nil
	h.ledger.setRunning(runningAt{dir: wt, session: RunningSession{SessionID: "terminal", Agent: agent.Claude}})

	_, hold = h.planBackground(t, readOnly(h.trustedPR))
	require.NotNil(t, hold)
	assert.Equal(t, HoldRunning, hold.Reason, "so does a session the user runs there")
}

func TestPlanBackgroundHoldsForAgentFilesUntilAPersonConfirmsTheHead(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	files := &fakeFiles{checks: map[int64]*sensitive.Check{
		h.trustedPR: checkOf(holdHeadA, true, ".claude/settings.json"),
	}}
	h.cfg.Files = files

	_, hold := h.planBackground(t, readOnly(h.trustedPR))
	require.NotNil(t, hold, "a trusted pull request's files are checked too, unattended")
	assert.Equal(t, HoldAgentFiles, hold.Reason)
	assert.Equal(t, []int64{h.trustedPR}, files.wide)

	req := readOnly(h.trustedPR)
	req.Routine = &RoutineLaunch{Name: "Review", Confirmed: []string{hold.Key}}

	bp, hold := h.planBackground(t, req)
	require.Nil(t, hold, "a person's Run on <commit> lets that head through")
	assert.Equal(t, strings.ToLower(holdHeadA), bp.Worktree.Head, "the worktree is made at the head checked")
}

func TestPreviewBackgroundShowsTheBackgroundFlagsAndHoldsNothing(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	wt := worktreeOf(WorktreeRequest{Clone: h.clone, Kind: "pr", Number: 7})
	h.cfg.RunFolders = fakeRunFolders{runs: map[string][]RunningSession{wt: {{SessionID: "x", Background: true}}}}

	preview, err := NewBackgrounds(h.service()).Preview(t.Context(), readOnly(h.trustedPR))
	require.NoError(t, err)
	assert.Contains(t, preview.Command, "--restricted --tools Read,Grep,Glob")
	assert.Contains(t, preview.Command, "-p --output-format stream-json")
	assert.Empty(t, preview.Terminal)
	assert.Equal(t, "fenced: Read only", preview.Permission)
	require.NotNil(t, preview.Worktree)
}

func TestALaunchIsHeldForABackgroundRunWithSessionsUnfollowed(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	wt := worktreeOf(WorktreeRequest{Clone: h.clone, Kind: "pr", Number: 7})
	h.cfg.Ledger = nil
	h.cfg.RunFolders = fakeRunFolders{runs: map[string][]RunningSession{
		wt: {{SessionID: "run-session", Agent: agent.Claude, Background: true}},
	}}

	on := true
	res, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR, Worktree: &on})
	require.NoError(t, err)
	require.NotNil(t, res.Hold, "the app's own run holds a click in its worktree")

	res, err = h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR, Worktree: &on, Alongside: res.Hold.Key})
	require.NoError(t, err)
	require.NotNil(t, res.Hold, "never started beside it")
	assert.Empty(t, h.iterm.opened)
}

func TestMadeHoldsARoutinesRunOnAWorktreeWithChanges(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	b := NewBackgrounds(h.service())

	bp, hold := h.planBackground(t, readOnly(h.trustedPR))
	require.Nil(t, hold)

	routine := &RoutineLaunch{Name: "Review"}
	clean := Worktree{Path: bp.Plan.Dir, Head: holdHeadA, Dirty: 0}
	dirty := Worktree{Path: bp.Plan.Dir, Head: holdHeadA, Dirty: 3}

	assert.Nil(t, b.Made(bp, clean, routine), "a clean worktree holds nothing")
	assert.Nil(t, b.Made(bp, dirty, nil), "a person's run is never held for changes in its worktree")

	held := b.Made(bp, dirty, routine)
	require.NotNil(t, held, "a routine never runs over changes no one confirmed")
	assert.Equal(t, HoldDirty, held.Reason)
	assert.Equal(t, 3, held.Dirty)
	assert.Equal(t, "pr-7", held.Worktree)
	assert.Equal(t, paths.Tilde(bp.Plan.Dir, h.home), held.Dir)

	routine.Confirmed = []string{held.Key}
	assert.Nil(t, b.Made(bp, dirty, routine), "the user's confirmation of exactly those changes lets it through")
	assert.NotNil(t, b.Made(bp, Worktree{Path: bp.Plan.Dir, Head: holdHeadA, Dirty: 4}, routine),
		"but not of changes it never saw")
}

func TestPlanBackgroundSaysWhyARunCantBePlanned(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		edit func(*launchHarness) BackgroundRequest
		is   error
		says string
	}{
		"an item no longer in the inbox": {
			edit: func(*launchHarness) BackgroundRequest { return readOnly(404) }, is: item.ErrNotFound,
		},
		"a profile that's gone": {
			edit: func(h *launchHarness) BackgroundRequest {
				req := readOnly(h.trustedPR)
				req.Profile = "nope"

				return req
			},
			is: profiles.ErrProfileNotFound,
		},
		"a repository with no clone": {
			edit: func(h *launchHarness) BackgroundRequest { return readOnly(h.orphanPR) }, is: localrepos.ErrCloneNotFound,
		},
		"an agent that isn't installed": {
			edit: func(h *launchHarness) BackgroundRequest {
				h.cfg.Finder = testutil.Finder{Err: errFakeNoClaude}

				return readOnly(h.trustedPR)
			},
			is: errFakeNoClaude, says: "Claude isn't installed (claude wasn't found).",
		},
		"no worktrees to make": {
			edit: func(h *launchHarness) BackgroundRequest {
				h.cfg.Worktrees = nil

				return readOnly(h.trustedPR)
			},
			is: ErrNoWorktrees,
		},
		"a worktree that can't be planned": {
			edit: func(h *launchHarness) BackgroundRequest {
				h.maker.planErr = errFakeOpen

				return readOnly(h.trustedPR)
			},
			is: errFakeOpen,
		},
		"files that can't be checked": {
			edit: func(h *launchHarness) BackgroundRequest {
				h.cfg.Files = &fakeFiles{err: errFakeFiles}

				return readOnly(h.trustedPR)
			},
			is: errFakeFiles,
		},
	} {
		h := newLaunchHarness(t)
		req := tc.edit(h)

		_, hold, err := NewBackgrounds(h.service()).Plan(t.Context(), req)
		require.Error(t, err, name)
		assert.Nil(t, hold, name)

		if tc.is != nil {
			require.ErrorIs(t, err, tc.is, name)
		}

		if tc.says != "" {
			assert.Equal(t, tc.says, uierr.Message(err), name)
		}
	}

	h := newLaunchHarness(t)

	_, err := NewBackgrounds(h.service()).Preview(t.Context(), readOnly(404))
	require.ErrorIs(t, err, item.ErrNotFound, "a preview of an item that's gone says so too")
}

// failingTruster is an agent's config that can't be written.
type failingTruster struct{}

func (failingTruster) Trust(context.Context, string, []string) error { return errFakeSettings }

func TestTrustTrustsTheWorktreeOnlyForAnAgentThatReadsIt(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	b := NewBackgrounds(h.service())

	bp, hold := h.planBackground(t, readOnly(h.trustedPR))
	require.Nil(t, hold)
	require.NoError(t, os.MkdirAll(bp.Plan.Dir, 0o700))

	require.NoError(t, b.Trust(t.Context(), bp))

	worktree, err := filepath.EvalSymlinks(bp.Plan.Dir)
	require.NoError(t, err)
	assert.True(t, trustedIn(t, filepath.Join(h.home, ".claude.json"), worktree),
		"Claude Code's run would stop at the trust question no one answers")

	codexReq := readOnly(h.trustedPR)
	codexReq.Agent = agent.Codex

	codexPlan, hold := h.planBackground(t, codexReq)
	require.Nil(t, hold)
	require.NoError(t, b.Trust(t.Context(), codexPlan))
	assert.NoDirExists(t, filepath.Join(h.home, ".codex"), "Codex's run reads no trust, so none is written")

	h.cfg.Trust = map[agent.ID]Truster{agent.Claude: failingTruster{}}
	require.ErrorIs(t, NewBackgrounds(h.service()).Trust(t.Context(), bp), errFakeSettings)

	bp.Plan.Agent = "an-agent-nobody-ships"
	require.Error(t, b.Trust(t.Context(), bp))
}

func TestFencedRefusesAPresetItsPlanWasntMadeFor(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.paths.AgentRuns = t.TempDir()

	bp, hold := h.planBackground(t, readOnly(h.trustedPR))
	require.Nil(t, hold)

	bp.Preset = fence.EditTestPush

	_, _, err := NewBackgrounds(h.service()).Fenced(bp, FenceIn{Worktree: t.TempDir(), Program: "/app"})
	require.ErrorIs(t, err, agent.ErrInvalidAccess, "a plan's fence is never widened after it was planned")
}
