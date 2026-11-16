package core

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestTheInboxKeepsTheItemsSessionsAndNotificationsName(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	now := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)

	testutil.Exec(t, db, `INSERT INTO accounts (id, login, created_at, updated_at) VALUES (1, 'octo-user', ?, ?)`, now, now)
	testutil.Exec(t, db, `INSERT INTO repos (id, owner, name, created_at, updated_at)
VALUES (1, 'acme-org', 'example-repo', ?, ?)`, now, now)

	for _, id := range []int{5, 6, 7} {
		testutil.Exec(t, db, `INSERT INTO items (id, account_id, repo_id, kind, number, item_created_at, item_updated_at,
    fetched_at) VALUES (?, 1, 1, 'pr', ?, ?, ?, ?)`, id, id, now, now, now)
	}

	testutil.Exec(t, db, `INSERT INTO agent_sessions (id, agent, item_id, owner, name, kind, number, dir, state,
    launched_at, state_at) VALUES ('s1', 'claude', 5, 'acme-org', 'example-repo', 'pr', 5, '~/projects/example-repo',
    'ended', ?, ?)`, now, now)
	testutil.Exec(t, db, `INSERT INTO agent_sessions (id, agent, owner, name, kind, number, dir, state, launched_at,
    state_at) VALUES ('s2', 'codex', 'acme-org', 'example-repo', 'pr', 9, '~/projects/example-repo', 'ended', ?, ?)`,
		now, now)
	testutil.Exec(t, db, `INSERT INTO notifications (kind, created_at, item_id) VALUES ('arrival', ?, 6), ('run_failed', ?, NULL)`,
		now, now)

	got, err := itemKeepers(db, &engineWaker{}).KeptItems(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []int64{5, 6}, got, "a session's item and a notification's, never one no row names")
}
