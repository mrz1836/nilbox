package timing

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
)

// fixedStart is an arbitrary process start time for deterministic measurements.
func fixedStart() time.Time {
	return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
}

// textLogger returns a logger writing slog text records into buf.
func textLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, nil))
}

func TestProcessStart(t *testing.T) {
	t.Parallel()

	start, source := processStart()

	require.False(t, start.IsZero())
	assert.False(t, start.After(time.Now()), "the process cannot start in the future")

	if runtime.GOOS == "darwin" {
		assert.Equal(t, SourceOS, source)
		assert.False(t, start.After(runtimeStart), "the OS start precedes package init")
	} else {
		assert.Equal(t, SourceRuntime, source)
		assert.Equal(t, runtimeStart, start)
	}
}

func TestMeasurementString(t *testing.T) {
	t.Parallel()

	m := Measurement{FirstPaintMS: 412, Source: SourceOS}

	assert.Equal(t, "startup.first_paint_ms=412 source=os", m.String())
}

func TestFirstPaintLoggedOnce(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logs := t.TempDir()
	now := fixedStart().Add(412 * time.Millisecond)

	r := NewRecorder(Config{
		Logger:  textLogger(&buf),
		LogsDir: logs,
		Start:   fixedStart(),
		Source:  SourceOS,
		Now:     func() time.Time { return now },
	})

	m, first, err := r.FirstPaint()
	require.NoError(t, err)
	assert.True(t, first)
	assert.Equal(t, Measurement{FirstPaintMS: 412, Source: SourceOS}, m)

	// A later report, even a second later, changes nothing.
	now = now.Add(time.Second)

	again, first, err := r.FirstPaint()
	require.NoError(t, err)
	assert.False(t, first)
	assert.Equal(t, m, again)

	assert.Equal(t, 1, strings.Count(buf.String(), "startup.first_paint_ms=412 source=os"))

	data, err := os.ReadFile(filepath.Join(logs, LogFileName)) //nolint:gosec // test temp file
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	require.Len(t, lines, 1)
	assert.True(t, strings.HasSuffix(lines[0], " startup.first_paint_ms=412 source=os"), lines[0])

	info, err := os.Stat(filepath.Join(logs, LogFileName))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestFirstPaintAppendsToExistingLog(t *testing.T) {
	t.Parallel()

	logs := t.TempDir()
	path := filepath.Join(logs, LogFileName)
	require.NoError(t, os.WriteFile(path, []byte("earlier line\n"), appdata.PrivateFilePerm))

	r := NewRecorder(Config{
		Logger:  textLogger(&bytes.Buffer{}),
		LogsDir: logs,
		Start:   fixedStart(),
		Source:  SourceRuntime,
		Now:     func() time.Time { return fixedStart().Add(90 * time.Millisecond) },
	})

	_, _, err := r.FirstPaint()
	require.NoError(t, err)

	data, err := os.ReadFile(path) //nolint:gosec // test temp file
	require.NoError(t, err)
	assert.Contains(t, string(data), "earlier line\n")
	assert.Contains(t, string(data), "startup.first_paint_ms=90 source=runtime\n")
}

func TestFirstPaintWithoutLogsDirOnlyLogs(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	r := NewRecorder(Config{
		Logger: textLogger(&buf),
		Start:  fixedStart(),
		Source: SourceOS,
		Now:    func() time.Time { return fixedStart().Add(5 * time.Millisecond) },
	})

	m, first, err := r.FirstPaint()
	require.NoError(t, err)
	assert.True(t, first)
	assert.Equal(t, int64(5), m.FirstPaintMS)
	assert.Contains(t, buf.String(), "startup.first_paint_ms=5 source=os")
}

func TestFirstPaintNeverNegative(t *testing.T) {
	t.Parallel()

	r := NewRecorder(Config{
		Logger: textLogger(&bytes.Buffer{}),
		Start:  fixedStart(),
		Source: SourceOS,
		Now:    func() time.Time { return fixedStart().Add(-time.Second) },
	})

	m, _, err := r.FirstPaint()
	require.NoError(t, err)
	assert.Equal(t, int64(0), m.FirstPaintMS)
}

func TestFirstPaintReportsLogFileError(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	// The logs "directory" is a regular file, so the timing log cannot be created.
	notDir := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(notDir, nil, appdata.PrivateFilePerm))

	r := NewRecorder(Config{
		Logger:  textLogger(&buf),
		LogsDir: notDir,
		Start:   fixedStart(),
		Source:  SourceOS,
		Now:     func() time.Time { return fixedStart().Add(7 * time.Millisecond) },
	})

	m, first, err := r.FirstPaint()
	require.ErrorContains(t, err, "timing log")
	assert.True(t, first)
	assert.Equal(t, int64(7), m.FirstPaintMS)
	assert.Contains(t, buf.String(), "startup.first_paint_ms=7", "the record is logged even when the file fails")

	// The failure is reported once, with the first measurement.
	_, first, err = r.FirstPaint()
	require.NoError(t, err)
	assert.False(t, first)
}

func TestNewRecorderDefaults(t *testing.T) {
	t.Parallel()

	r := NewRecorder(Config{})

	assert.NotNil(t, r.logger)
	assert.NotNil(t, r.now)
	assert.False(t, r.start.IsZero())
	assert.Contains(t, []string{SourceOS, SourceRuntime}, r.source)
}
