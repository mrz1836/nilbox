package migrate_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store/migrate"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// TestReviewColumnsStartEmptyOnARowStoredBefore migrates a database to 0113, stores a pull
// request's details, then applies 0114_approving_bots and 0115_review_texts: the row keeps
// every other column, and names no approving bot and counts no review that says something
// until the next read of its details.
func TestReviewColumnsStartEmptyOnARowStoredBefore(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)

	through113, _ := embeddedThrough(t, 113)
	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{Source: through113})
	require.NoError(t, err)

	execAll(t, db,
		`INSERT INTO accounts (login, created_at, updated_at) VALUES ('octo-user', `+at+`, `+at+`)`,
		`INSERT INTO repos (owner, name, created_at, updated_at) VALUES ('acme-org', 'example-repo', `+at+`, `+at+`)`,
		`INSERT INTO items (account_id, repo_id, kind, number, item_created_at, item_updated_at, fetched_at)
			VALUES (1, 1, 'pr', 7, `+at+`, `+at+`, `+at+`)`,
		`INSERT INTO item_details (item_id, approvals, fetched_at) VALUES (1, 1, `+at+`)`,
	)

	_, err = migrate.Apply(t.Context(), db, path, migrate.Options{})
	require.NoError(t, err)

	assert.Equal(t, []string{`[]`}, testutil.Column[string](t, db, `SELECT approving_bots FROM item_details`))
	assert.Equal(t, []int{0}, testutil.Column[int](t, db, `SELECT review_texts FROM item_details`))
	assert.Equal(t, []int{1}, testutil.Column[int](t, db, `SELECT approvals FROM item_details`),
		"the row keeps what it held")
}
