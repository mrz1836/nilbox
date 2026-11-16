package autopilot_test

import (
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// insertMatch records an open or closed match of the routine id in state, about no item,
// and returns its ID.
func (r *rig) insertMatch(t *testing.T, id int64, number int, state autopilot.State, blocker autopilot.Blocker) int64 {
	t.Helper()

	now := startedAt()

	res, err := r.db.ExecContext(t.Context(), `INSERT INTO autopilot_matches (routine_id, repo_owner, repo_name, kind,
		number, title, fire_key, state, blocked_by, created_at, updated_at, decided_at)
		VALUES (?, 'acme-org', 'example-repo', 'pr', ?, 'Bump example-lib', ?, ?, ?, ?, ?, ?)`,
		id, number, "appears:item:"+strconv.Itoa(number), string(state), string(blocker), now, now, now)
	require.NoError(t, err)

	match, err := res.LastInsertId()
	require.NoError(t, err)

	return match
}

// insertRun records a run of the match in status by runner, queued at queued and started
// at started (none when zero), and returns its ID.
func (r *rig) insertRun(t *testing.T, routine, match int64, number int, runner, status string, queued, started time.Time,
) int64 {
	t.Helper()

	var startedAt *time.Time
	if !started.IsZero() {
		startedAt = &started
	}

	res, err := r.db.ExecContext(t.Context(), `INSERT INTO autopilot_runs (match_id, routine_id, repo_owner, repo_name,
		kind, number, title, runner, agent, status, queued_at, started_at, updated_at)
		VALUES (?, ?, 'acme-org', 'example-repo', 'pr', ?, 'Bump example-lib', ?, 'claude', ?, ?, ?, ?)`,
		match, routine, number, runner, status, queued, startedAt, queued)
	require.NoError(t, err)

	run, err := res.LastInsertId()
	require.NoError(t, err)

	return run
}

func TestGlanceWithNoRoutine(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)

	got, err := r.svc.Glance(t.Context())
	require.NoError(t, err)
	assert.Equal(t, autopilot.Glance{On: true, Working: []autopilot.WorkingRun{}}, got, "no list is nil")
	assert.False(t, got.KeepsRunning())

	none, err := autopilot.NewService(autopilot.Config{}).Glance(t.Context())
	require.NoError(t, err)
	assert.NotNil(t, none.Working, "a Service with no database has nothing to show")
}

func TestGlanceCountsTheRoutinesWhatWaitsAndWhatWorks(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)
	on := r.save(t, draft("Tell me", notifySpec()))
	off := r.save(t, draft("Fix", agentSpec()))
	gone := r.save(t, draft("Gone", notifySpec()))

	_, err := r.svc.SetEnabled(t.Context(), on.ID, true, autopilot.CurrentRun)
	require.NoError(t, err)
	require.NoError(t, r.svc.Delete(t.Context(), gone.ID))

	got, err := r.svc.Glance(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, got.Routines, "the deleted one doesn't count")
	assert.Equal(t, 1, got.Enabled)
	assert.True(t, got.KeepsRunning(), "a routine on keeps the app running")

	r.insertMatch(t, off.ID, 1, autopilot.StateWaiting, autopilot.BlockAsk)
	r.insertMatch(t, off.ID, 2, autopilot.StateWaiting, autopilot.BlockCatchUp)
	r.insertMatch(t, off.ID, 3, autopilot.StateQueued, autopilot.BlockSlot)
	r.insertMatch(t, off.ID, 4, autopilot.StateDone, "")

	early, late := startedAt().Add(-10*time.Minute), startedAt().Add(-2*time.Minute)
	running := r.insertMatch(t, off.ID, 5, autopilot.StateRunning, "")
	terminal := r.insertMatch(t, off.ID, 6, autopilot.StateRunning, "")
	github := r.insertMatch(t, off.ID, 7, autopilot.StateRunning, "")
	ended := r.insertMatch(t, off.ID, 8, autopilot.StateDone, "")

	first := r.insertRun(t, off.ID, running, 5, "background", "running", early.Add(-time.Second), early)
	second := r.insertRun(t, off.ID, terminal, 6, "terminal", "starting", late, time.Time{})
	r.insertRun(t, off.ID, github, 7, "github", "running", late, late)
	r.insertRun(t, off.ID, ended, 8, "background", "ended", early, early)

	got, err = r.svc.Glance(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 2, got.NeedsYou, "the matches waiting for the user's decision")
	require.Len(t, got.Working, 2, "the agent runs going, not a GitHub action's")

	assert.Equal(t, autopilot.WorkingRun{
		RunID: first, MatchID: running, RoutineID: off.ID, Routine: "Fix", Agent: "claude", Where: "background",
		StartedAt: early, CreatedAt: early.Add(-time.Second),
		Item: autopilot.ItemRef{Kind: "pr", Repo: "acme-org/example-repo", Number: 5, Title: "Bump example-lib"},
	}, normalizeTimes(got.Working[0]), "oldest first")
	assert.Equal(t, second, got.Working[1].RunID)
	assert.True(t, late.Equal(got.Working[1].StartedAt), "one that hasn't said it started counts from its queueing")
	assert.Equal(t, "terminal", got.Working[1].Where)

	_, err = r.svc.SetEnabled(t.Context(), on.ID, false, autopilot.CurrentRun)
	require.NoError(t, err)

	got, err = r.svc.Glance(t.Context())
	require.NoError(t, err)
	assert.Zero(t, got.Enabled)
	assert.True(t, got.KeepsRunning(), "an agent working keeps the app running, every routine off")
}

