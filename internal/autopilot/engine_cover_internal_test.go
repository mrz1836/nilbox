package autopilot

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestTheEnginesLoopRunsOnce(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		svc := NewService(Config{Pool: testutil.DB(t), Prefs: switchedOn(true)})
		e := NewEngine(svc, EngineConfig{Items: &quiet{}})

		running(t, e)
		synctest.Wait()

		e.Run(t.Context())

		_, err := svc.Skip(t.Context(), 99)
		require.ErrorIs(t, err, ErrNoMatch, "a later call returned at once, and the first loop goes on")
	})
}

func TestWhatPanicsOnTheEnginesGoroutinesIsRecoveredAndLogged(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		logs := &testutil.Logs{}
		svc := NewService(Config{Pool: testutil.DB(t), Prefs: switchedOn(true), Logger: logs.Logger()})
		e := NewEngine(svc, EngineConfig{Items: &quiet{}})

		running(t, e)
		synctest.Wait()

		err := e.do(t.Context(), func(context.Context) error { panic("a command that panics") })
		require.ErrorIs(t, err, errPanic)
		assert.Equal(t, "Autopilot couldn't do that.", uierr.Message(err))
		assert.Contains(t, logs.String(), "an Autopilot command panicked")

		e.mail.post(func(context.Context) { panic("a result that panics") })
		synctest.Wait()
		assert.Contains(t, logs.String(), "applying an Autopilot result panicked")

		e.goWork(t.Context(), func(context.Context) { panic("a worker that panics") })
		e.work.Wait()
		assert.Contains(t, logs.String(), "an Autopilot worker panicked")

		_, err = svc.Skip(t.Context(), 99)
		require.ErrorIs(t, err, ErrNoMatch, "the loop goes on")
	})
}

func TestALockThatCantBeTakenIsLoggedAndTheEngineHoldsNothing(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(file, nil, 0o600))

		logs := &testutil.Logs{}
		svc := NewService(Config{Pool: testutil.DB(t), Prefs: switchedOn(true), Logger: logs.Logger()})

		// Core builds the searches' snapshot first, which reads the switch, so the engine
		// tries the lock as it is built.
		NewSearches(svc).Rebuild(t.Context())

		e := NewEngine(svc, EngineConfig{Items: &quiet{}, Lock: NewLock(filepath.Join(file, "autopilot.lock"))})

		assert.Contains(t, logs.String(), "could not take Autopilot's lock", "as it is built")

		running(t, e)
		synctest.Wait()

		status, err := svc.Engine(t.Context())
		require.NoError(t, err)
		assert.False(t, status.Holds)

		_, err = svc.Skip(t.Context(), 99)
		require.ErrorIs(t, err, ErrElsewhere, "a lock it can't take is no lock it holds")
	})
}

func TestAnEngineStateThatCantBeReadOrSavedIsLoggedAndTheEngineRunsOn(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		db := testutil.DB(t)
		testutil.Exec(t, db, `ALTER TABLE prefs RENAME TO prefs_away`)

		logs := &testutil.Logs{}
		svc := NewService(Config{Pool: db, Prefs: switchedOn(true), Logger: logs.Logger()})
		e := NewEngine(svc, EngineConfig{Items: &quiet{}})

		running(t, e)
		synctest.Wait()

		assert.Contains(t, logs.String(), "could not read Autopilot's engine state")

		_, err := svc.Skip(t.Context(), 99)
		require.ErrorIs(t, err, ErrNoMatch, "it runs none the less, from an empty state")

		// The loop alone keeps the state, so it saves it there.
		require.NoError(t, e.do(t.Context(), func(ctx context.Context) error {
			e.saveState(ctx)

			return nil
		}))
		assert.Contains(t, logs.String(), "could not save Autopilot's engine state", "the next cycle saves it again")
	})
}

func TestAnEngineStateThatIsntJSONIsStartedAgain(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	testutil.Exec(t, db, `INSERT INTO prefs (key, value, updated_at) VALUES (?, 'not json', ?)`, KeyEngine, cmdNow())

	e := NewEngine(NewService(Config{Pool: db}), EngineConfig{Items: &quiet{}})

	st, err := readEngineState(t.Context(), e.prefs)
	require.NoError(t, err)
	assert.Equal(t, engineState{FirstPass: map[int64]string{}, FromNow: map[int64]fromNow{}, CatchingUp: map[int64]bool{}}, st)
}

