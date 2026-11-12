package testutil_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// noted is a suite's own recording: what the recorder wrote, and a note it added.
type noted struct {
	testutil.Recording

	Note string `json:"note"`
}

// recorded is a recording as the recorder writes it, with fields no suite reads.
const recorded = `{"name": "patch", "note": "on master", "dir": "/work/patch", "argv": ["magex", "version:bump"],
	"stdout": "bumped\n", "stderr": "warned\n", "exit": 2, "durationMs": 13,
	"lines": [{"stream": "out", "ms": 6, "text": "bumped"}, {"stream": "err", "ms": 7, "text": "warned"}]}`

func TestLoadRecordingReadsWhatTheRecorderWrote(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testutil.WriteFile(t, filepath.Join(dir, "patch.json"), recorded)

	rec := testutil.LoadRecording[noted](t, dir, "patch")
	assert.Equal(t, noted{Recording: testutil.Recording{
		Name: "patch", Argv: []string{"magex", "version:bump"}, Stdout: "bumped\n", Stderr: "warned\n", Exit: 2,
		Lines: []testutil.RecordedLine{{Stream: "out", Text: "bumped"}, {Stream: "err", Text: "warned"}},
	}, Note: "on master"}, rec)

	assert.Equal(t, "bumped\nwarned\n", rec.Log("\n"))
	assert.Equal(t, "bumped\r\nwarned\r\n", rec.Log("\r\n"))
	assert.Equal(t, rec.Recording, testutil.LoadRecording[testutil.Recording](t, dir, "patch"))
	assert.Empty(t, testutil.Recording{}.Log("\n"), "no lines, no output")
}

func TestLoadRecordingFailsOnAFileNamedForAnother(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testutil.WriteFile(t, filepath.Join(dir, "minor.json"), recorded)
	testutil.WriteFile(t, filepath.Join(dir, "broken.json"), `{"name": "broken",`)

	for _, name := range []string{"minor", "broken", "missing"} {
		assert.True(t, fails(t, func(tb testing.TB) { testutil.LoadRecording[noted](tb, dir, name) }), name)
	}
}

func TestRecordingNamesListsARecordersFiles(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	for _, name := range []string{"run-pass.json", "run-fail.json", "status-ready.json", "notes.txt"} {
		testutil.WriteFile(t, filepath.Join(dir, name), "{}")
	}

	assert.Equal(t, []string{"run-fail", "run-pass", "status-ready"}, testutil.RecordingNames(t, dir, ""))
	assert.Equal(t, []string{"run-fail", "run-pass"}, testutil.RecordingNames(t, dir, "run-"))
	assert.True(t, fails(t, func(tb testing.TB) { testutil.RecordingNames(tb, dir, "install-") }),
		"a prefix no recording has fails, rather than leaving a test with no cases")
}
