package desktop

import (
	"encoding/json"
	"errors"

	"github.com/mrz1836/nilbox/internal/uierr"
)

// boundError is the cause the window gets with an error a bound method returned.
type boundError struct {
	// Message is what the user is told about the error.
	Message string `json:"message"`

	// Detail is what the window needs to answer the error, from an error in its chain
	// that carries some (Detailer), such as the question a repository command waits on;
	// absent when none does.
	Detail any `json:"detail,omitempty"`
}

// Detailer is an error that carries what the window needs to answer it, which the
// desktop runtime sends beside its message.
type Detailer interface {
	error
	Detail() any
}

// MarshalError encodes the cause the desktop runtime sends with an error a bound method
// returned: {"message": "..."}, holding what the user is told about it (see
// uierr.Message), and "detail" when an error in its chain is a Detailer. The runtime still
// sends the error's full text as the failure's own message, for the developer console;
// the window shows this one. It returns nil, which keeps the runtime's own encoding, only
// if the cause cannot be encoded.
func MarshalError(err error) []byte {
	out := boundError{Message: uierr.Message(err)}
	if d, ok := errors.AsType[Detailer](err); ok {
		out.Detail = d.Detail()
	}

	data, jsonErr := json.Marshal(out)
	if jsonErr != nil {
		return nil
	}

	return data
}
