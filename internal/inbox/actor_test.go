package inbox

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/sensitive"
	"github.com/mrz1836/nilbox/internal/uierr"
)

var errMergeRefused = errors.New("GitHub refused the merge")

// approve is an action that approves, with no time from GitHub, and then fails with err
// (nil to succeed), counting its runs.
func approve(err error, runs *int) func(context.Context, github.Token, PullTarget) (Acted, error) {
	return func(context.Context, github.Token, PullTarget) (Acted, error) {
		*runs++

		return Acted{Reviewed: true}, err
	}
}

func TestActRecordsAReviewAndAMerge(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	actor := NewActor(h.svc)

	var got PullTarget

	snap, err := actor.Act(t.Context(), id, func(_ context.Context, tok github.Token, target PullTarget) (Acted, error) {
		got = target

		assert.False(t, tok.IsZero())

		return Acted{Reviewed: true, ReviewedAt: time.Date(2026, 1, 10, 12, 11, 0, 0, time.UTC)}, nil
	})
	require.NoError(t, err)

	assert.Equal(t, id, got.ID)
	assert.Equal(t, "acme-org/example-repo#1", got.Ref.String())
	assert.Equal(t, item.KindPR, got.Ref.Kind)
	assert.Equal(t, nodeID(1), got.NodeID)
	assert.Equal(t, octoUser, got.Login)
	assert.True(t, got.ReviewRequested)
	assert.True(t, got.Assigned)

	assert.Equal(t, []int{2}, numbers(snap.ReviewRequested), "a review fulfills the request")
	assert.Equal(t, []int{1}, numbers(snap.Assigned), "and leaves the other lists")

	snap, err = actor.Act(t.Context(), id, func(context.Context, github.Token, PullTarget) (Acted, error) {
		return Acted{Merged: true}, nil
	})
	require.NoError(t, err)
	assert.Empty(t, snap.Assigned.Items, "a merge takes it out of every list at once")
	assert.Equal(t, [3]bool{false, false, false}, h.reasons(t, 1))
}

func TestActRecordsAClose(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)

	snap, err := NewActor(h.svc).Act(t.Context(), id, func(context.Context, github.Token, PullTarget) (Acted, error) {
		return Acted{Reviewed: true, Closed: true}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, []int{2}, numbers(snap.ReviewRequested), "a close takes it out of every list at once")
	assert.Empty(t, snap.Assigned.Items)
	assert.Equal(t, [3]bool{false, false, false}, h.reasons(t, 1))
}

func TestActKeepsWhatGitHubDidWhenAStepFails(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	runs := 0

	snap, err := NewActor(h.svc).Act(t.Context(), id, approve(errMergeRefused, &runs))
	require.ErrorIs(t, err, errMergeRefused)
	assert.Equal(t, 1, runs)
	assert.Equal(t, []int{2}, numbers(snap.ReviewRequested), "the approval stands")

	// The stamp is the item's own update time, so a refresh whose search still lists it
	// keeps it out.
	h.refresh(t)
	assert.False(t, h.reasons(t, 1)[0], "a refresh before the item moves keeps it out of the list")
}

func TestActRefusesAnotherAccountsToken(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)

	// A service started since the refresh has not seen the token yet, and checks it.
	h.svc = h.service()
	h.client.viewer = "someone-else"
	runs := 0

	_, err := NewActor(h.svc).Act(t.Context(), id, approve(nil, &runs))
	require.ErrorIs(t, err, ErrOtherAccount)
	assert.Zero(t, runs, "nothing was sent")
	assert.True(t, h.reasons(t, 1)[0])
}

func TestActRefusesWhatIsNotAListedPullRequest(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	runs := 0
	actor := NewActor(h.svc)

	_, err := actor.Act(t.Context(), ids[3], approve(nil, &runs))
	require.ErrorIs(t, err, ErrNotPull, "an issue")

	_, err = actor.Act(t.Context(), 9999, approve(nil, &runs))
	require.ErrorIs(t, err, item.ErrNotFound)

	h.exec(t, "UPDATE items SET reason_review_requested = 0, reason_assigned = 0, reason_authored = 0 WHERE id = ?", ids[2])

	_, err = actor.Act(t.Context(), ids[2], approve(nil, &runs))
	require.ErrorIs(t, err, ErrNotPull, "a pull request in no list")
	assert.Contains(t, uierr.Message(err), "no longer in your inbox")

	h.exec(t, "UPDATE accounts SET login = 'someone-else'")

	_, err = actor.Act(t.Context(), ids[1], approve(nil, &runs))
	require.ErrorIs(t, err, item.ErrNotFound, "another account's item")
	assert.Zero(t, runs)
}

