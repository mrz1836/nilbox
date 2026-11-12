package prompts

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// replyInput is a reply for action with body.
func replyInput(nickname string, action ReplyAction, body string) Input {
	return Input{Name: nickname + " reply", Nickname: nickname, Purpose: PurposeReply, ReplyAction: action, Body: body}
}

func TestParseReplyAllowsWhatARepliesNeeds(t *testing.T) {
	t.Parallel()

	tmpl, diags := ParseReply("- looks good\n- {{if .IsBot}}thanks, bot{{end}}")
	require.NotNil(t, tmpl, "a reply may start with a list")
	assert.Empty(t, diags)

	out, err := tmpl.Execute(Sample(item.KindPR))
	require.NoError(t, err)
	assert.Equal(t, "- looks good\n-\n", out)

	empty, diags := ParseReply("{{if .IsBot}}Thanks, bot.{{end}}")
	require.NotNil(t, empty, "a reply may be empty once filled in")
	assert.Empty(t, diags)

	out, err = empty.Execute(Sample(item.KindPR))
	require.NoError(t, err)
	assert.Empty(t, out)

	outside := Sample(item.KindPR)
	outside.OutsideContributor = true

	out, err = empty.Render(outside)
	require.NoError(t, err)
	assert.Empty(t, out, "a reply gets no notice: it goes to GitHub, not an agent")
}

func TestParseReplyWarnsAboutSessionVariables(t *testing.T) {
	t.Parallel()

	tmpl, diags := ParseReply("Approved from {{.Worktree}} with {{.Profile}} by {{.Me}}")
	require.NotNil(t, tmpl)
	require.Len(t, diags, 2)

	for _, d := range diags {
		assert.Equal(t, SeverityWarning, d.Severity)
		assert.Contains(t, d.Message, "means nothing in a reply")
	}

	_, diags = ParseReply("  \n")
	require.Len(t, diags, 1)
	assert.Equal(t, "Write the reply's text.", diags[0].Message)

	_, diags = Parse("- {{.URL}}", AppliesToPR)
	require.NotEmpty(t, diags, "an agent prompt still may not start with a dash")
}

func TestCreateAReply(t *testing.T) {
	t.Parallel()

	s, _ := empty(t, nil)
	ctx := t.Context()

	in := replyInput("Ship it", ReplyApprove, "Ship it, @{{.Author}}.")
	in.AppliesTo, in.Effort, in.Models = AppliesToIssue, "high", map[agent.ID]string{agent.Claude: "opus"}

	p, err := s.Create(ctx, in)
	require.NoError(t, err)
	assert.True(t, p.IsReply())
	assert.Equal(t, ReplyApprove, p.ReplyAction)
	assert.Equal(t, AppliesToPR, p.AppliesTo, "a reply is for pull requests")
	assert.Empty(t, p.Effort, "a reply has no launch settings")
	assert.Empty(t, p.Models)

	tmpl, err := p.Template()
	require.NoError(t, err)

	out, err := tmpl.Execute(Sample(item.KindPR))
	require.NoError(t, err)
	assert.Equal(t, "Ship it, @octo-user.\n", out)

	_, err = s.Create(ctx, replyInput("No action", "", "x"))
	require.ErrorIs(t, err, ErrInvalidPrompt)

	_, err = s.Create(ctx, replyInput("Ship it", ReplyComment, "x"))
	require.ErrorIs(t, err, ErrInvalidPrompt, "agent prompts and replies share nicknames")

	agentPrompt, err := s.Create(ctx, Input{Name: "Look", Nickname: "Look", AppliesTo: AppliesToPR, Body: "Look at {{.URL}}", ReplyAction: ReplyApprove})
	require.NoError(t, err)
	assert.Equal(t, PurposeAgent, agentPrompt.Purpose)
	assert.Empty(t, agentPrompt.ReplyAction, "an agent prompt has no review")
}

func TestUpdateKeepsAPromptsPurpose(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()

	lgtm := byNickname(t, s, "LGTM")

	in := replyInput("LGTM", ReplyApprove, "Approved.")
	in.Purpose = PurposeAgent

	got, err := s.Update(ctx, lgtm.ID, in)
	require.NoError(t, err)
	assert.True(t, got.IsReply(), "a reply stays a reply")
	assert.True(t, got.Modified)

	_, err = s.Update(ctx, lgtm.ID, replyInput("LGTM", ReplyComment, "Approved."))
	require.ErrorIs(t, err, ErrInvalidPrompt, "the default for approvals must stay an approval reply")

	reset, err := s.Reset(ctx, lgtm.ID)
	require.NoError(t, err)
	assert.False(t, reset.Modified)
	assert.Equal(t, replyLGTMBody, reset.Body)
}

