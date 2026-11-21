package autopilot

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agentrun/runevent"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// fakeViews is a runner's memory of the agents' runs: the progress it follows of each, the
// page and the diff it reads back, or err for every read; it notes the offsets asked.
type fakeViews struct {
	mu       sync.Mutex
	progress map[int64]runevent.Progress
	page     runevent.Page
	diff     runevent.Diff
	err      error
	after    []int64
	before   []int64
}

func (v *fakeViews) Progress(runID int64) (runevent.Progress, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()

	p, ok := v.progress[runID]

	return p, ok
}

func (v *fakeViews) Events(_ context.Context, _, after int64) (runevent.Page, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.after = append(v.after, after)

	return v.page, v.err
}

func (v *fakeViews) EventsBefore(_ context.Context, _, before int64) (runevent.Page, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.before = append(v.before, before)

	return v.page, v.err
}

func (v *fakeViews) Diff(context.Context, int64) (runevent.Diff, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	return v.diff, v.err
}

// goingRun records a run of runner on the match id, queued ago before the rig's now, in
// status, with what its launch started when session or agent is set, and returns its ID.
func (r *statsRig) goingRun(id int64, runner, status string, ago time.Duration, agent, session string) int64 {
	r.t.Helper()

	at := r.now.Add(-ago)

	runID, err := r.q.InsertRun(r.t.Context(), queries.InsertRunParams{
		MatchID: id, RoutineID: r.routines[0], Runner: runner, Status: status, Counted: true, QueuedAt: at,
		StartedAt: &at, UpdatedAt: at, Kind: "pr", RepoOwner: "acme-org", RepoName: "example-repo",
	})
	require.NoError(r.t, err)

	if agent != "" || session != "" {
		require.NoError(r.t, r.q.RecordRunLaunch(r.t.Context(), queries.RecordRunLaunchParams{
			SessionID: session, Agent: agent, UpdatedAt: at, ID: runID,
		}))
	}

	return runID
}

func TestTheRunViewReadsABackgroundRunsTimelineAndChangesFromItsRunner(t *testing.T) {
	t.Parallel()

	r := newStatsRig(t, time.Date(2026, time.November, 16, 9, 0, 0, 0, time.UTC))
	runID := r.goingRun(r.match(0, step(catalog.StepAgent, "")), runnerBackground, runRunning, time.Minute, "claude", "s-1")
	views := &fakeViews{
		page: runevent.Page{First: 4, Ending: &runevent.Ending{Summary: "Fixed the lint."}},
		diff: runevent.Diff{From: "abc123", Patch: "+ fixed"},
	}
	NewEngine(r.svc, EngineConfig{Items: &quiet{}, Views: views})

	page, err := r.svc.RunEvents(t.Context(), runID, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(4), page.First)
	assert.Equal(t, []runevent.Event{}, page.Events, "the window gets lists, never null")
	assert.Equal(t, []runevent.Todo{}, page.Progress.Todos)
	require.NotNil(t, page.Ending)
	assert.Equal(t, "Fixed the lint.", page.Ending.Summary)
	assert.Equal(t, []runevent.Todo{}, page.Ending.Todos)
	assert.Equal(t, []string{}, page.Ending.Changed)
	assert.Equal(t, []string{}, page.Ending.Models)

	_, err = r.svc.RunEventsBefore(t.Context(), runID, 99)
	require.NoError(t, err)
	assert.Equal(t, []int64{3}, views.after, "the events after the one the window has")
	assert.Equal(t, []int64{99}, views.before, "and the page before its first")

	diff, err := r.svc.RunDiff(t.Context(), runID)
	require.NoError(t, err)
	assert.Equal(t, runevent.Diff{From: "abc123", Patch: "+ fixed", Untracked: []string{}}, diff)

	// A runner that can't read them says so, in words for the window.
	views.err = errBroken

	page, err = r.svc.RunEvents(t.Context(), runID, 0)
	require.ErrorIs(t, err, errBroken)
	assert.Equal(t, "This run's events couldn't be read.", uierr.Message(err))
	assert.Equal(t, runevent.EmptyPage(), page)

	_, err = r.svc.RunEventsBefore(t.Context(), runID, 9)
	assert.Equal(t, "This run's earlier events couldn't be read.", uierr.Message(err))

	diff, err = r.svc.RunDiff(t.Context(), runID)
	assert.Equal(t, "This run's changes couldn't be read. Its worktree may be gone.", uierr.Message(err))
	assert.Equal(t, []string{}, diff.Untracked)
}

