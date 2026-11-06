package settings

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/store"
)

func TestNoSectionIsCollapsedOnAFreshDatabase(t *testing.T) {
	svc, _, _ := newTestService(t)

	got, err := svc.CollapsedSections(t.Context())
	require.NoError(t, err)
	assert.NotNil(t, got, "an empty list, not nil, so the UI receives []")
	assert.Empty(t, got)
}

func TestCollapsedSectionsPersist(t *testing.T) {
	svc, _, _ := newTestService(t)
	ctx := t.Context()

	require.NoError(t, svc.SetSectionCollapsed(ctx, SectionAssigned, true))
	require.NoError(t, svc.SetSectionCollapsed(ctx, SectionReviewRequested, true))

	got, err := svc.CollapsedSections(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{SectionReviewRequested, SectionAssigned}, got, "in the inbox's order")

	require.NoError(t, svc.SetSectionCollapsed(ctx, SectionReviewRequested, false))
	require.NoError(t, svc.SetSectionCollapsed(ctx, SectionAuthored, true))
	require.NoError(t, svc.SetSectionCollapsed(ctx, SectionAuthored, true), "collapsing twice is harmless")

	got, err = svc.CollapsedSections(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{SectionAuthored, SectionAssigned}, got)
}

func TestCollapsedSectionsSurviveARestart(t *testing.T) {
	ctx := t.Context()
	home := t.TempDir()
	path := filepath.Join(t.TempDir(), "app.db")

	db, err := store.Open(ctx, path)
	require.NoError(t, err)
	require.NoError(t, NewService(db, home).SetSectionCollapsed(ctx, SectionAuthored, true))
	require.NoError(t, store.Close(db))

	got, err := NewService(openDB(t, path), home).CollapsedSections(ctx)
	require.NoError(t, err)
	assert.Equal(t, []string{SectionAuthored}, got)
}

func TestSetSectionCollapsedRejectsAnUnknownSection(t *testing.T) {
	svc, _, _ := newTestService(t)

	for _, section := range []string{"", "Authored", "snoozed", "inbox.collapsed.authored", " authored"} {
		err := svc.SetSectionCollapsed(t.Context(), section, true)
		require.ErrorIs(t, err, ErrUnknownSection, section)
	}

	err := svc.SetSectionCollapsed(t.Context(), "mentions", true)
	require.ErrorContains(t, err, `"mentions": choose one of reviewRequested, authored, assigned`)

	got, err := svc.CollapsedSections(t.Context())
	require.NoError(t, err)
	assert.Empty(t, got, "rejected sections must not be stored")
}

func TestCollapsedSectionsIgnoreBadStoredValues(t *testing.T) {
	svc, db, _ := newTestService(t)

	setPref(t, db, keyCollapsedPrefix+SectionReviewRequested, "yes please")
	setPref(t, db, keyCollapsedPrefix+SectionAuthored, "")
	setPref(t, db, keyCollapsedPrefix+SectionAssigned, "1")
	setPref(t, db, keyCollapsedPrefix+"mentions", "true")

	got, err := svc.CollapsedSections(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{SectionAssigned}, got, "only a boolean counts, and only for a known section")
}

func TestCollapsedSectionsDatabaseErrors(t *testing.T) {
	svc, db, _ := newTestService(t)
	require.NoError(t, store.Close(db))

	_, err := svc.CollapsedSections(t.Context())
	require.ErrorContains(t, err, "read the collapsed inbox sections")

	err = svc.SetSectionCollapsed(t.Context(), SectionAuthored, true)
	require.ErrorContains(t, err, "save setting inbox.collapsed.authored")
}
