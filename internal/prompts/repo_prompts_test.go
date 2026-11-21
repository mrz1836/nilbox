package prompts

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

// repoInput returns a prompt for repositories that saves cleanly.
func repoInput(nickname string) Input {
	return Input{
		Name: nickname + " prompt", Nickname: nickname, AppliesTo: AppliesToRepo,
		Body: "Tidy {{.Repo}}{{if .InWorktree}} in {{.Worktree}}{{end}}.",
	}
}

func TestARepoPromptUsesTheRepositorysVariablesAlone(t *testing.T) {
	t.Parallel()

	body := "Tidy {{.Repo}} ({{.Owner}}/{{.RepoName}}) on {{.DefaultBranch}} from {{.Remote}}" +
		"{{if .RemoteIsPlain}}, a plain remote{{end}}{{if .InWorktree}} in {{.Worktree}}{{end}}, as {{.Profile}} for {{.Me}}."
	tmpl, diags := Parse(body, AppliesToRepo)
	require.NotNil(t, tmpl)
	assert.Empty(t, diags, "every repository and session variable is set for a repository")

	got := only(t, "Tidy {{.Repo}} for {{.Title}}", AppliesToRepo)
	assert.Equal(t, SeverityWarning, got.Severity)
	assert.Equal(t, ".Title is always empty here: it's only set for pull requests and issues, and this prompt is for "+
		"repositories.", got.Message)
	assert.Equal(t, ".Title", underlined("Tidy {{.Repo}} for {{.Title}}", got))

	for _, name := range []string{"Branch", "WorktreeOnPRHead", "OutsideContributor", "Routine", "Unattended"} {
		d := only(t, "Tidy {{.Repo}}{{if ."+name+"}}!{{end}}", AppliesToRepo)
		assert.Equal(t, SeverityWarning, d.Severity, name)
		assert.Contains(t, d.Message, "this prompt is for repositories", name)
	}

	_, diags = Parse("Review {{.URL}} on {{.Branch}}", AppliesToPR)
	assert.Empty(t, diags, "an item's prompt warns of nothing it has")
}

func TestARepoPromptIsFilledInForARepositoryAlone(t *testing.T) {
	t.Parallel()

	got := only(t, "{{if .Remote}}Tidy {{.Repo}}{{end}}", AppliesToRepo)
	assert.Equal(t, Diagnostic{Severity: SeverityError, Message: "The prompt is empty once filled in for repositories."}, got,
		"a repository with no remote is one of the samples")

	// An item's sample would fail it, since its .IsPR holds; a repository's never does.
	tmpl, diags := Parse("Tidy {{.Repo}}{{if .IsPR}}{{len .Number}}{{end}}", AppliesToRepo)
	require.NotNil(t, tmpl, "%+v", diags)

	for _, d := range diags {
		assert.Equal(t, SeverityWarning, d.Severity, d.Message)
	}

	_, diags = Parse("Tidy {{.Repo}}{{if .IsPR}}{{len .Number}}{{end}}", AppliesToPR)
	assert.NotEmpty(t, diags, "the same text fails for a pull request")

	tmpl = mustParse(t, "Tidy {{.Repo}}{{if .InWorktree}} in {{.Worktree}}{{end}}.", AppliesToRepo)

	d := RepoData(repo.Name{Owner: "acme-org", Repo: "example-repo"}, "main", "origin")

	out, err := tmpl.Render(d.WithWorktree("deps-20261121"))
	require.NoError(t, err)
	assert.Equal(t, "Tidy acme-org/example-repo in deps-20261121.\n", out, "a repository's launch adds no notice")
}

