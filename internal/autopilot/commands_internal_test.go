package autopilot

import (
	"context"
	"database/sql"
	"maps"
	"strconv"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// cmdNow is when the commands' tests run.
func cmdNow() time.Time { return time.Date(2026, time.November, 16, 9, 0, 0, 0, time.UTC) }

// The items the commands' rig stores: a pull request and an issue.
const (
	cmdPR    int64 = 5
	cmdIssue int64 = 6
)

// cmdItems is the inbox as the engine's commands read it: the facts of the items it holds,
// and the states stored of them, by ID, or err for every read of them and of their states;
// the rest is quiet's.
type cmdItems struct {
	quiet

	mu     sync.Mutex
	facts  map[int64]facts.Facts
	states map[int64]ItemState
	err    error
}

func (c *cmdItems) Facts(_ context.Context, ids []int64, _ time.Time) ([]facts.Facts, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.err != nil {
		return nil, c.err
	}

	out := []facts.Facts{}

	for _, id := range ids {
		if f, ok := c.facts[id]; ok {
			out = append(out, f)
		}
	}

	return out, nil
}

func (c *cmdItems) ItemStates(context.Context, []int64) (map[int64]ItemState, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	return maps.Clone(c.states), c.err
}

// list has the item id stored as listed in the inbox, so the ledger keeps its matches.
func (c *cmdItems) list(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.states == nil {
		c.states = map[int64]ItemState{}
	}

	c.states[id] = ItemState{State: "OPEN", FetchedAt: cmdNow(), Listed: true}
}

// fail has every read fail with err, or answer again for nil.
func (c *cmdItems) fail(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.err = err
}

// forget leaves the item id out of what is stored.
func (c *cmdItems) forget(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.facts, id)
}

// cmdRig is an engine over a fresh database that stores a pull request (cmdPR) and an
// issue (cmdIssue), with Autopilot on; start runs its loop.
type cmdRig struct {
	t       *testing.T
	db      *sql.DB
	svc     *Service
	e       *Engine
	items   *cmdItems
	logs    *testutil.Logs
	matches int
}

// seedItems stores the pull request cmdPR and the issue cmdIssue in db, as the inbox would,
// and returns their facts.
func seedItems(t *testing.T, db *sql.DB) map[int64]facts.Facts {
	t.Helper()

	now := cmdNow()

	testutil.Exec(t, db, `INSERT INTO accounts (id, login, created_at, updated_at) VALUES (1, 'octo-user', ?, ?)`, now, now)
	testutil.Exec(t, db, `INSERT INTO repos (id, owner, name, created_at, updated_at)
VALUES (1, 'acme-org', 'example-repo', ?, ?)`, now, now)
	testutil.Exec(t, db, `INSERT INTO items (id, account_id, repo_id, kind, number, item_created_at, item_updated_at,
    fetched_at) VALUES (?, 1, 1, 'pr', 5, ?, ?, ?), (?, 1, 1, 'issue', 6, ?, ?, ?)`,
		cmdPR, now, now, now, cmdIssue, now, now, now)

	return map[int64]facts.Facts{
		cmdPR:    {Item: facts.Item{ID: cmdPR, Kind: item.KindPR, Repo: "acme-org/example-repo", Number: 5}},
		cmdIssue: {Item: facts.Item{ID: cmdIssue, Kind: item.KindIssue, Repo: "acme-org/example-repo", Number: 6}},
	}
}

// newCmdRig returns a rig whose engine has cfg's runners and parts.
func newCmdRig(t *testing.T, cfg EngineConfig) *cmdRig {
	t.Helper()

	now := cmdNow()
	db := testutil.DB(t)
	items := &cmdItems{facts: seedItems(t, db)}
	logs := &testutil.Logs{}
	svc := NewService(Config{Pool: db, Prefs: switchedOn(true), Clock: testutil.NewClock(now), Logger: logs.Logger()})

	cfg.Items = items

	return &cmdRig{t: t, db: db, svc: svc, e: NewEngine(svc, cfg), items: items, logs: logs}
}

// start runs the rig's engine until the test ends; call it inside a synctest bubble.
func (r *cmdRig) start() {
	r.t.Helper()

	running(r.t, r.e)
	synctest.Wait()
}

