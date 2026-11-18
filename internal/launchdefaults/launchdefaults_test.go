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

// acme is the fake org most tests use, beta another, apiRepo a repository's name, and
// profileAlt the harness's Codex profile.
const (
	acme       = "acme-org"
	beta       = "beta-org"
	apiRepo    = "api"
	profileAlt = "alt"
)

// errFakeRepos is what failingRepos returns, and errFakeClones what failingClones does.
var (
	errFakeRepos  = errors.New("repositories unreadable")
	errFakeClones = errors.New("clones unreadable")
)

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

// fakeClones lists fixed origins, owner/name, as the clones' would be.
type fakeClones []string

func (f fakeClones) RepoNames(context.Context) ([]string, error) {
	return f, nil
}

// failingClones cannot list the clones.
type failingClones struct{}

func (failingClones) RepoNames(context.Context) ([]string, error) {
	return nil, errFakeClones
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
	require.NoError(t, known.Sync(t.Context(), []profiles.Profile{
		{Agent: agent.Claude, Name: "work", ConfigDir: "/tmp/work"},
		{Agent: agent.Codex, Name: profileAlt, ConfigDir: "/tmp/alt"},
	}))

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

// withSources returns a Service over the harness's stores that lists repos as the
// repositories the inbox has seen and clones as the clones', logging to logs.
func (h *harness) withSources(repos launchdefaults.RepoSource, clones launchdefaults.CloneSource,
	logs *testutil.Logs,
) *launchdefaults.Service {
	return launchdefaults.NewService(launchdefaults.Config{
		Store: h.store, Profiles: h.known, Prompts: h.library, Repos: repos, Clones: clones, Logger: logs.Logger(),
	})
}

// claude returns a per-agent setting that only Claude has.
func claude(v string) map[agent.ID]string {
	return map[agent.ID]string{agent.Claude: v}
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
	t.Parallel()

	h := newHarness(t)
	ctx := t.Context()

	org, err := h.svc.Save(ctx, launchdefaults.Input{
		Owner: " acme-org ", Profiles: claude("work"), PRPromptID: &h.review, ExtraFlags: claude("--advisor fable"),
	})
	require.NoError(t, err)
	assert.Equal(t, acme, org.Owner)
	assert.Empty(t, org.Name)
	assert.Equal(t, acme, org.Target())
	assert.Equal(t, claude("work"), org.Profiles)
	assert.Equal(t, map[agent.ID][]string{agent.Claude: {"--advisor", "fable"}}, org.ExtraArgs)
	assert.Equal(t, claude("--advisor fable"), org.ExtraFlags)
	assert.Equal(t, map[agent.ID]string{}, org.Models, "maps are never nil")
	assert.NotEmpty(t, org.UpdatedAt)

	repo, err := h.svc.Save(ctx, launchdefaults.Input{
		Owner: acme, Name: "example-repo", IssuePromptID: &h.work, Models: claude("sonnet"), Effort: "low", Worktree: new(false),
	})
	require.NoError(t, err)
	assert.Equal(t, "acme-org/example-repo", repo.Target())
	require.NotNil(t, repo.Worktree)
	assert.False(t, *repo.Worktree)
	assert.Nil(t, repo.PlanMode)
	assert.Empty(t, repo.ExtraArgs)
	assert.NotNil(t, repo.ExtraArgs)
	assert.Equal(t, claude("sonnet"), repo.Models)

	// Each agent has its own profile, model, and flags; the effort is shared.
	both, err := h.svc.Save(ctx, launchdefaults.Input{
		Owner: acme, Name: "both-agents", Agent: agent.Codex,
		Profiles:   map[agent.ID]string{agent.Claude: "work", agent.Codex: profileAlt},
		Models:     map[agent.ID]string{agent.Codex: "gpt-6-sol"},
		ExtraFlags: map[agent.ID]string{agent.Codex: "--search"},
		Effort:     "ultra",
	})
	require.NoError(t, err)
	assert.Equal(t, agent.Codex, both.Agent)
	assert.Equal(t, map[agent.ID]string{agent.Claude: "work", agent.Codex: profileAlt}, both.Profiles)
	assert.Equal(t, map[agent.ID]string{agent.Codex: "gpt-6-sol"}, both.Models)
	assert.Equal(t, map[agent.ID][]string{agent.Codex: {"--search"}}, both.ExtraArgs)
	assert.Equal(t, "ultra", both.Effort)
	require.NoError(t, h.svc.Remove(ctx, acme, "both-agents"))

	// Saving again replaces the row, matching the target whatever its case.
	updated, err := h.svc.Save(ctx, launchdefaults.Input{Owner: "ACME-ORG", Profiles: claude("work"), Effort: "max"})
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
	t.Parallel()

	h := newHarness(t)
	ctx := t.Context()

	_, err := h.svc.Save(ctx, launchdefaults.Input{Owner: acme, Models: claude("opus")})
	require.NoError(t, err)

	got, err := h.svc.Save(ctx, launchdefaults.Input{Owner: acme, ExtraFlags: claude("  ")})
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
	t.Parallel()

	h := newHarness(t)
	gone := new(int64(9999))

	lib, libErr := h.library.Library(t.Context())
	require.NoError(t, libErr)

	reply := new(lib.Defaults.Replies.Approve)

	tests := []struct {
		name  string
		in    launchdefaults.Input
		field string
		want  string
	}{
		{name: "no owner", in: launchdefaults.Input{Models: claude("opus")}, field: launchdefaults.FieldOwner, want: "Choose an org or user"},
		{name: "bad owner", in: launchdefaults.Input{Owner: "-acme", Models: claude("opus")}, field: launchdefaults.FieldOwner},
		{name: "bad name", in: launchdefaults.Input{Owner: acme, Name: "a b", Models: claude("opus")}, field: launchdefaults.FieldName},
		{name: "agent", in: launchdefaults.Input{Owner: acme, Agent: "example-agent"}, field: launchdefaults.FieldAgent, want: `There's no coding agent called "example-agent".`},
		{name: "profile", in: launchdefaults.Input{Owner: acme, Profiles: claude("nope")}, field: "profiles.claude", want: `There's no Claude profile called "nope".`},
		{name: "another agent's profile", in: launchdefaults.Input{Owner: acme, Profiles: map[agent.ID]string{agent.Codex: "work"}}, field: "profiles.codex", want: `There's no Codex profile called "work".`},
		{name: "unknown agent's profile", in: launchdefaults.Input{Owner: acme, Profiles: map[agent.ID]string{"example-agent": "work"}}, field: launchdefaults.FieldProfiles, want: `no coding agent called "example-agent"`},
		{name: "missing prompt", in: launchdefaults.Input{Owner: acme, PRPromptID: gone}, field: launchdefaults.FieldPRPromptID, want: "no longer exists"},
		{name: "issue prompt for PRs", in: launchdefaults.Input{Owner: acme, PRPromptID: &h.work}, field: launchdefaults.FieldPRPromptID, want: `"Work on it" isn't a prompt for pull requests.`},
		{name: "a review reply", in: launchdefaults.Input{Owner: acme, PRPromptID: reply}, field: launchdefaults.FieldPRPromptID, want: `"LGTM" is a review reply, not a prompt an agent can launch with.`},
		{name: "PR prompt for issues", in: launchdefaults.Input{Owner: acme, IssuePromptID: &h.review}, field: launchdefaults.FieldIssuePromptID, want: `"Review" isn't a prompt for issues.`},
		{name: "model", in: launchdefaults.Input{Owner: acme, Models: claude("-x")}, field: "models.claude"},
		{name: "codex model", in: launchdefaults.Input{Owner: acme, Models: map[agent.ID]string{agent.Codex: "a b"}}, field: "models.codex"},
		{name: "effort", in: launchdefaults.Input{Owner: acme, Effort: "huge"}, field: launchdefaults.FieldEffort},
		{name: "reserved flag", in: launchdefaults.Input{Owner: acme, ExtraFlags: claude("--dangerously-skip-permissions")}, field: "extraFlags.claude", want: "--dangerously-skip-permissions can't be added: permissions come from the profile"},
		{name: "codex reserved flag", in: launchdefaults.Input{Owner: acme, ExtraFlags: map[agent.ID]string{agent.Codex: "--yolo"}}, field: "extraFlags.codex", want: "--yolo can't be added"},
		{name: "open quote", in: launchdefaults.Input{Owner: acme, ExtraFlags: claude(`--x "y`)}, field: "extraFlags.claude", want: "never closed"},
		{name: "empty flag", in: launchdefaults.Input{Owner: acme, ExtraFlags: claude(`""`)}, field: "extraFlags.claude", want: "Remove the empty flag"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

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

	got, err := h.svc.Check(t.Context(), launchdefaults.Input{Owner: acme, Profiles: claude("work")})
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.NotNil(t, got, "the UI always gets a list")
}

func TestRemove(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ctx := t.Context()

	_, err := h.svc.Save(ctx, launchdefaults.Input{Owner: acme, Name: "example-repo", Models: claude("opus")})
	require.NoError(t, err)

	require.ErrorIs(t, h.svc.Remove(ctx, acme, ""), launchdefaults.ErrNotFound)
	require.NoError(t, h.svc.Remove(ctx, "Acme-Org", "Example-Repo"))

	all, err := h.svc.List(ctx)
	require.NoError(t, err)
	assert.Empty(t, all)
}

func TestDefaultPromptsCannotBeDeletedWhileUsed(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	ctx := t.Context()
	svc := h.prompts

	all, err := svc.List(ctx)
	require.NoError(t, err)

	var fix prompts.Prompt

	for _, p := range all {
		if p.StarterKey == prompts.StarterQuickLook {
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
	assert.Equal(t, `"Quick look" can't be deleted while it's a default for acme-org and acme-org/example-repo; `+
		"choose other defaults there first", err.Error())

	all, err = svc.List(ctx)
	require.NoError(t, err)

	fix.Usage = prompts.Usage{
		Defaults: []item.Kind{}, Slots: []prompts.DefaultSlot{}, Orgs: []string{acme},
		Repos: []string{"acme-org/example-repo"}, SlotPlaces: []prompts.SlotPlace{}, Kinds: []item.Kind{item.KindPR},
		Replies: []prompts.ReplySlot{}, Routines: []string{},
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
	_, err = h.svc.Save(ctx, launchdefaults.Input{Owner: beta, Effort: "high"})
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
		{Owner: acme, Name: apiRepo, Items: 2},
		{Owner: acme, Name: "web", Items: 1},
		{Owner: beta, Name: "tools"},
	}).Targets(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []launchdefaults.Target{
		{Owner: acme, Items: 3},
		{Owner: acme, Name: apiRepo, Items: 2},
		{Owner: acme, Name: "web", Items: 1},
		{Owner: beta, Items: 0},
		{Owner: beta, Name: "tools", Items: 0},
	}, got)
}

func TestTargetsGroupAnOwnerIgnoringCase(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	got, err := h.withRepos(fakeRepos{
		{Owner: "Acme-Org", Name: apiRepo, Items: 2},
		{Owner: acme, Name: "web", Items: 1},
	}).Targets(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []launchdefaults.Target{
		{Owner: "Acme-Org", Items: 3},
		{Owner: "Acme-Org", Name: apiRepo, Items: 2},
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

func TestTargetsAddTheClonesRepositories(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	got, err := h.withSources(fakeRepos{
		{Owner: acme, Name: apiRepo, Items: 2},
		{Owner: "gamma-org", Name: "tools", Items: 1},
	}, fakeClones{"beta-org/example-repo", "acme-org/web", "not a name"}, &testutil.Logs{}).Targets(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []launchdefaults.Target{
		{Owner: acme, Items: 2, Cloned: true},
		{Owner: acme, Name: apiRepo, Items: 2},
		{Owner: acme, Name: "web", Cloned: true},
		{Owner: beta, Cloned: true},
		{Owner: beta, Name: "example-repo", Cloned: true},
		{Owner: "gamma-org", Items: 1},
		{Owner: "gamma-org", Name: "tools", Items: 1},
	}, got, "an org only a clone names gets its rows, in order among the inbox's, and a name that can't be one is left out")
}

func TestTargetsMarkAnInboxRepositoryCloned(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	got, err := h.withSources(fakeRepos{
		{Owner: "Acme-Org", Name: "Example-Repo", Items: 3},
		{Owner: "Delta-Org", Name: "web"},
	}, fakeClones{"acme-org/aaa-repo", "acme-org/example-repo", "delta-org/web"}, &testutil.Logs{}).Targets(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []launchdefaults.Target{
		{Owner: "Acme-Org", Items: 3, Cloned: true},
		{Owner: acme, Name: "aaa-repo", Cloned: true},
		{Owner: "Acme-Org", Name: "Example-Repo", Items: 3, Cloned: true},
		{Owner: "Delta-Org", Cloned: true},
		{Owner: "Delta-Org", Name: "web", Cloned: true},
	}, got, "a repository both have is listed once, as the inbox spells it, and so is its org")
}

func TestTargetsWithClonesThatCantBeListed(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	inbox := fakeRepos{{Owner: acme, Name: apiRepo, Items: 2}}

	logs := &testutil.Logs{}
	got, err := h.withSources(inbox, failingClones{}, logs).Targets(t.Context())
	require.NoError(t, err, "a location that can't be read never empties the list")
	assert.Equal(t, []launchdefaults.Target{
		{Owner: acme, Items: 2},
		{Owner: acme, Name: apiRepo, Items: 2},
	}, got)
	assert.Contains(t, logs.String(), "could not list the clones' repositories")
	assert.Contains(t, logs.String(), errFakeClones.Error())

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err = h.withSources(inbox, failingClones{}, &testutil.Logs{}).Targets(ctx)
	require.ErrorIs(t, err, errFakeClones, "a caller that has gone gets the error")
	require.ErrorContains(t, err, "list the clones' repositories")
}

func TestTargetsWithNoClones(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	inbox := fakeRepos{{Owner: acme, Name: apiRepo, Items: 2}}
	want := []launchdefaults.Target{
		{Owner: acme, Items: 2},
		{Owner: acme, Name: apiRepo, Items: 2},
	}

	got, err := h.withSources(inbox, nil, &testutil.Logs{}).Targets(t.Context())
	require.NoError(t, err)
	assert.Equal(t, want, got, "with no clone source, the inbox's alone")

	got, err = h.withSources(inbox, fakeClones{}, &testutil.Logs{}).Targets(t.Context())
	require.NoError(t, err)
	assert.Equal(t, want, got, "with no clones, the inbox's alone")

	got, err = h.withSources(fakeRepos{}, fakeClones{"beta-org/example-repo"}, &testutil.Logs{}).Targets(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []launchdefaults.Target{
		{Owner: beta, Cloned: true},
		{Owner: beta, Name: "example-repo", Cloned: true},
	}, got, "with an empty inbox, the clones' alone")
}

func TestIndex(t *testing.T) {
	t.Parallel()

	idx := launchdefaults.NewIndex([]launchdefaults.Defaults{
		{Owner: acme, Models: claude("opus")},
		{Owner: acme, Name: apiRepo, Effort: "high"},
	})

	org, own := idx.For(repo.Name{Owner: "ACME-ORG", Repo: "Api"})
	require.NotNil(t, org)
	require.NotNil(t, own)
	assert.Equal(t, "opus", org.Models[agent.Claude])
	assert.Equal(t, "high", own.Effort)

	org, own = idx.For(repo.Name{Owner: acme, Repo: "web"})
	require.NotNil(t, org)
	assert.Nil(t, own)

	org, own = idx.For(repo.Name{Owner: "other-org"})
	assert.Nil(t, org)
	assert.Nil(t, own)
}

func TestInputEmpty(t *testing.T) {
	t.Parallel()

	assert.True(t, launchdefaults.Input{Owner: acme, ExtraFlags: claude(" ")}.Empty())
	assert.True(t, launchdefaults.Input{Owner: acme, Models: claude(""), Profiles: map[agent.ID]string{}}.Empty())
	assert.False(t, launchdefaults.Input{Owner: acme, Worktree: new(true)}.Empty())
	assert.False(t, launchdefaults.Input{Owner: acme, Agent: "claude"}.Empty())
	assert.False(t, launchdefaults.Input{Owner: acme, Profiles: map[agent.ID]string{agent.Codex: profileAlt}}.Empty())
}
