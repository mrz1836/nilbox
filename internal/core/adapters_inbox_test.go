package core

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
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

// errCacheLocked is a cache that can't be written.
var errCacheLocked = errors.New("database is locked")

// sheetEdits counts what the sheets' caches were told, failing with fail.
type sheetEdits struct {
	pulls  []string
	issues []int64
	fail   error
}

func (s *sheetEdits) Unassigned(_ context.Context, id int64, login string) error {
	s.pulls = append(s.pulls, fmt.Sprintf("%d %s", id, login))

	return s.fail
}

// issueEdits is sheetEdits as the issue sheet's cache.
type issueEdits struct{ s *sheetEdits }

func (i issueEdits) Unassigned(_ context.Context, id int64) error {
	i.s.issues = append(i.s.issues, id)

	return i.s.fail
}

func TestTheSheetsCachesFollowTheInboxsUnassignMe(t *testing.T) {
	t.Parallel()

	name := repo.Name{Owner: boundOwner, Repo: boundRepo}
	outcomes := []inbox.Outcome{
		{Kind: inbox.OutcomeUnassigned, Login: rigViewer, Ref: item.Ref{Repo: name, Number: 7, Kind: item.KindPR}, ItemID: 7},
		{Kind: inbox.OutcomeUnassigned, Login: rigViewer, Ref: item.Ref{Repo: name, Number: 8, Kind: item.KindIssue}, ItemID: 8},
		// The rest isn't an Unassign me the inbox made of a stored item.
		{Kind: inbox.OutcomeUnrequested, Login: rigViewer, Ref: item.Ref{Repo: name, Number: 9, Kind: item.KindPR}, ItemID: 9},
		{Kind: inbox.OutcomeUnassigned, Login: rigViewer, Ref: item.Ref{Repo: name, Number: 10, Kind: item.KindPR}},
	}

	(*sheetOutcomes)(nil).Settle(t.Context(), outcomes)
	(&sheetOutcomes{}).Settle(t.Context(), outcomes) // before core has the sheets

	edits := &sheetEdits{}
	(&sheetOutcomes{pulls: edits, issues: issueEdits{edits}}).Settle(t.Context(), outcomes)
	assert.Equal(t, []string{"7 " + rigViewer}, edits.pulls, "the review sheet takes the user off its pull request")
	assert.Equal(t, []int64{8}, edits.issues, "the issue sheet reads its issue again")

	logs := &testutil.Logs{}
	failing := &sheetEdits{fail: errCacheLocked}
	(&sheetOutcomes{pulls: failing, issues: issueEdits{failing}, logger: logs.Logger()}).Settle(t.Context(), outcomes[:1])
	assert.Contains(t, logs.String(), "could not follow an unassignment in the sheet's cache")
}
