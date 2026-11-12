package scriptrun

import (
	"sync"

	"github.com/mrz1836/nilbox/internal/textutil"
)

// MaxNotes caps each list of lines a reader of a run's output keeps, its warnings among
// them: the first ones.
const MaxNotes = 200

// LineWriter is the part every kind's reader of a run's output shares: an io.Writer for
// the run's Spec.Output, fed the log's bytes in order and split anywhere, which hands each
// whole line, as textutil.LineSplitter cleans it, to the func the reader gave
// NewLineWriter. It does so under its lock, which the reader's own methods take too (Lock
// and Unlock) to read what that func kept.
type LineWriter struct {
	mu    sync.Mutex
	read  func(line string)
	lines textutil.LineSplitter
}

// NewLineWriter returns a writer that hands each line it reads to read.
func NewLineWriter(read func(line string)) *LineWriter {
	return &LineWriter{read: read}
}

// Write reads p, the next part of the output. It never fails.
func (w *LineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.lines.Write(p, w.read)

	return len(p), nil
}

// Flush reads the last line when the output ended without a newline. Call it once the
// output has ended.
func (w *LineWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.lines.Flush(w.read)
}

// Lock takes the lock no line is read without, so what the reader kept can be read.
func (w *LineWriter) Lock() {
	w.mu.Lock()
}

// Unlock lets the lock go.
func (w *LineWriter) Unlock() {
	w.mu.Unlock()
}

// Notes are the lines a reader of a run's output keeps for the run's result: the tool's
// warnings, the first MaxNotes, and the lines the reader didn't recognize, of which it
// keeps the first MaxUnrecognized and counts them all.
type Notes struct {
	Warnings          []string `json:"warnings"`
	Unrecognized      []string `json:"unrecognized"`
	UnrecognizedCount int      `json:"unrecognizedCount"`
}

// Warn keeps a warning, up to MaxNotes.
func (n *Notes) Warn(line string) {
	n.Warnings = KeepNote(n.Warnings, line)
}

// Unknown keeps a line the reader didn't recognize, up to MaxUnrecognized, and counts it.
func (n *Notes) Unknown(line string) {
	n.UnrecognizedCount++

	if len(n.Unrecognized) < MaxUnrecognized {
		n.Unrecognized = append(n.Unrecognized, line)
	}
}

// KeepNote appends line to list, unless it holds MaxNotes lines already.
func KeepNote(list []string, line string) []string {
	if len(list) < MaxNotes {
		return append(list, line)
	}

	return list
}
