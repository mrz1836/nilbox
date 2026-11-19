package migrate_test

import (
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store/migrate"
)

// freeze names the newest migration of a history to freeze before a squash retires it.
var freeze = flag.Int("freeze", 0, //nolint:gochecknoglobals // a test flag
	"write squashed/NNNN.json and NNNN.schema, the history of the embedded migrations through version NNNN and "+
		"the schema they build, before a squash combines them (docs/architecture.md, internal/store)")

// squashedDir holds the squashed histories, beside the migrations.
const squashedDir = "squashed"

// embeddedThrough returns the embedded migrations up to and including version through.
func embeddedThrough(t *testing.T, through int) (fstest.MapFS, []migrate.Migration) {
	t.Helper()

	all, err := migrate.Load(migrate.Embedded())
	require.NoError(t, err)

	src := fstest.MapFS{}

	var kept []migrate.Migration

	for _, m := range all {
		if m.Version <= through {
			src[m.File] = &fstest.MapFile{Data: []byte(m.SQL)}
			kept = append(kept, m)
		}
	}

	return src, kept
}

// readSquashed reads the squashed history in file.
func readSquashed(t *testing.T, file string) migrate.SquashedHistory {
	t.Helper()

	body, err := os.ReadFile(file) //nolint:gosec // a file of this package's own
	require.NoError(t, err)

	var h migrate.SquashedHistory
	require.NoError(t, json.Unmarshal(body, &h))

	return h
}

