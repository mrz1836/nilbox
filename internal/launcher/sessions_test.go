package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// resumedID is the ID the harness's ledger holds for an ended session.
const resumedID = "11111111-2222-4333-8444-555555555555"

func TestLaunchRecordsTheSessionInTheLedger(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)

	res, _ := h.launch(t, LaunchRequest{ItemID: h.trustedPR})

	started := h.ledger.all()
	require.Len(t, started, 1)

	got := started[0]
	assert.Equal(t, testSessionID, got.ID)
	assert.Equal(t, testSessionID, got.AgentSessionID, "Claude takes the app's ID, so the session is linked at once")
	assert.Empty(t, got.ResumedFrom)
	assert.Equal(t, agent.Claude, got.Agent)
	assert.Equal(t, profiles.DefaultName, got.Profile)
	assert.Empty(t, got.ConfigDir, "the default profile runs with the agent's own config")
	assert.Equal(t, "Review", got.Prompt)
	assert.True(t, strings.HasPrefix(promptFor(t, res.ScriptPath), got.PromptHead))
	assert.LessOrEqual(t, len([]rune(got.PromptHead)), promptHeadRunes)
	assert.Equal(t, "iterm2", got.Terminal)
	assert.Equal(t, h.trustedPR, got.ItemID)
	assert.Equal(t, "acme-org", got.Owner)
	assert.Equal(t, exampleRepo, got.Name)
	assert.Equal(t, item.KindPR, got.Kind)
	assert.Equal(t, 7, got.Number)
	assert.Equal(t, "Change 7", got.Title)
	assert.Equal(t, worktreeOf(WorktreeRequest{Clone: h.clone, Kind: item.KindPR, Number: 7}), got.Dir)
	assert.Equal(t, h.clone, got.ClonePath)
	assert.Equal(t, "pr-7", got.Worktree)
	assert.Equal(t, fixedNow(), got.LaunchedAt)
}

func TestLaunchLeavesACodexSessionToBeLinked(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)

	_, script := h.launch(t, LaunchRequest{ItemID: h.issue, Agent: agent.Codex, Profile: "work"})

	started := h.ledger.all()
	require.Len(t, started, 1)
	assert.Equal(t, testSessionID, started[0].ID)
	assert.Empty(t, started[0].AgentSessionID, "Codex takes no ID: the app finds its thread once it starts")
	assert.Equal(t, filepath.Join(h.home, ".codex_work"), started[0].ConfigDir)
	assert.NotContains(t, script, testSessionID)
}

func TestLaunchStartsWhenTheLedgerFails(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.ledger.err = assert.AnError

	_, _ = h.launch(t, LaunchRequest{ItemID: h.trustedPR})

	assert.Len(t, h.iterm.opened, 1, "a ledger failure only costs following the session")
}

func TestResumeReopensTheConversationWithoutAPrompt(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	_, _ = h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	first := h.ledger.all()[0]

	h.ledger.resumable = map[string]ResumableSession{testSessionID: {
		ID: first.ID, AgentSessionID: first.AgentSessionID, Agent: agent.Claude, Profile: profiles.DefaultName,
		ItemID: h.trustedPR, Dir: first.Dir, Worktree: first.Worktree,
	}}

	const resumed = "22222222-3333-4444-8555-666666666666"

	h.cfg.NewID = func() string { return resumed }

	res, err := h.service().Resume(t.Context(), testSessionID)
	require.NoError(t, err)

	assert.Equal(t, resumed, res.SessionID)
	assert.Equal(t, WorktreeResume, res.WorktreeAction)

	script, err := os.ReadFile(res.ScriptPath)
	require.NoError(t, err)
	assert.NotContains(t, string(script), "prompt=", "a resumed session reads no prompt")
	assert.Contains(t, string(script), "'--dangerously-skip-permissions' '--resume="+testSessionID+"'\n")
	assert.NotContains(t, string(script), "--name=")
	assert.NotContains(t, string(script), "--session-id")
	assert.NoFileExists(t, strings.TrimSuffix(res.ScriptPath, ".command")+promptFileSuffix)

	started := h.ledger.all()
	require.Len(t, started, 2)
	assert.Equal(t, resumed, started[1].ID)
	assert.Equal(t, testSessionID, started[1].AgentSessionID, "the resumed session shares the agent's ID")
	assert.Equal(t, testSessionID, started[1].ResumedFrom)
	assert.Empty(t, started[1].Prompt)
	assert.Empty(t, started[1].PromptHead)
}

