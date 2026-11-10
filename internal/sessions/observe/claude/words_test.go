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

// wordsSession is the session the test transcript belongs to.
const wordsSession = "11111111-2222-4333-8444-555555555555"

func TestWordsReadsTheLastMessageInFull(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	project := filepath.Join(home, ".claude", "projects", "example-repo")
	require.NoError(t, os.MkdirAll(project, 0o700))

	require.NoError(t, os.WriteFile(filepath.Join(project, wordsSession+".jsonl"), fixture(t, "transcript.jsonl"), 0o600))

	words := claude.Words{Home: home}

	got, err := words.LastWords(t.Context(), sessions.Record{AgentSessionID: wordsSession})
	require.NoError(t, err)
	assert.Contains(t, got, "The change looks good.")

	_, err = words.LastWords(t.Context(), sessions.Record{AgentSessionID: "22222222-2222-4333-8444-555555555555"})
	require.ErrorIs(t, err, sessions.ErrNoWords, "no transcript")

	empty := filepath.Join(home, "other")
	require.NoError(t, os.MkdirAll(filepath.Join(empty, "projects", "p"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(empty, "projects", "p", wordsSession+".jsonl"), []byte("{}\n"), 0o600))

	_, err = words.LastWords(t.Context(), sessions.Record{AgentSessionID: wordsSession, ConfigDir: empty})
	require.ErrorIs(t, err, sessions.ErrNoWords, "a transcript with no message of the session's own")
}
