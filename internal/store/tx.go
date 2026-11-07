package store

import (
	"context"
	"database/sql"
	"fmt"
)

// DBTX is what a query runs on: the pool, a transaction, or a single connection. It
// matches the interface sqlc generates, so generated queries accept any of the three.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// The pool, a transaction, and a connection are all a DBTX.
var (
	_ DBTX = (*sql.DB)(nil)
	_ DBTX = (*sql.Tx)(nil)
	_ DBTX = (*sql.Conn)(nil)
)

// InTx runs fn in a transaction on db and commits it when fn returns nil. When fn
// returns an error, the transaction is rolled back and that error is returned as it is.
// When fn panics, the transaction is rolled back before the panic carries on, so the
// connection goes back to the pool without holding the write lock.
func InTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	// The transaction runs on a connection held here, so a failed commit can still be
	// rolled back on it.
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() { _ = conn.Close() }()

	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	// A no-op once the transaction has committed.
	defer func() { _ = tx.Rollback() }()

	if err = fn(tx); err != nil {
		return err
	}

	if err = tx.Commit(); err != nil {
		// SQLite can keep the transaction open when COMMIT fails, a deferred foreign key
		// violation for one, while database/sql already counts it as finished and will
		// not roll it back. Ending it here keeps the pooled connection from holding the
		// write lock and the uncommitted changes.
		_, _ = conn.ExecContext(context.WithoutCancel(ctx), "ROLLBACK")

		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}
