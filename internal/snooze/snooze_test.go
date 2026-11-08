package snooze

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// before is a pull request's activity when it was snoozed.
func before() State {
	return State{
		Head: "1111111111111111111111111111111111111111", Commits: 3, Comments: 4,
		Decision: "REVIEW_REQUIRED", Reviewed: 1,
	}
}

// all wakes on any push, any review, and any comment.
func all() Rules {
	return Rules{Commits: 1, Reviews: ReviewsAny, Comments: 1}
}

func TestDefaults(t *testing.T) {
	assert.Equal(t, Rules{Commits: 1, Reviews: ReviewsDecisions, Comments: 0}, DefaultRules())
	require.NoError(t, DefaultRules().Validate())
	assert.Equal(t, 3, DecodeDays(nil))
	assert.Equal(t, DefaultRules(), DecodeRules(nil))
}

func TestDecodeRoundTrips(t *testing.T) {
	rules := Rules{Commits: 5, Reviews: ReviewsNever, Comments: 10}

	assert.Equal(t, rules, DecodeRules(rules.Encode()))
	assert.Equal(t, 14, DecodeDays(map[string]string{PrefDays: "14"}))
}

func TestDecodeFallsBackPerRule(t *testing.T) {
	got := DecodeRules(map[string]string{
		PrefWakeCommits:  "-1",
		PrefWakeReviews:  "sometimes",
		PrefWakeComments: "3",
	})
	assert.Equal(t, Rules{Commits: 1, Reviews: ReviewsDecisions, Comments: 3}, got)

	got = DecodeRules(map[string]string{PrefWakeCommits: "100", PrefWakeComments: "lots"})
	assert.Equal(t, DefaultRules(), got)

	for _, bad := range []string{"0", "366", "soon", ""} {
		assert.Equal(t, DefaultDays, DecodeDays(map[string]string{PrefDays: bad}), bad)
	}
}

func TestValidate(t *testing.T) {
	for _, bad := range []Rules{
		{Commits: -1, Reviews: ReviewsAny},
		{Commits: MaxThreshold + 1, Reviews: ReviewsAny},
		{Comments: -1, Reviews: ReviewsAny},
		{Comments: MaxThreshold + 1, Reviews: ReviewsAny},
		{Reviews: ""},
		{Reviews: "ANY"},
	} {
		require.ErrorIs(t, bad.Validate(), ErrInvalidRules, bad)
	}

	require.NoError(t, Rules{Reviews: ReviewsNever}.Validate(), "every rule off is allowed")
	require.ErrorContains(t, Rules{Commits: 100, Reviews: ReviewsAny}.Validate(), "100 new commits; choose 0 to 99")

	assert.True(t, ValidDays(MinDays))
	assert.True(t, ValidDays(MaxDays))
	assert.False(t, ValidDays(0))
	assert.False(t, ValidDays(MaxDays+1))
}

func TestNothingNewKeepsTheSnooze(t *testing.T) {
	got := Wake(all(), before(), before(), false)

	assert.False(t, got.Woke())
	assert.Equal(t, Reasons{}, got)
}

func TestNewCommits(t *testing.T) {
	now := before()
	now.Commits = 5
	now.Head = "2222222222222222222222222222222222222222"

	assert.Equal(t, Reasons{Commits: 2}, Wake(Rules{Commits: 1, Reviews: ReviewsNever}, before(), now, false))
	assert.Equal(t, Reasons{Commits: 2}, Wake(Rules{Commits: 2, Reviews: ReviewsNever}, before(), now, false))
	assert.False(t, Wake(Rules{Commits: 3, Reviews: ReviewsNever}, before(), now, false).Woke(),
		"two new commits are under a threshold of three")
	assert.False(t, Wake(Rules{Reviews: ReviewsNever}, before(), now, false).Woke(), "commits never wake")
}

