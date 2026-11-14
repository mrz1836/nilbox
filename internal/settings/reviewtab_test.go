package settings

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheReviewSheetOpensOnTheTabLastChosen(t *testing.T) {
	t.Parallel()

	svc, _, _ := newTestService(t)

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ReviewTabFiles, got.ReviewTab, "a fresh database opens on the files")

	got, err = svc.SetReviewTab(t.Context(), ReviewTabComments)
	require.NoError(t, err)
	assert.Equal(t, ReviewTabComments, got.ReviewTab)

	got, err = svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ReviewTabComments, got.ReviewTab, "the choice is stored")

	_, err = svc.SetReviewTab(t.Context(), "checks")
	require.ErrorIs(t, err, ErrInvalidReviewTab)

	got, err = svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ReviewTabComments, got.ReviewTab, "a refused tab stores nothing")
}

func TestAnUnreadableReviewTabOpensOnTheFiles(t *testing.T) {
	t.Parallel()

	svc, db, _ := newTestService(t)
	setPref(t, db, KeyReviewTab, "checks")

	got, err := svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, ReviewTabFiles, got.ReviewTab)
}
