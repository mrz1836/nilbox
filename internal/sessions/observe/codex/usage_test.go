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

// usageRollout is testdata/usage-rollout.jsonl's lines.
func usageRollout(t *testing.T) []string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", "usage-rollout.jsonl"))
	require.NoError(t, err)

	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

func TestUsageReadsTheLastTokenCount(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeRollout(t, filepath.Join(home, ".codex"), usageRollout(t)...)

	got, err := Usage{Home: home}.Usage(t.Context(), sessions.Record{AgentSessionID: wordsThread})
	require.NoError(t, err)
	assert.Equal(t, sessions.Usage{Input: 19630, CacheRead: 73344, Output: 462}.Sum(), got)
	assert.Equal(t, int64(93436), got.Total, "the parts add up to Codex's own total")
}

func TestUsageOfATokenCount(t *testing.T) {
	t.Parallel()

	count := func(total string) string {
		return `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":` + total + `}}}`
	}

	tests := []struct {
		name string
		line string
		want sessions.Usage
		ok   bool
	}{
		{
			name: "input written to the cache", ok: true,
			line: count(`{"input_tokens":100,"cached_input_tokens":60,"cache_write_input_tokens":30,"output_tokens":5}`),
			want: sessions.Usage{Input: 10, CacheRead: 60, CacheWrite: 30, Output: 5}.Sum(),
		},
		{
			name: "more cached than the input says", ok: true,
			line: count(`{"input_tokens":10,"cached_input_tokens":60,"output_tokens":5}`),
			want: sessions.Usage{CacheRead: 60, Output: 5}.Sum(),
		},
		{name: "a negative count", line: count(`{"input_tokens":-1,"output_tokens":5}`)},
		{name: "no total", line: `{"type":"event_msg","payload":{"type":"token_count","info":{}}}`},
		{name: "no info", line: `{"type":"event_msg","payload":{"type":"token_count","info":null}}`},
		{name: "another event", line: `{"type":"event_msg","payload":{"type":"agent_message","message":"token_count"}}`},
		{name: "a response item", line: `{"type":"response_item","payload":{"type":"token_count"}}`},
		{name: "not json", line: `token_count`},
		{name: "empty", line: ``},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, ok := tokenCount([]byte(tt.line))
			assert.Equal(t, tt.ok, ok)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestUsageFindsNothingToCount(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	writeRollout(t, home, `{"type":"event_msg","payload":{"type":"task_started"}}`)

	for name, r := range map[string]sessions.Record{
		"no token count": {AgentSessionID: wordsThread, ConfigDir: home},
		"no thread":      {ConfigDir: home},
		"a bad id":       {AgentSessionID: "../x", ConfigDir: home},
		"no rollout":     {AgentSessionID: "other-thread", ConfigDir: home},
		"no sessions":    {AgentSessionID: wordsThread, ConfigDir: t.TempDir()},
	} {
		_, err := Usage{}.Usage(t.Context(), r)
		require.ErrorIs(t, err, sessions.ErrNoUsage, name)
	}
}

func TestUsageReadsOnlyTheTailOfALongRollout(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	lines := usageRollout(t)
	writeRollout(t, home, append(lines, strings.Repeat(`{"type":"event_msg","payload":{"type":"x"}}`+"\n", 64))...)

	day := filepath.Join(home, rolloutsDir, "2026", "11", "09")
	path := filepath.Join(day, rolloutPrefix+"2026-11-09T00-17-28-"+wordsThread+".jsonl")

	_, ok, err := lastTokenCount(path, 1024)
	require.NoError(t, err)
	assert.False(t, ok, "a token count before the tail isn't read")

	got, ok, err := lastTokenCount(path, 1<<20)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, int64(93436), got.Total)
}
