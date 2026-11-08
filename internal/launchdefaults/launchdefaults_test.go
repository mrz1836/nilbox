package launchdefaults_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// acme is the fake org most tests use.
const acme = "acme-org"

// errFakeRepos is what failingRepos returns.
var errFakeRepos = errors.New("repositories unreadable")

// fakeRepos lists fixed repositories, as the inbox would.
type fakeRepos []launchdefaults.Target

func (f fakeRepos) Repos(context.Context) ([]launchdefaults.Target, error) {
	return f, nil
}

// failingRepos cannot list the repositories.
type failingRepos struct{}

func (failingRepos) Repos(context.Context) ([]launchdefaults.Target, error) {
	return nil, errFakeRepos
}

// harness is a launch defaults Service over a database with the starter prompts and a
// "work" profile, and a prompt Service whose usage comes from the launch defaults.
type harness struct {
	db      *sql.DB
	store   *launchdefaults.Store
	known   *profiles.Store
	library *prompts.Store
	svc     *launchdefaults.Service
	prompts *prompts.Service
	review  int64 // the review starter, for pull requests
	work    int64 // the work-on-issue starter, for issues
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	db := testutil.DB(t)
	known := profiles.NewStore(db, nil)
	library := prompts.NewStore(db, nil)
	stored := launchdefaults.NewStore(db, nil)

	require.NoError(t, prompts.Seed(t.Context(), library))
	require.NoError(t, known.Sync(t.Context(), []profiles.Profile{{Name: "work", ConfigDir: "/tmp/work"}}))

	defaults, err := library.Defaults(t.Context())
	require.NoError(t, err)

	return &harness{
		db:      db,
		store:   stored,
		known:   known,
		library: library,
		svc:     launchdefaults.NewService(launchdefaults.Config{Store: stored, Profiles: known, Prompts: library}),
		prompts: prompts.NewService(prompts.Config{Store: library, Usage: stored}),
		review:  defaults.PR,
		work:    defaults.Issue,
	}
}

// withRepos returns a Service over the harness's stores that lists repos as the
// repositories the inbox has seen.
func (h *harness) withRepos(repos launchdefaults.RepoSource) *launchdefaults.Service {
	return launchdefaults.NewService(launchdefaults.Config{
		Store: h.store, Profiles: h.known, Prompts: h.library, Repos: repos,
	})
}

// fields maps a ValidationError's fields to their messages.
func fields(list []agent.FieldError) map[string]string {
	out := map[string]string{}
	for _, f := range list {
		out[f.Field] = f.Message
	}

	return out
}

func TestSaveAndList(t *testing.T) {
	h := newHarness(t)
	ctx := t.Context()

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
		Owner: acme, Name: "example-repo", IssuePromptID: &h.work, Model: "sonnet", Effort: "low", Worktree: new(false),
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
	gone := new(int64(9999))

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
		{name: "missing prompt", in: launchdefaults.Input{Owner: acme, PRPromptID: gone}, field: launchdefaults.FieldPRPromptID, want: "no longer exists"},
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
	svc := h.prompts

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
	_, err = h.svc.Save(ctx, launchdefaults.Input{Owner: acme, PRPromptID: &fix.ID})
	require.NoError(t, err)

	// The launch defaults' foreign keys refuse the delete, and their rows say where.
	var inUse *prompts.InUseError

	err = svc.Delete(ctx, fix.ID)
	require.ErrorIs(t, err, prompts.ErrPromptInUse)
	require.ErrorAs(t, err, &inUse)
	assert.Equal(t, `"Fix checks" can't be deleted while it's a default for acme-org and acme-org/example-repo; `+
		"choose other defaults there first", err.Error())

	all, err = svc.List(ctx)
	require.NoError(t, err)

	fix.Usage = prompts.Usage{
		Defaults: []item.Kind{}, Orgs: []string{acme}, Repos: []string{"acme-org/example-repo"},
		Kinds: []item.Kind{item.KindPR},
	}
	require.Contains(t, all, fix, "the prompt is still there, with where it is used")

	require.NoError(t, h.svc.Remove(ctx, acme, "example-repo"))
	require.NoError(t, h.svc.Remove(ctx, acme, ""))
	require.NoError(t, svc.Delete(ctx, fix.ID))
}

