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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.JSONEq(t, tt.want, string(desktop.MarshalError(tt.err)))
		})
	}
}
