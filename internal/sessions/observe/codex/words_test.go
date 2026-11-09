package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/sessions"
)

// wordsThread is the thread the test rollout file belongs to.
const wordsThread = "01a1bebd-6f52-7942-9e77-e69115ff8ee8"

// writeRollout writes a rollout file of lines for wordsThread under home.
func writeRollout(t *testing.T, home string, lines ...string) {
	t.Helper()

	day := filepath.Join(home, rolloutsDir, "2026", "11", "09")
	require.NoError(t, os.MkdirAll(day, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(day, rolloutPrefix+"2026-11-09T00-17-28-"+wordsThread+".jsonl"),
		[]byte(strings.Join(lines, "\n")+"\n"), 0o600))
}

func TestWordsReadsTheAgentsLastMessage(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeRollout(t, filepath.Join(home, ".codex"),
		`{"type":"session_meta","payload":{"id":"`+wordsThread+`"}}`,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"First."}]}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","message":" The verdict: ship it. "}}`,
		`{"type":"event_msg","payload":{"type":"task_complete"}}`,
		`not json`,
	)

	got, err := Words{Home: home}.LastWords(t.Context(), sessions.Record{AgentSessionID: wordsThread})
	require.NoError(t, err)
	assert.Equal(t, "The verdict: ship it.", got)
}

func TestWordsReadsAResponseItem(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeRollout(t, home,
		`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"One."},{"type":"reasoning","text":"x"},{"type":"output_text","text":"Two."}]}}`,
		`{"type":"response_item","payload":{"type":"message","role":"user","content":[{"type":"input_text","text":"Hi"}]}}`,
	)

	got, err := Words{}.LastWords(t.Context(), sessions.Record{AgentSessionID: wordsThread, ConfigDir: home})
	require.NoError(t, err)
	assert.Equal(t, "One.\nTwo.", got)
}

func TestWordsFindsNothingToRead(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeRollout(t, home, `{"type":"event_msg","payload":{"type":"task_started"}}`)

	for name, r := range map[string]sessions.Record{
		"no message":  {AgentSessionID: wordsThread, ConfigDir: home},
		"no thread":   {ConfigDir: home},
		"a bad id":    {AgentSessionID: "../x", ConfigDir: home},
		"no rollout":  {AgentSessionID: "other-thread", ConfigDir: home},
		"no sessions": {AgentSessionID: wordsThread, ConfigDir: t.TempDir()},
	} {
		_, err := Words{}.LastWords(t.Context(), r)
		require.ErrorIs(t, err, sessions.ErrNoWords, name)
	}
}

func TestWordsReadsOnlyTheTailOfALongRollout(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeRollout(t, home, `{"type":"event_msg","payload":{"type":"agent_message","message":"Old."}}`)

	path := findRollout(filepath.Join(home, rolloutsDir), wordsThread)
	require.NotEmpty(t, path)

	data, cut, err := readRolloutTail(path, 10)
	require.NoError(t, err)
	assert.True(t, cut)
	assert.Len(t, data, 10)

	_, _, err = readRolloutTail(filepath.Join(home, "missing"), 10)
	require.Error(t, err)

	_, _, err = readRolloutTail(home, 10)
	require.ErrorIs(t, err, errNotRegular, "a folder")
}
