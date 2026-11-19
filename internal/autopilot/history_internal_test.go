package autopilot

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// explainItems is the inbox as Why didn't it run? reads it: the items a routine's watch
// holds and their facts, or watchErr and factsErr for those reads.
type explainItems struct {
	watched  []int64
	facts    map[int64]facts.Facts
	watchErr error
	factsErr error
}

func (x explainItems) Suggestions(context.Context) (Suggestions, error) { return Suggestions{}, nil }

func (x explainItems) WatchedItems(context.Context, []string, []catalog.ListSource) ([]int64, error) {
	return x.watched, x.watchErr
}

func (x explainItems) Facts(_ context.Context, ids []int64, _ time.Time) ([]facts.Facts, error) {
	out := []facts.Facts{}

	for _, id := range ids {
		if f, ok := x.facts[id]; ok {
			out = append(out, f)
		}
	}

	return out, x.factsErr
}

func (x explainItems) KnownOwners(context.Context) (map[string]bool, error) {
	return map[string]bool{}, nil
}

func (x explainItems) SearchNow(context.Context, []string) (map[string]SearchResult, int, error) {
	return map[string]SearchResult{}, 0, nil
}

func (x explainItems) ReadChecks(context.Context, []int64) (int, error) { return 0, nil }

func TestTheHistoryReadsNothingWithoutADatabase(t *testing.T) {
	t.Parallel()

	svc := NewService(Config{})
	ctx := t.Context()

	page, err := svc.Activity(ctx, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, ActivityPage{Days: []ActivityDay{}, LeftAlone: []LeftAlone{}}, page)

	page, err = svc.ActivityFrom(ctx, 0, 0, 0)
	require.NoError(t, err)
	assert.Equal(t, ActivityPage{Days: []ActivityDay{}, LeftAlone: []LeftAlone{}}, page)

	history, err := svc.ItemHistory(ctx, cmdPR)
	require.NoError(t, err)
	assert.Equal(t, ItemHistory{ItemID: cmdPR, Matches: []Match{}}, history)

	for name, read := range map[string]func() ([]Match, error){
		"latest":    func() ([]Match, error) { return svc.LatestMatches(ctx, []int64{cmdPR}) },
		"needs you": func() ([]Match, error) { return svc.NeedsYou(ctx) },
	} {
		got, readErr := read()
		require.NoError(t, readErr, name)
		assert.Equal(t, []Match{}, got, name)
	}

	names, err := svc.RoutineNames(ctx)
	require.NoError(t, err)
	assert.Equal(t, []RoutineName{}, names)

	status, err := svc.Engine(ctx)
	require.NoError(t, err)
	assert.Equal(t, EngineStatus{Problems: []Problem{}}, status, "no engine, no problems")

	_, err = svc.Get(ctx, 1)
	require.ErrorIs(t, err, ErrNotSetUp)
}

func TestTheHistoryReadsSayWhatCouldntBeRead(t *testing.T) {
	t.Parallel()

	svc := NewService(Config{Pool: testutil.ClosedDB(t)})
	ctx := t.Context()

	for _, c := range []struct {
		name string
		read func() error
		want string
	}{
		{"Activity", func() error { return testutil.ErrOf(svc.Activity(ctx, 0, 0)) }, "Autopilot's history couldn't be read."},
		{"Activity from", func() error { return testutil.ErrOf(svc.ActivityFrom(ctx, 1, 9, 0)) }, "Autopilot's history couldn't be read."},
		{"an item's history", func() error { return testutil.ErrOf(svc.ItemHistory(ctx, cmdPR)) }, "The item's history couldn't be read."},
		{
			"the rows' chips", func() error { return testutil.ErrOf(svc.LatestMatches(ctx, []int64{cmdPR})) },
			"Autopilot's history of the items couldn't be read.",
		},
		{"Needs you", func() error { return testutil.ErrOf(svc.NeedsYou(ctx)) }, "What needs you couldn't be read."},
		{"the names", func() error { return testutil.ErrOf(svc.RoutineNames(ctx)) }, "The routines' names couldn't be read."},
	} {
		err := c.read()
		require.Error(t, err, c.name)
		assert.Equal(t, c.want, uierr.Message(err), c.name)
	}
}

func TestAPageOfTheHistorySaysWhichPartOfItCouldntBeRead(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		name, breaks, want string
	}{
		{"the runs", `DROP TABLE autopilot_runs`, "The runs couldn't be read."},
		{"the effects", `DROP TABLE autopilot_effects`, "What the routines did couldn't be read."},
		{"a match's steps", `UPDATE autopilot_matches SET steps = '"not steps"'`, "A match couldn't be read."},
		{"what the routines leave alone", `DROP TABLE autopilot_hands_off`, "The items the routines leave alone couldn't be read."},
	} {
		r := newCmdRig(t, EngineConfig{})
		r.match(r.checks(), StateQueued, "", step(catalog.StepNotify, ""))
		testutil.Exec(t, r.db, c.breaks)

		_, err := r.svc.Activity(t.Context(), 0, 0)
		require.Error(t, err, c.name)
		assert.Equal(t, c.want, uierr.Message(err), c.name)
	}

	// An item's history and Needs you read the matches' runs as Activity does.
	r := newCmdRig(t, EngineConfig{})
	r.match(r.checks(), StateQueued, "", step(catalog.StepNotify, ""))
	testutil.Exec(t, r.db, `UPDATE autopilot_matches SET steps = '"not steps"'`)

	_, err := r.svc.ItemHistory(t.Context(), cmdPR)
	assert.Equal(t, "A match couldn't be read.", uierr.Message(err))

	_, err = r.svc.NeedsYou(t.Context())
	assert.Equal(t, "A match couldn't be read.", uierr.Message(err))
}