func TestPromptUses(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ctx := t.Context()

	_, err := h.svc.Save(ctx, launchdefaults.Input{Owner: acme, PRPromptID: &h.review, IssuePromptID: &h.work})
	require.NoError(t, err)
	_, err = h.svc.Save(ctx, launchdefaults.Input{Owner: acme, Name: "example-repo", IssuePromptID: &h.work})
	require.NoError(t, err)
	_, err = h.svc.Save(ctx, launchdefaults.Input{Owner: "beta-org", Effort: "high"})
	require.NoError(t, err)

	uses, err := h.store.PromptUses(ctx)
	require.NoError(t, err)
	assert.Equal(t, []prompts.Use{
		{PromptID: h.review, Kind: item.KindPR, Target: repo.Name{Owner: acme}},
		{PromptID: h.work, Kind: item.KindIssue, Target: repo.Name{Owner: acme}},
		{PromptID: h.work, Kind: item.KindIssue, Target: repo.Name{Owner: acme, Repo: "example-repo"}},
	}, uses, "every org's and repository's default prompt, and nothing for defaults that name none")
}

func TestTargets(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	got, err := h.withRepos(fakeRepos{
		{Owner: acme, Name: "api", Items: 2},
		{Owner: acme, Name: "web", Items: 1},
		{Owner: "beta-org", Name: "tools"},
	}).Targets(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []launchdefaults.Target{
		{Owner: acme, Items: 3},
		{Owner: acme, Name: "api", Items: 2},
		{Owner: acme, Name: "web", Items: 1},
		{Owner: "beta-org", Items: 0},
		{Owner: "beta-org", Name: "tools", Items: 0},
	}, got)
}

func TestTargetsGroupAnOwnerIgnoringCase(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	got, err := h.withRepos(fakeRepos{
		{Owner: "Acme-Org", Name: "api", Items: 2},
		{Owner: acme, Name: "web", Items: 1},
	}).Targets(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []launchdefaults.Target{
		{Owner: "Acme-Org", Items: 3},
		{Owner: "Acme-Org", Name: "api", Items: 2},
		{Owner: acme, Name: "web", Items: 1},
	}, got, "one owner, named as its first repository spells it")
}

func TestTargetsWithNoRepositories(t *testing.T) {
	t.Parallel()

	got, err := newHarness(t).withRepos(fakeRepos{}).Targets(t.Context())
	require.NoError(t, err)
	assert.NotNil(t, got, "the UI always gets a list")
	assert.Empty(t, got)
}

func TestIndex(t *testing.T) {
	idx := launchdefaults.NewIndex([]launchdefaults.Defaults{
		{Owner: acme, Model: "opus"},
		{Owner: acme, Name: "api", Effort: "high"},
	})

	org, own := idx.For(repo.Name{Owner: "ACME-ORG", Repo: "Api"})
	require.NotNil(t, org)
	require.NotNil(t, own)
	assert.Equal(t, "opus", org.Model)
	assert.Equal(t, "high", own.Effort)

	org, own = idx.For(repo.Name{Owner: acme, Repo: "web"})
	require.NotNil(t, org)
	assert.Nil(t, own)

	org, own = idx.For(repo.Name{Owner: "other-org"})
	assert.Nil(t, org)
	assert.Nil(t, own)
}

func TestInputEmpty(t *testing.T) {
	assert.True(t, launchdefaults.Input{Owner: acme, ExtraFlags: " "}.Empty())
	assert.False(t, launchdefaults.Input{Owner: acme, Worktree: new(true)}.Empty())
	assert.False(t, launchdefaults.Input{Owner: acme, Agent: "claude"}.Empty())
}
