package update

import (
	"path/filepath"
	"strings"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/paths"
)

// systemApplications is the Applications folder every user shares.
const systemApplications = "/Applications"

// classify says where the bundle at bundle is: an Applications folder (the system's, at
// applications, or the one in home), a path macOS translocated it to, a mounted volume such
// as a disk image, or elsewhere, which "" (no bundle) is too.
func classify(bundle, home, applications string) LocationKind {
	if bundle == "" {
		return LocationElsewhere
	}

	clean := filepath.Clean(bundle)

	switch {
	case strings.Contains(clean, "/AppTranslocation/"):
		return LocationTranslocated
	case strings.HasPrefix(clean, "/Volumes/"):
		return LocationDiskImage
	case strings.HasPrefix(clean, filepath.Clean(applications)+"/"):
		return LocationApplications
	case home != "" && strings.HasPrefix(clean, filepath.Join(home, "Applications")+"/"):
		return LocationUserApplications
	default:
		return LocationElsewhere
	}
}

// BundleOf returns the .app bundle the executable at exe runs from, or "" when it runs
// outside one, as a build from magex app:dev does.
func BundleOf(exe string) string {
	macOS := filepath.Dir(filepath.Clean(exe))
	contents := filepath.Dir(macOS)
	bundle := filepath.Dir(contents)

	if filepath.Base(macOS) != "MacOS" || filepath.Base(contents) != "Contents" || filepath.Ext(bundle) != ".app" {
		return ""
	}

	return bundle
}

// locate works out where the bundle is, whether the app can update there (in an
// Applications folder, or in rehearsal, a rehearsal's own folder, "" for none, whose folder
// the user can write to, where the update is staged), where moving it would put it
// (moveTarget), and whether Move to Applications can: a bundle that can't update where it
// is, outside every Applications folder. A bundle in rehearsal keeps the kind its path has,
// and moving it still puts it in Applications.
func locate(bundle, home, applications, rehearsal string, writable func(string) bool) Location {
	kind := classify(bundle, home, applications)
	inApplications := kind == LocationApplications || kind == LocationUserApplications
	ok := (inApplications || within(bundle, rehearsal)) && writable(filepath.Dir(filepath.Clean(bundle)))

	return Location{
		OK:      ok,
		Kind:    kind,
		Target:  paths.Tilde(moveTarget(home, applications, writable), home),
		Movable: !ok && !inApplications && bundle != "",
	}
}

// moveTarget is where moving the app puts it: in the system's Applications folder,
// applications, when the user can write to it, else in the one in home.
func moveTarget(home, applications string, writable func(string) bool) string {
	if writable(applications) {
		return filepath.Join(applications, brand.Name+".app")
	}

	return filepath.Join(home, "Applications", brand.Name+".app")
}

// within reports whether path lies inside folder; nothing lies inside "".
func within(path, folder string) bool {
	if path == "" || folder == "" {
		return false
	}

	return strings.HasPrefix(filepath.Clean(path), filepath.Clean(folder)+string(filepath.Separator))
}
