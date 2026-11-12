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
	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/paths"
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
	assert.Equal(t, WorktreeCreate, got.WorktreeAction)
	assert.Equal(t, "full access", got.Permission)
	assert.Equal(t, res.Permission, got.Permission, "the ledger keeps the launch's own words")
	assert.Empty(t, got.Notice)
	assert.Equal(t, fixedNow(), got.LaunchedAt)
}

func TestLaunchRecordsWhatItWorkedAround(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.maker.notice = "Couldn't fetch origin/main, so the worktree starts from the last fetched copy."

	res, _ := h.launch(t, LaunchRequest{ItemID: h.issue})

	started := h.ledger.all()
	require.Len(t, started, 1)
	assert.Equal(t, h.maker.notice, res.Notice)
	assert.Equal(t, h.maker.notice, started[0].Notice, "the session's panel says so after the launch")
	assert.Equal(t, WorktreeCreate, started[0].WorktreeAction)
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

	res, err := h.service().Resume(t.Context(), ResumeRequest{SessionID: testSessionID})
	require.NoError(t, err)

	assert.Equal(t, resumed, res.SessionID)
	assert.Equal(t, WorktreeResume, res.WorktreeAction)

	script, err := os.ReadFile(res.ScriptPath)
	require.NoError(t, err)
	assert.NotContains(t, string(script), "prompt=", "a resumed session reads no prompt")
	assert.Contains(t, string(script), "'--dangerously-skip-permissions' '--resume="+testSessionID+"'\n")
	assert.NotContains(t, string(script), "--name=")
	assert.NotContains(t, string(script), "--session-id")
	assert.NoFileExists(t, strings.TrimSuffix(res.ScriptPath, ".command")+terminal.PromptSuffix)

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

	res, err := h.service().Resume(t.Context(), ResumeRequest{SessionID: testSessionID})
	require.NoError(t, err)

	script, err := os.ReadFile(res.ScriptPath)
	require.NoError(t, err)
	assert.Contains(t, string(script), "/codex' 'resume' '"+thread+"' '--dangerously-bypass-approvals-and-sandbox' '--'\n")
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

	running, carried, unlinked, moved, gone := base, base, base, base, base
	running.Live = true
	carried.Carried = true
	unlinked.AgentSessionID = ""
	moved.Dir = filepath.Join(h.root, "elsewhere")
	gone.ItemID = 999

	h.ledger.resumable = map[string]ResumableSession{
		"running": running, "carried": carried, "unlinked": unlinked, "moved": moved, "gone": gone,
	}

	tests := map[string]struct {
		want error
		says string
	}{
		"running":  {want: ErrSessionRunning, says: "still running"},
		"carried":  {want: ErrSessionRunning, says: "already running in another session"},
		"unlinked": {want: ErrNotLinked, says: "nothing to resume"},
		"moved":    {want: ErrSessionMoved, says: "~/projects/elsewhere"},
		"gone":     {want: item.ErrNotFound, says: "no longer in your inbox"},
	}

	for id, tt := range tests {
		_, err := h.service().Resume(t.Context(), ResumeRequest{SessionID: id})
		require.ErrorIs(t, err, tt.want, id)
		assert.Contains(t, uierr.Message(err), tt.says, id)
	}

	_, err := h.service().Resume(t.Context(), ResumeRequest{SessionID: "unknown"})
	require.ErrorIs(t, err, errUnknownSession)

	assert.Len(t, h.iterm.opened, 1, "nothing was opened for a refused resume")

	h.cfg.Ledger = nil
	_, err = h.service().Resume(t.Context(), ResumeRequest{SessionID: resumedID})
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

	_, err := h.service().Resume(t.Context(), ResumeRequest{SessionID: testSessionID})
	require.ErrorIs(t, err, ErrSessionMoved, "a resume makes no worktree")
	assert.Len(t, h.maker.ensures(), 1, "only the launch asked for the worktree")
}

func TestPlanArgvWithoutAPrompt(t *testing.T) {
	t.Parallel()

	p := Plan{Binary: "/opt/agent/claude", Args: []string{"--resume=abc"}, NoPrompt: true}
	assert.Equal(t, []string{"/opt/agent/claude", "--resume=abc"}, p.Argv("ignored"))
}

// runningIn is a Claude session of item #7 needing approval in a worktree of the
// harness's clone, or in the clone itself for "".
func (h *launchHarness) runningIn(id, worktree string) runningAt {
	dir := h.clone
	if worktree != "" {
		dir = filepath.Join(filepath.Dir(worktreeOf(WorktreeRequest{Clone: h.clone, Kind: item.KindPR, Number: 7})),
			worktree)
	}

	return runningAt{dir: dir, session: RunningSession{
		SessionID: id, Agent: agent.Claude, Doing: "needs approval", Since: "2026-03-14T15:00:00Z",
		Dir: paths.Tilde(dir, h.home), Worktree: worktree, Repo: "acme-org/" + exampleRepo, Number: 7, CanFocus: true,
	}}
}

