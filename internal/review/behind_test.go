package review

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/github"
	"github.com/mrz1836/nilbox/internal/store"
)

func TestTheSheetSaysHowFarBehindItsBaseItIs(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	h.github.review.BehindBy = 12
	h.github.review.ViewerCanUpdateBranch = true

	r := h.refreshed(t)
	assert.True(t, r.Behind)
	assert.Equal(t, 12, r.BehindBy)
	assert.Equal(t, []string{"refs/pull/7/head"}, h.github.compared,
		"the read compares the base with the pull request's head ref, which a fork's has too")

	update := offerOf(t, r, ActionUpdateBranch)
	assert.True(t, update.Enabled)
	assert.NotEqual(t, ActionUpdateBranch, r.Primary, "being behind holds nothing back here: the button merges")

	// Opened again, from the cache, the sheet says the same.
	opened, err := h.svc.Open(t.Context(), pullID)
	require.NoError(t, err)
	assert.True(t, opened.Behind)
	assert.Equal(t, 12, opened.BehindBy)
}

func TestWhenTheSheetCountsItBehind(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		setUp    func(*github.PullReview)
		behind   bool
		behindBy int
	}{
		{name: "up to date", setUp: func(*github.PullReview) {}},
		{
			name:   "GitHub lets the user update it, with no count",
			setUp:  func(r *github.PullReview) { r.ViewerCanUpdateBranch = true },
			behind: true,
		},
		{
			name:   "the rules hold it back for being behind",
			setUp:  func(r *github.PullReview) { r.MergeState = github.MergeStateBehind },
			behind: true,
		},
		{
			name:   "the compare counted commits it lacks",
			setUp:  func(r *github.PullReview) { r.BehindBy = 2 },
			behind: true, behindBy: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := newHarness(t)
			tt.setUp(h.github.review)

			r := h.refreshed(t)
			assert.Equal(t, tt.behind, r.Behind)
			assert.Equal(t, tt.behindBy, r.BehindBy)
		})
	}
}

func TestASheetNeverReadIsNotBehind(t *testing.T) {
	t.Parallel()

	r := build(storedPull(), nil, nil, Prefs{Method: PrefRepo})
	assert.False(t, r.Behind)
	assert.Zero(t, r.BehindBy)
}

func TestAClosedPullRequestIsNotBehind(t *testing.T) {
	t.Parallel()

	c := &cached{head: headA, context: pullContext{
		Head: headA, State: github.StateClosed, BehindBy: 4, CanUpdateBranch: true, MergeState: github.MergeStateBehind,
	}}

	r := build(storedPull(), c, nil, Prefs{Method: PrefRepo})
	assert.False(t, r.Behind, "only an open pull request is behind")
	assert.Equal(t, 4, r.BehindBy)
}

func TestAContextAnOlderBuildCachedReadsAsNotCounted(t *testing.T) {
	t.Parallel()

	pc, err := store.DecodeJSON[pullContext](`{"head":"` + headA + `","state":"OPEN","mergeState":"CLEAN"}`)
	require.NoError(t, err)
	assert.Zero(t, pc.BehindBy)
}
