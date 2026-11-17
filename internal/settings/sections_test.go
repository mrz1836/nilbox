package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/prefs/prefval"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestCollapsedSectionsPersist(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)
	ctx := t.Context()

	got, err := svc.CollapsedSections(ctx)
	require.NoError(t, err)
	assert.NotNil(t, got, "an empty list, not nil, so the UI receives []")
	assert.Empty(t, got)

	require.NoError(t, testutil.ErrOf(svc.SetSectionCollapsed(ctx, ListYours, true)))
	require.NoError(t, testutil.ErrOf(svc.SetSectionCollapsed(ctx, ListReviewRequested, true)))

	got, err = svc.CollapsedSections(ctx)
	require.NoError(t, err)
	assert.Equal(t, []ListKey{ListReviewRequested, ListYours}, got, "in the inbox's order")

	require.NoError(t, testutil.ErrOf(svc.SetSectionCollapsed(ctx, ListReviewRequested, false)))
	require.NoError(t, testutil.ErrOf(svc.SetSectionCollapsed(ctx, ListYours, true)), "collapsing twice is harmless")

	got, err = svc.CollapsedSections(ctx)
	require.NoError(t, err)
	assert.Equal(t, []ListKey{ListYours}, got)
}

func TestSetSectionCollapsedRefusesAFormerList(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	for _, former := range []ListKey{formerAuthored, formerAssigned} {
		_, err := svc.SetSectionCollapsed(t.Context(), former, true)
		require.ErrorIs(t, err, ErrUnknownSection, string(former))
	}
}

func TestCollapsedSectionsIgnoreBadStoredValues(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)

	setPref(t, db, collapsedKey(ListReviewRequested), "yes please")
	setPref(t, db, collapsedKey(ListYours), "1")
	setPref(t, db, keyCollapsedPrefix+"mentions", prefval.EncodeBool(true))
	setPref(t, db, keyCollapsedPrefix+"assigned", prefval.EncodeBool(true))

	got, err := svc.CollapsedSections(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []ListKey{ListYours}, got, "only a boolean counts, and only for a known section")
}

func TestAdoptSectionsCarriesTheAssignedListOverToYours(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	ctx := t.Context()

	setPref(t, db, "ui.collapsed.authored", "false")
	setPref(t, db, "ui.collapsed.assigned", prefval.EncodeBool(true))
	setPref(t, db, "ui.listKind.authored", "issue")
	setPref(t, db, "ui.listKind.assigned", "pr")
	setPref(t, db, "ui.statusSort.authored", prefval.EncodeBool(true))
	setPref(t, db, "ui.statusSort.assigned", prefval.EncodeBool(true))
	setPref(t, db, "ui.statusSort.yours", "false")
	setPref(t, db, "ui.statusSort.reviewRequested", prefval.EncodeBool(true))

	require.NoError(t, AdoptSections(ctx, db, nil))

	got, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, []ListKey{ListYours}, got.CollapsedSections, "Yours takes Assigned's")
	assert.Equal(t, map[ListKey]ListKind{ListYours: ListKindPRs}, got.InboxOrder.Kinds, "Authored's is dropped")
	assert.Equal(t, []ListKey{ListReviewRequested}, got.InboxOrder.StatusSorted,
		"Yours keeps a value of its own, and Review requested is untouched")

	stored, err := prefs.New(db, nil).GetMany(ctx,
		"ui.collapsed.authored", "ui.collapsed.assigned", "ui.listKind.authored", "ui.listKind.assigned",
		"ui.statusSort.authored", "ui.statusSort.assigned")
	require.NoError(t, err)
	assert.Empty(t, stored, "the former lists' keys are deleted")

	require.NoError(t, AdoptSections(ctx, db, nil))

	again, err := svc.Get(ctx)
	require.NoError(t, err)
	assert.Equal(t, got, again, "running it again changes nothing")
}

func TestAdoptSectionsOnAFreshDatabaseStoresNothing(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	before, err := svc.Get(t.Context())
	require.NoError(t, err)

	require.NoError(t, AdoptSections(t.Context(), db, nil))

	after, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, before, after)

	stored, err := prefs.New(db, nil).GetMany(t.Context(), collapsedKey(ListYours), statusSortKey(ListYours),
		listKindKey(ListYours))
	require.NoError(t, err)
	assert.Empty(t, stored, "nothing is written")
}

func TestAdoptSectionsWritesNothingOnceTheFormerKeysAreGone(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	db.SetMaxOpenConns(1)
	testutil.Exec(t, db, "PRAGMA query_only = ON")

	require.NoError(t, AdoptSections(t.Context(), db, nil), "a start with nothing to carry over only reads")

	testutil.Exec(t, db, "PRAGMA query_only = OFF")
	setPref(t, db, "ui.collapsed.assigned", prefval.EncodeBool(true))
	testutil.Exec(t, db, "PRAGMA query_only = ON")

	require.Error(t, AdoptSections(t.Context(), db, nil), "one with a former key writes")
}

func TestAdoptSectionsChangesAllOrNothing(t *testing.T) {
	t.Parallel()

	db := testutil.DB(t)
	setPref(t, db, "ui.collapsed.assigned", prefval.EncodeBool(true))
	setPref(t, db, "ui.listKind.assigned", "pr")
	testutil.Exec(t, db, `CREATE TRIGGER refuse BEFORE DELETE ON prefs WHEN OLD.key = 'ui.listKind.assigned'
		BEGIN SELECT RAISE(ABORT, 'refused'); END`)

	require.Error(t, AdoptSections(t.Context(), db, nil))

	stored, err := prefs.New(db, nil).GetMany(t.Context(), "ui.collapsed.assigned", collapsedKey(ListYours))
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"ui.collapsed.assigned": prefval.EncodeBool(true)}, stored, "the first section's change rolled back")
}
