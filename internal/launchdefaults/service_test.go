package launchdefaults_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/launchdefaults"
	"github.com/mrz1836/nilbox/internal/profiles"
	"github.com/mrz1836/nilbox/internal/prompts"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestValidationErrorListsEveryProblemInOrder(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	gone := new(int64(9999))

	_, err := h.svc.Save(t.Context(), launchdefaults.Input{
		Name: "a b", Agent: "example-agent", Profiles: claude("nope"), PRPromptID: gone, IssuePromptID: &h.review,
		Models: claude("-x"), Effort: "huge", ExtraFlags: claude(`--x "y`),
	})
	require.ErrorIs(t, err, launchdefaults.ErrInvalidDefaults)

	var invalid *agent.ValidationError
	require.ErrorAs(t, err, &invalid)

	want := []agent.FieldError{
		{Field: launchdefaults.FieldOwner, Message: "Choose an org or user, such as acme-org."},
		{Field: launchdefaults.FieldName, Message: "A repository name holds only letters, digits, and . _ -, can't start with -, and can't be . or .. alone."},
		{Field: launchdefaults.FieldAgent, Message: `There's no coding agent called "example-agent".`},
		{Field: "profiles.claude", Message: `There's no Claude profile called "nope".`},
		{Field: launchdefaults.FieldPRPromptID, Message: "That prompt no longer exists; choose another."},
		{Field: launchdefaults.FieldIssuePromptID, Message: `"Review" isn't a prompt for issues.`},
		{Field: "models.claude", Message: `A model is one word, such as "opus" or "sonnet", that doesn't start with "-".`},
		{Field: launchdefaults.FieldEffort, Message: "Choose an effort level from the list."},
		{Field: "extraFlags.claude", Message: "A quote in the flags is never closed."},
	}
	assert.Equal(t, want, invalid.Fields)

	assert.Equal(t, "the launch defaults can't be saved: Choose an org or user, such as acme-org. "+
		"A repository name holds only letters, digits, and . _ -, can't start with -, and can't be . or .. alone. "+
		`There's no coding agent called "example-agent". There's no Claude profile called "nope". `+
		`That prompt no longer exists; choose another. "Review" isn't a prompt for issues. `+
		`A model is one word, such as "opus" or "sonnet", that doesn't start with "-". `+
		"Choose an effort level from the list. A quote in the flags is never closed.", err.Error())

	checked, err := h.svc.Check(t.Context(), launchdefaults.Input{
		Name: "a b", Agent: "example-agent", Profiles: claude("nope"), PRPromptID: gone, IssuePromptID: &h.review,
		Models: claude("-x"), Effort: "huge", ExtraFlags: claude(`--x "y`),
	})
	require.NoError(t, err)
	assert.Equal(t, want, checked, "Check reports what Save refuses")
}

func TestExtraFlagMessages(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for flags, want := range map[string]string{
		"--model opus":     "--model is set by the Model setting.",
		"-w":               "-w is set by the Worktree setting.",
		"--effort=high":    "--effort is set by the Effort setting.",
		"--print":          "--print can't be added: sessions run in your terminal.",
		"-n session":       "-n can't be added: each session is named for you.",
		"--add-dir ~/docs": `~ is not expanded in "~/docs": write the full path.`,
		`--x ""`:           "Remove the empty flag ('').",
	} {
		got, err := h.svc.Check(t.Context(), launchdefaults.Input{Owner: acme, ExtraFlags: claude(flags)})
		require.NoError(t, err)
		assert.Equal(t, []agent.FieldError{{Field: "extraFlags.claude", Message: want}}, got, flags)
	}
}

func TestSaveRoundTripsEverySetting(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	saved, err := h.svc.Save(t.Context(), launchdefaults.Input{
		Owner: acme, Name: " example-repo ", Agent: " claude ", Profiles: claude(" work "), PRPromptID: &h.review,
		IssuePromptID: &h.work, Models: claude(" opus "), Effort: "high", PlanMode: new(true), Worktree: new(false),
		ExtraFlags: claude(`--append-system-prompt "be brief" --verbose`),
	})
	require.NoError(t, err)

	assert.Equal(t, acme, saved.Owner)
	assert.Equal(t, "example-repo", saved.Name, "text settings are trimmed")
	assert.Equal(t, agent.Claude, saved.Agent)
	assert.Equal(t, claude("work"), saved.Profiles)
	assert.Equal(t, &h.review, saved.PRPromptID)
	assert.Equal(t, &h.work, saved.IssuePromptID)
	assert.Equal(t, claude("opus"), saved.Models)
	assert.Equal(t, "high", saved.Effort)
	assert.Equal(t, new(true), saved.PlanMode)
	assert.Equal(t, new(false), saved.Worktree)
	assert.Equal(t, map[agent.ID][]string{agent.Claude: {"--append-system-prompt", "be brief", "--verbose"}}, saved.ExtraArgs)
	assert.Equal(t, claude("--append-system-prompt 'be brief' --verbose"), saved.ExtraFlags)
	assert.Regexp(t, `^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$`, saved.UpdatedAt, "stamped in UTC, to the second")

	all, err := h.svc.List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []launchdefaults.Defaults{saved}, all)
}

