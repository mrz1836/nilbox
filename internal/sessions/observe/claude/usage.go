package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/sessions"
)

const (
	// maxUsageBytes caps how much of one transcript is ever read for its token counts: a
	// session past it reports at least what that much held (sessions.Usage.Partial).
	maxUsageBytes = 64 << 20

	// maxUsageLine caps one line of a transcript: a longer one is passed over, and the
	// counts are partial, since it may have held some.
	maxUsageLine = 16 << 20

	// keptTranscripts caps the transcripts whose counts are kept between reads.
	keptTranscripts = 32

	// usageBuffer is the read buffer; a longer line is read in pieces of it.
	usageBuffer = 64 << 10
)

// Usage reads a Claude Code session's token counts from its transcript, for
// sessions.Service.Usage: every assistant message's usage, each message once, though
// Claude Code writes a line per part of a message, each with the message's usage. It keeps
// what it read of each transcript, by file, and reads only what was added since, so the
// session panel reading it again is cheap. It is safe for concurrent use.
type Usage struct {
	home string
	caps usageCaps

	mu   sync.Mutex
	kept map[string]*transcriptUsage
}

// usageCaps are how much of a transcript, and of one of its lines, is read.
type usageCaps struct {
	bytes int64
	line  int
}

// NewUsage returns the reader of the sessions whose default config directory is home's
// .claude.
func NewUsage(home string) *Usage {
	return &Usage{
		home: home, caps: usageCaps{bytes: maxUsageBytes, line: maxUsageLine}, kept: map[string]*transcriptUsage{},
	}
}

// Usage returns the token counts of the session r, read from its transcript under its
// config directory. It returns sessions.ErrNoUsage when the transcript can't be found or
// reports no usage.
func (u *Usage) Usage(_ context.Context, r sessions.Record) (sessions.Usage, error) {
	dir := r.ConfigDir
	if dir == "" {
		dir = filepath.Join(u.home, agentInfo().ConfigDir)
	}

	path := findTranscript(dir, r.AgentSessionID)
	if path == "" {
		return sessions.Usage{}, sessions.ErrNoUsage
	}

	u.mu.Lock()
	defer u.mu.Unlock()

	t := u.kept[path]
	if t == nil {
		if len(u.kept) >= keptTranscripts {
			clear(u.kept)
		}

		t = &transcriptUsage{caps: u.caps}
		u.kept[path] = t
	}

	if err := t.read(path); err != nil {
		delete(u.kept, path)

		return sessions.Usage{}, err
	}

	if len(t.messages) == 0 {
		return sessions.Usage{}, sessions.ErrNoUsage
	}

	return t.total(), nil
}

// transcriptUsage is what was read of one transcript: up to where, the file it was, and
// each message's usage by its ID, the largest of each count any of its lines gave.
type transcriptUsage struct {
	caps     usageCaps
	file     os.FileInfo
	offset   int64
	partial  bool
	messages map[string]tokens
	unnamed  tokens
}

// tokens are one message's counts, as a transcript's usage names them.
type tokens struct {
	Input      int64 `json:"input_tokens"`
	Output     int64 `json:"output_tokens"`
	CacheRead  int64 `json:"cache_read_input_tokens"`
	CacheWrite int64 `json:"cache_creation_input_tokens"`
}

// largest returns the larger of each of t's and o's counts.
func (t tokens) largest(o tokens) tokens {
	return tokens{
		Input: max(t.Input, o.Input), Output: max(t.Output, o.Output), CacheRead: max(t.CacheRead, o.CacheRead),
		CacheWrite: max(t.CacheWrite, o.CacheWrite),
	}
}

// plus returns t and o added.
func (t tokens) plus(o tokens) tokens {
	return tokens{
		Input: t.Input + o.Input, Output: t.Output + o.Output, CacheRead: t.CacheRead + o.CacheRead,
		CacheWrite: t.CacheWrite + o.CacheWrite,
	}
}

