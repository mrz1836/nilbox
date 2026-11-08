package desktop

import (
	"encoding/json"

	"github.com/mrz1836/nilbox/internal/uierr"
)

// boundError is the cause the window gets with an error a bound method returned.
type boundError struct {
	// Message is what the user is told about the error.
	Message string `json:"message"`
}

// MarshalError encodes the cause the desktop runtime sends with an error a bound method
// returned: {"message": "..."}, holding what the user is told about it (see
// uierr.Message). The runtime still sends the error's full text as the failure's own
// message, for the developer console; the window shows this one. It returns nil, which
// keeps the runtime's own encoding, only if the cause cannot be encoded.
func MarshalError(err error) []byte {
	data, jsonErr := json.Marshal(boundError{Message: uierr.Message(err)})
	if jsonErr != nil {
		return nil
	}

	return data
}
