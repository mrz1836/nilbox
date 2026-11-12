package prompts

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

var errFakeItemGone = errors.New("item not found")

// launchDefaults lists where the launch defaults name prompts by reading their table,
// as the launchdefaults package does for the app, which this package cannot import.
type launchDefaults struct {
	db *sql.DB
}

func (l launchDefaults) PromptUses(ctx context.Context) ([]Use, error) {
	rows, err := l.db.QueryContext(ctx, "SELECT owner, name, pr_prompt_id, issue_prompt_id FROM launch_defaults")
	if err != nil {
		return nil, err
	}

	defer func() { _ = rows.Close() }()

	var uses []Use

	for rows.Next() {
		var (
			target    repo.Name
			pr, issue *int64
		)

		if err = rows.Scan(&target.Owner, &target.Repo, &pr, &issue); err != nil {
			return nil, err
		}

		if pr != nil {
			uses = append(uses, Use{PromptID: *pr, Kind: item.KindPR, Target: target})
		}

		if issue != nil {
			uses = append(uses, Use{PromptID: *issue, Kind: item.KindIssue, Target: target})
		}
	}

	return uses, rows.Err()
}

// newService returns a Service over the pool db, reading usage from its launch defaults.
func newService(db *sql.DB, source PreviewSource) *Service {
	return NewService(Config{Store: NewStore(db, nil), Usage: launchDefaults{db: db}, Source: source})
}

// empty returns a Service over a fresh database with no prompts.
func empty(t *testing.T, source PreviewSource) (*Service, *sql.DB) {
	t.Helper()

	db := testutil.DB(t)

	return newService(db, source), db
}

// seeded returns a Service over a database holding the starter prompts.
func seeded(t *testing.T, source PreviewSource) (*Service, *sql.DB) {
	t.Helper()

	s, db := empty(t, source)
	require.NoError(t, Seed(t.Context(), s.store))

	return s, db
}

// closedService returns a Service over a database that is already closed, so every
// query fails.
func closedService(t *testing.T) *Service {
	t.Helper()

	return newService(testutil.ClosedDB(t), nil)
}

// byNickname finds a listed prompt by nickname.
func byNickname(t *testing.T, s *Service, nickname string) Prompt {
	t.Helper()

	all, err := s.List(t.Context())
	require.NoError(t, err)

	for _, p := range all {
		if p.Nickname == nickname {
			return p
		}
	}

	require.Failf(t, "no prompt", "nickname %q", nickname)

	return Prompt{}
}

// nicknames lists the listed prompts' nicknames.
func nicknames(t *testing.T, s *Service) []string {
	t.Helper()

	all, err := s.List(t.Context())
	require.NoError(t, err)

	out := make([]string, 0, len(all))
	for _, p := range all {
		out = append(out, p.Nickname)
	}

	return out
}

// lookAtURL is a prompt text that fills in cleanly.
const lookAtURL = "Look at {{.URL}}"

// validInput returns a prompt Input that saves cleanly.
func validInput() Input {
	return Input{
		Name: "Address review feedback", Nickname: "Feedback", AppliesTo: AppliesToPR,
		Body: "Address the review comments on {{.URL}}", Models: map[agent.ID]string{agent.Claude: "opus"},
		Effort: "high", ExtraFlags: map[agent.ID]string{agent.Claude: "--advisor fable"},
	}
}

// fieldErrors returns the fields a agent.ValidationError names, with their messages.
func fieldErrors(t *testing.T, err error) map[string]string {
	t.Helper()

	var verr *agent.ValidationError
	require.ErrorAs(t, err, &verr)
	require.ErrorIs(t, err, ErrInvalidPrompt)

	out := map[string]string{}
	for _, f := range verr.Fields {
		out[f.Field] = f.Message
	}

	return out
}

// addLaunchDefault stores an org's (name "") or a repository's default pull request
// prompt.
func addLaunchDefault(t *testing.T, db *sql.DB, owner, name string, pr int64) {
	t.Helper()

	testutil.Exec(t, db, `INSERT INTO launch_defaults (owner, name, pr_prompt_id, created_at, updated_at)
		VALUES (?, ?, ?, '2026-01-01 00:00:00', '2026-01-01 00:00:00')`, owner, name, pr)
}

