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
	"github.com/mrz1836/nilbox/internal/uierr"
)

var errFakeGate = errors.New("the budget refused the edit")

// FetchViewer answers with the canned account.
func (c *fakeClient) FetchViewer(_ context.Context, tok github.Token) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.gotTokens = append(c.gotTokens, tok.Reveal())
	c.viewerReads++

	if c.viewerErr != nil {
		return "", github.Redact(c.viewerErr, tok)
	}

	if c.viewer == "" {
		return octoUser, nil
	}

	return c.viewer, nil
}

// RemoveAssignee records the edit and answers with the canned answer.
func (c *fakeClient) RemoveAssignee(_ context.Context, tok github.Token, ref item.Ref, login string) (*github.Edited, error) {
	return c.recordEdit(tok, "unassign "+ref.String()+" "+login)
}

// RemoveReviewRequest records the edit and answers with the canned answer.
func (c *fakeClient) RemoveReviewRequest(
	_ context.Context, tok github.Token, ref item.Ref, login string,
) (*github.Edited, error) {
	return c.recordEdit(tok, "unreview "+ref.String()+" "+login)
}

// recordEdit records one edit and answers it.
func (c *fakeClient) recordEdit(tok github.Token, edit string) (*github.Edited, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.gotTokens = append(c.gotTokens, tok.Reveal())
	c.edits = append(c.edits, edit)

	if c.editErr != nil {
		return nil, github.Redact(c.editErr, tok)
	}

	if c.edited == nil {
		return &github.Edited{}, nil
	}

	return new(*c.edited), nil
}

// editHarness refreshes an inbox where pull request #1 asks for the user's review by name
// and is assigned to them, #2 asks only a team, and issue #3 is assigned, and returns the
// harness with the stored ids by number.
func editHarness(t *testing.T) (*harness, map[int]int64) {
	t.Helper()

	h := newHarness(t)
	a, b, c := byOther(pr(1, "MEMBER", false)), byOther(pr(2, "MEMBER", false)), byOther(issue("example-repo", 3, "MEMBER"))
	h.client.inbox = fetched([]github.Item{a, b}, nil, []github.Item{a, c})

	asksUser, asksTeam := prDetails(), prDetails()
	asksUser.ReviewRequests = github.ReviewRequests{Users: []string{"someone", "Octo-User"}, Teams: []string{"backend"}}
	asksTeam.ReviewRequests = github.ReviewRequests{Users: []string{"someone"}, Teams: []string{"backend"}}
	h.client.details = map[string]github.ItemDetails{nodeID(1): asksUser, nodeID(2): asksTeam, nodeID(3): {}}

	snap := h.refresh(t)
	h.clock.Advance(time.Minute)

	return h, map[int]int64{
		1: itemByNumber(t, snap.ReviewRequested, 1).ID,
		2: itemByNumber(t, snap.ReviewRequested, 2).ID,
		3: itemByNumber(t, snap.Assigned, 3).ID,
	}
}

func TestSnapshotSaysHowAnItemAsksTheUser(t *testing.T) {
	t.Parallel()

	h, _ := editHarness(t)

	snap, err := h.svc.Snapshot(t.Context())
	require.NoError(t, err)

	byName := itemByNumber(t, snap.ReviewRequested, 1)
	assert.Equal(t, ReviewRequestYou, byName.ReviewRequest, "asked by name, in any case, as well as through a team")
	assert.True(t, byName.Assigned)
	assert.Equal(t, []string{"backend"}, byName.ReviewTeams)

	team := itemByNumber(t, snap.ReviewRequested, 2)
	assert.Equal(t, ReviewRequestTeam, team.ReviewRequest)
	assert.False(t, team.Assigned)

	assigned := itemByNumber(t, snap.Assigned, 3)
	assert.Equal(t, ReviewRequestNone, assigned.ReviewRequest)
	assert.True(t, assigned.Assigned)
	assert.Equal(t, []string{}, assigned.ReviewTeams, "never nil")
}

func TestSnapshotCountsUnreadRequestsAsByName(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.inbox = fetched([]github.Item{pr(1, "MEMBER", false)}, nil, nil)

	snap := h.refresh(t)

	got := itemByNumber(t, snap.ReviewRequested, 1)
	assert.Nil(t, got.Details, "no details were read")
	assert.Equal(t, ReviewRequestYou, got.ReviewRequest)
}

