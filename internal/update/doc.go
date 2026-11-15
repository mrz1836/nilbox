// Package update is how the app updates itself. For now it holds the manifest every
// release attaches beside its zip: the manifest's format, the message its Ed25519
// signature covers, the names of the release's files, and the checks that make a
// manifest and the zip it names trustworthy. The release targets in magefiles build and
// verify releases with it, so the packaging and the app can never disagree on what a
// valid release is.
//
// It imports no service and speaks no HTTP: the app reads the manifest and the zip
// through the GitHub client, and hands this package only their bytes.
package update
