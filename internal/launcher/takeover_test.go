package launcher

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// takenOverID is the session a take over resumes into.
const takenOverID = "33333333-4444-4555-8666-777777777777"

// fakeTakeOvers stands in for the background runner and Autopilot: TakeOver records the
// run, and marks its session ended in the ledger, as the run's end recorded does.
type fakeTakeOvers struct {
	ledger    *fakeLedger
	sessionID string

	mu    sync.Mutex
	taken []int64
	err   error
}

func (f *fakeTakeOvers) TakeOver(_ context.Context, runID int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.err != nil {
		return f.err
	}

	f.taken = append(f.taken, runID)

	f.ledger.mu.Lock()
	s := f.ledger.resumable[f.sessionID]
	s.Live = false
	f.ledger.resumable[f.sessionID] = s
	f.ledger.mu.Unlock()

	return nil
}

func (f *fakeTakeOvers) runs() []int64 {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]int64{}, f.taken...)
}

// backgroundRun sets the harness up with a background run's session working on item, in
// its worktree, under preset, with the run's own folder holding its fence's settings, and
// returns the take overs' fake and the run's folder.
func (h *launchHarness) backgroundRun(t *testing.T, itemID int64, preset fence.Preset) (*fakeTakeOvers, string) {
	t.Helper()

	_, _ = h.launch(t, LaunchRequest{ItemID: itemID})
	first := h.ledger.all()[0]

	runDir := filepath.Join(h.paths.AgentRuns, testSessionID)
	require.NoError(t, os.MkdirAll(runDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(runDir, agent.FenceSettingsName), []byte("{}\n"), 0o600))

	h.ledger.resumable = map[string]ResumableSession{testSessionID: {
		ID: first.ID, AgentSessionID: first.AgentSessionID, Agent: agent.Claude, Profile: profiles.DefaultName,
		ItemID: itemID, Dir: first.Dir, Worktree: first.Worktree, Live: true, RunID: 42,
		Fence: &Fence{Preset: preset, RunDir: runDir},
	}}

	takes := &fakeTakeOvers{ledger: h.ledger, sessionID: testSessionID}
	h.cfg.TakeOvers = takes
	h.cfg.NewID = func() string { return takenOverID }

	return takes, runDir
}

// scriptOf reads a launch's script.
func scriptOf(t *testing.T, res LaunchResult) string {
	t.Helper()

	script, err := os.ReadFile(res.ScriptPath)
	require.NoError(t, err)

	return string(script)
}

// quoted writes argv as the launch script writes it: each value quoted as one word.
func quoted(argv ...string) string {
	out := ""

	for i, a := range argv {
		if i > 0 {
			out += " "
		}

		out += textutil.ShellQuote(a)
	}

	return out
}

func TestTakeOverStopsTheRunThenResumesItUnderItsFence(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	takes, runDir := h.backgroundRun(t, h.trustedPR, fence.EditTest)

	res, err := h.service().TakeOver(t.Context(), TakeOverRequest{SessionID: testSessionID})
	require.NoError(t, err)
	require.Nil(t, res.Hold)

	assert.Equal(t, []int64{42}, takes.runs(), "the run is stopped, and Autopilot told, before the resume")
	assert.Equal(t, takenOverID, res.SessionID)
	assert.Equal(t, "fenced: Edit and test, no push; asks you about the rest", res.Permission)

	script := scriptOf(t, res)
	assert.Contains(t, script, quoted(
		"--setting-sources", "", "--tools", "Bash,Read,Edit,Write,Grep,Glob", "--permission-mode", "manual",
		"--strict-mcp-config", "--settings", filepath.Join(runDir, agent.FenceSettingsName), "--resume="+testSessionID,
	)+"\n", "the resume's argv, each value its own word, keeps the fence's access and asks about the rest")
	assert.NotContains(t, script, "--dangerously-skip-permissions", "the profile's full access isn't taken")
	assert.NotContains(t, script, "dontAsk", "someone answers now")
	assert.NotContains(t, script, "--print", "it runs in the terminal, not headless")
	assert.NotContains(t, script, "prompt=", "the note isn't sent again: the run's snapshot was off")

	started := h.ledger.all()
	require.Len(t, started, 2)
	assert.Equal(t, takenOverID, started[1].ID)
	assert.Equal(t, testSessionID, started[1].ResumedFrom, "the resumed session names the run's")
	assert.Equal(t, testSessionID, started[1].AgentSessionID, "it carries the run's conversation on")
	assert.Equal(t, started[0].Dir, started[1].Dir, "in the run's worktree")
	assert.Equal(t, res.Permission, started[1].Permission)
}

