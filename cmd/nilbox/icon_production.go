//go:build desktop && production

package main

// appIcon returns nil: a production build runs from its app bundle, whose icon set
// (Contents/Resources, named by the Info.plist) the system shows and adapts itself.
func appIcon() []byte {
	return nil
}
