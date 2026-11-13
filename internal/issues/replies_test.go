package issues

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
)

func TestRepliesOffersTheLibrarysCommentReplies(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	got, err := h.svc.Replies(t.Context())
	require.NoError(t, err)
	assert.NotNil(t, got, "a library with none offers an empty list")
	assert.Empty(t, got)

	h.replies.offered = []Reply{{ID: 7, Name: "Looking into it", Nickname: "Looking"}}

	got, err = h.svc.Replies(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []Reply{{ID: 7, Name: "Looking into it", Nickname: "Looking"}}, got)

	h.replies.err = errNoSuchReply

	got, err = h.svc.Replies(t.Context())
	require.ErrorIs(t, err, errNoSuchReply)
	assert.NotNil(t, got)
}

func TestFillReplyFillsInAReplyForTheIssue(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.replies.filled = map[int64]string{7: "Looking into it, @octo-user."}

	text, err := h.svc.FillReply(t.Context(), issueID, 7)
	require.NoError(t, err)
	assert.Equal(t, "Looking into it, @octo-user.", text)

	_, err = h.svc.FillReply(t.Context(), issueID, 8)
	require.ErrorIs(t, err, errNoSuchReply)

	h.inbox.issue.Kind = item.KindPR

	_, err = h.svc.FillReply(t.Context(), issueID, 7)
	require.ErrorIs(t, err, ErrNotIssue, "the sheet fills in replies for an issue alone")
}
