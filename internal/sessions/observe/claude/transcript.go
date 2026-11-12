package claude

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
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

// lastMessageIn is LastMessage, reading the last n bytes of the transcript.
func lastMessageIn(path string, n int64) (string, error) {
	tail, err := readTail(path, n)
	if err != nil {
		return "", err
	}

	lines := bytes.Split(tail.data, []byte("\n"))
	if tail.cut && len(lines) > 0 {
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

// tail is the end of a file, and whether it is not the whole file.
type tail struct {
	data []byte
	cut  bool
}

// errNotAFile is returned for a transcript path that is not a regular file, such as a
// pipe, which opening could block on.
var errNotAFile = errors.New("the transcript is not a regular file")

// readTail reads up to n bytes from the end of the regular file at path. Anything else
// there is refused before it is opened, so a pipe never blocks the read.
func readTail(path string, n int64) (tail, error) {
	if info, err := os.Lstat(path); err != nil {
		return tail{}, fmt.Errorf("open the transcript: %w", err)
	} else if !info.Mode().IsRegular() {
		return tail{}, errNotAFile
	}

	f, err := os.Open(path) //nolint:gosec // a transcript under the agent's own config directory
	if err != nil {
		return tail{}, fmt.Errorf("open the transcript: %w", err)
	}

	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return tail{}, fmt.Errorf("read the transcript: %w", err)
	}

	if !info.Mode().IsRegular() {
		return tail{}, errNotAFile
	}

	offset := max(info.Size()-n, 0)

	data := make([]byte, info.Size()-offset)
	if _, err = f.ReadAt(data, offset); err != nil && !errors.Is(err, io.EOF) {
		return tail{}, fmt.Errorf("read the transcript: %w", err)
	}

	return tail{data: data, cut: offset > 0}, nil
}
