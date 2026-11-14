package prompts

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/store"
)

// applyBundle plans b under mode and applies it as an import does, returning the plan,
// what the apply counted, and the links it recorded.
func applyBundle(t *testing.T, s *Service, db *sql.DB, b Bundle, mode backup.Mode, clears bool,
) (BackupPlan, backup.Count, map[string]int64) {
	t.Helper()

	part := NewBackupPart(s)

	plan, err := part.PlanBackup(t.Context(), b, mode, clears)
	require.NoError(t, err)

	var count backup.Count

	links := map[string]int64{}

	require.NoError(t, store.InTx(t.Context(), db, func(tx *sql.Tx) error {
		var applyErr error

		count, applyErr = part.ApplyBackup(t.Context(), tx, plan, links)

		return applyErr
	}))

	return plan, count, links
}

// items lists a plan's items as "<change> <label>".
func items(p backup.Plan) []string {
	out := make([]string, 0, len(p.Items))
	for _, item := range p.Items {
		out = append(out, string(item.Change)+" "+item.Label)
	}

	return out
}

func TestBackupPartBundleIsTheWholeLibrary(t *testing.T) {
	t.Parallel()

	s, _, _ := keeping(t)

	b, err := NewBackupPart(s).Bundle(t.Context())
	require.NoError(t, err)
	assert.Len(t, b.Prompts, len(Starters()))
	assert.Equal(t, reviewNick, b.Defaults.PR)
	assert.NotEmpty(t, b.StartersAdded)
	assert.Equal(t, "v1.2.3", b.App)
}

func TestMergeImportsAsImportDoesWithTheFilesDefaults(t *testing.T) {
	t.Parallel()

	s, db := seeded(t, nil)

	b := bundleOf(mine(reviewNick), replyOf("Ship it", ReplyApprove, AppliesToPR))
	b.Prompts[0].AppliesTo = AppliesToPR
	b.Defaults = BundleDefaults{PR: reviewNick, Replies: map[ReplySlot]string{SlotApprove: "Ship it"}}

	plan, count, links := applyBundle(t, s, db, b, backup.ModeMerge, false)
	assert.Equal(t, []string{
		"add Review 2", "add Ship it", "update Default prompt for pull requests", "update Default approval reply",
	}, items(plan.Plan))
	assert.Equal(t, backup.Count{Add: 2, Update: 2}, count, "two prompts added, two defaults changed")
	assert.Equal(t, map[string]backup.PromptKind{
		reviewNick: {AppliesTo: "pr"}, "Ship it": {Reply: true, AppliesTo: "pr"},
	}, plan.Prompts)

	review2 := byNickname(t, s, "Review 2")
	assert.Equal(t, review2.ID, links[reviewNick], "a nickname in the file links to the prompt it became")

	defaults, err := s.Defaults(t.Context())
	require.NoError(t, err)
	assert.Equal(t, review2.ID, defaults.PR)
	assert.Equal(t, byNickname(t, s, "Ship it").ID, defaults.Replies.Approve)
	assert.Len(t, nicknames(t, s), len(Starters())+2, "Merge deletes nothing")
}

func TestReplaceMatchesByNicknameAndDeletesTheRest(t *testing.T) {
	t.Parallel()

	s, db := seeded(t, nil)
	ctx := t.Context()

	before := byNickname(t, s, reviewNick)
	quick := byNickname(t, s, "Quick look")
	addLaunchDefault(t, db, "acme-org", "", quick.ID)

	edited := starterAsBundled(t, StarterReviewPR, func(p *BundlePrompt) { p.Body = "Review {{.URL}} closely." })
	lgtm := starterAsBundled(t, StarterReplyLGTM, func(p *BundlePrompt) { p.Purpose, p.Action = PurposeReply, ReplyApprove })
	b := bundleOf(edited, lgtm, mine("Fresh"), replyOf("Fix checks", ReplyComment, AppliesToPR))
	b.Defaults = BundleDefaults{PR: reviewNick, Issue: "Fresh", Replies: map[ReplySlot]string{SlotApprove: lgtmNick}}
	b.StartersAdded = []string{"a-later-starter"}

	plan, count, links := applyBundle(t, s, db, b, backup.ModeReplace, false)
	assert.Equal(t, []string{
		"update Review", "same LGTM", "add Fresh", "add Fix checks",
		"remove Fix checks", "remove Needs changes", "remove Question", "remove Update OK", "remove Work on it",
		"update Default prompt for issues", "update Default approval reply for bots",
		"update Default reply for requested changes", "update Default comment reply",
	}, items(plan.Plan))
	assert.Equal(t, []string{"“Quick look” is kept: the launch defaults for acme-org use it."}, plan.Notes)
	assert.Equal(t, backup.Count{Add: 2, Update: 1 + 4, Same: 1, Remove: 5}, count,
		"the prompts, and the four defaults the file changes or leaves with none")

	assert.Equal(t, []string{"Fix checks", "Fresh", lgtmNick, "Quick look", reviewNick}, nicknames(t, s),
		"exactly the file's prompts, and the one a launch default keeps")

	review := byNickname(t, s, reviewNick)
	assert.Equal(t, before.ID, review.ID, "a prompt matched by nickname is saved over in place")
	assert.Equal(t, "Review {{.URL}} closely.", review.Body)
	assert.Equal(t, review.ID, links[reviewNick])
	assert.True(t, byNickname(t, s, "Fix checks").IsReply(),
		"a reply never saves over an agent prompt: the agent prompt is deleted, which frees its nickname")

	defaults, err := s.Defaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, review.ID, defaults.PR)
	assert.Equal(t, byNickname(t, s, "Fresh").ID, defaults.Issue)
	assert.Equal(t, byNickname(t, s, lgtmNick).ID, defaults.Replies.Approve)
	assert.Equal(t, ReplyDefaults{Approve: defaults.Replies.Approve}, defaults.Replies,
		"a reply slot the file names nothing for is left with none")

	seededKeys, err := s.store.seeded(ctx)
	require.NoError(t, err)
	assert.Contains(t, seededKeys, "a-later-starter", "the file's record of starters is kept too")
	assert.Contains(t, seededKeys, StarterFixChecks, "and a starter Replace deleted never comes back")
}

func TestReplaceWithTheLaunchDefaultsKeepsNoPromptForThem(t *testing.T) {
	t.Parallel()

	s, db := seeded(t, nil)
	quick := byNickname(t, s, "Quick look")

	part := NewBackupPart(s)
	b := bundleOf(mine("Only"))

	plan, err := part.PlanBackup(t.Context(), b, backup.ModeReplace, true)
	require.NoError(t, err)
	assert.Empty(t, plan.Notes, "the launch defaults are replaced too, so none keeps a prompt")
	assert.Contains(t, items(plan.Plan), "remove Quick look")

	addLaunchDefault(t, db, "acme-org", "", quick.ID)

	plan, err = part.PlanBackup(t.Context(), b, backup.ModeReplace, false)
	require.NoError(t, err)
	assert.NotContains(t, items(plan.Plan), "remove Quick look")
}

func TestReplaceOfTheSameLibraryChangesNothing(t *testing.T) {
	t.Parallel()

	s, db := seeded(t, nil)

	b, err := NewBackupPart(s).Bundle(t.Context())
	require.NoError(t, err)

	before := nicknames(t, s)

	plan, count, _ := applyBundle(t, s, db, b, backup.ModeReplace, false)
	assert.False(t, plan.Count.Changes(), "%v", items(plan.Plan))
	assert.False(t, count.Changes())
	assert.Equal(t, before, nicknames(t, s))
}