func TestSeed(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()

	assert.Equal(t, []string{
		"Fix checks", "LGTM", "Needs changes", "Question", "Quick look", "Review", "Update OK", "Work on it",
	}, nicknames(t, s))

	review, work := byNickname(t, s, "Review"), byNickname(t, s, "Work on it")
	assert.Equal(t, StarterReviewPR, review.StarterKey)
	assert.False(t, review.Modified)
	assert.Equal(t, []item.Kind{item.KindPR}, review.Usage.Defaults)
	assert.Equal(t, []item.Kind{item.KindIssue}, work.Usage.Defaults)

	defaults, err := s.Defaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, DefaultPrompts{PR: review.ID, Issue: work.ID, Replies: ReplyDefaults{
		Approve: byNickname(t, s, "LGTM").ID, ApproveBots: byNickname(t, s, "Update OK").ID,
		RequestChanges: byNickname(t, s, "Needs changes").ID, Comment: byNickname(t, s, "Question").ID,
	}}, defaults)

	quick := byNickname(t, s, "Quick look")
	assert.Equal(t, map[agent.ID]string{agent.Claude: "sonnet"}, quick.Models)
	require.NotNil(t, quick.Worktree)
	assert.False(t, *quick.Worktree)
	assert.Empty(t, quick.Usage.Defaults)

	// Seeding again adds nothing, and a deleted starter stays deleted.
	require.NoError(t, Seed(ctx, s.store))
	require.NoError(t, s.Delete(ctx, quick.ID))
	require.NoError(t, Seed(ctx, s.store))
	assert.Equal(t, []string{
		"Fix checks", "LGTM", "Needs changes", "Question", "Review", "Update OK", "Work on it",
	}, nicknames(t, s))
}

func TestSeedFindsAFreeNickname(t *testing.T) {
	t.Parallel()

	s, _ := empty(t, nil)

	mine, err := s.Create(t.Context(), Input{Name: "Mine", Nickname: "review", AppliesTo: AppliesToAny, Body: lookAtURL})
	require.NoError(t, err)

	require.NoError(t, Seed(t.Context(), s.store))
	assert.Contains(t, nicknames(t, s), "Review 2")

	// A default was already missing, so the review starter became it, not the user's.
	defaults, err := s.Defaults(t.Context())
	require.NoError(t, err)
	assert.NotEqual(t, mine.ID, defaults.PR)
	assert.Equal(t, byNickname(t, s, "Review 2").ID, defaults.PR)
}

func TestCreateAndUpdate(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()

	in := validInput()
	in.PlanMode = new(true)
	in.Name, in.Nickname = "  "+in.Name+" ", " Feedback "
	in.Body = "Address the review comments on {{.URL}}\r\nThanks"

	created, err := s.Create(ctx, in)
	require.NoError(t, err)
	assert.Positive(t, created.ID)
	assert.Equal(t, "Address review feedback", created.Name, "trimmed")
	assert.Equal(t, "Feedback", created.Nickname)
	assert.Equal(t, "Address the review comments on {{.URL}}\nThanks", created.Body, "newlines normalized")
	assert.Equal(t, map[agent.ID][]string{agent.Claude: {"--advisor", "fable"}}, created.ExtraArgs)
	assert.Equal(t, map[agent.ID]string{agent.Claude: "--advisor fable"}, created.ExtraFlags)
	assert.Equal(t, map[agent.ID]string{agent.Claude: "opus"}, created.Models)
	require.NotNil(t, created.PlanMode)
	assert.True(t, *created.PlanMode)
	assert.Nil(t, created.Worktree)
	assert.Empty(t, created.StarterKey)
	assert.False(t, created.Modified)
	assert.False(t, created.Usage.InUse())
	assert.NotEmpty(t, created.UpdatedAt)

	in = validInput()
	in.Nickname, in.AppliesTo = "Feedback 2", AppliesToAny
	in.Models = map[agent.ID]string{agent.Claude: " ", agent.Codex: " gpt-6-sol "}
	in.ExtraFlags = map[agent.ID]string{agent.Codex: `--note 'two words'`, agent.Claude: "  "}
	in.Effort = "ultra"

	updated, err := s.Update(ctx, created.ID, in)
	require.NoError(t, err)
	assert.Equal(t, created.ID, updated.ID)
	assert.Equal(t, "Feedback 2", updated.Nickname)
	assert.Equal(t, AppliesToAny, updated.AppliesTo)
	assert.Equal(t, map[agent.ID]string{agent.Codex: "gpt-6-sol"}, updated.Models, "blank models inherit")
	assert.Equal(t, "ultra", updated.Effort, "an effort only some agents take is kept")
	assert.Nil(t, updated.PlanMode)
	assert.Equal(t, map[agent.ID][]string{agent.Codex: {"--note", "two words"}}, updated.ExtraArgs)
	assert.Equal(t, map[agent.ID]string{agent.Codex: `--note 'two words'`}, updated.ExtraFlags)

	_, err = s.Update(ctx, 9999, validInput())
	require.ErrorIs(t, err, ErrPromptNotFound)
}