func TestUnassignTakesTheItemOutOfTheAssignedList(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	calls := h.events.Count()
	h.client.edited = &github.Edited{UpdatedAt: h.clock.Now()}

	snap, err := h.svc.Unassign(t.Context(), ids[3])
	require.NoError(t, err)

	assert.Equal(t, []string{"unassign acme-org/example-repo#3 octo-user"}, h.client.edits)
	assert.Equal(t, 0, h.client.viewerReads, "the refresh's token is known to be the user's")
	assert.Equal(t, []int{1}, numbers(snap.Assigned))
	assert.Equal(t, [3]bool{false, false, false}, h.reasons(t, 3))
	assert.Greater(t, h.events.Count(), calls, "the window hears of it")

	snap, err = h.svc.Unassign(t.Context(), ids[1])
	require.NoError(t, err)

	assert.Empty(t, snap.Assigned.Items)
	assert.Equal(t, []int{2, 1}, numbers(snap.ReviewRequested), "an item in another list stays there")
	assert.False(t, itemByNumber(t, snap.ReviewRequested, 1).Assigned)
}

func TestRemoveReviewRequestTakesTheItemOutOfTheReviewList(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	h.client.edited = &github.Edited{
		UpdatedAt: h.clock.Now(), ReviewRequests: github.ReviewRequests{Users: []string{"someone"}, Teams: []string{}},
	}

	snap, err := h.svc.RemoveReviewRequest(t.Context(), ids[1])
	require.NoError(t, err)

	assert.Equal(t, []string{"unreview acme-org/example-repo#1 octo-user"}, h.client.edits)
	assert.Equal(t, []int{2}, numbers(snap.ReviewRequested))
	assert.Equal(t, []int{3, 1}, numbers(snap.Assigned), "the assignment stays")
	assert.Equal(t, ReviewRequestNone, itemByNumber(t, snap.Assigned, 1).ReviewRequest)
	assert.Equal(t, []string{`["someone"]`}, h.column(t, "SELECT review_users FROM item_details WHERE item_id = ?", ids[1]),
		"who it still asks is GitHub's answer")
}

func TestRemoveReviewRequestKeepsAnItemATeamIsStillAsked(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	h.client.edited = &github.Edited{
		UpdatedAt: h.clock.Now(), ReviewRequests: github.ReviewRequests{Users: []string{}, Teams: []string{"backend"}},
	}

	snap, err := h.svc.RemoveReviewRequest(t.Context(), ids[1])
	require.NoError(t, err)

	got := itemByNumber(t, snap.ReviewRequested, 1)
	assert.Equal(t, ReviewRequestTeam, got.ReviewRequest, "only the next refresh can tell whether the team is the user's")
	assert.Equal(t, []string{"backend"}, got.ReviewTeams)

	// The search still lists it at the next refresh, so the user is in the team.
	snap = h.refresh(t)
	assert.Equal(t, []int{2, 1}, numbers(snap.ReviewRequested))
}

func TestEditsRefuseItemsNotInTheirList(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)

	_, err := h.svc.Unassign(t.Context(), ids[2])
	require.ErrorIs(t, err, ErrNotAssigned)
	assert.Equal(t, "You are no longer assigned to acme-org/example-repo#2.", uierr.Message(err))

	_, err = h.svc.RemoveReviewRequest(t.Context(), ids[3])
	require.ErrorIs(t, err, ErrNotRequested, "an issue asks no review")

	_, err = h.svc.Unassign(t.Context(), 999)
	require.ErrorIs(t, err, item.ErrNotFound)

	assert.Empty(t, h.client.edits, "nothing was sent")
}

func TestEditsRefuseAnotherAccountsToken(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)

	// A service started since the refresh has not seen the token yet, and checks it.
	h.svc = h.service()
	h.client.viewer = "someone-else"

	_, err := h.svc.Unassign(t.Context(), ids[3])
	require.ErrorIs(t, err, ErrOtherAccount)
	assert.Equal(t, "The GitHub CLI is signed in as someone-else, but the inbox shows octo-user's items. "+
		"Refresh the inbox, then try again.", uierr.Message(err))
	assert.Empty(t, h.client.edits, "nothing was sent")
	assert.Equal(t, [3]bool{false, false, true}, h.reasons(t, 3))

	_, err = h.svc.Unassign(t.Context(), ids[3])
	require.ErrorIs(t, err, ErrOtherAccount)
	assert.Equal(t, 1, h.client.viewerReads, "a token checked is not checked again")

	// The user refreshes, as asked, and GitHub says whose the token is.
	h.refresh(t)

	_, err = h.svc.Unassign(t.Context(), ids[3])
	require.NoError(t, err)
	assert.Equal(t, 1, h.client.viewerReads)
	assert.Len(t, h.client.edits, 1)
}

