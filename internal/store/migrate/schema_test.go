package migrate_test

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store/migrate"
)

// at is the time the schema tests stamp their rows with.
const at = "'2026-11-01 10:00:00+00:00'"

// migratedDB opens a fresh database migrated with the embedded migrations.
func migratedDB(t *testing.T) *sql.DB {
	t.Helper()

	db, path := openDB(t)

	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{})
	require.NoError(t, err)

	return db
}

// execAll runs each statement, failing the test on the first error.
func execAll(t *testing.T, db *sql.DB, stmts ...string) {
	t.Helper()

	for _, stmt := range stmts {
		_, err := db.ExecContext(t.Context(), stmt)
		require.NoError(t, err, stmt)
	}
}

func TestSchemaConstraints(t *testing.T) {
	t.Parallel()

	// SQLite names the CHECK expression or the UNIQUE columns that refused a statement.
	const (
		check  = "CHECK constraint failed: "
		unique = "UNIQUE constraint failed: "
	)

	// jsonObject is the CHECK that keeps a column to a JSON object.
	jsonObject := func(column string) string {
		return check + "json_valid(" + column + ") AND json_type(" + column + ") = 'object'"
	}

	seedLaunchDefaults := `INSERT INTO launch_defaults (owner, name, created_at, updated_at)
		VALUES ('acme-org', '', ` + at + `, ` + at + `)`
	seedPrompt := `INSERT INTO prompts (name, nickname, applies_to, body, created_at, updated_at)
		VALUES ('Review', 'Review', 'pr', 'x', ` + at + `, ` + at + `)`
	seedProfile := `INSERT INTO agent_profiles (agent, name, created_at, updated_at)
		VALUES ('claude', 'work', ` + at + `, ` + at + `)`
	seedWorktree := `INSERT INTO worktree_records (clone_path, worktree, owner, name, kind, number, created_at)
		VALUES ('/srv/projects/example-repo', 'pr-184', 'acme-org', 'example-repo', 'pr', 184, ` + at + `)`
	seedClear := `INSERT INTO gh_thread_clears (account_login, thread_id, repo_owner, repo_name, item_kind, number,
		trigger, action, state, thread_updated_at, queued_at, send_after) VALUES ('octo-user', 20000000001, 'acme-org',
		'example-repo', 'pr', 7, 'dismissed', 'done', 'queued', ` + at + `, ` + at + `, ` + at + `)`
	seedDetails := `INSERT INTO accounts (login, created_at, updated_at) VALUES ('octo-user', ` + at + `, ` + at + `);
		INSERT INTO repos (owner, name, created_at, updated_at) VALUES ('acme-org', 'example-repo', ` + at + `, ` + at + `);
		INSERT INTO items (account_id, repo_id, kind, number, item_created_at, item_updated_at, fetched_at)
			VALUES (1, 1, 'pr', 7, ` + at + `, ` + at + `, ` + at + `);
		INSERT INTO item_details (item_id, fetched_at) VALUES (1, ` + at + `)`
	seedMatch := `INSERT INTO autopilot_routines (name, name_key, mode, spec, created_at, updated_at)
			VALUES ('Fix checks', 'fix checks', 'ask', '{}', ` + at + `, ` + at + `);
		INSERT INTO autopilot_matches (routine_id, fire_key, state, created_at, updated_at, decided_at)
			VALUES (1, 'checks-failed', 'running', ` + at + `, ` + at + `, ` + at + `)`
	run := func(runner string) string {
		return `INSERT INTO autopilot_runs (match_id, routine_id, runner, status, queued_at, updated_at)
			VALUES (1, 1, '` + runner + `', 'queued', ` + at + `, ` + at + `)`
	}

	tests := []struct {
		name string
		// seed stores the rows the statements act on.
		seed string
		// refused is a statement the schema must refuse, with an error containing want.
		refused string
		want    string
		// accepted is a like statement the schema takes.
		accepted string
	}{
		{
			name:     "launch defaults' profiles that are a list",
			seed:     seedLaunchDefaults,
			refused:  `UPDATE launch_defaults SET profiles = '["work"]'`,
			want:     jsonObject("profiles"),
			accepted: `UPDATE launch_defaults SET profiles = '{"claude":"work"}'`,
		},
		{
			name:     "launch defaults' models that are a list",
			seed:     seedLaunchDefaults,
			refused:  `UPDATE launch_defaults SET models = '["opus"]'`,
			want:     jsonObject("models"),
			accepted: `UPDATE launch_defaults SET models = '{"claude":"opus"}'`,
		},
		{
			name:     "launch defaults' flags that are a list",
			seed:     seedLaunchDefaults,
			refused:  `UPDATE launch_defaults SET agent_args = '["--verbose"]'`,
			want:     jsonObject("agent_args"),
			accepted: `UPDATE launch_defaults SET agent_args = '{"claude":["--verbose"]}'`,
		},
		{
			name:     "a prompt's models that are text",
			seed:     seedPrompt,
			refused:  `UPDATE prompts SET models = 'opus'`,
			want:     jsonObject("models"),
			accepted: `UPDATE prompts SET models = '{"claude":"opus"}'`,
		},
		{
			name:     "a prompt's flags that are a list",
			seed:     seedPrompt,
			refused:  `UPDATE prompts SET agent_args = '[]'`,
			want:     jsonObject("agent_args"),
			accepted: `UPDATE prompts SET agent_args = '{"codex":["--search"]}'`,
		},
		{
			name:     "a profile with no agent",
			seed:     seedProfile,
			refused:  `UPDATE agent_profiles SET agent = ''`,
			want:     check + "agent <> ''",
			accepted: `UPDATE agent_profiles SET agent = 'codex'`,
		},
		{
			name: "one agent's profile name twice",
			seed: seedProfile,
			refused: `INSERT INTO agent_profiles (agent, name, created_at, updated_at)
				VALUES ('claude', 'work', ` + at + `, ` + at + `)`,
			want: unique + "agent_profiles.agent, agent_profiles.name",
			// Each agent has its own names, so another agent's profile may share one.
			accepted: `INSERT INTO agent_profiles (agent, name, created_at, updated_at)
				VALUES ('codex', 'work', ` + at + `, ` + at + `)`,
		},
		{
			name:     "a negative launch count",
			seed:     seedWorktree,
			refused:  `UPDATE worktree_records SET launches = -1`,
			want:     check + "launches >= 0",
			accepted: `UPDATE worktree_records SET launches = 2`,
		},
		{
			name: "a second queued clear of one thread",
			seed: seedClear,
			refused: `INSERT INTO gh_thread_clears (account_login, thread_id, repo_owner, repo_name, item_kind, number,
				trigger, action, state, thread_updated_at, queued_at, send_after) VALUES ('Octo-User', 20000000001,
				'acme-org', 'example-repo', 'pr', 7, 'by_hand', 'done', 'queued', ` + at + `, ` + at + `, ` + at + `)`,
			want: unique + "gh_thread_clears.account_login, gh_thread_clears.thread_id",
			// A settled clear of the thread may sit beside the queued one.
			accepted: `INSERT INTO gh_thread_clears (account_login, thread_id, repo_owner, repo_name, item_kind, number,
				trigger, action, state, thread_updated_at, queued_at, send_after, settled_at) VALUES ('octo-user',
				20000000001, 'acme-org', 'example-repo', 'pr', 7, 'merged', 'done', 'kept', ` + at + `, ` + at + `, ` +
				at + `, ` + at + `)`,
		},
		{
			name:     "a settled clear with no time it settled",
			seed:     seedClear,
			refused:  `UPDATE gh_thread_clears SET state = 'skipped'`,
			want:     check + "(state = 'queued') = (settled_at IS NULL)",
			accepted: `UPDATE gh_thread_clears SET state = 'skipped', settled_at = ` + at,
		},
		{
			name:     "a sent clear with no time it was sent",
			seed:     seedClear,
			refused:  `UPDATE gh_thread_clears SET state = 'sent', settled_at = ` + at,
			want:     check + "(state = 'sent') = (sent_at IS NOT NULL)",
			accepted: `UPDATE gh_thread_clears SET state = 'sent', settled_at = ` + at + `, sent_at = ` + at,
		},
		{
			name:     "a clear that does what no clear does",
			seed:     seedClear,
			refused:  `UPDATE gh_thread_clears SET action = 'unsubscribe'`,
			want:     check + "action IN ('done', 'read')",
			accepted: `UPDATE gh_thread_clears SET action = 'read'`,
		},
		{
			name: "a moment cleared by hand",
			seed: seedClear,
			refused: `INSERT INTO gh_thread_moments (account_login, repo_owner, repo_name, item_kind, number, trigger, at,
				created_at) VALUES ('octo-user', 'acme-org', 'example-repo', 'pr', 7, 'by_hand', ` + at + `, ` + at + `)`,
			want: check + "trigger <> '' AND trigger <> 'by_hand'",
			accepted: `INSERT INTO gh_thread_moments (account_login, repo_owner, repo_name, item_kind, number, trigger, at,
				created_at) VALUES ('octo-user', 'acme-org', 'example-repo', 'pr', 7, 'merged', ` + at + `, ` + at + `)`,
		},
		{
			name:     "held runs that are an object",
			seed:     seedDetails,
			refused:  `UPDATE item_details SET checks_held = '{}'`,
			want:     check + "json_valid(checks_held) AND json_type(checks_held) = 'array'",
			accepted: `UPDATE item_details SET checks_held = '[{"workflow":"CI","onGitHub":true}]'`,
		},
		{
			name:     "a negative count of runs held on GitHub",
			seed:     seedDetails,
			refused:  `UPDATE item_details SET checks_awaiting_on_github = -1`,
			want:     check + "checks_awaiting_on_github >= 0",
			accepted: `UPDATE item_details SET checks_awaiting_on_github = 2`,
		},
		{
			name:     "an issue's planning that is a list",
			seed:     seedDetails,
			refused:  `UPDATE item_details SET issue_info = '[]'`,
			want:     jsonObject("issue_info"),
			accepted: `UPDATE item_details SET issue_info = '{"type":"Task"}'`,
		},
		{
			name:     "a run by no runner there is",
			seed:     seedMatch,
			refused:  run("cron"),
			want:     check + "runner IN ('terminal', 'background', 'github', 'script')",
			accepted: run("script"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			db := migratedDB(t)
			execAll(t, db, tt.seed)

			_, err := db.ExecContext(t.Context(), tt.refused)
			require.ErrorContains(t, err, tt.want, tt.refused)

			res, err := db.ExecContext(t.Context(), tt.accepted)
			require.NoError(t, err, tt.accepted)

			changed, err := res.RowsAffected()
			require.NoError(t, err)
			require.Equal(t, int64(1), changed, "the accepted statement changes the seeded row or adds one")
		})
	}
}

