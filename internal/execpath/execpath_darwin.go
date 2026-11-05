//go:build darwin

package execpath

import "path/filepath"

// wellKnownDirs returns the macOS directories where command-line tools are commonly
// installed but which launchd's PATH for GUI apps does not include.
func wellKnownDirs(home string) []string {
	const maxDirs = 5

	dirs := make([]string, 0, maxDirs)
	dirs = append(dirs, "/opt/homebrew/bin", "/usr/local/bin")

	if home == "" {
		return dirs
	}

	return append(dirs,
		filepath.Join(home, ".local", "bin"),
		filepath.Join(home, "bin"),
		filepath.Join(home, "go", "bin"),
	)
}