func TestTurnedOnAgainWhileItDrainsTheEngineRunsOnWithTheLockItHeld(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "autopilot.lock")
		prefs := switchedOn(true)
		r := newStatsRig(t, cmdNow())
		logs := &testutil.Logs{}
		r.svc = NewService(Config{Pool: r.svc.store.db, Clock: r.svc.clock, Prefs: prefs, Logger: logs.Logger()})
		e := NewEngine(r.svc, EngineConfig{Items: &quiet{}, Lock: NewLock(path), Runner: &stopper{}})

		running(t, e)
		synctest.Wait()

		// A run goes on as Autopilot is turned off, so this copy drains, holding the lock.
		r.goingRun(r.match(0, step(catalog.StepAgent, "")), runnerBackground, runRunning, time.Minute, "", "")

		prefs.on.Store(false)
		e.Wake()
		synctest.Wait()
		assert.False(t, lockFree(t, path), "it lets go once nothing it started still goes")

		// The runs going can't be read: it keeps the lock, and says so.
		testutil.Exec(t, r.svc.store.db, `ALTER TABLE autopilot_runs RENAME TO autopilot_runs_away`)
		e.Wake()
		synctest.Wait()
		assert.Contains(t, logs.String(), "Autopilot could not read the runs going")
		assert.False(t, lockFree(t, path))

		testutil.Exec(t, r.svc.store.db, `ALTER TABLE autopilot_runs_away RENAME TO autopilot_runs`)

		prefs.on.Store(true)
		e.Wake()
		synctest.Wait()

		assert.True(t, e.Enabled())
		assert.False(t, lockFree(t, path))

		_, err := r.svc.Skip(t.Context(), 99)
		require.ErrorIs(t, err, ErrNoMatch, "it takes commands again")
	})
}

func TestTurnedOffWhileAnotherCopyHoldsTheEngineItNoLongerSaysSo(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "autopilot.lock")
		other := NewLock(path)
		held, err := other.TryLock()
		require.NoError(t, err)
		require.True(t, held)
		t.Cleanup(other.Unlock)

		prefs := switchedOn(true)
		svc := NewService(Config{Pool: testutil.DB(t), Prefs: prefs})
		e := NewEngine(svc, EngineConfig{Items: &quiet{}, Lock: NewLock(path)})

		running(t, e)
		synctest.Wait()

		status, err := svc.Engine(t.Context())
		require.NoError(t, err)
		assert.True(t, status.Elsewhere)

		prefs.on.Store(false)
		e.Wake()
		synctest.Wait()

		status, err = svc.Engine(t.Context())
		require.NoError(t, err)
		assert.False(t, status.Elsewhere, "off, nothing runs anywhere for it to say")
		assert.False(t, status.Holds)
	})
}

func TestWhileAnotherCopyHoldsTheEngineAGlanceThatCantBeReadIsLogged(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "autopilot.lock")
		other := NewLock(path)
		_, err := other.TryLock()
		require.NoError(t, err)
		t.Cleanup(other.Unlock)

		logs := &testutil.Logs{}
		db := testutil.DB(t)
		svc := NewService(Config{Pool: db, Prefs: switchedOn(true), Logger: logs.Logger()})
		e := NewEngine(svc, EngineConfig{Items: &quiet{}, Lock: NewLock(path)})

		running(t, e)
		synctest.Wait()

		testutil.Exec(t, db, `DROP TABLE autopilot_effects; DROP TABLE autopilot_runs`)
		time.Sleep(DefaultTick)
		synctest.Wait()

		assert.Contains(t, logs.String(), "could not read Autopilot while another copy runs it")
	})
}

func TestWhatTheEngineCantReadOrRecordOfTheLedgerIsSaid(t *testing.T) {
	t.Parallel()

	logs := &testutil.Logs{}
	e := NewEngine(NewService(Config{Pool: testutil.ClosedDB(t), Logger: logs.Logger()}), EngineConfig{Items: &quiet{}})

	_, err := e.KeptItems(t.Context())
	require.Error(t, err, "the inbox's prune keeps nothing it can't be told of")

	e.Outcomes(t.Context(), []Outcome{
		{Kind: "done", Reversed: Reversal{RoutineID: 1, ItemID: cmdPR, Action: catalog.StepPin}},
		{Kind: "closed"},
	})
	assert.Contains(t, logs.String(), "Autopilot could not record what the user took back of a routine's")
}
