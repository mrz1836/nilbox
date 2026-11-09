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
			args: []string{flagAdvisor, "fable", "--add-dir", "/srv/a b"},
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

			args, fields := agent.CheckOverrides(agent.Claude, tt.model, tt.effort, tt.flags)
			assert.Equal(t, tt.args, args)
			assert.Equal(t, tt.fields, fields)
		})
	}
}

func TestCheckOverridesCodex(t *testing.T) {
	t.Parallel()

	args, fields := agent.CheckOverrides(agent.Codex, "gpt-6-sol", "ultra", "--search -c model_verbosity=low")
	assert.Equal(t, []string{"--search", "-c", "model_verbosity=low"}, args)
	assert.Empty(t, fields)

	args, fields = agent.CheckOverrides(agent.Codex, "-x", "huge", "-c model=o3")
	assert.Nil(t, args)
	assert.Equal(t, []agent.FieldError{
		{Field: agent.FieldModel, Message: `A model is one word, such as "gpt-6-sol", that doesn't start with "-".`},
		{Field: agent.FieldEffort, Message: "Choose an effort level from the list."},
		{Field: agent.FieldExtraFlags, Message: "-c model is set by the Model setting."},
	}, fields)

	_, fields = agent.CheckOverrides(unknownAgent, "", "", "")
	assert.Equal(t, []agent.FieldError{{Field: agent.FieldExtraFlags, Message: `There's no coding agent called "example-agent".`}}, fields)
}

func TestCheckSettings(t *testing.T) {
	t.Parallel()

	checked, fields := agent.CheckSettings(agent.Settings{
		Models:     map[agent.ID]string{agent.Claude: " opus ", agent.Codex: ""},
		Effort:     "ultra",
		ExtraFlags: map[agent.ID]string{agent.Claude: "--advisor fable", agent.Codex: "  "},
	})
	assert.Empty(t, fields)
	assert.Equal(t, map[agent.ID]string{agent.Claude: "opus"}, checked.Models, "blank models are left out")
	assert.Equal(t, map[agent.ID][]string{agent.Claude: {flagAdvisor, "fable"}}, checked.Args, "blank flags are left out")

	checked, fields = agent.CheckSettings(agent.Settings{})
	assert.Empty(t, fields)
	assert.NotNil(t, checked.Models)
	assert.NotNil(t, checked.Args)

	_, fields = agent.CheckSettings(agent.Settings{
		Models:     map[agent.ID]string{agent.Codex: "-x", unknownAgent: "m"},
		Effort:     "huge",
		ExtraFlags: map[agent.ID]string{agent.Claude: "--model opus", agent.Codex: "--sandbox read-only", unknownAgent: "--x"},
	})
	assert.Equal(t, []agent.FieldError{
		{Field: "models.codex", Message: `A model is one word, such as "gpt-6-sol", that doesn't start with "-".`},
		{Field: agent.FieldModels, Message: `There's no coding agent called "example-agent".`},
		{Field: agent.FieldEffort, Message: "Choose an effort level from the list."},
		{Field: "extraFlags.claude", Message: "--model is set by the Model setting."},
		{Field: "extraFlags.codex", Message: "--sandbox can't be added: permissions come from the profile and the outside-contributor rule."},
		{Field: agent.FieldExtraFlags, Message: `There's no coding agent called "example-agent".`},
	}, fields)

	assert.Equal(t, "models.codex", agent.FieldFor(agent.FieldModels, agent.Codex))
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
