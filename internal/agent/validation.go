package agent

import (
	"errors"
	"strings"

	"github.com/mrz1836/nilbox/internal/uierr"
)

// Fields a FieldError from CheckOverrides and CheckSettings names, matching the editors'
// JSON names.
const (
	FieldModel      = "model"
	FieldModels     = "models"
	FieldEffort     = "effort"
	FieldExtraFlags = "extraFlags"
)

// FieldError is a problem with one field of something being saved.
type FieldError struct {
	// Field is the field's JSON name, such as "model".
	Field string `json:"field"`

	// Message is a sentence saying what is wrong and how to fix it.
	Message string `json:"message"`
}

// ValidationError lists why something cannot be saved, one FieldError per problem. It
// matches Err, the error naming what could not be saved, such as "the prompt can't be
// saved".
type ValidationError struct {
	Err    error
	Fields []FieldError
}

// Error reads like "<Err>: <each field's message>".
func (e *ValidationError) Error() string {
	messages := make([]string, 0, len(e.Fields))
	for _, f := range e.Fields {
		messages = append(messages, f.Message)
	}

	prefix := "can't be saved"
	if e.Err != nil {
		prefix = e.Err.Error()
	}

	return prefix + ": " + strings.Join(messages, " ")
}

// Unwrap returns Err, so errors.Is matches it.
func (e *ValidationError) Unwrap() error {
	return e.Err
}

// CheckOverrides checks the launch settings a prompt or a launch default may set for the
// agent with id: the model, the effort level, and the extra flags typed on one line. It
// returns the extra flags as arguments, or nil with a FieldError when they cannot be read
// or used, and a FieldError for a model or effort level the agent would not accept.
func CheckOverrides(id ID, model, effort, flags string) ([]string, []FieldError) {
	s, err := specFor(id)
	if err != nil {
		return nil, []FieldError{{Field: FieldExtraFlags, Message: unknownAgent(id)}}
	}

	var fields []FieldError

	if ValidateModel(model) != nil {
		fields = append(fields, FieldError{Field: FieldModel, Message: s.modelHint()})
	}

	if ValidateEffort(id, effort) != nil {
		fields = append(fields, FieldError{Field: FieldEffort, Message: effortHint})
	}

	args, flagErr := checkFlags(id, flags)
	if flagErr != "" {
		return nil, append(fields, FieldError{Field: FieldExtraFlags, Message: flagErr})
	}

	return args, fields
}

// Settings are the launch settings a prompt or a launch default stores: one effort level
// for every agent, and each agent's own model and extra flags. A setting that is "" (or
// an agent with no entry) inherits.
type Settings struct {
	// Models is each agent's model.
	Models map[ID]string

	// Effort is the effort level, shared by every agent; an agent that lacks it is
	// launched with its own default.
	Effort string

	// ExtraFlags is each agent's extra arguments typed on one line, quoted as in a shell.
	ExtraFlags map[ID]string
}

// Checked is Settings ready to store: the models that are set, trimmed, and the extra
// arguments of each agent that has any. Neither map is nil.
type Checked struct {
	Models map[ID]string
	Args   map[ID][]string
}

// FieldFor names one agent's entry of a per-agent field, as in "models.codex".
func FieldFor(field string, id ID) string {
	return field + "." + string(id)
}

// CheckSettings checks Settings the way the prompt and launch-default editors show them.
// It returns them ready to store, and a FieldError for each problem: an agent this build
// does not know (on FieldModels or FieldExtraFlags), a model the agent would misread or
// flags it cannot take (on FieldFor(FieldModels, id) or FieldFor(FieldExtraFlags, id)),
// and an effort level no agent accepts (on FieldEffort).
func CheckSettings(in Settings) (Checked, []FieldError) {
	out := Checked{Models: map[ID]string{}, Args: map[ID][]string{}}

	var fields []FieldError

	for _, id := range sortedIDs(in.Models) {
		model := strings.TrimSpace(in.Models[id])

		switch s, err := specFor(id); {
		case err != nil:
			fields = append(fields, FieldError{Field: FieldModels, Message: unknownAgent(id)})
		case ValidateModel(model) != nil:
			fields = append(fields, FieldError{Field: FieldFor(FieldModels, id), Message: s.modelHint()})
		case model != "":
			out.Models[id] = model
		}
	}

	if ValidateEffortAny(in.Effort) != nil {
		fields = append(fields, FieldError{Field: FieldEffort, Message: effortHint})
	}

	for _, id := range sortedIDs(in.ExtraFlags) {
		if !Known(id) {
			fields = append(fields, FieldError{Field: FieldExtraFlags, Message: unknownAgent(id)})

			continue
		}

		args, message := checkFlags(id, in.ExtraFlags[id])

		switch {
		case message != "":
			fields = append(fields, FieldError{Field: FieldFor(FieldExtraFlags, id), Message: message})
		case len(args) > 0:
			out.Args[id] = args
		}
	}

	return out, fields
}

// effortHint is the sentence the editors show for an effort level that is not offered.
const effortHint = "Choose an effort level from the list."

// checkFlags reads extra flags typed on one line for the agent with id, and returns the
// arguments, or why they cannot be used.
func checkFlags(id ID, flags string) ([]string, string) {
	args, err := Split(flags)
	if err != nil {
		return nil, "A quote in the flags is never closed."
	}

	if err = CheckExtra(id, args); err != nil {
		return nil, flagMessage(err)
	}

	return args, ""
}

// unknownAgent says that id names no agent.
func unknownAgent(id ID) string {
	return "There's no coding agent called \"" + string(id) + "\"."
}

// flagMessage explains why extra flags were refused.
func flagMessage(err error) string {
	if errors.Is(err, ErrEmptyFlag) {
		return "Remove the empty flag ('')."
	}

	return uierr.Message(err)
}
