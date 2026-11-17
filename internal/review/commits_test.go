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
)

// sha is a commit SHA of one repeated character.
func sha(c string) string {
	return strings.Repeat(c, 40)
}

// commitsAt are cached commits with SHAs of the characters in order, oldest first.
func commitsAt(chars ...string) []contextCommit {
	out := make([]contextCommit, 0, len(chars))
	for _, c := range chars {
		out = append(out, contextCommit{OID: sha(c), Headline: "Commit " + c})
	}

	return out
}

// reviewedAt is the user's standing request for changes, left on the commit of c at noon.
func reviewedAt(c string) *contextReview {
	return &contextReview{
		State: github.ReviewStateChangesRequested, Commit: sha(c), URL: "https://github.com/r",
		SubmittedAt: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC),
	}
}

func TestYourReviewGoesByWhereTheReviewedCommitIs(t *testing.T) {
	t.Parallel()

	forcePushAt := func(at string) []timeline.Entry {
		return []timeline.Entry{{Kind: timeline.KindForcePushed, At: at}}
	}

	tests := []struct {
		name    string
		op      *contextReview
		commits []contextCommit
		total   int
		entries []timeline.Entry
		since   Since
		count   int
	}{
		{name: "at the head", op: reviewedAt("c"), commits: commitsAt("a", "b", "c"), total: 3},
		{name: "one since", op: reviewedAt("b"), commits: commitsAt("a", "b", "c"), total: 3, since: SincePushed, count: 1},
		{
			name: "three since", op: reviewedAt("a"), commits: commitsAt("a", "b", "c", "d"), total: 4,
			since: SincePushed, count: 3,
		},
		{
			name: "the SHA in another case", op: &contextReview{State: github.ReviewStateApproved, Commit: strings.ToUpper(sha("b"))},
			commits: commitsAt("a", "b", "c"), total: 3, since: SincePushed, count: 1,
		},
		{name: "rewritten", op: reviewedAt("x"), commits: commitsAt("a", "b", "c"), total: 3, since: SinceRewritten},
		{
			name: "beyond a list cut short", op: reviewedAt("x"), commits: commitsAt("a", "b", "c"), total: 150,
			since: SinceBeyond,
		},
		{
			name: "a list cut short, with a force push after the review", op: reviewedAt("x"), commits: commitsAt("a", "b"),
			total: 150, entries: forcePushAt("2026-11-09T13:00:00Z"), since: SinceRewritten,
		},
		{
			name: "a list cut short, with a force push before the review", op: reviewedAt("x"), commits: commitsAt("a", "b"),
			total: 150, entries: forcePushAt("2026-11-09T11:00:00Z"), since: SinceBeyond,
		},
		{name: "no commits read, as an older build's cache", op: reviewedAt("a"), total: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := yourReview(tt.op, tt.commits, tt.total, tt.entries)
			require.NotNil(t, got)
			assert.Equal(t, tt.since, got.Since)
			assert.Equal(t, tt.count, got.Count)
			assert.Equal(t, tt.op.Commit, got.Commit)
			assert.Equal(t, timeline.ReviewState(tt.op.State), got.State)
		})
	}

	assert.Nil(t, yourReview(nil, commitsAt("a"), 1, nil), "no standing review, as one that only commented")
	assert.Nil(t, yourReview(&contextReview{State: github.ReviewStateApproved}, commitsAt("a"), 1, nil),
		"a review on no known commit tells nothing")

	got := yourReview(reviewedAt("a"), commitsAt("a"), 1, nil)
	assert.Equal(t, "2026-11-09T12:00:00Z", got.At)
	assert.Equal(t, "https://github.com/r", got.URL)
}

