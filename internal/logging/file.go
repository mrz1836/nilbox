package logging

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/mrz1836/nilbox/internal/appdata"
)

// file is a log file capped at a size: a write that would take it past the cap first
// renames it to <name>.1, shifting the older copies along and dropping the oldest, and
// starts a new one. Writes go straight to the file, unbuffered, so a crash loses no
// record and Close has nothing to flush. It is safe for concurrent use.
type file struct {
	path     string
	maxBytes int64

	// mu guards the fields below. f is nil after Close, and after a rotation that could
	// not open the new file, which the next write tries again.
	mu     sync.Mutex
	f      *os.File
	size   int64
	closed bool
}

// openFile opens FileName in dir for appending, creating dir and the file readable only
// by the user.
func openFile(dir string, maxBytes int64) (*file, error) {
	if err := os.MkdirAll(dir, appdata.PrivateDirPerm); err != nil {
		return nil, fmt.Errorf("create the logs folder: %w", err)
	}

	w := &file{path: filepath.Join(dir, FileName), maxBytes: maxBytes}
	if err := w.open(); err != nil {
		return nil, err
	}

	return w, nil
}

// Write appends p, one record, rotating first when p would take the file past its cap. A
// record is never split, and one larger than the cap still goes into a file of its own.
// When the file cannot be renamed, p goes on the end of it as it is, and the next write
// tries again. Writes after Close fail.
func (w *file) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return 0, fs.ErrClosed
	}

	if w.f != nil && w.size > 0 && w.size+int64(len(p)) > w.maxBytes {
		w.rotate()
	}

	if w.f == nil {
		if err := w.open(); err != nil {
			return 0, err
		}
	}

	n, err := w.f.Write(p)
	w.size += int64(n)

	if err != nil {
		return n, fmt.Errorf("write the log file: %w", err)
	}

	return n, nil
}

// Close closes the file. Writes after it fail.
func (w *file) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return nil
	}

	w.closed = true

	if w.f == nil {
		return nil
	}

	err := w.f.Close()
	w.f = nil

	if err != nil {
		return fmt.Errorf("close the log file: %w", err)
	}

	return nil
}

// rotate closes the file and shifts the files along, <name>.1 to <name>.2 and <name> to
// <name>.1, leaving the write that follows to open a new <name>. Nothing can be logged
// about a failure from inside the log, so a file that cannot be renamed is opened again
// and left to grow.
func (w *file) rotate() {
	_ = w.f.Close() // a failure cannot be logged from here, and the write that follows opens the file again
	w.f = nil

	for i := KeptFiles - 1; i > 0; i-- {
		_ = os.Rename(w.name(i-1), w.name(i)) // a missing older copy is expected, and a stuck one only grows
	}
}

// open opens the file at path for appending, creating it readable only by the user, and
// reads its size.
func (w *file) open() error {
	f, err := os.OpenFile(w.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, appdata.PrivateFilePerm)
	if err != nil {
		return fmt.Errorf("open the log file: %w", err)
	}

	info, err := f.Stat()
	if err != nil {
		_ = f.Close() // the stat failure is what is reported

		return fmt.Errorf("read the log file's size: %w", err)
	}

	w.f, w.size = f, info.Size()

	return nil
}

// name returns the path of the i-th file: the live one for 0, else <name>.<i>.
func (w *file) name(i int) string {
	if i == 0 {
		return w.path
	}

	return w.path + "." + strconv.Itoa(i)
}
