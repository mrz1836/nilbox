//go:build !darwin

package update

// renameNew renames from to to, failing with fs.ErrExist when anything is at to already.
// Only macOS moves the app, so elsewhere it looks first (renameAfterLook).
func renameNew(from, to string) error {
	return renameAfterLook(from, to)
}