func TestResumeACodexThread(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	_, _ = h.launch(t, LaunchRequest{ItemID: h.issue, Agent: agent.Codex})
	first := h.ledger.all()[0]

	const thread = "019a1bc2-0000-7000-8000-00000000abcd"

	h.ledger.resumable = map[string]ResumableSession{testSessionID: {
		ID: first.ID, AgentSessionID: thread, Agent: agent.Codex, Profile: profiles.DefaultName, ItemID: h.issue,
		Dir: first.Dir, Worktree: first.Worktree,
	}}

	res, err := h.service().Resume(t.Context(), testSessionID)
	require.NoError(t, err)

	script, err := os.ReadFile(res.ScriptPath)
	require.NoError(t, err)
	assert.Contains(t, string(script), "/codex' 'resume' '--dangerously-bypass-approvals-and-sandbox' '--' '"+thread+"'\n")
	assert.Equal(t, thread, h.ledger.all()[1].AgentSessionID)
}

func TestResumeRefuses(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	_, _ = h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	first := h.ledger.all()[0]
	base := ResumableSession{
		ID: first.ID, AgentSessionID: first.AgentSessionID, Agent: agent.Claude, Profile: profiles.DefaultName,
		ItemID: h.trustedPR, Dir: first.Dir, Worktree: first.Worktree,
	}

	running, unlinked, moved, gone := base, base, base, base
	running.Live = true
	unlinked.AgentSessionID = ""
	moved.Dir = filepath.Join(h.root, "elsewhere")
	gone.ItemID = 999

	h.ledger.resumable = map[string]ResumableSession{"running": running, "unlinked": unlinked, "moved": moved, "gone": gone}

	tests := map[string]struct {
		want error
		says string
	}{
		"running":  {want: ErrSessionRunning, says: "still running"},
		"unlinked": {want: ErrNotLinked, says: "nothing to resume"},
		"moved":    {want: ErrSessionMoved, says: "~/projects/elsewhere"},
		"gone":     {want: item.ErrNotFound, says: "no longer in your inbox"},
	}

	for id, tt := range tests {
		_, err := h.service().Resume(t.Context(), id)
		require.ErrorIs(t, err, tt.want, id)
		assert.Contains(t, uierr.Message(err), tt.says, id)
	}

	_, err := h.service().Resume(t.Context(), "unknown")
	require.ErrorIs(t, err, errUnknownSession)

	assert.Len(t, h.iterm.opened, 1, "nothing was opened for a refused resume")

	h.cfg.Ledger = nil
	_, err = h.service().Resume(t.Context(), resumedID)
	require.ErrorIs(t, err, ErrNoLedger)
}

func TestResumeNeedsTheWorktreeItRanIn(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	_, _ = h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	first := h.ledger.all()[0]

	require.NoError(t, os.RemoveAll(first.Dir))

	h.ledger.resumable = map[string]ResumableSession{testSessionID: {
		ID: first.ID, AgentSessionID: first.AgentSessionID, Agent: agent.Claude, Profile: profiles.DefaultName,
		ItemID: h.trustedPR, Dir: first.Dir, Worktree: first.Worktree,
	}}

	_, err := h.service().Resume(t.Context(), testSessionID)
	require.ErrorIs(t, err, ErrSessionMoved, "a resume makes no worktree")
	assert.Len(t, h.maker.ensures(), 1, "only the launch asked for the worktree")
}

func TestPlanArgvWithoutAPrompt(t *testing.T) {
	t.Parallel()

	p := Plan{Binary: "/opt/agent/claude", Args: []string{"--resume=abc"}, NoPrompt: true}
	assert.Equal(t, []string{"/opt/agent/claude", "--resume=abc"}, p.Argv("ignored"))
}

