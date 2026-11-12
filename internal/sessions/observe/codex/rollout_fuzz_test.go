package codex

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FuzzAgentText checks that any rollout line reads as the agent's text with no space
// around it, and that a line Codex writes for what the agent said, as an event or as a
// response item, reads back as its text.
func FuzzAgentText(f *testing.F) {
	for _, seed := range []struct {
		line    string
		text    string
		asEvent bool
	}{
		{"", "Done.", false},
		{`{"type":"event_msg","payload":{"type":"agent_message","message":" hi "}}`, " ", true},
		{`{"type":"response_item","payload":{"type":"message","role":"assistant","content":[{"type":"output_text","text":"a"},{"type":"input_text","text":"b"}]}}`, "a\nb", false},
		{`{"type":"response_item","payload":{"type":"message","role":"user","content":[]}}`, "été", true},
		{`{"type":"event_msg","payload":{"type":"agent_message","message":7}}`, "\xff", false},
		{"  \n\t", "x", true},
	} {
		f.Add([]byte(seed.line), seed.text, seed.asEvent)
	}

	f.Fuzz(func(t *testing.T, line []byte, text string, asEvent bool) {
		got := agentText(line)
		assert.Equal(t, strings.TrimSpace(got), got)

		if !utf8.ValidString(text) {
			return
		}

		payload := map[string]any{
			"type": "message", "role": "assistant", "content": []map[string]string{{"type": "output_text", "text": text}},
		}
		kind := "response_item"

		if asEvent {
			payload, kind = map[string]any{"type": "agent_message", "message": text}, "event_msg"
		}

		written, err := json.Marshal(map[string]any{"type": kind, "payload": payload})
		require.NoError(t, err)
		assert.Equal(t, strings.TrimSpace(text), agentText(written))
	})
}

// FuzzRolloutMeta checks that any rollout file's first line is read whole, or reported
// still growing or too long, and names a thread only for a session's own first line with
// an ID, and that the first line Codex writes for a thread, its start as RFC 3339 or only
// in the file's name, reads back as that thread.
func FuzzRolloutMeta(f *testing.F) {
	for _, seed := range []struct {
		data, name, id, cwd string
		started             int64
		inName              bool
	}{
		{"", "rollout-2026-11-09T05-11-00-abc.jsonl", "t1", "/src/example-repo", 1_762_665_060, false},
		{`{"type":"session_meta","payload":{"id":"t1","timestamp":"2026-11-09T05:11:00Z","source":"cli"}}` + "\n", "x", "t", "", 0, true},
		{`{"type":"session_meta","payload":{"id":"t2","source":{"subagent":{}}}}`, "rollout-2026-11-09T05-11-00", "sub", "~", -1, true},
		{`{"type":"session_meta","payload":{"id":"","timestamp":"bad"}}` + "\nsecond", "rollout-short", "é", "a b", 4_102_444_800, false},
		{"not json\n", "rollout-2026-13-45T99-99-99", "id", "", 253_402_300_799, true},
	} {
		f.Add([]byte(seed.data), seed.name, seed.id, seed.cwd, seed.started, seed.inName)
	}

	f.Fuzz(func(t *testing.T, data []byte, name, id, cwd string, started int64, inName bool) {
		checkFirstLine(t, data)

		var meta rolloutMeta
		if line, _ := firstLine(bytes.NewReader(data)); json.Unmarshal(line, &meta) == nil {
			if th, ok := meta.thread(name); ok {
				assert.Equal(t, sessionMeta, meta.Type)
				assert.NotEmpty(t, th.ID)
			}
		}

		// A start is written in UTC and named in local time, each with a four-digit year.
		at := time.Unix(started, 0)
		if id == "" || !utf8.ValidString(id) || !utf8.ValidString(cwd) || !fourDigitYear(at) || !fourDigitYear(at.UTC()) {
			return
		}

		checkThreadRoundTrip(t, id, cwd, at, inName)
	})
}

// fourDigitYear reports a time whose year RFC 3339 and a rollout file's name can write.
func fourDigitYear(at time.Time) bool {
	return at.Year() >= 1 && at.Year() <= 9999
}

// checkFirstLine checks what firstLine makes of data: a line no longer than the cap, whole
// when it ended with a newline or could never fit, or nothing for a line too long.
func checkFirstLine(t *testing.T, data []byte) {
	t.Helper()

	line, whole := firstLine(bytes.NewReader(data))
	end := bytes.IndexByte(data, '\n')

	switch {
	case line == nil:
		assert.True(t, whole || len(data) == 0, "only a line too long reads as nothing")
		assert.True(t, end < 0 && len(data) > maxMetaBytes || end > maxMetaBytes || len(data) == 0)
	case whole:
		assert.Equal(t, data[:end+1], line)
	default:
		assert.Equal(t, data, line, "a line not ended yet is all there is")
	}
}

// checkThreadRoundTrip checks that the first line Codex writes for a thread with id, cwd,
// and a start at, recorded in the line or else only in the file's name, reads back as
// that thread.
func checkThreadRoundTrip(t *testing.T, id, cwd string, at time.Time, inName bool) {
	t.Helper()

	payload := map[string]any{"id": id, "cwd": cwd, "timestamp": at.UTC().Format(time.RFC3339Nano), "source": "cli"}
	name := "rollout-2026-01-01T00-00-00-" + id + ".jsonl"

	if inName {
		delete(payload, "timestamp")

		at = time.Date(at.Year(), at.Month(), at.Day(), at.Hour(), at.Minute(), at.Second(), 0, time.Local) //nolint:gosmopolitan // Codex names its files by the local time
		name = rolloutPrefix + at.Format(nameStamp) + "-" + id + ".jsonl"
	}

	written, err := json.Marshal(map[string]any{"type": sessionMeta, "payload": payload})
	require.NoError(t, err)

	line, whole := firstLine(bytes.NewReader(append(written, '\n')))
	require.True(t, whole)

	var meta rolloutMeta
	require.NoError(t, json.Unmarshal(line, &meta))

	th, ok := meta.thread(name)
	require.True(t, ok)
	assert.Equal(t, Thread{ID: id, Cwd: cwd, CreatedAt: at.Unix()}, th)
}
