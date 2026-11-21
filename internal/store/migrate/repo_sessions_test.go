package migrate_test

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store/migrate"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// sessionColumn is one column of agent_sessions as SQLite describes it.
type sessionColumn struct {
	cid, notNull, pk int
	name, kind       string
	dflt             sql.NullString
}

// sessionColumns describes every column of agent_sessions, in its order.
func sessionColumns(t *testing.T, db *sql.DB) []sessionColumn {
	t.Helper()

	return testutil.Query(t, db, func(c *sessionColumn) []any {
		return []any{&c.cid, &c.name, &c.kind, &c.notNull, &c.dflt, &c.pk}
	}, `SELECT cid, name, type, "notnull", dflt_value, pk FROM pragma_table_info('agent_sessions') ORDER BY cid`)
}

// storedSessions reads every row of agent_sessions, every column of it in order, as JSON,
// by id.
func storedSessions(t *testing.T, db *sql.DB) []string {
	t.Helper()

	return testutil.Column[string](t, db, `SELECT json_array(id, agent_session_id, resumed_from, agent, profile,
		config_dir, prompt, prompt_head, terminal, item_id, owner, name, kind, number, title, url, dir, clone_path,
		worktree, pid, state, state_detail, last_message, launched_at, linked_at, state_at, ended_at, permission,
		worktree_action, launch_notice, run_id, github_event, github_event_commit, github_event_at)
		FROM agent_sessions ORDER BY id`)
}

// sessionIndexes lists each index made on agent_sessions, with its columns in order.
func sessionIndexes(t *testing.T, db *sql.DB) map[string][]string {
	t.Helper()

	type part struct{ index, column string }

	parts := testutil.Query(t, db, func(p *part) []any { return []any{&p.index, &p.column} },
		`SELECT il.name, ii.name FROM pragma_index_list('agent_sessions') AS il, pragma_index_info(il.name) AS ii
		WHERE il.origin = 'c' ORDER BY il.name, ii.seqno`)

	out := map[string][]string{}
	for _, p := range parts {
		out[p.index] = append(out[p.index], p.column)
	}

	return out
}

// TestRepoSessionsKeepEverySession migrates a database to 0111, stores sessions of a pull
// request and an issue, one with every column set and one with the defaults, then applies
// 0112_repo_sessions, which makes agent_sessions again: every session comes through as it
// was, every column keeps its order, type, and default, the two indexes are back, and a
// session with no item stores only as a repository's launch's.
func TestRepoSessionsKeepEverySession(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)

	through111, _ := embeddedThrough(t, 111)
	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{Source: through111})
	require.NoError(t, err)

	execAll(t, db,
		`INSERT INTO agent_sessions (id, agent_session_id, resumed_from, agent, profile, config_dir, prompt,
			prompt_head, terminal, item_id, owner, name, kind, number, title, url, dir, clone_path, worktree, pid,
			state, state_detail, last_message, launched_at, linked_at, state_at, ended_at, permission,
			worktree_action, launch_notice, run_id, github_event, github_event_commit, github_event_at)
			VALUES ('s1', 'a1', 's0', 'claude', 'work', '/srv/.claude_work', 'Review', 'Review it', 'iterm2', 7,
			'acme-org', 'example-repo', 'pr', 12, 'Fix it', 'https://github.com/acme-org/example-repo/pull/12',
			'/srv/wt/pr-12', '/srv/example-repo', 'pr-12', 4100, 'ended', 'quit', 'Done.', `+at+`, `+at+`, `+at+`,
			`+at+`, 'full access', 'create', 'Started from the last fetched copy.', 3, 'merged', 'abc123', `+at+`)`,
		`INSERT INTO agent_sessions (id, agent, owner, name, kind, number, dir, state, launched_at, state_at)
			VALUES ('s2', 'codex', 'Acme-Org', 'Example-Repo', 'issue', 3, '/srv/example-repo', 'starting', `+at+`,
			`+at+`)`,
	)

	columns, sessions := sessionColumns(t, db), storedSessions(t, db)
	require.Len(t, columns, 34)
	require.Len(t, sessions, 2)

	through112, _ := embeddedThrough(t, 112)
	res, err := migrate.Apply(t.Context(), db, path, migrate.Options{Source: through112})
	require.NoError(t, err)
	assert.Equal(t, []int{112}, res.Applied)

	assert.Equal(t, columns, sessionColumns(t, db), "every column in its order, with its type, its NOT NULL, and its default")
	assert.Equal(t, sessions, storedSessions(t, db), "every session as it was")
	assert.Equal(t, map[string][]string{
		"agent_sessions_item":  {"owner", "name", "number", "launched_at"},
		"agent_sessions_state": {"state"},
	}, sessionIndexes(t, db), "both indexes made again")
	assert.NotContains(t, tables(t, db), "agent_sessions_next", "the new table took the old one's name")

	assertRepoSessionsStore(t, db)
}

// assertRepoSessionsStore checks the wider checks: a repository's launch's session, with no
// kind, number 0, no item, and the worktree it made, stores; half of one, or an item's
// with no number, is refused.
func assertRepoSessionsStore(t *testing.T, db *sql.DB) {
	t.Helper()

	const (
		check  = "CHECK constraint failed: "
		insert = `INSERT INTO agent_sessions (id, agent, owner, name, kind, number, item_id, dir, worktree, state,
			launched_at, state_at) VALUES (?, 'claude', 'acme-org', 'example-repo', ?, ?, ?, '/srv/wt/deps-20261121',
			?, 'starting', ` + at + `, ` + at + `)`
	)

	_, err := db.ExecContext(t.Context(), insert, "r1", "", 0, 0, "deps-20261121")
	require.NoError(t, err, "a repository's launch's session")

	assert.Equal(t, []string{"r1:0:0:deps-20261121"}, testutil.Column[string](t, db,
		`SELECT id || ':' || number || ':' || item_id || ':' || worktree FROM agent_sessions WHERE kind = ''`))

	for _, refused := range []struct {
		name, kind, worktree string
		number, item         int
		want                 string
	}{
		{name: "no kind with a number", number: 4, worktree: "deps-20261121", want: "(kind = '') = (number = 0)"},
		{name: "a kind with no number", kind: "pr", worktree: "pr-0", want: "(kind = '') = (number = 0)"},
		{name: "no kind with an item", item: 7, worktree: "deps-20261121", want: "kind <> '' OR (item_id = 0 AND worktree <> '')"},
		{name: "no kind and no worktree", want: "kind <> '' OR (item_id = 0 AND worktree <> '')"},
		{name: "another kind", kind: "repo", worktree: "deps-20261121", want: "kind IN ('', 'pr', 'issue')"},
		{name: "a number below 0", kind: "pr", number: -1, worktree: "pr-1", want: "number >= 0"},
	} {
		_, err = db.ExecContext(t.Context(), insert, "x-"+refused.name, refused.kind, refused.number, refused.item,
			refused.worktree)
		require.ErrorContains(t, err, check+refused.want, refused.name)
	}
}
