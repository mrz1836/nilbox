package inbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// nudgeBody is the comment the tests' nudges post.
const nudgeBody = "Hi @octo-other, just checking in."

// commentURL is the address the fake GitHub gives a nudge's comment.
const commentURL = "https://github.com/acme-org/example-repo/pull/1#issuecomment-7"

func TestNudgePostsOneCommentAndRecordsIt(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	calls := h.events.Count()
	posted := h.clock.Now().Add(-time.Second)
	h.client.comment = &github.Comment{ID: 7, URL: commentURL, CreatedAt: posted}
	before := itemByNumber(t, h.snapshot(t).ReviewRequested, 1)
	require.NotNil(t, before.Details)
	require.Empty(t, before.NudgedAt, "never nudged")

	requests := h.client.requests()

	got, err := h.svc.Nudge(t.Context(), NudgeRequest{ItemID: ids[1], Body: nudgeBody})
	require.NoError(t, err)

	assert.Equal(t, []string{"comment acme-org/example-repo#1 " + nudgeBody}, h.client.edits, "one comment")
	assert.Equal(t, requests+1, h.client.requests(), "and no other request: no read, no account check")
	assert.Equal(t, commentURL, got.URL)

	after := itemByNumber(t, got.Snapshot.ReviewRequested, 1)
	assert.Equal(t, clock.Format(posted.UTC()), after.NudgedAt, "stamped with GitHub's time for the comment")
	require.NotNil(t, after.Details)
	assert.Equal(t, before.Details.Comments+1, after.Details.Comments, "the comment counts at once")
	assert.Equal(t, before.UpdatedAt, after.UpdatedAt, "the item's activity is GitHub's to say")
	assert.Equal(t, [3]bool{true, false, true}, h.reasons(t, 1), "it stays in its lists")
	assert.Greater(t, h.events.Count(), calls, "the window hears of it")
}

func TestNudgeWithoutGitHubsTimeStampsNow(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	h.client.comment = &github.Comment{URL: commentURL}

	got, err := h.svc.Nudge(t.Context(), NudgeRequest{ItemID: ids[1], Body: nudgeBody})
	require.NoError(t, err)

	assert.Equal(t, clock.Format(h.clock.Now().UTC()), itemByNumber(t, got.Snapshot.ReviewRequested, 1).NudgedAt)
}

func TestANudgeBeforeTheLatestVerdictIsAPastRounds(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	posted := h.clock.Now()
	h.client.comment = &github.Comment{URL: commentURL, CreatedAt: posted}

	_, err := h.svc.Nudge(t.Context(), NudgeRequest{ItemID: ids[1], Body: nudgeBody})
	require.NoError(t, err)

	h.exec(t, "UPDATE items SET viewer_review_state = 'CHANGES_REQUESTED', viewer_review_at = ? WHERE id = ?",
		posted.Add(-time.Minute).UTC(), ids[1])
	h.svc.snapshots.empty()
	assert.Equal(t, clock.Format(posted.UTC()), itemByNumber(t, h.snapshot(t).ReviewRequested, 1).NudgedAt,
		"a nudge after the verdict shows")

	h.exec(t, "UPDATE items SET viewer_review_at = ? WHERE id = ?", posted.Add(time.Minute).UTC(), ids[1])
	h.svc.snapshots.empty()
	assert.Empty(t, itemByNumber(t, h.snapshot(t).ReviewRequested, 1).NudgedAt,
		"a new round of review starts with none")
}

func TestNudgeRefusesWhatHasNoAuthorToNudge(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)

	_, err := h.svc.Nudge(t.Context(), NudgeRequest{ItemID: ids[3], Body: nudgeBody})
	require.ErrorIs(t, err, ErrNotPull, "an issue")

	_, err = h.svc.Nudge(t.Context(), NudgeRequest{ItemID: ids[1], Body: " \n\t"})
	require.ErrorIs(t, err, ErrEmptyNudge, "no text")
	assert.Equal(t, "Write the comment first.", uierr.Message(err))

	h.exec(t, "UPDATE items SET author_login = 'Octo-User' WHERE id = ?", ids[1])
	_, err = h.svc.Nudge(t.Context(), NudgeRequest{ItemID: ids[1], Body: nudgeBody})
	require.ErrorIs(t, err, ErrNotNudgeable, "the user's own, in any case")
	assert.Equal(t, "acme-org/example-repo#1 is your own pull request.", uierr.Message(err))

	for _, state := range []string{github.StateClosed, github.StateMerged} {
		h.exec(t, "UPDATE items SET state = ? WHERE id = ?", state, ids[2])
		_, err = h.svc.Nudge(t.Context(), NudgeRequest{ItemID: ids[2], Body: nudgeBody})
		require.ErrorIs(t, err, ErrNotNudgeable, state)
		assert.Equal(t, "acme-org/example-repo#2 is no longer open.", uierr.Message(err))
	}

	_, err = h.svc.Nudge(t.Context(), NudgeRequest{ItemID: 999, Body: nudgeBody})
	require.ErrorIs(t, err, item.ErrNotFound)

	assert.Empty(t, h.client.edits, "GitHub was never asked")
}

func TestNudgeRefusesAnotherSignedInAccount(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	h.svc = h.service() // a token this Service has not used yet
	h.client.viewer = "someone-else"

	_, err := h.svc.Nudge(t.Context(), NudgeRequest{ItemID: ids[1], Body: nudgeBody})
	require.ErrorIs(t, err, ErrOtherAccount)
	assert.Empty(t, h.client.edits, "nothing is posted as another account")
	assert.Empty(t, itemByNumber(t, h.snapshot(t).ReviewRequested, 1).NudgedAt)
}

func TestANudgeGitHubRefusedRecordsNothing(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	h.client.editErr = errFakeGate

	_, err := h.svc.Nudge(t.Context(), NudgeRequest{ItemID: ids[1], Body: nudgeBody})
	require.ErrorIs(t, err, errFakeGate)
	assert.Empty(t, itemByNumber(t, h.snapshot(t).ReviewRequested, 1).NudgedAt)
}

func TestANudgeGitHubPostedButTheInboxCouldNotRecordSaysSo(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	h.client.comment = &github.Comment{URL: commentURL, CreatedAt: h.clock.Now()}
	before := itemByNumber(t, h.snapshot(t).ReviewRequested, 1)
	require.NotNil(t, before.Details)

	refuse(t, h, "BEFORE UPDATE OF comments ON item_details", "")

	_, err := h.svc.Nudge(t.Context(), NudgeRequest{ItemID: ids[1], Body: nudgeBody})
	require.ErrorContains(t, err, "count the comment")
	assert.Equal(t, "GitHub posted the comment, but the inbox could not record it. The next refresh catches it up.",
		uierr.Message(err))
	assert.Len(t, h.client.edits, 1, "GitHub was asked once")

	h.svc.snapshots.empty()
	after := itemByNumber(t, h.snapshot(t).ReviewRequested, 1)
	assert.Empty(t, after.NudgedAt, "nothing was stored")
	require.NotNil(t, after.Details)
	assert.Equal(t, before.Details.Comments, after.Details.Comments)
}
