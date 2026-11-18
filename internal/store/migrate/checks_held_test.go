package migrate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store/migrate"
)

func TestTheHeldRunsMigrationStartsEveryRowWithNoNames(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)

	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{
		Source: embeddedBefore(t, "0024_checks_held.sql"),
	})
	require.NoError(t, err)

	exec := func(stmt string) {
		t.Helper()

		_, execErr := db.ExecContext(t.Context(), stmt)
		require.NoError(t, execErr, stmt)
	}

	exec(`INSERT INTO accounts (id, login, created_at, updated_at) VALUES (1, 'octo-user', ` + at + `, ` + at + `)`)
	exec(`INSERT INTO repos (id, owner, name, created_at, updated_at) VALUES (1, 'acme-org', 'example-repo', ` + at +
		`, ` + at + `)`)
	exec(`INSERT INTO items (id, account_id, repo_id, kind, number, author_login, author_type, item_created_at,
		item_updated_at, fetched_at) VALUES (7, 1, 1, 'pr', 7, 'contributor-a', 'User', ` + at + `, ` + at + `, ` + at + `)`)
	exec(`INSERT INTO item_details (item_id, checks_awaiting, checks_awaiting_on_github, fetched_at)
		VALUES (7, 1, 1, ` + at + `)`)

	_, err = migrate.Apply(t.Context(), db, path, migrate.Options{})
	require.NoError(t, err)

	// The details stored before the migration keep their counts, with no names until the
	// next read of them.
	var held string

	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT checks_held FROM item_details WHERE item_id = 7`).Scan(&held))
	assert.Equal(t, "[]", held)

	// A NULL, and anything but a JSON array, are refused.
	_, err = db.ExecContext(t.Context(), `UPDATE item_details SET checks_held = NULL WHERE item_id = 7`)
	require.ErrorContains(t, err, "NOT NULL constraint failed")

	for _, bad := range []string{`{}`, `not json`} {
		_, err = db.ExecContext(t.Context(), `UPDATE item_details SET checks_held = ? WHERE item_id = 7`, bad)
		require.ErrorContains(t, err, "CHECK constraint failed", bad)
	}

	exec(`UPDATE item_details SET checks_held = '[{"workflow":"Auto-merge Trigger","onGitHub":true}]' WHERE item_id = 7`)
	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT checks_held FROM item_details WHERE item_id = 7`).Scan(&held))
	assert.JSONEq(t, `[{"workflow":"Auto-merge Trigger","onGitHub":true}]`, held)
}
