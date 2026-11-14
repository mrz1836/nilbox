package review

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/timeline"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// conversation is a page of #7's conversation as GitHub gives it: the user's own comment,
// a review of an earlier commit with a line comment, and an approval of the head.
func conversation() github.TimelinePage {
	return github.TimelinePage{
		Items: []github.TimelineItem{
			{Type: github.TimelineComment, ID: "IC_1", CreatedAt: updatedAt, Actor: "Octo-User", Body: "Ready?"},
			{
				Type: github.TimelineReview, ID: "PRR_1", CreatedAt: updatedAt, Actor: octoOther, Body: "One thing.",
				URL: "https://github.com/acme-org/example-repo/pull/7#pullrequestreview-1", Outdated: true,
				ReviewState: github.ReviewStateChangesRequested, LineCommentCount: 2,
				LineComments: []github.ReviewLineComment{{
					ID: "PRRC_1", Path: cacheFile, Line: 3, Body: "Why?", DiffHunk: "@@ -1 +1,2 @@\n line\n+new",
				}},
			},
			{Type: github.TimelineReview, ID: "PRR_2", CreatedAt: updatedAt, Actor: "octo-reviewer", ReviewState: github.ReviewStateApproved},
			{Type: "PinnedEvent", ID: "PE_1", CreatedAt: updatedAt},
		},
		HasEarlier: true, Before: "cursor-7", Total: 12,
	}
}

func TestRefreshReadsTheConversation(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.review.Timeline = conversation()
	h.github.review.LastEditedAt = updatedAt
	h.github.review.Reactions = []github.Reaction{{Content: "HEART", Count: 2}}

	r := h.refreshed(t)

	assert.Equal(t, stamp(updatedAt), r.EditedAt)
	assert.Equal(t, []timeline.Reaction{{Content: "HEART", Count: 2}}, r.Reactions)
	assert.False(t, r.BodyTrimmed)
	assert.Equal(t, 12, r.Timeline.Total)
	assert.Equal(t, "cursor-7", r.Timeline.Before)
	require.Len(t, r.Timeline.Items, 3, "a type the sheet doesn't read is left out")

	assert.True(t, r.Timeline.Items[0].Mine, "the user's own, by their login in any case")

	review := r.Timeline.Items[1]
	assert.Equal(t, timeline.KindReviewed, review.Kind)
	assert.Equal(t, timeline.ReviewChangesRequested, review.ReviewState)
	assert.True(t, review.Outdated, "a review of an earlier commit")
	assert.False(t, review.Mine)
	assert.Equal(t, 1, review.MoreLineComments)
	assert.Equal(t, []timeline.LineComment{{
		ID: "PRRC_1", Path: cacheFile, Line: 3, Body: "Why?", Hunk: []string{" line", "+new"},
	}}, review.LineComments)

	assert.Equal(t, timeline.ReviewApproved, r.Timeline.Items[2].ReviewState)
	assert.False(t, r.Timeline.Items[2].Outdated)
}

func TestABuiltReviewHoldsNoNilList(t *testing.T) {
	t.Parallel()

	r := build(storedPull(), nil, nil, PrefRepo)
	assert.NotNil(t, r.Reactions)
	assert.NotNil(t, r.Timeline.Items)

	// A cached entry from before line comments were read has none, never null.
	c := &cached{head: headA, context: pullContext{Timeline: []timeline.Entry{{Kind: timeline.KindComment}}}}
	r = build(storedPull(), c, nil, PrefRepo)
	assert.NotNil(t, r.Reactions)
	assert.NotNil(t, r.Timeline.Items[0].LineComments)
	assert.Equal(t, 1, r.Timeline.Total, "never fewer than the entries held")
}

func TestOpenStartsAVisitFromTheReadItShows(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	r, err := h.svc.Open(t.Context(), pullID)
	require.NoError(t, err)
	assert.Empty(t, r.LastSeenAt, "never read, so never seen")

	h.refreshed(t)

	r, err = h.svc.Open(t.Context(), pullID)
	require.NoError(t, err)
	assert.Equal(t, stamp(updatedAt), r.LastSeenAt, "the visit starts from the read the cache holds")

	// GitHub has more now: the refresh brings it, and the visit still starts where it did.
	h.clock.Advance(time.Minute)
	h.github.review = pullReview(headA, github.MergeStateClean)
	h.github.review.Update.Item.UpdatedAt = updatedAt.Add(30 * time.Minute)
	r = h.refreshed(t)
	assert.Equal(t, stamp(updatedAt), r.LastSeenAt)

	r, err = h.svc.ReadAgain(t.Context(), pullID)
	require.NoError(t, err)
	assert.Equal(t, stamp(updatedAt), r.LastSeenAt, "until the next Open")

	r, err = h.svc.Open(t.Context(), pullID)
	require.NoError(t, err)
	assert.Equal(t, stamp(updatedAt.Add(30*time.Minute)), r.LastSeenAt)
}

func TestARecentReadIsReadAgainOnceTheCardShowsActivity(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.refreshed(t)
	assert.Equal(t, 1, h.github.called(callReview))

	h.refreshed(t)
	assert.Equal(t, 1, h.github.called(callReview), "a read this recent is served as it is")

	// A comment since: the inbox's card says so, and the read under 20 seconds old would
	// never show it.
	h.inbox.mu.Lock()
	h.inbox.pull.UpdatedAt = updatedAt.Add(time.Second)
	h.inbox.mu.Unlock()

	h.refreshed(t)
	assert.Equal(t, 2, h.github.called(callReview))

	// A context an older build cached says nothing of when, so it is read again.
	c := mustCache(t, h)
	assert.False(t, recent(storedPull(), &cached{head: headA, contextAt: testNow, context: pullContext{}}, testNow))
	assert.True(t, recent(storedPull(), &cached{head: headA, contextAt: testNow, context: c.context}, testNow))
}