func TestARunNoRunnerHereFollowsHasAnEmptyTimeline(t *testing.T) {
	t.Parallel()

	r := newStatsRig(t, time.Date(2026, time.November, 16, 9, 0, 0, 0, time.UTC))
	id := r.match(0, step(catalog.StepAgent, ""))
	terminal := r.goingRun(id, "terminal", runRunning, time.Minute, "", "")
	script := r.goingRun(r.match(0, step(catalog.StepScript, "")), runnerScript, runEnded, time.Hour, "", "")
	background := r.goingRun(r.match(0, step(catalog.StepAgent, "")), runnerBackground, runRunning, time.Minute, "", "")

	// No engine, so no runner keeps any run's events: one in the terminal has its session
	// panel, and a script's its run in Scripts → Runs.
	page, err := r.svc.RunEvents(t.Context(), terminal, 0)
	require.NoError(t, err)
	assert.Equal(t, runevent.EmptyPage(), page, "still going")

	page, err = r.svc.RunEventsBefore(t.Context(), script, 0)
	require.NoError(t, err)
	assert.True(t, page.Done, "ended")

	for _, runID := range []int64{background, 999} {
		_, err = r.svc.RunEvents(t.Context(), runID, 0)
		require.ErrorIs(t, err, ErrNoViews, "run %d", runID)
		assert.Equal(t, "This run's events aren't kept here.", uierr.Message(err))

		_, err = r.svc.RunEventsBefore(t.Context(), runID, 0)
		require.ErrorIs(t, err, ErrNoViews)
	}

	diff, err := r.svc.RunDiff(t.Context(), background)
	require.ErrorIs(t, err, ErrNoViews)
	assert.Equal(t, "This run's changes can't be read here.", uierr.Message(err))
	assert.Equal(t, []string{}, diff.Untracked)

	// Without a database, nothing is followed elsewhere either.
	_, err = NewService(Config{}).RunEvents(t.Context(), terminal, 0)
	require.ErrorIs(t, err, ErrNoViews)
}

func TestTheRunViewShowsTheRunWithItsMatchAndWhetherItCanBeTakenOver(t *testing.T) {
	t.Parallel()

	r := newStatsRig(t, time.Date(2026, time.November, 16, 9, 0, 0, 0, time.UTC))
	id := r.match(0, step(catalog.StepAgent, ""))
	going := r.goingRun(id, runnerBackground, runRunning, time.Minute, "claude", "s-1")
	ended := r.goingRun(r.match(0, step(catalog.StepAgent, "")), "terminal", runEnded, time.Hour, "claude", "s-2")
	unrecorded := r.goingRun(r.match(0, step(catalog.StepAgent, "")), runnerBackground, runStarting, 0, "", "")

	got, err := r.svc.RunView(t.Context(), going)
	require.NoError(t, err)
	assert.Equal(t, going, got.Run.ID)
	assert.Equal(t, id, got.Match.ID)
	assert.Equal(t, "first", got.Match.Routine)
	assert.True(t, got.Live)
	assert.True(t, got.CanTakeOver, "Claude Code resumes its conversation in the terminal")

	got, err = r.svc.RunView(t.Context(), ended)
	require.NoError(t, err)
	assert.False(t, got.Live)
	assert.False(t, got.CanTakeOver, "a run in the terminal is the user's already")

	got, err = r.svc.RunView(t.Context(), unrecorded)
	require.NoError(t, err)
	assert.True(t, got.Live)
	assert.False(t, got.CanTakeOver, "no session recorded yet to resume")

	_, err = r.svc.RunView(t.Context(), 999)
	require.ErrorIs(t, err, ErrNoRun)
	assert.Equal(t, "That run is no longer in Autopilot's history.", uierr.Message(err))

	_, err = NewService(Config{}).RunView(t.Context(), going)
	require.ErrorIs(t, err, ErrNoRun)
	assert.Equal(t, "That run isn't in Autopilot's history.", uierr.Message(err))

	assert.False(t, canTakeOver(Run{Runner: runnerBackground, SessionID: "s-3", Agent: "no-such-agent"}),
		"an agent this build doesn't know")
}