func TestEditsCheckATokenTheInboxHasNotUsed(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	h.svc = h.service()

	_, err := h.svc.Unassign(t.Context(), ids[3])
	require.NoError(t, err)

	_, err = h.svc.Unassign(t.Context(), ids[1])
	require.NoError(t, err)

	assert.Equal(t, 1, h.client.viewerReads)
	assert.Len(t, h.client.edits, 2)
}

func TestEditFailuresChangeNothing(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	h.client.editErr = uierr.New("GitHub kept you assigned.", github.ErrEditIgnored)

	_, err := h.svc.Unassign(t.Context(), ids[3])
	require.ErrorIs(t, err, github.ErrEditIgnored)
	assert.Equal(t, "GitHub kept you assigned.", uierr.Message(err))
	assert.Equal(t, [3]bool{false, false, true}, h.reasons(t, 3))

	h.client.editErr = errFakeGate

	_, err = h.svc.RemoveReviewRequest(t.Context(), ids[1])
	require.ErrorIs(t, err, errFakeGate)
	assert.Equal(t, [3]bool{true, false, true}, h.reasons(t, 1))
}

func TestEditsRetryARefusedToken(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	h.client.editErr = &github.StatusError{StatusCode: http.StatusUnauthorized, Message: "Bad credentials"}

	_, err := h.svc.Unassign(t.Context(), ids[3])
	require.Error(t, err)
	assert.Len(t, h.client.edits, 2, "tried once more with a fresh token")
	assert.Equal(t, []string{fakeToken}, h.tokens.forgot)
}

func TestARefreshKeepsALeftListLeftUntilTheItemMoves(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	sink := &fakeSink{}
	h.svc = NewService(Config{
		Pool: h.pool, Tokens: h.tokens, Client: h.client, Clock: h.clock, Events: h.events, Arrivals: sink,
	})

	// GitHub answers the edits with the item's new update time, and its search lags.
	edited := byOther(pr(1, "MEMBER", false))
	edited.UpdatedAt = h.clock.Now()
	h.client.edited = &github.Edited{UpdatedAt: edited.UpdatedAt}

	_, err := h.svc.Unassign(t.Context(), ids[1])
	require.NoError(t, err)

	_, err = h.svc.RemoveReviewRequest(t.Context(), ids[1])
	require.NoError(t, err)

	second, third := byOther(pr(2, "MEMBER", false)), byOther(issue("example-repo", 3, "MEMBER"))
	h.client.inbox = fetched([]github.Item{edited, second}, nil, []github.Item{edited, third})
	h.clock.Advance(time.Minute)

	snap := h.refresh(t)

	assert.Equal(t, []int{2}, numbers(snap.ReviewRequested), "the search lags behind the edit")
	assert.Equal(t, []int{3}, numbers(snap.Assigned))
	assert.Empty(t, sink.calls, "nothing arrives from a lagging search")

	// Someone assigns the user again, which moves the item.
	edited.UpdatedAt = h.clock.Now()
	h.client.inbox = fetched([]github.Item{second}, nil, []github.Item{edited})
	h.clock.Advance(time.Minute)

	snap = h.refresh(t)

	assert.Equal(t, []int{1}, numbers(snap.Assigned), "an item that moved is believed")
	assert.Equal(t, []string{"assigned #1"}, kindsAndNumbers(sink.last()), "and arrives")
	assert.Equal(t, []string{"1 1"}, h.column(t,
		"SELECT (assign_left_at IS NULL) || ' ' || (review_left_at IS NULL) FROM items WHERE id = ?", ids[1]),
		"both stamps are cleared")
}

func TestAnEditGitHubMadeButTheInboxCouldNotRecordSaysSo(t *testing.T) {
	t.Parallel()

	h, ids := editHarness(t)
	h.exec(t, `CREATE TRIGGER refuse_left BEFORE UPDATE OF assign_left_at ON items
BEGIN SELECT RAISE(ABORT, 'disk full'); END`)

	_, err := h.svc.Unassign(t.Context(), ids[3])
	require.Error(t, err)
	assert.Equal(t, "GitHub made the change, but the inbox could not record it. The next refresh catches it up.",
		uierr.Message(err))
	assert.Len(t, h.client.edits, 1, "GitHub was asked once")
	assert.Equal(t, [3]bool{false, false, true}, h.reasons(t, 3), "nothing was stored")
}
