package launcher

import (
	"os"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/sensitive"
)

// routineOf is a routine's launch of req, named Fix checks, its first try.
func routineOf(req LaunchRequest, confirmed ...string) LaunchRequest {
	req.Routine = &RoutineLaunch{Name: "Fix checks", Trigger: "Checks fail", Attempt: 1, Confirmed: confirmed}

	return req
}

// launchFileName matches a launch file's stamped name, which differs from one launch to
// the next.
var launchFileName = regexp.MustCompile(`[a-z]+-\d+-\d{8}T\d{6}Z-[0-9a-f]+`)

func TestARoutinesLaunchIsExactlyAClicksLaunch(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	click := LaunchRequest{ItemID: h.trustedPR, Profile: profileWork, Model: new(strOpus)}

	clicked, clickScript := h.launch(t, click)
	clickPrompt := promptFor(t, clicked.ScriptPath)

	require.NoError(t, os.RemoveAll(worktreeOf(WorktreeRequest{Clone: h.clone, Kind: item.KindPR, Number: 7})))

	ran, runScript := h.launch(t, routineOf(click))

	assert.Equal(t, clickPrompt, promptFor(t, ran.ScriptPath), "the same prompt")
	assert.Equal(t, launchFileName.ReplaceAllString(clickScript, "F"), launchFileName.ReplaceAllString(runScript, "F"),
		"the same script: the folder, the agent, its flags, and its environment")

	clicked.ScriptPath, ran.ScriptPath = "", ""
	assert.Equal(t, clicked, ran, "the same plan: the clone, the worktree, the agent, the profile, the permissions")

	ensured := h.maker.ensures()
	require.Len(t, ensured, 2)
	assert.Equal(t, ensured[0], ensured[1], "the same worktree")

	preview, err := h.service().Preview(t.Context(), click)
	require.NoError(t, err)
	routinePreview, err := h.service().Preview(t.Context(), routineOf(click))
	require.NoError(t, err)
	assert.Equal(t, preview, routinePreview, "and the same Preview")
}

func TestARoutinesLaunchChecksTheFilesOfEveryPullRequestTheUserDidntOpen(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	files := h.withFiles(checkOf(holdHeadA, true, "main.go"))
	agentFiles := checkOf(holdHeadA, true, ".claude/settings.json", "main.go")
	files.unattended = map[int64]*sensitive.Check{h.trustedPR: agentFiles}

	// A click on a member's pull request checks nothing: its files aren't checked.
	clicked, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR})
	require.NoError(t, err)
	assert.Nil(t, clicked.Hold)
	assert.Empty(t, files.wide)

	require.NoError(t, os.RemoveAll(worktreeOf(WorktreeRequest{Clone: h.clone, Kind: item.KindPR, Number: 7})))

	// A routine's launch checks them under the unattended scope, and is held for its
	// agent files.
	held, err := h.service().Launch(t.Context(), routineOf(LaunchRequest{ItemID: h.trustedPR}))
	require.NoError(t, err)
	require.NotNil(t, held.Hold, "a routine's launch on a pull request that changes agent files waits for the user")
	assert.Equal(t, HoldAgentFiles, held.Hold.Reason)
	assert.Equal(t, []int64{h.trustedPR}, files.wide)

	// The request's own Confirmed, a click's, never confirms a routine's hold.
	again, err := h.service().Launch(t.Context(), routineOf(LaunchRequest{ItemID: h.trustedPR, Confirmed: held.Hold.Key}))
	require.NoError(t, err)
	assert.NotNil(t, again.Hold)

	// The user's Run on exactly that commit does.
	ran, err := h.service().Launch(t.Context(), routineOf(LaunchRequest{ItemID: h.trustedPR}, held.Hold.Key))
	require.NoError(t, err)
	assert.Nil(t, ran.Hold)
	assert.NotEmpty(t, ran.ScriptPath)
}

func TestARoutinesLaunchOnAPullRequestTheUserOpenedChecksNoFiles(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	files := h.withFiles(checkOf(holdHeadA, true, "main.go"))
	files.unattended = map[int64]*sensitive.Check{}

	// The fake answers ErrNotChecked for an item with no check, as the inbox does for the
	// user's own pull request.
	ran, err := h.service().Launch(t.Context(), routineOf(LaunchRequest{ItemID: h.trustedPR}))
	require.NoError(t, err)
	assert.Nil(t, ran.Hold)
}

func TestARoutineNeverStartsBesideASessionNorLiftsTheOutsideRule(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.ledger.setRunning(h.runningIn("s1", "pr-7"))

	held, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR})
	require.NoError(t, err)
	require.NotNil(t, held.Hold)
	require.Equal(t, HoldRunning, held.Hold.Reason)

	still, err := h.service().Launch(t.Context(), routineOf(LaunchRequest{ItemID: h.trustedPR, Alongside: held.Hold.Key}))
	require.NoError(t, err)
	require.NotNil(t, still.Hold, "a routine never starts beside a session, whatever its request says")
	assert.Equal(t, HoldRunning, still.Hold.Reason)

	h2 := newLaunchHarness(t)
	h2.withFiles(checkOf(holdHeadA, true, "main.go"))

	ran, err := h2.service().Launch(t.Context(), routineOf(LaunchRequest{ItemID: h2.outsidePR, FullAccessOverride: true}))
	require.NoError(t, err)
	assert.Equal(t, "plan mode (outside contributor)", ran.Permission, "only a person lifts the outside contributor rule")
}

