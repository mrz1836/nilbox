package inbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/inbox/internal/queries"
	"github.com/mrz1836/nilbox/internal/item"
)

// verdict is the user's verdict state on commit, submitted at.
func verdict(state, commit string, at time.Time) *github.Verdict {
	return &github.Verdict{State: state, Commit: commit, SubmittedAt: at.UTC()}
}

// readOf is GitHub's answer to a read of pull request 5 alone, by another, open at
// headNow, with the user's verdict v (nil with none) read with it.
func readOf(h *harness, v *github.Verdict) *github.ItemUpdate {
	it := byOther(pr(5, "MEMBER", false))
	it.State, it.UpdatedAt, it.HeadOID, it.Verdict = github.StateOpen, h.clock.Now(), headNow, v

	return &github.ItemUpdate{Found: true, Item: it, Details: atHead(), RateLimit: itemRate(), VerdictRead: true}
}

// storedVerdictOf returns the user's verdict stored on the item with id.
func (h *harness) storedVerdictOf(t *testing.T, id int64) queries.GetViewerVerdictRow {
	t.Helper()

	got, err := h.svc.q.GetViewerVerdict(t.Context(), id)
	require.NoError(t, err)

	return got
}

// pushedHarness is a harness whose last refresh listed pull request 5 in Review requested,
// pushed to since the user requested changes, and returns the item's id.
func pushedHarness(t *testing.T) (*harness, int64) {
	t.Helper()

	h := newHarness(t)
	h.client.details = detailsOf(5)
	h.client.inbox = withReviewed(fetched(nil, nil, nil), pushed(5))

	return h, itemByNumber(t, h.refresh(t).ReviewRequested, 5).ID
}

func TestASingleReadOfAnApprovalTakesAPushedPullRequestOut(t *testing.T) {
	t.Parallel()

	h, id := pushedHarness(t)

	approvedAt := h.clock.Now().Add(-time.Minute)
	h.client.items = map[string]*github.ItemUpdate{
		nodeID(5): readOf(h, verdict(github.ReviewStateApproved, headNow, approvedAt)),
	}

	snap, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)
	assert.Empty(t, snap.ReviewRequested.Items, "an approval since answers the push, at once")

	got := h.storedVerdictOf(t, id)
	assert.Equal(t, github.ReviewStateApproved, got.ViewerReviewState)
	assert.Equal(t, headNow, got.ViewerReviewCommit)
	require.NotNil(t, got.ViewerReviewAt)
	assert.True(t, approvedAt.Equal(*got.ViewerReviewAt))
	require.NotNil(t, got.ViewerReviewSeenAt)
	assert.True(t, approvedAt.Equal(*got.ViewerReviewSeenAt))
}

func TestASingleReadOfChangesRequestedAtTheHeadTakesItOut(t *testing.T) {
	t.Parallel()

	h, id := pushedHarness(t)

	h.client.items = map[string]*github.ItemUpdate{
		nodeID(5): readOf(h, verdict(github.ReviewStateChangesRequested, headNow, h.clock.Now())),
	}

	snap, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)
	assert.Empty(t, snap.ReviewRequested.Items, "changes requested again at the head answer the push")
}

func TestASingleReadOfChangesRequestedAtAnOldCommitKeepsItPushed(t *testing.T) {
	t.Parallel()

	h, id := pushedHarness(t)

	h.client.items = map[string]*github.ItemUpdate{
		nodeID(5): readOf(h, verdict(github.ReviewStateChangesRequested, headReviewed, h.clock.Now().Add(-time.Hour))),
	}

	snap, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)
	require.Equal(t, []int{5}, numbers(snap.ReviewRequested), "still pushed to since the user's request for changes")
	assert.True(t, snap.ReviewRequested.Items[0].Details.PushedSinceReview)
}

func TestASingleReadOfADismissedVerdictClearsIt(t *testing.T) {
	t.Parallel()

	h, id := pushedHarness(t)

	h.client.items = map[string]*github.ItemUpdate{nodeID(5): readOf(h, nil)}

	snap, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)
	assert.Empty(t, snap.ReviewRequested.Items, "no verdict is pushed past")

	got := h.storedVerdictOf(t, id)
	assert.Empty(t, got.ViewerReviewState)
	assert.Empty(t, got.ViewerReviewCommit)
	assert.Nil(t, got.ViewerReviewAt)
}