func TestSetReplyDefault(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()

	question, lgtm, review := byNickname(t, s, "Question"), byNickname(t, s, "LGTM"), byNickname(t, s, "Review")

	require.ErrorIs(t, s.SetReplyDefault(ctx, SlotApprove, question.ID), ErrNotReply, "a comment can't be an approval")
	require.ErrorIs(t, s.SetReplyDefault(ctx, SlotComment, review.ID), ErrNotReply, "an agent prompt is no reply")
	require.ErrorIs(t, s.SetReplyDefault(ctx, "later", lgtm.ID), ErrNotReply)
	require.ErrorIs(t, s.SetReplyDefault(ctx, SlotComment, 9999), ErrPromptNotFound)

	require.NoError(t, s.SetReplyDefault(ctx, SlotApproveBots, lgtm.ID))
	require.NoError(t, s.SetReplyDefault(ctx, SlotComment, 0))

	defaults, err := s.Defaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, lgtm.ID, defaults.Replies.ApproveBots)
	assert.Zero(t, defaults.Replies.Comment, "a slot can be left with no reply")
	assert.Equal(t, []ReplySlot{SlotApprove, SlotApproveBots}, byNickname(t, s, "LGTM").Usage.Replies)

	require.ErrorIs(t, s.SetDefault(ctx, item.KindPR, lgtm.ID), ErrNotAgentPrompt, "a reply is never a launch's default")
}

func TestDeletingADefaultReplyLeavesItsReviewsWithNone(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()

	lgtm := byNickname(t, s, "LGTM")
	require.NoError(t, s.SetReplyDefault(ctx, SlotApproveBots, lgtm.ID))
	require.NoError(t, s.Delete(ctx, lgtm.ID), "a default reply can be deleted")

	defaults, err := s.Defaults(ctx)
	require.NoError(t, err)
	assert.Zero(t, defaults.Replies.Approve)
	assert.Zero(t, defaults.Replies.ApproveBots)
	assert.NotZero(t, defaults.Replies.Comment)
}

func TestReplyDefaultsSkipWhatNoLongerFits(t *testing.T) {
	t.Parallel()

	s, db := seeded(t, nil)
	ctx := t.Context()

	review := byNickname(t, s, "Review")
	testutil.Exec(t, db, "UPDATE prefs SET value = ? WHERE key = 'prompts.default.reply.comment'", review.ID)
	testutil.Exec(t, db, "UPDATE prefs SET value = '9999' WHERE key = 'prompts.default.reply.approve'")
	testutil.Exec(t, db, "UPDATE prefs SET value = 'x' WHERE key = 'prompts.default.reply.request_changes'")

	defaults, err := s.Defaults(ctx)
	require.NoError(t, err)
	assert.Zero(t, defaults.Replies.Comment, "an agent prompt is no reply")
	assert.Zero(t, defaults.Replies.Approve, "a reply that is gone")
	assert.Zero(t, defaults.Replies.RequestChanges, "a value that is not an ID")
	assert.NotZero(t, defaults.Replies.ApproveBots)
	assert.Equal(t, defaults.Replies.ApproveBots, defaults.Replies.For(SlotApproveBots))
	assert.Zero(t, defaults.Replies.For("later"))

	testutil.Exec(t, db, "UPDATE prefs SET value = ? WHERE key = 'prompts.default.pr'", byNickname(t, s, "LGTM").ID)

	defaults, err = s.Defaults(ctx)
	require.NoError(t, err)
	assert.NotEqual(t, byNickname(t, s, "LGTM").ID, defaults.PR, "a reply is never the default prompt")
}

func TestStarterRepliesFillIn(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)

	fill := func(nickname string, data Data) string {
		t.Helper()

		tmpl, err := byNickname(t, s, nickname).Template()
		require.NoError(t, err)

		out, err := tmpl.Execute(data)
		require.NoError(t, err)

		return out
	}

	bot := Sample(item.KindPR)
	bot.IsBot, bot.Author = true, "dependabot"

	assert.Equal(t, "Reviewed this automated update. All checks pass. Approved.\n", fill("Update OK", bot))

	update := bot.WithUpdate("build(deps): bump golang.org/x/net from 0.30.0 to 0.31.0")
	update.Checks = "failing"
	assert.Equal(t, "Reviewed the minor update of `golang.org/x/net` from 0.30.0 to 0.31.0. Approved.\n",
		fill("Update OK", update))

	renovate := bot.WithUpdate("Update dependency eslint to v9")
	assert.Equal(t, "Reviewed the major update of `eslint` to v9. All checks pass. Approved.\n", fill("Update OK", renovate))

	assert.Equal(t, "Reviewed and approved. Thanks for the work on this, @octo-user!\n", fill("LGTM", Sample(item.KindPR)))

	for _, starter := range Starters() {
		if starter.Purpose == PurposeReply {
			assert.Equal(t, starter.ReplyAction, starter.Slot.Action(), starter.Key)
		}
	}
}