func TestSaveReplacesTheRowIgnoringCase(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	_, err := h.svc.Save(t.Context(), launchdefaults.Input{Owner: "Acme-Org", Name: "Example-Repo", Models: claude("opus")})
	require.NoError(t, err)

	got, err := h.svc.Save(t.Context(), launchdefaults.Input{Owner: acme, Name: "example-repo", Effort: "low"})
	require.NoError(t, err)

	assert.Equal(t, "Acme-Org", got.Owner, "the row keeps the casing it was first saved with")
	assert.Equal(t, "Example-Repo", got.Name)
	assert.Empty(t, got.Models, "a save replaces every setting")
	assert.Equal(t, "low", got.Effort)

	all, err := h.svc.List(t.Context())
	require.NoError(t, err)
	assert.Len(t, all, 1)
}

func TestListOrdersByOwnerThenRepositoryIgnoringCase(t *testing.T) {
	t.Parallel()

	h := newHarness(t)

	for _, target := range [][2]string{{"beta-org", ""}, {acme, "web"}, {"Acme-Org", ""}, {acme, "API"}} {
		_, err := h.svc.Save(t.Context(), launchdefaults.Input{Owner: target[0], Name: target[1], Models: claude("opus")})
		require.NoError(t, err)
	}

	all, err := h.svc.List(t.Context())
	require.NoError(t, err)

	targets := make([]string, 0, len(all))
	for _, d := range all {
		targets = append(targets, d.Target())
	}

	assert.Equal(t, []string{"Acme-Org", "acme-org/API", "acme-org/web", "beta-org"}, targets)
}

func TestServiceReportsAClosedDatabase(t *testing.T) {
	t.Parallel()

	db := testutil.ClosedDB(t)

	stored := launchdefaults.NewStore(db, nil)
	svc := launchdefaults.NewService(launchdefaults.Config{
		Store: stored, Profiles: profiles.NewStore(db, nil), Prompts: prompts.NewStore(db, nil), Repos: failingRepos{},
	})
	ctx := t.Context()

	_, err := svc.List(ctx)
	require.ErrorContains(t, err, "read the launch defaults")

	_, err = stored.All(ctx)
	require.ErrorContains(t, err, "read the launch defaults")

	_, err = stored.PromptUses(ctx)
	require.ErrorContains(t, err, "read the launch defaults")

	_, err = svc.Targets(ctx)
	require.ErrorContains(t, err, "list the repositories")
	require.ErrorIs(t, err, errFakeRepos)

	err = svc.Remove(ctx, acme, "")
	require.ErrorContains(t, err, "remove the launch defaults for acme-org")
	require.NotErrorIs(t, err, launchdefaults.ErrNotFound)

	_, err = svc.Save(ctx, launchdefaults.Input{Owner: acme, Name: "example-repo", Models: claude("opus")})
	require.ErrorContains(t, err, "save the launch defaults for acme-org/example-repo")

	_, err = svc.Save(ctx, launchdefaults.Input{Owner: acme})
	require.ErrorContains(t, err, "remove the launch defaults for acme-org", "clearing defaults removes the row")

	_, err = svc.Check(ctx, launchdefaults.Input{Owner: acme, Profiles: claude("work")})
	require.ErrorContains(t, err, "look up profile")

	_, err = svc.Save(ctx, launchdefaults.Input{Owner: acme, Profiles: claude("work")})
	require.ErrorContains(t, err, "look up profile")
	require.NotErrorIs(t, err, launchdefaults.ErrInvalidDefaults, "a check that could not run is not a validation failure")

	_, err = svc.Check(ctx, launchdefaults.Input{Owner: acme, PRPromptID: new(int64(1))})
	require.ErrorContains(t, err, "read the prompt")

	var invalid *agent.ValidationError
	require.NotErrorAs(t, err, &invalid)
}