func TestARepoPromptSavesAsAnAgentPromptThatIsNoDefault(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()

	created, err := s.Create(ctx, repoInput("Tidy"))
	require.NoError(t, err)
	assert.Equal(t, AppliesToRepo, created.AppliesTo)
	assert.True(t, created.IsRepo())

	require.ErrorIs(t, s.SetDefault(ctx, item.KindPR, created.ID), ErrWrongKind)
	require.ErrorIs(t, s.SetDefault(ctx, item.KindIssue, created.ID), ErrWrongKind)
	require.ErrorIs(t, s.SetSlotDefault(ctx, DefaultSlotChecksFailing, created.ID), ErrWrongKind)

	reply := replyInput("Thanks", ReplyComment, "Thanks!")
	reply.AppliesTo = AppliesToRepo

	_, err = s.Create(ctx, reply)
	assert.Equal(t, map[string]string{FieldAppliesTo: "Choose what the reply is for: pull requests, issues, or both."},
		fieldErrors(t, err), "a reply is never for repositories")

	review := byNickname(t, s, reviewNick)
	in := repoInput(review.Nickname)

	_, err = s.Update(ctx, review.ID, in)
	assert.Equal(t, "It's a default for pull requests (the default for pull requests), so it has to apply to them.",
		fieldErrors(t, err)[FieldAppliesTo])
}

func TestTheDefaultsNeverFallBackToARepoPrompt(t *testing.T) {
	t.Parallel()

	s, _ := empty(t, nil)

	_, err := s.Create(t.Context(), repoInput("Tidy"))
	require.NoError(t, err)

	defaults, err := s.Defaults(t.Context())
	require.NoError(t, err)
	assert.Equal(t, DefaultPrompts{}, defaults, "a prompt for repositories is no item's default")
}

func TestARoutinesPromptCantBecomeARepoPrompt(t *testing.T) {
	t.Parallel()

	s := withRoutines(t, routineUses{
		{Routine: "Fix my checks", Starter: "fix-checks"}, {Routine: "Tidy nightly", Nickname: "Upgrade deps"},
	}, nil)
	ctx := t.Context()
	require.NoError(t, s.SetSlotDefault(ctx, DefaultSlotChecksFailing, 0), "only the routine uses it")

	fix := byNickname(t, s, "Fix checks")
	in := repoInput(fix.Nickname)

	_, err := s.Update(ctx, fix.ID, in)
	want := "It's used by the routine “Fix my checks”, and a routine runs its prompts on pull requests and issues, so " +
		"it can't be for repositories."
	assert.Equal(t, map[string]string{FieldAppliesTo: want}, fieldErrors(t, err))

	checked, err := s.Check(ctx, CheckRequest{ID: fix.ID, Input: in})
	require.NoError(t, err)
	assert.Equal(t, []agent.FieldError{{Field: FieldAppliesTo, Message: want}}, checked.Fields, "the editor says so too")

	// A prompt no routine runs switches, and one already for repositories stays free to
	// change, though a routine names it: nothing switches.
	quick := byNickname(t, s, "Quick look")
	_, err = s.Update(ctx, quick.ID, repoInput(quick.Nickname))
	require.NoError(t, err)

	deps := byNickname(t, s, "Upgrade deps")
	require.Equal(t, []string{"Tidy nightly"}, deps.Usage.Routines)

	edited := repoInput(deps.Nickname)
	_, err = s.Update(ctx, deps.ID, edited)
	require.NoError(t, err)

	_, err = s.Reset(ctx, deps.ID)
	require.NoError(t, err, "a starter for repositories resets, whatever a routine names")
}

