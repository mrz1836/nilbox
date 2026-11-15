package desktop

import (
	"path/filepath"
	"strings"

	"github.com/mrz1836/nilbox/internal/appdata"
)

// OpenBin is macOS's open, which opens an app bundle as Finder does.
const OpenBin = "/usr/bin/open"

// RelaunchArgs works out the command that opens the app again once it has quit, as a reset
// the user asked for does, so its next start carries the reset out: `open -n <bundle>`,
// the bundle the running executable exe is in, with --args --data-dir=<dataDir> for a
// sandbox's data directory ("" for the user's own). open needs -n because the app that
// asks is still running as it does, and would be brought forward instead. An executable
// that isn't inside a .app bundle, as `magex app:dev` runs it, has nothing to open: ok is
// false, and the app only quits.
func RelaunchArgs(exe, dataDir string) (argv []string, ok bool) {
	bundle, ok := bundleOf(exe)
	if !ok {
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

	if filepath.Base(macOS) != "MacOS" || filepath.Base(contents) != "Contents" ||
		!strings.HasSuffix(bundle, ".app") || filepath.Base(bundle) == ".app" {
		return "", false
	}

	return bundle, true
}