func TestValidation(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)

	tests := []struct {
		name  string
		edit  func(in *Input)
		field string
		want  string
	}{
		{name: "no title", edit: func(in *Input) { in.Name = " " }, field: FieldName, want: "Give the prompt a title."},
		{name: "long title", edit: func(in *Input) { in.Name = string(make([]byte, 81)) }, field: FieldName},
		{name: "title on two lines", edit: func(in *Input) { in.Name = "a\nb" }, field: FieldName, want: "Keep the title on one line."},
		{name: "no nickname", edit: func(in *Input) { in.Nickname = "" }, field: FieldNickname, want: "Give the prompt a nickname for the launch menu."},
		{
			name: "long nickname", edit: func(in *Input) { in.Nickname = "A nickname that is too long" },
			field: FieldNickname, want: "Keep the nickname to 20 characters so it fits the launch button.",
		},
		{name: "taken nickname", edit: func(in *Input) { in.Nickname = "REVIEW" }, field: FieldNickname, want: `Another prompt is already called "REVIEW".`},
		{name: "kind", edit: func(in *Input) { in.AppliesTo = "discussion" }, field: FieldAppliesTo, want: "Choose what the prompt is for"},
		{name: "model", edit: func(in *Input) { in.Models = map[agent.ID]string{agent.Claude: "-x"} }, field: "models.claude", want: `doesn't start with "-"`},
		{name: "codex model", edit: func(in *Input) { in.Models = map[agent.ID]string{agent.Codex: "a b"} }, field: "models.codex", want: `"gpt-6-sol"`},
		{name: "unknown agent's model", edit: func(in *Input) { in.Models = map[agent.ID]string{"example-agent": "x"} }, field: FieldModels, want: `no coding agent called "example-agent"`},
		{name: "effort", edit: func(in *Input) { in.Effort = "huge" }, field: FieldEffort, want: "Choose an effort level from the list."},
		{name: "reserved flag", edit: func(in *Input) { in.ExtraFlags = map[agent.ID]string{agent.Claude: "--model opus"} }, field: "extraFlags.claude", want: "--model is set by the Model setting."},
		{name: "codex reserved flag", edit: func(in *Input) {
			in.ExtraFlags = map[agent.ID]string{agent.Codex: "-c sandbox_mode=danger-full-access"}
		}, field: "extraFlags.codex", want: "permissions come from"},
		{name: "open quote", edit: func(in *Input) { in.ExtraFlags = map[agent.ID]string{agent.Claude: "--x 'y"} }, field: "extraFlags.claude", want: "A quote in the flags is never closed."},
		{name: "empty flag", edit: func(in *Input) { in.ExtraFlags = map[agent.ID]string{agent.Claude: "''"} }, field: "extraFlags.claude", want: "Remove the empty flag"},
		{name: "tilde", edit: func(in *Input) { in.ExtraFlags = map[agent.ID]string{agent.Claude: "--add-dir ~/x"} }, field: "extraFlags.claude", want: "write the full path"},
		{name: "text", edit: func(in *Input) { in.Body = "{{.Nope}}" }, field: FieldBody, want: "Fix the problems in the prompt's text."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			in := validInput()
			tt.edit(&in)

			_, err := s.Create(t.Context(), in)
			fields := fieldErrors(t, err)
			require.Contains(t, fields, tt.field)
			assert.Contains(t, fields[tt.field], tt.want)
			assert.Len(t, fields, 1, "only the broken field is reported: %v", fields)
		})
	}
}

