package scriptrun

import (
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// amendTool is the tool the amended runs used.
const amendTool = "mage-x"

// endedRow records a run of kind as going and then as ended with status, its outcome
// outcome, and returns its ID.
func (h *harness) endedRow(kind Kind, status Status, outcome any) int64 {
	h.t.Helper()

	ctx := h.t.Context()

	id, err := h.e.store.insert(ctx, started{
		spec:    Spec{Kind: kind, Title: "Release v1.4.3", Repo: testRepo(), Dir: h.clone(), Argv: []string{"/usr/bin/true"}},
		command: "true", runsIn: RunsInApp, at: time.Now(), result: Result{Tool: amendTool, Intent: map[string]any{"version": "v1.4.3"}},
	})
	require.NoError(h.t, err)

	require.NoError(h.t, h.e.store.finish(ctx, finished{
		id: id, status: status, summary: "v1.4.3 is on GitHub.", at: time.Now(),
		result: Result{
			Tool: amendTool, Intent: map[string]any{"version": "v1.4.3"}, Warnings: []string{"careful"},
			Unrecognized: []string{}, Outcome: outcome,
		},
	}))

	return id
}

func TestAmendRewritesAnEndedRunsOutcome(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	id := h.endedRow(KindRelease, StatusSucceeded, map[string]any{"kind": "released", "follow": "following"})
	before := h.changes.Load()

	var seen Record

	changed, err := h.e.Amend(t.Context(), id, func(rec Record) (Amendment, bool) {
		seen = rec

		return Amendment{Summary: "v1.4.3 is published.", Outcome: map[string]any{"kind": "released", "follow": "published"}}, true
	})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, map[string]any{"kind": "released", "follow": "following"}, seen.Result.Outcome,
		"change is given the run as recorded")

	rec := h.record(id)
	assert.Equal(t, "v1.4.3 is published.", rec.Summary)
	assert.Equal(t, map[string]any{"kind": "released", "follow": "published"}, rec.Result.Outcome)
	assert.Equal(t, map[string]any{"version": "v1.4.3"}, rec.Result.Intent, "the rest of the result stays")
	assert.Equal(t, []string{"careful"}, rec.Result.Warnings)
	assert.Equal(t, StatusSucceeded, rec.Status)
	assert.Equal(t, before+1, h.changes.Load(), "the window hears of it")
}

func TestAmendLeavesARunChangeDeclines(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	id := h.endedRow(KindRelease, StatusSucceeded, map[string]any{"kind": "released"})
	before := h.changes.Load()

	changed, err := h.e.Amend(t.Context(), id, func(Record) (Amendment, bool) {
		return Amendment{Summary: "never written"}, false
	})
	require.NoError(t, err)
	assert.False(t, changed)
	assert.Equal(t, "v1.4.3 is on GitHub.", h.record(id).Summary)
	assert.Equal(t, before, h.changes.Load(), "nothing changed, so nothing is told")
}

func TestAmendRefusesARunGoingOrGone(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	going := h.insertRow(RunsInApp, h.clone(), 0, 0, time.Time{})
	never := func(Record) (Amendment, bool) {
		t.Error("change isn't asked")

		return Amendment{}, false
	}

	_, err := h.e.Amend(t.Context(), going, never)
	require.ErrorIs(t, err, ErrStillGoing, "its end, once recorded, would write over it")

	_, err = h.e.Amend(t.Context(), going+100, never)
	require.ErrorIs(t, err, ErrNotFound)
}

func TestAmendFailsWhenTheRecordCantBeWritten(t *testing.T) {
	t.Parallel()

	var db *sql.DB

	h := newHarness(t, func(cfg *Config) { db = cfg.DB })
	id := h.endedRow(KindRelease, StatusSucceeded, map[string]any{"kind": "released"})

	_, err := db.ExecContext(t.Context(), `CREATE TRIGGER refuse_amend BEFORE UPDATE ON script_runs
		BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)
	require.NoError(t, err)

	changed, err := h.e.Amend(t.Context(), id, func(Record) (Amendment, bool) {
		return Amendment{Summary: "v1.4.3 is published."}, true
	})
	require.Error(t, err)
	assert.False(t, changed)

	_, err = h.e.Amend(t.Context(), id, func(Record) (Amendment, bool) {
		return Amendment{Outcome: func() {}}, true
	})
	require.Error(t, err, "an outcome that can't be encoded")
}

func TestEndedListsAKindsEndedRunsOldestFirst(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	first := h.endedRow(KindRelease, StatusSucceeded, nil)
	h.endedRow(KindScript, StatusSucceeded, nil)
	h.insertRow(RunsInApp, h.clone(), 0, 0, time.Time{})
	second := h.endedRow(KindRelease, StatusFailed, nil)

	got, err := h.e.Ended(t.Context(), KindRelease)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, first, got[0].ID)
	assert.Equal(t, second, got[1].ID)

	none, err := h.e.Ended(t.Context(), KindPull)
	require.NoError(t, err)
	assert.NotNil(t, none)
	assert.Empty(t, none)
}
