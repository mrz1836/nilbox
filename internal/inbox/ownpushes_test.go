package inbox

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
)

// errCommitsRead is a failed read of the commit authors.
var errCommitsRead = errors.New("the commit authors could not be read")

// commitsBy are a pull request's newest commits, oldest first, each "sha:login".
func commitsBy(pairs ...string) []github.CommitAuthor {
	out := make([]github.CommitAuthor, 0, len(pairs))

	for _, p := range pairs {
		oid, login, _ := strings.Cut(p, ":")
		out = append(out, github.CommitAuthor{OID: oid, Login: login})
	}

	return out
}

func TestOwnSince(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		commits []github.CommitAuthor
		covered bool
		ok      bool
	}{
		{name: "only the user's merge since", commits: commitsBy("v:octo-other", "h:octo-user"), covered: true, ok: true},
		{
			name: "the user's commits, any case", commits: commitsBy("v:octo-other", "m:Octo-User", "h:OCTO-USER"),
			covered: true, ok: true,
		},
		{name: "the author pushed", commits: commitsBy("v:octo-other", "h:octo-other"), ok: true},
		{
			name: "the author pushed, then the user on top", commits: commitsBy("v:octo-other", "a:octo-other", "h:octo-user"),
			ok: true,
		},
		{name: "an author matching no account", commits: commitsBy("v:octo-other", "h:"), ok: true},
		{name: "the verdict older than every commit read", commits: commitsBy("x:octo-user", "h:octo-user"), ok: true},
		{name: "pushed to between the two reads", commits: commitsBy("v:octo-other", "z:octo-user")},
		{name: "nothing read"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			covered, ok := ownSince(tt.commits, "v", "h", octoUser)
			assert.Equal(t, tt.ok, ok, "ok")
			assert.Equal(t, tt.covered, covered, "covered")
		})
	}

	covered, ok := ownSince(commitsBy("v:octo-other", "h:octo-user"), "v", "h", "")
	assert.True(t, ok)
	assert.False(t, covered, "with no viewer known, no commit is the user's")
}

func TestYourOwnPushIsNotPushedSinceYourReview(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.details = detailsOf(5)
	h.client.inbox = withReviewed(fetched(nil, nil, nil), pushed(5))
	h.client.commits = map[string][]github.CommitAuthor{
		nodeID(5): commitsBy(headReviewed+":octo-other", headNow+":"+octoUser),
	}

	snap := h.refresh(t)
	assert.Empty(t, snap.ReviewRequested.Items, "only the user pushed since: the author still owes the changes")
	assert.Equal(t, [3]bool{false, false, false}, h.reasons(t, 5))
	assert.Equal(t, [][]string{{nodeID(5)}}, h.client.commitIDs)

	// The same verdict and head are never asked about again.
	h.clock.Advance(time.Minute)
	h.refresh(t)
	assert.Len(t, h.client.commitIDs, 1, "an answer is kept for its verdict and head")

	// The author pushes: listed again, with the tag.
	again := pushed(5)
	again.HeadOID = strings.Repeat("4", 40)
	again.UpdatedAt = h.clock.Now()
	h.client.inbox = withReviewed(fetched(nil, nil, nil), again)
	h.client.commits[nodeID(5)] = commitsBy(headReviewed+":octo-other", headNow+":"+octoUser, again.HeadOID+":octo-other")
	d := atHead()
	d.HeadOID = again.HeadOID
	h.client.details = map[string]github.ItemDetails{nodeID(5): d}
	h.clock.Advance(time.Minute)

	snap = h.refresh(t)
	require.Equal(t, []int{5}, numbers(snap.ReviewRequested))
	assert.True(t, snap.ReviewRequested.Items[0].Details.PushedSinceReview)
	assert.Len(t, h.client.commitIDs, 2, "a new head is asked about")
}

func TestYourOwnPushOnListedPullRequestTagsNothing(t *testing.T) {
	t.Parallel()

	// Assigned to the user too, it stays in the inbox, without the tag.
	h := newHarness(t)
	h.client.details = detailsOf(5)
	h.client.inbox = withReviewed(fetched(nil, nil, []github.Item{byOther(pr(5, "MEMBER", false))}), pushed(5))
	h.client.commits = map[string][]github.CommitAuthor{
		nodeID(5): commitsBy(headReviewed+":octo-other", headNow+":"+octoUser),
	}

	snap := h.refresh(t)
	require.Equal(t, []int{5}, numbers(snap.Yours))
	assert.False(t, snap.Yours.Items[0].Details.PushedSinceReview)
	assert.Empty(t, snap.ReviewRequested.Items)
}

func TestAPushStaysTheAuthorsWhenItCantBeTold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		commits []github.CommitAuthor
		err     error
		asks    int
	}{
		{name: "the read fails, and is asked again", err: errCommitsRead, asks: 2},
		{name: "pushed to between the reads, asked again", commits: commitsBy(headReviewed + ":octo-other"), asks: 2},
		{name: "the author pushed on top of the user", commits: commitsBy(
			headReviewed+":octo-other", strings.Repeat("6", 40)+":octo-other", headNow+":"+octoUser,
		), asks: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			h.client.details = detailsOf(5)
			h.client.inbox = withReviewed(fetched(nil, nil, nil), pushed(5))
			h.client.commits = map[string][]github.CommitAuthor{nodeID(5): tt.commits}
			h.client.commitsErr = tt.err

			snap := h.refresh(t)
			require.Equal(t, []int{5}, numbers(snap.ReviewRequested), "listed as pushed to, as before")
			assert.True(t, snap.ReviewRequested.Items[0].Details.PushedSinceReview)

			h.clock.Advance(time.Minute)
			h.refresh(t)
			assert.Len(t, h.client.commitIDs, tt.asks)
		})
	}
}

func TestOnlyAPullRequestPushedPastTheVerdictIsAskedAbout(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.client.details = detailsOf(5, 6, 7)
	h.client.inbox = withReviewed(fetched(nil, nil, nil),
		pushed(5),
		reviewed(6, github.ReviewStateChangesRequested, headNow),
		reviewed(7, github.ReviewStateApproved, headReviewed),
	)

	h.refresh(t)
	assert.Equal(t, [][]string{{nodeID(5)}}, h.client.commitIDs,
		"not one at its head, nor one approved: no read for most refreshes")
}
