package migrate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store/migrate"
)

func TestTheAuthorAvatarMigrationStartsEveryItemWithNoPicture(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)

	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{
		Source: embeddedBefore(t, "0021_author_avatar_url.sql"),
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
		item_updated_at, fetched_at) VALUES (9, 1, 1, 'pr', 9, 'dependabot', 'Bot', ` + at + `, ` + at + `, ` + at + `)`)

	_, err = migrate.Apply(t.Context(), db, path, migrate.Options{})
	require.NoError(t, err)

	// An item stored before the migration has no picture URL until a read asks for one.
	var stored string

	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT author_avatar_url FROM items WHERE id = 9`).Scan(&stored))
	assert.Empty(t, stored)

	// A NULL is refused: an author with no picture is ''.
	_, err = db.ExecContext(t.Context(), `UPDATE items SET author_avatar_url = NULL WHERE id = 9`)
	require.ErrorContains(t, err, "NOT NULL constraint failed")

	exec(`UPDATE items SET author_avatar_url = 'https://avatars.githubusercontent.com/in/102?s=40&v=4' WHERE id = 9`)
	require.NoError(t, db.QueryRowContext(t.Context(),
		`SELECT author_avatar_url FROM items WHERE id = 9`).Scan(&stored))
	assert.Equal(t, "https://avatars.githubusercontent.com/in/102?s=40&v=4", stored)
}
