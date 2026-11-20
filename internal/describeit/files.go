package describeit

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/appdata"
	"github.com/mrz1836/nilbox/internal/autopilot/kit"
	"github.com/mrz1836/nilbox/internal/paths"
)

// errTooLarge is returned for a file past what it may hold.
var errTooLarge = errors.New("the file is larger than it may be")

// ensureDir makes the folder dir, and every folder above it, the user's alone, and
// replaces a link left where it should be with a folder, so nothing the app writes there
// lands elsewhere.
func ensureDir(dir string) error {
	if info, err := os.Lstat(dir); err == nil && info.Mode()&fs.ModeSymlink != 0 {
		if err = os.Remove(dir); err != nil {
			return fmt.Errorf("replace the link at %s: %w", filepath.Base(dir), err)
		}
	}

	if err := os.MkdirAll(dir, appdata.PrivateDirPerm); err != nil {
		return fmt.Errorf("make %s: %w", filepath.Base(dir), err)
	}

	return nil
}

// readFile reads the regular file at path, as paths.ReadHead does, refusing one larger
// than limit with errTooLarge. A missing file is an error matching fs.ErrNotExist.
func readFile(path string, limit int64) ([]byte, error) {
	data, err := paths.ReadHead(path, limit+1)
	if err != nil {
		return nil, err
	}

	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: %s holds more than %d bytes", errTooLarge, filepath.Base(path), limit)
	}

	return data, nil
}

// writeIfChanged writes data to the user-only file at path, whole, unless the file holds
// it already, making its folder first, and reports whether it wrote.
func writeIfChanged(path string, data []byte) (bool, error) {
	if old, err := readFile(path, int64(len(data))); err == nil && bytes.Equal(old, data) {
		return false, nil
	}

	if err := ensureDir(filepath.Dir(path)); err != nil {
		return false, err
	}

	if err := appdata.WriteFile(path, data); err != nil {
		return false, err
	}

	return true, nil
}

// proposalIDs returns the IDs of the proposal files in dir, oldest first: each a regular
// file, never a link, named <ID>.json for an ID the tool mode makes. A folder that isn't
// there holds none.
func proposalIDs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return []string{}, nil
		}

		return nil, err
	}

	ids := make([]string, 0, len(entries))

	for _, e := range entries {
		id, ok := strings.CutSuffix(e.Name(), ".json")
		if !ok || !kit.ValidProposalID(id) || !e.Type().IsRegular() {
			continue
		}

		ids = append(ids, id)
	}

	slices.Sort(ids)

	return ids, nil
}
