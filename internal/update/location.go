package update

import (
	"path/filepath"
	"strings"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/paths"
)

// systemApplications is the Applications folder every user shares.
const systemApplications = "/Applications"

// Classify says where the bundle at bundle is: an Applications folder (the system's, or
// the one in home), a path macOS translocated it to, a mounted volume such as a disk image,
// or elsewhere, which "" (no bundle) is too.
func Classify(bundle, home string) LocationKind {
	return classify(bundle, home, systemApplications)
}

// classify is Classify with the system's Applications folder at applications.
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
// Applications folder whose folder the user can write to, where the update is staged), and
// where moving it would put it: the system's Applications folder, applications, when
// writable, else the one in home.
func locate(bundle, home, applications string, writable func(string) bool) Location {
	kind := classify(bundle, home, applications)
	inApplications := kind == LocationApplications || kind == LocationUserApplications

	target := filepath.Join(home, "Applications", brand.Name+".app")
	if writable(applications) {
		target = filepath.Join(applications, brand.Name+".app")
	}

	return Location{
		OK:     inApplications && writable(filepath.Dir(filepath.Clean(bundle))),
		Kind:   kind,
		Target: paths.Tilde(target, home),
	}
}
