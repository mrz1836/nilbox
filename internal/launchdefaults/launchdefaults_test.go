package launchdefaults_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	inboxmodel "github.com/mrz1836/nilbox/internal/inbox/model"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	profilesmodel "github.com/mrz1836/nilbox/internal/profiles/model"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// acme is the fake org most tests use.
const acme = "acme-org"

// harness is a launch defaults Service over a database with the starter prompts and a
// "work" profile.
type harness struct {
	db     *gorm.DB
	svc    *launchdefaults.Service
	review int64 // the review starter, for pull requests
	work   int64 // the work-on-issue starter, for issues
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	db := testutil.DB(t)

	require.NoError(t, prompts.Seed(t.Context(), db))
	require.NoError(t, db.Create(&profilesmodel.ClaudeProfile{Name: "work", ConfigDir: "/tmp/work"}).Error)

	defaults, err := prompts.LoadDefaults(t.Context(), db)
	require.NoError(t, err)

	return &harness{db: db, svc: launchdefaults.NewService(db), review: defaults.PR, work: defaults.Issue}
}

// fields maps a ValidationError's fields to their messages.
func fields(list []launchdefaults.FieldError) map[string]string {
	out := map[string]string{}
	for _, f := range list {
		out[f.Field] = f.Message
	}

	return out
}

func TestSaveAndList(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	off := false

	org, err := h.svc.Save(ctx, launchdefaults.Input{
		Owner: " acme-org ", Profile: "work", PRPromptID: &h.review, ExtraFlags: "--advisor fable",
	})
	require.NoError(t, err)
	assert.Equal(t, acme, org.Owner)
	assert.Empty(t, org.Name)
	assert.Equal(t, acme, org.Target())
	assert.Equal(t, "work", org.Profile)
	assert.Equal(t, []string{"--advisor", "fable"}, org.ExtraArgs)
	assert.Equal(t, "--advisor fable", org.ExtraFlags)
	assert.NotEmpty(t, org.UpdatedAt)

	repo, err := h.svc.Save(ctx, launchdefaults.Input{
		Owner: acme, Name: "example-repo", IssuePromptID: &h.work, Model: "sonnet", Effort: "low", Worktree: &off,
	})
	require.NoError(t, err)
	assert.Equal(t, "acme-org/example-repo", repo.Target())
	require.NotNil(t, repo.Worktree)
	assert.False(t, *repo.Worktree)
	assert.Nil(t, repo.PlanMode)
	assert.Empty(t, repo.ExtraArgs)

	// Saving again replaces the row, matching the target whatever its case.
	updated, err := h.svc.Save(ctx, launchdefaults.Input{Owner: "ACME-ORG", Profile: "work", Effort: "max"})
	require.NoError(t, err)
	assert.Equal(t, acme, updated.Owner, "the stored name keeps its case")
	assert.Equal(t, "max", updated.Effort)
	assert.Nil(t, updated.PRPromptID, "the whole row is replaced")

	all, err := h.svc.List(ctx)
	require.NoError(t, err)
	require.Len(t, all, 2)
	assert.Equal(t, acme, all[0].Target(), "an owner's own defaults come first")
	assert.Equal(t, "acme-org/example-repo", all[1].Target())
}

func TestSaveWithNothingSetRemoves(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	_, err := h.svc.Save(ctx, launchdefaults.Input{Owner: acme, Model: "opus"})
	require.NoError(t, err)

	got, err := h.svc.Save(ctx, launchdefaults.Input{Owner: acme, ExtraFlags: "  "})
	require.NoError(t, err)
	assert.Equal(t, acme, got.Owner)

	all, err := h.svc.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, all)

	// Clearing defaults that were never saved is fine too.
	_, err = h.svc.Save(ctx, launchdefaults.Input{Owner: "other-org"})
	require.NoError(t, err)
}

