package core

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/inbox"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/pushwatch"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestTheInboxsChangesWakeThePushWatch(t *testing.T) {
	t.Parallel()

	events := &testutil.Events{}
	waker := &pushWaker{}
	inboxEvents := pushesOnInbox{sink: events, pushes: waker}

	inboxEvents.Emit(event.InboxUpdated{})
	assert.Equal(t, []event.Event{event.InboxUpdated{}}, events.All(), "before the watch is built, the events still pass")

	c := bootstrap(t, testOptions(t))
	waker.watcher = c.PushWatcher

	inboxEvents.Emit(event.InboxUpdated{})
	inboxEvents.Emit(event.InboxItemsChanged{IDs: []int64{1}})
	assert.Equal(t, []event.Event{event.InboxUpdated{}, event.InboxUpdated{}, event.InboxItemsChanged{IDs: []int64{1}}},
		events.All(), "every event passes on, the watch woken alongside")
}

func TestThePushWatchReadsTheInboxsPullRequests(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	ctx := t.Context()
	targets := pushTargets{store: inbox.NewStore(db, nil)}

	got, err := targets.PushTargets(ctx)
	require.NoError(t, err)
	assert.Equal(t, []pushwatch.Target{}, got, "none before an account is known, never nil")

	now := time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)
	head := strings.Repeat("a", 40)

	testutil.Exec(t, db, `INSERT INTO accounts (id, login, created_at, updated_at) VALUES (1, 'octo-user', ?, ?)`, now, now)
	testutil.Exec(t, db, `INSERT INTO repos (id, owner, name, created_at, updated_at)
VALUES (1, 'acme-org', 'example-repo', ?, ?)`, now, now)
	testutil.Exec(t, db, `INSERT INTO items (id, account_id, repo_id, kind, number, node_id, head_ref, reason_authored,
    item_created_at, item_updated_at, fetched_at) VALUES (5, 1, 1, 'pr', 5, 'NODE_5', 'feat/watch', 1, ?, ?, ?)`,
		now, now, now)
	testutil.Exec(t, db, `INSERT INTO item_details (item_id, fetched_at, head_oid) VALUES (5, ?, ?)`, now, head)
	require.NoError(t, prefs.New(db, nil).Set(ctx, "inbox.viewer", "octo-user"))

	got, err = targets.PushTargets(ctx)
	require.NoError(t, err)
	assert.Equal(t, []pushwatch.Target{{
		ItemID: 5, Repo: repo.Name{Owner: boundOwner, Repo: boundRepo}, Branch: "feat/watch", Head: head,
	}}, got)
}

func TestThePushWatchListsTheClones(t *testing.T) {
	t.Parallel()

	c := bootstrap(t, testOptions(t))
	ctx := t.Context()

	root := t.TempDir()
	clone := filepath.Join(root, "example-repo")
	testutil.WriteFile(t, filepath.Join(clone, ".git", "config"), "[core]\n")
	testutil.WriteFile(t, filepath.Join(root, "notes", "readme.md"), "not a clone\n")
	require.NoError(t, testutil.ErrOf(c.Settings.SetCloneRoot(ctx, root)))

	dirs, err := pushClones{settings: c.Settings, home: t.TempDir()}.CloneDirs(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{clone}, dirs, "the clones alone")
}
