package logging

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/brand"
)

// env returns a getenv func backed by a fixed map.
func env(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

// records reads the JSON records in the file at path, failing on any line that is not
// one whole record.
func records(t *testing.T, path string) []map[string]any {
	t.Helper()

	data, err := os.ReadFile(path) //nolint:gosec // the test's own temp file
	require.NoError(t, err)

	var out []map[string]any

	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		var rec map[string]any
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &rec), "line %q", scanner.Text())

		out = append(out, rec)
	}

	require.NoError(t, scanner.Err())

	return out
}

func TestLevel(t *testing.T) {
	t.Parallel()

	key := DebugEnv()

	tests := []struct {
		name string
		args []string
		vars map[string]string
		want slog.Level
	}{
		{name: "default", want: slog.LevelInfo},
		{name: "flag", args: []string{"--measure-startup", DebugFlag}, want: slog.LevelDebug},
		{name: "env on", vars: map[string]string{key: "1"}, want: slog.LevelDebug},
		{name: "env true", vars: map[string]string{key: "true"}, want: slog.LevelDebug},
		{name: "env off", vars: map[string]string{key: "0"}, want: slog.LevelInfo},
		{name: "env not a boolean", vars: map[string]string{key: "verbose"}, want: slog.LevelInfo},
		{name: "another flag", args: []string{"--debugger"}, want: slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, Level(tt.args, env(tt.vars)))
		})
	}
}

func TestDebugEnvComesFromTheBrand(t *testing.T) {
	t.Parallel()

	assert.Equal(t, strings.ToUpper(brand.Slug)+"_DEBUG", DebugEnv())
	assert.Equal(t, brand.Slug+".log", FileName)
}

func TestNewWritesTextToStderrAndJSONToTheFile(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "logs")

	var stderr bytes.Buffer

	logger, closer := newLogger(&stderr, dir, slog.LevelInfo, MaxFileBytes)
	logger.Info("refreshed the inbox", "items", 3)
	logger.Debug("left out at Info")
	require.NoError(t, closer.Close())

	assert.Contains(t, stderr.String(), `level=INFO msg="refreshed the inbox" items=3`)
	assert.NotContains(t, stderr.String(), "left out")

	got := records(t, filepath.Join(dir, FileName))
	require.Len(t, got, 1)
	assert.Equal(t, "INFO", got[0]["level"])
	assert.Equal(t, "refreshed the inbox", got[0]["msg"])
	assert.InDelta(t, 3, got[0]["items"], 0)
}

func TestNewKeepsDebugRecordsAtDebug(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	var stderr bytes.Buffer

	logger, closer := newLogger(&stderr, dir, slog.LevelDebug, MaxFileBytes)
	logger.With("job", "sweep").Debug("skipped a tick")
	require.NoError(t, closer.Close())

	assert.Contains(t, stderr.String(), `level=DEBUG msg="skipped a tick" job=sweep`)

	got := records(t, filepath.Join(dir, FileName))
	require.Len(t, got, 1)
	assert.Equal(t, "sweep", got[0]["job"])
}

func TestNewCreatesPrivateFiles(t *testing.T) {
	t.Parallel()

	paths := appdata.NewPaths(filepath.Join(t.TempDir(), "data"))

	logger, closer := New(paths, slog.LevelInfo)
	logger.Debug("not written at Info")
	require.NoError(t, closer.Close())

	info, err := os.Stat(paths.Logs)
	require.NoError(t, err)
	assert.Equal(t, appdata.PrivateDirPerm, info.Mode().Perm())

	info, err = os.Stat(filepath.Join(paths.Logs, FileName))
	require.NoError(t, err)
	assert.Equal(t, appdata.PrivateFilePerm, info.Mode().Perm())
}

func TestAnUnwritableLogFolderFallsBackToStderr(t *testing.T) {
	t.Parallel()

	// A file where the folder should be cannot hold the log, even for root.
	blocker := filepath.Join(t.TempDir(), "blocker")
	require.NoError(t, os.WriteFile(blocker, nil, appdata.PrivateFilePerm))

	var stderr bytes.Buffer

	logger, closer := newLogger(&stderr, filepath.Join(blocker, "logs"), slog.LevelInfo, MaxFileBytes)
	logger.Info("still logged")
	require.NoError(t, closer.Close())

	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	require.Len(t, lines, 2)
	assert.Contains(t, lines[0], `level=WARN msg="logging to stderr only: the log file could not be opened"`)
	assert.Contains(t, lines[0], "not a directory")
	assert.Contains(t, lines[1], `msg="still logged"`)
}