// checksFailed is a routine that tells the user when one of their pull requests' checks
// fail.
func checksFailed() spec.Spec {
	return spec.Spec{
		Version: spec.Version,
		Watch:   spec.Watch{Sources: []spec.Source{{Lists: []catalog.ListSource{catalog.SourceAuthored}}}},
		Trigger: spec.Trigger{Event: catalog.EventChecksFailed},
		Steps:   []spec.Step{{Notify: &spec.NotifyStep{Note: "Have a look"}}},
	}
}

// checks saves the routine Checks over checksFailed, and returns its ID.
func (r *cmdRig) checks() int64 {
	r.t.Helper()

	got, err := r.svc.Save(r.t.Context(), Draft{Name: "Checks", Spec: checksFailed()})
	require.NoError(r.t, err)

	return got.ID
}

// brokenRoutine stores a routine whose definition can't be read, and returns its ID.
func (r *cmdRig) brokenRoutine() int64 {
	r.t.Helper()

	id, err := queries.New(r.db).InsertRoutine(r.t.Context(), queries.InsertRoutineParams{
		AccountLogin: galleryViewer, Name: "Broken", NameKey: "broken", Mode: "auto", Spec: "not a spec", SpecVersion: 2,
		CreatedAt: cmdNow(), UpdatedAt: cmdNow(),
	})
	require.NoError(r.t, err)

	return id
}

// match records the routine's match on the pull request in state, with steps, and returns
// its ID.
func (r *cmdRig) match(routine int64, state State, blocker Blocker, steps ...stepRun) int64 {
	r.t.Helper()

	r.matches++
	now := cmdNow()
	m := &match{
		RoutineID: routine, ItemID: new(cmdPR), AccountLogin: galleryViewer, RepoOwner: "acme-org",
		RepoName: "example-repo", Kind: "pr", Number: 5, FireKey: "appears:item:" + strconv.Itoa(r.matches),
		State: string(state), BlockedBy: string(blocker), CreatedAt: now, UpdatedAt: now, DecidedAt: now,
		steps: steps,
	}

	if !state.Open() {
		m.ClosedAt = new(now.Add(-time.Hour))
	}

	inserted, err := newLedger(r.db, r.svc.clock).insert(r.t.Context(), m)
	require.NoError(r.t, err)
	require.True(r.t, inserted)

	return m.ID
}

// run records a run of runner on the match id in status, and returns its ID.
func (r *cmdRig) run(match, routine int64, runner, status string) int64 {
	r.t.Helper()

	now := cmdNow()

	id, err := queries.New(r.db).InsertRun(r.t.Context(), queries.InsertRunParams{
		MatchID: match, RoutineID: routine, Runner: runner, Status: status, QueuedAt: now, UpdatedAt: now,
	})
	require.NoError(r.t, err)

	return id
}

func TestEveryLedgerActionWithoutAnEngineSaysAutopilotIsntRunning(t *testing.T) {
	t.Parallel()

	svc := NewService(Config{Pool: testutil.DB(t)})
	ctx := t.Context()

	matches, err := svc.PostAsComment(ctx, 1)
	assert.Equal(t, []Match{}, matches)
	require.ErrorIs(t, err, ErrNoEngine)
	assert.Equal(t, "Autopilot isn't running.", uierr.Message(err))

	matches, err = svc.TryAgain(ctx, 1, 2)
	assert.Equal(t, []Match{}, matches)
	require.ErrorIs(t, err, ErrNoEngine)

	for name, act := range map[string]func() error{
		"Skip":          func() error { return testutil.ErrOf(svc.Skip(ctx, 1)) },
		"Run now":       func() error { return testutil.ErrOf(svc.RunNow(ctx, 1, 2, false)) },
		"Let act again": func() error { return svc.LetActAgain(ctx, 1, 2) },
		"Clear history": func() error { return testutil.ErrOf(svc.ClearHistory(ctx)) },
		"Stop":          func() error { return svc.StopRun(ctx, 1) },
		"Stop all":      func() error { return svc.StopAll(ctx) },
	} {
		require.ErrorIs(t, act(), ErrNoEngine, name)
	}
}