func TestASingleReadKeepsTheCommitTheRefreshCovered(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.details = detailsOf(5)

	// The user requested changes, then alone pushed: the whole refresh moved the verdict's
	// commit onto the head (see coverOwnPushes), so it isn't pushed to.
	requestedAt := h.clock.Now().Add(-time.Hour)
	covered := reviewed(5, github.ReviewStateChangesRequested, headNow)
	covered.Verdict.SubmittedAt = requestedAt
	h.client.inbox = withReviewed(fetched(nil, nil, nil), covered)
	h.refresh(t)

	id := h.storedItemNumbered(t, 5).ID

	// A read alone reads the same review at the commit it was left on.
	h.client.items = map[string]*github.ItemUpdate{
		nodeID(5): readOf(h, verdict(github.ReviewStateChangesRequested, headReviewed, requestedAt)),
	}

	snap, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)
	assert.Empty(t, snap.ReviewRequested.Items, "a read alone never lists it")

	got := h.storedVerdictOf(t, id)
	assert.Equal(t, headNow, got.ViewerReviewCommit, "the same review keeps the commit the refresh covered")
}

func TestAReadKeepsAVerdictNewerThanItsOwn(t *testing.T) {
	t.Parallel()

	h, id := pushedHarness(t)

	// The user approved from the inbox after the read went out.
	approvedAt := h.clock.Now()
	h.exec(t, `UPDATE items SET viewer_review_state = 'APPROVED', viewer_review_commit = ?, viewer_review_at = ?,
    viewer_review_seen_at = ? WHERE id = ?`, headNow, approvedAt, approvedAt, id)

	h.client.items = map[string]*github.ItemUpdate{
		nodeID(5): readOf(h, verdict(github.ReviewStateChangesRequested, headReviewed, approvedAt.Add(-time.Hour))),
	}

	_, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)

	got := h.storedVerdictOf(t, id)
	assert.Equal(t, github.ReviewStateApproved, got.ViewerReviewState, "an older read never writes over a newer verdict")
	assert.Equal(t, headNow, got.ViewerReviewCommit)
}

func TestAnotherAccountsTokenReadsNoVerdict(t *testing.T) {
	t.Parallel()

	h, id := pushedHarness(t)
	h.client.items = map[string]*github.ItemUpdate{
		nodeID(5): readOf(h, verdict(github.ReviewStateApproved, headNow, h.clock.Now())),
	}

	// gh auth switch: the token now belongs to another account, whose verdict GitHub
	// would call the viewer's.
	h.svc.viewers.Remember(github.NewToken(fakeToken), "someone-else")

	snap, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, []int{5}, numbers(snap.ReviewRequested), "another account's verdict is not the user's")
	assert.Equal(t, github.ReviewStateChangesRequested, h.storedVerdictOf(t, id).ViewerReviewState)
}

func TestAReadBeforeTheTokensAccountIsKnownReadsNoVerdict(t *testing.T) {
	t.Parallel()

	h, id := pushedHarness(t)
	h.client.items = map[string]*github.ItemUpdate{
		nodeID(5): readOf(h, verdict(github.ReviewStateApproved, headNow, h.clock.Now())),
	}

	// A new start: no refresh has said whose the token is yet, and asking would cost a
	// request.
	h.svc = h.service()

	snap, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, []int{5}, numbers(snap.ReviewRequested))
	assert.Equal(t, github.ReviewStateChangesRequested, h.storedVerdictOf(t, id).ViewerReviewState)
}

func TestAnOlderRefreshKeepsTheVerdictALaterReadStored(t *testing.T) {
	t.Parallel()

	h, id := pushedHarness(t)
	c := h.gated()
	h.svc.viewers.Remember(github.NewToken(fakeToken), octoUser)

	// The refresh's inbox query went out before the agent approved, so it still reads the
	// request for changes.
	h.clock.Advance(time.Minute)
	refresh := refreshUntilGate(t, h, c)

	h.client.items = map[string]*github.ItemUpdate{
		nodeID(5): readOf(h, verdict(github.ReviewStateApproved, headNow, h.clock.Now())),
	}

	snap, err := h.svc.RefreshItem(bounded(t), id)
	require.NoError(t, err)
	assert.Empty(t, snap.ReviewRequested.Items)

	c.inboxGate.release()

	got := await(t, refresh)
	require.NoError(t, got.err)
	assert.Empty(t, got.snap.ReviewRequested.Items, "the read is newer than the refresh it outlived")
	assert.Equal(t, github.ReviewStateApproved, h.storedVerdictOf(t, id).ViewerReviewState)
}

