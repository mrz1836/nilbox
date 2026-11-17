package migrate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store/migrate"
)

// seedRunRows stores a routine, and a match of it the runs below are on.
func seedRunRows(t *testing.T, exec func(stmt string, args ...any)) {
	t.Helper()

	exec(`INSERT INTO autopilot_routines (account_login, name, name_key, mode, spec, spec_version, created_at, updated_at)
		VALUES ('octo-user', 'Fix', 'fix', 'auto', '{}', 1, ` + at + `, ` + at + `)`)
	exec(`INSERT INTO autopilot_matches (routine_id, account_login, fire_key, state, created_at, updated_at, decided_at)
		VALUES (1, 'octo-user', 'appears:item', 'done', ` + at + `, ` + at + `, ` + at + `)`)
}

func TestTheScriptRunsMigrationKeepsEveryRunAndTakesAScripts(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)

	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{
		Source: embeddedBefore(t, "0016_autopilot_script_runs.sql"),
	})
	require.NoError(t, err)

	exec := func(stmt string, args ...any) {
		t.Helper()

		_, execErr := db.ExecContext(t.Context(), stmt, args...)
		require.NoError(t, execErr, stmt)
	}

	seedRunRows(t, exec)
	exec(`INSERT INTO autopilot_runs (match_id, routine_id, runner, agent, status, outcome, cost, cost_estimated,
		tokens_in, tokens_out, summary, queued_at, started_at, updated_at)
		VALUES (1, 1, 'background', 'claude', 'ended', 'done', 1.25, 1, 1000, 200, 'It pushed.', ` + at + `, ` + at + `,
		` + at + `)`)
	exec(`INSERT INTO autopilot_runs (match_id, routine_id, runner, github_action, status, queued_at, updated_at)
		VALUES (1, 1, 'github', 'rebase', 'ended', ` + at + `, ` + at + `)`)

	_, err = db.ExecContext(t.Context(), `INSERT INTO autopilot_runs (match_id, routine_id, runner, status, queued_at,
		updated_at) VALUES (1, 1, 'script', 'starting', `+at+`, `+at+`)`)
	require.ErrorContains(t, err, "CHECK constraint failed", "before it, a script's run had no runner")

	_, err = migrate.Apply(t.Context(), db, path, migrate.Options{})
	require.NoError(t, err)

	var (
		count            int
		runner, agent    string
		cost             float64
		tokens, summary  string
		action           string
		indexes, actives int
		estimated        bool
	)

	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM autopilot_runs`).Scan(&count))
	assert.Equal(t, 2, count, "every run is kept")

	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT runner, agent, cost, cost_estimated, tokens_in || '/' || tokens_out, summary FROM autopilot_runs WHERE id = 1`,
	).Scan(&runner, &agent, &cost, &estimated, &tokens, &summary))
	assert.Equal(t, []any{"background", "claude", 1.25, true, "1000/200", "It pushed."},
		[]any{runner, agent, cost, estimated, tokens, summary})

	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT github_action FROM autopilot_runs WHERE id = 2`).Scan(&action))
	assert.Equal(t, "rebase", action)

	exec(`INSERT INTO autopilot_runs (match_id, routine_id, runner, status, script_run_id, queued_at, updated_at)
		VALUES (1, 1, 'script', 'running', 41, ` + at + `, ` + at + `)`)

	_, err = db.ExecContext(t.Context(), `INSERT INTO autopilot_runs (match_id, routine_id, runner, status, queued_at,
		updated_at) VALUES (1, 1, 'robot', 'running', `+at+`, `+at+`)`)
	require.ErrorContains(t, err, "CHECK constraint failed", "a runner there isn't is still refused")

	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND tbl_name = 'autopilot_runs'
		 AND name IN ('autopilot_runs_active_item', 'autopilot_runs_routine_started', 'autopilot_runs_started',
		              'autopilot_runs_match', 'autopilot_runs_script_run')`).Scan(&indexes))
	assert.Equal(t, 5, indexes, "its indexes are made again, and the run engine's run's added")

	// The one active run per item holds: a second going on one item is refused.
	exec(`INSERT INTO accounts (id, login, created_at, updated_at) VALUES (1, 'octo-user', ` + at + `, ` + at + `)`)
	exec(`INSERT INTO repos (id, owner, name, created_at, updated_at) VALUES (1, 'acme-org', 'example-repo', ` + at +
		`, ` + at + `)`)
	exec(`INSERT INTO items (id, account_id, repo_id, kind, number, item_created_at, item_updated_at, fetched_at)
		VALUES (9, 1, 1, 'pr', 9, ` + at + `, ` + at + `, ` + at + `)`)
	exec(`INSERT INTO autopilot_runs (match_id, routine_id, item_id, runner, status, queued_at, updated_at)
		VALUES (1, 1, 9, 'script', 'running', ` + at + `, ` + at + `)`)

	_, err = db.ExecContext(t.Context(), `INSERT INTO autopilot_runs (match_id, routine_id, item_id, runner, status,
		queued_at, updated_at) VALUES (1, 1, 9, 'background', 'starting', `+at+`, `+at+`)`)
	require.ErrorContains(t, err, "UNIQUE constraint failed")

	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT COUNT(*) FROM autopilot_runs WHERE status IN ('starting', 'running', 'stopping')`).Scan(&actives))
	assert.Equal(t, 2, actives)
}
