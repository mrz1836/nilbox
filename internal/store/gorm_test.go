package store_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	settingsmodel "github.com/mrz1836/nilbox/internal/settings/model"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestGormSharesThePool(t *testing.T) {
	t.Parallel()

	pool := testutil.SQLDB(t)

	db, err := store.Gorm(pool)
	require.NoError(t, err)

	got, err := db.DB()
	require.NoError(t, err)
	assert.Same(t, pool, got)

	var foreignKeys int
	require.NoError(t, db.Raw("PRAGMA foreign_keys").Scan(&foreignKeys).Error)
	assert.Equal(t, 1, foreignKeys, "GORM's queries run on the configured connections")
}

func TestGormReportsAClosedPool(t *testing.T) {
	t.Parallel()

	pool := testutil.SQLDB(t)
	require.NoError(t, pool.Close())

	_, err := store.Gorm(pool)
	require.ErrorContains(t, err, "open database for GORM")
}

func TestGormStoresTimestampsAsSortableUTCText(t *testing.T) {
	t.Parallel()

	pool := testutil.SQLDB(t)

	db, err := store.Gorm(pool)
	require.NoError(t, err)

	// GORM stamps each from its clock as it is written.
	for i := range 3 {
		require.NoError(t, db.Create(&settingsmodel.Pref{Key: fmt.Sprintf("example.%d", i), Value: "v"}).Error)
	}

	stamps := storedStamps(t, pool)
	assertUTCText(t, stamps)

	keys, _ := keysAndTexts(stamps)
	assert.Equal(t, []string{"example.0", "example.1", "example.2"}, keys,
		"ordered as text, the stamps are in the order they were written")
}

func TestCloseClosesPool(t *testing.T) {
	t.Parallel()

	opened, err := store.Open(t.Context(), filepath.Join(t.TempDir(), "app.db"))
	require.NoError(t, err)

	pool := opened.DB

	db, err := store.Gorm(pool)
	require.NoError(t, err)
	require.NoError(t, store.Close(db))

	require.Error(t, pool.PingContext(t.Context()))
	require.Error(t, db.Exec("SELECT 1").Error)
	require.NoError(t, store.Close(db), "closing an already closed pool is harmless")
}

func TestSQLTxJoinsTheGormTransaction(t *testing.T) {
	t.Parallel()

	pool := counterDB(t)

	db, err := store.Gorm(pool)
	require.NoError(t, err)

	_, err = store.SQLTx(db.WithContext(t.Context()))
	require.ErrorIs(t, err, store.ErrNotInTx)

	err = db.WithContext(t.Context()).Transaction(func(gtx *gorm.DB) error {
		tx, txErr := store.SQLTx(gtx)
		require.NoError(t, txErr)
		require.NoError(t, increment(t, tx))

		return errExample
	})
	require.ErrorIs(t, err, errExample)
	assert.Zero(t, count(t, pool), "what ran on the transaction rolls back with GORM's")

	require.NoError(t, db.WithContext(t.Context()).Transaction(func(gtx *gorm.DB) error {
		tx, txErr := store.SQLTx(gtx)
		if txErr != nil {
			return txErr
		}

		if txErr = increment(t, tx); txErr != nil {
			return txErr
		}

		return gtx.Exec("UPDATE counter SET n = n + 1 WHERE id = 1").Error
	}))
	assert.Equal(t, 2, count(t, pool), "and commits with GORM's writes")
}
