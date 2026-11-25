package inbox

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/github"
)

// The commits a pull request the user reviewed is at: the head now, and the one the user's
// verdict was left on.
const (
	headNow      = "3333333333333333333333333333333333333333"
	headReviewed = "2222222222222222222222222222222222222222"
)

// reviewed is pull request number by another, as the fourth search returns it: at headNow,
// with the user's verdict state left on commit.
func reviewed(number int, state, commit string) github.Item {
	it := byOther(pr(number, "MEMBER", false))
	it.HeadOID = headNow
	it.Verdict = &github.Verdict{State: state, Commit: commit}

	return it
}

// pushed is pull request number, which the user requested changes of before its author
// pushed.
func pushed(number int) github.Item {
	return reviewed(number, github.ReviewStateChangesRequested, headReviewed)
}

// withReviewed is in with the fourth search returning items.
func withReviewed(in *github.Inbox, items ...github.Item) *github.Inbox {
	in.Reviewed = github.SearchResult{IssueCount: len(items), Items: items}

	return in
}

// atHead is a pull request's details, its head at headNow.
func atHead() github.ItemDetails {
	d := prDetails()
	d.HeadOID = headNow

	return d
}

// detailsOf is details at headNow for each pull request numbered.
func detailsOf(numbers ...int) map[string]github.ItemDetails {
	out := make(map[string]github.ItemDetails, len(numbers))
	for _, n := range numbers {
		out[nodeID(n)] = atHead()
	}

	return out
}

func TestAPullRequestPushedSinceYourChangesIsInReviewRequested(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.details = detailsOf(5, 6, 7)
	h.client.inbox = withReviewed(fetched(nil, nil, nil),
		pushed(5),
		reviewed(6, github.ReviewStateChangesRequested, headNow),
		reviewed(7, github.ReviewStateApproved, headReviewed),
	)

	snap := h.refresh(t)

	require.Equal(t, []int{5}, numbers(snap.ReviewRequested),
		"only the one pushed to since you requested changes: not one at its head, nor one you approved")
	it := snap.ReviewRequested.Items[0]
	assert.Equal(t, ReviewRequestPushed, it.ReviewRequest)
	require.NotNil(t, it.Details)
	assert.True(t, it.Details.PushedSinceReview)
	assert.Equal(t, headNow, it.Details.HeadOID, "the head the details read, for the card's sessions")
	assert.Empty(t, it.ReviewRequestedAt, "listed for the push alone: no request arrived")
	assert.Empty(t, snap.Yours.Items)

	assert.Equal(t, [][]string{{nodeID(5)}}, h.client.detailsIDs,
		"the details of the one listed alone: one only the fourth search returned is never read")

	h.clock.Advance(time.Minute)
	h.refresh(t)
	assert.Equal(t, [][]string{{nodeID(5)}, {nodeID(5)}}, h.client.detailsIDs,
		"a listed one's details ride the first batch, and the others are never asked for")

	assert.Equal(t, [3]bool{false, false, false}, h.reasons(t, 6), "stored, in no list")
}

func TestAPushedPullRequestYouAreAssignedIsInReviewRequested(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.details = detailsOf(5)
	h.client.inbox = withReviewed(fetched(nil, nil, []github.Item{byOther(pr(5, "MEMBER", false))}), pushed(5))

	snap := h.refresh(t)

	require.Equal(t, []int{5}, numbers(snap.ReviewRequested))
	assert.True(t, snap.ReviewRequested.Items[0].Assigned, "its card still says it is assigned to you")
	assert.Equal(t, ReviewRequestPushed, snap.ReviewRequested.Items[0].ReviewRequest)
	assert.Empty(t, snap.Yours.Items)
}

// askingAgain is a pull request's details at headNow, asking the user by name to review it.
func askingAgain() map[string]github.ItemDetails {
	d := atHead()
	d.ReviewRequests = github.ReviewRequests{Users: []string{"octo-reviewer", octoUser}}

	return map[string]github.ItemDetails{nodeID(5): d}
}