func TestASessionsWatchFindsItsRunByTheSession(t *testing.T) {
	t.Parallel()

	r := newStatsRig(t, time.Date(2026, time.November, 16, 9, 0, 0, 0, time.UTC))
	runID := r.goingRun(r.match(0, step(catalog.StepAgent, "")), runnerBackground, runRunning, time.Minute, "claude", "s-1")

	got, err := r.svc.RunBySession(t.Context(), "s-1")
	require.NoError(t, err)
	assert.Equal(t, runID, got)

	for _, session := range []string{"s-other", ""} {
		got, err = r.svc.RunBySession(t.Context(), session)
		require.NoError(t, err)
		assert.Zero(t, got, "%q is no run of Autopilot's", session)
	}

	got, err = NewService(Config{}).RunBySession(t.Context(), "s-1")
	require.NoError(t, err)
	assert.Zero(t, got)

	_, err = NewService(Config{Pool: testutil.ClosedDB(t)}).RunBySession(t.Context(), "s-1")
	assert.Equal(t, "Autopilot's runs couldn't be read.", uierr.Message(err))
}

func TestWorkingShowsEachAgentsRunGoingWithHowFarItHasGot(t *testing.T) {
	t.Parallel()

	r := newStatsRig(t, time.Date(2026, time.November, 16, 9, 0, 0, 0, time.UTC))
	followed := r.goingRun(r.match(0, step(catalog.StepAgent, "")), runnerBackground, runRunning, 2*time.Minute, "", "")
	recorded := r.goingRun(r.match(0, step(catalog.StepAgent, "")), "terminal", runStarting, time.Minute, "", "")
	r.goingRun(r.match(0, step(catalog.StepGitHub, "")), runnerGitHub, runRunning, time.Minute, "", "")

	_, err := r.q.SetRunUsage(t.Context(), queries.SetRunUsageParams{
		TokensIn: 40, TokensOut: 2, CacheReadTokens: 100, UpdatedAt: r.now, ID: recorded,
	})
	require.NoError(t, err)

	progress := runevent.Progress{Step: "Running the tests", TokensIn: 900, Todos: []runevent.Todo{{Text: "Fix lint"}}}
	NewEngine(r.svc, EngineConfig{Items: &quiet{}, Views: &fakeViews{progress: map[int64]runevent.Progress{followed: progress}}})

	got, err := r.svc.Working(t.Context())
	require.NoError(t, err)
	require.Len(t, got.Working, 2, "a GitHub action is no agent at work")

	assert.Equal(t, followed, got.Working[0].RunID)
	assert.Equal(t, runRunning, got.Working[0].Status)
	assert.Equal(t, progress, got.Working[0].Progress, "as its runner follows it")

	assert.Equal(t, recorded, got.Working[1].RunID)
	assert.Equal(t, runevent.Progress{TokensIn: 40, TokensOut: 2, CacheRead: 100, Todos: []runevent.Todo{}},
		got.Working[1].Progress, "its tokens as last recorded, for a run no runner here follows")

	none, err := NewService(Config{}).Working(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []Working{}, none.Working)

	_, err = NewService(Config{Pool: testutil.ClosedDB(t)}).Working(t.Context())
	assert.Equal(t, "The routines' names couldn't be read.", uierr.Message(err))
}

