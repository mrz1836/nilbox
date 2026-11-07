package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ErrNotInTx is returned by SQLTx for a GORM handle outside a transaction.
var ErrNotInTx = errors.New("the GORM handle is not in a transaction")

// Gorm returns a GORM handle over db's connection pool, for code that reads and writes
// through GORM. The handle shares db's connections, pragmas, and pool limits; it never
// logs SQL or its arguments, and the timestamps GORM sets are in UTC. Closing db closes
// the handle too. When the handle cannot be built, GORM closes db.
func Gorm(db *sql.DB) (*gorm.DB, error) {
	gdb, err := gorm.Open(&sqlite.Dialector{Conn: db}, &gorm.Config{
		Logger:  logger.Discard, // never log SQL or its arguments
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, fmt.Errorf("open database for GORM: %w", err)
	}

	return gdb, nil
}

// Close closes the connection pool under a GORM handle. Closing it twice is harmless.
func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("close database: %w", err)
	}

	if err = sqlDB.Close(); err != nil {
		return fmt.Errorf("close database: %w", err)
	}

	return nil
}

// SQLTx returns the database/sql transaction under tx, a handle that GORM's Transaction
// passed in, so code on database/sql can run inside the same transaction: its writes then
// commit or roll back together with GORM's. A handle outside a transaction is
// ErrNotInTx. It goes when nothing runs on GORM any more.
func SQLTx(tx *gorm.DB) (*sql.Tx, error) {
	if sqlTx, ok := tx.Statement.ConnPool.(*sql.Tx); ok {
		return sqlTx, nil
	}

	return nil, ErrNotInTx
}
