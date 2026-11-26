package procs_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/procs"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// errPSFailed stands in for ps exiting with an error, as it does for a process that is gone.
var errPSFailed = errors.New("exit status 1")

func TestTTY(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		runner *testutil.Runner
		want   string
	}{
		{name: "macOS", runner: &testutil.Runner{Stdout: "ttys004\n"}, want: "/dev/ttys004"},
		{name: "Linux names its terminals pts/<n>", runner: &testutil.Runner{Stdout: "pts/3\n"}, want: "/dev/pts/3"},
		{name: "a whole device path", runner: &testutil.Runner{Stdout: "/dev/ttys001\n"}, want: "/dev/ttys001"},
		{name: "no terminal", runner: &testutil.Runner{Stdout: "??\n"}},
		{name: "nothing printed", runner: &testutil.Runner{Stdout: "\n"}},
		{name: "a line with spaces", runner: &testutil.Runner{Stdout: "ttys004 extra\n"}},
		{name: "ps fails, as for a process gone", runner: &testutil.Runner{Err: errPSFailed}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := procs.TTY(t.Context(), tt.runner, 7)
			if tt.want == "" {
				require.ErrorIs(t, err, procs.ErrNoTTY)
				assert.Empty(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, [][]string{{"/bin/ps", "-o", "tty=", "-p", "7"}}, tt.runner.Calls())
		})
	}
}