func TestARunsTokensSoFarAreRecordedOnTheLoop(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		logs := &testutil.Logs{}
		r := newStatsRig(t, time.Date(2026, time.November, 16, 9, 0, 0, 0, time.UTC))
		r.svc = NewService(Config{Pool: r.svc.store.db, Clock: r.svc.clock, Prefs: switchedOn(true), Logger: logs.Logger()})
		e := NewEngine(r.svc, EngineConfig{Items: &quiet{}})

		running(t, e)
		synctest.Wait()

		runID := r.goingRun(r.match(0, step(catalog.StepAgent, "")), runnerBackground, runRunning, time.Minute, "", "")

		e.RunUsage(runID, RunUsage{TokensIn: 1200, TokensOut: 30, CacheRead: 9000})
		synctest.Wait()

		got, err := r.svc.Working(t.Context())
		require.NoError(t, err)
		require.Len(t, got.Working, 1)
		assert.Equal(t, runevent.Progress{TokensIn: 1200, TokensOut: 30, CacheRead: 9000, Todos: []runevent.Todo{}},
			got.Working[0].Progress, "what a crash leaves of it, and what the window shows")

		// A write that fails is logged, and the run goes on.
		testutil.Exec(t, r.svc.store.db, `CREATE TRIGGER no_usage BEFORE UPDATE OF tokens_in ON autopilot_runs
			BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)

		e.RunUsage(runID, RunUsage{TokensIn: 1300})
		synctest.Wait()

		assert.Contains(t, logs.String(), "Autopilot could not record a run's tokens")

		got, err = r.svc.Working(t.Context())
		require.NoError(t, err)
		assert.Equal(t, int64(1200), got.Working[0].Progress.TokensIn)
	})
}

// told is a fake of the notifications that records each notice, or fails with err.
type told struct {
	mu      sync.Mutex
	notices []Notice
	err     error
}

func (n *told) Notice(_ context.Context, notice Notice) error {
	n.mu.Lock()
	defer n.mu.Unlock()

	if n.err != nil {
		return n.err
	}

	n.notices = append(n.notices, notice)

	return nil
}

func TestAgentFinishedTellsHowABackgroundRunEndedAndWhatItCost(t *testing.T) {
	t.Parallel()

	r := newStatsRig(t, time.Date(2026, time.November, 16, 9, 0, 0, 0, time.UTC))
	logs := &testutil.Logs{}
	r.svc.logger = logs.Logger()
	sink := &told{}
	e := NewEngine(r.svc, EngineConfig{Items: &quiet{}, Finished: sink})
	run := queries.AutopilotRun{
		ID: 7, MatchID: 3, RoutineID: r.routines[0], Runner: runnerBackground, Kind: "pr", RepoOwner: "acme-org",
		RepoName: "example-repo", Number: 12, Title: "Bump example-lib", QueuedAt: r.now,
	}

	e.tellFinished(t.Context(), run, RunEnd{Outcome: RunDone, Summary: "It fixed the lint.", Cost: new(0.5)})
	e.tellFinished(t.Context(), run, RunEnd{
		Outcome: RunFailed, Error: "it exited with status 2", Cost: new(0.123), CostEstimated: true,
	})

	// A run stopped or interrupted tells nothing, nor does one in the terminal.
	e.tellFinished(t.Context(), run, RunEnd{Outcome: RunStopped})
	e.tellFinished(t.Context(), run, RunEnd{Outcome: RunInterrupted})

	terminal := run
	terminal.Runner = "terminal"
	e.tellFinished(t.Context(), terminal, RunEnd{Outcome: RunDone})

	require.Len(t, sink.notices, 2)
	assert.Equal(t, NoticeFinished, sink.notices[0].Kind)
	assert.Equal(t, "first: It fixed the lint. Cost $0.50.", sink.notices[0].Body)
	assert.Equal(t, "first: It exited with status 2. About $0.12, estimated.", sink.notices[1].Body)
	assert.Equal(t, "acme-org/example-repo", sink.notices[1].Item.Repo)
	assert.Equal(t, "Bump example-lib", sink.notices[1].Title)

	// A routine no longer recorded has no name to head it, and a notice not told is logged.
	gone := run
	gone.RoutineID = 999

	e.tellFinished(t.Context(), gone, RunEnd{Outcome: RunDone, Summary: "Done."})
	require.Len(t, sink.notices, 3)
	assert.Equal(t, ": Done.", sink.notices[2].Body)
	assert.Empty(t, sink.notices[2].Routine)

	sink.err = errBroken

	e.tellFinished(t.Context(), run, RunEnd{Outcome: RunDone})
	assert.Contains(t, logs.String(), "Autopilot could not tell that an agent finished")
}
