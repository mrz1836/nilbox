package migrate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store/migrate"
)

func TestTheAwaitingOnGitHubMigrationKeepsEveryHeldRunUntilTheNextRead(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)

	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{
		Source: embeddedBefore(t, "0023_checks_awaiting_on_github.sql"),
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
	exec(`INSERT INTO item_details (item_id, checks_awaiting, fetched_at) VALUES (7, 2, ` + at + `)`)

	_, err = migrate.Apply(t.Context(), db, path, migrate.Options{})
	require.NoError(t, err)

	// The details stored before the migration keep their held runs as they were counted,
	// and count none approved on GitHub until the next read of them.
	var awaiting, onGitHub int

	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT checks_awaiting, checks_awaiting_on_github FROM item_details WHERE item_id = 7`).Scan(&awaiting, &onGitHub))
	assert.Equal(t, 2, awaiting)
	assert.Zero(t, onGitHub)

	// A NULL and a negative count are refused.
	_, err = db.ExecContext(t.Context(), `UPDATE item_details SET checks_awaiting_on_github = NULL WHERE item_id = 7`)
	require.ErrorContains(t, err, "NOT NULL constraint failed")

	_, err = db.ExecContext(t.Context(), `UPDATE item_details SET checks_awaiting_on_github = -1 WHERE item_id = 7`)
	require.ErrorContains(t, err, "CHECK constraint failed")

	exec(`UPDATE item_details SET checks_awaiting_on_github = 1 WHERE item_id = 7`)
	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT checks_awaiting_on_github FROM item_details WHERE item_id = 7`).Scan(&onGitHub))
	assert.Equal(t, 1, onGitHub)
}