func TestValidation(t *testing.T) {
	h := newHarness(t)
	gone := int64(9999)

	tests := []struct {
		name  string
		in    launchdefaults.Input
		field string
		want  string
	}{
		{name: "no owner", in: launchdefaults.Input{Model: "opus"}, field: launchdefaults.FieldOwner, want: "Choose an org or user"},
		{name: "bad owner", in: launchdefaults.Input{Owner: "-acme", Model: "opus"}, field: launchdefaults.FieldOwner},
		{name: "bad name", in: launchdefaults.Input{Owner: acme, Name: "a b", Model: "opus"}, field: launchdefaults.FieldName},
		{name: "agent", in: launchdefaults.Input{Owner: acme, Agent: "codex"}, field: launchdefaults.FieldAgent, want: `There's no coding tool called "codex".`},
		{name: "profile", in: launchdefaults.Input{Owner: acme, Profile: "nope"}, field: launchdefaults.FieldProfile, want: `There's no profile called "nope".`},
		{name: "missing prompt", in: launchdefaults.Input{Owner: acme, PRPromptID: &gone}, field: launchdefaults.FieldPRPromptID, want: "no longer exists"},
		{name: "issue prompt for PRs", in: launchdefaults.Input{Owner: acme, PRPromptID: &h.work}, field: launchdefaults.FieldPRPromptID, want: `"Work on it" isn't a prompt for pull requests.`},
		{name: "PR prompt for issues", in: launchdefaults.Input{Owner: acme, IssuePromptID: &h.review}, field: launchdefaults.FieldIssuePromptID, want: `"Review" isn't a prompt for issues.`},
		{name: "model", in: launchdefaults.Input{Owner: acme, Model: "-x"}, field: launchdefaults.FieldModel},
		{name: "effort", in: launchdefaults.Input{Owner: acme, Effort: "huge"}, field: launchdefaults.FieldEffort},
		{name: "reserved flag", in: launchdefaults.Input{Owner: acme, ExtraFlags: "--dangerously-skip-permissions"}, field: launchdefaults.FieldExtraFlags, want: "--dangerously-skip-permissions can't be added: permissions come from the profile"},
		{name: "open quote", in: launchdefaults.Input{Owner: acme, ExtraFlags: `--x "y`}, field: launchdefaults.FieldExtraFlags, want: "never closed"},
		{name: "empty flag", in: launchdefaults.Input{Owner: acme, ExtraFlags: `""`}, field: launchdefaults.FieldExtraFlags, want: "Remove the empty flag"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := h.svc.Check(t.Context(), tt.in)
			require.NoError(t, err)

			byField := fields(got)
			require.Contains(t, byField, tt.field)
			assert.Contains(t, byField[tt.field], tt.want)
			assert.Len(t, byField, 1, "%v", byField)

			_, err = h.svc.Save(t.Context(), tt.in)
			require.ErrorIs(t, err, launchdefaults.ErrInvalidDefaults)
		})
	}

	got, err := h.svc.Check(t.Context(), launchdefaults.Input{Owner: acme, Profile: "work"})
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.NotNil(t, got, "the UI always gets a list")
}

func TestRemove(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

	_, err := h.svc.Save(ctx, launchdefaults.Input{Owner: acme, Name: "example-repo", Model: "opus"})
	require.NoError(t, err)

	require.ErrorIs(t, h.svc.Remove(ctx, acme, ""), launchdefaults.ErrNotFound)
	require.NoError(t, h.svc.Remove(ctx, "Acme-Org", "Example-Repo"))

	all, err := h.svc.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, all)
}

func TestDefaultPromptsCannotBeDeletedWhileUsed(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()
	svc := prompts.NewService(prompts.Config{DB: h.db})

	all, err := svc.List(ctx)
	require.NoError(t, err)

	var fix prompts.Prompt

	for _, p := range all {
		if p.StarterKey == prompts.StarterFixChecks {
			fix = p
		}
	}

	_, err = h.svc.Save(ctx, launchdefaults.Input{Owner: acme, Name: "example-repo", PRPromptID: &fix.ID})
	require.NoError(t, err)
	require.ErrorIs(t, svc.Delete(ctx, fix.ID), prompts.ErrPromptInUse)

	require.NoError(t, h.svc.Remove(ctx, acme, "example-repo"))
	require.NoError(t, svc.Delete(ctx, fix.ID))
}

func TestTargets(t *testing.T) {
	h := newHarness(t)
	at := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)

	account := inboxmodel.Account{Login: "octo-user"}
	require.NoError(t, h.db.Create(&account).Error)

	repos := []inboxmodel.Repo{
		{Owner: acme, Name: "web"},
		{Owner: acme, Name: "api"},
		{Owner: "beta-org", Name: "tools"},
		{Owner: acme, Name: "old", IsArchived: true},
	}
	for i := range repos {
		require.NoError(t, h.db.Create(&repos[i]).Error)
	}

	for n, repo := range []int64{repos[1].ID, repos[1].ID, repos[0].ID, repos[2].ID} {
		require.NoError(t, h.db.Create(&inboxmodel.Item{
			AccountID: account.ID, RepoID: repo, Kind: inboxmodel.KindPR, Number: n + 1,
			ItemCreatedAt: at, ItemUpdatedAt: at, FetchedAt: at, ReasonAssigned: n != 3,
		}).Error)
	}

	got, err := h.svc.Targets(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []launchdefaults.Target{
		{Owner: acme, Items: 3},
		{Owner: acme, Name: "api", Items: 2},
		{Owner: acme, Name: "web", Items: 1},
		{Owner: "beta-org", Items: 0},
		{Owner: "beta-org", Name: "tools", Items: 0},
	}, got)
}

func TestIndex(t *testing.T) {
	idx := launchdefaults.NewIndex([]launchdefaults.Defaults{
		{Owner: acme, Model: "opus"},
		{Owner: acme, Name: "api", Effort: "high"},
	})

	org, repo := idx.For("ACME-ORG", "Api")
	require.NotNil(t, org)
	require.NotNil(t, repo)
	assert.Equal(t, "opus", org.Model)
	assert.Equal(t, "high", repo.Effort)

	org, repo = idx.For(acme, "web")
	require.NotNil(t, org)
	assert.Nil(t, repo)

	org, repo = idx.For("other-org", "")
	assert.Nil(t, org)
	assert.Nil(t, repo)
}

func TestInputEmpty(t *testing.T) {
	on := true

	assert.True(t, launchdefaults.Input{Owner: acme, ExtraFlags: " "}.Empty())
	assert.False(t, launchdefaults.Input{Owner: acme, Worktree: &on}.Empty())
	assert.False(t, launchdefaults.Input{Owner: acme, Agent: "claude"}.Empty())
}
