package claude

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/paths"
)

// tailBytes is how much of a transcript's end is read for its last message, and
// wordsTailBytes how much is read for the whole of it, which can run long.
const (
	tailBytes      = 64 << 10
	wordsTailBytes = 4 << 20
)

// record is one line of a transcript, as far as the last message needs it.
type record struct {
	Type        string `json:"type"`
	IsSidechain bool   `json:"isSidechain"`
	Message     struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// block is one part of a message's content.
type block struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// LastMessage returns the text of the last message the agent wrote in the transcript at
// path, read from its last 64 KB: the last assistant record of the session itself, not
// of a subagent, that holds text. It returns "" when the tail holds none, and an error
// only when the file cannot be read.
func LastMessage(path string) (string, error) {
	return lastMessageIn(path, tailBytes)
}

// lastMessageIn is LastMessage, reading the last n bytes of the transcript. Only a regular
// file, or a link to one, is read (paths.ReadTail), so a pipe never blocks the read.
func lastMessageIn(path string, n int64) (string, error) {
	data, cut, err := paths.ReadTail(path, n)
	if err != nil {
		return "", fmt.Errorf("read the transcript: %w", err)
	}

	lines := bytes.Split(data, []byte("\n"))
	if cut && len(lines) > 0 {
		lines = lines[1:]
	}

	for _, line := range slices.Backward(lines) {
		if text := assistantText(line); text != "" {
			return text, nil
		}
	}

	return "", nil
}

// assistantText returns the text of a transcript line that is the session's own
// assistant message, or "".
func assistantText(line []byte) string {
	line = bytes.TrimSpace(line)
	if len(line) == 0 || !bytes.Contains(line, []byte(`"assistant"`)) {
		return ""
	}

	var r record
	if json.Unmarshal(line, &r) != nil || r.Type != "assistant" || r.IsSidechain || r.Message.Role != "assistant" {
		return ""
	}

	return contentText(r.Message.Content)
}

// contentText returns the text of a message's content: a plain string, or its text
// blocks, one per line.
func contentText(content json.RawMessage) string {
	var blocks []block
	if json.Unmarshal(content, &blocks) != nil {
		var text string
		if json.Unmarshal(content, &text) != nil {
			return ""
		}

		return strings.TrimSpace(text)
	}

	var parts []string

	for _, b := range blocks {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, strings.TrimSpace(b.Text))
		}
	}

	return strings.Join(parts, "\n")
}