func TestARequestAgainAfterAPushIsAnOrdinaryRequestWithTheTag(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.details = askingAgain()
	h.client.inbox = withReviewed(fetched([]github.Item{byOther(pr(5, "MEMBER", false))}, nil, nil), pushed(5))

	arrived := clock.Format(h.clock.Now().UTC())
	snap := h.refresh(t)

	require.Equal(t, []int{5}, numbers(snap.ReviewRequested))
	it := snap.ReviewRequested.Items[0]
	assert.Equal(t, ReviewRequestYou, it.ReviewRequest, "you can take yourself off it")
	assert.True(t, it.Details.PushedSinceReview)
	assert.True(t, it.Details.ReviewRequestedAgain, "the card says the review was requested again")
	assert.Equal(t, arrived, it.ReviewRequestedAt, "when the request arrived, which the card's sessions are held to")

	// Asked only through a team, the user isn't asked again by name.
	d := atHead()
	d.ReviewRequests = github.ReviewRequests{Teams: []string{"backend"}}
	h.client.details = map[string]github.ItemDetails{nodeID(5): d}
	h.clock.Advance(time.Minute)

	snap = h.refresh(t)
	require.Equal(t, []int{5}, numbers(snap.ReviewRequested))
	assert.False(t, snap.ReviewRequested.Items[0].Details.ReviewRequestedAgain)
	assert.True(t, snap.ReviewRequested.Items[0].Details.PushedSinceReview)
	assert.Equal(t, arrived, snap.ReviewRequested.Items[0].ReviewRequestedAt, "kept while it stays in the list")
}

func TestReviewingAPushedPullRequestTakesItOutAtOnce(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		event string
	}{
		{name: "approved", event: github.ReviewApprove},
		{name: "changes requested again", event: github.ReviewRequestChanges},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			h.client.details = detailsOf(5)
			h.client.inbox = withReviewed(fetched(nil, nil, nil), pushed(5))
			id := itemByNumber(t, h.refresh(t).ReviewRequested, 5).ID

			snap, err := NewActor(h.svc).Act(t.Context(), id, acting(Acted{
				Reviewed: true, ReviewEvent: tt.event, ReviewedAt: h.clock.Now(),
			}))
			require.NoError(t, err)
			assert.Empty(t, snap.ReviewRequested.Items, "the review takes it out")
			assert.Empty(t, h.client.itemIDs, "with no request")
			assert.Empty(t, h.client.itemsIDs)

			// The search trails the review: it still says the old verdict.
			h.clock.Advance(time.Minute)
			snap = h.refresh(t)
			assert.Empty(t, snap.ReviewRequested.Items, "a refresh the search hasn't caught up with keeps it out")

			// The author pushes again: the search, caught up, says the new verdict on the
			// commit before.
			again := reviewed(5, github.ReviewStateChangesRequested, headNow)
			again.HeadOID = strings.Repeat("4", 40)
			again.UpdatedAt = h.clock.Now().Add(time.Minute)
			h.client.inbox = withReviewed(fetched(nil, nil, nil), again)
			d := atHead()
			d.HeadOID = again.HeadOID
			h.client.details = map[string]github.ItemDetails{nodeID(5): d}
			h.clock.Advance(time.Hour)

			snap = h.refresh(t)
			require.Equal(t, []int{5}, numbers(snap.ReviewRequested), "pushed to since again, it comes back")
		})
	}
}

func TestAReviewFromTheSheetClearsTheTagInEveryList(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.details = detailsOf(5)
	h.client.inbox = withReviewed(fetched([]github.Item{byOther(pr(5, "MEMBER", false))}, nil, nil), pushed(5))
	id := itemByNumber(t, h.refresh(t).ReviewRequested, 5).ID

	_, err := NewActor(h.svc).Act(t.Context(), id, acting(Acted{
		Reviewed: true, ReviewEvent: github.ReviewComment, ReviewedAt: h.clock.Now(),
	}))
	require.NoError(t, err)

	// A comment is no verdict, but it answers the request: the card's tag waits for the
	// pull request to move.
	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)
	assert.Empty(t, snap.ReviewRequested.Items)

	h.client.inbox = withReviewed(fetched(nil, []github.Item{}, []github.Item{byOther(pr(5, "MEMBER", false))}), pushed(5))
	h.clock.Advance(time.Minute)

	snap = h.refresh(t)
	require.Equal(t, []int{5}, numbers(snap.Yours))
	assert.False(t, snap.Yours.Items[0].Details.PushedSinceReview, "you reviewed it since it last moved")
}

