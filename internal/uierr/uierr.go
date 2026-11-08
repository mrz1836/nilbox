package uierr

import (
	"errors"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// fallback is the message for an error that has no text at all.
const fallback = "Something went wrong."

// Error is a failure with a message for the user. Its Error method returns the cause's
// full text, for the logs, and Unwrap returns the cause, so errors.Is and errors.As see
// through it.
type Error struct {
	msg   string
	cause error
}

// New returns an error whose user-facing message is msg, a complete sentence, and whose
// Error text and chain are cause's. A nil cause makes msg the Error text too.
func New(msg string, cause error) error {
	return &Error{msg: msg, cause: cause}
}

// Error returns the cause's text, or the message when there is no cause.
func (e *Error) Error() string {
	if e.cause == nil {
		return e.msg
	}

	return e.cause.Error()
}

// Unwrap returns the cause.
func (e *Error) Unwrap() error {
	return e.cause
}

// Message returns what the user is told about err: the message of the outermost Error in
// its chain, or else err's own text made into a sentence. An Error with an empty message
// counts as none, and a nil error has no message.
func Message(err error) string {
	if err == nil {
		return ""
	}

	if e, ok := errors.AsType[*Error](err); ok && e.msg != "" {
		return e.msg
	}

	if msg := textutil.Sentence(err.Error()); msg != "" {
		return msg
	}

	return fallback
}
