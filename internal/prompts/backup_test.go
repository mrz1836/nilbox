package prompts

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/testutil"
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
		"remove Address review", "remove Fix checks", "remove Fix dep security", "remove Heal Dependabot PR",
		"remove Needs changes", "remove Needs repro", "remove Question", "remove Re-review", "remove Re-run failed jobs",
		"remove Resolve conflicts", "remove Update OK", "remove Work on it",
		"update Default prompt for issues", "update Default prompt when checks fail",
		"update Default prompt when it has conflicts", "update Default prompt when changes are requested",
		"update Default prompt when you're asked to review again", "update Default approval reply for bots",
		"update Default reply for requested changes", "update Default comment reply",
	}, items(plan.Plan))
	assert.Equal(t, []string{"“Quick look” is kept: the launch defaults for acme-org use it."}, plan.Notes)
	assert.Equal(t, backup.Count{Add: 2, Update: 1 + 8, Same: 1, Remove: 12}, count,
		"the prompts, and the eight defaults the file changes or leaves with none")

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

// planOnly plans b under mode as an import does, with the launch defaults kept.
func planOnly(t *testing.T, s *Service, b Bundle, mode backup.Mode) BackupPlan {
	t.Helper()

	plan, err := NewBackupPart(s).PlanBackup(t.Context(), b, mode, false)
	require.NoError(t, err)

	return plan
}

// applyErr applies plan as an import does and returns what failed.
func applyErr(t *testing.T, s *Service, db *sql.DB, plan BackupPlan) error {
	t.Helper()

	return store.InTx(t.Context(), db, func(tx *sql.Tx) error {
		_, err := NewBackupPart(s).ApplyBackup(t.Context(), tx, plan, map[string]int64{})

		return err
	})
}

func TestBackupPartBundleReportsALibraryItCannotRead(t *testing.T) {
	t.Parallel()

	_, err := NewBackupPart(closedService(t)).Bundle(t.Context())
	require.ErrorContains(t, err, "read setting "+prefDefaultPR)

	for _, key := range []string{prefSeeded, prefSeededSlots} {
		s, _ := seeded(t, nil)
		require.NoError(t, s.store.prefs.Set(t.Context(), key, "not a list"))

		_, err = NewBackupPart(s).Bundle(t.Context())
		require.ErrorContains(t, err, "read setting "+key, "a damaged record of the library's history")
	}
}

func TestPlanBackupReportsWhatItCannotPlan(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)

	_, err := NewBackupPart(s).PlanBackup(t.Context(), bundleOf(make([]BundlePrompt, maxBundlePrompts+1)...),
		backup.ModeMerge, false)
	require.ErrorIs(t, err, ErrBundleTooLarge)

	for table, want := range map[string]string{
		"prefs":           "read setting " + prefDefaultPR,
		"launch_defaults": "read where the prompts are used",
	} {
		s, db := seeded(t, nil)
		testutil.Exec(t, db, "DROP TABLE "+table)

		_, err = NewBackupPart(s).PlanBackup(t.Context(), bundleOf(mine("Fresh")), backup.ModeReplace, false)
		require.ErrorContains(t, err, want, table)
	}
}

func TestApplyBackupWritesAllOfTheLibraryOrNothing(t *testing.T) {
	t.Parallel()

	const dropPrefs = "DROP TABLE prefs"

	edited := func(t *testing.T) Bundle {
		t.Helper()

		return bundleOf(starterAsBundled(t, StarterReviewPR, func(p *BundlePrompt) { p.Body = "Review {{.URL}} my way." }))
	}

	fresh := func(*testing.T) Bundle { return bundleOf(mine("Fresh")) }

	freshDefault := func(*testing.T) Bundle {
		b := bundleOf(mine("Fresh"))
		b.Defaults = BundleDefaults{Issue: "Fresh"}

		return b
	}

	tests := []struct {
		name    string
		mode    backup.Mode
		refuse  string
		bundle  func(t *testing.T) Bundle
		wantErr string
	}{
		{
			name: "merge, a prompt that cannot be added", mode: backup.ModeMerge, refuse: refusePromptInserts,
			bundle: fresh, wantErr: `import "Fresh"`,
		},
		{
			name: "merge, a starter that cannot be saved over", mode: backup.ModeMerge, refuse: refusePromptUpdates,
			bundle: edited, wantErr: `import "Review"`,
		},
		{
			name: "merge, a default that cannot be stored", mode: backup.ModeMerge, refuse: refusePrefInserts,
			bundle: freshDefault, wantErr: "refused by the test",
		},
		{
			name: "replace, a prompt that cannot be deleted", mode: backup.ModeReplace, refuse: refusePromptDeletes,
			bundle: fresh, wantErr: "refused by the test",
		},
		{
			name: "replace, a prompt that cannot be saved over", mode: backup.ModeReplace, refuse: refusePromptUpdates,
			bundle: edited, wantErr: `import "Review"`,
		},
		{
			name: "replace, a prompt that cannot be added", mode: backup.ModeReplace, refuse: refusePromptInserts,
			bundle: fresh, wantErr: `import "Fresh"`,
		},
		{
			name: "replace, a default that cannot be stored", mode: backup.ModeReplace, refuse: refusePrefInserts,
			bundle: freshDefault, wantErr: "refused by the test",
		},
		{
			name: "replace, a library gone unreadable", mode: backup.ModeReplace, refuse: dropPrefs,
			bundle: fresh, wantErr: "read setting " + prefDefaultPR,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s, db := seeded(t, nil)
			before := mustList(t, s)
			plan := planOnly(t, s, tt.bundle(t), tt.mode)

			testutil.Exec(t, db, tt.refuse)

			require.ErrorContains(t, applyErr(t, s, db, plan), tt.wantErr)

			if tt.refuse != dropPrefs {
				assert.Equal(t, before, mustList(t, s), "the import's transaction rolled back")
			}
		})
	}
}

