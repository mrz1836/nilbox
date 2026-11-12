package testutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// Recording is one recorded run of a real tool, as the recorder writes it to a file
// <name>.json in a suite's testdata: its name, its arguments, what it wrote to stdout and
// stderr, its exit status, and its lines in the order they arrived. A suite whose
// recorder writes more embeds it in a type of its own, which LoadRecording reads too.
type Recording struct {
	Name   string         `json:"name"`
	Argv   []string       `json:"argv"`
	Stdout string         `json:"stdout"`
	Stderr string         `json:"stderr"`
	Exit   int            `json:"exit"`
	Lines  []RecordedLine `json:"lines"`
}

// RecordedLine is one line a recorded run wrote: its stream, "out" or "err", and its text
// without the newline.
type RecordedLine struct {
	Stream string `json:"stream"`
	Text   string `json:"text"`
}

// Recorded is a Recording, or a type that embeds one.
type Recorded interface {
	recording() Recording
}

// Log returns the run's output as one log holds it, stdout and stderr in the order they
// arrived, each line ended by eol.
func (r Recording) Log(eol string) string {
	var b strings.Builder

	for _, l := range r.Lines {
		b.WriteString(l.Text)
		b.WriteString(eol)
	}

	return b.String()
}

// recording returns r, for LoadRecording to check the name of whatever embeds it.
func (r Recording) recording() Recording { return r }

// LoadRecording reads the recording called name from the folder dir, and fails the test
// unless it names itself name.
func LoadRecording[T Recorded](t testing.TB, dir, name string) T {
	t.Helper()

	data, err := os.ReadFile(filepath.Join(dir, name+".json")) //nolint:gosec // a fixture the test names
	require.NoError(t, err)

	var rec T
	require.NoError(t, json.Unmarshal(data, &rec))
	require.Equal(t, name, rec.recording().Name)

	return rec
}

// RecordingNames lists the names of the recordings in the folder dir whose names start
// with prefix, in order, and fails the test when there are none.
func RecordingNames(t testing.TB, dir, prefix string) []string {
	t.Helper()

	paths, err := filepath.Glob(filepath.Join(dir, prefix+"*.json"))
	require.NoError(t, err)
	require.NotEmpty(t, paths, "no recordings %s*.json in %s", prefix, dir)

	names := make([]string, 0, len(paths))
	for _, p := range paths {
		names = append(names, strings.TrimSuffix(filepath.Base(p), ".json"))
	}

	return names
}
