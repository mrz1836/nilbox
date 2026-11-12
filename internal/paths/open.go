package paths

import (
	"errors"
	"io"
	"os"
	"syscall"
)

// ErrNotRegular is returned for a path that is there but isn't a regular file, such as a
// folder, a named pipe, or a device, which is never read.
var ErrNotRegular = errors.New("not a regular file")

// OpenRegular opens the regular file at path to read, following a link, and refuses
// anything else with ErrNotRegular. A path that isn't one is never opened, and one swapped
// for a pipe or a device since it was looked at is opened without waiting, as opening a
// pipe that nothing writes to otherwise would, and refused once open. Every file a
// repository or an agent controls is read through it, so none can hang a read.
func OpenRegular(path string) (*os.File, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if !info.Mode().IsRegular() {
		return nil, ErrNotRegular
	}

	return openChecked(path)
}

// openChecked opens path to read without waiting, and keeps it open only when what it
// opened is a regular file.
func openChecked(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0) //nolint:gosec // only read, and only a regular file is kept
	if err != nil {
		return nil, err
	}

	info, err := f.Stat()
	if err == nil && !info.Mode().IsRegular() {
		err = ErrNotRegular
	}

	if err != nil {
		_ = f.Close()

		return nil, err
	}

	return f, nil
}

// ReadHead reads up to limit bytes from the start of the regular file at path, as
// OpenRegular opens it.
func ReadHead(path string, limit int64) ([]byte, error) {
	f, err := OpenRegular(path)
	if err != nil {
		return nil, err
	}

	defer func() { _ = f.Close() }()

	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return nil, err
	}

	return data, nil
}

// ReadTail reads up to limit bytes from the end of the regular file at path, as
// OpenRegular opens it, and reports whether that is less than the whole file. A file that
// shrinks while it is read gives the bytes that were there.
func ReadTail(path string, limit int64) ([]byte, bool, error) {
	f, err := OpenRegular(path)
	if err != nil {
		return nil, false, err
	}

	defer func() { _ = f.Close() }()

	info, err := f.Stat()
	if err != nil {
		return nil, false, err
	}

	offset := max(info.Size()-max(limit, 0), 0)

	data := make([]byte, info.Size()-offset)

	n, err := f.ReadAt(data, offset)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, false, err
	}

	return data[:n], offset > 0, nil
}
