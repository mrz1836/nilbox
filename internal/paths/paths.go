package paths

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

var (
	// ErrEmpty is returned for an empty path.
	ErrEmpty = errors.New("path is empty")

	// ErrNoHome is returned when a path starts with ~ but the home directory is unknown.
	ErrNoHome = errors.New("home directory is unknown")

	// ErrRelative is returned for a path that is neither absolute nor ~-relative.
	ErrRelative = errors.New("path must be absolute or start with ~/")
)

// ExpandHome turns a typed path into an absolute, cleaned one: surrounding space is
// trimmed, and a leading ~ (alone or followed by /) becomes home. Any other path must
// already be absolute; "~user/..." is not expanded and so is refused as relative.
func ExpandHome(path, home string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", ErrEmpty
	}

	if path == "~" || strings.HasPrefix(path, "~/") {
		if home == "" {
			return "", ErrNoHome
		}

		path = filepath.Join(home, path[1:])
	}

	if !filepath.IsAbs(path) {
		return "", fmt.Errorf("%w: %s", ErrRelative, path)
	}

	return filepath.Clean(path), nil
}

// Tilde shows path relative to home as ~/..., or unchanged when it is outside home or
// home is unknown.
func Tilde(path, home string) string {
	if home == "" {
		return path
	}

	rel, err := filepath.Rel(filepath.Clean(home), filepath.Clean(path))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return path
	}

	if rel == "." {
		return "~"
	}

	return "~/" + filepath.ToSlash(rel)
}

// Resolved returns path cleaned, with symbolic links resolved. A path that does not exist
// is resolved through its nearest existing parent, so it still matches the form the OS and
// git report for it; a path with no existing parent is only cleaned.
func Resolved(path string) string {
	clean := filepath.Clean(path)

	for dir := clean; ; dir = filepath.Dir(dir) {
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			rest, relErr := filepath.Rel(dir, clean)
			if relErr != nil {
				return clean
			}

			return filepath.Join(r, rest)
		}

		if filepath.Dir(dir) == dir {
			return clean
		}
	}
}

// Same reports whether a and b name the same folder: they are equal once cleaned, or once
// their symbolic links are resolved.
func Same(a, b string) bool {
	if a == "" || b == "" {
		return a == b
	}

	return filepath.Clean(a) == filepath.Clean(b) || Resolved(a) == Resolved(b)
}