func TestAReviewMadeElsewhereIsToldOnceFromASingleRead(t *testing.T) {
	t.Parallel()

	h, id := pushedHarness(t)
	sink := h.telling(t)
	h.svc.viewers.Remember(github.NewToken(fakeToken), octoUser)

	approvedAt := h.clock.Now().Add(-time.Minute)
	h.client.items = map[string]*github.ItemUpdate{
		nodeID(5): readOf(h, verdict(github.ReviewStateApproved, headNow, approvedAt)),
	}

	_, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, []Outcome{told(OutcomeReviewed, prRef(5), approvedAt)}, sink.all(),
		"an agent's approval is the user's review, at GitHub's time")

	// The card's refresh reads it again, and the next whole refresh's fourth search too.
	_, err = h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)

	again := reviewed(5, github.ReviewStateApproved, headNow)
	again.Verdict.SubmittedAt = approvedAt
	h.client.inbox = withReviewed(fetched(nil, nil, nil), again)
	h.clock.Advance(time.Minute)
	h.refresh(t)

	assert.Len(t, sink.all(), 1, "each review is told once")
}

func TestAReviewMadeElsewhereIsToldOnceFromAWholeRefresh(t *testing.T) {
	t.Parallel()

	h, _ := pushedHarness(t)
	sink := h.telling(t)

	// The user approved in a browser; the next whole refresh's fourth search reads it.
	approvedAt := h.clock.Now().Add(-10 * time.Minute)
	approved := reviewed(5, github.ReviewStateApproved, headNow)
	approved.Verdict.SubmittedAt = approvedAt
	h.client.inbox = withReviewed(fetched(nil, nil, nil), approved)
	h.clock.Advance(time.Minute)

	snap := h.refresh(t)
	assert.Empty(t, snap.ReviewRequested.Items)
	assert.Contains(t, sink.all(), told(OutcomeReviewed, prRef(5), approvedAt))

	h.clock.Advance(time.Minute)
	h.refresh(t)

	count := 0

	for _, o := range sink.all() {
		if o.Kind == OutcomeReviewed {
			count++
		}
	}

	assert.Equal(t, 1, count, "the next refresh reads it again and tells nothing")
}

func TestYourOwnReviewIsNotToldAgainByARead(t *testing.T) {
	t.Parallel()

	h, id := pushedHarness(t)
	sink := h.telling(t)
	h.svc.viewers.Remember(github.NewToken(fakeToken), octoUser)

	reviewedAt := h.clock.Now()
	_, err := NewActor(h.svc).Act(t.Context(), id, acting(Acted{
		Reviewed: true, ReviewEvent: github.ReviewApprove, ReviewedAt: reviewedAt,
	}))
	require.NoError(t, err)
	require.Equal(t, []Outcome{told(OutcomeReviewed, prRef(5), reviewedAt)}, sink.all())

	h.client.items = map[string]*github.ItemUpdate{
		nodeID(5): readOf(h, verdict(github.ReviewStateApproved, headNow, reviewedAt)),
	}

	_, err = h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)
	assert.Len(t, sink.all(), 1, "the inbox told its own review when it sent it")
}

func TestAReviewSeenLongAfterIsNotTold(t *testing.T) {
	t.Parallel()

	h, id := pushedHarness(t)
	sink := h.telling(t)
	h.svc.viewers.Remember(github.NewToken(fakeToken), octoUser)

	// The first read after an upgrade finds an approval from yesterday.
	h.client.items = map[string]*github.ItemUpdate{
		nodeID(5): readOf(h, verdict(github.ReviewStateApproved, headNow, h.clock.Now().Add(-24*time.Hour))),
	}

	snap, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)
	assert.Empty(t, snap.ReviewRequested.Items, "the card still catches up")
	assert.Empty(t, sink.all(), "a review this old was handled long ago")
}

