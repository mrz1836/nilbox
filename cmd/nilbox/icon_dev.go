//go:build desktop && !production

package main

import _ "embed"

// devIcon is the app icon for development runs (magex app:dev). They run the bare binary,
// with no app bundle to carry an icon, so the app sets this one itself; magex brand:icons
// renders it from the same artwork as the bundle's icon set.
//
//go:embed appicon.png
var devIcon []byte

// appIcon returns the icon the app sets on itself at startup.
func appIcon() []byte {
	return devIcon
}
