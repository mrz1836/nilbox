package autopilot

import (
	"context"
	"database/sql"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// fakeTriage changes nothing of an inbox: it hands record what it is told to, in a
// transaction of its own database, and counts the steps it ran.
type fakeTriage struct {
	db     *sql.DB
	change TriageChange

	mu    sync.Mutex
	asked []TriageRequest
}

func (f *fakeTriage) Apply(ctx context.Context, req TriageRequest,
	record func(ctx context.Context, tx *sql.Tx, change TriageChange) error,
) (TriageChange, error) {
	f.mu.Lock()
	f.asked = append(f.asked, req)
	f.mu.Unlock()

	if !f.change.Changed() {
		return f.change, nil
	}

	return f.change, store.InTx(ctx, f.db, func(tx *sql.Tx) error { return record(ctx, tx, f.change) })
}

func (f *fakeTriage) Undo(context.Context, []TriageUndo, func(context.Context, *sql.Tx, []bool) error) ([]bool, error) {
	return nil, nil
}

func (f *fakeTriage) AsLeft(_ context.Context, undos []TriageUndo) ([]bool, error) {
	return make([]bool, len(undos)), nil
}

// asks returns the steps it ran.
func (f *fakeTriage) asks() []TriageRequest {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]TriageRequest(nil), f.asked...)
}

// triageRig is a database with a routine and its match on an item, and an engine whose
// triage is fake.
type triageRig struct {
	db     *sql.DB
	triage *fakeTriage
	engine *Engine
	job    Job
}

// newTriageRig returns a rig whose triage answers with change.
func newTriageRig(t *testing.T, change TriageChange) *triageRig {
	t.Helper()

	db := testutil.DB(t)
	now := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)

	testutil.Exec(t, db, `INSERT INTO accounts (id, login, created_at, updated_at) VALUES (1, 'octo-user', ?, ?)`, now, now)
	testutil.Exec(t, db, `INSERT INTO repos (id, owner, name, created_at, updated_at)
VALUES (1, 'acme-org', 'example-repo', ?, ?)`, now, now)
	testutil.Exec(t, db, `INSERT INTO items (id, account_id, repo_id, kind, number, item_created_at, item_updated_at,
    fetched_at) VALUES (5, 1, 1, 'pr', 5, ?, ?, ?)`, now, now, now)
	testutil.Exec(t, db, `INSERT INTO autopilot_routines (id, name, name_key, mode, spec, created_at, updated_at)
VALUES (1, 'Snooze', 'snooze', 'auto', '{}', ?, ?)`, now, now)
	testutil.Exec(t, db, `INSERT INTO autopilot_matches (id, routine_id, item_id, fire_key, state, created_at,
    updated_at, decided_at) VALUES (1, 1, 5, 'appears:item', 'queued', ?, ?, ?)`, now, now, now)

	fake := &fakeTriage{db: db, change: change}
	svc := NewService(Config{Pool: db, Clock: testutil.NewClock(now)})

	return &triageRig{
		db: db, triage: fake, engine: NewEngine(svc, EngineConfig{Triage: fake}),
		job: Job{
			MatchID: 1, RoutineID: 1, Item: ItemRef{ID: 5}, Step: spec.Step{Snooze: &spec.SnoozeStep{Days: new(3)}},
		},
	}
}

