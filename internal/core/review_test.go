package core

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/review"
	"github.com/mrz1836/nilbox/internal/settings"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestTheReviewSheetReadsAnEarlierPageThroughTheInboxStoringNothing(t *testing.T) {
	t.Parallel()

	r := newGHRig(t)

	page, err := r.steps.review.Earlier(t.Context(), r.pull, "CURSOR_1")
	require.NoError(t, err)
	assert.Equal(t, 3, page.Total)
	require.Len(t, page.Items, 1)
	assert.Equal(t, "An early comment.", page.Items[0].Body)
	assert.Equal(t, []string{"timeline " + ghNode(ghPullNumber) + " before CURSOR_1"}, r.pulls.asked(),
		"one read of the page, for the pull request the inbox stored, before the cursor asked")

	_, err = r.steps.review.Earlier(t.Context(), r.issue, "CURSOR_1")
	require.ErrorIs(t, err, review.ErrNotPull, "an issue has no review sheet")

	_, err = r.steps.review.Earlier(t.Context(), 999, "CURSOR_1")
	require.ErrorIs(t, err, item.ErrNotFound, "nor an item the inbox doesn't hold")
	assert.Len(t, r.pulls.asked(), 1, "neither asks GitHub")
}

func TestTheReviewSheetReadsTheMergeMethodAsTheSettingsStoreIt(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	db := testutil.DB(t)
	prefs := settings.NewService(db, profiles.NewStore(db, nil), t.TempDir())
	sheet := reviewSettings{settings: prefs}

	got, err := sheet.MergeMethod(ctx)
	require.NoError(t, err)
	assert.Equal(t, review.PrefRepo, got, "as each repository offers first, until the user picks one")

	_, err = prefs.SetMergeMethod(ctx, settings.MergeMethodRebase)
	require.NoError(t, err)

	got, err = sheet.MergeMethod(ctx)
	require.NoError(t, err)
	assert.Equal(t, review.PrefRebase, got)

	closed := testutil.ClosedDB(t)
	broken := reviewSettings{settings: settings.NewService(closed, profiles.NewStore(closed, nil), t.TempDir())}

	_, err = broken.MergeMethod(ctx)
	require.Error(t, err, "settings that can't be read fail the sheet's read")

	_, err = broken.Sensitive(ctx)
	require.Error(t, err)

	_, err = broken.IgnoredWorkflows(ctx)
	require.Error(t, err)
}

func TestTheReviewSheetsRepliesFallBackToThePersonsDefaultForABot(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	library := prompts.NewStore(testutil.DB(t), testutil.NewClock(ghNow()))
	require.NoError(t, prompts.Seed(ctx, library))

	replies := reviewReplies{replyFiller{store: library}}

	require.NoError(t, prompts.NewService(prompts.Config{Store: library}).SetReplyDefault(ctx, prompts.SlotApproveBots, 0))

	approve, err := replies.Offered(ctx, review.ReplyApprove)
	require.NoError(t, err)
	require.NotZero(t, approve.Default)
	assert.Equal(t, approve.Default, approve.DefaultBots, "with none chosen for a bot's, the person's default stands")

	changes, err := replies.Offered(ctx, review.ReplyRequestChanges)
	require.NoError(t, err)

	nicknames := make([]string, 0, len(changes.Replies))
	for _, p := range changes.Replies {
		assert.Equal(t, review.ReplyRequestChanges, p.Action)

		nicknames = append(nicknames, p.Nickname)
	}

	assert.Contains(t, nicknames, "Needs changes", "a request for changes offers only its own replies")
	assert.NotContains(t, nicknames, "Question")
	assert.Equal(t, changes.Default, changes.DefaultBots, "one default for a person's and a bot's alike")

	_, err = reviewReplies{replyFiller{store: prompts.NewStore(testutil.ClosedDB(t), nil)}}.Offered(ctx, review.ReplyApprove)
	require.Error(t, err, "a library that can't be read offers nothing")
}