func TestALedgerActionBeforeTheEnginesLoopRunsIsRefused(t *testing.T) {
	t.Parallel()

	svc := NewService(Config{Pool: testutil.DB(t), Prefs: switchedOn(true)})
	NewEngine(svc, EngineConfig{Items: &quiet{}})

	ctx := t.Context()

	for name, act := range map[string]func() error{
		"Post as a comment": func() error { return testutil.ErrOf(svc.PostAsComment(ctx, 1)) },
		"Skip":              func() error { return testutil.ErrOf(svc.Skip(ctx, 1)) },
		"Run now":           func() error { return testutil.ErrOf(svc.RunNow(ctx, 1, 2, false)) },
		"Try again":         func() error { return testutil.ErrOf(svc.TryAgain(ctx, 1, 2)) },
		"Let act again":     func() error { return svc.LetActAgain(ctx, 1, 2) },
		"Clear history":     func() error { return testutil.ErrOf(svc.ClearHistory(ctx)) },
	} {
		err := act()
		require.ErrorIs(t, err, ErrNotRunning, name)
		assert.Equal(t, "Autopilot is starting; try again in a moment.", uierr.Message(err), name)
	}
}

func TestSkipClosesAnOpenMatchAsTheUsersAndLeavesAClosedOneAsItIs(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newCmdRig(t, EngineConfig{})
		id := r.checks()
		queued := r.match(id, StateQueued, "", step(catalog.StepNotify, ""))
		done := r.match(id, StateDone, "", step(catalog.StepNotify, ""))
		going := r.match(id, StateRunning, "", step(catalog.StepNotify, ""))
		r.start()

		m, err := r.svc.Skip(t.Context(), queued)
		require.NoError(t, err)
		assert.Equal(t, StateDismissed, m.State)
		assert.Equal(t, ReasonYouSkipped, m.Reason)
		assert.Equal(t, "You skipped it.", m.Words)
		assert.NotNil(t, m.ClosedAt)

		m, err = r.svc.Skip(t.Context(), done)
		require.NoError(t, err)
		assert.Equal(t, StateDone, m.State, "a closed match stays as it closed")

		_, err = r.svc.Skip(t.Context(), going)
		require.ErrorIs(t, err, ErrRunning)
		assert.Equal(t, "It's running: stop the run instead.", uierr.Message(err))

		_, err = r.svc.Skip(t.Context(), 999)
		require.ErrorIs(t, err, ErrNoMatch)
		assert.Equal(t, "That match is gone.", uierr.Message(err))
	})
}

func TestPostAsACommentIsOnlyForAMatchAboutToPostAnAgentsReview(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newCmdRig(t, EngineConfig{})
		id := r.checks()
		review := []stepRun{step(catalog.StepAgent, catalog.OutcomeApprove), step(catalog.StepPostReview, "")}
		review[1].Status = stepPending

		waiting := r.match(id, StateWaiting, BlockAsk, review...)
		waiting2 := r.match(id, StateWaiting, BlockAsk, step(catalog.StepNotify, ""))
		closed := r.match(id, StateDone, "", review...)
		testutil.Exec(t, r.db, `UPDATE autopilot_matches SET step = 1 WHERE id IN (?, ?)`, waiting, closed)
		r.start()

		got, err := r.svc.PostAsComment(t.Context(), waiting)
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, PostAsComment, got[0].Steps[1].PostAs)
		assert.Equal(t, StateQueued, got[0].State, "with the user's OK, it posts as soon as the limits let it")
		assert.True(t, got[0].Approved)

		for _, other := range []int64{waiting2, closed} {
			_, err = r.svc.PostAsComment(t.Context(), other)
			require.ErrorIs(t, err, ErrNoMatch, "match %d", other)
			assert.Equal(t, "It isn't about to post an agent's review.", uierr.Message(err))
		}

		_, err = r.svc.PostAsComment(t.Context(), 999)
		assert.Equal(t, "That match is gone.", uierr.Message(err))
	})
}

