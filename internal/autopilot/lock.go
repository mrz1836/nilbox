package autopilot

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/mrz1836/nilbox/internal/appdata"
)

// errLocked is a lock another process, or another open of the file, holds.
var errLocked = errors.New("another copy holds the lock")

// Lock is the one-engine-per-data-folder lock: an exclusive advisory lock (flock) on a
// file in the data folder, so a second copy of the app on the same data, such as a build
// from source beside the app, or a restart that overlaps the old process, shows the
// routines but starts nothing. The operating system lets go of it when its process ends,
// so a crash never leaves it held. A Lock with no path always holds, for a build with no
// data folder of its own.
type Lock struct {
	path string

	mu   sync.Mutex
	file *os.File
}

// NewLock returns the lock on the file at path, not yet held.
func NewLock(path string) *Lock {
	return &Lock{path: path}
}

// TryLock takes the lock if it is free, and reports whether this copy holds it now.
func (l *Lock) TryLock() (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.path == "" || l.file != nil {
		return true, nil
	}

	if err := os.MkdirAll(filepath.Dir(l.path), appdata.PrivateDirPerm); err != nil {
		return false, fmt.Errorf("make the lock's folder: %w", err)
	}

	f, err := os.OpenFile(l.path, os.O_RDWR|os.O_CREATE, appdata.PrivateFilePerm)
	if err != nil {
		return false, fmt.Errorf("open the lock: %w", err)
	}

	if err = flock(f); err != nil {
		_ = f.Close()

		if errors.Is(err, errLocked) {
			return false, nil
		}

		return false, fmt.Errorf("take the lock: %w", err)
	}

	l.file = f

	return true, nil
}

// Held reports whether this copy holds the lock.
func (l *Lock) Held() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.path == "" || l.file != nil
}

// Unlock lets go of the lock, if held.
func (l *Lock) Unlock() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file == nil {
		return
	}

	_ = funlock(l.file)
	_ = l.file.Close()
	l.file = nil
}
