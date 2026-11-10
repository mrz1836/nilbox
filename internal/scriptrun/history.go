package scriptrun

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"unicode/utf8"
)

// maxChunk caps how much of a log one Output call returns.
const maxChunk = 256 << 10

// retention is how much history is kept: runs at most, and bytes of logs in all.
type retention struct {
	runs  int
	bytes int64
}

// drop returns the runs to forget, from recs, newest first: the oldest finished ones,
// until what is left is within the limits. A run going counts, by its log's size so far,
// but is never dropped.
func (r retention) drop(recs []Record, size func(id int64) int64) []int64 {
	count, total := len(recs), int64(0)

	sizes := make([]int64, len(recs))
	for i, rec := range recs {
		sizes[i] = rec.LogBytes
		if rec.Status.Live() {
			sizes[i] = size(rec.ID)
		}

		total += sizes[i]
	}

	var out []int64

	for i := len(recs) - 1; i >= 0 && (count > r.runs || total > r.bytes); i-- {
		if recs[i].Status.Live() {
			continue
		}

		out = append(out, recs[i].ID)
		count--
		total -= sizes[i]
	}

	return out
}

// prune forgets the runs the retention drops, their logs with them.
func (e *Engine) prune(ctx context.Context) {
	e.pruneMu.Lock()
	defer e.pruneMu.Unlock()

	recs, err := e.store.list(ctx)
	if err != nil {
		e.logger.WarnContext(ctx, "couldn't read the runs to keep the history short", "error", err)

		return
	}

	if err = e.forget(ctx, e.keep.drop(recs, e.files.size)); err != nil {
		e.logger.WarnContext(ctx, "couldn't keep the history short", "error", err)
	}
}

// clearHistory forgets every run that has ended, with its log. Runs going are kept.
func (e *Engine) clearHistory(ctx context.Context) error {
	e.pruneMu.Lock()
	defer e.pruneMu.Unlock()

	recs, err := e.store.list(ctx)
	if err != nil {
		return err
	}

	ids := make([]int64, 0, len(recs))

	for _, rec := range recs {
		if !rec.Status.Live() {
			ids = append(ids, rec.ID)
		}
	}

	err = e.forget(ctx, ids)

	e.notify()

	return err
}

// forget removes the files of the runs ids, which have ended, and then their records.
// The files go first: a run's ID can be handed out again once its record, the newest,
// is gone, and the files of a run that took it must never be removed.
func (e *Engine) forget(ctx context.Context, ids []int64) error {
	var errs []error

	for _, id := range ids {
		if err := e.files.clear(id); err != nil {
			errs = append(errs, err)
		}
	}

	if err := e.store.delete(ctx, ids); err != nil {
		return err
	}

	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("remove the forgotten runs' logs: %w", err)
	}

	return nil
}

// Chunk is a part of a run's log, for the window.
type Chunk struct {
	// Text is the part, cut between characters.
	Text string `json:"text"`

	// Offset is where in the log Text starts, and Next where the next part starts: the
	// offset to ask for next.
	Offset int64 `json:"offset"`
	Next   int64 `json:"next"`

	// Size is the log's size now.
	Size int64 `json:"size"`

	// Done reports that the run has ended and Next is the log's end: no more will come.
	Done bool `json:"done"`

	// Restart reports an offset past the log's end, as when a long log was cut down at its
	// run's end: read it again from the start.
	Restart bool `json:"restart"`
}

// readChunk reads up to limit bytes of the log at path from offset. A negative offset
// reads the log's last limit bytes. ended says the run has ended, so the log won't grow.
func readChunk(path string, offset, limit int64, ended bool) (Chunk, error) {
	if limit <= 0 || limit > maxChunk {
		limit = maxChunk
	}

	f, err := os.Open(path) //nolint:gosec // the run's own log, under the app's data folder
	if errors.Is(err, fs.ErrNotExist) {
		at := max(offset, 0)

		return Chunk{Offset: at, Next: at, Done: ended}, nil
	}

	if err != nil {
		return Chunk{}, fmt.Errorf("read the run's log: %w", err)
	}

	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return Chunk{}, fmt.Errorf("read the run's log: %w", err)
	}

	return chunkOf(f, info.Size(), offset, max(limit, utf8.UTFMax), ended), nil
}

// chunkOf reads up to limit bytes of a log of size from offset, or its last limit bytes
// for a negative offset, cut between characters: a read that starts inside a character
// starts after it, and one that ends inside a character ends before it while more may
// come.
func chunkOf(src io.ReaderAt, size, offset, limit int64, ended bool) Chunk {
	fromEnd := offset < 0
	if fromEnd {
		offset = max(0, size-limit)
	}

	if offset > size {
		return Chunk{Offset: offset, Next: offset, Size: size, Done: ended, Restart: true}
	}

	buf := make([]byte, min(limit, size-offset))
	n, _ := src.ReadAt(buf, offset) // a short read is what there is
	buf = buf[:n]

	if offset > 0 {
		skip := partialStart(buf)
		buf, offset = buf[skip:], offset+int64(skip)
	}

	if !ended || offset+int64(len(buf)) < size {
		buf = buf[:completeEnd(buf)]
	}

	next := offset + int64(len(buf))

	return Chunk{Text: string(buf), Offset: offset, Next: next, Size: size, Done: ended && next >= size}
}

// partialStart returns how many bytes at buf's start belong to a character that started
// before it.
func partialStart(buf []byte) int {
	skip := 0
	for skip < len(buf) && skip < utf8.UTFMax && !utf8.RuneStart(buf[skip]) {
		skip++
	}

	return skip
}