func TestRunNowSaysWhyARoutineCantRunOnAnItem(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newCmdRig(t, EngineConfig{})
		id := r.checks()
		broken := r.brokenRoutine()
		r.start()

		ctx := t.Context()

		_, err := r.svc.RunNow(ctx, 999, cmdPR, true)
		require.ErrorIs(t, err, ErrNotFound)
		assert.Equal(t, "The routine was deleted.", uierr.Message(err))

		_, err = r.svc.RunNow(ctx, broken, cmdPR, true)
		require.ErrorIs(t, err, ErrBroken)
		assert.Equal(t, "Its definition can't be read. It can't run.", uierr.Message(err))

		r.items.forget(cmdIssue)
		_, err = r.svc.RunNow(ctx, id, cmdIssue, true)
		require.ErrorIs(t, err, ErrNoItems)
		assert.Equal(t, "The item isn't stored on this Mac.", uierr.Message(err))

		r.items.fail(errBroken)
		_, err = r.svc.RunNow(ctx, id, cmdPR, true)
		require.ErrorIs(t, err, errBroken)
		assert.Equal(t, "The item's facts couldn't be read.", uierr.Message(err))
		r.items.fail(nil)

		// A match the ledger refuses to record is no run.
		testutil.Exec(t, r.db, `CREATE TRIGGER no_matches BEFORE INSERT ON autopilot_matches
			BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)

		_, err = r.svc.RunNow(ctx, id, cmdPR, true)
		require.Error(t, err)
		assert.Equal(t, "The run couldn't be recorded.", uierr.Message(err))

		testutil.Exec(t, r.db, `DROP TRIGGER no_matches`)

		m, err := r.svc.RunNow(ctx, id, cmdPR, true)
		require.NoError(t, err)
		assert.True(t, m.Manual)
		assert.True(t, m.Approved, "a click needs no OK")
		assert.Contains(t, m.FireKey, "manual:", "Run anyway runs once per click")
	})
}

func TestTryAgainResetsTheTriesAndSaysWhatItCouldntDo(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newCmdRig(t, EngineConfig{})
		id := r.checks()
		broken := r.brokenRoutine()
		r.start()

		ctx := t.Context()

		_, err := r.svc.TryAgain(ctx, 999, cmdPR)
		require.ErrorIs(t, err, ErrNotFound)

		opened, err := r.svc.TryAgain(ctx, broken, cmdPR)
		require.NoError(t, err)
		assert.Equal(t, []Match{}, opened, "a routine that won't load opens nothing, its tries reset")

		opened, err = r.svc.TryAgain(ctx, id, cmdPR)
		require.NoError(t, err)
		assert.Equal(t, []Match{}, opened, "it doesn't match the item now: its checks aren't known to fail")

		r.items.fail(errBroken)
		_, err = r.svc.TryAgain(ctx, id, cmdPR)
		require.ErrorIs(t, err, errBroken)
		r.items.fail(nil)

		testutil.Exec(t, r.db, `DROP TABLE autopilot_tries`)

		_, err = r.svc.TryAgain(ctx, id, cmdPR)
		require.Error(t, err)
		assert.Equal(t, "The tries couldn't be reset.", uierr.Message(err))
	})
}

func TestLetActAgainSaysWhenItCouldntLetTheRoutineAct(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newCmdRig(t, EngineConfig{})
		id := r.checks()
		r.start()

		require.NoError(t, r.svc.LetActAgain(t.Context(), id, cmdPR), "an item it never left alone")

		testutil.Exec(t, r.db, `DROP TABLE autopilot_hands_off`)

		err := r.svc.LetActAgain(t.Context(), id, cmdPR)
		require.Error(t, err)
		assert.Equal(t, "The routine couldn't be let act again.", uierr.Message(err))
	})
}

// runLogs is the runner's logs of the agents' runs, which Clear history clears, or fails to
// with err.
type runLogs struct {
	mu      sync.Mutex
	cleared int
	err     error
}

func (l *runLogs) ClearAgentHistory(context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.err != nil {
		return l.err
	}

	l.cleared++

	return nil
}

func TestClearHistoryClearsTheAgentsRunLogsWithTheMatches(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		logs := &runLogs{}
		r := newCmdRig(t, EngineConfig{RunLogs: logs})
		id := r.checks()
		open := r.match(id, StateQueued, "", step(catalog.StepAgent, ""))
		ended := r.run(open, id, runnerBackground, runEnded)
		r.match(id, StateDone, "", step(catalog.StepAgent, ""))
		r.start()

		cleared, err := r.svc.ClearHistory(t.Context())
		require.NoError(t, err)
		assert.Equal(t, Cleared{Removed: 1}, cleared, "the closed match, its item done with; the open one stays")
		assert.Equal(t, 1, logs.cleared)

		view, err := r.svc.RunView(t.Context(), ended)
		require.NoError(t, err)
		assert.True(t, view.Run.LogCleared, "the run that stays says its log is gone")

		// What couldn't be cleared is said.
		logs.err = errBroken
		_, err = r.svc.ClearHistory(t.Context())
		require.ErrorIs(t, err, errBroken)
		assert.Equal(t, "The agents' run logs couldn't be cleared.", uierr.Message(err))

		logs.err = nil

		testutil.Exec(t, r.db, `CREATE TRIGGER no_marks BEFORE UPDATE OF log_cleared ON autopilot_runs
			BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)

		_, err = r.svc.ClearHistory(t.Context())
		require.Error(t, err)
		assert.Equal(t, "The agents' run logs couldn't be cleared.", uierr.Message(err))

		r.items.fail(errBroken)
		_, err = r.svc.ClearHistory(t.Context())
		require.ErrorIs(t, err, errBroken)
		assert.Equal(t, "The history couldn't be cleared.", uierr.Message(err))
	})
}

