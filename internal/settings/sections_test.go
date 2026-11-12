package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

	require.NoError(t, testutil.ErrOf(svc.SetSectionCollapsed(ctx, ListAssigned, true)))
	require.NoError(t, testutil.ErrOf(svc.SetSectionCollapsed(ctx, ListReviewRequested, true)))

	got, err = svc.CollapsedSections(ctx)
	require.NoError(t, err)
	assert.Equal(t, []ListKey{ListReviewRequested, ListAssigned}, got, "in the inbox's order")

	require.NoError(t, testutil.ErrOf(svc.SetSectionCollapsed(ctx, ListReviewRequested, false)))
	require.NoError(t, testutil.ErrOf(svc.SetSectionCollapsed(ctx, ListAuthored, true)))
	require.NoError(t, testutil.ErrOf(svc.SetSectionCollapsed(ctx, ListAuthored, true)), "collapsing twice is harmless")

	got, err = svc.CollapsedSections(ctx)
	require.NoError(t, err)
	assert.Equal(t, []ListKey{ListAuthored, ListAssigned}, got)
}

func TestCollapsedSectionsIgnoreBadStoredValues(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)

	setPref(t, db, collapsedKey(ListReviewRequested), "yes please")
	setPref(t, db, collapsedKey(ListAuthored), "")
	setPref(t, db, collapsedKey(ListAssigned), "1")
	setPref(t, db, keyCollapsedPrefix+"mentions", "true")

	got, err := svc.CollapsedSections(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []ListKey{ListAssigned}, got, "only a boolean counts, and only for a known section")
}
