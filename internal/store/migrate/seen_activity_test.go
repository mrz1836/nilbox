package migrate_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store/migrate"
)

func TestTheSeenActivityMigrationCountsEveryStoredItemAsSeen(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)

	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{
		Source: embeddedBefore(t, "0022_seen_activity.sql"),
	})
	require.NoError(t, err)

	exec := func(stmt string) {
		t.Helper()

		_, execErr := db.ExecContext(t.Context(), stmt)
		require.NoError(t, execErr, stmt)
	}

	const later = "'2026-11-02 08:30:00.25+00:00'"

	exec(`INSERT INTO accounts (id, login, created_at, updated_at) VALUES (1, 'octo-user', ` + at + `, ` + at + `)`)
	exec(`INSERT INTO repos (id, owner, name, created_at, updated_at) VALUES (1, 'acme-org', 'example-repo', ` + at +
		`, ` + at + `)`)
	exec(`INSERT INTO items (id, account_id, repo_id, kind, number, author_login, author_type, item_created_at,
		item_updated_at, fetched_at) VALUES (7, 1, 1, 'pr', 7, 'contributor-a', 'User', ` + at + `, ` + at + `, ` + at + `)`)
	exec(`INSERT INTO items (id, account_id, repo_id, kind, number, author_login, author_type, item_created_at,
		item_updated_at, fetched_at) VALUES (8, 1, 1, 'issue', 8, 'contributor-a', 'User', ` + at + `, ` + later + `, ` +
		later + `)`)

	_, err = migrate.Apply(t.Context(), db, path, migrate.Options{})
	require.NoError(t, err)

	// Each item stored before the migration has seen its own activity, so none is unread.
	rows, err := db.QueryContext(t.Context(), `SELECT id, item_updated_at, seen_activity_at FROM items ORDER BY id`)
	require.NoError(t, err)

	defer func() { _ = rows.Close() }()

	var ids []int64

	for rows.Next() {
		var (
			id            int64
			updated, seen time.Time
		)

		require.NoError(t, rows.Scan(&id, &updated, &seen))
		assert.True(t, seen.Equal(updated), "item %d's seen stamp is its activity: %v, not %v", id, updated, seen)

		ids = append(ids, id)
	}

	require.NoError(t, rows.Err())
	assert.Equal(t, []int64{7, 8}, ids)

	// The stamp may be NULL: an item a later refresh stores starts unread.
	exec(`UPDATE items SET seen_activity_at = NULL WHERE id = 7`)
}