// effects counts the effects recorded.
func (r *triageRig) effects(t *testing.T) int {
	t.Helper()

	var n int
	require.NoError(t, r.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM autopilot_effects`).Scan(&n))

	return n
}

func TestATriageStepIsRecordedAsAnEffectOnce(t *testing.T) {
	t.Parallel()

	r := newTriageRig(t, TriageChange{Before: `{}`, After: `{"snoozedUntil":"2026-11-19T09:00:00Z"}`})
	d := r.engine.dispatchers[catalog.DispatchTriage]
	require.NotNil(t, d, "the triage runs the triage steps")

	res, err := d.Dispatch(t.Context(), r.job)
	require.NoError(t, err)
	assert.Equal(t, "Snoozed it for 3 days.", res.Summary)
	assert.Equal(t, []TriageRequest{{RoutineID: 1, ItemID: 5, Action: catalog.StepSnooze, Days: 3}}, r.triage.asks())
	assert.Equal(t, 1, r.effects(t), "recorded in the triage's own transaction")

	// A crash after the change was stored, before the loop recorded the step done, runs
	// the step again: its effect is there, so it is done, and nothing changes twice.
	res, err = d.Dispatch(t.Context(), r.job)
	require.NoError(t, err)
	assert.Equal(t, "Snoozed it for 3 days.", res.Summary)
	assert.Len(t, r.triage.asks(), 1)
	assert.Equal(t, 1, r.effects(t))
}

func TestATriageStepPassesOnTheStateItsRoutineDecidedOn(t *testing.T) {
	t.Parallel()

	r := newTriageRig(t, TriageChange{Kept: KeptChanged})
	r.job.Local = `{"dismissedAt":"2026-11-16T09:00:00Z"}`

	res, err := r.engine.dispatchers[catalog.DispatchTriage].Dispatch(t.Context(), r.job)
	require.NoError(t, err)
	assert.Equal(t, r.job.Local, r.triage.asks()[0].Expect, "the step is a compare-and-set on it")
	assert.Equal(t, "Not done: the item changed since this matched.", res.Summary,
		"worded apart from Left alone, which a hands off and Let it act again go with")
	assert.Zero(t, r.effects(t))
}

func TestATriageStepOnItsWayChangesNothingOnceItsRoutineIsLeftAlone(t *testing.T) {
	t.Parallel()

	r := newTriageRig(t, TriageChange{Before: `{}`, After: `{"dismissedAt":"2026-11-16T09:00:00Z"}`})

	// The user took back what the routine did after the cycle decided on the step, so the
	// hands off is there by the time the triage's own transaction runs.
	testutil.Exec(t, r.db, `INSERT INTO autopilot_hands_off (routine_id, item_id, since, why) VALUES (1, 5, ?, 'x')`,
		time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC))

	res, err := r.engine.dispatchers[catalog.DispatchTriage].Dispatch(t.Context(), r.job)
	require.NoError(t, err)
	assert.Equal(t, "Left alone: you took back what it did here.", res.Summary)
	assert.Zero(t, r.effects(t), "the change rolls back with its record")
}

func TestHandsOffTakesBackOnlyWhatTheRoutineDidBeforeTheUsersTriage(t *testing.T) {
	t.Parallel()

	r := newTriageRig(t, TriageChange{})
	at := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)

	testutil.Exec(t, r.db, `INSERT INTO autopilot_matches (id, routine_id, item_id, fire_key, state, created_at,
    updated_at, decided_at) VALUES (2, 1, 5, 'appears:again', 'done', ?, ?, ?)`, at, at, at)

	for _, e := range []struct {
		match int
		at    time.Time
	}{{1, at}, {2, at.Add(2 * time.Minute)}} {
		testutil.Exec(t, r.db, `INSERT INTO autopilot_effects (match_id, routine_id, item_id, action, applied_at)
VALUES (?, 1, 5, 'dismiss', ?)`, e.match, e.at)
	}

	// The user restored it a minute after the first dismissal; the second, on its way then,
	// landed after.
	r.engine.Outcomes(t.Context(), []Outcome{{
		Kind: "restored", At: at.Add(time.Minute),
		Reversed: Reversal{RoutineID: 1, ItemID: 5, Action: catalog.StepDismiss},
	}})

	var reversed []int

	rows, err := r.db.QueryContext(t.Context(), `SELECT match_id FROM autopilot_effects WHERE reversed_at IS NOT NULL`)
	require.NoError(t, err)

	defer func() { require.NoError(t, rows.Close()) }()

	for rows.Next() {
		var id int
		require.NoError(t, rows.Scan(&id))
		reversed = append(reversed, id)
	}

	require.NoError(t, rows.Err())
	assert.Equal(t, []int{1}, reversed, "the step applied after the user's restore keeps its Undo")
}

func TestTheInboxKeepsTheMarkOfARestoreUndoCanStillTakeBack(t *testing.T) {
	t.Parallel()

	r := newTriageRig(t, TriageChange{})
	at := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)

	testutil.Exec(t, r.db, `INSERT INTO autopilot_effects (match_id, step, routine_id, item_id, action, applied_at)
VALUES (1, 0, 1, 5, 'pin', ?), (1, 1, 1, 5, 'restore', ?)`, at, at)

	kept, err := r.engine.KeptMarks(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []int64{5}, kept, "an Undo Activity still offers reads the mark, however old")

	testutil.Exec(t, r.db, `UPDATE autopilot_effects SET undone_at = ? WHERE action = 'restore'`, at)

	kept, err = r.engine.KeptMarks(t.Context())
	require.NoError(t, err)
	assert.Empty(t, kept, "once undone, nothing reads it")
	assert.NotNil(t, kept)
}

func TestATriageStepThatChangesNothingRecordsNothing(t *testing.T) {
	t.Parallel()

	r := newTriageRig(t, TriageChange{Kept: KeptYours})

	res, err := r.engine.dispatchers[catalog.DispatchTriage].Dispatch(t.Context(), r.job)
	require.NoError(t, err)
	assert.Equal(t, "You set it aside yourself, so it's left as you left it.", res.Summary)
	assert.Zero(t, r.effects(t))
}

func TestADispatcherOfItsOwnRunsTheTriageStepsInstead(t *testing.T) {
	t.Parallel()

	r := newTriageRig(t, TriageChange{})
	own := &countingDispatcher{}
	e := NewEngine(r.engine.svc, EngineConfig{
		Triage: r.triage, Dispatchers: map[catalog.Dispatcher]Dispatcher{catalog.DispatchTriage: own},
	})

	assert.Same(t, own, e.dispatchers[catalog.DispatchTriage])
	assert.Nil(t, NewEngine(r.engine.svc, EngineConfig{}).dispatchers[catalog.DispatchTriage], "no triage, no step runs")
}

// countingDispatcher is a dispatcher of a test's own.
type countingDispatcher struct{}

func (*countingDispatcher) Dispatch(context.Context, Job) (Result, error) { return Result{}, nil }

func TestUndoNeedsTheEngineAndTheTriage(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	svc := NewService(Config{Pool: db})

	got, err := svc.Undo(t.Context(), []int64{1})
	require.ErrorIs(t, err, ErrNoEngine)
	assert.Equal(t, emptyUndone(), got, "no list is nil")

	NewEngine(svc, EngineConfig{})

	_, err = svc.Undo(t.Context(), []int64{1})
	require.ErrorIs(t, err, ErrNoTriage)
}

func TestTheWordsOfAnUndoAndOfWhatTheUserTookBack(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		u    Undone
		want string
	}{
		{Undone{PutBack: []int64{1, 2}}, "Put back 2 items."},
		{Undone{PutBack: []int64{1}, Left: []int64{2, 3}}, "Put back 1 item. 2 changed since, so they're left as they are."},
		{Undone{Left: []int64{2}}, "1 changed since, so it's left as it is."},
		{Undone{Skipped: []int64{4}}, "There was nothing left to undo."},
	} {
		assert.Equal(t, c.want, undoneWords(c.u))
	}

	for _, c := range []struct {
		kind   string
		action catalog.StepKind
		want   string
	}{
		{"restored", catalog.StepSnooze, "You brought back what it snoozed."},
		{"restored", catalog.StepDismiss, "You brought back what it dismissed."},
		{"snoozed", catalog.StepSnooze, "You snoozed it again yourself."},
		{"snoozed", catalog.StepRestore, "You snoozed what it brought back."},
		{"dismissed", catalog.StepRestore, "You dismissed what it brought back."},
		{"unpinned", catalog.StepPin, "You unpinned what it pinned."},
		{"unpinned", "", "You took back what it did."},
	} {
		assert.Equal(t, c.want, reversalWords(c.kind, c.action))
	}
}

func TestActivityGroupsTheLocalStepsByRoutineAndDay(t *testing.T) {
	t.Parallel()

	monday := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	tuesday := monday.Add(24 * time.Hour)

	match := func(id, routine int64, number int, at time.Time, effects ...Effect) Match {
		return Match{
			ID: id, RoutineID: routine, Routine: "Routine " + strconv.FormatInt(routine, 10), CreatedAt: at,
			Item: ItemRef{ID: id, Number: number}, Effects: effects,
		}
	}

	snoozed := func(id int64, at time.Time, can bool) Effect {
		return Effect{ID: id, Action: catalog.StepSnooze, AppliedAt: at, CanUndo: can}
	}

	views := []Match{
		match(4, 1, 4, tuesday, snoozed(40, tuesday, true)),
		match(3, 1, 3, monday, snoozed(30, tuesday, false)),
		match(2, 2, 2, monday, Effect{ID: 20, Action: catalog.StepPin, AppliedAt: monday, CanUndo: true}),
		match(1, 1, 1, monday, snoozed(10, monday, true), Effect{
			ID: 11, Action: catalog.StepSnooze, AppliedAt: monday, UndoneAt: new(monday),
		}),
	}

	days := groupLocal(byDay(views, time.UTC), views, time.UTC)
	require.Len(t, days, 2)
	assert.Equal(t, "2026-11-17", days[0].Date)
	require.Len(t, days[0].Groups, 1)

	tue := days[0].Groups[0]
	assert.Equal(t, "Snoozed 2", tue.Words, "a step runs on the day it ran, whichever day its match is under")
	assert.Equal(t, []int64{40}, tue.Undo, "only what Undo can still put back")
	assert.Equal(t, []int64{4, 3}, tue.Matches)
	assert.True(t, tue.Latest.Equal(tuesday))

	mon := days[1].Groups
	require.Len(t, mon, 2)
	assert.Equal(t, "Pinned 1", mon[0].Words, "a line for each routine and step")
	assert.Equal(t, "Snoozed 2", mon[1].Words)
	assert.Equal(t, 1, mon[1].Undone)
	assert.Equal(t, []int64{10}, mon[1].Undo)
	assert.Equal(t, []ItemRef{{ID: 1, Number: 1}}, mon[1].Items, "an item once")

	// A day that has only steps, no match of its own, gets an entry.
	only := []Match{match(9, 1, 9, monday, snoozed(90, tuesday, true))}
	days = groupLocal(byDay(only, time.UTC), only, time.UTC)
	require.Len(t, days, 2)
	assert.Equal(t, "2026-11-17", days[0].Date)
	assert.Empty(t, days[0].Matches)
	assert.NotNil(t, days[0].Matches, "no list is nil")
	assert.Empty(t, days[1].Groups)
	assert.NotNil(t, days[1].Groups)
}