func TestCommitsOfNameEachAuthorAndLink(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	got := commitsOf([]contextCommit{
		{OID: sha("a"), Headline: "First", Author: "octo-user", AuthorName: "Octo User", CommittedAt: at},
		{OID: sha("b"), Headline: "Second", AuthorName: "dependabot[bot]"},
	}, "https://github.com/acme-org/example-repo/pull/7")

	assert.Equal(t, []Commit{
		{
			OID: sha("a"), Short: "aaaaaaa", Headline: "First", Author: "octo-user", CommittedAt: "2026-11-09T12:00:00Z",
			URL: "https://github.com/acme-org/example-repo/pull/7/commits/" + sha("a"),
		},
		{
			OID: sha("b"), Short: "bbbbbbb", Headline: "Second", Author: "dependabot[bot]",
			URL: "https://github.com/acme-org/example-repo/pull/7/commits/" + sha("b"),
		},
	}, got, "the login GitHub matched, else the commit's own name")
	assert.Equal(t, []Commit{}, commitsOf(nil, ""))
}

func TestTheCacheKeepsTheCommitsAndTheVerdict(t *testing.T) {
	t.Parallel()

	r := pullReview(headA, github.MergeStateClean)
	r.CommitTotal = 120
	r.Verdict = &github.Verdict{
		State: github.ReviewStateChangesRequested, Commit: headA, URL: "https://github.com/v",
		SubmittedAt: time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC),
	}

	for i := range github.PullCommitsMax {
		r.Commits = append(r.Commits, github.PullCommit{
			OID: strings.Repeat(string(rune('a'+i%26)), 39) + string(rune('a'+i/26)), Headline: strings.Repeat("h", 300),
			Author: "octo-user",
		})
	}

	pc := contextOf(r, octoUser)
	require.Len(t, pc.Commits, github.PullCommitsMax)
	assert.Equal(t, 120, pc.CommitTotal)
	assert.Len(t, []rune(pc.Commits[0].Headline), headlineRunes, "a long headline is cut")
	assert.True(t, strings.HasSuffix(pc.Commits[0].Headline, "…"))
	require.NotNil(t, pc.Opinion)
	assert.Equal(t, headA, pc.Opinion.Commit)

	// A context large enough to cut keeps every commit.
	pc.Body = strings.Repeat("b", 2<<20)

	encoded, err := fitContext(pc)
	require.NoError(t, err)

	back, err := store.DecodeJSON[pullContext](encoded)
	require.NoError(t, err)
	assert.True(t, back.BodyTrimmed)
	assert.Len(t, back.Commits, github.PullCommitsMax, "fitContext never cuts the commits")
	assert.Equal(t, pc.Opinion, back.Opinion)

	// With no verdict, none is kept.
	r.Verdict = nil
	assert.Nil(t, contextOf(r, octoUser).Opinion)
}

func TestTheSheetShowsTheCommitsSinceYourReview(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.review.CommitTotal = 3
	h.github.review.Commits = []github.PullCommit{
		{OID: headB, Headline: "Add the cache", Author: octoOther},
		{OID: sha("c"), Headline: "Address the review", Author: octoOther},
		{OID: headA, Headline: "Fix the tests", Author: octoOther},
	}
	h.github.review.Verdict = &github.Verdict{State: github.ReviewStateChangesRequested, Commit: headB}

	r := h.refreshed(t)
	require.Len(t, r.CommitList, 3)
	assert.Equal(t, 3, r.Commits, "the tab counts every commit")
	assert.Zero(t, r.EarlierCommits)
	require.NotNil(t, r.YourReview)
	assert.Equal(t, timeline.ReviewChangesRequested, r.YourReview.State)
	assert.Equal(t, SincePushed, r.YourReview.Since)
	assert.Equal(t, 2, r.YourReview.Count)

	// Opened again, from the cache, the sheet says the same.
	opened, err := h.svc.Open(t.Context(), pullID)
	require.NoError(t, err)
	assert.Equal(t, r.YourReview, opened.YourReview)
	assert.Equal(t, r.CommitList, opened.CommitList)
}

func TestASheetNeverReadHasNoCommits(t *testing.T) {
	t.Parallel()

	r := build(storedPull(), nil, nil, PrefRepo)
	assert.Equal(t, []Commit{}, r.CommitList, "never null")
	assert.Nil(t, r.YourReview)
	assert.Equal(t, 1, r.Commits, "the card's count")
}
