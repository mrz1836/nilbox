package codex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/sessions"
)

// wordsTailBytes is how much of a rollout file's end is read for its last message, which
// can run long.
const wordsTailBytes = 4 << 20

// rolloutDepth is how deep a Codex home's rollout files sit under its sessions folder:
// YYYY/MM/DD.
const rolloutDepth = 3

// errNotRegular is returned for a rollout path that is not a regular file.
var errNotRegular = errors.New("the rollout file is not a regular file")

// Words reads a Codex session's last message in full from its rollout file, for
// sessions.Service.LastWords.
type Words struct {
	// Home is the user's home directory, whose .codex is the default Codex home.
	Home string
}

// LastWords returns the last message the agent wrote in the session r's thread, read from
// the end of the thread's rollout file. It returns sessions.ErrNoWords when the file can't
// be found or holds no message from the agent.
func (w Words) LastWords(_ context.Context, r sessions.Record) (string, error) {
	home := r.ConfigDir
	if home == "" {
		home = filepath.Join(w.Home, codexInfo().ConfigDir)
	}

	path := findRollout(filepath.Join(home, rolloutsDir), r.AgentSessionID)
	if path == "" {
		return "", sessions.ErrNoWords
	}

	data, cut, err := readRolloutTail(path, wordsTailBytes)
	if err != nil {
		return "", err
	}

	lines := bytes.Split(data, []byte("\n"))
	if cut && len(lines) > 0 {
		lines = lines[1:]
	}

	for _, line := range slices.Backward(lines) {
		if text := agentText(line); text != "" {
			return text, nil
		}
	}

	return "", sessions.ErrNoWords
}

// findRollout returns the rollout file of the thread with id under root, a Codex home's
// sessions folder, or "": a regular file named rollout-<time>-<id>.jsonl, up to
// rolloutDepth folders down.
func findRollout(root, id string) string {
	if id == "" || strings.ContainsAny(id, `/\`) {
		return ""
	}

	suffix := "-" + id + ".jsonl"
	found := ""

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return nil //nolint:nilerr // a folder that can't be read is skipped, not fatal
		case d.IsDir():
			return skipDeep(root, path)
		case d.Type().IsRegular() && strings.HasPrefix(d.Name(), rolloutPrefix) && strings.HasSuffix(d.Name(), suffix):
			found = path

			return filepath.SkipAll
		default:
			return nil
		}
	})

	return found
}

// skipDeep skips a folder deeper under root than rollout files sit.
func skipDeep(root, path string) error {
	if rel, err := filepath.Rel(root, path); err == nil && rel != "." && depth(rel) > rolloutDepth {
		return filepath.SkipDir
	}

	return nil
}

// depth counts the folders in rel, a path relative to the sessions folder.
func depth(rel string) int {
	return strings.Count(rel, string(filepath.Separator)) + 1
}

// rolloutLine is one line of a rollout file, as far as the agent's messages need it: an
// event that the agent said something, or a response item that is the agent's message.
type rolloutLine struct {
	Type    string `json:"type"`
	Payload struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		Role    string `json:"role"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	} `json:"payload"`
}

// agentText returns the text of a rollout line that is a message from the agent, or "".
func agentText(line []byte) string {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return ""
	}

	var l rolloutLine
	if json.Unmarshal(line, &l) != nil {
		return ""
	}

	switch {
	case l.Type == "event_msg" && l.Payload.Type == "agent_message":
		return strings.TrimSpace(l.Payload.Message)
	case l.Type == "response_item" && l.Payload.Type == "message" && l.Payload.Role == "assistant":
		return l.outputText()
	default:
		return ""
	}
}

// outputText joins the text the agent output in a response item, one part per line.
func (l rolloutLine) outputText() string {
	var parts []string

	for _, c := range l.Payload.Content {
		if c.Type == "output_text" && strings.TrimSpace(c.Text) != "" {
			parts = append(parts, strings.TrimSpace(c.Text))
		}
	}

	return strings.Join(parts, "\n")
}

// readRolloutTail reads up to n bytes from the end of the regular file at path, and
// whether that is not the whole file.
func readRolloutTail(path string, n int64) ([]byte, bool, error) {
	f, err := os.Open(path) //nolint:gosec // a rollout file under the agent's own home
	if err != nil {
		return nil, false, fmt.Errorf("open the rollout file: %w", err)
	}

	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, false, fmt.Errorf("read the rollout file: %w", err)
	}

	if !info.Mode().IsRegular() {
		return nil, false, errNotRegular
	}

	offset := max(info.Size()-n, 0)

	data := make([]byte, info.Size()-offset)
	if _, err = f.ReadAt(data, offset); err != nil && !errors.Is(err, io.EOF) {
		return nil, false, fmt.Errorf("read the rollout file: %w", err)
	}

	return data, offset > 0, nil
}