// runningIn is a Claude session needing approval in a worktree, or the clone for "".
func runningIn(id, worktree string) RunningSession {
	return RunningSession{
		SessionID: id, Agent: agent.Claude, Doing: "needs approval", Since: "2026-03-14T15:00:00Z",
		Dir: "~/projects/x/" + worktree, Worktree: worktree, CanFocus: true,
	}
}

func TestLaunchWaitsWhileASessionRunsInTheSameFolder(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.ledger.setRunning(runningIn("s2", "pr-7"), runningIn("s1", "pr-7"), runningIn("s3", ""))

	res, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR})
	require.NoError(t, err)
	require.NotNil(t, res.Hold)
	assert.Equal(t, HoldRunning, res.Hold.Reason)
	assert.Equal(t, "running:s1,s2", res.Hold.Key)
	assert.Equal(t, []RunningSession{runningIn("s2", "pr-7"), runningIn("s1", "pr-7")}, res.Hold.Running,
		"only the sessions in the folder it would start in")
	assert.Equal(t, "pr-7", res.Hold.Worktree)
	assert.NotNil(t, res.Hold.Files)
	assert.Empty(t, h.iterm.opened, "nothing started")
	assert.Empty(t, h.ledger.all())
	assert.Equal(t, []string{"acme-org/example-repo#7"}, h.ledger.asked)

	res, err = h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR, Alongside: "running:s1"})
	require.NoError(t, err)
	require.NotNil(t, res.Hold, "a confirmation for other sessions doesn't carry over")

	res, err = h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR, Alongside: "running:s1,s2"})
	require.NoError(t, err)
	assert.Nil(t, res.Hold)
	assert.Len(t, h.iterm.opened, 1, "started beside them once confirmed")
}

func TestLaunchInTheCloneIgnoresSessionsInTheWorktree(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.ledger.setRunning(runningIn("s1", "pr-7"))

	res, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR, Worktree: new(false)})
	require.NoError(t, err)
	assert.Nil(t, res.Hold, "another folder")

	h.ledger.setRunning(runningIn("s2", ""))

	res, err = h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR, Worktree: new(false)})
	require.NoError(t, err)
	require.NotNil(t, res.Hold)
	assert.Equal(t, "running:s2", res.Hold.Key)
}

func TestLaunchGoesAheadWhenTheLedgerCannotBeRead(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.ledger.runErr = assert.AnError

	_, _ = h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	assert.Len(t, h.iterm.opened, 1, "following sessions never stops a launch")

	h.cfg.Ledger = nil
	_, _ = h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	assert.Len(t, h.iterm.opened, 2)
}

func TestARunningHoldAndAFilesHoldAreConfirmedApart(t *testing.T) {
	t.Parallel()

	alongside := "running:s1"

	assert.Nil(t, holdRunning([]RunningSession{runningIn("s1", "pr-7")}, "pr-7", alongside))
	assert.Nil(t, holdRunning(nil, "pr-7", ""))

	hold := holdFor(nil, false, "", "")
	assert.Nil(t, hold, "an unchecked item has no files hold")
}

func TestARunningHoldAsksGitHubNothingAndChainsWithAFilesHold(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	files := h.withFiles(checkOf(holdHeadA, true, ".claude/settings.json"))
	h.ledger.setRunning(runningIn("s1", "pr-8"))

	res, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.outsidePR})
	require.NoError(t, err)
	require.NotNil(t, res.Hold)
	assert.Equal(t, HoldRunning, res.Hold.Reason)
	assert.Empty(t, files.fresh, "no file check for a launch that waits")

	alongside := res.Hold.Key

	res, err = h.service().Launch(t.Context(), LaunchRequest{ItemID: h.outsidePR, Alongside: alongside})
	require.NoError(t, err)
	require.NotNil(t, res.Hold)
	assert.Equal(t, HoldAgentFiles, res.Hold.Reason, "then the pull request's own hold")
	assert.Equal(t, []int64{h.outsidePR}, files.fresh)

	res, err = h.service().Launch(t.Context(), LaunchRequest{
		ItemID: h.outsidePR, Alongside: alongside, Confirmed: res.Hold.Key,
	})
	require.NoError(t, err)
	assert.Nil(t, res.Hold, "each confirmed apart, both hold")
	assert.Len(t, h.iterm.opened, 1)
}
