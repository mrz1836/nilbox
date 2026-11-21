// Package toolsigns names the files in a clone that show it is set up for a tool: the
// setup's Tools step counts the clones that hold them, and Deps finds them in the tree of
// the commit it reads. Each is a path from the clone's top, with slashes, and a folder
// counts whatever it holds. It is a leaf: it imports nothing of the app's, so the setup
// and Deps share the list without either importing the other.
package toolsigns

// MageX returns the signs of a clone set up for mage-x: its env file, its .mage.yaml, or a
// magefiles folder.
func MageX() []string {
	return []string{".github/env/10-mage-x.env", ".mage.yaml", "magefiles"}
}
