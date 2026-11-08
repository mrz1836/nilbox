package store_test

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// parentAndChild returns a fresh database with a parent row and a child row whose
// RESTRICT foreign key names it, a unique name on the parent, and a trigger that refuses
// to rename the parent.
func parentAndChild(t *testing.T) *sql.DB {
	t.Helper()

	db := testutil.DB(t)
	_, err := db.ExecContext(t.Context(), `CREATE TABLE parent (id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE);
CREATE TABLE child (id INTEGER PRIMARY KEY, parent_id INTEGER REFERENCES parent (id) ON DELETE RESTRICT);
INSERT INTO parent (id, name) VALUES (1, 'one');
INSERT INTO child (id, parent_id) VALUES (1, 1);
CREATE TRIGGER refuse_rename BEFORE UPDATE OF name ON parent BEGIN SELECT RAISE(ABORT, 'refused by the test'); END`)
	require.NoError(t, err)

	return db
}

func TestIsForeignKeyViolation(t *testing.T) {
	t.Parallel()

	db := parentAndChild(t)
	ctx := t.Context()

	t.Run("deleting a row a RESTRICT key names", func(t *testing.T) {
		t.Parallel()

		_, err := db.ExecContext(ctx, "DELETE FROM parent WHERE id = 1")
		require.Error(t, err)
		assert.True(t, store.IsForeignKeyViolation(err))
		assert.True(t, store.IsForeignKeyViolation(fmt.Errorf("delete the parent: %w", err)), "found through a wrap")
	})

	t.Run("naming a row that is not there", func(t *testing.T) {
		t.Parallel()

		_, err := db.ExecContext(ctx, "INSERT INTO child (parent_id) VALUES (99)")
		require.Error(t, err)
		assert.True(t, store.IsForeignKeyViolation(err))
	})

	t.Run("other failures are not", func(t *testing.T) {
		t.Parallel()

		_, err := db.ExecContext(ctx, "INSERT INTO parent (name) VALUES ('one')")
		require.Error(t, err, "a unique constraint fails too, but it is not a foreign key")
		assert.False(t, store.IsForeignKeyViolation(err))

		_, err = db.ExecContext(ctx, "UPDATE parent SET name = 'two'")
		require.ErrorContains(t, err, "refused by the test", "a trigger fails like a RESTRICT key, with its own message")
		assert.False(t, store.IsForeignKeyViolation(err))

		_, err = db.ExecContext(ctx, "SELECT * FROM no_such_table")
		require.Error(t, err)
		assert.False(t, store.IsForeignKeyViolation(err))

		assert.False(t, store.IsForeignKeyViolation(errExample), "not a driver error")
		assert.False(t, store.IsForeignKeyViolation(nil))
	})
}

func TestIsBusyIsOnlyALock(t *testing.T) {
	t.Parallel()

	db := parentAndChild(t)

	_, err := db.ExecContext(t.Context(), "DELETE FROM parent WHERE id = 1")
	require.Error(t, err)
	assert.False(t, store.IsBusy(err), "a constraint is not a lock")

	assert.False(t, store.IsBusy(errExample), "not a driver error")
	assert.False(t, store.IsBusy(nil))
}
