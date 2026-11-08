package uierr_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/uierr"
)

var (
	errDial     = errors.New("dial tcp: lookup api.example.com: no such host")
	errEmpty    = errors.New("")
	errSentence = errors.New(" GitHub said no. ")
	errSecret   = errors.New("upstream said: secret-value")
)

// hidden wraps an error without exposing it through Unwrap, only through As, the way
// an error that rewrites its text does.
type hidden struct{ err error }

func (h hidden) Error() string { return "[hidden]" }

func (h hidden) As(target any) bool { return errors.As(h.err, target) }

func TestNewKeepsTheChainForTheLogs(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("refresh the inbox: %w", uierr.New("Could not reach GitHub.", fmt.Errorf("call the API: %w", errDial)))

	assert.Equal(t, "refresh the inbox: call the API: "+errDial.Error(), err.Error())
	require.ErrorIs(t, err, errDial)
	assert.Equal(t, "Could not reach GitHub.", uierr.Message(err))

	var ui *uierr.Error
	require.ErrorAs(t, err, &ui)
	assert.Equal(t, "call the API: "+errDial.Error(), ui.Error())
}

func TestNewWithoutACause(t *testing.T) {
	t.Parallel()

	err := uierr.New("Pick a terminal first.", nil)

	assert.Equal(t, "Pick a terminal first.", err.Error())
	assert.Equal(t, "Pick a terminal first.", uierr.Message(err))
	assert.NoError(t, errors.Unwrap(err))
}

func TestMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "nil", err: nil, want: ""},
		{name: "plain error", err: errDial, want: "Dial tcp: lookup api.example.com: no such host."},
		{name: "already a sentence", err: errSentence, want: "GitHub said no."},
		{
			name: "wrapped plain error", err: fmt.Errorf("read the details: %w", errDial),
			want: "Read the details: dial tcp: lookup api.example.com: no such host.",
		},
		{name: "no text", err: errEmpty, want: "Something went wrong."},
		{name: "outermost message wins", err: uierr.New("Outer.", uierr.New("Inner.", errDial)), want: "Outer."},
		{
			name: "an empty message counts as none", err: uierr.New("", errDial),
			want: "Dial tcp: lookup api.example.com: no such host.",
		},
		{name: "joined", err: errors.Join(errDial, uierr.New("Could not save.", errDial)), want: "Could not save."},
		{name: "found through As", err: hidden{err: uierr.New("Kept.", errDial)}, want: "Kept."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, uierr.Message(tt.err))
		})
	}
}

func TestMessageNeverReadsTheHiddenText(t *testing.T) {
	t.Parallel()

	// An error that rewrites its text, such as one that hides a secret, is read by its
	// own Error method, never by the text of what it wraps.
	err := hidden{err: errSecret}

	assert.Equal(t, "[hidden].", uierr.Message(err))
}
