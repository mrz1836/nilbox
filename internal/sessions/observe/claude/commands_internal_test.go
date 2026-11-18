package claude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The lines of a Bash call that changes GitHub, and of its result without an error.
const (
	reviewCall   = `{"type":"assistant","timestamp":"2026-11-09T05:11:20.000Z","message":{"role":"assistant","content":[{"type":"tool_use","id":"t9","name":"Bash","input":{"command":"gh pr review 12 --approve"}}]}}` + "\n"
	reviewResult = `{"type":"user","timestamp":"2026-11-09T05:11:21.000Z","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"t9","content":"Approved"}]}}` + "\n"
	plainLine    = `{"type":"assistant","timestamp":"2026-11-09T05:11:19.000Z","message":{"role":"assistant","content":[{"type":"text","text":"Reading the diff."}]}}` + "\n"
)

// commandsSince is the time the scan tests count calls from.
func commandsSince() time.Time {
	return time.Date(2026, 11, 9, 5, 11, 0, 0, time.UTC)
}

// writeTranscript writes content as a transcript and returns its path.
func writeTranscript(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "session.jsonl")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))

	return path
}

// appendText adds text to the file at path.
func appendText(t *testing.T, path, text string) {
	t.Helper()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the test's own file
	require.NoError(t, err)

	_, err = f.WriteString(text)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

// follow runs one follow that must read the file.
func follow(t *testing.T, c *commandScan, path string) bool {
	t.Helper()

	acted, err := c.follow(path, commandsSince())
	require.NoError(t, err)

	return acted
}

func TestCommandScanLeavesALineStillBeingWritten(t *testing.T) {
	t.Parallel()

	path := writeTranscript(t, plainLine)

	var c commandScan

	assert.False(t, follow(t, &c, path))
	assert.Equal(t, int64(len(plainLine)), c.offset)

	// Claude Code is writing the call's line: what is there is left for the next read.
	appendText(t, path, reviewCall[:40])
	assert.False(t, follow(t, &c, path))
	assert.Equal(t, int64(len(plainLine)), c.offset, "a line not yet ended is not read")

	appendText(t, path, reviewCall[40:]+reviewResult)
	assert.True(t, follow(t, &c, path))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.True(t, c.unchanged(info), "read to its end, so the next poll opens nothing")
}

func TestCommandScanPassesOverALineTooLongToWeigh(t *testing.T) {
	t.Parallel()

	path := writeTranscript(t, plainLine)

	var c commandScan

	follow(t, &c, path)

	// A command's output longer than a poll reads, then the call and its result.
	long := `{"type":"user","message":{"content":"` + strings.Repeat("x", commandsPerPoll+commandsLine) + `"}}` + "\n"
	appendText(t, path, long+reviewCall+reviewResult)

	assert.False(t, follow(t, &c, path), "the first poll reads only the long line's start")
	assert.True(t, c.midLine, "and passes over the rest of it next time")
	assert.True(t, follow(t, &c, path), "the next poll passes over its rest, then reads the call and its result")
}

func TestCommandScanReadsOnlyTheEndOfALongTranscriptFirst(t *testing.T) {
	t.Parallel()

	// A call and its result, then more of the conversation than the first read takes.
	filler := strings.Repeat(plainLine, commandsFirstRead/len(plainLine)+10)
	path := writeTranscript(t, reviewCall+reviewResult+filler)

	var c commandScan

	assert.False(t, follow(t, &c, path), "what came before the end is the conversation's history")

	appendText(t, path, strings.Replace(reviewCall, "t9", "t10", 1)+strings.Replace(reviewResult, "t9", "t10", 1))
	assert.True(t, follow(t, &c, path))
}

func TestCommandScanStartsOverOnAnotherFile(t *testing.T) {
	t.Parallel()

	path := writeTranscript(t, plainLine+plainLine)

	var c commandScan

	follow(t, &c, path)

	// The transcript is written anew, shorter than what was read.
	next := filepath.Join(filepath.Dir(path), "next.jsonl")
	require.NoError(t, os.WriteFile(next, []byte(reviewCall+reviewResult), 0o600))
	require.NoError(t, os.Rename(next, path))

	assert.True(t, follow(t, &c, path), "another file is read from its start")
}

func TestCommandScanKeepsFewCallsWaiting(t *testing.T) {
	t.Parallel()

	var calls strings.Builder
	for i := range commandsPending + 5 {
		calls.WriteString(strings.Replace(reviewCall, `"t9"`, `"c`+string(rune('a'+i%26))+strings.Repeat("x", i)+`"`, 1))
	}

	path := writeTranscript(t, calls.String())

	var c commandScan

	follow(t, &c, path)
	assert.LessOrEqual(t, len(c.pending), commandsPending, "calls that never got a result don't pile up")
}

func FuzzCommandScanWeigh(f *testing.F) {
	for _, seed := range []string{reviewCall, reviewResult, plainLine, `{"message":{"content":"text"}}`, `not json`, ""} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, line string) {
		c := commandScan{pending: map[string]bool{"t9": true}}

		acted := c.weigh([]byte(line), commandsSince())

		// Only a call's result, without an error, tells a call finished.
		if acted {
			assert.Contains(t, line, "tool_result")
			assert.NotContains(t, c.pending, "t9", "a call is told once")
		}

		assert.LessOrEqual(t, len(c.pending), commandsPending)
	})
}