func TestUpdateKeepsTheKindsAPromptIsADefaultFor(t *testing.T) {
	t.Parallel()

	s, db := seeded(t, nil)
	ctx := t.Context()
	fix := byNickname(t, s, "Fix checks")

	addLaunchDefault(t, db, "acme-org", "", fix.ID)

	in := validInput()
	in.Nickname, in.AppliesTo = fix.Nickname, AppliesToIssue

	_, err := s.Update(ctx, fix.ID, in)
	fields := fieldErrors(t, err)
	assert.Equal(t, "It's a default for pull requests (a default for acme-org), so it has to apply to them.",
		fields[FieldAppliesTo])

	in.AppliesTo = AppliesToAny
	_, err = s.Update(ctx, fix.ID, in)
	require.NoError(t, err, "both kinds still covers pull requests")
}

func TestDeleteRefusesAPromptInUse(t *testing.T) {
	t.Parallel()

	s, db := seeded(t, nil)
	ctx := t.Context()

	review := byNickname(t, s, "Review")
	err := s.Delete(ctx, review.ID)
	require.ErrorIs(t, err, ErrPromptInUse)
	assert.Equal(t, `"Review" can't be deleted while it's the default for pull requests; choose other defaults there first`,
		err.Error())

	fix := byNickname(t, s, "Fix checks")
	addLaunchDefault(t, db, "acme-org", "", fix.ID)
	addLaunchDefault(t, db, "acme-org", "api", fix.ID)
	addLaunchDefault(t, db, "other-org", "web", fix.ID)

	fix = byNickname(t, s, "Fix checks")
	assert.Equal(t, Usage{
		Defaults: []item.Kind{}, Orgs: []string{"acme-org"}, Repos: []string{"acme-org/api", "other-org/web"},
		Kinds: []item.Kind{item.KindPR}, Replies: []ReplySlot{},
	}, fix.Usage)

	var inUse *InUseError

	err = s.Delete(ctx, fix.ID)
	require.ErrorAs(t, err, &inUse)
	assert.Equal(t, "Fix checks", inUse.Nickname)
	assert.Contains(t, err.Error(), "a default for acme-org and 2 repositories")

	testutil.Exec(t, db, "DELETE FROM launch_defaults")
	require.NoError(t, s.Delete(ctx, fix.ID))
	assert.NotContains(t, nicknames(t, s), "Fix checks")

	require.ErrorIs(t, s.Delete(ctx, fix.ID), ErrPromptNotFound)
}

func TestDuplicate(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()
	review := byNickname(t, s, "Review")

	first, err := s.Duplicate(ctx, review.ID)
	require.NoError(t, err)
	assert.Equal(t, "Review copy", first.Nickname)
	assert.Equal(t, "Review a pull request (copy)", first.Name)
	assert.Equal(t, review.Body, first.Body)
	assert.Empty(t, first.StarterKey, "a copy is the user's own")
	assert.False(t, first.Usage.InUse())

	second, err := s.Duplicate(ctx, review.ID)
	require.NoError(t, err)
	assert.Equal(t, "Review copy 2", second.Nickname)

	_, err = s.Duplicate(ctx, 9999)
	require.ErrorIs(t, err, ErrPromptNotFound)
}

func TestReset(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()
	review := byNickname(t, s, "Review")

	in := validInput()
	in.Name, in.Nickname, in.Body = "My review", "Deep review", "Review {{.URL}} deeply"

	edited, err := s.Update(ctx, review.ID, in)
	require.NoError(t, err)
	assert.True(t, edited.Modified)

	reset, err := s.Reset(ctx, review.ID)
	require.NoError(t, err)
	assert.False(t, reset.Modified)
	assert.Equal(t, "Review", reset.Nickname)
	assert.Equal(t, reviewPRBody, reset.Body)
	assert.Empty(t, reset.Models)
	assert.Empty(t, reset.ExtraArgs)

	// When another prompt took the starter's nickname, the reset keeps its own.
	_, err = s.Update(ctx, review.ID, Input{Name: "x", Nickname: "Mine", AppliesTo: AppliesToPR, Body: "Do {{.URL}}"})
	require.NoError(t, err)

	other := validInput()
	other.Nickname = "Review"
	_, err = s.Create(ctx, other)
	require.NoError(t, err)

	reset, err = s.Reset(ctx, review.ID)
	require.NoError(t, err)
	assert.Equal(t, "Mine", reset.Nickname)
	assert.False(t, reset.Modified)

	mine, err := s.Create(ctx, Input{Name: "x", Nickname: "x", AppliesTo: AppliesToPR, Body: "Do {{.URL}}"})
	require.NoError(t, err)

	_, err = s.Reset(ctx, mine.ID)
	require.ErrorIs(t, err, ErrNotStarter)
}