func TestForcePush(t *testing.T) {
	now := before()
	now.Head = "2222222222222222222222222222222222222222"

	got := Wake(Rules{Commits: 1, Reviews: ReviewsNever}, before(), now, false)
	assert.Equal(t, Reasons{ForcePushed: true}, got)
	assert.True(t, got.Woke())

	assert.False(t, Wake(Rules{Commits: 1, Reviews: ReviewsNever}, before(), now, true).Woke(),
		"a bot's force-push is a rebase")
	assert.False(t, Wake(Rules{Commits: 3, Reviews: ReviewsNever}, before(), now, false).Woke(),
		"only any push counts a force-push")

	unknown := before()
	unknown.Head = ""
	assert.False(t, Wake(Rules{Commits: 1, Reviews: ReviewsNever}, unknown, now, false).Woke(),
		"a head not known when snoozed is not a push")

	// A removed commit (a force-push that dropped one) is still a push.
	now.Commits = 2
	assert.Equal(t, Reasons{ForcePushed: true}, Wake(Rules{Commits: 1, Reviews: ReviewsNever}, before(), now, false))
}

func TestReviews(t *testing.T) {
	approved := before()
	approved.Approvals = 1
	approved.Reviewed = 2

	changes := before()
	changes.ChangesRequested = 1
	changes.Reviewed = 2

	commented := before()
	commented.Reviewed = 2

	decided := before()
	decided.Decision = "APPROVED"

	blocked := before()
	blocked.Decision = "CHANGES_REQUESTED"

	tests := []struct {
		name string
		rule ReviewRule
		now  State
		want ReviewOutcome
	}{
		{"approval, decisions", ReviewsDecisions, approved, ReviewApproved},
		{"changes, decisions", ReviewsDecisions, changes, ReviewChangesRequested},
		{"comment-only review, decisions", ReviewsDecisions, commented, ""},
		{"comment-only review, any", ReviewsAny, commented, ReviewNew},
		{"decision turns approved", ReviewsDecisions, decided, ReviewApproved},
		{"decision turns to changes", ReviewsDecisions, blocked, ReviewChangesRequested},
		{"approval, never", ReviewsNever, approved, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Wake(Rules{Reviews: tt.rule}, before(), tt.now, false)
			assert.Equal(t, Reasons{Review: tt.want}, got)
		})
	}

	// Changes requested outranks an approval in the same refresh.
	both := before()
	both.Approvals = 1
	both.ChangesRequested = 1
	assert.Equal(t, ReviewChangesRequested, Wake(Rules{Reviews: ReviewsAny}, before(), both, false).Review)

	// A decision that was already there is not new.
	was := before()
	was.Decision = "APPROVED"
	assert.Empty(t, Wake(Rules{Reviews: ReviewsAny}, was, was, false).Review)
}

func TestReviewOutcomesAreValid(t *testing.T) {
	for _, o := range []ReviewOutcome{ReviewApproved, ReviewChangesRequested, ReviewNew, ReviewNone} {
		assert.True(t, o.Valid(), o)
	}

	assert.False(t, ReviewOutcome("praised").Valid())
}

func TestComments(t *testing.T) {
	now := before()
	now.Comments = 7

	assert.Equal(t, Reasons{Comments: 3}, Wake(Rules{Reviews: ReviewsNever, Comments: 1}, before(), now, false))
	assert.Equal(t, Reasons{Comments: 3}, Wake(Rules{Reviews: ReviewsNever, Comments: 3}, before(), now, false))
	assert.False(t, Wake(Rules{Reviews: ReviewsNever, Comments: 5}, before(), now, false).Woke())
	assert.False(t, Wake(Rules{Reviews: ReviewsNever}, before(), now, false).Woke(), "comments never wake")

	fewer := before()
	fewer.Comments = 1
	assert.False(t, Wake(all(), before(), fewer, false).Woke(), "deleted comments are not new")
}

func TestEveryReasonAtOnce(t *testing.T) {
	now := before()
	now.Commits = 4
	now.Head = "2222222222222222222222222222222222222222"
	now.Approvals = 1
	now.Comments = 6

	assert.Equal(t, Reasons{Commits: 1, Review: ReviewApproved, Comments: 2}, Wake(all(), before(), now, false))
}
