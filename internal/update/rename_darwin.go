//go:build darwin

package update

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// renameNew renames from to to, failing with fs.ErrExist when anything is at to already:
// renamex_np's RENAME_EXCL, so nothing that appears there between a move's look and its
// rename is ever replaced. A file system that can't rename exclusively renames the plain
// way, once nothing is at to (renameAfterLook).
func renameNew(from, to string) error {
	err := unix.RenamexNp(from, to, unix.RENAME_EXCL)
	if errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EINVAL) {
		return renameAfterLook(from, to)
	}

	if err != nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: err}
	}

	return nil
}