func TestLaunchWaitsWhileASessionRunsInTheSameFolder(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	s1, s2 := h.runningIn("s1", "pr-7"), h.runningIn("s2", "pr-7")
	h.ledger.setRunning(s2, s1, h.runningIn("s3", ""))

	res, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR})
	require.NoError(t, err)
	require.NotNil(t, res.Hold)
	assert.Equal(t, HoldRunning, res.Hold.Reason)
	assert.Equal(t, "running:s1,s2", res.Hold.Key)
	assert.Equal(t, []RunningSession{s2.session, s1.session}, res.Hold.Running,
		"only the sessions in the folder it would start in")
	assert.Equal(t, "pr-7", res.Hold.Worktree)
	assert.NotNil(t, res.Hold.Files)
	assert.Empty(t, h.iterm.opened, "nothing started")
	assert.Empty(t, h.ledger.all())
	assert.Equal(t, []string{s1.dir}, h.ledger.asked, "asked about the folder it would start in")

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
	h.ledger.setRunning(h.runningIn("s1", "pr-7"))

	res, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR, Worktree: new(false)})
	require.NoError(t, err)
	assert.Nil(t, res.Hold, "another folder")

	h.ledger.setRunning(h.runningIn("s2", ""))

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

	assert.Nil(t, holdRunning([]RunningSession{{SessionID: "s1"}}, "pr-7", alongside))
	assert.Nil(t, holdRunning(nil, "pr-7", ""))

	hold := holdFor(nil, false, "", "")
	assert.Nil(t, hold, "an unchecked item has no files hold")
}

func TestARunningHoldAsksGitHubNothingAndChainsWithAFilesHold(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	files := h.withFiles(checkOf(holdHeadA, true, ".claude/settings.json"))
	h.ledger.setRunning(h.runningIn("s1", "pr-8"))

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

func TestLaunchInTheCloneWaitsForAnotherItemsSessionThere(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	other := h.runningIn("s1", "")
	other.session.Number = 12
	h.ledger.setRunning(other)

	res, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR, Worktree: new(false)})
	require.NoError(t, err)
	require.NotNil(t, res.Hold, "two items' sessions in one clone would each check out their own pull request")
	assert.Equal(t, 12, res.Hold.Running[0].Number, "the hold names the item the running session is on")
}

func TestLaunchDoesNotWaitForTheItemsSessionInAnotherClone(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	elsewhere := h.runningIn("s1", "pr-7")
	elsewhere.dir = filepath.Join(h.root, "other-clone", "pr-7")
	h.ledger.setRunning(elsewhere)

	res, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR})
	require.NoError(t, err)
	assert.Nil(t, res.Hold, "the same item's worktree in another clone is another folder")
}

func TestResumeWaitsLikeALaunch(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	files := h.withFiles(checkOf(holdHeadA, true, ".claude/settings.json"))

	held, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.outsidePR})
	require.NoError(t, err)
	require.NotNil(t, held.Hold)

	_, err = h.service().Launch(t.Context(), LaunchRequest{ItemID: h.outsidePR, Confirmed: held.Hold.Key})
	require.NoError(t, err)

	first := h.ledger.all()[0]
	h.ledger.resumable = map[string]ResumableSession{testSessionID: {
		ID: first.ID, AgentSessionID: first.AgentSessionID, Agent: agent.Claude, Profile: profiles.DefaultName,
		ItemID: h.outsidePR, Dir: first.Dir, Worktree: first.Worktree,
	}}

	other := h.runningIn("s9", "pr-8")
	h.ledger.setRunning(other)

	res, err := h.service().Resume(t.Context(), ResumeRequest{SessionID: testSessionID})
	require.NoError(t, err)
	require.NotNil(t, res.Hold)
	assert.Equal(t, HoldRunning, res.Hold.Reason, "another session runs in the folder")

	alongside := res.Hold.Key

	res, err = h.service().Resume(t.Context(), ResumeRequest{SessionID: testSessionID, Alongside: alongside})
	require.NoError(t, err)
	require.NotNil(t, res.Hold)
	assert.Equal(t, HoldAgentFiles, res.Hold.Reason, "then the pull request's files, as last checked")

	res, err = h.service().Resume(t.Context(), ResumeRequest{
		SessionID: testSessionID, Alongside: alongside, Confirmed: res.Hold.Key,
	})
	require.NoError(t, err)
	assert.Nil(t, res.Hold)
	assert.Len(t, h.iterm.opened, 2, "the launch, then the resume once both were confirmed")
	assert.Equal(t, []int64{h.outsidePR, h.outsidePR}, files.fresh, "only the launches asked GitHub")
}

func TestResumeThroughALinkToItsFolder(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	_, _ = h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	first := h.ledger.all()[0]

	link := filepath.Join(t.TempDir(), "worktrees")
	require.NoError(t, os.Symlink(filepath.Dir(first.Dir), link))

	h.ledger.resumable = map[string]ResumableSession{testSessionID: {
		ID: first.ID, AgentSessionID: first.AgentSessionID, Agent: agent.Claude, Profile: profiles.DefaultName,
		ItemID: h.trustedPR, Dir: filepath.Join(link, filepath.Base(first.Dir)), Worktree: first.Worktree,
	}}

	res, err := h.service().Resume(t.Context(), ResumeRequest{SessionID: testSessionID})
	require.NoError(t, err, "the folder recorded through a link is the same folder")
	assert.Equal(t, WorktreeResume, res.WorktreeAction)
}

func TestResumeFillsInNoPrompt(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	_, _ = h.launch(t, LaunchRequest{ItemID: h.trustedPR})
	first := h.ledger.all()[0]

	h.ledger.resumable = map[string]ResumableSession{testSessionID: {
		ID: first.ID, AgentSessionID: first.AgentSessionID, Agent: agent.Claude, Profile: profiles.DefaultName,
		ItemID: h.trustedPR, Dir: first.Dir, Worktree: first.Worktree,
	}}
	h.cfg.Items = failingItems{ItemStore: h.cfg.Items, viewerErr: errFakeViewer}

	_, err := h.service().Resume(t.Context(), ResumeRequest{SessionID: testSessionID})
	require.NoError(t, err, "a resume reads no prompt, so it needs no signed-in account to fill one in")
}
