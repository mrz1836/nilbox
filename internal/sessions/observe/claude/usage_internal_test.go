package claude

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/sessions"
)

// usageSession is the session the transcripts these tests write belong to.
const usageSession = "11111111-2222-4333-8444-555555555555"

// usageLineOf is a transcript line of the assistant message id, which wrote out tokens.
func usageLineOf(id string, out int) string {
	return `{"type":"assistant","message":{"id":"` + id + `","usage":{"output_tokens":` + strconv.Itoa(out) + `}}}` + "\n"
}

// cappedUsage returns a reader that reads at most total bytes of a transcript, and lines
// of at most perLine.
func cappedUsage(total int64, perLine int) *Usage {
	u := NewUsage("")
	u.caps = usageCaps{bytes: total, line: perLine}

	return u
}

// transcriptAt writes data as usageSession's transcript under the config directory dir.
func transcriptAt(t *testing.T, dir, data string) {
	t.Helper()

	project := filepath.Join(dir, projectsDir, "example-repo")
	require.NoError(t, os.MkdirAll(project, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(project, usageSession+".jsonl"), []byte(data), 0o600))
}

func TestUsageOfAPartlyReadTranscript(t *testing.T) {
	t.Parallel()

	long := `{"type":"assistant","message":{"id":"big","usage":{"output_tokens":9},"content":"` +
		strings.Repeat("x", 300) + `"}}` + "\n"
	two := int64(len(usageLineOf("a", 1)) + len(usageLineOf("b", 2)))

	tests := []struct {
		name    string
		data    string
		total   int64
		perLine int
		want    sessions.Usage
	}{
		{
			name: "every line read", data: usageLineOf("a", 1) + usageLineOf("b", 2), total: 1 << 20, perLine: 1 << 10,
			want: sessions.Usage{Output: 3}.Sum(),
		},
		{
			name: "a line too long to read", data: usageLineOf("a", 1) + long + usageLineOf("b", 2), total: 1 << 20,
			perLine: 200, want: sessions.Usage{Output: 3, Partial: true}.Sum(),
		},
		{
			name: "more than the cap", data: usageLineOf("a", 1) + usageLineOf("b", 2) + usageLineOf("c", 4),
			total: two, perLine: 1 << 10, want: sessions.Usage{Output: 3, Partial: true}.Sum(),
		},
		{
			name: "a line cut by the cap", data: usageLineOf("a", 1) + usageLineOf("b", 2) + usageLineOf("c", 4),
			total: two + 5, perLine: 1 << 10, want: sessions.Usage{Output: 3, Partial: true}.Sum(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			transcriptAt(t, dir, tt.data)

			reader := cappedUsage(tt.total, tt.perLine)
			r := sessions.Record{AgentSessionID: usageSession, ConfigDir: dir}

			got, err := reader.Usage(t.Context(), r)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)

			again, err := reader.Usage(t.Context(), r)
			require.NoError(t, err)
			assert.Equal(t, tt.want, again, "read again, from what it kept")
		})
	}
}

func TestUsageKeepsABoundedNumberOfTranscripts(t *testing.T) {
	t.Parallel()

	reader := NewUsage("")

	for i := range keptTranscripts + 3 {
		dir := t.TempDir()
		transcriptAt(t, dir, usageLineOf("a", i+1))

		got, err := reader.Usage(t.Context(), sessions.Record{AgentSessionID: usageSession, ConfigDir: dir})
		require.NoError(t, err)
		assert.Equal(t, int64(i+1), got.Output)
	}

	assert.LessOrEqual(t, len(reader.kept), keptTranscripts)
}

func TestUsageOfATranscriptThatIsGone(t *testing.T) {
	t.Parallel()

	reader := NewUsage("")
	dir := t.TempDir()
	transcriptAt(t, dir, usageLineOf("a", 1))

	r := sessions.Record{AgentSessionID: usageSession, ConfigDir: dir}
	_, err := reader.Usage(t.Context(), r)
	require.NoError(t, err)

	require.NoError(t, os.RemoveAll(filepath.Join(dir, projectsDir)))

	_, err = reader.Usage(t.Context(), r)
	require.ErrorIs(t, err, sessions.ErrNoUsage)

	err = (&transcriptUsage{caps: reader.caps}).read(filepath.Join(dir, "nope.jsonl"))
	require.Error(t, err, "a transcript that went between finding and opening it")
}