func TestAReadWithoutTheVerdictLeavesThePushedFlag(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.details = detailsOf(5)
	h.client.inbox = withReviewed(fetched(nil, nil, nil), pushed(5))
	id := itemByNumber(t, h.refresh(t).ReviewRequested, 5).ID

	h.client.items = map[string]*github.ItemUpdate{nodeID(5): open(h, 5, atHead())}

	snap, err := h.svc.RefreshItem(t.Context(), id)
	require.NoError(t, err)
	assert.Equal(t, []int{5}, numbers(snap.ReviewRequested),
		"a read that didn't ask for the verdict keeps the list as the refresh left it")
}

func TestYouCannotTakeYourselfOffAPushedPullRequest(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.details = detailsOf(5)
	h.client.inbox = withReviewed(fetched(nil, nil, nil), pushed(5))
	id := itemByNumber(t, h.refresh(t).ReviewRequested, 5).ID

	_, err := h.svc.RemoveReviewRequest(t.Context(), id)
	require.ErrorIs(t, err, ErrNotRequested, "there is no request to take yourself off")
}

func TestAPushedPullRequestArrivesOnceWithoutABurst(t *testing.T) {
	t.Parallel()

	h, sink := arrivalHarness(t)
	h.client.details = detailsOf(5, 6, 7)
	h.client.inbox = fetched(nil, nil, nil)
	h.refresh(t)

	// The first refresh after an upgrade finds a pull request pushed to long ago: it is
	// listed, but nothing arrives.
	old := pushed(5)

	h.clock.Advance(5 * time.Minute)
	h.client.inbox = withReviewed(fetched(nil, nil, nil), old)
	snap := h.refresh(t)
	require.Equal(t, []int{5}, numbers(snap.ReviewRequested))
	assert.Empty(t, sink.last(), "no burst for what was pushed before the last refresh")

	// One pushed since the last refresh arrives, as a review request that was pushed to.
	fresh := pushed(6)
	fresh.UpdatedAt = h.clock.Now()
	h.clock.Advance(5 * time.Minute)
	h.client.inbox = withReviewed(fetched(nil, nil, nil), old, fresh)
	h.refresh(t)

	got := sink.last()
	require.Len(t, got, 1)
	assert.Equal(t, ArrivalReviewRequested, got[0].Kind)
	assert.Equal(t, 6, got[0].Number)
	assert.True(t, got[0].Pushed)

	// Still listed at the next refresh, it doesn't arrive again; dismissed, nothing does.
	dismissed := pushed(7)
	dismissed.UpdatedAt = h.clock.Now()
	h.client.inbox = withReviewed(fetched(nil, nil, nil), old, fresh, dismissed)
	h.clock.Advance(time.Minute)
	h.refresh(t)

	got = sink.last()
	require.Len(t, got, 1)
	assert.Equal(t, 7, got[0].Number)

	_, err := h.svc.Dismiss(t.Context(), itemByNumber(t, h.refresh(t).ReviewRequested, 7).ID)
	require.NoError(t, err)

	calls := len(sink.calls)

	h.clock.Advance(time.Minute)
	h.refresh(t)
	assert.Len(t, sink.calls, calls, "nothing arrives again")
}