// usageLine is one line of a transcript, as far as its usage needs it.
type usageLine struct {
	Type    string `json:"type"`
	Message struct {
		ID    string  `json:"id"`
		Role  string  `json:"role"`
		Usage *tokens `json:"usage"`
	} `json:"message"`
}

// total returns every message's counts added up.
func (t *transcriptUsage) total() sessions.Usage {
	sum := t.unnamed

	for _, m := range t.messages {
		sum = sum.plus(m)
	}

	return sessions.Usage{
		Input: sum.Input, Output: sum.Output, CacheRead: sum.CacheRead, CacheWrite: sum.CacheWrite, Partial: t.partial,
	}.Sum()
}

// read reads what the transcript at path holds past what was read of it before: all of
// it again when it is another file, or shorter than what was read. Only a regular file,
// or a link to one, is read (paths.OpenRegular), so a pipe never blocks the read.
func (t *transcriptUsage) read(path string) error {
	f, err := paths.OpenRegular(path)
	if err != nil {
		return fmt.Errorf("open the transcript: %w", err)
	}

	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return fmt.Errorf("read the transcript: %w", err)
	}

	if t.file == nil || !os.SameFile(t.file, info) || info.Size() < t.offset {
		*t = transcriptUsage{caps: t.caps, messages: map[string]tokens{}}
	}

	t.file = info

	if t.offset >= t.caps.bytes {
		t.partial = t.partial || info.Size() > t.offset

		return nil
	}

	if _, err = f.Seek(t.offset, io.SeekStart); err != nil {
		return fmt.Errorf("read the transcript: %w", err)
	}

	return t.scan(bufio.NewReaderSize(io.LimitReader(f, t.caps.bytes-t.offset), usageBuffer), info.Size())
}

// scan reads whole lines from in, each counted once, moving the offset past each, and
// leaves a last line not yet ended for the next read, as Claude Code may be writing it.
func (t *transcriptUsage) scan(in *bufio.Reader, size int64) error {
	for {
		line, n, long, err := readLine(in, t.caps.line)

		switch {
		case errors.Is(err, io.EOF):
			// The file goes on past what the cap let through.
			if size > t.offset+int64(n) {
				t.partial = true
			}

			return nil
		case err != nil:
			return fmt.Errorf("read the transcript: %w", err)
		}

		t.offset += int64(n)

		if long {
			t.partial = true

			continue
		}

		t.count(line)
	}
}

// count adds the usage a line reports: an assistant message's, once per message.
func (t *transcriptUsage) count(line []byte) {
	if !bytes.Contains(line, []byte(`"usage"`)) {
		return
	}

	var l usageLine
	if json.Unmarshal(line, &l) != nil || l.Type != "assistant" || l.Message.Usage == nil {
		return
	}

	got := *l.Message.Usage
	if got.Input < 0 || got.Output < 0 || got.CacheRead < 0 || got.CacheWrite < 0 {
		return
	}

	if l.Message.ID == "" {
		t.unnamed = t.unnamed.plus(got)

		return
	}

	t.messages[l.Message.ID] = t.messages[l.Message.ID].largest(got)
}

// readLine reads one line from in, its newline included, and how many bytes it took; long
// reports a line over limit, which is passed over and not returned. A last line with no
// newline yet is io.EOF, with the bytes it took, which the caller leaves unread.
func readLine(in *bufio.Reader, limit int) (line []byte, n int, long bool, err error) {
	var buf []byte

	for {
		chunk, readErr := in.ReadSlice('\n')
		n += len(chunk)

		if !long {
			if len(buf)+len(chunk) > limit {
				long, buf = true, nil
			} else {
				buf = append(buf, chunk...)
			}
		}

		switch {
		case readErr == nil:
			return buf, n, long, nil
		case errors.Is(readErr, bufio.ErrBufferFull):
			continue
		default:
			return nil, n, long, readErr
		}
	}
}