func TestAWholeRefreshTellsNoReviewOfAnotherAccount(t *testing.T) {
	t.Parallel()

	h, _ := pushedHarness(t)
	sink := h.telling(t)

	// gh auth switch: the refresh reads another account, whose last refresh this isn't.
	approved := reviewed(5, github.ReviewStateApproved, headNow)
	approved.Verdict.SubmittedAt = h.clock.Now()
	in := withReviewed(fetched(nil, nil, nil), approved)
	in.Viewer.Login = "someone-else"
	h.client.inbox = in
	h.clock.Advance(time.Minute)
	h.refresh(t)

	assert.Empty(t, sink.all())
}

func TestNewlySubmitted(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC)
	at := now.Add(-time.Minute)
	earlier, same := at.Add(-time.Second), at.Add(500*time.Millisecond)

	tests := []struct {
		name string
		v    *github.Verdict
		seen *time.Time
		want bool
	}{
		{name: "none", v: nil, want: false},
		{name: "no time", v: &github.Verdict{State: github.ReviewStateApproved}, want: false},
		{name: "never seen", v: verdict(github.ReviewStateApproved, headNow, at), want: true},
		{name: "newer than seen", v: verdict(github.ReviewStateApproved, headNow, at), seen: &earlier, want: true},
		{name: "seen, to the second", v: verdict(github.ReviewStateApproved, headNow, at), seen: &same, want: false},
		{name: "older than seen", v: verdict(github.ReviewStateApproved, headNow, earlier), seen: &at, want: false},
		{name: "at the window's edge", v: verdict(github.ReviewStateApproved, headNow, now.Add(-reviewWindow)), want: true},
		{name: "past the window", v: verdict(github.ReviewStateApproved, headNow, now.Add(-reviewWindow-time.Second)), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, newlySubmitted(tt.v, tt.seen, now))
		})
	}
}

func TestACardKnowsTheUsersOwnVerdict(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.details = detailsOf(5, 6, 7, 8)

	assigned := make([]github.Item, 0, 4)
	for _, n := range []int{5, 6, 7, 8} {
		assigned = append(assigned, byOther(pr(n, "MEMBER", false)))
	}

	h.client.inbox = withReviewed(fetched(nil, nil, assigned),
		reviewed(5, github.ReviewStateChangesRequested, headNow),
		pushed(6),
		reviewed(7, github.ReviewStateApproved, headReviewed),
	)

	snap := h.refresh(t)

	waiting := itemByNumber(t, snap.Yours, 5).Details
	require.NotNil(t, waiting)
	assert.Equal(t, ViewerVerdictChanges, waiting.ViewerVerdict, "changes requested at its head: it waits on the author")
	assert.False(t, waiting.PushedSinceReview)

	again := itemByNumber(t, snap.ReviewRequested, 6).Details
	require.NotNil(t, again)
	assert.Equal(t, ViewerVerdictChanges, again.ViewerVerdict)
	assert.True(t, again.PushedSinceReview, "the author pushed since")

	assert.Equal(t, ViewerVerdictApproved, itemByNumber(t, snap.Yours, 7).Details.ViewerVerdict)
	assert.Equal(t, ViewerVerdictNone, itemByNumber(t, snap.Yours, 8).Details.ViewerVerdict, "never reviewed")

	assert.Len(t, h.client.detailsIDs, 1, "the verdict rides the reads the refresh makes anyway")

	// A read of it alone that finds the review dismissed clears it.
	h.client.items = map[string]*github.ItemUpdate{nodeID(5): readOf(h, nil)}

	snap, err := h.svc.RefreshItem(t.Context(), itemByNumber(t, snap.Yours, 5).ID)
	require.NoError(t, err)
	assert.Equal(t, ViewerVerdictNone, itemByNumber(t, snap.Yours, 5).Details.ViewerVerdict)
}

func TestAnIssueHoldsNoVerdict(t *testing.T) {
	t.Parallel()

	assert.Equal(t, ViewerVerdictNone, viewerVerdict(""))
	assert.Equal(t, ViewerVerdictNone, viewerVerdict(github.ReviewStateDismissed))

	row := itemRow{Kind: item.KindIssue, HasDetails: true, ViewerReviewState: github.ReviewStateChangesRequested}
	assert.Equal(t, ViewerVerdictNone, row.details(octoUser, nil).ViewerVerdict, "only a pull request is reviewed")
}
