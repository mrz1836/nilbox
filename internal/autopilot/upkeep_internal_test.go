package autopilot

import (
	"context"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/internal/queries"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// problemsTold is a fake of the problems' notices that records each problem told.
type problemsTold struct {
	mu   sync.Mutex
	told []ProblemNotice
}

func (p *problemsTold) Problem(_ context.Context, n ProblemNotice) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.told = append(p.told, n)

	return nil
}

// matchCount returns how many matches the rig's ledger records.
func (r *cmdRig) matchCount() int {
	r.t.Helper()

	page, err := r.svc.Activity(r.t.Context(), 0, 0)
	require.NoError(r.t, err)

	n := 0
	for _, d := range page.Days {
		n += len(d.Matches)
	}

	return n
}

func TestClearHistoryClearsNothingWhenAPartCantBeCleared(t *testing.T) {
	t.Parallel()

	refused := func(table, event string) string {
		return `CREATE TRIGGER refused BEFORE ` + event + ` ON ` + table + ` BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`
	}

	for _, c := range []struct {
		name, breaks string
	}{
		{"the closed matches can't be read", `DROP TABLE autopilot_effects; DROP TABLE autopilot_runs; DROP TABLE autopilot_matches`},
		{"a match done with can't be removed", refused("autopilot_matches", "DELETE")},
		{"a kept match's trace can't be cleared", refused("autopilot_matches", "UPDATE OF trace")},
		{"a kept run's summary can't be cleared", refused("autopilot_runs", "UPDATE OF summary")},
		{"what deleted routines saw can't be forgotten", `DROP TABLE autopilot_baselines`},
		{"deleted routines' tries can't be forgotten", `DROP TABLE autopilot_tries`},
	} {
		synctest.Test(t, func(t *testing.T) {
			r := newCmdRig(t, EngineConfig{})
			id := r.checks()

			// One match done with, its item gone, and one kept, its item still listed.
			gone := r.match(id, StateDone, "", step(catalog.StepAgent, ""))
			testutil.Exec(t, r.db, `UPDATE autopilot_matches SET item_id = NULL WHERE id = ?`, gone)

			kept := r.match(id, StateDone, "", step(catalog.StepAgent, ""))
			r.run(kept, id, runnerBackground, runEnded)
			r.items.list(cmdPR)
			r.start()

			before := r.matchCount()
			testutil.Exec(t, r.db, c.breaks)

			_, err := r.svc.ClearHistory(t.Context())
			require.Error(t, err, c.name)
			assert.Equal(t, "The history couldn't be cleared.", uierr.Message(err), c.name)

			if c.name != "the closed matches can't be read" {
				assert.Equal(t, before, r.matchCount(), "%s: one transaction, so nothing went", c.name)
			}
		})
	}
}

func TestClearHistoryKeepsAMatchWhoseItemIsStillListedWithoutItsTrace(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		r := newCmdRig(t, EngineConfig{})
		id := r.checks()
		gone := r.match(id, StateDone, "", step(catalog.StepAgent, ""))
		testutil.Exec(t, r.db, `UPDATE autopilot_matches SET item_id = NULL, trace = '[{"path":"trigger"}]'`)

		kept := r.match(id, StateDone, "", step(catalog.StepAgent, ""))
		testutil.Exec(t, r.db, `UPDATE autopilot_matches SET trace = '[{"path":"trigger"}]' WHERE id = ?`, kept)
		r.items.list(cmdPR)
		r.start()

		cleared, err := r.svc.ClearHistory(t.Context())
		require.NoError(t, err)
		assert.Equal(t, Cleared{Removed: 1, Kept: 1}, cleared)

		history, err := r.svc.ItemHistory(t.Context(), cmdPR)
		require.NoError(t, err)
		require.Len(t, history.Matches, 1, "it still blocks its occurrence")
		assert.Equal(t, kept, history.Matches[0].ID)
		assert.Empty(t, history.Matches[0].Trace, "without its trace")

		_, err = r.svc.matchByID(t.Context(), gone)
		require.ErrorIs(t, err, ErrNoMatch, "the one with no item is gone")
	})
}

func TestAPruneThatCantClearWaitsForTheNextHour(t *testing.T) {
	t.Parallel()

	r := newCmdRig(t, EngineConfig{})
	r.match(r.checks(), StateDone, "", step(catalog.StepAgent, ""))

	cfg := autopilotprefs.Defaults()
	cfg.HistoryDays = 0
	now := cmdNow()

	testutil.Exec(t, r.db, `CREATE TRIGGER refused BEFORE DELETE ON autopilot_matches
		BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)

	r.e.prune(t.Context(), now, cfg)
	assert.Contains(t, r.logs.String(), "Autopilot could not prune its history")
	assert.Equal(t, 1, r.matchCount())

	testutil.Exec(t, r.db, `DROP TRIGGER refused`)

	r.e.prune(t.Context(), now.Add(time.Minute), cfg)
	assert.Equal(t, 1, r.matchCount(), "the retention runs at most every hour")

	r.e.prune(t.Context(), now.Add(pruneEvery), cfg)
	assert.Zero(t, r.matchCount())
	assert.Contains(t, r.logs.String(), "Autopilot pruned its history")
}

func TestAProblemWhoseStartCantBeSavedIsToldOnALaterCycle(t *testing.T) {
	t.Parallel()

	sink := &problemsTold{}
	r := newCmdRig(t, EngineConfig{Problems: sink})
	p := &pass{
		e: r.e, now: cmdNow(), inbox: InboxStatus{Viewer: galleryViewer}, byID: map[int64]*routineRun{},
		others: map[int64]queries.AutopilotRoutine{
			7: {ID: 7, Name: "Broken", AccountLogin: galleryViewer, Enabled: true, Spec: "not a spec", CreatedAt: cmdNow()},
			8: {ID: 8, Name: "Off", AccountLogin: galleryViewer, Spec: "not a spec"},
			9: {ID: 9, Name: "Theirs", AccountLogin: "other-user", Enabled: true, Spec: "not a spec"},
		},
	}

	testutil.Exec(t, r.db, `CREATE TRIGGER refused BEFORE INSERT ON prefs BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)

	r.e.updateProblems(t.Context(), p)
	assert.Empty(t, sink.told, "its start is saved before it is told, as its notice is keyed by it")
	assert.Contains(t, r.logs.String(), "could not save Autopilot's problems; telling them on a later cycle")

	testutil.Exec(t, r.db, `DROP TRIGGER refused`)

	r.e.updateProblems(t.Context(), p)
	require.Len(t, sink.told, 1, "only a routine switched on, of the account signed in")
	assert.Equal(t, ProblemWontLoad, sink.told[0].Kind)
	assert.Equal(t, "Broken", sink.told[0].Routine, "named though it won't load")
	assert.True(t, cmdNow().Equal(sink.told[0].RoutineCreated))
	assert.Equal(t, "Its definition can't be read.", sink.told[0].Detail)
}