func TestALogFileThatCannotBeOpenedFallsBackToStderr(t *testing.T) {
	t.Parallel()

	// A folder where the log file should be cannot be opened for writing.
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, FileName), appdata.PrivateDirPerm))

	var stderr bytes.Buffer

	logger, closer := newLogger(&stderr, dir, slog.LevelInfo, MaxFileBytes)
	logger.Info("still logged")
	require.NoError(t, closer.Close())

	assert.Contains(t, stderr.String(), `level=WARN msg="logging to stderr only: the log file could not be opened"`)
	assert.Contains(t, stderr.String(), "open the log file")
	assert.Contains(t, stderr.String(), `msg="still logged"`)
}

func TestNoDataDirectoryLogsToStderrAlone(t *testing.T) {
	t.Parallel()

	var stderr bytes.Buffer

	logger, closer := newLogger(&stderr, "", slog.LevelInfo, MaxFileBytes)
	logger.Info("still logged")
	require.NoError(t, closer.Close())

	lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
	require.Len(t, lines, 1, "no warning: there was no file to open")
	assert.Contains(t, lines[0], `msg="still logged"`)
}

func TestFileRotatesAtTheCap(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	record := []byte(strings.Repeat("x", 39) + "\n") // 40 bytes

	w, err := openFile(dir, 100)
	require.NoError(t, err)

	// Two records fit under the cap; the third starts a new file. Ten records make five
	// files' worth, of which the newest three are kept.
	for i := range 10 {
		line := fmt.Appendf(nil, "%02d%s", i, record[2:])
		_, err = w.Write(line)
		require.NoError(t, err)
	}

	require.NoError(t, w.Close())

	want := map[string]string{
		FileName:        "08" + string(record[2:]) + "09" + string(record[2:]),
		FileName + ".1": "06" + string(record[2:]) + "07" + string(record[2:]),
		FileName + ".2": "04" + string(record[2:]) + "05" + string(record[2:]),
	}

	for name, content := range want {
		data, readErr := os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // the test's own temp file
		require.NoError(t, readErr, name)
		assert.Equal(t, content, string(data), name)
	}

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Len(t, entries, KeptFiles, "nothing beyond %s.2 is kept", FileName)
}

func TestFileKeepsARecordWhole(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	big := []byte(strings.Repeat("y", 150) + "\n")

	w, err := openFile(dir, 100)
	require.NoError(t, err)

	_, err = w.Write([]byte("small\n"))
	require.NoError(t, err)

	n, err := w.Write(big)
	require.NoError(t, err)
	assert.Equal(t, len(big), n)
	require.NoError(t, w.Close())

	data, err := os.ReadFile(filepath.Join(dir, FileName)) //nolint:gosec // the test's own temp file
	require.NoError(t, err)
	assert.Equal(t, string(big), string(data), "a record over the cap gets a file of its own")

	data, err = os.ReadFile(filepath.Join(dir, FileName+".1")) //nolint:gosec // the test's own temp file
	require.NoError(t, err)
	assert.Equal(t, "small\n", string(data))
}

func TestFileAppendsToAnExistingLog(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, FileName)
	require.NoError(t, os.WriteFile(path, []byte(strings.Repeat("z", 95)+"\n"), appdata.PrivateFilePerm))

	w, err := openFile(dir, 100)
	require.NoError(t, err)

	// The earlier run's 96 bytes count toward the cap.
	_, err = w.Write([]byte("next run\n"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	data, err := os.ReadFile(path) //nolint:gosec // the test's own temp file
	require.NoError(t, err)
	assert.Equal(t, "next run\n", string(data))

	data, err = os.ReadFile(path + ".1") //nolint:gosec // the test's own temp file
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("z", 95)+"\n", string(data))
}

