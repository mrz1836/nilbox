package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// readOnlyParams open the connection the inspector of a build from source reads the
// database over (internal/devtools): read-only at the file and query_only, so nothing it
// runs can write however it is spelled, waiting up to five seconds for the app's own
// writes.
const readOnlyParams = "mode=ro&_pragma=busy_timeout(5000)&_pragma=query_only(1)"

// ErrNotReadOnly is returned for a query ReadOnlyQuery does not run: more than one
// statement, or one that does not read.
var ErrNotReadOnly = errors.New("only one statement that reads runs here: SELECT, WITH, EXPLAIN, VALUES, or a PRAGMA that sets nothing")

// QueryResult is what a read-only query found: its columns, and at most the rows it was
// allowed, each value as text; Truncated says the query had more.
type QueryResult struct {
	Columns   []string   `json:"columns"`
	Rows      [][]string `json:"rows"`
	Truncated bool       `json:"truncated"`
}

// TableInfo names one of the database's tables and how many rows it holds.
type TableInfo struct {
	Name string `json:"name"`
	Rows int64  `json:"rows"`
}

// ReadOnlyQuery runs query against the database at path, over a connection of its own
// that writes nothing, and returns its columns and at most maxRows of its rows. It is
// for the inspector of a build from source, which reads the live database while the app
// runs; no table's owner reads through it. query must be one statement that reads
// (readOnlyStatement), and the connection refuses every write besides. A value reads as
// text: NULL as "NULL", a blob that is not UTF-8 as its size, a time in UTC.
func ReadOnlyQuery(ctx context.Context, path, query string, maxRows int) (QueryResult, error) {
	if err := readOnlyStatement(query); err != nil {
		return QueryResult{}, err
	}

	db, err := openReadOnly(path)
	if err != nil {
		return QueryResult{}, err
	}

	defer func() { _ = db.Close() }()

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return QueryResult{}, fmt.Errorf("run the query: %w", err)
	}

	defer func() { _ = rows.Close() }()

	return collectRows(rows, maxRows)
}

// Tables lists the tables of the database at path by name, with how many rows each
// holds, as ReadOnlyQuery reads it.
func Tables(ctx context.Context, path string) ([]TableInfo, error) {
	db, err := openReadOnly(path)
	if err != nil {
		return nil, err
	}

	defer func() { _ = db.Close() }()

	names, err := tableNames(ctx, db)
	if err != nil {
		return nil, err
	}

	tables := make([]TableInfo, 0, len(names))

	for _, name := range names {
		info := TableInfo{Name: name}

		// tableNames keeps only plain identifiers, so the name quotes safely.
		count := `SELECT count(*) FROM "` + name + `"`
		if err = db.QueryRowContext(ctx, count).Scan(&info.Rows); err != nil {
			return nil, fmt.Errorf("count the rows of %s: %w", name, err)
		}

		tables = append(tables, info)
	}

	return tables, nil
}

// openReadOnly opens the database at path read-only, as readOnlyParams set out, over one
// connection.
func openReadOnly(path string) (*sql.DB, error) {
	if err := checkPath(path); err != nil {
		return nil, err
	}

	dsn := (&url.URL{Scheme: "file", Path: filepath.ToSlash(path), RawQuery: readOnlyParams}).String()

	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return nil, fmt.Errorf("open the database read-only: %w", err)
	}

	db.SetMaxOpenConns(1)

	return db, nil
}

// tableNames returns the names of the database's tables that are plain identifiers, in
// order.
func tableNames(ctx context.Context, db *sql.DB) ([]string, error) {
	rows, err := db.QueryContext(ctx, "SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name")
	if err != nil {
		return nil, fmt.Errorf("list the tables: %w", err)
	}

	defer func() { _ = rows.Close() }()

	names := []string{}

	for rows.Next() {
		var name string
		if err = rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("list the tables: %w", err)
		}

		if identifierPattern.MatchString(name) {
			names = append(names, name)
		}
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("list the tables: %w", err)
	}

	return names, nil
}

