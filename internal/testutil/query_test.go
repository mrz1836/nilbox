package testutil_test

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// exampleRow is a row of the example table queryDB makes.
type exampleRow struct {
	id    int
	label string
	note  *string
}

// queryDB returns a database holding an example table of three rows, the last with no note.
func queryDB(t *testing.T) *sql.DB {
	t.Helper()

	db := testutil.DB(t)
	testutil.Exec(t, db, `CREATE TABLE example (id INTEGER PRIMARY KEY, label TEXT NOT NULL, note TEXT)`)
	testutil.Exec(t, db, `INSERT INTO example (id, label, note) VALUES (1, 'bug', 'first'), (2, 'docs', 'second'),
		(3, 'bug', NULL)`)

	return db
}

// overflowQuery fails on its second row only, as SQLite reads it: abs of the smallest
// integer has no answer, so the failure is reading the rows, not running the query. It
// has no ORDER BY, which would read every row before handing over the first.
const overflowQuery = `SELECT CASE WHEN n = 2 THEN abs(n - 9223372036854775807 - 3) ELSE n END
	FROM (SELECT 1 AS n UNION ALL SELECT 2)`

// exampleFields are the destinations of an exampleRow's columns.
func exampleFields(r *exampleRow) []any { return []any{&r.id, &r.label, &r.note} }

func TestQueryScansEachRowInOrder(t *testing.T) {
	t.Parallel()

	db := queryDB(t)
	first, second := "first", "second"

	assert.Equal(t, []exampleRow{{1, "bug", &first}, {2, "docs", &second}, {3, "bug", nil}},
		testutil.Query(t, db, exampleFields, `SELECT id, label, note FROM example ORDER BY id`))
	assert.Equal(t, []exampleRow{{3, "bug", nil}, {1, "bug", &first}},
		testutil.Query(t, db, exampleFields, `SELECT id, label, note FROM example WHERE label = ? ORDER BY id DESC`, "bug"))
}

func TestQueryOfNoRowsIsAnEmptyList(t *testing.T) {
	t.Parallel()

	got := testutil.Query(t, queryDB(t), exampleFields, `SELECT id, label, note FROM example WHERE id > ?`, 9)

	require.NotNil(t, got, "never nil")
	assert.Empty(t, got)
}

func TestQueryScansAColumnItDoesntKeep(t *testing.T) {
	t.Parallel()

	// The label alone, the id read into a destination of its own, as a test reads one
	// column of a query plan.
	label := func(l *string) []any {
		var id int

		return []any{&id, l}
	}

	assert.Equal(t, []string{"bug", "docs", "bug"},
		testutil.Query(t, queryDB(t), label, `SELECT id, label FROM example ORDER BY id`))
}

func TestColumnReadsOneColumn(t *testing.T) {
	t.Parallel()

	db := queryDB(t)

	assert.Equal(t, []int{1, 2, 3}, testutil.Column[int](t, db, `SELECT id FROM example ORDER BY id`))
	assert.Equal(t, []string{"bug", "docs"},
		testutil.Column[string](t, db, `SELECT label FROM example WHERE id >= ? ORDER BY id DESC`, 2))

	none := testutil.Column[string](t, db, `SELECT label FROM example WHERE id > 9`)
	require.NotNil(t, none, "never nil")
	assert.Empty(t, none)
}

func TestMapKeysTheSecondColumnByTheFirst(t *testing.T) {
	t.Parallel()

	db := queryDB(t)

	assert.Equal(t, map[string]int{"bug": 3, "docs": 2},
		testutil.Map[string, int](t, db, `SELECT label, id FROM example ORDER BY id`),
		"a key repeated holds the last row's value")
	assert.Equal(t, map[string]int{"bug": 2}, testutil.Map[string, int](t, db,
		`SELECT label, COUNT(*) FROM example WHERE label = ? GROUP BY label`, "bug"))

	none := testutil.Map[int, string](t, db, `SELECT id, label FROM example WHERE id > 9`)
	require.NotNil(t, none, "never nil")
	assert.Empty(t, none)
}

func TestQueryFailsTheTest(t *testing.T) {
	t.Parallel()

	db := queryDB(t)
	closed := testutil.ClosedDB(t)

	for _, tc := range []struct {
		name string
		run  func(tb testing.TB)
	}{
		{name: "a query that doesn't prepare", run: func(tb testing.TB) {
			testutil.Query(tb, db, exampleFields, `SELECT id, label, note FROM missing`)
		}},
		{name: "a database it can't use", run: func(tb testing.TB) {
			testutil.Column[int](tb, closed, `SELECT 1`)
		}},
		{name: "a row it can't scan", run: func(tb testing.TB) {
			testutil.Column[int](tb, db, `SELECT label FROM example`)
		}},
		{name: "a row of more columns than fields", run: func(tb testing.TB) {
			testutil.Column[int](tb, db, `SELECT id, label FROM example`)
		}},
		{name: "a row that fails as it is read", run: func(tb testing.TB) {
			testutil.Column[int](tb, db, overflowQuery)
		}},
		{name: "a map of one column", run: func(tb testing.TB) {
			testutil.Map[int, string](tb, db, `SELECT id FROM example`)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.True(t, fails(t, tc.run))
		})
	}
}

// TestTheOverflowFailsAsTheRowsAreRead pins that overflowQuery fails where it says it
// does: the query runs and its first row reads, and only reading on fails.
func TestTheOverflowFailsAsTheRowsAreRead(t *testing.T) {
	t.Parallel()

	rows, err := queryDB(t).QueryContext(t.Context(), overflowQuery)
	require.NoError(t, err)

	defer func() { require.NoError(t, rows.Close()) }()

	require.True(t, rows.Next())

	var n int
	require.NoError(t, rows.Scan(&n))
	assert.Equal(t, 1, n)

	assert.False(t, rows.Next())
	require.ErrorContains(t, rows.Err(), "integer overflow")
}
