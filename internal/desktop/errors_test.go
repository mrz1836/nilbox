package desktop_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/desktop"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// errDial is a failure with no message for the user.
var errDial = errors.New(`dial tcp: lookup "api.example.com": no such host`)

// detailed is a failure that carries what the window needs to answer it: detail, or a
// question with a command when detail is nil.
type detailed struct {
	err    error
	detail any
}

func (d detailed) Error() string { return d.err.Error() }

func (d detailed) Unwrap() error { return d.err }

func (d detailed) Detail() any {
	if d.detail != nil {
		return d.detail
	}

	return map[string]string{"command": "magex test"}
}

func TestMarshalErrorSendsWhatTheUserIsTold(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "a worded message",
			err:  fmt.Errorf("refresh the inbox: %w", uierr.New("Could not reach GitHub.", errDial)),
			want: `{"message":"Could not reach GitHub."}`,
		},
		{
			name: "the error's own text, as a sentence",
			err:  fmt.Errorf("refresh the inbox: %w", errDial),
			want: `{"message":"Refresh the inbox: dial tcp: lookup \"api.example.com\": no such host."}`,
		},
		{
			name: "what the window needs to answer it",
			err:  fmt.Errorf("start the run: %w", detailed{err: uierr.New("Confirm it first.", errDial)}),
			want: `{"message":"Confirm it first.","detail":{"command":"magex test"}}`,
		},
		{
			name: "a detail that can't be encoded",
			err:  detailed{err: errDial, detail: func() {}},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.want == "" {
				assert.Nil(t, desktop.MarshalError(tt.err), "the runtime's own encoding")

				return
			}

			assert.JSONEq(t, tt.want, string(desktop.MarshalError(tt.err)))
		})
	}
}
