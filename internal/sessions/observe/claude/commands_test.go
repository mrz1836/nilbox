package claude_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
)

// bashCall is a transcript line in which the agent runs command, as the call id, at.
func bashCall(t *testing.T, id, command string, at time.Time, background bool) string {
	t.Helper()

	input := map[string]any{"command": command}
	if background {
		input["run_in_background"] = true
	}

	return transcriptLine(t, map[string]any{
		"type": "assistant", "isSidechain": false, "timestamp": at.Format(time.RFC3339Nano),
		"message": map[string]any{"role": "assistant", "content": []map[string]any{
			{"type": "tool_use", "id": id, "name": "Bash", "input": input},
		}},
	})
}

// bashResult is a transcript line holding the result of the call id, an error or not.
func bashResult(t *testing.T, id string, isError bool, at time.Time) string {
	t.Helper()

	result := map[string]any{"type": "tool_result", "tool_use_id": id, "content": "done"}
	if isError {
		result["is_error"] = true
	}

	return transcriptLine(t, map[string]any{
		"type": "user", "isSidechain": false, "timestamp": at.Format(time.RFC3339Nano),
		"message": map[string]any{"role": "user", "content": []map[string]any{result}},
	})
}

// transcriptLine is v as one transcript line, its newline included.
func transcriptLine(t *testing.T, v any) string {
	t.Helper()

	raw, err := json.Marshal(v)
	require.NoError(t, err)

	return string(raw) + "\n"
}

// appendTo adds lines to the file at path.
func appendTo(t *testing.T, path string, lines ...string) {
	t.Helper()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // the test's own file
	require.NoError(t, err)

	for _, l := range lines {
		_, err = f.WriteString(l)
		require.NoError(t, err)
	}

	require.NoError(t, f.Close())
}

// transcriptHarness is a harness with the transcript of the idle session, as the fixture
// holds it, and its path.
func transcriptHarness(t *testing.T) (*harness, string) {
	t.Helper()

	h := newHarness(t)
	path := filepath.Join(h.home, ".claude", "projects", "-srv-wt-example-repo-pr-12", idleID+".jsonl")
	testutil.WriteFile(t, path, string(fixture(t, "transcript.jsonl")))

	return h, path
}

func TestObserveTellsACommandThatChangedGitHub(t *testing.T) {
	t.Parallel()

	h, path := transcriptHarness(t)
	r := h.record(idleID)

	assert.False(t, h.observe(t, r)[r.ID].Acted, "a git diff changes nothing on GitHub")

	appendTo(t, path, bashCall(t, "t9", "gh pr review 12 --approve", h.now, false))
	assert.False(t, h.observe(t, r)[r.ID].Acted, "not until it finished")

	appendTo(t, path, bashResult(t, "t9", false, h.now.Add(time.Second)))
	assert.True(t, h.observe(t, r)[r.ID].Acted, "it finished without an error")
	assert.False(t, h.observe(t, r)[r.ID].Acted, "told once")

	// A push, behind a cd, counts too.
	appendTo(t, path,
		bashCall(t, "t10", "cd /srv/wt/example-repo && git push origin HEAD", h.now.Add(2*time.Second), false),
		bashResult(t, "t10", false, h.now.Add(3*time.Second)))
	assert.True(t, h.observe(t, r)[r.ID].Acted)
}

func TestObserveTellsNoCommandThatFailedOrRunsInTheBackground(t *testing.T) {
	t.Parallel()

	h, path := transcriptHarness(t)
	r := h.record(idleID)
	h.observe(t, r)

	appendTo(t, path,
		bashCall(t, "t9", "gh pr merge 12 --squash", h.now, false),
		bashResult(t, "t9", true, h.now.Add(time.Second)),
		bashCall(t, "t10", "gh pr comment 12 --body ok", h.now, true),
		bashResult(t, "t10", false, h.now.Add(time.Second)),
		bashCall(t, "t11", "gh pr view 12 --comments", h.now, false),
		bashResult(t, "t11", false, h.now.Add(time.Second)))

	assert.False(t, h.observe(t, r)[r.ID].Acted,
		"a refused merge, a comment still running in the background, and a read change nothing yet")
}

func TestObserveTellsNothingFromBeforeTheLaunch(t *testing.T) {
	t.Parallel()

	h, path := transcriptHarness(t)
	r := h.record(idleID)

	// A conversation resumed carries what it did before this session was launched.
	before := r.LaunchedAt.Add(-time.Hour)
	appendTo(t, path,
		bashCall(t, "t9", "gh pr merge 12", before, false),
		bashResult(t, "t9", false, before.Add(time.Second)))

	assert.False(t, h.observe(t, r)[r.ID].Acted, "an hour before the launch")
}
