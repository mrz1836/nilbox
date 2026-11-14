package prompts

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
)

// Nicknames of the starters the default tests name.
const (
	reviewNick = "Review"
	lgtmNick   = "LGTM"
)

// replyOf is a reply for action, applying to appliesTo, in portable form.
func replyOf(nickname string, action ReplyAction, appliesTo AppliesTo) BundlePrompt {
	return BundlePrompt{
		Name: nickname + " reply", Nickname: nickname, AppliesTo: appliesTo, Body: "Thanks for " + nickname + ".",
		Purpose: PurposeReply, Action: action, Models: map[agent.ID]string{}, ExtraArgs: map[agent.ID][]string{},
	}
}

// chosen returns the defaults the import dialog sends for offers, each named by the
// nickname of its entry's prompt as the preview gave it.
func chosen(p ImportPreview, offers ...DefaultOffer) BundleDefaults {
	var out BundleDefaults
	for _, o := range offers {
		out.Set(o.Slot, p.Entries[o.Entry].Prompt.Nickname)
	}

	return out
}

// prompts returns the preview's prompts, as the import dialog sends the ticked ones.
func (p ImportPreview) prompts(entries ...int) []BundlePrompt {
	out := make([]BundlePrompt, 0, len(entries))
	for _, i := range entries {
		out = append(out, p.Entries[i].Prompt)
	}

	return out
}

func TestExportHoldsEveryKindOfPromptAndEveryDefault(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()

	issues := replyInput("Looking", ReplyComment, "Looking into it, @{{.Author}}.")
	issues.AppliesTo = AppliesToIssue

	_, err := s.Create(ctx, issues)
	require.NoError(t, err)

	path := filepath.Join(t.TempDir(), "all.json")
	n, err := s.Export(ctx, path, nil)
	require.NoError(t, err)
	assert.Equal(t, ExportResult{Prompts: 4, Replies: 5}, n)

	b, err := readBundleFile(path)
	require.NoError(t, err)

	kinds := map[string]string{}
	for _, p := range b.Prompts {
		kinds[p.Nickname] = string(p.Purpose) + " " + string(p.Action) + " " + string(p.AppliesTo)
	}

	assert.Equal(t, map[string]string{
		reviewNick: "agent  pr", "Work on it": "agent  issue", "Quick look": "agent  pr", "Fix checks": "agent  pr",
		lgtmNick: "reply approve pr", "Update OK": "reply approve pr", "Needs changes": "reply request_changes pr",
		"Question": "reply comment pr", "Looking": "reply comment issue",
	}, kinds, "every purpose, review, and kind of item is written, a comment reply for issues too")

	for _, slot := range DefaultSlots() {
		assert.NotEmpty(t, b.Defaults.For(slot), "the default for %s is written", slot)
	}
}

func TestPreviewOffersTheFilesDefaultsThatFit(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()

	_, err := s.Create(ctx, replyInput("Thanks", ReplyApprove, "Thanks for Thx."))
	require.NoError(t, err)

	b := bundleOf(
		mine(reviewNick), // renamed: the starter has the nickname
		replyOf("Thx", ReplyApprove, AppliesToPR),           // skipped: "Thanks" holds it
		replyOf("Ship it", ReplyApprove, AppliesToPR),       // added
		replyOf("For issues", ReplyComment, AppliesToIssue), // added, but no default reply applies to issues
	)
	b.Prompts[0].AppliesTo = AppliesToPR
	b.Prompts[1].Name = "Thanks reply"
	b.Defaults = BundleDefaults{PR: reviewNick, Issue: reviewNick, Replies: map[ReplySlot]string{
		SlotApprove: "Thx", SlotApproveBots: "Ship it", SlotRequestChanges: "Ship it", SlotComment: "For issues",
	}}

	got := previewAll(t, s, b)
	require.Equal(t, []string{"rename Review 2", "skip Thanks", "add Ship it", "add For issues"}, outcomes(got.Entries))
	assert.Equal(t, []DefaultOffer{
		{Slot: DefaultSlotPR, Nickname: "Review 2", Current: reviewNick, Entry: 0},
		{Slot: DefaultSlotApprove, Nickname: "Thanks", Current: lgtmNick, Entry: 1},
		{Slot: DefaultSlotApproveBots, Nickname: "Ship it", Current: "Update OK", Entry: 2},
	}, got.Defaults, "a default is offered through a rename and a skip, never where its prompt doesn't fit the slot")

	res, err := s.Import(ctx, got.prompts(0, 1, 2, 3), chosen(got, got.Defaults...))
	require.NoError(t, err)
	assert.Equal(t, ImportResult{Added: 3, Skipped: 1, Defaults: 3, First: res.First}, res)

	defaults, err := s.Defaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, byNickname(t, s, "Review 2").ID, defaults.PR)
	assert.Equal(t, byNickname(t, s, "Thanks").ID, defaults.Replies.Approve)
	assert.Equal(t, byNickname(t, s, "Ship it").ID, defaults.Replies.ApproveBots)
	assert.Equal(t, byNickname(t, s, "Needs changes").ID, defaults.Replies.RequestChanges, "an agent prompt is never a reply")
	assert.Equal(t, byNickname(t, s, "Question").ID, defaults.Replies.Comment, "a reply for issues is never the default")
	assert.Equal(t, byNickname(t, s, "Work on it").ID, defaults.Issue, "a prompt for pull requests is never the issues' default")
}

