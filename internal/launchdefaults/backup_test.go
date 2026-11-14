package launchdefaults_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/store"
)

// reviewNick is the review starter's nickname.
const reviewNick = "Review"

// knows finds the starters by nickname, as the import would among the library's prompts:
// Review for pull requests, Work on it for issues.
func (h *harness) knows(nickname string, kind item.Kind) bool {
	id, ok := h.promptID(nickname)

	return ok && (kind == item.KindPR && id == h.review || kind == item.KindIssue && id == h.work)
}

// find is knows inside an import's transaction.
func (h *harness) find(_ context.Context, nickname string, kind item.Kind) (int64, bool, error) {
	id, _ := h.promptID(nickname)

	return id, h.knows(nickname, kind), nil
}

// importKept plans kept under mode and applies it as an import does, clearing first under
// Replace, and returns the plan.
func (h *harness) importKept(t *testing.T, kept []launchdefaults.Kept, mode backup.Mode) launchdefaults.BackupPlan {
	t.Helper()

	part := launchdefaults.NewBackupPart(h.svc)

	plan, err := part.PlanBackup(t.Context(), kept, mode, h.knows)
	require.NoError(t, err)

	require.NoError(t, store.InTx(t.Context(), h.db, func(tx *sql.Tx) error {
		if mode == backup.ModeReplace {
			if err := part.ClearTx(t.Context(), tx); err != nil {
				return err
			}
		}

		_, err := part.ApplyBackup(t.Context(), tx, plan, h.find)

		return err
	}))

	return plan
}

// targets lists the stored launch defaults' targets.
func (h *harness) targets(t *testing.T) []string {
	t.Helper()

	all, err := h.svc.List(t.Context())
	require.NoError(t, err)

	out := make([]string, 0, len(all))
	for _, d := range all {
		out = append(out, d.Target())
	}

	return out
}

// labels lists a plan's items as "<change> <label>".
func labels(p backup.Plan) []string {
	out := make([]string, 0, len(p.Items))
	for _, item := range p.Items {
		out = append(out, string(item.Change)+" "+item.Label)
	}

	return out
}

func TestBackupPartExportsEveryRowByNickname(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	_, err := h.svc.Save(t.Context(), launchdefaults.Input{Owner: acme, PRPromptID: &h.review, Effort: "high"})
	require.NoError(t, err)

	kept, err := launchdefaults.NewBackupPart(h.svc).Kept(t.Context())
	require.NoError(t, err)
	require.Len(t, kept, 1)
	assert.Equal(t, reviewNick, kept[0].PRPrompt)

	data, err := launchdefaults.EncodeKept(kept, "v1.2.3", time.Date(2026, time.November, 14, 17, 0, 0, 0, time.UTC))
	require.NoError(t, err)

	back, err := launchdefaults.DecodeKept(data)
	require.NoError(t, err)
	assert.Equal(t, kept, back, "the section reads back as written")
}

func TestImportMergesLaunchDefaultsByTarget(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ctx := t.Context()

	_, err := h.svc.Save(ctx, launchdefaults.Input{Owner: acme, Effort: "low"})
	require.NoError(t, err)
	_, err = h.svc.Save(ctx, launchdefaults.Input{Owner: acme, Name: "kept-repo", Effort: "medium"})
	require.NoError(t, err)

	plan := h.importKept(t, []launchdefaults.Kept{
		{Owner: "ACME-ORG", PRPrompt: reviewNick, Effort: "high"},
		{Owner: acme, Name: "new-repo", IssuePrompt: "Work on it", Models: claude("opus")},
	}, backup.ModeMerge)

	assert.Equal(t, []string{"update acme-org", "add acme-org/new-repo"}, labels(plan.Plan))
	assert.Empty(t, plan.Notes)
	assert.Equal(t, []string{acme, "acme-org/kept-repo", "acme-org/new-repo"}, h.targets(t), "Merge deletes nothing")

	all, err := h.svc.List(ctx)
	require.NoError(t, err)
	assert.Equal(t, "high", all[0].Effort, "the file's row replaces this Mac's row for its target")
	assert.Equal(t, &h.review, all[0].PRPromptID)
	assert.Equal(t, "medium", all[1].Effort)
	assert.Equal(t, &h.work, all[2].IssuePromptID)
	assert.Equal(t, claude("opus"), all[2].Models)
}

func TestImportReplacesLaunchDefaults(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ctx := t.Context()

	_, err := h.svc.Save(ctx, launchdefaults.Input{Owner: acme, Name: "old-repo", Effort: "medium"})
	require.NoError(t, err)

	plan := h.importKept(t, []launchdefaults.Kept{{Owner: acme, Effort: "high"}}, backup.ModeReplace)

	assert.Equal(t, []string{"add acme-org", "remove acme-org/old-repo"}, labels(plan.Plan))
	assert.Equal(t, []string{acme}, h.targets(t), "Replace leaves exactly the file's rows")
}

func TestImportNotesWhatItLeavesOut(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	plan := h.importKept(t, []launchdefaults.Kept{
		{
			Owner: acme, PRPrompt: "Gone", IssuePrompt: reviewNick, Agent: "example-agent", Effort: "huge",
			Models: map[agent.ID]string{agent.Claude: "-x", agent.Codex: "gpt"}, Profiles: claude("missing-here"),
		},
		{Owner: "not an org"},
		{Owner: "other-org", PRPrompt: "Gone"},
		{Owner: acme, Effort: "low"},
	}, backup.ModeMerge)

	assert.Equal(t, []string{
		`acme-org: its coding agent, "example-agent", was left out: this version doesn't know it.`,
		`acme-org: its effort, "huge", was left out: it isn't one of the levels.`,
		`acme-org: its model for Claude, "-x", was left out.`,
		"acme-org: its prompt for pull requests, “Gone”, isn't here, so it inherits.",
		"acme-org: its prompt for issues, “Review”, isn't here, so it inherits.",
		`The launch defaults for "not an org" were left out: that isn't an org or a repository.`,
		"other-org: its prompt for pull requests, “Gone”, isn't here, so it inherits.",
		"The launch defaults for other-org were left out: they set nothing.",
		"The file holds the launch defaults for acme-org twice; the first was imported.",
	}, plan.Notes)
	assert.Equal(t, []string{"add acme-org"}, labels(plan.Plan))

	all, err := h.svc.List(t.Context())
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Nil(t, all[0].PRPromptID, "a prompt not found inherits")
	assert.Nil(t, all[0].IssuePromptID, "and so does one that isn't for the kind")
	assert.Equal(t, map[agent.ID]string{agent.Codex: "gpt"}, all[0].Models)
	assert.Equal(t, claude("missing-here"), all[0].Profiles, "a profile is kept by name, as a restore keeps it")
}

func TestImportOfTheSameRowsChangesNothing(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	_, err := h.svc.Save(t.Context(), launchdefaults.Input{Owner: acme, PRPromptID: &h.review, Effort: "high"})
	require.NoError(t, err)

	kept, err := launchdefaults.NewBackupPart(h.svc).Kept(t.Context())
	require.NoError(t, err)

	plan := h.importKept(t, kept, backup.ModeMerge)
	assert.Equal(t, []string{"same acme-org"}, labels(plan.Plan))
	assert.False(t, plan.Count.Changes())
}
