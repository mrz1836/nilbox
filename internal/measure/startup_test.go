package measure

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errLaunchFailed = errors.New("launch failed")

func TestMedian(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		values []int64
		want   int64
	}{
		{name: "empty", values: nil, want: 0},
		{name: "one", values: []int64{7}, want: 7},
		{name: "odd count, unsorted", values: []int64{900, 400, 600}, want: 600},
		{name: "even count takes the mean of the middle two", values: []int64{4, 1, 3, 2}, want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			original := append([]int64(nil), tt.values...)

			assert.Equal(t, tt.want, Median(tt.values))
			assert.Equal(t, original, tt.values, "the input is left unchanged")
		})
	}

	assert.Equal(t, 2*time.Second, Median([]time.Duration{time.Second, 3 * time.Second, 2 * time.Second}))
}

func TestParseFirstPaint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line string
		want Startup
		ok   bool
	}{
		{line: "startup.first_paint_ms=412 source=os", want: Startup{FirstPaintMS: 412, Source: "os"}, ok: true},
		{line: "  startup.first_paint_ms=90  source=runtime ", want: Startup{FirstPaintMS: 90, Source: "runtime"}, ok: true},
		{line: "startup.first_paint_ms=5", want: Startup{FirstPaintMS: 5}, ok: true},
		{line: "INFO startup timing startup.first_paint_ms=412 source=os"},
		{line: "startup.first_paint_ms=-1 source=os"},
		{line: "startup.first_paint_ms=abc"},
		{line: ""},
	}

	for _, tt := range tests {
		t.Run(tt.line, func(t *testing.T) {
			t.Parallel()

			got, ok := ParseFirstPaint(tt.line)

			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

// fakeApp writes an executable shell script standing in for the desktop binary.
func fakeApp(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "app")
	require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o700)) //nolint:gosec // a test executable

	return path
}

func TestLaunchApp(t *testing.T) {
	t.Parallel()

	t.Run("reads the timing line", func(t *testing.T) {
		t.Parallel()

		exe := fakeApp(t, `test "$1" = "--measure-startup" || exit 9
echo "starting"
echo "startup.first_paint_ms=640 source=os"
echo "startup.first_paint_ms=999 source=os"
`)

		got, err := LaunchApp(t.Context(), exe)
		require.NoError(t, err)

		assert.Equal(t, int64(640), got.FirstPaintMS, "the first timing line counts")
		assert.Equal(t, "os", got.Source)
		assert.Positive(t, got.Wall)
	})

	t.Run("no timing line", func(t *testing.T) {
		t.Parallel()

		exe := fakeApp(t, "echo starting\necho 'the window did not paint' >&2\n")

		_, err := LaunchApp(t.Context(), exe)
		require.ErrorIs(t, err, ErrNoTimingLine)
		assert.Contains(t, err.Error(), "the window did not paint")
	})

	t.Run("app fails", func(t *testing.T) {
		t.Parallel()

		exe := fakeApp(t, "echo 'startup.first_paint_ms=1 source=os'\necho boom >&2\nexit 3\n")

		_, err := LaunchApp(t.Context(), exe)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "boom")
	})

	t.Run("missing binary", func(t *testing.T) {
		t.Parallel()

		_, err := LaunchApp(t.Context(), filepath.Join(t.TempDir(), "missing"))
		require.Error(t, err)
	})
}

func TestTimeStartups(t *testing.T) {
	t.Parallel()

	readings := []int64{1700, 950, 900, 1000, 920, 940}
	calls := 0

	launch := func(context.Context) (Startup, error) {
		s := Startup{FirstPaintMS: readings[calls], Source: "os", Wall: time.Duration(readings[calls]+50) * time.Millisecond}
		calls++

		return s, nil
	}

	report, err := TimeStartups(t.Context(), len(readings), launch)
	require.NoError(t, err)

	assert.Len(t, report.Runs, 6)
	assert.Equal(t, int64(1700), report.First().FirstPaintMS)
	assert.Equal(t, int64(940), report.MedianMS(), "the median of launches 2-6")
	assert.Equal(t, 990*time.Millisecond, report.MedianWall())
	assert.Equal(t, []string{
		"startup.run=1 startup.first_paint_ms=1700 wall_ms=1750 source=os",
		"startup.run=2 startup.first_paint_ms=950 wall_ms=1000 source=os",
		"startup.run=3 startup.first_paint_ms=900 wall_ms=950 source=os",
		"startup.run=4 startup.first_paint_ms=1000 wall_ms=1050 source=os",
		"startup.run=5 startup.first_paint_ms=920 wall_ms=970 source=os",
		"startup.run=6 startup.first_paint_ms=940 wall_ms=990 source=os",
		"startup.first_launch_ms=1700",
		"startup.median_ms=940 (launches 2-6)",
		"startup.median_wall_ms=990",
	}, report.Lines())
}

func TestTimeStartupsErrors(t *testing.T) {
	t.Parallel()

	_, err := TimeStartups(t.Context(), 1, nil)
	require.ErrorIs(t, err, ErrTooFewRuns)

	calls := 0
	report, err := TimeStartups(t.Context(), 3, func(context.Context) (Startup, error) {
		calls++
		if calls == 2 {
			return Startup{}, errLaunchFailed
		}

		return Startup{FirstPaintMS: 800}, nil
	})
	require.ErrorIs(t, err, errLaunchFailed)
	assert.Contains(t, err.Error(), "launch 2 of 3")
	assert.Len(t, report.Runs, 1, "the launches before the failure are kept")
}

func TestStartupReportEmpty(t *testing.T) {
	t.Parallel()

	var report StartupReport

	assert.Equal(t, Startup{}, report.First())
	assert.Zero(t, report.MedianMS())
	assert.Zero(t, report.MedianWall())
}