func TestGlanceCountsOnlyTheRoutinesTheEngineRuns(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)
	mine := r.save(t, draft("Mine", notifySpec()))

	_, err := r.svc.SetEnabled(t.Context(), mine.ID, true, autopilot.CurrentRun)
	require.NoError(t, err)

	// Another account's routine, and one that won't load, both switched on.
	others := r.insertRaw(t, "Theirs", "ask", string(mustSpec(t, notifySpec())))
	broken := r.insertRaw(t, "Broken", "ask", `{"version":1,"watch":`)
	_, err = r.db.ExecContext(t.Context(),
		`UPDATE autopilot_routines SET enabled = 1, enabled_at = ?, account_login = CASE id WHEN ? THEN 'someone-else' ELSE account_login END
		 WHERE id IN (?, ?)`, startedAt(), others, others, broken)
	require.NoError(t, err)

	got, err := r.svc.Glance(t.Context())
	require.NoError(t, err)
	assert.Equal(t, 3, got.Routines, "the group shows for every routine")
	assert.Equal(t, 1, got.Enabled, "but only the account signed in's, that loads, runs")
	assert.True(t, got.KeepsRunning())

	_, err = r.svc.SetEnabled(t.Context(), mine.ID, false, autopilot.CurrentRun)
	require.NoError(t, err)

	got, err = r.svc.Glance(t.Context())
	require.NoError(t, err)
	assert.Zero(t, got.Enabled)
	assert.False(t, got.KeepsRunning(), "another account's routine, on, keeps nothing running")

	_, err = r.svc.SetEnabled(t.Context(), mine.ID, true, autopilot.CurrentRun)
	require.NoError(t, err)
	r.accounts.signIn("")

	got, err = r.svc.Glance(t.Context())
	require.NoError(t, err)
	assert.Zero(t, got.Enabled, "no one signed in: nothing runs")
	assert.False(t, got.KeepsRunning())
}

func TestGlanceWhileAutopilotIsOff(t *testing.T) {
	t.Parallel()

	r := newRig(t, autopilotprefs.ModeAsk)
	mine := r.save(t, draft("Mine", notifySpec()))

	_, err := r.svc.SetEnabled(t.Context(), mine.ID, true, autopilot.CurrentRun)
	require.NoError(t, err)

	// The same data, with Autopilot (beta) off, as a fresh install has it.
	off := autopilot.NewService(autopilot.Config{
		Pool: r.db, Prefs: prefs{cfg: autopilotprefs.Defaults()}, Accounts: r.accounts, Clock: r.clock,
	})

	got, err := off.Glance(t.Context())
	require.NoError(t, err)
	assert.False(t, got.On)
	assert.Equal(t, 1, got.Routines, "the routine stays as it is")
	assert.Zero(t, got.Enabled, "but none runs")
	assert.False(t, got.KeepsRunning(), "a routine on keeps nothing running while Autopilot is off")
	assert.True(t, r.list(t)[0].Enabled, "and its own switch is left on")

	// An agent it started before it was turned off still keeps the app running.
	match := r.insertMatch(t, mine.ID, 5, autopilot.StateRunning, "")
	r.insertRun(t, mine.ID, match, 5, "background", "running", startedAt(), startedAt())

	got, err = off.Glance(t.Context())
	require.NoError(t, err)
	assert.True(t, got.KeepsRunning(), "until it ends")
}

func TestGlanceSaysWhetherAutopilotIsPaused(t *testing.T) {
	t.Parallel()

	cfg := autopilotprefs.Defaults()
	cfg.Paused = true

	svc := autopilot.NewService(autopilot.Config{Pool: testutil.DB(t), Prefs: prefs{cfg: cfg}})

	got, err := svc.Glance(t.Context())
	require.NoError(t, err)
	assert.True(t, got.Paused)
}

func TestGlanceSaysWhyItCantBeRead(t *testing.T) {
	t.Parallel()

	svc := autopilot.NewService(autopilot.Config{Pool: testutil.ClosedDB(t)})

	_, err := svc.Glance(t.Context())
	require.Error(t, err)
}

// normalizeTimes puts w's times in UTC, as the database hands them back in it.
func normalizeTimes(w autopilot.WorkingRun) autopilot.WorkingRun {
	w.StartedAt, w.CreatedAt = w.StartedAt.UTC(), w.CreatedAt.UTC()

	return w
}