func TestARepoPromptPreviewsWithTheSampleRepository(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, fakeSource{1: Sample(item.KindPR)})
	ctx := t.Context()

	got, err := s.Check(ctx, CheckRequest{Input: repoInput("Tidy"), InWorktree: true})
	require.NoError(t, err)
	assert.Empty(t, got.Fields)
	assert.Empty(t, got.Diagnostics)
	assert.Equal(t, "Tidy acme-org/example-repo in "+sampleRepoWorktree+".\n", got.Preview)
	assert.True(t, got.Sample)
	assert.Empty(t, got.PreviewNote)
	assert.Empty(t, got.Notice)
	assert.Equal(t, sampleRepoWorktree, got.Values["Worktree"])
	assert.Equal(t, "0", got.Values["Number"], "no item")

	got, err = s.Check(ctx, CheckRequest{Input: repoInput("Tidy"), ItemID: 1})
	require.NoError(t, err)
	assert.Equal(t, "Tidy acme-org/example-repo.\n", got.Preview, "in the clone")
	assert.True(t, got.Sample)
	assert.Equal(t, "This prompt is for repositories, so the preview uses a sample.", got.PreviewNote)

	reply := replyInput("Thanks", ReplyComment, "Thanks, @{{.Author}}!")
	reply.AppliesTo = AppliesToRepo

	got, err = s.Check(ctx, CheckRequest{Input: reply})
	require.NoError(t, err)
	assert.Equal(t, "Thanks, @octo-user!\n", got.Preview, "a reply said to be for repositories previews as one for both")
}

func TestARepoPromptImportsAsOne(t *testing.T) {
	t.Parallel()

	s, _ := empty(t, nil)
	ctx := t.Context()

	tidy := mine("Tidy")
	tidy.AppliesTo, tidy.Body = AppliesToRepo, "Tidy {{.Repo}}"
	thanks := replyOf("Thanks", ReplyComment, AppliesToRepo)
	unknown := mine("Odd")
	unknown.AppliesTo = "repository"

	res, err := s.Import(ctx, []BundlePrompt{tidy, thanks, unknown}, BundleDefaults{})
	require.NoError(t, err)
	assert.Equal(t, 3, res.Added)

	assert.Equal(t, AppliesToRepo, byNickname(t, s, "Tidy").AppliesTo)
	assert.Equal(t, AppliesToPR, byNickname(t, s, "Thanks").AppliesTo, "a reply is never for repositories")
	assert.Equal(t, AppliesToAny, byNickname(t, s, "Odd").AppliesTo, "what this build doesn't know still applies to both")

	assert.Equal(t, 4, BundleVersion, "a file that can hold prompts for repositories is one an older build refuses")

	for version := 1; version <= BundleVersion; version++ {
		b := bundleOf(mine("Old"))
		b.Version = version

		data, encodeErr := EncodeBundle(b)
		require.NoError(t, encodeErr)

		got, decodeErr := DecodeBundle(data)
		require.NoError(t, decodeErr, "a version %d file still reads", version)
		assert.Equal(t, AppliesToAny, got.Prompts[0].AppliesTo)
	}
}

func TestAnImportNeverSavesARepoPromptOverAStarterARoutineRuns(t *testing.T) {
	t.Parallel()

	s := withRoutines(t, routineUses{{Routine: "Fix my checks", Starter: "fix-checks"}}, nil)
	ctx := t.Context()
	require.NoError(t, s.SetSlotDefault(ctx, DefaultSlotChecksFailing, 0), "only the routine uses it")

	forRepos := starterAsBundled(t, StarterFixChecks, func(p *BundlePrompt) { p.AppliesTo = AppliesToRepo })
	edited := starterAsBundled(t, StarterReviewPR, func(p *BundlePrompt) { p.Body += "Be brief.\n" })

	pl, err := s.planner(ctx)
	require.NoError(t, err)

	got := pl.plan(forRepos)
	assert.Equal(t, ImportRename, got.Outcome, "the starter a routine runs stays one for pull requests")
	assert.False(t, got.keepStarter, "the library holds that starter already")

	assert.Equal(t, ImportReplace, pl.plan(edited).Outcome, "an edit of an untouched starter still saves over it")

	deps := starterAsBundled(t, StarterDepsMinor, func(p *BundlePrompt) { p.Body += "Be brief.\n" })
	assert.Equal(t, ImportReplace, pl.plan(deps).Outcome, "a starter for repositories takes its own copy's place")
}
