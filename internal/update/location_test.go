package update_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/update"
)

const testHome = "/Users/example"

func TestClassify(t *testing.T) {
	t.Parallel()

	app := brand.Name + ".app"

	for bundle, want := range map[string]update.LocationKind{
		"/Applications/" + app:            update.LocationApplications,
		"/Applications/Utilities/" + app:  update.LocationApplications,
		testHome + "/Applications/" + app: update.LocationUserApplications,
		"/private/var/folders/xy/abc123/T/AppTranslocation/0A1B2C3D-4E5F/d/" + app: update.LocationTranslocated,
		"/Volumes/" + brand.Name + "/" + app:                                       update.LocationDiskImage,
		testHome + "/Downloads/" + app:                                             update.LocationElsewhere,
		"/Applications" + app:                                                      update.LocationElsewhere,
		testHome + "/ApplicationsOld/" + app:                                       update.LocationElsewhere,
		"":                                                                         update.LocationElsewhere,
	} {
		assert.Equal(t, want, update.Classify(bundle, testHome), bundle)
	}
}

func TestBundleOf(t *testing.T) {
	t.Parallel()

	app := "/Applications/" + brand.Name + ".app"

	assert.Equal(t, app, update.BundleOf(app+"/Contents/MacOS/"+brand.Slug))
	assert.Empty(t, update.BundleOf("/usr/local/bin/"+brand.Slug), "outside a bundle")
	assert.Empty(t, update.BundleOf("/tmp/build/Contents/MacOS/"+brand.Slug), "not a .app")
	assert.Empty(t, update.BundleOf(app+"/Contents/Resources/"+brand.Slug))
}

func TestLocate(t *testing.T) {
	t.Parallel()

	app := brand.Name + ".app"
	everywhere := func(string) bool { return true }
	notSystem := func(dir string) bool { return dir != "/Applications" }

	assert.Equal(t, update.Location{
		OK: true, Kind: update.LocationApplications, Target: "/Applications/" + app,
	}, update.Locate("/Applications/"+app, testHome, everywhere))

	assert.Equal(t, update.Location{
		OK: false, Kind: update.LocationApplications, Target: "~/Applications/" + app,
	}, update.Locate("/Applications/"+app, testHome, notSystem), "a standard user can't stage an update there")

	assert.Equal(t, update.Location{
		OK: true, Kind: update.LocationUserApplications, Target: "~/Applications/" + app,
	}, update.Locate(testHome+"/Applications/"+app, testHome, notSystem))

	assert.Equal(t, update.Location{
		OK: false, Kind: update.LocationElsewhere, Target: "/Applications/" + app, Movable: true,
	}, update.Locate(testHome+"/Downloads/"+app, testHome, everywhere))

	assert.Equal(t, update.Location{
		OK: false, Kind: update.LocationElsewhere, Target: "~/Applications/" + app,
	}, update.Locate("", testHome, notSystem), "no bundle, nothing to move")
}

// A rehearsal build updates itself in its own folder as in an Applications folder, and a
// release never does: moving either still puts it in Applications.
func TestARehearsalUpdatesInItsOwnFolderAndAReleaseNever(t *testing.T) {
	t.Parallel()

	app := brand.Name + ".app"

	tests := []struct {
		name   string
		flavor update.Flavor
		bundle string // under the rig's root
		want   bool
	}{
		{name: "a rehearsal in its folder", flavor: update.FlavorRehearsal, bundle: "rehearsal/A/" + app, want: true},
		{name: "a rehearsal elsewhere", flavor: update.FlavorRehearsal, bundle: "Downloads/" + app},
		{name: "a rehearsal in a folder named like its own", flavor: update.FlavorRehearsal, bundle: "rehearsal-old/A/" + app},
		{name: "a release in a rehearsal's folder", flavor: update.FlavorRelease, bundle: "rehearsal/A/" + app},
		{name: "a source build in a rehearsal's folder", flavor: update.FlavorSource, bundle: "rehearsal/A/" + app},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			r := newRig(t, func(c *update.Config) {
				root := filepath.Dir(c.Applications)
				c.Flavor, c.Bundle = tt.flavor, filepath.Join(root, tt.bundle)
				c.RehearsalFolder = filepath.Join(root, "rehearsal")
			})

			got := r.status(t).Location

			assert.Equal(t, tt.want, got.OK)
			assert.Equal(t, update.LocationElsewhere, got.Kind, "the kind is its path's")
			assert.Equal(t, filepath.Join(r.cfg.Applications, app), got.Target, "moving it still puts it in Applications")
		})
	}
}
