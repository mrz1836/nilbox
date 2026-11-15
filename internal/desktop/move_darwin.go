//go:build darwin

package desktop

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// quarantineAttr is the extended attribute macOS marks a download with: Gatekeeper asks
// about a marked app before it first opens, and runs one that wasn't moved by Finder from a
// hidden, read-only copy (App Translocation).
const quarantineAttr = "com.apple.quarantine"

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
