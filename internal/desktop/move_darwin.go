//go:build darwin

package desktop

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const (
	// quarantineAttr is the extended attribute macOS marks a download with: Gatekeeper asks
	// about a marked app before it first opens, and runs one that wasn't moved by Finder
	// from a hidden, read-only copy (App Translocation).
	quarantineAttr = "com.apple.quarantine"

	// qtnMoved is the flag macOS adds to a mark's flags, its first field in hex
	// ("0181;…"), once the user moves the app with Finder, after which the app opens where
	// it is: every marked app dragged into Applications carries it (0181, 01c1, 03c1),
	// while a download not moved yet has none (0081, 0083).
	qtnMoved = 0x0100

	// maxQuarantine bounds the read of a mark, a few dozen bytes.
	maxQuarantine = 1 << 10
)

// translocates reports whether macOS would run the bundle at bundle from a hidden, read-only
// copy (App Translocation): its own folder carries the quarantine mark, and the mark's flags
// lack qtnMoved, as a copy put there in Terminal (cp, mv) does. A mark that can't be read as one macOS writes counts as none, since macOS can't read
// it either. It reads the bundle's link itself, never what it points to, and changes nothing.
func translocates(bundle string) (bool, error) {
	mark := make([]byte, maxQuarantine)

	n, err := unix.Lgetxattr(bundle, quarantineAttr, mark)

	switch {
	case errors.Is(err, unix.ENOATTR):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("read the quarantine mark of %s: %w", bundle, err)
	}

	field, _, _ := strings.Cut(string(mark[:n]), ";")

	flags, err := strconv.ParseUint(field, 16, 32)
	if err != nil {
		return false, nil //nolint:nilerr // a mark macOS can't read moves nothing to a hidden folder
	}

	return flags&qtnMoved == 0, nil
}

// unquarantine takes the quarantine mark off every file of the bundle at bundle, a link's
// own rather than what it points to, so a copy of the app the user already opened opens
// from Applications without being translocated or asked about again. A file without the
// mark is left as it is.
func unquarantine(bundle string) error {
	err := filepath.WalkDir(bundle, func(path string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if err = unix.Lremovexattr(path, quarantineAttr); err != nil && !errors.Is(err, unix.ENOATTR) {
			return fmt.Errorf("take the quarantine mark off %s: %w", path, err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("take the quarantine mark off the copy: %w", err)
	}

	return nil
}