// collectRows reads at most maxRows of rows as text, marking the result truncated when
// rows held more.
func collectRows(rows *sql.Rows, maxRows int) (QueryResult, error) {
	columns, err := rows.Columns()
	if err != nil {
		return QueryResult{}, fmt.Errorf("read the query's columns: %w", err)
	}

	result := QueryResult{Columns: columns, Rows: [][]string{}}
	values := make([]any, len(columns))
	targets := make([]any, len(columns))

	for i := range values {
		targets[i] = &values[i]
	}

	for rows.Next() {
		if len(result.Rows) >= maxRows {
			result.Truncated = true

			break
		}

		if err = rows.Scan(targets...); err != nil {
			return QueryResult{}, fmt.Errorf("read the query's rows: %w", err)
		}

		row := make([]string, len(values))
		for i, value := range values {
			row[i] = valueText(value)
		}

		result.Rows = append(result.Rows, row)
	}

	if err = rows.Err(); err != nil {
		return QueryResult{}, fmt.Errorf("read the query's rows: %w", err)
	}

	return result, nil
}

// valueText is how a query's value reads: NULL as "NULL", a blob as its text when it is
// UTF-8 and as its size when not, a time as RFC 3339 in UTC, anything else as Go prints
// it.
func valueText(value any) string {
	switch v := value.(type) {
	case nil:
		return "NULL"
	case []byte:
		if utf8.Valid(v) {
			return string(v)
		}

		return fmt.Sprintf("<%d bytes>", len(v))
	case time.Time:
		return v.UTC().Format(time.RFC3339Nano)
	default:
		return fmt.Sprint(v)
	}
}

// readOnlyStatement returns ErrNotReadOnly unless query is one statement that reads:
// one that starts SELECT, WITH, EXPLAIN, or VALUES, or a PRAGMA that sets nothing. The
// read-only connection refuses a write whatever this lets through; this keeps the
// inspector to what it is for.
func readOnlyStatement(query string) error {
	statements := splitStatements(query)
	if len(statements) != 1 {
		return fmt.Errorf("%w (found %d statements)", ErrNotReadOnly, len(statements))
	}

	statement := statements[0]

	switch verb := strings.ToUpper(firstWord(statement)); verb {
	case "SELECT", "WITH", "EXPLAIN", "VALUES":
		return nil
	case "PRAGMA":
		if !strings.Contains(statement, "=") {
			return nil
		}

		return fmt.Errorf("%w (a PRAGMA that sets a value)", ErrNotReadOnly)
	default:
		return fmt.Errorf("%w (found %s)", ErrNotReadOnly, verb)
	}
}

// splitStatements returns the statements of query, split at each semicolon outside a
// quoted string or name and outside a comment, each trimmed, its comments blanked, and
// the empty ones left out.
func splitStatements(query string) []string {
	var (
		statements []string
		current    strings.Builder
	)

	flush := func() {
		if s := strings.TrimSpace(current.String()); s != "" {
			statements = append(statements, s)
		}

		current.Reset()
	}

	for i := 0; i < len(query); {
		end, comment := tokenEnd(query, i)

		switch {
		case comment:
			current.WriteByte(' ')
		case query[i] == ';':
			flush()
		default:
			current.WriteString(query[i:end])
		}

		i = end
	}

	flush()

	return statements
}

// tokenEnd returns where the token starting at query[i] ends, and whether it is a
// comment: a quoted string or name (', ", `, or [...]) runs to its closing quote, a
// comment (-- to the line's end, /* to */) to its end, anything else is one byte. One
// left open runs to the end of query.
func tokenEnd(query string, i int) (int, bool) {
	rest := query[i:]

	switch {
	case strings.HasPrefix(rest, "--"):
		return closeAfter(query, i+2, "\n"), true
	case strings.HasPrefix(rest, "/*"):
		return closeAfter(query, i+2, "*/"), true
	}

	switch query[i] {
	case '\'', '"', '`':
		return closeAfter(query, i+1, query[i:i+1]), false
	case '[':
		return closeAfter(query, i+1, "]"), false
	default:
		return i + 1, false
	}
}

// closeAfter returns the index just past the first closing at or after from, or the end
// of query when there is none.
func closeAfter(query string, from int, closing string) int {
	if at := strings.Index(query[from:], closing); at >= 0 {
		return from + at + len(closing)
	}

	return len(query)
}

// firstWord returns the letters statement starts with.
func firstWord(statement string) string {
	end := strings.IndexFunc(statement, func(r rune) bool { return !unicode.IsLetter(r) })
	if end < 0 {
		return statement
	}

	return statement[:end]
}
