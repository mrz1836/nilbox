package appdata

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// DataDirFlag runs the app on the data directory it names, as --data-dir=<absolute path>,
// in place of the user's own: a sandbox, such as the one magex app:fresh opens, whose
// database, copies, and logs all stay in that directory, so the user's own data is never
// touched.
const DataDirFlag = "--data-dir"

// ErrDataDirFlag is returned for a DataDirFlag that names no absolute path.
var ErrDataDirFlag = errors.New("--data-dir needs an absolute path, as --data-dir=/path/to/folder")

// FromArgs returns the data directory args name with DataDirFlag, cleaned, or "" when
// they name none. A flag with no value, or with a path that is not absolute, is
// ErrDataDirFlag: a relative path would depend on where the app happened to start.
func FromArgs(args []string) (string, error) {
	for _, arg := range args {
		if arg == DataDirFlag {
			return "", ErrDataDirFlag
		}

		value, ok := strings.CutPrefix(arg, DataDirFlag+"=")
		if !ok {
			continue
		}

		if value == "" || !filepath.IsAbs(value) {
			return "", fmt.Errorf("%w (got %q)", ErrDataDirFlag, value)
		}

		return filepath.Clean(value), nil
	}

	return "", nil
}
