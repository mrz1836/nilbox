// Package brand is the single source of truth for the product's identity: its name,
// identifiers, tagline, color palette, and logo mark.
//
// Nothing else in the codebase should hard-code the product name. Keeping identity in one
// place means a rename is a change to this package plus the files generated from it.
package brand

const (
	// Name is the human-facing product name.
	Name = "Nilbox"

	// Slug is the lowercase identifier used for binaries, directories, and URLs.
	Slug = "nilbox"

	// BundleID is the reverse-DNS application identifier (the macOS bundle ID).
	BundleID = "com.mrz1836." + Slug

	// Tagline is the one-line product promise.
	Tagline = "Your GitHub inbox, set to nil."

	// DataDirName is the name of the per-user application data directory.
	DataDirName = Name
)