// Each situation a pull request can be in has a default prompt of its own, a foreign key
// that keeps the prompt it names, as pr_prompt_id and issue_prompt_id do.
func TestLaunchDefaultsKeepTheirSituationPrompts(t *testing.T) {
	t.Parallel()

	db := migratedDB(t)

	run := func(stmt string, args ...any) error {
		_, err := db.ExecContext(t.Context(), stmt, args...)

		return err
	}

	exec := func(stmt string, args ...any) {
		t.Helper()

		require.NoError(t, run(stmt, args...), stmt)
	}

	exec(`INSERT INTO launch_defaults (owner, name, created_at, updated_at) VALUES ('acme-org', '', ` + at + `, ` + at + `)`)

	for _, column := range []string{"checks_prompt_id", "conflicts_prompt_id", "changes_prompt_id", "rereview_prompt_id"} {
		var (
			stored *int64
			index  int
			id     int64
		)

		require.NoError(t, db.QueryRowContext(t.Context(),
			`SELECT `+column+` FROM launch_defaults WHERE owner = 'acme-org'`).Scan(&stored))
		assert.Nil(t, stored, "%s: a new row inherits", column)

		require.NoError(t, db.QueryRowContext(t.Context(),
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`, "launch_defaults_"+column).Scan(&index))
		assert.Equal(t, 1, index, "%s has an index named like the others", column)

		exec(`INSERT INTO prompts (name, nickname, applies_to, body, created_at, updated_at)
			VALUES ('Fix', ?, 'pr', 'x', `+at+`, `+at+`)`, column)
		require.NoError(t, db.QueryRowContext(t.Context(), `SELECT id FROM prompts WHERE nickname = ?`, column).Scan(&id))
		exec(`UPDATE launch_defaults SET `+column+` = ? WHERE owner = 'acme-org'`, id)

		require.ErrorContains(t, run(`DELETE FROM prompts WHERE id = ?`, id), "FOREIGN KEY constraint failed",
			"%s: a prompt it names stays", column)
		require.ErrorContains(t, run(`UPDATE launch_defaults SET `+column+` = 999 WHERE owner = 'acme-org'`),
			"FOREIGN KEY constraint failed", "%s names only a stored prompt", column)

		exec(`UPDATE launch_defaults SET ` + column + ` = NULL WHERE owner = 'acme-org'`)
		exec(`DELETE FROM prompts WHERE id = ?`, id)
	}
}
