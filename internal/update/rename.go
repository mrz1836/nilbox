package update

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// renameAfterLook renames from to to once nothing is at to, failing with fs.ErrExist when
// something is: a rename onto an empty folder would replace it, so it looks first. Something
// that appears between the look and the rename can still be an empty folder replaced, which
// renameNew's exclusive rename, where the file system has one, rules out.
func renameAfterLook(from, to string) error {
	if _, err := os.Lstat(to); err == nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: fs.ErrExist}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("look where the copy goes: %w", err)
	}

	if err := os.Rename(from, to); err != nil {
		return fmt.Errorf("put the copy in place: %w", err)
	}

	return nil
}
