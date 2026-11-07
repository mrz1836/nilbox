package store_test

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// execAll runs each statement on db, failing the test on the first error.
func execAll(t *testing.T, db *sql.DB, stmts ...string) {
	t.Helper()

	for _, stmt := range stmts {
		_, err := db.ExecContext(t.Context(), stmt)
		require.NoError(t, err, stmt)
	}
}

// rowCount counts the rows of table.
func rowCount(t *testing.T, db *sql.DB, table string) int {
	t.Helper()

	var n int
	require.NoError(t, db.QueryRowContext(t.Context(), "SELECT count(*) FROM "+table).Scan(&n))

	return n
}

// inboxRows returns a database holding an account (id 1), a repository (id 1), and a
// pull request (id 1) in it.
func inboxRows(t *testing.T) *sql.DB {
	t.Helper()

	db := testutil.DB(t)
	execAll(t, db,
		`INSERT INTO accounts (id, login, created_at, updated_at)
			VALUES (1, 'octo-user', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		`INSERT INTO repos (id, owner, name, created_at, updated_at)
			VALUES (1, 'acme-org', 'example-repo', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		`INSERT INTO items (id, account_id, repo_id, kind, number, item_created_at, item_updated_at, fetched_at)
			VALUES (1, 1, 1, 'pr', 1, '2026-01-01 00:00:00', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
	)

	return db
}

func TestSchemaRejectsInvalidInboxRows(t *testing.T) {
	t.Parallel()

	db := inboxRows(t)

	for name, stmt := range map[string]string{
		"a repository differing only by case": `INSERT INTO repos (owner, name, created_at, updated_at)
			VALUES ('Acme-Org', 'EXAMPLE-REPO', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		"a repository flag other than 0 or 1": `UPDATE repos SET is_archived = 2 WHERE id = 1`,
		"an unknown item kind": `INSERT INTO items (account_id, repo_id, kind, number, item_created_at, item_updated_at, fetched_at)
			VALUES (1, 1, 'discussion', 2, '2026-01-01 00:00:00', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		"an item for a missing repository": `INSERT INTO items (account_id, repo_id, kind, number, item_created_at, item_updated_at, fetched_at)
			VALUES (1, 99, 'issue', 2, '2026-01-01 00:00:00', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		"labels that are not an array": `INSERT INTO item_details (item_id, labels, fetched_at)
			VALUES (1, '{"not": "an array"}', '2026-01-01 00:00:00')`,
		"a negative count": `INSERT INTO item_details (item_id, comments, fetched_at)
			VALUES (1, -1, '2026-01-01 00:00:00')`,
		"more reviewed than reviewers": `INSERT INTO item_details (item_id, reviewers, reviewed, fetched_at)
			VALUES (1, 1, 2, '2026-01-01 00:00:00')`,
		"details for a missing item": `INSERT INTO item_details (item_id, fetched_at) VALUES (99, '2026-01-01 00:00:00')`,
		"an item both dismissed and snoozed": `INSERT INTO item_local_state (item_id, dismissed_at, snoozed_until, updated_at)
			VALUES (1, '2026-01-01 00:00:00', '2026-01-02 00:00:00', '2026-01-01 00:00:00')`,
		"triage state for a missing item": `INSERT INTO item_local_state (item_id, dismissed_at, updated_at)
			VALUES (99, '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		"a snooze baseline that is not JSON": `INSERT INTO item_local_state (item_id, snoozed_until, snooze_baseline, updated_at)
			VALUES (1, '2026-01-02 00:00:00', 'not json', '2026-01-01 00:00:00')`,
		"wake reasons that are not an object": `INSERT INTO item_local_state (item_id, snoozed_until, wake_reasons, updated_at)
			VALUES (1, '2026-01-02 00:00:00', '[1, 2]', '2026-01-01 00:00:00')`,
		"a snooze baseline that is an array": `INSERT INTO item_local_state (item_id, snoozed_until, snooze_baseline, updated_at)
			VALUES (1, '2026-01-02 00:00:00', '[1, 2]', '2026-01-01 00:00:00')`,
		"wake reasons that are not JSON": `INSERT INTO item_local_state (item_id, snoozed_until, wake_reasons, updated_at)
			VALUES (1, '2026-01-02 00:00:00', 'not json', '2026-01-01 00:00:00')`,
		"profile directories that are not an array": `INSERT INTO claude_profiles (name, add_dirs, created_at, updated_at)
			VALUES ('broken', '{"not": "an array"}', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
	} {
		_, err := db.ExecContext(t.Context(), stmt)
		require.Error(t, err, name)
	}

	execAll(t, db, `INSERT INTO item_local_state (item_id, snoozed_until, snooze_baseline, wake_reasons, updated_at)
		VALUES (1, '2026-01-02 00:00:00', '{"commits":3}', '{"commits":2}', '2026-01-01 00:00:00')`)
}

func TestSchemaCascadesAnAccountsDeletion(t *testing.T) {
	t.Parallel()

	db := inboxRows(t)
	execAll(t, db,
		`INSERT INTO item_details (item_id, fetched_at) VALUES (1, '2026-01-01 00:00:00')`,
		`INSERT INTO item_local_state (item_id, dismissed_at, updated_at)
			VALUES (1, '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		`INSERT INTO rate_snapshots (account_id, resource, limit_total, remaining, used, reset_at, taken_at)
			VALUES (1, 'graphql', 5000, 4999, 1, '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		`DELETE FROM accounts WHERE id = 1`,
	)

	for _, table := range []string{"items", "item_details", "item_local_state", "rate_snapshots"} {
		assert.Zero(t, rowCount(t, db, table), "%s goes with its account", table)
	}

	assert.Equal(t, 1, rowCount(t, db, "repos"), "the repository outlives the account")
}

func TestSchemaRejectsInvalidPromptsAndLaunchDefaults(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	execAll(t, db,
		`INSERT INTO prompts (id, name, nickname, applies_to, body, created_at, updated_at)
			VALUES (1, 'Review', 'Review', 'pr', 'x', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		`INSERT INTO launch_defaults (owner, pr_prompt_id, created_at, updated_at)
			VALUES ('acme-org', 1, '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		`INSERT INTO launch_defaults (owner, name, created_at, updated_at)
			VALUES ('acme-org', 'example-repo', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
	)

	for name, stmt := range map[string]string{
		"a nickname differing only by case": `INSERT INTO prompts (name, nickname, applies_to, body, created_at, updated_at)
			VALUES ('Other', 'REVIEW', 'pr', 'x', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		"an unknown target": `INSERT INTO prompts (name, nickname, applies_to, body, created_at, updated_at)
			VALUES ('Other', 'Other', 'discussion', 'x', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		"a plan mode other than 0, 1, or NULL": `INSERT INTO prompts (name, nickname, applies_to, body, plan_mode, created_at, updated_at)
			VALUES ('x', 'bad plan', 'pr', 'x', 2, '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		"extra arguments that are not an array": `INSERT INTO prompts (name, nickname, applies_to, body, extra_args, created_at, updated_at)
			VALUES ('x', 'bad args', 'pr', 'x', '{}', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		"a default naming a missing prompt": `INSERT INTO launch_defaults (owner, name, pr_prompt_id, created_at, updated_at)
			VALUES ('acme-org', 'other-repo', 999, '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		"a worktree setting other than 0, 1, or NULL": `INSERT INTO launch_defaults (owner, name, worktree, created_at, updated_at)
			VALUES ('acme-org', 'other-repo', 3, '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		"a second row for an owner, by case": `INSERT INTO launch_defaults (owner, created_at, updated_at)
			VALUES ('ACME-ORG', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		"a second row for a repository, by case": `INSERT INTO launch_defaults (owner, name, created_at, updated_at)
			VALUES ('acme-org', 'Example-Repo', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		"deleting a prompt that is a default": `DELETE FROM prompts WHERE id = 1`,
	} {
		_, err := db.ExecContext(t.Context(), stmt)
		require.Error(t, err, name)
	}
}