func TestTakingOverWidensOnlyWhenTheBoxIsTicked(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	_, runDir := h.backgroundRun(t, h.trustedPR, fence.ReadOnly)

	res, err := h.service().TakeOver(t.Context(), TakeOverRequest{SessionID: testSessionID, Profile: true})
	require.NoError(t, err)

	script := scriptOf(t, res)
	assert.Contains(t, script, quoted("--dangerously-skip-permissions", "--resume="+testSessionID)+"\n",
		"ticked, the session takes the profile's own permissions")
	assert.NotContains(t, script, runDir, "and none of the run's fence")
	assert.Equal(t, "full access", res.Permission)

	h.ledger.resumable[testSessionID] = ResumableSession{
		ID: testSessionID, AgentSessionID: testSessionID, Agent: agent.Claude, Profile: profiles.DefaultName,
		ItemID: h.trustedPR, Dir: h.ledger.all()[0].Dir, Worktree: h.ledger.all()[0].Worktree, RunID: 42,
		Fence: &Fence{Preset: fence.ReadOnly, RunDir: runDir},
	}

	res, err = h.service().TakeOver(t.Context(), TakeOverRequest{SessionID: testSessionID})
	require.NoError(t, err)
	assert.Contains(t, scriptOf(t, res), quoted("--restricted", "--tools", "Read,Grep,Glob", "--permission-mode", "manual"),
		"unticked, it keeps the run's own preset")
}

func TestTakingOverAnOutsideContributorsItemStaysPlanningOnly(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.backgroundRun(t, h.outsidePR, fence.EditTest)

	res, err := h.service().TakeOver(t.Context(), TakeOverRequest{SessionID: testSessionID})
	require.NoError(t, err)
	require.Nil(t, res.Hold)
	assert.Contains(t, scriptOf(t, res), quoted("--restricted", "--tools", "Read,Grep,Glob"),
		"the trust rule holds the fence to Read only")

	res, err = h.service().TakeOver(t.Context(), TakeOverRequest{SessionID: testSessionID, Profile: true})
	require.NoError(t, err)
	assert.Contains(t, scriptOf(t, res), quoted("--permission-mode", "plan", "--resume="+testSessionID),
		"widened, it plans only, under the trust rule as always")
}

func TestTakingOverARunWhoseFenceIsGone(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	_, runDir := h.backgroundRun(t, h.trustedPR, fence.EditTest)
	require.NoError(t, os.RemoveAll(runDir))

	_, err := h.service().TakeOver(t.Context(), TakeOverRequest{SessionID: testSessionID})
	require.ErrorIs(t, err, ErrNoFence)
	assert.Contains(t, uierr.Message(err), "Use the profile's permissions")
	assert.Len(t, h.iterm.opened, 1, "nothing opened beyond the first launch")

	res, err := h.service().TakeOver(t.Context(), TakeOverRequest{SessionID: testSessionID, Profile: true})
	require.NoError(t, err, "the user can still take it over with the profile's permissions")
	assert.Equal(t, takenOverID, res.SessionID)
}

func TestTakeOverRefuses(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	takes, _ := h.backgroundRun(t, h.trustedPR, fence.EditTest)

	terminalSession := h.ledger.resumable[testSessionID]
	terminalSession.RunID, terminalSession.Fence, terminalSession.Live = 0, nil, false
	h.ledger.resumable["terminal"] = terminalSession

	_, err := h.service().TakeOver(t.Context(), TakeOverRequest{SessionID: "terminal"})
	require.ErrorIs(t, err, ErrNotBackground)

	_, err = h.service().Resume(t.Context(), ResumeRequest{SessionID: "terminal", Access: ResumeFence})
	require.ErrorIs(t, err, ErrNotBackground, "a session in the terminal has no fence to resume under")

	_, err = h.service().Resume(t.Context(), ResumeRequest{SessionID: "terminal", Access: "loose"})
	require.ErrorIs(t, err, ErrInvalidResumeAccess)

	_, err = h.service().Resume(t.Context(), ResumeRequest{SessionID: testSessionID})
	require.ErrorIs(t, err, ErrSessionRunning)
	assert.Contains(t, uierr.Message(err), "Use Take over", "a run still working is taken over, not resumed")

	takes.err = assert.AnError
	_, err = h.service().TakeOver(t.Context(), TakeOverRequest{SessionID: testSessionID})
	require.ErrorIs(t, err, assert.AnError, "a run that couldn't be stopped isn't resumed beside it")
	assert.Len(t, h.iterm.opened, 1)
}

func TestTheItemStaysHeldWhileTheResumedSessionRuns(t *testing.T) {
	t.Parallel()

	h := newLaunchHarness(t)
	h.backgroundRun(t, h.trustedPR, fence.EditTest)

	_, err := h.service().TakeOver(t.Context(), TakeOverRequest{SessionID: testSessionID})
	require.NoError(t, err)

	resumed := h.ledger.all()[1]
	h.ledger.setRunning(runningAt{dir: resumed.Dir, session: RunningSession{
		SessionID: resumed.ID, Agent: agent.Claude, Doing: "is working", Dir: resumed.Dir, Worktree: resumed.Worktree,
		Repo: "acme-org/" + exampleRepo, Number: 7, CanFocus: true,
	}})

	res, err := h.service().Launch(t.Context(), LaunchRequest{ItemID: h.trustedPR})
	require.NoError(t, err)
	require.NotNil(t, res.Hold, "a click on the item waits for the session that carries the run on")
	assert.Equal(t, HoldRunning, res.Hold.Reason)
	assert.Equal(t, takenOverID, res.Hold.Running[0].SessionID)
	assert.False(t, res.Hold.Running[0].Background, "the resumed session is the user's, in the terminal")
}