func TestMergeSavesOverAStarterStillAsTheAppAddedIt(t *testing.T) {
	t.Parallel()

	s, db := seeded(t, nil)
	review := byNickname(t, s, reviewNick)
	edit := starterAsBundled(t, StarterReviewPR, func(p *BundlePrompt) { p.Body = "Review {{.URL}} my way." })

	plan, count, links := applyBundle(t, s, db, bundleOf(edit), backup.ModeMerge, false)
	assert.Equal(t, []backup.Item{{Label: reviewNick, Change: backup.ChangeUpdate, Detail: "saved over the starter"}},
		plan.Items)
	assert.Equal(t, backup.Count{Update: 1}, count)
	assert.Equal(t, review.ID, links[reviewNick])
	assert.Equal(t, "Review {{.URL}} my way.", byNickname(t, s, reviewNick).Body)
}

func TestBackupPlansNoteWhatCleaningAPromptChanged(t *testing.T) {
	t.Parallel()

	odd := mine("Fresh")
	odd.Effort = "warp"

	for _, mode := range []backup.Mode{backup.ModeMerge, backup.ModeReplace} {
		s, _ := seeded(t, nil)

		plan := planOnly(t, s, bundleOf(odd), mode)
		assert.Contains(t, plan.Notes, `“Fresh”: Its effort, "warp", was left out: it isn’t one of the levels.`, mode)
	}
}

func TestReplaceRenamesAPromptWhoseNicknameTheFileHoldsTwice(t *testing.T) {
	t.Parallel()

	s, db := seeded(t, nil)

	other := mine("Fresh")
	other.Body = "Another text for {{.URL}}"

	plan, count, links := applyBundle(t, s, db, bundleOf(mine("Fresh"), other), backup.ModeReplace, true)
	require.GreaterOrEqual(t, len(plan.Items), 2)
	assert.Equal(t, []backup.Item{
		{Label: "Fresh", Change: backup.ChangeAdd},
		{Label: "Fresh 2", Change: backup.ChangeAdd, Detail: "renamed from “Fresh”"},
	}, plan.Items[:2])
	assert.Equal(t, 2, count.Add)
	assert.Equal(t, byNickname(t, s, "Fresh").ID, links["Fresh"], "the file's nickname links to the first")
	assert.Equal(t, "Another text for {{.URL}}", byNickname(t, s, "Fresh 2").Body)
}

func TestReplaceKeepsAPromptALaunchDefaultStillNames(t *testing.T) {
	t.Parallel()

	s, db := seeded(t, nil)
	quick := byNickname(t, s, "Quick look")

	part := NewBackupPart(s)
	plan, err := part.PlanBackup(t.Context(), bundleOf(mine("Only")), backup.ModeReplace, true)
	require.NoError(t, err)
	assert.Contains(t, items(plan.Plan), "remove Quick look", "the plan expects the launch defaults to be replaced")

	// The launch defaults the import was to replace still name the prompt as it applies.
	addLaunchDefault(t, db, "acme-org", "", quick.ID)

	var count backup.Count

	require.NoError(t, store.InTx(t.Context(), db, func(tx *sql.Tx) error {
		count, err = part.ApplyBackup(t.Context(), tx, plan, map[string]int64{})

		return err
	}))
	assert.Equal(t, plan.Count.Remove-1, count.Remove, "the prompt a launch default names is not counted removed")
	assert.Equal(t, []string{"Only", "Quick look"}, nicknames(t, s), "its foreign key keeps it")
}

func TestADefaultSlotOfAnotherBuildIsNamedPlainly(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Default", DefaultSlot("later").Label())
	assert.Equal(t, "Default approval reply", DefaultSlotApprove.Label())
}
