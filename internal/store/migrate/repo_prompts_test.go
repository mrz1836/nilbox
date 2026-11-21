package migrate_test

import (
	"database/sql"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store/migrate"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// promptColumns are launch_defaults' six columns that name a prompt, each ON DELETE
// RESTRICT.
func promptColumns() []string {
	return []string{
		"pr_prompt_id", "issue_prompt_id", "checks_prompt_id", "conflicts_prompt_id", "changes_prompt_id",
		"rereview_prompt_id",
	}
}

// storedPrompt is one row of the prompts table, every column of it.
type storedPrompt struct {
	id                                                 int64
	name, nickname, appliesTo, body, effort, starter   string
	planMode, worktree                                 *bool
	createdAt, updatedAt, models, args, purpose, reply string
}

// storedPrompts reads every row of the prompts table, by id.
func storedPrompts(t *testing.T, db *sql.DB) []storedPrompt {
	t.Helper()

	return testutil.Query(t, db, func(p *storedPrompt) []any {
		return []any{
			&p.id, &p.name, &p.nickname, &p.appliesTo, &p.body, &p.effort, &p.planMode, &p.worktree, &p.starter,
			&p.createdAt, &p.updatedAt, &p.models, &p.args, &p.purpose, &p.reply,
		}
	}, `SELECT id, name, nickname, applies_to, body, effort, plan_mode, worktree, starter_key, created_at, updated_at,
		models, agent_args, purpose, reply_action FROM prompts ORDER BY id`)
}

// namedPrompts reads, for each launch default by its owner and name, the prompt each of its
// six columns names, 0 for none.
func namedPrompts(t *testing.T, db *sql.DB) map[string][]int64 {
	t.Helper()

	type row struct {
		target string
		ids    [6]sql.NullInt64
	}

	rows := testutil.Query(t, db, func(r *row) []any {
		return []any{&r.target, &r.ids[0], &r.ids[1], &r.ids[2], &r.ids[3], &r.ids[4], &r.ids[5]}
	}, `SELECT owner || '/' || name, pr_prompt_id, issue_prompt_id, checks_prompt_id, conflicts_prompt_id,
		changes_prompt_id, rereview_prompt_id FROM launch_defaults ORDER BY id`)

	out := make(map[string][]int64, len(rows))

	for _, r := range rows {
		for _, id := range r.ids {
			out[r.target] = append(out[r.target], id.Int64)
		}
	}

	return out
}

// TestRepoPromptsKeepEveryPromptAndLaunchDefault migrates a database to 0110, stores prompts
// that launch defaults name in all six of their prompt columns, then applies
// 0111_repo_prompts, which makes the prompts table again: every prompt and every launch
// default's prompt comes through as it was, the foreign keys still hold, and a prompt for
// repositories is an agent prompt alone.
func TestRepoPromptsKeepEveryPromptAndLaunchDefault(t *testing.T) {
	t.Parallel()

	db, path := openDB(t)

	through110, _ := embeddedThrough(t, 110)
	_, err := migrate.Apply(t.Context(), db, path, migrate.Options{Source: through110})
	require.NoError(t, err)

	for i, column := range promptColumns() {
		execAll(t, db, fmt.Sprintf(`INSERT INTO prompts (id, name, nickname, applies_to, body, effort, plan_mode, worktree,
			starter_key, created_at, updated_at, models, agent_args) VALUES (%d, 'Prompt %d', '%s', 'pr', 'Look at {{.URL}}',
			'high', 1, NULL, 'review-pr', `+at+`, `+at+`, '{"claude":"opus"}', '{"codex":["--search"]}')`,
			10+i, i, column))
	}

	execAll(t, db,
		`INSERT INTO prompts (id, name, nickname, applies_to, body, created_at, updated_at, purpose, reply_action)
			VALUES (30, 'Approve', 'LGTM', 'pr', 'Approved.', `+at+`, `+at+`, 'reply', 'approve')`,
		`INSERT INTO launch_defaults (id, owner, name, pr_prompt_id, issue_prompt_id, checks_prompt_id,
			conflicts_prompt_id, changes_prompt_id, rereview_prompt_id, created_at, updated_at)
			VALUES (1, 'acme-org', '', 10, 11, 12, 13, 14, 15, `+at+`, `+at+`)`,
		`INSERT INTO launch_defaults (id, owner, name, rereview_prompt_id, created_at, updated_at)
			VALUES (2, 'acme-org', 'example-repo', 10, `+at+`, `+at+`)`,
		`INSERT INTO launch_defaults (id, owner, name, agent, created_at, updated_at)
			VALUES (3, 'acme-org', 'other-repo', 'codex', `+at+`, `+at+`)`,
	)

	prompts, named := storedPrompts(t, db), namedPrompts(t, db)
	require.Len(t, prompts, 7)

	through111, _ := embeddedThrough(t, 111)
	res, err := migrate.Apply(t.Context(), db, path, migrate.Options{Source: through111})
	require.NoError(t, err)
	assert.Equal(t, []int{111}, res.Applied)

	assert.Equal(t, prompts, storedPrompts(t, db), "every prompt is kept as it was, under its own id")
	assert.Equal(t, map[string][]int64{
		"acme-org/":             {10, 11, 12, 13, 14, 15},
		"acme-org/example-repo": {0, 0, 0, 0, 0, 10},
		"acme-org/other-repo":   {0, 0, 0, 0, 0, 0},
	}, named)
	assert.Equal(t, named, namedPrompts(t, db), "every launch default names the prompts it did")

	assertPromptsStillReferenced(t, db)
	assertRepoPromptsAreAgentPrompts(t, db)
}

// assertPromptsStillReferenced checks that launch_defaults' six columns still reference the
// prompts table, that nothing breaks a foreign key, that a prompt a launch default names
// still can't be deleted, and that the scratch table is gone.
func assertPromptsStillReferenced(t *testing.T, db *sql.DB) {
	t.Helper()

	type reference struct{ from, table string }

	refs := testutil.Query(t, db, func(r *reference) []any { return []any{&r.from, &r.table} },
		`SELECT "from", "table" FROM pragma_foreign_key_list('launch_defaults') ORDER BY "from"`)
	require.Len(t, refs, len(promptColumns()))

	for _, r := range refs {
		assert.Contains(t, promptColumns(), r.from)
		assert.Equal(t, "prompts", r.table, "%s references the prompts table", r.from)
	}

	assert.Empty(t, testutil.Column[string](t, db, `SELECT "table" FROM pragma_foreign_key_check`),
		"no row breaks a foreign key")

	for id := 10; id <= 15; id++ {
		_, err := db.ExecContext(t.Context(), `DELETE FROM prompts WHERE id = ?`, id)
		require.ErrorContains(t, err, "FOREIGN KEY constraint failed", "prompt %d is named, so it stays", id)
	}

	_, err := db.ExecContext(t.Context(), `DELETE FROM prompts WHERE id = 30`)
	require.NoError(t, err, "a prompt no launch default names deletes")

	assert.NotContains(t, tables(t, db), "launch_defaults_prompt_ids", "the scratch table is gone")
	assert.NotContains(t, tables(t, db), "prompts_next")
}

// assertRepoPromptsAreAgentPrompts checks the new applies_to: a prompt for repositories is
// stored as an agent prompt and refused as a review reply.
func assertRepoPromptsAreAgentPrompts(t *testing.T, db *sql.DB) {
	t.Helper()

	execAll(t, db, `INSERT INTO prompts (name, nickname, applies_to, body, created_at, updated_at)
		VALUES ('Upgrade dependencies', 'Upgrade deps', 'repo', 'Upgrade {{.Repo}}', `+at+`, `+at+`)`)

	_, err := db.ExecContext(t.Context(), `INSERT INTO prompts (name, nickname, applies_to, body, created_at, updated_at,
		purpose, reply_action) VALUES ('Thanks', 'Thanks', 'repo', 'Thanks!', `+at+`, `+at+`, 'reply', 'comment')`)
	require.ErrorContains(t, err, "CHECK constraint failed: applies_to <> 'repo' OR purpose = 'agent'")

	_, err = db.ExecContext(t.Context(), `UPDATE prompts SET applies_to = 'repository' WHERE nickname = 'Upgrade deps'`)
	require.ErrorContains(t, err, "CHECK constraint failed: applies_to IN ('pr', 'issue', 'any', 'repo')")
}
