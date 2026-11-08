package agent_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/agent"
)

var errFakeSave = errors.New("the thing can't be saved")

func TestCheckOverrides(t *testing.T) {
	t.Parallel()

	modelMsg := `A model is one word, such as "opus" or "sonnet", that doesn't start with "-".`
	effortMsg := "Choose an effort level from the list."

	tests := []struct {
		name   string
		model  string
		effort string
		flags  string
		args   []string
		fields []agent.FieldError
	}{
		{name: "all inherit", args: []string{}},
		{
			name: "valid", model: "opus", effort: "high", flags: `--advisor fable --add-dir '/srv/a b'`,
			args: []string{"--advisor", "fable", "--add-dir", "/srv/a b"},
		},
		{name: "bad model and effort", model: "-x", effort: "huge", args: []string{}, fields: []agent.FieldError{
			{Field: agent.FieldModel, Message: modelMsg},
			{Field: agent.FieldEffort, Message: effortMsg},
		}},
		{name: "unclosed quote", flags: `--x "y`, fields: []agent.FieldError{
			{Field: agent.FieldExtraFlags, Message: "A quote in the flags is never closed."},
		}},
		{name: "empty flag", flags: `--x ''`, fields: []agent.FieldError{
			{Field: agent.FieldExtraFlags, Message: "Remove the empty flag ('')."},
		}},
		{name: "reserved flag", effort: "huge", flags: "--model opus", fields: []agent.FieldError{
			{Field: agent.FieldEffort, Message: effortMsg},
			{Field: agent.FieldExtraFlags, Message: "--model is set by the Model setting."},
		}},
		{name: "tilde", flags: "--add-dir ~/docs", fields: []agent.FieldError{
			{Field: agent.FieldExtraFlags, Message: `~ is not expanded in "~/docs": write the full path.`},
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			args, fields := agent.CheckOverrides(tt.model, tt.effort, tt.flags)
			assert.Equal(t, tt.args, args)
			assert.Equal(t, tt.fields, fields)
		})
	}
}

func TestValidationError(t *testing.T) {
	t.Parallel()

	err := error(&agent.ValidationError{Err: errFakeSave, Fields: []agent.FieldError{
		{Field: "name", Message: "Give it a name."},
		{Field: agent.FieldModel, Message: "Choose a model."},
	}})

	assert.Equal(t, "the thing can't be saved: Give it a name. Choose a model.", err.Error())
	require.ErrorIs(t, err, errFakeSave)

	var verr *agent.ValidationError
	require.ErrorAs(t, err, &verr)
	assert.Len(t, verr.Fields, 2)

	assert.Equal(t, "the thing can't be saved: ", (&agent.ValidationError{Err: errFakeSave}).Error())
	assert.Equal(t, "can't be saved: Give it a name.",
		(&agent.ValidationError{Fields: []agent.FieldError{{Message: "Give it a name."}}}).Error())
}
