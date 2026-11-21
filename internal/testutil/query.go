package testutil

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/require"
)

// Query runs query on db with args, for a check of what the code under test stored, and
// returns its rows in order, each scanned into a T through the destinations fields gives
// for it:
//
//	testutil.Query(t, db, func(r *row) []any { return []any{&r.id, &r.label} }, `SELECT id, label FROM ...`)
//
// fields may also hand Scan a destination of its own for a column the test doesn't keep.
// Query fails the test, naming the query, when the query, a row's scan, or reading the rows
// fails; it closes the rows, and returns an empty list, never nil, for none.
func Query[T any](t testing.TB, db *sql.DB, fields func(*T) []any, query string, args ...any) []T {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), query, args...)
	require.NoError(t, err, query)

	defer func() { require.NoError(t, rows.Close(), query) }()

	out := []T{}

	for rows.Next() {
		var row T
		require.NoError(t, rows.Scan(fields(&row)...), query)

		out = append(out, row)
	}

	require.NoError(t, rows.Err(), query)

	return out
}

// Column runs query on db with args, which selects one column, and returns its values in
// order, as Query does: an empty list, never nil, for none.
func Column[T any](t testing.TB, db *sql.DB, query string, args ...any) []T {
	t.Helper()

	return Query(t, db, func(v *T) []any { return []any{v} }, query, args...)
}

// Map runs query on db with args, which selects two columns, and returns the second keyed
// by the first, as Query reads them: a key a later row repeats holds that row's value, and
// none is an empty map, never nil.
func Map[K comparable, V any](t testing.TB, db *sql.DB, query string, args ...any) map[K]V {
	t.Helper()

	rows := Query(t, db, func(e *entry[K, V]) []any { return []any{&e.key, &e.value} }, query, args...)

	out := make(map[K]V, len(rows))
	for _, e := range rows {
		out[e.key] = e.value
	}

	return out
}

// entry is one row Map reads: its key and its value.
type entry[K comparable, V any] struct {
	key   K
	value V
}
