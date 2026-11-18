package migrate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store/migrate"
)

func TestTheSituationPromptsMigrationAddsFourRestrictedPromptColumns(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)

	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{
		Source: embeddedBefore(t, "0020_situation_prompts.sql"),
	})
	require.NoError(t, err)

	run := func(stmt string, args ...any) error {
		_, execErr := db.ExecContext(t.Context(), stmt, args...)

		return execErr
	}

	exec := func(stmt string, args ...any) {
		t.Helper()

		require.NoError(t, run(stmt, args...), stmt)
	}

	exec(`INSERT INTO prompts (name, nickname, applies_to, body, created_at, updated_at)
		VALUES ('Review', 'Review', 'pr', 'x', ` + at + `, ` + at + `)`)
	exec(`INSERT INTO launch_defaults (owner, name, pr_prompt_id, created_at, updated_at)
		VALUES ('acme-org', '', 1, ` + at + `, ` + at + `)`)

	_, err = migrate.Apply(t.Context(), db, path, migrate.Options{})
	require.NoError(t, err)

	// The launch defaults' prompt for each situation, which the migration adds.
	for _, column := range []string{"checks_prompt_id", "conflicts_prompt_id", "changes_prompt_id", "rereview_prompt_id"} {
		var (
			stored *int64
			index  int
			id     int64
		)

		require.NoError(t, db.QueryRowContext(t.Context(),
			`SELECT `+column+` FROM launch_defaults WHERE owner = 'acme-org'`).Scan(&stored))
		assert.Nil(t, stored, "%s: a row stored before the migration inherits", column)

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