// stopper is a runner that records the runs it is asked to stop, and every Stop all, or
// fails them with err; it starts nothing.
type stopper struct {
	mu      sync.Mutex
	stopped []int64
	all     int
	err     error
}

func (s *stopper) Dispatch(context.Context, Job) (Result, error) { return Result{}, nil }

func (s *stopper) Stop(_ context.Context, runID int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stopped = append(s.stopped, runID)

	return s.err
}

func (s *stopper) StopAll(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.all++

	return s.err
}

func (s *stopper) Alive(context.Context, int64) (Liveness, error) { return LiveAlive, nil }

func TestStopGoesStraightToTheRunsOwnRunner(t *testing.T) {
	t.Parallel()

	agents, scripts := &stopper{}, &stopper{}
	r := newCmdRig(t, EngineConfig{Runner: agents, Scripts: scripts})
	id := r.checks()
	agent := r.run(r.match(id, StateRunning, "", step(catalog.StepAgent, "")), id, runnerBackground, runRunning)
	script := r.run(r.match(id, StateRunning, "", step(catalog.StepScript, "")), id, runnerScript, runRunning)
	ended := r.run(r.match(id, StateDone, "", step(catalog.StepAgent, "")), id, runnerBackground, runEnded)
	ctx := t.Context()

	// Never queued behind the engine's loop, which doesn't run here.
	require.NoError(t, r.svc.StopRun(ctx, agent))
	require.NoError(t, r.svc.StopRun(ctx, script))
	assert.Equal(t, []int64{agent}, agents.stopped)
	assert.Equal(t, []int64{script}, scripts.stopped)

	for _, runID := range []int64{ended, 999} {
		err := r.svc.StopRun(ctx, runID)
		require.ErrorIs(t, err, ErrNoRun, "run %d", runID)
		assert.Equal(t, "That run isn't going.", uierr.Message(err))
	}

	agents.err = errBroken
	err := r.svc.StopRun(ctx, agent)
	require.ErrorIs(t, err, errBroken)
	assert.Equal(t, "The run couldn't be stopped.", uierr.Message(err))

	// An app with no scripts' runner has none to stop a script with.
	alone := newCmdRig(t, EngineConfig{Runner: &stopper{}})
	aloneID := alone.checks()
	aloneScript := alone.run(alone.match(aloneID, StateRunning, "", step(catalog.StepScript, "")), aloneID, runnerScript,
		runRunning)

	err = alone.svc.StopRun(ctx, aloneScript)
	require.ErrorIs(t, err, ErrNoRunner)
	assert.Equal(t, "There's no run to stop.", uierr.Message(err))
}

func TestStopAllStopsTheAgentsAndTheScriptsAndSaysWhenItCouldnt(t *testing.T) {
	t.Parallel()

	agents, scripts := &stopper{}, &stopper{}
	r := newCmdRig(t, EngineConfig{Runner: agents, Scripts: scripts})

	require.NoError(t, r.svc.StopAll(t.Context()))
	assert.Equal(t, 1, agents.all)
	assert.Equal(t, 1, scripts.all)

	scripts.err = errBroken
	err := r.svc.StopAll(t.Context())
	require.ErrorIs(t, err, errBroken)
	assert.Equal(t, "The runs couldn't be stopped.", uierr.Message(err))
	assert.Equal(t, 2, agents.all, "one runner's failure stops the other's none the less")

	require.NoError(t, newCmdRig(t, EngineConfig{}).svc.StopAll(t.Context()), "with no runner, nothing to stop")
}
