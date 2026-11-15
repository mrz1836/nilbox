package update

import (
	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/updateprefs"
)

// Flavor is the kind of build the app is, which says whether and where it updates from.
type Flavor string

// The flavors, stamped into the build (main.flavor); an empty or unknown one is source.
const (
	// FlavorSource is a build made from source (magex app:build, app:run, app:dev): it never
	// checks for updates.
	FlavorSource Flavor = "source"

	// FlavorRelease is a release magex app:package made: it reads GitHub's latest release
	// and trusts only ReleaseKey.
	FlavorRelease Flavor = "release"

	// FlavorRehearsal is a pair of builds magex app:rehearse makes to try an update end to
	// end on one Mac: it reads its releases from a local folder and trusts the key the
	// build was given.
	FlavorRehearsal Flavor = "rehearsal"
)

// State is where the app's update stands.
type State string

// The states. A failure changes none of them: it is the status's LastError, so an update
// that is ready stays ready when a later check fails.
const (
	// StateOff is a build that can't update (Status.Off says why).
	StateOff State = "off"

	// StateIdle is no newer version known: up to date, or never checked.
	StateIdle State = "idle"

	// StateChecking is a check running while no newer version is known.
	StateChecking State = "checking"

	// StateDownloading is the newer version downloading (Status.Progress).
	StateDownloading State = "downloading"

	// StateAvailable is a newer version found and not downloaded yet, as Just tell me and
	// Don't check leave one: Update downloads it, then restarts.
	StateAvailable State = "available"

	// StateReady is a newer version downloaded and checked, waiting for a restart.
	StateReady State = "ready"

	// StateInstalling is a restart under way: the data backed up, the update staged, the
	// app about to quit.
	StateInstalling State = "installing"

	// StateFinishing is a restart whose quit was held (a modal panel, say) after the
	// runtime's helper started: the update stays staged and recorded, and the app quits on
	// its own within moments so the helper installs it.
	StateFinishing State = "finishing"
)

// LocationKind says where the running app is, which decides whether it can replace itself.
type LocationKind string

// The kinds of location.
const (
	// LocationApplications is the Applications folder, or a folder in it.
	LocationApplications LocationKind = "applications"

	// LocationUserApplications is the Applications folder in the home folder.
	LocationUserApplications LocationKind = "userApplications"

	// LocationTranslocated is a random read-only path macOS runs a downloaded app from
	// until it is moved (App Translocation).
	LocationTranslocated LocationKind = "translocated"

	// LocationDiskImage is a mounted disk image or another volume.
	LocationDiskImage LocationKind = "diskImage"

	// LocationElsewhere is anywhere else, such as Downloads, or no bundle at all.
	LocationElsewhere LocationKind = "elsewhere"
)

// Location is where the running app is.
type Location struct {
	// OK reports that the app can update where it is: in an Applications folder whose
	// folder it can write to, where the update is staged beside it.
	OK bool `json:"ok"`

	// Kind says where it is.
	Kind LocationKind `json:"kind"`

	// Target is where moving it would put it, with ~ for home: the Applications folder
	// when the user can write to it, else the one in the home folder.
	Target string `json:"target"`
}

// InstallFailure is the update the last restart didn't install, or, in a later run, the
// newest version found, when an earlier restart failed to install it for good.
type InstallFailure struct {
	// Version is the version it was meant to install, or "" when the last restart
	// installed what it meant to, or there was none.
	Version string `json:"version"`

	// Reason says why, in words, as the update's helper's log tells it.
	Reason string `json:"reason"`

	// Log reports that the helper's log was kept, which ShowInstallLog shows.
	Log bool `json:"log"`

	// Retryable reports that trying the same update again may work: the failure was the
	// moment's (the app didn't quit in time, macOS kept it from being replaced, it couldn't
	// be copied aside, or the helper's log doesn't say), and the update stays offered. A
	// failure of the update itself (it failed its signature check, couldn't be installed, or
	// wouldn't open) fails the same way every time: that update is never offered again, and
	// the release page is the way to get it.
	Retryable bool `json:"retryable"`
}

// Status is the app's update as the window shows it. Every field is a plain value.
type Status struct {
	// Flavor is the kind of build, and Enabled whether it can update: only a release or a
	// rehearsal build with a key can. Off says why not, in words, or "".
	Flavor  Flavor `json:"flavor"`
	Enabled bool   `json:"enabled"`
	Off     string `json:"off"`

	// Current is the running release's version, such as 0.2.0.
	Current string `json:"current"`

	// Mode is what the app does when a new version is out (the setting).
	Mode updateprefs.Mode `json:"mode"`

	// State is where the update stands, and Checking whether a check runs now, in any
	// state.
	State    State `json:"state"`
	Checking bool  `json:"checking"`

	// Progress is how much of the download has arrived, 0 to 100, while downloading.
	Progress int `json:"progress"`

	// Latest is the newer version found, or "".
	Latest string `json:"latest"`

	// ReleaseURL is the release page of Latest, or of Current when none is found, for
	// What's new; "" for a build that can't update.
	ReleaseURL string `json:"releaseUrl"`

	// LastChecked is when the last check that reached the releases ended, as RFC 3339, or
	// "", and LastError what went wrong last, in words, or "" once a later step worked.
	LastChecked string `json:"lastChecked"`
	LastError   string `json:"lastError"`

	// BannerHidden reports that the user chose Later for Latest less than a day ago.
	BannerHidden bool `json:"bannerHidden"`

	// JustUpdated is the version this launch updated to, until AckUpdated: the window
	// says so once.
	JustUpdated string `json:"justUpdated"`

	// InstallFailed is the update the last restart didn't install, if any.
	InstallFailed InstallFailure `json:"installFailed"`

	// Location is where the running app is.
	Location Location `json:"location"`
}

// ReleaseURL is the GitHub page of the release of version.
func ReleaseURL(version string) string {
	return brand.Repository + "/releases/tag/v" + version
}
