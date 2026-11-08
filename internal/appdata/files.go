package appdata

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

var (
	// ErrTooLarge is returned by ReadFile for a file larger than the limit it was given.
	ErrTooLarge = errors.New("the file is too large")

	// ErrNotAFile is returned by ReadFile for a path that is not a regular file.
	ErrNotAFile = errors.New("not a file")
)

// WriteFile writes data to a user-only file at path: to a temporary file beside it
// first, flushed to disk, then renamed over path, so a reader sees the old file or the
// new one whole, never part of either, whatever stops the write.
func WriteFile(path string, data []byte) error {
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}

	tmp, err := os.CreateTemp(dir, "."+base+".*.tmp")
	if err != nil {
		return fmt.Errorf("write %s: %w", base, err)
	}

	name := tmp.Name()
	fail := func(err error) error {
		return errors.Join(fmt.Errorf("write %s: %w", base, err), tmp.Close(), os.Remove(name))
	}

	if err = tmp.Chmod(PrivateFilePerm); err != nil {
		return fail(err)
	}

	if _, err = tmp.Write(data); err != nil {
		return fail(err)
	}

	if err = tmp.Sync(); err != nil {
		return fail(err)
	}

	if err = tmp.Close(); err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", base, err), os.Remove(name))
	}

	if err = os.Rename(name, path); err != nil {
		return errors.Join(fmt.Errorf("write %s: %w", base, err), os.Remove(name))
	}

	return nil
}

// ReadFile reads the regular file at path, refusing one larger than limit bytes with
// ErrTooLarge before reading it. A missing file is an error matching fs.ErrNotExist.
func ReadFile(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path) //nolint:gosec // a file the app keeps, or one the user chose
	if err != nil {
		return nil, fmt.Errorf("read the file: %w", err)
	}

	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("read the file: %w", err)
	}

	switch {
	case !info.Mode().IsRegular():
		return nil, fmt.Errorf("read the file: %w", ErrNotAFile)
	case info.Size() > limit:
		return nil, ErrTooLarge
	}

	var buf bytes.Buffer
	if _, err = buf.ReadFrom(f); err != nil {
		return nil, fmt.Errorf("read the file: %w", err)
	}

	return buf.Bytes(), nil
}