// TestNoRoutineLiftsTheOutsideContributorRule runs a routine's launch on each trust class
// with every request a routine could carry: an outside contributor's item always plans
// only, and a trusted one runs as its profile says, so FullAccessOverride never takes
// effect for a routine, whatever its mode (which never reaches the launcher).
func TestNoRoutineLiftsTheOutsideContributorRule(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name     string
		item     func(h *launchHarness) int64
		override bool
		want     string
	}{
		{"an outside contributor's item", func(h *launchHarness) int64 { return h.outsidePR }, false, "plan mode (outside contributor)"},
		{"an outside contributor's item, override asked", func(h *launchHarness) int64 { return h.outsidePR }, true, "plan mode (outside contributor)"},
		{"a trusted item", func(h *launchHarness) int64 { return h.trustedPR }, false, "full access"},
		{"a trusted item, override asked", func(h *launchHarness) int64 { return h.trustedPR }, true, "full access"},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			h := newLaunchHarness(t)
			h.withFiles(checkOf(holdHeadA, true, "main.go"))

			preview, err := h.service().Preview(t.Context(), routineOf(LaunchRequest{ItemID: c.item(h), FullAccessOverride: c.override}))
			require.NoError(t, err)
			assert.Equal(t, c.want, preview.Permission)

			ran, err := h.service().Launch(t.Context(), routineOf(LaunchRequest{ItemID: c.item(h), FullAccessOverride: c.override}))
			require.NoError(t, err)
			assert.Equal(t, c.want, ran.Permission)
		})
	}
}

func TestARoutinesLaunchIntoADirtyWorktreeWaitsForTheUser(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.maker.dirty, h.maker.head = 3, holdHeadA
	require.NoError(t, os.MkdirAll(worktreeOf(WorktreeRequest{Clone: h.clone, Kind: item.KindPR, Number: 7}), 0o700))

	clicked, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR})
	require.NoError(t, err)
	assert.Nil(t, clicked.Hold, "a click into a dirty worktree goes ahead, as today")

	held, err := h.service().Launch(t.Context(), routineOf(LaunchRequest{ItemID: h.trustedPR}))
	require.NoError(t, err)
	require.NotNil(t, held.Hold)
	assert.Equal(t, HoldDirty, held.Hold.Reason)
	assert.Equal(t, 3, held.Hold.Dirty)
	assert.Equal(t, "pr-7", held.Hold.Worktree)

	ran, err := h.service().Launch(t.Context(), routineOf(LaunchRequest{ItemID: h.trustedPR}, held.Hold.Key))
	require.NoError(t, err)
	assert.Nil(t, ran.Hold, "the user's OK of exactly those files lets it through")

	h.maker.dirty = 4
	moved, err := h.service().Launch(t.Context(), routineOf(LaunchRequest{ItemID: h.trustedPR}, held.Hold.Key))
	require.NoError(t, err)
	assert.NotNil(t, moved.Hold, "an OK of 3 files is no OK of 4")
}

func TestARoutinesLaunchTellsItsPromptTheRoutine(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	svc := prompts.NewService(prompts.Config{Store: h.prompts})
	p, err := svc.Create(t.Context(), prompts.Input{
		Name: "Routine", Nickname: "Routine", AppliesTo: prompts.AppliesToAny,
		Body: "{{.Routine}} · {{.Trigger}} · try {{.Attempt}} · unattended {{.Unattended}} · {{.URL}}",
	})
	require.NoError(t, err)

	clicked, _ := h.launch(t, LaunchRequest{ItemID: h.issue, PromptID: p.ID, Worktree: new(false)})
	assert.Contains(t, promptFor(t, clicked.ScriptPath), " ·  · try 0 · unattended false", "a click names no routine")

	ran, _ := h.launch(t, routineOf(LaunchRequest{ItemID: h.issue, PromptID: p.ID, Worktree: new(false)}))
	assert.Contains(t, promptFor(t, ran.ScriptPath), "Fix checks · Checks fail · try 1 · unattended false",
		"a routine's run in the terminal is attended")
}

func TestTheAPIKeySettingDecidesWhetherALaunchUnsetsTheKey(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)

	off, err := h.service().Preview(t.Context(), LaunchRequest{ItemID: h.issue, Worktree: new(false)})
	require.NoError(t, err)
	assert.Contains(t, off.Env, "unset ANTHROPIC_API_KEY", "off by default: the profile's own sign-in is used")

	_, err = h.settings.SetClaudeAPIKey(t.Context(), true)
	require.NoError(t, err)

	on, err := h.service().Preview(t.Context(), LaunchRequest{ItemID: h.issue, Worktree: new(false)})
	require.NoError(t, err)
	assert.NotContains(t, on.Env, "unset ANTHROPIC_API_KEY", "on, a key from the shell passes through")

	codex, err := h.service().Preview(t.Context(), LaunchRequest{ItemID: h.issue, Worktree: new(false), Agent: "codex"})
	require.NoError(t, err)
	assert.Contains(t, codex.Env, "unset OPENAI_API_KEY", "each agent has its own setting")
}
