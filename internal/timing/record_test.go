package timing

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFirstPaintWritesOneStampedLine(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	logs := t.TempDir()

	r := NewRecorder(Config{
		Logger:  textLogger(&buf),
		LogsDir: logs,
		Start:   fixedStart(),
		Source:  SourceOS,
		Now:     func() time.Time { return fixedStart().Add(412 * time.Millisecond).In(time.FixedZone("EST", -5*3600)) },
	})

	_, _, err := r.FirstPaint()
	require.NoError(t, err)

	data, err := os.ReadFile(filepath.Join(logs, LogFileName)) //nolint:gosec // test temp file
	require.NoError(t, err)
	assert.Equal(t, "2026-01-02T03:04:05Z startup.first_paint_ms=412 source=os\n", string(data),
		"the line is stamped in UTC to the second")

	assert.Contains(t, buf.String(), `level=INFO msg="startup timing" startup.first_paint_ms=412 source=os`)
}

func TestFirstPaintRecordsOnceUnderConcurrentReports(t *testing.T) {
	t.Parallel()

	logs := t.TempDir()

	r := NewRecorder(Config{
		Logger:  textLogger(&bytes.Buffer{}),
		LogsDir: logs,
		Start:   fixedStart(),
		Source:  SourceRuntime,
		Now:     func() time.Time { return fixedStart().Add(250 * time.Millisecond) },
	})

	const reports = 16

	var (
		wg     sync.WaitGroup
		mu     sync.Mutex
		firsts int
		seen   []Measurement
		errs   []error
	)

	for range reports {
		wg.Go(func() {
			m, first, err := r.FirstPaint()

			mu.Lock()
			defer mu.Unlock()

			seen = append(seen, m)
			errs = append(errs, err)

			if first {
				firsts++
			}
		})
	}

	wg.Wait()

	require.NoError(t, errors.Join(errs...))
	assert.Equal(t, 1, firsts, "exactly one report is the first")
	assert.Len(t, seen, reports)

	for _, m := range seen {
		assert.Equal(t, Measurement{FirstPaintMS: 250, Source: SourceRuntime}, m)
	}

	data, err := os.ReadFile(filepath.Join(logs, LogFileName)) //nolint:gosec // test temp file
	require.NoError(t, err)
	assert.Equal(t, 1, strings.Count(string(data), "\n"), "one line is written")
}

func TestFirstPaintKeepsAnExistingLogsMode(t *testing.T) {
	t.Parallel()

	logs := t.TempDir()
	path := filepath.Join(logs, LogFileName)
	require.NoError(t, os.WriteFile(path, nil, 0o644)) //nolint:gosec // a mode the app would never choose

	r := NewRecorder(Config{Logger: textLogger(&bytes.Buffer{}), LogsDir: logs, Start: fixedStart(), Source: SourceOS})

	_, _, err := r.FirstPaint()
	require.NoError(t, err)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o644), info.Mode().Perm(), "only a new log is created private")
}

func TestOSProcessStartRefusesAProcessThatDoesNotExist(t *testing.T) {
	t.Parallel()

	_, err := osProcessStart(-1)
	require.Error(t, err)
}