func TestEarlierReadsAPageAndKeepsNothing(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.review.Timeline = conversation()
	h.refreshed(t)

	h.github.earlier = &github.TimelinePage{Items: []github.TimelineItem{
		{Type: github.TimelineComment, ID: "IC_0", CreatedAt: testNow, Actor: octoUser, Body: "First."},
		{Type: github.TimelineMerged, ID: "ME_0", CreatedAt: testNow, Actor: octoOther, Commit: "abc1234", Branch: "main"},
		{Type: "PinnedEvent", ID: "PE_0", CreatedAt: testNow},
	}, Total: 9}

	page, err := h.svc.Earlier(t.Context(), pullID, "cursor-7")
	require.NoError(t, err)

	assert.Equal(t, timeline.Page{Items: []timeline.Entry{
		{
			Kind: timeline.KindComment, ID: "IC_0", At: stamp(testNow), Actor: octoUser, Mine: true, Body: "First.",
			LineComments: []timeline.LineComment{},
		},
		{
			Kind: timeline.KindMerged, ID: "ME_0", At: stamp(testNow), Actor: octoOther, Commit: "abc1234",
			Branch: "main", LineComments: []timeline.LineComment{},
		},
	}, Total: 9}, page, "the user's own marked, a type the sheet doesn't read left out, and no earlier page")
	assert.Equal(t, []string{"cursor-7"}, h.github.befores)
	assert.Equal(t, 1, h.inbox.peeks, "through the inbox, which stores nothing")
	assert.Equal(t, 1, h.github.called(callReview))
	assert.Equal(t, "IC_1", mustCache(t, h).context.Timeline[0].ID, "the cache keeps the newest page")

	h.github.earlierErr = github.ErrInvalidCursor
	_, err = h.svc.Earlier(t.Context(), pullID, "")
	require.ErrorIs(t, err, github.ErrInvalidCursor)
	assert.Equal(t, "That part of the conversation can't be read. Reload the pull request, then try again.",
		uierr.Message(err))

	h.github.earlierErr = errFakeRead
	_, err = h.svc.Earlier(t.Context(), pullID, "cursor-7")
	require.ErrorIs(t, err, errFakeRead)

	h.inbox.mu.Lock()
	h.inbox.pulled = errFakeRead
	h.inbox.mu.Unlock()

	_, err = h.svc.Earlier(t.Context(), pullID, "cursor-7")
	require.ErrorIs(t, err, errFakeRead)
}

func TestFitContextCutsLongTextsInTurn(t *testing.T) {
	t.Parallel()

	small := pullContext{Body: "Short.", Timeline: []timeline.Entry{{Kind: timeline.KindComment, Body: "Hi."}}}
	encoded, err := fitContext(small)
	require.NoError(t, err)
	assert.NotContains(t, encoded, `"bodyTrimmed":true`, "a context that fits is kept whole")

	reviews := func(count, size int) []timeline.Entry {
		out := make([]timeline.Entry, 0, count)
		for range count {
			out = append(out, timeline.Entry{
				Kind: timeline.KindReviewed, Body: strings.Repeat("r", size),
				LineComments: []timeline.LineComment{{Body: strings.Repeat("l", size)}},
			})
		}

		return out
	}

	tests := []struct {
		name  string
		pc    pullContext
		limit int
	}{
		{name: "a long description", pc: pullContext{Body: strings.Repeat("b", 2<<20)}, limit: 16 << 10},
		{name: "long reviews", pc: pullContext{Timeline: reviews(40, 20<<10)}, limit: 4 << 10},
		{name: "many reviews", pc: pullContext{Timeline: reviews(200, 5<<10)}, limit: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			encoded, err := fitContext(tt.pc)
			require.NoError(t, err)
			assert.LessOrEqual(t, len(encoded), maxContextBytes)

			pc, err := store.DecodeJSON[pullContext](encoded)
			require.NoError(t, err)
			assert.Len(t, pc.Timeline, len(tt.pc.Timeline), "no entry is dropped")

			if tt.pc.Body != "" {
				assert.True(t, pc.BodyTrimmed)
				assert.Len(t, pc.Body, tt.limit)
			}

			for _, e := range pc.Timeline {
				assert.True(t, e.Trimmed)
				assert.Len(t, e.Body, tt.limit)
				assert.True(t, e.LineComments[0].Trimmed)
			}
		})
	}
}

func TestEveryWriteOfTheContextFitsIt(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.review.Body = strings.Repeat("d", 2<<20)

	r := h.refreshed(t)
	assert.True(t, r.BodyTrimmed, "the read that lists the files")
	assert.Len(t, r.Body, 16<<10)

	// Read again with nothing pushed: the context alone is written.
	h.clock.Advance(time.Minute)
	h.github.review = pullReview(headA, github.MergeStateClean)
	h.github.review.Timeline.Items = []github.TimelineItem{
		{Type: github.TimelineComment, ID: "IC_1", CreatedAt: updatedAt, Body: strings.Repeat("c", 2<<20)},
	}

	r = h.refreshed(t)
	assert.Equal(t, 1, h.github.called(callDiff), "the files at that head are cached")
	require.Len(t, r.Timeline.Items, 1)
	assert.True(t, r.Timeline.Items[0].Trimmed)
	assert.Len(t, r.Timeline.Items[0].Body, 16<<10)
}
