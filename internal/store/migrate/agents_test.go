package migrate_test

import (
	"database/sql"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store/migrate"
)

// through returns the shipped migrations up to and including version.
func through(t *testing.T, version int) fstest.MapFS {
	t.Helper()

	all, err := migrate.Load(migrate.Embedded())
	require.NoError(t, err)

	files := fstest.MapFS{}

	for _, m := range all {
		if m.Version > version {
			continue
		}

		data, readErr := fs.ReadFile(migrate.Embedded(), m.File)
		require.NoError(t, readErr)

		files[m.File] = &fstest.MapFile{Data: data}
	}

	return files
}

// execAll runs each statement, failing the test on the first error.
func execAll(t *testing.T, db *sql.DB, stmts ...string) {
	t.Helper()

	for _, stmt := range stmts {
		_, err := db.ExecContext(t.Context(), stmt)
		require.NoError(t, err, stmt)
	}
}

// text reads one text value.
func text(t *testing.T, db *sql.DB, query string, args ...any) string {
	t.Helper()

	var v sql.NullString
	require.NoError(t, db.QueryRowContext(t.Context(), query, args...).Scan(&v), query)

	return v.String
}

func TestAgentsMigrationKeepsEverythingAsClaudes(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)

	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{Source: through(t, 4)})
	require.NoError(t, err)

	const at = "'2026-11-01 10:00:00+00:00'"

	execAll(t, db,
		`INSERT INTO claude_profiles (id, name, config_dir, model, permission_mode, extra_args, env, created_at, updated_at)
			VALUES (3, 'default', '', 'opus', 'full', '[]', '{}', `+at+`, `+at+`),
			(7, 'work', '/srv/home/.claude_work', '', 'acceptEdits', '["--verbose"]', '{"EXAMPLE": "1"}', `+at+`, `+at+`)`,
		`INSERT INTO prefs (key, value, updated_at) VALUES ('profile.default', 'work', `+at+`)`,
		`INSERT INTO prompts (id, name, nickname, applies_to, body, model, effort, extra_args, created_at, updated_at)
			VALUES (1, 'Review', 'Review', 'pr', 'x', 'sonnet', 'low', '["--advisor","fable"]', `+at+`, `+at+`),
			(2, 'Plain', 'Plain', 'any', 'x', '', '', '[]', `+at+`, `+at+`)`,
		`INSERT INTO launch_defaults (owner, name, agent, profile, pr_prompt_id, model, effort, extra_args, created_at, updated_at)
			VALUES ('acme-org', '', 'claude', 'work', 1, 'opus', 'high', '["--add-dir=/srv/shared"]', `+at+`, `+at+`),
			('acme-org', 'example-repo', '', '', NULL, '', '', '[]', `+at+`, `+at+`)`,
		`INSERT INTO worktree_launches (clone_path, worktree, owner, name, kind, number, launched_at)
			VALUES ('/srv/projects/example-repo', 'pr-184', 'acme-org', 'example-repo', 'pr', 184, `+at+`)`,
	)

	res, err := migrate.Apply(t.Context(), db, path, migrate.Options{})
	require.NoError(t, err)
	assert.Contains(t, res.Applied, 5)

	// Every Claude profile keeps its id and settings, now Claude's.
	assert.Equal(t, "claude|default|opus|full", text(t, db,
		`SELECT agent || '|' || name || '|' || model || '|' || permission_mode FROM agent_profiles WHERE id = 3`))
	assert.Equal(t, `claude|work|/srv/home/.claude_work|["--verbose"]|{"EXAMPLE": "1"}`, text(t, db,
		`SELECT agent || '|' || name || '|' || config_dir || '|' || extra_args || '|' || env FROM agent_profiles WHERE id = 7`))

	// The default profile is Claude's.
	assert.Equal(t, "work", text(t, db, `SELECT value FROM prefs WHERE key = 'profile.default.claude'`))
	assert.Equal(t, "0", text(t, db, `SELECT COUNT(*) FROM prefs WHERE key = 'profile.default'`))

	// Each prompt's model and flags are Claude's; one with none has empty maps.
	assert.JSONEq(t, `{"claude":"sonnet"}`, text(t, db, `SELECT models FROM prompts WHERE id = 1`))
	assert.JSONEq(t, `{"claude":["--advisor","fable"]}`, text(t, db, `SELECT agent_args FROM prompts WHERE id = 1`))
	assert.Equal(t, "low", text(t, db, `SELECT effort FROM prompts WHERE id = 1`))
	assert.JSONEq(t, `{}`, text(t, db, `SELECT models FROM prompts WHERE id = 2`))
	assert.JSONEq(t, `{}`, text(t, db, `SELECT agent_args FROM prompts WHERE id = 2`))

	// So are each launch default's profile, model, and flags; the effort stays shared.
	for column, want := range map[string]string{
		"profiles": `{"claude":"work"}`, "models": `{"claude":"opus"}`, "agent_args": `{"claude":["--add-dir=/srv/shared"]}`,
	} {
		assert.JSONEq(t, want, text(t, db, `SELECT `+column+` FROM launch_defaults WHERE name = ''`), column)
		assert.JSONEq(t, `{}`, text(t, db, `SELECT `+column+` FROM launch_defaults WHERE name = 'example-repo'`), column)
	}

	assert.Equal(t, "claude|high|1", text(t, db,
		`SELECT agent || '|' || effort || '|' || pr_prompt_id FROM launch_defaults WHERE name = ''`))

	// A launch is a record of Claude's worktree under the clone, with one session.
	assert.Equal(t, "|worktree-pr-184|claude|1|2026-11-01 10:00:00+00:00|2026-11-01 10:00:00+00:00", text(t, db,
		`SELECT path || '|' || branch || '|' || agent || '|' || launches || '|' || CAST(created_at AS TEXT) || '|' ||
			CAST(last_agent_at AS TEXT) FROM worktree_records WHERE worktree = 'pr-184'`))

	for _, column := range [][2]string{
		{"launch_defaults", "profile"},
		{"launch_defaults", "model"},
		{"launch_defaults", "extra_args"},
		{"prompts", "model"},
		{"prompts", "extra_args"},
	} {
		assert.Equal(t, "0", text(t, db, `SELECT COUNT(*) FROM pragma_table_info(?) WHERE name = ?`, column[0], column[1]),
			"%s.%s is gone", column[0], column[1])
	}

	assert.Equal(t, "0", text(t, db, `SELECT COUNT(*) FROM sqlite_master WHERE name IN ('claude_profiles', 'worktree_launches')`))

	// The new columns refuse what is not a JSON object, and the profiles a blank agent.
	for name, stmt := range map[string]string{
		"profiles that are a list": `UPDATE launch_defaults SET profiles = '["work"]'`,
		"models that are text":     `UPDATE prompts SET models = 'opus'`,
		"flags that are a list":    `UPDATE prompts SET agent_args = '[]'`,
		"a profile with no agent":  `UPDATE agent_profiles SET agent = ''`,
		"one agent's name twice":   `INSERT INTO agent_profiles (agent, name, created_at, updated_at) VALUES ('claude', 'work', ` + at + `, ` + at + `)`,
		"a negative launch count":  `UPDATE worktree_records SET launches = -1`,
	} {
		_, err = db.ExecContext(t.Context(), stmt)
		require.Error(t, err, name)
	}

	execAll(t, db, `INSERT INTO agent_profiles (agent, name, created_at, updated_at) VALUES ('codex', 'work', `+at+`, `+at+`)`)
}