func TestAMatchSaysWhereItIsWhenNothingMoreWasSaid(t *testing.T) {
	t.Parallel()

	for _, c := range []struct {
		state   State
		blocker Blocker
		want    string
	}{
		{StateWaiting, BlockAsk, "Waiting for your OK."},
		{StateBlocked, BlockPaused, "Autopilot is paused."},
		{StateQueued, "", "Queued."},
		{StateRunning, "", "Running."},
		{StateVerifying, "", "Waiting for the checks on its commit."},
		{StateDone, "", "It ran."},
		{StateDismissed, "", "You skipped it."},
		{StateBaseline, "", "It already matched when you chose only what changes from now on."},
		{StateClosed, "", "Closed."},
	} {
		assert.Equal(t, c.want, defaultWords(c.state, c.blocker), c.state)
	}
}

// explainRig is a database holding the pull request cmdPR and the routine Checks, with a
// Service that reads the inbox as items says.
type explainRig struct {
	*cmdRig

	svc     *Service
	routine int64
}

// newExplainRig returns a rig whose inbox is items, nil for none.
func newExplainRig(t *testing.T, items Items) *explainRig {
	t.Helper()

	r := newCmdRig(t, EngineConfig{})
	svc := NewService(Config{Pool: r.db, Items: items, Clock: testutil.NewClock(cmdNow())})

	saved, err := svc.Save(t.Context(), Draft{Name: "Checks", Spec: checksFailed()})
	require.NoError(t, err)

	return &explainRig{cmdRig: r, svc: svc, routine: saved.ID}
}

func TestWhyDidntItRunSaysWhatItCouldntRead(t *testing.T) {
	t.Parallel()

	stored := func() explainItems {
		return explainItems{watched: []int64{cmdPR}, facts: map[int64]facts.Facts{
			cmdPR: {Item: facts.Item{ID: cmdPR, Kind: "pr", Repo: "acme-org/example-repo", Number: 5}},
		}}
	}

	for _, c := range []struct {
		name   string
		items  func() explainItems
		breaks string
		want   string
	}{
		{"the routine's watch", func() explainItems { return explainItems{watchErr: errBroken} }, "", "The routine's items couldn't be read."},
		{"the item's facts", func() explainItems { return explainItems{factsErr: errBroken} }, "", "The item's facts couldn't be read."},
		{"the item's matches", stored, `DROP TABLE autopilot_matches`, "The item's history couldn't be read."},
		{"the item's runs", stored, `DROP TABLE autopilot_runs`, "The item's runs couldn't be read."},
		{"what the routine first saw", stored, `DROP TABLE autopilot_baselines`, "The routine's history couldn't be read."},
		{"the tries", stored, `DROP TABLE autopilot_tries`, "The routine's tries couldn't be read."},
		{"what it leaves alone", stored, `DROP TABLE autopilot_hands_off`, "The items the routine leaves alone couldn't be read."},
	} {
		r := newExplainRig(t, c.items())
		if c.breaks != "" {
			testutil.Exec(t, r.db, c.breaks)
		}

		_, err := r.svc.Explain(t.Context(), r.routine, cmdPR)
		require.Error(t, err, c.name)
		assert.Equal(t, c.want, uierr.Message(err), c.name)
	}

	r := newExplainRig(t, stored())
	r.match(r.routine, StateDone, "", step(catalog.StepNotify, ""))
	testutil.Exec(t, r.db, `UPDATE autopilot_matches SET steps = '"not steps"'`)

	_, err := r.svc.Explain(t.Context(), r.routine, cmdPR)
	assert.Equal(t, "A match couldn't be read.", uierr.Message(err))
}

func TestWhyDidntItRunSaysWhatItCanWithoutTheItemOrTheRoutine(t *testing.T) {
	t.Parallel()

	r := newExplainRig(t, explainItems{})
	ctx := t.Context()

	got, err := r.svc.Explain(ctx, r.routine, cmdPR)
	require.NoError(t, err)
	assert.Equal(t, []string{"It isn't in this routine's watch.", "The item isn't stored on this Mac."}, got.Ledger)
	assert.False(t, got.InWatch)

	_, err = r.svc.Explain(ctx, 999, cmdPR)
	require.ErrorIs(t, err, ErrNotFound)

	broken := r.brokenRoutine()

	got, err = r.svc.Explain(ctx, broken, cmdPR)
	require.NoError(t, err)
	assert.Equal(t, []string{"Its definition can't be read."}, got.Ledger, "a routine that won't load says why alone")

	_, err = newExplainRig(t, nil).svc.Explain(ctx, r.routine, cmdPR)
	require.ErrorIs(t, err, ErrNoItems)
	assert.Equal(t, "The inbox can't be read, so the routine can't be explained.", uierr.Message(err))
}
