package desktop

import (
	"path/filepath"
	"strings"

	"github.com/mrz1836/nilbox/internal/appdata"
)

// OpenBin is macOS's open, which opens an app bundle as Finder does.
const OpenBin = "/usr/bin/open"

// RelaunchArgs works out the command that opens the app again once it has quit, as a reset
// the user asked for does, so its next start carries the reset out: OpenArgs of the bundle
// the running executable exe is in. An executable that isn't inside a .app bundle, as `magex
// app:dev` runs it, has nothing to open: ok is false, and the app only quits.
func RelaunchArgs(exe, dataDir string) (argv []string, ok bool) {
	bundle, ok := bundleOf(exe)
	if !ok {
		return nil, false
	}

	return OpenArgs(bundle, dataDir)
}

// OpenArgs works out the command that opens the app bundle at bundle once this app has
// quit: `open -n <bundle>`, with --args --data-dir=<dataDir> for a sandbox's data directory
// ("" for the user's own). open needs -n because the app that asks is still running as it
// does, and, with the same bundle identifier, would be brought forward instead. A path that
// isn't the absolute, clean path of a .app bundle opens nothing: ok is false.
func OpenArgs(bundle, dataDir string) (argv []string, ok bool) {
	if !isBundle(bundle) {
		return nil, false
	}

	argv = []string{OpenBin, "-n", bundle}
	if dataDir != "" {
		argv = append(argv, "--args", appdata.DataDirFlag+"="+dataDir)
	}

	return argv, true
}

// bundleOf returns the .app bundle the executable exe runs from, as
// <bundle>.app/Contents/MacOS/<exe>, and whether it runs from one.
func bundleOf(exe string) (string, bool) {
	if exe == "" || !filepath.IsAbs(exe) {
		return "", false
	}

	macOS := filepath.Dir(filepath.Clean(exe))
	contents := filepath.Dir(macOS)
	bundle := filepath.Dir(contents)

	if filepath.Base(macOS) != "MacOS" || filepath.Base(contents) != "Contents" || !isBundle(bundle) {
		return "", false
	}

	return bundle, true
}

// isBundle reports whether path is the absolute, clean path of a .app bundle.
func isBundle(path string) bool {
	return filepath.IsAbs(path) && filepath.Clean(path) == path && strings.HasSuffix(path, ".app") &&
		filepath.Base(path) != ".app"
}