func TestARefreshCutShortKeepsAPushedPullRequestPastIt(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.details = detailsOf(5)
	h.client.inbox = withReviewed(fetched(nil, nil, nil), pushed(5))
	h.refresh(t)

	// The fourth search matched more than it returned, and #5 was past the page: it leaves
	// the list, as GitHub's own lists' items do, with no outcome told for it.
	cut := withReviewed(fetched(nil, nil, nil))
	cut.Reviewed.IssueCount = 150
	h.client.inbox = cut

	h.clock.Advance(time.Minute)

	snap := h.refresh(t)
	assert.Empty(t, snap.ReviewRequested.Items)
}

func TestAMergeFollowReadsAPushedPullRequestOnItsBase(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = withReviewed(fetched([]github.Item{pr(1, "OWNER", false)}, nil, nil), pushed(5))

	pushedOnMain := onBase("main", mergeableClean)
	pushedOnMain.HeadOID = headNow
	h.client.details = map[string]github.ItemDetails{nodeID(1): onBase("main", mergeableClean), nodeID(5): pushedOnMain}

	snap := h.refresh(t)
	require.ElementsMatch(t, []int{1, 5}, numbers(snap.ReviewRequested))

	f := NewMergeFollower(h.svc, h.logs.Logger())
	mergeFirst(t, h, map[int]int64{1: itemByNumber(t, snap.ReviewRequested, 1).ID})
	h.answer(map[int]string{5: mergeableClean})
	h.clock.Advance(mergeSettle)
	f.pass(t.Context())

	assert.Equal(t, [][]string{{nodeID(5)}}, h.client.itemsIDs,
		"a pull request pushed to since you requested changes is listed, so a merge into its base reads it again")
}

func TestReRequestingYourReviewPutsItBackInReviewRequestedAtOnce(t *testing.T) {
	t.Parallel()

	h, sink := arrivalHarness(t)
	h.client.details = detailsOf(5)
	h.client.inbox = withReviewed(fetched(nil, nil, nil), pushed(5))

	it := itemByNumber(t, h.refresh(t).ReviewRequested, 5)
	require.Equal(t, ReviewRequestPushed, it.ReviewRequest)
	require.False(t, it.Details.ReviewRequestedAgain, "nobody asked again yet")

	// A stamp of the user's leaving the list, as a review from the inbox leaves, which the
	// search's next answer would otherwise be held to.
	h.exec(t, `UPDATE items SET review_left_at = '2026-01-10 12:00:00+00:00' WHERE id = ?`, it.ID)

	at := h.clock.Now().Add(time.Second)
	snap, err := NewActor(h.svc).Act(t.Context(), it.ID, acting(Acted{
		Rerequested: true, RerequestedAt: at,
		ReviewRequests: github.ReviewRequests{Users: []string{"octo-reviewer", octoUser}, Teams: []string{}},
	}))
	require.NoError(t, err)

	require.Equal(t, []int{5}, numbers(snap.ReviewRequested))
	again := snap.ReviewRequested.Items[0]
	assert.Equal(t, ReviewRequestYou, again.ReviewRequest, "GitHub asks the user by name again, at once")
	assert.True(t, again.Details.PushedSinceReview, "still pushed to since the user's review")
	assert.True(t, again.Details.ReviewRequestedAgain, "the card says so at once, with no refresh")
	assert.Equal(t, clock.Format(at.UTC()), again.ReviewRequestedAt, "the request arrived when the user asked again")
	assert.Empty(t, snap.Yours.Items)
	assert.Empty(t, h.client.itemIDs, "with no request")
	assert.Empty(t, h.client.itemsIDs)

	assert.Equal(t, [3]bool{true, false, false}, h.reasons(t, 5))
	assert.Empty(t, h.reviewLeftAt(t, it.ID), "the stamp of leaving it is cleared")
	assert.Equal(t, []string{`["octo-reviewer","octo-user"]`},
		h.column(t, "SELECT review_users FROM item_details WHERE item_id = ?", it.ID), "who GitHub asks now")

	calls := len(sink.calls)

	// A refresh whose search hasn't caught up lists it as pushed to since, as before, and one
	// that has lists it as asked; it was listed all along, so neither tells an arrival. The
	// details GitHub answers ask the user again either way, so the card says so throughout.
	h.client.details = askingAgain()
	h.clock.Advance(time.Minute)

	snap = h.refresh(t)
	require.Equal(t, []int{5}, numbers(snap.ReviewRequested), "it never leaves the list")
	assert.Equal(t, ReviewRequestPushed, snap.ReviewRequested.Items[0].ReviewRequest)
	assert.True(t, snap.ReviewRequested.Items[0].Details.ReviewRequestedAgain, "a search that trails changes no card")

	asked := byOther(pr(5, "MEMBER", false))
	asked.HeadOID, asked.UpdatedAt = headNow, h.clock.Now()
	h.client.inbox = withReviewed(fetched([]github.Item{asked}, nil, nil), pushed(5))
	h.clock.Advance(time.Minute)

	snap = h.refresh(t)
	require.Equal(t, []int{5}, numbers(snap.ReviewRequested))
	assert.Equal(t, ReviewRequestYou, snap.ReviewRequested.Items[0].ReviewRequest)
	assert.True(t, snap.ReviewRequested.Items[0].Details.ReviewRequestedAgain)
	assert.Len(t, sink.calls, calls, "nothing arrives: it was in Review requested all along")
}

