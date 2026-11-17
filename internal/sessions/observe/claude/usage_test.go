package claude_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/sessions"
	"github.com/mrz1836/nilbox/internal/sessions/observe/claude"
)

// writeTranscript writes data as the transcript of wordsSession under the config
// directory dir, and returns its path.
func writeTranscript(t *testing.T, dir string, data []byte) string {
	t.Helper()

	project := filepath.Join(dir, "projects", "example-repo")
	require.NoError(t, os.MkdirAll(project, 0o700))

	path := filepath.Join(project, wordsSession+".jsonl")
	require.NoError(t, os.WriteFile(path, data, 0o600))

	return path
}

// usageWant is what testdata/usage.jsonl counts: each message once, at the largest counts
// its lines gave, a subagent's message included, and no count a tool's output spells.
func usageWant() sessions.Usage {
	return sessions.Usage{Input: 18, Output: 245, CacheRead: 33200, CacheWrite: 1500}.Sum()
}

func TestUsageCountsEachMessageOnce(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeTranscript(t, filepath.Join(home, ".claude"), fixture(t, "usage.jsonl"))

	got, err := claude.NewUsage(home).Usage(t.Context(), sessions.Record{AgentSessionID: wordsSession})
	require.NoError(t, err)
	assert.Equal(t, usageWant(), got)
	assert.Equal(t, int64(34963), got.Total)
}

func TestUsageReadsOnlyWhatWasAddedSince(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	data := fixture(t, "usage.jsonl")
	path := writeTranscript(t, dir, data)
	reader := claude.NewUsage(t.TempDir())
	r := sessions.Record{AgentSessionID: wordsSession, ConfigDir: dir}

	_, err := reader.Usage(t.Context(), r)
	require.NoError(t, err)

	more := `{"type":"assistant","message":{"id":"msg_03","usage":{"input_tokens":5,"output_tokens":150}}}` + "\n" +
		`{"type":"assistant","message":{"id":"msg_05","usage":{"input_tokens":7,"output_tokens":3,` +
		`"cache_read_input_tokens":40}}}` + "\n" + `{"type":"assistant","message":{"id":"msg_06","usage":{"output_tokens":`

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0) //nolint:gosec // the test's own file
	require.NoError(t, err)
	_, err = f.WriteString(more)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	got, err := reader.Usage(t.Context(), r)
	require.NoError(t, err)

	want := usageWant()
	want.Input, want.Output, want.CacheRead = want.Input+7, want.Output+30+3, want.CacheRead+40
	assert.Equal(t, want.Sum(), got, "the message's larger count, a new one, and no line not yet ended")

	require.NoError(t, os.WriteFile(path, []byte(`{"type":"assistant","message":{"id":"x","usage":{"output_tokens":2}}}`+"\n"), 0o600))

	got, err = reader.Usage(t.Context(), r)
	require.NoError(t, err)
	assert.Equal(t, sessions.Usage{Output: 2}.Sum(), got, "a transcript written again is read again")
}

func TestUsageFindsNothingToCount(t *testing.T) {
	t.Parallel()

	empty := t.TempDir()
	writeTranscript(t, empty, []byte(`{"type":"user","message":{"role":"user","content":"hi"}}`+"\n"))

	for name, r := range map[string]sessions.Record{
		"no transcript":  {AgentSessionID: "22222222-2222-4333-8444-555555555555", ConfigDir: t.TempDir()},
		"no session ID":  {ConfigDir: empty},
		"a bad ID":       {AgentSessionID: "../x", ConfigDir: empty},
		"no usage in it": {AgentSessionID: wordsSession, ConfigDir: empty},
	} {
		_, err := claude.NewUsage(t.TempDir()).Usage(t.Context(), r)
		require.ErrorIs(t, err, sessions.ErrNoUsage, name)
	}
}
