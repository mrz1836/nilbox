package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/issues"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/review"
)

func TestEachSheetFillsInTheRepliesForItsKind(t *testing.T) {
	t.Parallel()

	c, ids := seedBound(t)
	ctx := t.Context()

	looking, err := c.Prompts.Create(ctx, prompts.Input{
		Name: "Looking into it", Nickname: "Looking", Purpose: prompts.PurposeReply, ReplyAction: prompts.ReplyComment,
		AppliesTo: prompts.AppliesToIssue, Body: "Looking into #{{.Number}}, @{{.Author}}.",
	})
	require.NoError(t, err)

	both, err := c.Prompts.Create(ctx, prompts.Input{
		Name: "Thanks", Nickname: "Thanks", Purpose: prompts.PurposeReply, ReplyAction: prompts.ReplyComment,
		AppliesTo: prompts.AppliesToAny, Body: "Thanks!",
	})
	require.NoError(t, err)

	offered, err := c.Issues.Replies(ctx)
	require.NoError(t, err)
	assert.Equal(t, []issues.Reply{
		{ID: looking.ID, Name: "Looking into it", Nickname: "Looking"},
		{ID: both.ID, Name: "Thanks", Nickname: "Thanks"},
	}, offered, "the comment replies for issues, the starters' being for pull requests")

	comments, err := c.Review.Replies(ctx, review.ReplyComment)
	require.NoError(t, err)

	nicknames := make([]string, 0, len(comments.Replies))
	for _, r := range comments.Replies {
		nicknames = append(nicknames, r.Nickname)
	}

	assert.Contains(t, nicknames, "Thanks")
	assert.NotContains(t, nicknames, "Looking", "the review sheet offers none for issues alone")

	text, err := c.Issues.FillReply(ctx, ids.stuck, looking.ID)
	require.NoError(t, err)
	assert.Regexp(t, `^Looking into #\d+, @\S+\.$`, text)

	_, err = c.Review.FillReply(ctx, ids.item, looking.ID)
	require.ErrorIs(t, err, prompts.ErrWrongKind, "a pull request gets no reply for issues alone")

	question := firstReply(t, c, "Question")

	_, err = c.Issues.FillReply(ctx, ids.stuck, question.ID)
	require.ErrorIs(t, err, prompts.ErrWrongKind, "an issue gets no reply for pull requests")

	agentPrompt := firstPrompt(ctx, t, c)

	_, err = c.Issues.FillReply(ctx, ids.stuck, agentPrompt.ID)
	require.ErrorIs(t, err, prompts.ErrNotReply)

	_, err = c.Issues.FillReply(ctx, ids.item, looking.ID)
	require.ErrorIs(t, err, issues.ErrNotIssue)
}

// firstReply returns the stored reply with nickname.
func firstReply(t *testing.T, c *Core, nickname string) prompts.Prompt {
	t.Helper()

	all, err := c.Prompts.List(t.Context())
	require.NoError(t, err)

	for _, p := range all {
		if p.Nickname == nickname {
			return p
		}
	}

	require.Failf(t, "no such reply", "%q", nickname)

	return prompts.Prompt{}
}