func TestAReviewerEditRewritesWhoTheCardAsksAtOnce(t *testing.T) {
	t.Parallel()

	h, _ := arrivalHarness(t)
	h.client.details = detailsOf(5)
	h.client.inbox = withReviewed(fetched(nil, nil, nil), pushed(5))

	it := itemByNumber(t, h.refresh(t).ReviewRequested, 5)
	left := h.reviewLeftAt(t, it.ID)

	snap, err := NewActor(h.svc).Act(t.Context(), it.ID, acting(Acted{
		RequestsEdited: true,
		ReviewRequests: github.ReviewRequests{Users: []string{"octo-reviewer", "octo-other"}, Teams: []string{"web"}},
	}))
	require.NoError(t, err)

	require.Equal(t, []int{5}, numbers(snap.ReviewRequested), "it stays in the list it was in")
	assert.Equal(t, []string{`["octo-reviewer","octo-other"]`},
		h.column(t, "SELECT review_users FROM item_details WHERE item_id = ?", it.ID), "who GitHub asks now")
	assert.Equal(t, []string{`["web"]`}, h.column(t, "SELECT review_teams FROM item_details WHERE item_id = ?", it.ID))
	assert.Equal(t, left, h.reviewLeftAt(t, it.ID), "nothing else moves")
	assert.Empty(t, h.client.itemIDs, "with no request")
	assert.Empty(t, h.client.itemsIDs)
}

func TestAReviewerEditIsRecordedWithItsTryAndRetry(t *testing.T) {
	t.Parallel()

	first := Acted{RequestsEdited: true, ReviewRequests: github.ReviewRequests{Users: []string{"octo-a"}, Teams: []string{}}}
	second := Acted{RequestsEdited: true, ReviewRequests: github.ReviewRequests{Users: []string{}, Teams: []string{}}}

	assert.Equal(t, first, first.with(Acted{}), "a later try that sent nothing keeps what the first made happen")
	assert.Equal(t, second, first.with(second), "the later answer says who it asks now")
	assert.True(t, first.any(), "a reviewer edit is something the inbox records")
}

func TestAReRequestIsRecordedWithItsTryAndRetry(t *testing.T) {
	t.Parallel()

	first := Acted{
		Rerequested: true, RerequestedAt: time.Date(2026, 1, 10, 12, 0, 0, 0, time.UTC),
		ReviewRequests: github.ReviewRequests{Users: []string{octoUser}, Teams: []string{}},
	}

	got := first.with(Acted{})
	assert.Equal(t, first, got, "a later try that sent nothing keeps what the first made happen")
	assert.True(t, got.any(), "a re-request is something the inbox records")

	got = Acted{}.with(first)
	assert.Equal(t, first, got)
	assert.False(t, Acted{}.any())
}
