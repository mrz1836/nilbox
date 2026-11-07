package store_test

import (
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
)

var errExample = errors.New("example failure")

// counterDB returns a fresh database with a one-row counter table set to zero.
func counterDB(t *testing.T) *sql.DB {
	t.Helper()

	db := testutil.SQLDB(t)
	_, err := db.ExecContext(t.Context(), `CREATE TABLE counter (id INTEGER PRIMARY KEY, n INTEGER NOT NULL);
INSERT INTO counter (id, n) VALUES (1, 0)`)
	require.NoError(t, err)

	return db
}

// count reads the counter.
func count(t *testing.T, db store.DBTX) int {
	t.Helper()

	var n int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT n FROM counter WHERE id = 1").Scan(&n))

	return n
}

// increment reads the counter and writes it back one higher, in tx.
func increment(t *testing.T, tx *sql.Tx) error {
	t.Helper()

	var n int
	if err := tx.QueryRowContext(t.Context(), "SELECT n FROM counter WHERE id = 1").Scan(&n); err != nil {
		return err
	}

	_, err := tx.ExecContext(t.Context(), "UPDATE counter SET n = ? WHERE id = 1", n+1)

	return err
}

func TestInTxCommits(t *testing.T) {
	t.Parallel()

	db := counterDB(t)

	require.NoError(t, store.InTx(t.Context(), db, func(tx *sql.Tx) error {
		return increment(t, tx)
	}))
	assert.Equal(t, 1, count(t, db))
}

func TestInTxRollsBackOnError(t *testing.T) {
	t.Parallel()

	db := counterDB(t)

	err := store.InTx(t.Context(), db, func(tx *sql.Tx) error {
		require.NoError(t, increment(t, tx))

		return errExample
	})
	require.ErrorIs(t, err, errExample)
	require.EqualError(t, err, errExample.Error(), "fn's error is returned as it is")
	assert.Zero(t, count(t, db), "nothing fn wrote is kept")
}

func TestInTxRollsBackAndRepanicsOnPanic(t *testing.T) {
	t.Parallel()

	db := counterDB(t)

	assert.PanicsWithValue(t, "example panic", func() {
		_ = store.InTx(t.Context(), db, func(tx *sql.Tx) error {
			require.NoError(t, increment(t, tx))

			panic("example panic")
		})
	})
	assert.Zero(t, count(t, db), "nothing fn wrote is kept")

	// A transaction left open would still hold the write lock, and this one would time
	// out waiting for it.
	require.NoError(t, store.InTx(t.Context(), db, func(tx *sql.Tx) error {
		return increment(t, tx)
	}))
	assert.Equal(t, 1, count(t, db))
}

func TestInTxReportsAFailedCommit(t *testing.T) {
	t.Parallel()

	db := testutil.SQLDB(t)
	_, err := db.ExecContext(t.Context(), `CREATE TABLE parent (id INTEGER PRIMARY KEY);
CREATE TABLE child (parent_id INTEGER NOT NULL REFERENCES parent (id))`)
	require.NoError(t, err)

	// A deferred foreign key is only checked when the transaction commits.
	err = store.InTx(t.Context(), db, func(tx *sql.Tx) error {
		if _, execErr := tx.ExecContext(t.Context(), "PRAGMA defer_foreign_keys = ON"); execErr != nil {
			return execErr
		}

		_, execErr := tx.ExecContext(t.Context(), "INSERT INTO child (parent_id) VALUES (42)")

		return execErr
	})
	require.ErrorContains(t, err, "commit transaction")

	var children int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM child").Scan(&children))
	assert.Zero(t, children, "the failed commit keeps nothing")

	// A connection left inside the failed transaction would refuse to begin another, or
	// hold the write lock this one waits for.
	require.NoError(t, store.InTx(t.Context(), db, func(tx *sql.Tx) error {
		_, execErr := tx.ExecContext(t.Context(), "INSERT INTO parent (id) VALUES (42)")

		return execErr
	}))
}

func TestInTxReportsAFailedBegin(t *testing.T) {
	t.Parallel()

	db := testutil.SQLDB(t)
	require.NoError(t, db.Close())

	called := false
	err := store.InTx(t.Context(), db, func(*sql.Tx) error {
		called = true

		return nil
	})
	require.ErrorContains(t, err, "begin transaction")
	assert.False(t, called)
}

func TestInTxSerializesConcurrentReadThenWrite(t *testing.T) {
	t.Parallel()

	const writers, rounds = 8, 10

	db := counterDB(t)

	// Each transaction reads the counter, then writes it. Begun DEFERRED, two of them
	// could read the same snapshot, and the second to write would fail with
	// SQLITE_BUSY rather than wait; begun IMMEDIATE, they take turns.
	var g errgroup.Group

	for range writers {
		g.Go(func() error {
			for range rounds {
				if err := store.InTx(t.Context(), db, func(tx *sql.Tx) error { return increment(t, tx) }); err != nil {
					return err
				}
			}

			return nil
		})
	}

	require.NoError(t, g.Wait())
	assert.Equal(t, writers*rounds, count(t, db), "no increment is lost")
}
