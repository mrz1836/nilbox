package settings

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestNoSectionIsCollapsedOnAFreshDatabase(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	got, err := svc.CollapsedSections(t.Context())
	require.NoError(t, err)
	assert.NotNil(t, got, "an empty list, not nil, so the UI receives []")
	assert.Empty(t, got)
}

func TestCollapsedSectionsPersist(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)
	ctx := t.Context()

	require.NoError(t, testutil.ErrOf(svc.SetSectionCollapsed(ctx, ListAssigned, true)))
	require.NoError(t, testutil.ErrOf(svc.SetSectionCollapsed(ctx, ListReviewRequested, true)))

	got, err := svc.CollapsedSections(ctx)
	require.NoError(t, err)
	assert.Equal(t, []ListKey{ListReviewRequested, ListAssigned}, got, "in the inbox's order")

	require.NoError(t, testutil.ErrOf(svc.SetSectionCollapsed(ctx, ListReviewRequested, false)))
	require.NoError(t, testutil.ErrOf(svc.SetSectionCollapsed(ctx, ListAuthored, true)))
	require.NoError(t, testutil.ErrOf(svc.SetSectionCollapsed(ctx, ListAuthored, true)), "collapsing twice is harmless")

	got, err = svc.CollapsedSections(ctx)
	require.NoError(t, err)
	assert.Equal(t, []ListKey{ListAuthored, ListAssigned}, got)
}

func TestCollapsedSectionsSurviveARestart(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	home := t.TempDir()
	path := filepath.Join(t.TempDir(), "app.db")

	db := testutil.DBAt(t, path)
	require.NoError(t, testutil.ErrOf(serviceOver(t, db, home).SetSectionCollapsed(ctx, ListAuthored, true)))
	require.NoError(t, db.Close())

	got, err := serviceOver(t, testutil.DBAt(t, path), home).CollapsedSections(ctx)
	require.NoError(t, err)
	assert.Equal(t, []ListKey{ListAuthored}, got)
}

func TestSetSectionCollapsedRejectsAnUnknownSection(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	for _, section := range []ListKey{"", "Authored", "snoozed", "ui.collapsed.authored", " authored"} {
		_, err := svc.SetSectionCollapsed(t.Context(), section, true)
		require.ErrorIs(t, err, ErrUnknownSection, string(section))
	}

	_, err := svc.SetSectionCollapsed(t.Context(), "mentions", true)
	require.ErrorContains(t, err, `"mentions": choose one of reviewRequested, authored, assigned`)

	got, err := svc.CollapsedSections(t.Context())
	require.NoError(t, err)
	assert.Empty(t, got, "rejected sections must not be stored")
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

func TestCollapsedSectionsDatabaseErrors(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	require.NoError(t, db.Close())

	_, err := svc.CollapsedSections(t.Context())
	require.ErrorContains(t, err, "read the collapsed inbox sections")

	_, err = svc.SetSectionCollapsed(t.Context(), ListAuthored, true)
	require.ErrorContains(t, err, "begin transaction")
}
