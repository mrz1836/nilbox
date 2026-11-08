package agent

import (
	"errors"
	"strings"

	"github.com/mrz1836/nilbox/internal/uierr"
)

// Fields a FieldError from CheckOverrides names, matching the editors' JSON names.
const (
	FieldModel      = "model"
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

// CheckOverrides checks the launch settings a prompt or a launch default may set: the
// model, the effort level, and the extra flags typed on one line. It returns the extra
// flags as arguments, or nil with a FieldError when they cannot be read or used, and a
// FieldError for a model or effort level the agent would not accept.
func CheckOverrides(model, effort, flags string) ([]string, []FieldError) {
	var fields []FieldError

	if ValidateModel(model) != nil {
		fields = append(fields, FieldError{
			Field: FieldModel, Message: `A model is one word, such as "opus" or "sonnet", that doesn't start with "-".`,
		})
	}

	if ValidateEffort(effort) != nil {
		fields = append(fields, FieldError{Field: FieldEffort, Message: "Choose an effort level from the list."})
	}

	args, err := Split(flags)
	if err != nil {
		return nil, append(fields, FieldError{Field: FieldExtraFlags, Message: "A quote in the flags is never closed."})
	}

	if err = CheckExtra(args); err != nil {
		return nil, append(fields, FieldError{Field: FieldExtraFlags, Message: flagMessage(err)})
	}

	return args, fields
}

// flagMessage explains why extra flags were refused.
func flagMessage(err error) string {
	if errors.Is(err, ErrEmptyFlag) {
		return "Remove the empty flag ('')."
	}

	return uierr.Message(err)
}