func TestActRetriesARefusedTokenWithoutRepeatingAStep(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	approved, tries := false, 0

	snap, err := NewActor(h.svc).Act(t.Context(), id, func(context.Context, github.Token, PullTarget) (Acted, error) {
		tries++

		if !approved {
			approved = true

			return Acted{Reviewed: true}, &github.StatusError{StatusCode: http.StatusUnauthorized, Message: "Bad credentials"}
		}

		return Acted{Merged: true}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, 2, tries, "tried once more with a fresh token")
	assert.Empty(t, snap.Assigned.Items, "both tries' steps are recorded")
}

func TestActThatChangesNothingStoresNothing(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	before := len(h.events.All())

	snap, err := NewActor(h.svc).Act(t.Context(), id, func(context.Context, github.Token, PullTarget) (Acted, error) {
		return Acted{}, errMergeRefused
	})
	require.ErrorIs(t, err, errMergeRefused)
	assert.ElementsMatch(t, []int{1, 2}, numbers(snap.ReviewRequested))
	assert.Len(t, h.events.All(), before, "nothing changed")
}

func TestAMergeStaysUnlistedThroughARefreshThatStartedEarlier(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	c := h.gated()

	// The refresh's inbox query was sent before the merge, so it still lists #1, open.
	refresh := refreshUntilGate(t, h, c)

	snap, err := NewActor(h.svc).Act(bounded(t), id, func(context.Context, github.Token, PullTarget) (Acted, error) {
		return Acted{Merged: true}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, []int{2}, numbers(snap.ReviewRequested))

	c.inboxGate.release()

	got := await(t, refresh)
	require.NoError(t, got.err)
	assert.Equal(t, []int{2}, numbers(got.snap.ReviewRequested), "the merged item stays out of the lists")
	assert.Empty(t, got.snap.Assigned.Items)
}

func TestReadPullRewritesTheCardAndServesTheFileCheck(t *testing.T) {
	t.Parallel()

	outsider := from(1, "outsider", "NONE", false)
	h := filesHarness(t, outsider)
	h.refresh(t)
	h.clock.Advance(time.Minute)

	id := h.storedItemNumbered(t, 1).ID
	h.client.fileReads = nil

	moved := outsider
	moved.Title = "Read again"
	details := prDetails()
	details.HeadOID = headB

	var target PullTarget

	snap, err := NewActor(h.svc).ReadPull(t.Context(), id, func(_ context.Context, _ github.Token, t PullTarget) (PullRead, error) {
		target = t

		return PullRead{
			Update: &github.ItemUpdate{Found: true, Item: moved, Details: details, RateLimit: itemRate()},
			Files:  changed(headB),
		}, nil
	})
	require.NoError(t, err)

	assert.Equal(t, headA, target.Head, "the head the inbox stored")

	it := itemByNumber(t, snap.ReviewRequested, 1)
	assert.Equal(t, "Read again", it.Title)
	require.NotNil(t, it.Files)
	assert.Equal(t, sensitive.StatusFlagged, it.Files.Status, "the read's files are the new head's check")
	assert.Equal(t, headB, h.storedHead(t, "item_files", id))
	assert.Empty(t, h.client.fileReads, "no request of its own")
}

func TestReadPullLeavesADueCheckWithoutFilesForTheNextRefresh(t *testing.T) {
	t.Parallel()

	outsider := from(1, "outsider", "NONE", false)
	h := filesHarness(t, outsider)
	h.refresh(t)

	id := h.storedItemNumbered(t, 1).ID
	details := prDetails()
	details.HeadOID = headB

	_, err := NewActor(h.svc).ReadPull(t.Context(), id, func(context.Context, github.Token, PullTarget) (PullRead, error) {
		return PullRead{
			Update: &github.ItemUpdate{Found: true, Item: outsider, Details: details, RateLimit: itemRate()},
			Files:  changed(headA),
		}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, headA, h.storedHead(t, "item_files", id), "files of another head are no check")
}

func TestReadPullFailures(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	actor := NewActor(h.svc)

	_, err := actor.ReadPull(t.Context(), id, func(context.Context, github.Token, PullTarget) (PullRead, error) {
		return PullRead{}, errMergeRefused
	})
	require.ErrorIs(t, err, errMergeRefused)

	_, err = actor.ReadPull(t.Context(), id, func(context.Context, github.Token, PullTarget) (PullRead, error) {
		return PullRead{}, nil
	})
	require.ErrorIs(t, err, ErrIncompleteFetch)

	_, err = actor.ReadPull(t.Context(), 9999, func(context.Context, github.Token, PullTarget) (PullRead, error) {
		return PullRead{}, nil
	})
	require.ErrorIs(t, err, item.ErrNotFound)
}

func TestReadPullOfAMergedPullRequestUnlistsIt(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)

	merged := pr(1, "OWNER", false)
	merged.State = github.StateMerged

	snap, err := NewActor(h.svc).ReadPull(t.Context(), id, func(context.Context, github.Token, PullTarget) (PullRead, error) {
		return PullRead{Update: &github.ItemUpdate{Found: true, Item: merged, Details: prDetails(), RateLimit: itemRate()}}, nil
	})
	require.NoError(t, err)
	assert.Equal(t, []int{2}, numbers(snap.ReviewRequested))
}

func TestActsOnOneItemAtATime(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	unlock, err := h.svc.items.lock(t.Context(), id)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = NewActor(h.svc).Act(ctx, id, approve(nil, new(int)))
	require.ErrorIs(t, err, context.Canceled, "it waits for other work on the item")

	_, err = NewActor(h.svc).ReadPull(ctx, id, func(context.Context, github.Token, PullTarget) (PullRead, error) {
		return PullRead{}, nil
	})
	require.ErrorIs(t, err, context.Canceled)

	unlock()
}

func TestStoreReadsAReviewItem(t *testing.T) {
	t.Parallel()

	h, id := itemHarness(t)
	h.exec(t, `UPDATE repos SET viewer_permission = 'WRITE', viewer_merge_method = 'SQUASH', squash_merge_allowed = 1,
		auto_merge_allowed = 1, default_branch = 'main'`)

	got, err := NewStore(h.pool, h.clock).ReviewItem(t.Context(), id)
	require.NoError(t, err)

	assert.Equal(t, nodeID(1), got.NodeID)
	assert.Equal(t, item.KindPR, got.Kind)
	assert.Equal(t, "Item 1", got.Title)
	assert.Equal(t, octoUser, got.Viewer)
	assert.Equal(t, "WRITE", got.ViewerPermission)
	assert.Equal(t, "SQUASH", got.ViewerMergeMethod)
	assert.True(t, got.SquashMergeAllowed)
	assert.True(t, got.AutoMergeAllowed)
	assert.False(t, got.RebaseMergeAllowed)
	assert.Equal(t, "main", got.DefaultBranch)
	assert.True(t, got.HasDetails)
	assert.Equal(t, []string{"enhancement"}, got.Labels)
	assert.Equal(t, "APPROVED", got.ReviewDecision)
	assert.True(t, got.ReviewRequested)

	_, err = NewStore(h.pool, h.clock).ReviewItem(t.Context(), 9999)
	require.ErrorIs(t, err, item.ErrNotFound)
}

func TestTheCardShowsAutoMerge(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	a := pr(1, "OWNER", false)
	h.client.inbox = fetched([]github.Item{a}, nil, nil)

	d := prDetails()
	d.AutoMerge = github.AutoMerge{Method: "SQUASH", EnabledAt: time.Date(2026, 1, 10, 11, 0, 0, 0, time.UTC)}
	h.client.details = map[string]github.ItemDetails{nodeID(1): d}

	snap := h.refresh(t)
	got := itemByNumber(t, snap.ReviewRequested, 1)
	require.NotNil(t, got.Details)
	assert.Equal(t, "SQUASH", got.Details.AutoMerge)

	assert.Nil(t, autoMergeAt(github.AutoMerge{}))
	assert.Nil(t, autoMergeAt(github.AutoMerge{Method: "MERGE"}), "on, but GitHub did not say since when")
}
