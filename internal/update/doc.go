// Package update is how the app updates itself.
//
// Every release attaches a manifest beside its zip: the manifest's format, the message its
// Ed25519 signature covers, the names of the release's files, and the checks that make a
// manifest and the zip it names trustworthy live here. The release targets in magefiles
// build and verify releases with them, so the packaging and the app never disagree on what
// a valid release is.
//
// The Engine checks the latest release, downloads its zip into the Cache, checks it against
// the manifest and the key the build trusts (ReleaseKey for a release; a rehearsal's own),
// and installs it through the Installer the desktop shell attaches, after backing the
// database up and letting the quit through. What a build does is its Flavor's: a source
// build never updates. The Worker runs the timed checks, the Service is the window's face,
// and the next start reads how the restart went (JustUpdated, InstallFailed) and cleans up
// what the desktop runtime's updater leaves behind.
//
// It imports no service and speaks no HTTP: the app reads the manifest and the zip through
// a Feed, which core adapts over the GitHub client's release downloads, and keeps its state
// in the update.* preferences through Memory.
package update