func TestPreviewOffersNoDefaultTheLibraryAlreadyHas(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	b := bundleOf(starterAsBundled(t, StarterReviewPR, nil), starterAsBundled(t, StarterReplyLGTM, func(p *BundlePrompt) {
		p.Purpose, p.Action = PurposeReply, ReplyApprove
	}))
	b.Defaults = BundleDefaults{PR: reviewNick, Replies: map[ReplySlot]string{SlotApprove: lgtmNick}}

	got := previewAll(t, s, b)
	assert.Equal(t, []string{"skip Review", "skip LGTM"}, outcomes(got.Entries))
	assert.Empty(t, got.Defaults, "importing would change neither default")
}

func TestImportLeavesOutADefaultWhosePromptIsNotImported(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()

	b := bundleOf(mine(reviewNick), replyOf("Ship it", ReplyApprove, AppliesToPR))
	b.Defaults = BundleDefaults{PR: reviewNick, Replies: map[ReplySlot]string{SlotApprove: "Ship it"}}

	got := previewAll(t, s, b)
	require.Len(t, got.Defaults, 2)

	// The user unticked "Ship it", and the dialog sent its offer anyway.
	res, err := s.Import(ctx, got.prompts(0), chosen(got, got.Defaults...))
	require.NoError(t, err)
	assert.Equal(t, 1, res.Added)
	assert.Equal(t, 1, res.Defaults)

	defaults, err := s.Defaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, byNickname(t, s, "Review 2").ID, defaults.PR, "a nickname names the prompt imported, not the library's")
	assert.Equal(t, byNickname(t, s, lgtmNick).ID, defaults.Replies.Approve, "a reply not imported is never a default")

	for _, p := range mustList(t, s) {
		assert.NotEqual(t, "Ship it", p.Nickname)
	}
}

func TestImportWithNoDefaultsChangesNone(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()

	before, err := s.Defaults(ctx)
	require.NoError(t, err)

	b := bundleOf(mine(reviewNick))
	got := previewAll(t, s, b)

	res, err := s.Import(ctx, got.prompts(0), BundleDefaults{})
	require.NoError(t, err)
	assert.Zero(t, res.Defaults)

	after, err := s.Defaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, after)
}

func TestDefaultSlotsCoverTheKindsAndTheReplies(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []DefaultSlot{
		DefaultSlotPR, DefaultSlotIssue, DefaultSlotApprove, DefaultSlotApproveBots, DefaultSlotRequestChanges,
		DefaultSlotComment,
	}, DefaultSlots())

	for _, slot := range ReplySlots() {
		_, isKind := DefaultSlot(slot).kind()
		assert.False(t, isKind, slot)
	}

	kind, ok := DefaultSlotIssue.kind()
	assert.True(t, ok)
	assert.Equal(t, item.KindIssue, kind)

	var d BundleDefaults
	for _, slot := range DefaultSlots() {
		d.Set(slot, string(slot))
		assert.Equal(t, string(slot), d.For(slot))
	}

	assert.Empty(t, d.For("later"))
}

// mustList lists s's prompts.
func mustList(t *testing.T, s *Service) []Prompt {
	t.Helper()

	all, err := s.List(t.Context())
	require.NoError(t, err)

	return all
}
