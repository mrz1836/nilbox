package launchdefaults_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
)

func TestRestoreFindsADefaultsPromptByItsUUIDBeforeItsNickname(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	uuids, ids := h.uuids(t), h.ids(t)

	// The copy names Review by a nickname the restored library gave to another prompt, and
	// Work on it by one the restore cleaned: each UUID still finds its prompt.
	kept := []launchdefaults.Kept{{
		Owner: acme, PRPrompt: "Work on it", IssuePrompt: "work  on it",
		PromptUUIDs: map[string]string{"pr": uuids["Review"], "issue": uuids["Work on it"]},
	}}

	byUUID := func(uuid, nickname string) (int64, bool) {
		for n, u := range uuids {
			if u == uuid {
				return ids[n], true
			}
		}

		id, ok := ids[nickname]

		return id, ok
	}

	n, err := launchdefaults.Restore(t.Context(), h.store, kept, byUUID)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	back, err := h.store.All(t.Context())
	require.NoError(t, err)
	require.Len(t, back, 1)
	assert.Equal(t, new(ids["Review"]), back[0].PRPromptID, "the UUID wins over a nickname another prompt has")
	assert.Equal(t, new(ids["Work on it"]), back[0].IssuePromptID, "the UUID finds one whose nickname was cleaned")
}

func TestABackupsDefaultFindsAPromptRenamedSinceByItsUUID(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ctx := t.Context()

	_, err := h.svc.Save(ctx, launchdefaults.Input{Owner: acme, PRPromptID: &h.review})
	require.NoError(t, err)

	kept, err := launchdefaults.NewBackupPart(h.svc).Kept(ctx)
	require.NoError(t, err)
	require.Len(t, kept, 1)
	require.NotEmpty(t, kept[0].PromptUUIDs["pr"])

	// The user renamed Review since, and cleared the row.
	review, err := h.library.Lookup(ctx, h.review)
	require.NoError(t, err)
	require.NoError(t, h.svc.Remove(ctx, acme, ""))

	known := func(uuid, _ string, kind item.Kind) bool { return uuid == review.UUID && kind == item.KindPR }
	find := func(_ context.Context, uuid, _ string, kind item.Kind) (int64, bool, error) {
		return h.review, known(uuid, "", kind), nil
	}

	plan := h.importWith(t, kept, backup.ModeMerge, known, find)
	assert.Empty(t, plan.Notes, "the prompt is known by its UUID, so nothing inherits")

	got, err := h.store.All(ctx)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, &h.review, got[0].PRPromptID)
}

func TestABackupsRowsTheSameButForTheirUUIDsChangeNothing(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	_, err := h.svc.Save(t.Context(), launchdefaults.Input{Owner: acme, PRPromptID: &h.review})
	require.NoError(t, err)

	kept, err := launchdefaults.NewBackupPart(h.svc).Kept(t.Context())
	require.NoError(t, err)

	// The same row, from before UUIDs or from another Mac, and one naming a UUID that isn't one.
	kept[0].PromptUUIDs = map[string]string{"pr": "not-a-uuid"}

	plan := h.importWith(t, kept, backup.ModeMerge, h.knows, h.find)
	assert.Equal(t, backup.Count{Same: 1}, plan.Count, "a default's UUID is no setting of the row's")
}
