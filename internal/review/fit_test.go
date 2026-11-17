package review

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheFitterKeepsOnlyTheLineCommentsTheDiffShows(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.refreshed(t)

	comments := []LineComment{
		{Path: cacheFile, Line: 2, Side: SideRight, Body: "on the diff"},
		{Path: cacheFile, Line: 99, Side: SideRight, Body: "off it"},
		{Path: "logo.png", Line: 1, Side: SideRight, Body: "a file with no diff"},
		{Path: "nope.go", Line: 1, Side: SideRight, Body: "no such file"},
	}

	kept, dropped, err := NewFitter(h.svc).Fit(t.Context(), pullID, headA, comments)
	require.NoError(t, err)
	assert.Equal(t, comments[:1], kept)
	assert.Equal(t, 3, dropped)

	_, _, err = NewFitter(h.svc).Fit(t.Context(), pullID, headB, comments)
	require.ErrorIs(t, err, ErrPushedSince, "comments meant for another head fit nothing")

	none, dropped, err := NewFitter(h.svc).Fit(t.Context(), pullID, headB, nil)
	require.NoError(t, err)
	assert.NotNil(t, none)
	assert.Zero(t, dropped)
}
