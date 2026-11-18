package claude

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/paths"
)

// A session's transcript records each Bash command its agent runs (a tool_use block,
// named Bash, holding the command) and, on a later line, the command's result (a
// tool_result block naming the call, with is_error when it failed or was refused). The
// observer follows each live session's transcript from where it last read it, so a
// command that changed GitHub (fence.ChangesGitHub) and finished is reported once
// (Observation.Acted), and the inbox reads the session's item again while the session
// still works.

// Bounds on following a transcript's commands: how much is read the first time (its end,
// which holds the session's own commands, never the history a resumed conversation
// carries in), how much at most on each poll, and the longest line weighed. A longer line
// (a long command's output) is passed over, which leaves its command to the read after the
// session stops working.
const (
	commandsFirstRead = 4 << 20
	commandsPerPoll   = 4 << 20
	commandsLine      = 1 << 20
	commandsPending   = 64
)

// commandScan is how far a transcript's commands were followed: the file it was, the
// offset past the last whole line read, whether that offset is inside a line (a first
// read that started inside the file, or a line too long to weigh) whose rest is passed
// over, and the calls waiting for their result that change GitHub, by the call's ID.
type commandScan struct {
	file    os.FileInfo
	offset  int64
	midLine bool
	pending map[string]bool
}

// unchanged reports whether info, the transcript as it is now, holds nothing past what was
// read of it, so it needn't be opened.
func (c *commandScan) unchanged(info os.FileInfo) bool {
	return c.file != nil && os.SameFile(c.file, info) && info.Size() == c.offset
}

// commandLine is one transcript line, as far as its commands need it.
type commandLine struct {
	Timestamp time.Time `json:"timestamp"`
	Message   struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

// commandBlock is one part of a message: a Bash call, or a call's result.
type commandBlock struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Name  string `json:"name"`
	Input struct {
		Command         string `json:"command"`
		RunInBackground bool   `json:"run_in_background"`
	} `json:"input"`
	ToolUseID string `json:"tool_use_id"`
	IsError   bool   `json:"is_error"`
}

// follow reads what the transcript at path holds past what was read of it, up to
// commandsPerPoll, counting only calls made at or after since, and reports whether a
// call that changes GitHub got its result without an error. A file read for the first
// time, another file, or one shorter than what was read is read from its last
// commandsFirstRead. Only a regular file is read (paths.OpenRegular), so a pipe never
// blocks it; the caller has refused a link.
func (c *commandScan) follow(path string, since time.Time) (bool, error) {
	f, err := paths.OpenRegular(path)
	if err != nil {
		return false, fmt.Errorf("open the transcript: %w", err)
	}

	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return false, fmt.Errorf("read the transcript: %w", err)
	}

	if c.file == nil || !os.SameFile(c.file, info) || info.Size() < c.offset {
		// A first read that starts inside the file starts inside a line.
		start := max(0, info.Size()-commandsFirstRead)
		*c = commandScan{offset: start, midLine: start > 0, pending: map[string]bool{}}
	}

	c.file = info

	if c.offset >= info.Size() {
		return false, nil
	}

	if _, err = f.Seek(c.offset, io.SeekStart); err != nil {
		return false, fmt.Errorf("read the transcript: %w", err)
	}

	return c.scan(bufio.NewReaderSize(io.LimitReader(f, commandsPerPoll), usageBuffer), since)
}

// scan weighs the whole lines in, moving the offset past each. It passes over the rest of
// a line it is inside of, and a line too long to weigh, and leaves a last line not yet
// ended for the next read, as Claude Code may be writing it, unless that is already too
// long to weigh: what of it was read is passed over, and its rest next time.
func (c *commandScan) scan(in *bufio.Reader, since time.Time) (bool, error) {
	acted := false

	for {
		line, n, long, err := readLine(in, commandsLine)

		switch {
		case errors.Is(err, io.EOF):
			c.stopAt(n, long)

			return acted, nil
		case err != nil:
			return acted, fmt.Errorf("read the transcript: %w", err)
		}

		c.offset += int64(n)

		if c.midLine || long {
			c.midLine = false

			continue
		}

		acted = c.weigh(line, since) || acted
	}
}

// stopAt leaves the last line a read reached the end of what it could read inside, n bytes
// of it read, long when that is over commandsLine: one still short is read whole next
// time, as Claude Code may still be writing it, and one too long to weigh is passed over,
// what was read of it now and its rest next time.
func (c *commandScan) stopAt(n int, long bool) {
	if long || n >= commandsLine {
		c.offset += int64(n)
		c.midLine = true
	}
}

// weigh notes line's Bash calls that change GitHub, made at or after since, and reports
// whether it holds the result, without an error, of one noted before. Its bytes are
// checked before it is decoded, so most lines are never decoded.
func (c *commandScan) weigh(line []byte, since time.Time) bool {
	if !c.mayMatter(line) {
		return false
	}

	at, blocks, ok := commandBlocks(line)
	if !ok {
		return false
	}

	acted := false

	for _, b := range blocks {
		switch {
		case b.Type == "tool_use" && b.Name == "Bash":
			c.noteCall(b, at, since)
		case b.Type == "tool_result" && c.pending[b.ToolUseID]:
			delete(c.pending, b.ToolUseID)
			acted = acted || !b.IsError
		}
	}

	return acted
}

// mayMatter reports, from its bytes alone, whether line may hold a Bash call or the result
// of a call waiting for one, which is all weigh decodes a line for.
func (c *commandScan) mayMatter(line []byte) bool {
	if bytes.Contains(line, []byte(`"tool_use"`)) && bytes.Contains(line, []byte(`"Bash"`)) {
		return true
	}

	return len(c.pending) > 0 && bytes.Contains(line, []byte(`"tool_result"`)) && c.namesPending(line)
}

// commandBlocks decodes a transcript line into when it was written and its message's
// parts; false for a line that isn't one, or whose message is plain text.
func commandBlocks(line []byte) (time.Time, []commandBlock, bool) {
	var l commandLine
	if json.Unmarshal(line, &l) != nil {
		return time.Time{}, nil, false
	}

	var blocks []commandBlock
	if json.Unmarshal(l.Message.Content, &blocks) != nil {
		return time.Time{}, nil, false
	}

	return l.Timestamp, blocks, true
}

// noteCall notes a Bash call that changes GitHub, made at or after since (a call with no
// time counts), unless it runs in the background, whose result comes before it finishes.
func (c *commandScan) noteCall(b commandBlock, at, since time.Time) {
	if b.ID == "" || b.Input.RunInBackground || (!at.IsZero() && at.Before(since)) {
		return
	}

	if !fence.ChangesGitHub(b.Input.Command) {
		return
	}

	if len(c.pending) >= commandsPending {
		clear(c.pending)
	}

	c.pending[b.ID] = true
}

// namesPending reports whether line names a call waiting for its result.
func (c *commandScan) namesPending(line []byte) bool {
	for id := range c.pending {
		if bytes.Contains(line, []byte(id)) {
			return true
		}
	}

	return false
}
