package claude

import (
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// FuzzAssistantText checks that any transcript line reads as a message's text with no
// space around it, and only from a line that names the assistant, and that a line Claude
// Code writes for an assistant message, its content a string or a text block, reads back
// as its text, unless a subagent wrote it.
func FuzzAssistantText(f *testing.F) {
	for _, seed := range []struct {
		line      string
		text      string
		asString  bool
		sidechain bool
	}{
		{"", "Done.", false, false},
		{`{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":" hi "}]}}`, " ", true, false},
		{`{"type":"assistant","isSidechain":true,"message":{"role":"assistant","content":"x"}}`, "a\nb", false, true},
		{`{"type":"user","message":{"role":"assistant","content":"x"}} "assistant"`, "été", true, false},
		{`{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use"},{"type":"text","text":""}]}}`, "", false, false},
		{`{"type":"assistant","message":{"role":"assistant","content":{"not":"a list"}}}`, "\xff", false, false},
	} {
		f.Add([]byte(seed.line), seed.text, seed.asString, seed.sidechain)
	}

	f.Fuzz(func(t *testing.T, line []byte, text string, asString, sidechain bool) {
		got := assistantText(line)
		assert.Equal(t, strings.TrimSpace(got), got)

		if got != "" {
			assert.Contains(t, string(line), `"assistant"`)
		}

		if !utf8.ValidString(text) {
			return
		}

		var content any = []block{{Type: "text", Text: text}}
		if asString {
			content = text
		}

		written, err := json.Marshal(map[string]any{
			"type": "assistant", "isSidechain": sidechain,
			"message": map[string]any{"role": "assistant", "content": content},
		})
		require.NoError(t, err)

		want := strings.TrimSpace(text)
		if sidechain {
			want = ""
		}

		assert.Equal(t, want, assistantText(written))
	})
}