func TestFileKeepsWritingWhenItCannotRotate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Folders that are not empty in the rotated files' places cannot be renamed over.
	for _, name := range []string{FileName + ".1", FileName + ".2"} {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, name, "keep"), appdata.PrivateDirPerm))
	}

	w, err := openFile(dir, 10)
	require.NoError(t, err)

	for _, line := range []string{"first line\n", "second line\n", "third line\n"} {
		_, err = w.Write([]byte(line))
		require.NoError(t, err)
	}

	require.NoError(t, w.Close())

	data, err := os.ReadFile(filepath.Join(dir, FileName)) //nolint:gosec // the test's own temp file
	require.NoError(t, err)
	assert.Equal(t, "first line\nsecond line\nthird line\n", string(data))
}

func TestCloseFlushesAndEndsWriting(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	var stderr bytes.Buffer

	logger, closer := newLogger(&stderr, dir, slog.LevelInfo, MaxFileBytes)
	for i := range 5 {
		logger.Info("record", "n", i)
	}

	require.NoError(t, closer.Close())
	require.NoError(t, closer.Close(), "closing twice is harmless")

	assert.Len(t, records(t, filepath.Join(dir, FileName)), 5)

	// A record after Close still reaches stderr.
	logger.Info("after close")
	assert.Contains(t, stderr.String(), `msg="after close"`)
	assert.Len(t, records(t, filepath.Join(dir, FileName)), 5)

	// A closed file stays closed, even when a write would have rotated it.
	small := t.TempDir()
	w, err := openFile(small, 10)
	require.NoError(t, err)

	_, err = w.Write([]byte("filled the file\n"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	_, err = w.Write([]byte("late\n"))
	require.ErrorIs(t, err, fs.ErrClosed)

	entries, err := os.ReadDir(small)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "nothing was rotated or opened after Close")
}

func TestFileOpensAgainAfterARotationThatCouldNotOpenIt(t *testing.T) {
	t.Parallel()

	// testutil.SkipAsRoot would import this package back through the timing package.
	if os.Geteuid() == 0 {
		t.Skip("root writes to a read-only folder")
	}

	dir := t.TempDir()

	w, err := openFile(dir, 10)
	require.NoError(t, err)

	_, err = w.Write([]byte("first line\n"))
	require.NoError(t, err)

	// With the live file gone and the folder read-only, the rotation cannot open a new
	// file: that record is lost, and the next write, once the folder can be written,
	// opens the file again.
	require.NoError(t, os.Remove(filepath.Join(dir, FileName)))
	require.NoError(t, os.Chmod(dir, 0o500)) //nolint:gosec // a read-only folder, briefly, to make the open fail

	_, err = w.Write([]byte("lost line\n"))
	require.Error(t, err)

	require.NoError(t, os.Chmod(dir, appdata.PrivateDirPerm))

	_, err = w.Write([]byte("third line\n"))
	require.NoError(t, err)
	require.NoError(t, w.Close())

	data, err := os.ReadFile(filepath.Join(dir, FileName)) //nolint:gosec // the test's own temp file
	require.NoError(t, err)
	assert.Equal(t, "third line\n", string(data))
}

func TestConcurrentWritersNeverSplitARecord(t *testing.T) {
	t.Parallel()

	const (
		writers = 8
		each    = 50
	)

	dir := t.TempDir()

	// About 45 KB of records over 20 KB files rotates twice, so every record is kept.
	logger, closer := newLogger(&bytes.Buffer{}, dir, slog.LevelInfo, 20<<10)

	var wg sync.WaitGroup

	for w := range writers {
		wg.Go(func() {
			for n := range each {
				logger.Info("concurrent record", "writer", w, "n", n)
			}
		})
	}

	wg.Wait()
	require.NoError(t, closer.Close())

	seen := map[string]bool{}

	for i := range KeptFiles {
		name := filepath.Join(dir, FileName)
		if i > 0 {
			name += fmt.Sprintf(".%d", i)
		}

		for _, rec := range records(t, name) {
			seen[fmt.Sprint(rec["writer"], "/", rec["n"])] = true
		}
	}

	assert.Len(t, seen, writers*each)
}

func TestOr(t *testing.T) {
	t.Parallel()

	discard := Or(nil)
	require.NotNil(t, discard)
	assert.False(t, discard.Enabled(t.Context(), slog.LevelError))

	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))
	assert.Same(t, logger, Or(logger))
}