func TestSetDefault(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()
	fix, work := byNickname(t, s, "Fix checks"), byNickname(t, s, "Work on it")

	require.ErrorIs(t, s.SetDefault(ctx, item.KindIssue, fix.ID), ErrWrongKind)
	require.ErrorIs(t, s.SetDefault(ctx, "discussion", fix.ID), item.ErrInvalidKind)
	require.ErrorIs(t, s.SetDefault(ctx, item.KindPR, 9999), ErrPromptNotFound)

	require.NoError(t, s.SetDefault(ctx, item.KindPR, fix.ID))

	defaults, err := s.store.Defaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, fix.ID, defaults.PR)
	assert.Equal(t, work.ID, defaults.Issue)
	assert.Equal(t, fix.ID, defaults.For(item.KindPR))
	assert.Equal(t, work.ID, defaults.For(item.KindIssue))

	// The old default is free to delete now.
	require.NoError(t, s.Delete(ctx, byNickname(t, s, "Review").ID))
}

func TestDefaultsFallBack(t *testing.T) {
	t.Parallel()

	s, _ := empty(t, nil)
	ctx := t.Context()

	defaults, err := s.store.Defaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, DefaultPrompts{}, defaults, "no prompts, no defaults")

	require.NoError(t, Seed(ctx, s.store))
	require.NoError(t, s.store.prefs.Set(ctx, prefDefaultPR, "9999"))
	require.NoError(t, s.store.prefs.Set(ctx, prefDefaultIssue, "not a number"))

	defaults, err = s.store.Defaults(ctx)
	require.NoError(t, err)
	assert.Equal(t, byNickname(t, s, "Fix checks").ID, defaults.PR, "the first pull request prompt by nickname")
	assert.Equal(t, byNickname(t, s, "Work on it").ID, defaults.Issue)
}

func TestLookupAndAll(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()
	review := byNickname(t, s, "Review")

	got, err := s.store.Lookup(ctx, review.ID)
	require.NoError(t, err)
	assert.Equal(t, "Review", got.Nickname)

	_, err = s.store.Lookup(ctx, 9999)
	require.ErrorIs(t, err, ErrPromptNotFound)

	all, err := s.store.All(ctx)
	require.NoError(t, err)
	assert.Len(t, all, len(Starters()))

	tmpl, err := got.Template()
	require.NoError(t, err)

	out, err := tmpl.Execute(Sample(item.KindPR))
	require.NoError(t, err)
	assert.Contains(t, out, "Review pull request #7")

	_, err = Prompt{Nickname: "bad", Body: "{{.Nope}}", AppliesTo: AppliesToAny}.Template()
	require.ErrorIs(t, err, ErrInvalidTemplate)
	assert.Contains(t, err.Error(), ".Nope isn't a variable")
}

// fakeSource answers PreviewData from a map of item ID to data.
type fakeSource map[int64]Data

func (f fakeSource) PreviewData(_ context.Context, itemID int64) (Data, error) {
	d, ok := f[itemID]
	if !ok {
		return Data{}, errFakeItemGone
	}

	return d, nil
}

