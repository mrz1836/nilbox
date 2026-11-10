package terminal

import (
	"context"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeDriver is a driver registered under id that opens nothing.
type fakeDriver struct {
	id ID
}

func (d *fakeDriver) ID() ID { return d.id }

func (*fakeDriver) Open(context.Context, string) error { return nil }

func TestUnsupportedOS(t *testing.T) {
	t.Parallel()

	r := NewRegistry()

	for _, id := range []ID{"iterm2", "terminal", ""} {
		d, err := r.Get(id)
		require.ErrorIs(t, err, ErrUnsupportedOS)
		assert.Nil(t, d)
		require.EqualError(t, err, "launching a terminal is not supported on this OS yet")
	}

	assert.Empty(t, r.IDs())
}

func TestRegistryGet(t *testing.T) {
	t.Parallel()

	iterm := &fakeDriver{id: "iterm2"}
	terminal := &fakeDriver{id: "terminal"}
	r := NewRegistry(terminal, iterm)

	d, err := r.Get("iterm2")
	require.NoError(t, err)
	assert.Same(t, iterm, d)

	d, err = r.Get("terminal")
	require.NoError(t, err)
	assert.Same(t, terminal, d)

	_, err = r.Get("kitty")
	require.ErrorIs(t, err, ErrUnknownTerminal)
	require.NotErrorIs(t, err, ErrUnsupportedOS)
	assert.Contains(t, err.Error(), "iterm2, terminal")

	assert.Equal(t, []ID{"iterm2", "terminal"}, r.IDs())
}

func TestRegistryLaterDriverWins(t *testing.T) {
	t.Parallel()

	first := &fakeDriver{id: "iterm2"}
	second := &fakeDriver{id: "iterm2"}

	d, err := NewRegistry(first, second).Get("iterm2")
	require.NoError(t, err)
	assert.Same(t, second, d)
}

func TestValidateScriptPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		ok   bool
	}{
		{name: "absolute", path: "/tmp/launch/pr-7.command", ok: true},
		{name: "awkward but safe", path: awkwardScriptPath, ok: true},
		{name: "unicode", path: "/tmp/café 🚀/a.command", ok: true},
		{name: "relative", path: "launch/pr-7.command"},
		{name: "empty", path: ""},
		{name: "newline", path: "/tmp/a\nrm -rf x.command"},
		{name: "carriage return", path: "/tmp/a\rb.command"},
		{name: "tab", path: "/tmp/a\tb.command"},
		{name: "nul", path: "/tmp/a\x00b.command"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := validateScriptPath(tt.path)
			if tt.ok {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, ErrInvalidScriptPath)
		})
	}
}

func TestOpenError(t *testing.T) {
	t.Parallel()

	exitErr := &exec.ExitError{}

	tests := []struct {
		name     string
		stderr   string
		denied   bool
		contains string
	}{
		{
			name:   "automation denied",
			stderr: "36:120: execution error: Not authorized to send Apple events to iTerm. (-1743)\n",
			denied: true,
		},
		{
			name:     "other failure",
			stderr:   "\n  execution error: iTerm got an error: something broke. (-2700)\nmore\n",
			contains: "iTerm2: execution error: iTerm got an error: something broke. (-2700)",
		},
		{name: "no output", stderr: "", contains: "could not open the terminal iTerm2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := openError(appITerm2, []byte(tt.stderr), exitErr)
			if tt.denied {
				require.ErrorIs(t, err, ErrAutomationDenied)
				assert.Contains(t, err.Error(), "Privacy & Security → Automation")
				assert.Contains(t, err.Error(), appITerm2)

				return
			}

			require.ErrorIs(t, err, ErrTerminalOpen)
			require.ErrorIs(t, err, exitErr)
			assert.Contains(t, err.Error(), tt.contains)
			assert.NotContains(t, err.Error(), "more")
		})
	}
}

func TestErrDetailCapsLength(t *testing.T) {
	t.Parallel()

	long := make([]rune, maxErrDetail+50)
	for i := range long {
		long[i] = 'x'
	}

	assert.Len(t, []rune(errDetail([]byte(string(long)))), maxErrDetail)
	assert.Empty(t, errDetail([]byte(" \n\t\n")))
}

func TestName(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "iTerm2", Name(ITerm2))
	assert.Equal(t, "Terminal", Name(TerminalApp))
	assert.Empty(t, Name("hyper"))
}