// squashedFiles lists the embedded squashed histories' files.
func squashedFiles(t *testing.T) []string {
	t.Helper()

	files, err := filepath.Glob(filepath.Join(squashedDir, "*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, files, "a squash keeps the history of the newest release it combined")

	return files
}

// Run with -freeze NNNN before a squash deletes the migrations it combines, once per
// history users' databases may have: the newest release's, and a build from source's.
func TestFreezeASquashedHistory(t *testing.T) {
	t.Parallel()

	if *freeze == 0 {
		t.Skip("run with -freeze NNNN to freeze the history of the migrations through NNNN before a squash")
	}

	src, kept := embeddedThrough(t, *freeze)
	require.NotEmpty(t, kept)
	require.Equal(t, *freeze, kept[len(kept)-1].Version, "freeze a version this build has")

	db, path := openDB(t)
	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{Source: src})
	require.NoError(t, err)

	h := migrate.SquashedHistory{About: "Set through, and say which builds recorded this history."}
	for _, m := range kept {
		h.Migrations = append(h.Migrations, migrate.Applied{Version: m.Version, Name: m.Name, Checksum: m.Checksum})
	}

	body, err := json.MarshalIndent(h, "", "    ") // as magex format:fix lays JSON out
	require.NoError(t, err)

	base := filepath.Join(squashedDir, fmt.Sprintf("%04d", *freeze))
	require.NoError(t, os.WriteFile(base+".json", append(body, '\n'), 0o600))
	require.NoError(t, os.WriteFile(base+".schema", []byte(schemaOf(t, db)), 0o600))
}

// A squash's base migrations must build, through each history's Through, exactly the
// schema the history's own migrations built, frozen beside it when they still existed:
// a database carried over runs nothing, so any difference would be a schema the app
// never made.
func TestEverySquashedHistoryBuildsTheSchemaOfItsBase(t *testing.T) {
	t.Parallel()

	for _, file := range squashedFiles(t) {
		t.Run(filepath.Base(file), func(t *testing.T) {
			t.Parallel()

			h := readSquashed(t, file)
			src, _ := embeddedThrough(t, h.Through)

			db, path := openDB(t)
			_, err := migrate.Apply(t.Context(), db, path, migrate.Options{Source: src})
			require.NoError(t, err)

			want, err := os.ReadFile(strings.TrimSuffix(file, ".json") + ".schema")
			require.NoError(t, err)
			assert.Equal(t, string(want), schemaOf(t, db),
				"the base migrations through %d build the schema %s's migrations built", h.Through, filepath.Base(file))
		})
	}
}

// A database whose history is one a squash froze is carried over to the base, keeping
// its data, and brought up to date.
func TestADatabaseWithAnEmbeddedSquashedHistoryIsCarriedOver(t *testing.T) {
	t.Parallel()

	all, err := migrate.Load(migrate.Embedded())
	require.NoError(t, err)

	newest := all[len(all)-1].Version

	for _, file := range squashedFiles(t) {
		t.Run(filepath.Base(file), func(t *testing.T) {
			t.Parallel()

			h := readSquashed(t, file)
			legacy := h.Migrations[len(h.Migrations)-1].Version

			// The schema the history built is the base's through h.Through (the test above),
			// so a database that ran the history is that schema with the history recorded.
			src, _ := embeddedThrough(t, h.Through)
			db, path := openDB(t)
			_, err := migrate.Apply(t.Context(), db, path, migrate.Options{Source: src})
			require.NoError(t, err)
			recordHistory(t, db, h.Migrations)
			execAll(t, db, `INSERT INTO prefs (key, value, updated_at) VALUES ('example.key', 'kept', `+at+`)`)

			require.NoError(t, migrate.Check(t.Context(), db, migrate.Options{}), "Open lets it through")

			res, err := migrate.Apply(t.Context(), db, path, migrate.Options{})
			require.NoError(t, err)

			var later []int

			for _, m := range all {
				if m.Version > h.Through {
					later = append(later, m.Version)
				}
			}

			assert.Equal(t, legacy, res.From)
			assert.Equal(t, newest, res.To)
			assert.True(t, res.CarriedOver)
			assert.Equal(t, later, res.Applied, "only the migrations after the base run")
			assert.True(t, strings.HasSuffix(res.Backup, fmt.Sprintf("-v%d", legacy)),
				"backed up first, named for the history, which the older build offers to put back: %s", res.Backup)

			var value string
			require.NoError(t, db.QueryRowContext(t.Context(),
				`SELECT value FROM prefs WHERE key = 'example.key'`).Scan(&value))
			assert.Equal(t, "kept", value, "the data is kept")

			assert.Equal(t, versionsOf(all), recordedVersions(t, db), "recorded as this build's own")

			again, err := migrate.Apply(t.Context(), db, path, migrate.Options{})
			require.NoError(t, err)
			assert.Equal(t, migrate.Result{From: newest, To: newest}, again, "carried over once")
		})
	}
}

func TestTheEmbeddedSquashedHistoriesAreValid(t *testing.T) {
	t.Parallel()

	all, err := migrate.Load(migrate.Embedded())
	require.NoError(t, err)

	known, err := migrate.KnownSchemas(migrate.Options{})
	require.NoError(t, err, "every squashed history loads and is carried over through a migration there is")

	assert.Equal(t, all[0].Version, known.Oldest)
	assert.Equal(t, all[len(all)-1].Version, known.Newest)

	for _, file := range squashedFiles(t) {
		h := readSquashed(t, file)
		legacy := h.Migrations[len(h.Migrations)-1].Version

		assert.Contains(t, known.Squashed, legacy, file)
		assert.True(t, known.Opens(legacy), "a backup of %s's history opens here", file)
		assert.NotEmpty(t, h.About, "%s says which builds recorded it", file)
		assert.NotEqual(t, "Set through, and say which builds recorded this history.", h.About, file)
	}

	assert.Zero(t, all[0].Version%100, "a squash's base starts at a hundred above every version a build shipped")
}

// recordHistory replaces the migration history db records with history.
func recordHistory(t *testing.T, db *sql.DB, history []migrate.Applied) {
	t.Helper()

	execAll(t, db, `DELETE FROM schema_migrations`)

	for _, m := range history {
		_, err := db.ExecContext(t.Context(), `INSERT INTO schema_migrations (version, name, checksum, applied_at)
			VALUES (?, ?, ?, '2026-11-01 10:00:00+00:00')`, m.Version, m.Name, m.Checksum)
		require.NoError(t, err)
	}
}

// recordedVersions lists the versions db records, oldest first.
func recordedVersions(t *testing.T, db *sql.DB) []int {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), `SELECT version FROM schema_migrations ORDER BY version`)
	require.NoError(t, err)

	defer func() { _ = rows.Close() }()

	var versions []int

	for rows.Next() {
		var v int
		require.NoError(t, rows.Scan(&v))

		versions = append(versions, v)
	}

	require.NoError(t, rows.Err())

	return versions
}

// versionsOf lists the versions of migrations.
func versionsOf(migrations []migrate.Migration) []int {
	versions := make([]int, 0, len(migrations))
	for _, m := range migrations {
		versions = append(versions, m.Version)
	}

	return versions
}

// schemaOf describes db's schema in a form that ignores how its SQL was written: for each
// table, a line naming it and a line for each of its columns and constraints, in order;
// then a line for each index, trigger, and view; each kind sorted by name. Comments,
// whitespace, letter case outside strings, and quotes around names are left out, so a
// table made whole reads the same as one made and altered after, as SQLite stores it:
// ADD COLUMN writes the column after the last one, before the table's constraints.
// SQLite's own tables, and the migration history, are left out.
func schemaOf(t *testing.T, db *sql.DB) string {
	t.Helper()

	rows, err := db.QueryContext(t.Context(), `SELECT type, sql FROM sqlite_master
		WHERE sql IS NOT NULL AND name <> 'schema_migrations' AND name NOT LIKE 'sqlite\_%' ESCAPE '\'
		ORDER BY CASE type WHEN 'table' THEN 0 WHEN 'index' THEN 1 WHEN 'trigger' THEN 2 ELSE 3 END, name`)
	require.NoError(t, err)

	defer func() { _ = rows.Close() }()

	var b strings.Builder

	for rows.Next() {
		var kind, stmt string
		require.NoError(t, rows.Scan(&kind, &stmt))

		toks := sqlTokens(stmt)
		if kind != "table" {
			b.WriteString(joinTokens(toks) + "\n")

			continue
		}

		writeTable(&b, toks)
	}

	require.NoError(t, rows.Err())

	return b.String()
}

