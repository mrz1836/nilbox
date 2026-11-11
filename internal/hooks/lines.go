package hooks

import (
	"sync"

	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/textutil"
)

const (
	// maxNotes caps the warnings, errors, and tool installs an output keeps: the first.
	maxNotes = 200

	// maxDetail caps the lines a check's detail keeps.
	maxDetail = 40
)

// lineReader is the part a reader of a run's output shares: an io.Writer for a run's
// Spec.Output, fed the log's bytes in order and split anywhere, which hands each whole line,
// as textutil.LineSplitter cleans it, to read, under mu, which the reader's own methods
// hold too.
type lineReader struct {
	mu sync.Mutex

	// read reads one line; the reader that embeds the lineReader sets it.
	read func(line string)

	lines textutil.LineSplitter
}

// Write reads p, the next part of the output. It never fails.
func (r *lineReader) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lines.Write(p, r.read)

	return len(p), nil
}

// Flush reads the last line when the output ended without a newline. Call it once the
// output has ended.
func (r *lineReader) Flush() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.lines.Flush(r.read)
}

// notes are the lines a reader keeps for a run's result: the tool's warnings, and the
// lines it didn't recognize, of which it keeps the first scriptrun.MaxUnrecognized and
// counts them all.
type notes struct {
	warnings          []string
	unrecognized      []string
	unrecognizedCount int
}

// warn keeps a warning, up to maxNotes.
func (n *notes) warn(line string) {
	if len(n.warnings) < maxNotes {
		n.warnings = append(n.warnings, line)
	}
}

// unknown keeps a line the reader didn't recognize.
func (n *notes) unknown(line string) {
	n.unrecognizedCount++

	if len(n.unrecognized) < scriptrun.MaxUnrecognized {
		n.unrecognized = append(n.unrecognized, line)
	}
}

// keepNote appends line to list, up to maxNotes.
func keepNote(list []string, line string) []string {
	if len(list) < maxNotes {
		return append(list, line)
	}

	return list
}