func TestCheck(t *testing.T) {
	t.Parallel()

	outside := Sample(item.KindPR)
	outside.Number, outside.Title, outside.OutsideContributor = 41, "Speed up the cache", true

	dash := Sample(item.KindPR)
	dash.Title = "--dangerously-skip-permissions"

	s, _ := seeded(t, fakeSource{1: Sample(item.KindPR), 2: outside, 3: Sample(item.KindIssue), 4: dash})
	ctx := t.Context()
	review := byNickname(t, s, "Review")

	in := validInput()
	in.Nickname, in.Body = review.Nickname, review.Body

	t.Run("a starter previews cleanly against an item", func(t *testing.T) {
		t.Parallel()

		got, err := s.Check(ctx, CheckRequest{ID: review.ID, Input: in, ItemID: 1, InWorktree: true, AtPRHead: true})
		require.NoError(t, err)
		assert.Empty(t, got.Fields, "its own nickname is not taken")
		assert.Empty(t, got.Diagnostics)
		assert.Contains(t, got.Preview, "This worktree is already at the pull request's head commit.")
		assert.Empty(t, got.Notice)
		assert.False(t, got.Sample)
		assert.Equal(t, "Add the widget cache", got.Values["Title"])
		assert.Equal(t, "pr-7", got.Values["Worktree"])
		assert.Equal(t, len([]rune(got.Preview)), got.Characters)
	})

	t.Run("a new prompt's nickname must be free", func(t *testing.T) {
		t.Parallel()

		got, err := s.Check(ctx, CheckRequest{Input: in})
		require.NoError(t, err)
		require.Len(t, got.Fields, 1)
		assert.Equal(t, FieldNickname, got.Fields[0].Field)
		assert.True(t, got.Sample)
		assert.NotEmpty(t, got.Preview, "field problems don't stop the preview")
	})

	t.Run("an outside contributor's item adds the notice", func(t *testing.T) {
		t.Parallel()

		got, err := s.Check(ctx, CheckRequest{ID: review.ID, Input: in, ItemID: 2})
		require.NoError(t, err)
		assert.Contains(t, got.Preview, "Speed up the cache")
		assert.Contains(t, got.Notice, "outside contributor")
		assert.Equal(t, len([]rune(got.Preview))+len([]rune(got.Notice))+2, got.Characters)
	})

	t.Run("an item the prompt is not for falls back to a sample", func(t *testing.T) {
		t.Parallel()

		got, err := s.Check(ctx, CheckRequest{ID: review.ID, Input: in, ItemID: 3})
		require.NoError(t, err)
		assert.True(t, got.Sample)
		assert.Equal(t, "This prompt isn't for issues, so the preview uses a sample.", got.PreviewNote)
		assert.Contains(t, got.Preview, "pull request #7")
	})

	t.Run("a missing item falls back to a sample", func(t *testing.T) {
		t.Parallel()

		got, err := s.Check(ctx, CheckRequest{ID: review.ID, Input: in, ItemID: 99})
		require.NoError(t, err)
		assert.True(t, got.Sample)
		assert.Contains(t, got.PreviewNote, "no longer in the inbox")
	})

	t.Run("text errors leave no preview", func(t *testing.T) {
		t.Parallel()

		broken := in
		broken.Body = "Review {{.Titel}}"

		got, err := s.Check(ctx, CheckRequest{ID: review.ID, Input: broken, ItemID: 1})
		require.NoError(t, err)
		require.Len(t, got.Diagnostics, 1)
		assert.Empty(t, got.Preview)
		assert.Empty(t, got.Fields, "the text's problems are diagnostics, not a field error")
		assert.NotEmpty(t, got.Values, "the cheat sheet still gets the item's values")
	})

	t.Run("an item that would start the prompt with a dash", func(t *testing.T) {
		t.Parallel()

		leading := in
		leading.Body = "{{.Title}} needs a review"

		got, err := s.Check(ctx, CheckRequest{ID: review.ID, Input: leading, ItemID: 4})
		require.NoError(t, err)
		assert.Empty(t, got.Preview)
		assert.Contains(t, got.PreviewError, `For this item the prompt starts with "-"`)
	})
}

func TestDescribeUsage(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "not used anywhere", describeUsage(Usage{}))
	assert.Equal(t, "the default for pull requests, and the default for issues",
		describeUsage(Usage{Defaults: []item.Kind{item.KindPR, item.KindIssue}}))
	assert.Equal(t, "a default for 2 orgs and acme-org/api",
		describeUsage(Usage{Orgs: []string{"a", "b"}, Repos: []string{"acme-org/api"}}))
}

func TestVariablesService(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	assert.Equal(t, Variables(), s.Variables())
}
