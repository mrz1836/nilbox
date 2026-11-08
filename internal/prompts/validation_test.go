package prompts

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
)

func TestValidationErrorListsEveryProblemInOrder(t *testing.T) {
	t.Parallel()

	s, _ := empty(t, nil)

	_, err := s.Create(t.Context(), Input{Model: "-x", Effort: "huge", ExtraFlags: "--model opus"})

	var verr *agent.ValidationError
	require.ErrorAs(t, err, &verr)
	require.ErrorIs(t, err, ErrInvalidPrompt)
	assert.Equal(t, []agent.FieldError{
		{Field: FieldName, Message: "Give the prompt a title."},
		{Field: FieldNickname, Message: "Give the prompt a nickname for the launch menu."},
		{Field: FieldAppliesTo, Message: "Choose what the prompt is for: pull requests, issues, or both."},
		{Field: FieldModel, Message: `A model is one word, such as "opus" or "sonnet", that doesn't start with "-".`},
		{Field: FieldEffort, Message: "Choose an effort level from the list."},
		{Field: FieldExtraFlags, Message: "--model is set by the Model setting."},
		{Field: FieldBody, Message: "Fix the problems in the prompt's text."},
	}, verr.Fields)
	assert.Equal(t, "the prompt can't be saved: Give the prompt a title. "+
		"Give the prompt a nickname for the launch menu. "+
		"Choose what the prompt is for: pull requests, issues, or both. "+
		`A model is one word, such as "opus" or "sonnet", that doesn't start with "-". `+
		"Choose an effort level from the list. "+
		"--model is set by the Model setting. "+
		"Fix the problems in the prompt's text.", err.Error())

	assert.Equal(t, "the prompt can't be saved: ", (&agent.ValidationError{Err: ErrInvalidPrompt}).Error())
}

func TestCheckExplainsLongAndMultiLineNames(t *testing.T) {
	t.Parallel()

	s, _ := empty(t, nil)

	in := validInput()
	in.Name, in.Nickname = strings.Repeat("a", maxNameRunes+1), "two\nlines"

	got, err := s.Check(t.Context(), CheckRequest{Input: in})
	require.NoError(t, err)
	assert.Equal(t, []agent.FieldError{
		{Field: FieldName, Message: "Keep the title to 80 characters."},
		{Field: FieldNickname, Message: "Keep the nickname on one line."},
	}, got.Fields)
}

func TestResetRefusesToDropAKindThePromptIsADefaultFor(t *testing.T) {
	t.Parallel()

	s, _ := seeded(t, nil)
	ctx := t.Context()
	review := byNickname(t, s, "Review")

	// Made to apply to both kinds and the default for issues, the review starter can no
	// longer go back to applying to pull requests only.
	in := validInput()
	in.Nickname, in.AppliesTo = review.Nickname, AppliesToAny
	_, err := s.Update(ctx, review.ID, in)
	require.NoError(t, err)
	require.NoError(t, s.SetDefault(ctx, item.KindIssue, review.ID))

	_, err = s.Reset(ctx, review.ID)
	fields := fieldErrors(t, err)
	assert.Equal(t, map[string]string{
		FieldAppliesTo: "It's a default for issues (the default for pull requests, and the default for issues), " +
			"so it has to apply to them.",
	}, fields)
}

func TestCheckPreviewsAnIssuePromptWithTheIssueSample(t *testing.T) {
	t.Parallel()

	s, _ := empty(t, nil)

	in := validInput()
	in.AppliesTo, in.Body = AppliesToIssue, "Fix {{.Title}}"

	got, err := s.Check(t.Context(), CheckRequest{Input: in})
	require.NoError(t, err)
	assert.True(t, got.Sample)
	assert.Equal(t, "Fix "+Sample(item.KindIssue).Title+"\n", got.Preview)
}

// emptyTitleSource serves one pull request whose title is empty.
type emptyTitleSource struct{}

func (emptyTitleSource) PreviewData(context.Context, int64) (Data, error) {
	d := Sample(item.KindPR)
	d.Title = ""

	return d, nil
}

func TestCheckExplainsAPromptThatIsEmptyForTheItem(t *testing.T) {
	t.Parallel()

	s, _ := empty(t, emptyTitleSource{})

	in := validInput()
	in.Body = "{{.Title}}"

	got, err := s.Check(t.Context(), CheckRequest{Input: in, ItemID: 1})
	require.NoError(t, err)
	assert.Empty(t, got.Preview)
	assert.Equal(t, "For this item the prompt is empty.", got.PreviewError)
	assert.False(t, got.Sample, "the item itself was used")
}

func TestRenderRefusesAnEmptyPrompt(t *testing.T) {
	t.Parallel()

	tmpl, _ := Parse("{{.Title}}", AppliesToPR)
	require.NotNil(t, tmpl)

	d := Sample(item.KindPR)
	d.Title = ""

	_, err := tmpl.Render(d)
	require.ErrorIs(t, err, ErrEmptyPrompt)
}