func TestReplySlotWords(t *testing.T) {
	t.Parallel()

	assert.Equal(t, ReplyApprove, SlotApproveBots.Action())
	assert.Equal(t, ReplyRequestChanges, SlotRequestChanges.Action())
	assert.Equal(t, ReplyComment, SlotComment.Action())
	assert.Empty(t, ReplySlot("x").Action())

	for slot, words := range map[ReplySlot]string{
		SlotApprove: "approvals", SlotApproveBots: "bots", SlotRequestChanges: "changes", SlotComment: "comments",
	} {
		assert.Contains(t, slot.describe(), words)
	}
}

func TestCheckAReply(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)

	in := replyInput("Thanks", ReplyApprove, "- Thanks, @{{.Author}}")
	outside := Sample(item.KindPR)
	outside.OutsideContributor = true

	res, err := s.Check(t.Context(), CheckRequest{Input: in})
	require.NoError(t, err)
	assert.Empty(t, res.Fields)
	assert.Equal(t, "- Thanks, @octo-user\n", res.Preview)
	assert.Empty(t, res.Notice)
}

func TestBundleCarriesReplies(t *testing.T) {
	t.Parallel()

	src, _ := seeded(t, nil)
	path := filepath.Join(t.TempDir(), "prompts.json")

	_, err := src.Export(t.Context(), path, nil)
	require.NoError(t, err)

	b, err := readBundleFile(path)
	require.NoError(t, err)
	assert.Equal(t, BundleVersion, b.Version)

	var lgtm BundlePrompt

	for _, p := range b.Prompts {
		if p.Nickname == "LGTM" {
			lgtm = p
		}
	}

	assert.Equal(t, PurposeReply, lgtm.Purpose)
	assert.Equal(t, ReplyApprove, lgtm.Action)

	dst, _ := empty(t, nil)
	res, err := Restore(t.Context(), dst.store, "", NewLibraryFile(path, "", nil))
	require.NoError(t, err)
	assert.Equal(t, len(Starters()), res.Count)

	restored := byNickname(t, dst, "LGTM")
	assert.True(t, restored.IsReply())

	defaults, err := dst.Defaults(t.Context())
	require.NoError(t, err)
	assert.Equal(t, restored.ID, defaults.Replies.Approve, "the default replies come back")
}

func TestImportReadsAnOlderFileAsAgentPrompts(t *testing.T) {
	t.Parallel()

	v2 := []byte(`{"format":"` + BundleFormat + `","version":2,"defaults":{"pr":"Old"},"prompts":[` +
		`{"name":"Old","nickname":"Old","appliesTo":"pr","body":"Look at {{.URL}}","action":"approve"}]}`)

	b, err := DecodeBundle(v2)
	require.NoError(t, err)

	s, _ := empty(t, nil)
	_, err = s.Import(t.Context(), b.Prompts)
	require.NoError(t, err)

	old := byNickname(t, s, "Old")
	assert.Equal(t, PurposeAgent, old.Purpose)
	assert.Empty(t, old.ReplyAction)
}

func TestImportCleansAReply(t *testing.T) {
	t.Parallel()

	p, notes := cleanImport(BundlePrompt{
		Name: "Odd", Nickname: "Odd", Purpose: PurposeReply, Action: "ship", AppliesTo: AppliesToIssue, Effort: "high",
		Body: "Thanks {{.Nope}}", Models: map[agent.ID]string{agent.Claude: "opus"},
	})

	assert.Equal(t, ReplyComment, p.Action, "an unknown review becomes a comment")
	assert.Equal(t, AppliesToPR, p.AppliesTo)
	assert.Empty(t, p.Effort)
	assert.Empty(t, p.Models)
	assert.Len(t, notes, 2, "the review it was for, and the text's problems")

	ok, notes := cleanImport(BundlePrompt{Name: "Fine", Nickname: "Fine", Purpose: PurposeReply, Action: ReplyApprove, Body: "Thanks"})
	assert.Equal(t, ReplyApprove, ok.Action)
	assert.Empty(t, notes)
}
