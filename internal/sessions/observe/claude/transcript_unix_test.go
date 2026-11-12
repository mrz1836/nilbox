//go:build unix

package claude_test

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/sessions/observe/claude"
	"github.com/mrz1836/nilbox/internal/testutil"
)

func TestLastMessageRefusesAPipe(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "s.jsonl")
	require.NoError(t, syscall.Mkfifo(path, 0o600))

	link := filepath.Join(dir, "linked.jsonl")
	require.NoError(t, os.Symlink(path, link))

	done := make(chan [2]error, 1)

	go func() {
		_, pipeErr := claude.LastMessage(path)

		_, linkErr := claude.LastMessage(link)

		done <- [2]error{pipeErr, linkErr}
	}()

	select {
	case errs := <-done:
		require.ErrorIs(t, errs[0], paths.ErrNotRegular, "a pipe where the transcript should be is refused")
		require.ErrorIs(t, errs[1], paths.ErrNotRegular, "and so is a link to one")
	case <-time.After(10 * time.Second):
		require.FailNow(t, "reading a transcript waited on a named pipe")
	}
}

func TestObserveNeverReadsATranscriptThroughALinkSwappedIn(t *testing.T) {
	t.Parallel()

	h := newHarness(t)
	path := filepath.Join(h.home, ".claude", "projects", "-srv-wt-example-repo-pr-12", idleID+".jsonl")
	testutil.WriteFile(t, path, string(fixture(t, "transcript.jsonl")))

	r := h.record(idleID)
	first := "The change looks good.\nOne nit: the new **helper** has no test."
	require.Equal(t, first, h.observe(t, r)[r.ID].LastMessage)

	elsewhere := filepath.Join(t.TempDir(), "elsewhere.jsonl")
	testutil.WriteFile(t, elsewhere, assistantLine("Through the link."))
	require.NoError(t, os.Chtimes(elsewhere, h.now, h.now.Add(time.Hour)))
	require.NoError(t, os.Remove(path))
	require.NoError(t, os.Symlink(elsewhere, path))

	assert.Equal(t, first, h.observe(t, r)[r.ID].LastMessage,
		"a transcript found as a regular file and then swapped for a link is not read through it")
	assert.NotContains(t, h.observe(t, r)[r.ID].LastMessage, "Through the link",
		"nor once it is looked for again")

	require.NoError(t, os.Remove(path))
	testutil.WriteFile(t, path, assistantLine("Back as a file."))

	assert.Equal(t, "Back as a file.", h.observe(t, r)[r.ID].LastMessage,
		"a regular file there again is found and read")
}

// assistantLine is a transcript line in which the session itself says text.
func assistantLine(text string) string {
	return `{"type":"assistant","isSidechain":false,"message":{"role":"assistant",` +
		`"content":[{"type":"text","text":"` + text + `"}]}}` + "\n"
}