// writeTable writes a CREATE TABLE's tokens: its head, then each column and constraint
// indented on a line of its own, then what follows the closing parenthesis, if anything.
func writeTable(b *strings.Builder, toks []string) {
	open := slices.Index(toks, "(")
	if open < 0 {
		b.WriteString(joinTokens(toks) + "\n")

		return
	}

	b.WriteString(joinTokens(toks[:open]) + "\n")

	items, rest := tableItems(toks[open:])
	for _, item := range items {
		b.WriteString("    " + joinTokens(item) + "\n")
	}

	if len(rest) > 0 {
		b.WriteString("    ) " + joinTokens(rest) + "\n")
	}
}

// tableItems splits a CREATE TABLE's tokens from its opening parenthesis into its columns
// and constraints, at the commas between them, and what follows the closing parenthesis.
func tableItems(toks []string) (items [][]string, rest []string) {
	depth, start := 0, 1

	for i, tok := range toks {
		switch {
		case tok == "(":
			depth++
		case tok == "," && depth == 1:
			items = append(items, toks[start:i])
			start = i + 1
		case tok == ")":
			depth--
			if depth == 0 {
				return append(items, toks[start:i]), toks[i+1:]
			}
		}
	}

	return items, nil
}

// joinTokens joins toks with single spaces, none inside parentheses or before a comma.
func joinTokens(toks []string) string {
	var b strings.Builder

	for i, tok := range toks {
		if i > 0 && tok != ")" && tok != "," && toks[i-1] != "(" {
			b.WriteByte(' ')
		}

		b.WriteString(tok)
	}

	return b.String()
}

// twoByteOperators are SQLite's operators of two characters.
var twoByteOperators = []string{"<>", "<=", ">=", "!=", "==", "||", "<<", ">>"} //nolint:gochecknoglobals // a constant list

// sqlTokens splits a statement into its tokens: words lower-cased, quoted names unquoted
// and lower-cased, strings as written, and operators, with comments and whitespace left
// out.
func sqlTokens(stmt string) []string {
	var toks []string

	for i := 0; i < len(stmt); {
		if next, ok := skipped(stmt, i); ok {
			i = next

			continue
		}

		tok, next := nextToken(stmt, i)
		toks = append(toks, tok)
		i = next
	}

	return toks
}

// skipped returns where the whitespace or comment at stmt[i] ends, and whether there is one.
func skipped(stmt string, i int) (int, bool) {
	switch {
	case strings.IndexByte(" \t\n\r", stmt[i]) >= 0:
		return i + 1, true
	case strings.HasPrefix(stmt[i:], "--"):
		if end := strings.IndexByte(stmt[i:], '\n'); end >= 0 {
			return i + end, true
		}

		return len(stmt), true
	case strings.HasPrefix(stmt[i:], "/*"):
		if end := strings.Index(stmt[i+2:], "*/"); end >= 0 {
			return i + end + 4, true
		}

		return len(stmt), true
	default:
		return i, false
	}
}

// nextToken returns the token at stmt[i], which is no whitespace or comment, and where it
// ends.
func nextToken(stmt string, i int) (string, int) {
	c := stmt[i]

	switch {
	case c == '\'':
		j := closingQuote(stmt, i)

		return stmt[i:j], j
	case c == '"' || c == '`':
		j := closingQuote(stmt, i)
		name := strings.ReplaceAll(stmt[i+1:max(i+1, j-1)], string([]byte{c, c}), string(c))

		return strings.ToLower(name), j
	case isWordByte(c):
		j := i
		for j < len(stmt) && isWordByte(stmt[j]) {
			j++
		}

		return strings.ToLower(stmt[i:j]), j
	case i+1 < len(stmt) && slices.Contains(twoByteOperators, stmt[i:i+2]):
		return stmt[i : i+2], i + 2
	default:
		return string(c), i + 1
	}
}

// closingQuote returns the index just past the quote that closes the one at stmt[i], a
// doubled quote standing for one inside.
func closingQuote(stmt string, i int) int {
	q := stmt[i]

	for j := i + 1; j < len(stmt); j++ {
		if stmt[j] != q {
			continue
		}

		if j+1 < len(stmt) && stmt[j+1] == q {
			j++

			continue
		}

		return j + 1
	}

	return len(stmt)
}

// isWordByte reports whether c is part of a word: a keyword, a name, or a number.
func isWordByte(c byte) bool {
	return c == '_' || c == '$' || c >= 0x80 ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}
