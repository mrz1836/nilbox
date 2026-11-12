package toolinstall

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// newSuffix and oldSuffix name the files an install puts beside its target: the new copy
// before it is renamed into place, and a link to the old one until the new one runs.
func (in *Installer) newSuffix() string { return "." + in.Slug + "-new" }

func (in *Installer) oldSuffix() string { return "." + in.Slug + "-old" }

// place puts the program extracted into work, as Binary, at <Dir>/<Binary> so that the
// target is never missing: it works in the folder Dir resolves to, copies the program
// beside the target, links the old copy aside, renames the new one over the target (atomic,
// and a new inode, so a running copy never sees a half-written file), and tries the placed
// copy, putting the old one back when it won't run. It returns the target's path and
// whether a copy was replaced.
func (in *Installer) place(ctx context.Context, work *os.Root, req Request) (string, bool, error) {
	if err := os.MkdirAll(req.Dir, execPerm); err != nil {
		return "", false, fmt.Errorf("%w: %w", ErrWrite, err)
	}

	dir, err := filepath.EvalSymlinks(req.Dir)
	if err != nil {
		return "", false, fmt.Errorf("%w: %w", ErrWrite, err)
	}

	target := filepath.Join(dir, req.Binary)

	replacing, err := checkTarget(target)
	if err != nil {
		return target, false, err
	}

	fresh, old := target+in.newSuffix(), target+in.oldSuffix()

	// A leftover of an install that stopped short is this app's own, by its name.
	_ = os.Remove(fresh)
	_ = os.Remove(old)

	if err = copyProgram(work, req.Binary, fresh); err != nil {
		_ = os.Remove(fresh)

		return target, false, fmt.Errorf("%w: %w", ErrWrite, err)
	}

	if replacing {
		if err = os.Link(target, old); err != nil {
			_ = os.Remove(fresh)

			return target, false, fmt.Errorf("%w: keep the old copy: %w", ErrWrite, err)
		}
	}

	if err = os.Rename(fresh, target); err != nil {
		_ = os.Remove(fresh)
		_ = os.Remove(old)

		return target, false, fmt.Errorf("%w: %w", ErrWrite, err)
	}

	syncDir(dir)

	if err = req.Smoke(ctx, target); err != nil {
		return target, false, undo(dir, target, old, replacing, err)
	}

	_ = os.Remove(old)

	return target, replacing, nil
}

// checkTarget reports whether there is a copy at target to replace, refusing a link or
// anything but a file.
func checkTarget(target string) (bool, error) {
	info, err := os.Lstat(target)

	switch {
	case errors.Is(err, os.ErrNotExist):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("%w: %w", ErrWrite, err)
	case info.Mode()&os.ModeSymlink != 0:
		return false, ErrLinkTarget
	case !info.Mode().IsRegular():
		return false, fmt.Errorf("%w: %s isn't a file", ErrWrite, target)
	default:
		return true, nil
	}
}

// undo rolls a placed copy that didn't run back, and returns the error that says how it
// went: ErrRolledBack, or ErrRollbackFailed when the old copy couldn't be put back.
func undo(dir, target, old string, replacing bool, smokeErr error) error {
	undoErr := rollBack(target, old, replacing)

	syncDir(dir)

	if undoErr != nil {
		return fmt.Errorf("%w: %w (and %w)", ErrRollbackFailed, smokeErr, undoErr)
	}

	return fmt.Errorf("%w: %w", ErrRolledBack, smokeErr)
}

// rollBack puts the old copy back over target, or removes target when there was none,
// and returns why it couldn't.
func rollBack(target, old string, replacing bool) error {
	if replacing {
		return os.Rename(old, target) //nolint:wrapcheck // worded by the caller
	}

	return os.Remove(target) //nolint:wrapcheck // worded by the caller
}

// copyProgram copies the program named name in work, the one this install extracted, to
// dst, a new file with mode 0755, and syncs it.
func copyProgram(work *os.Root, name, dst string) error {
	in, err := work.Open(name)
	if err != nil {
		return err //nolint:wrapcheck // worded by the caller
	}

	defer func() { _ = in.Close() }()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, execPerm) //nolint:gosec // a program, to be run
	if err != nil {
		return err //nolint:wrapcheck // worded by the caller
	}

	_, err = io.Copy(out, in)
	if err == nil {
		err = out.Sync()
	}

	if closeErr := out.Close(); err == nil {
		err = closeErr
	}

	return err //nolint:wrapcheck // worded by the caller
}

// syncDir flushes a folder's entries to disk. A folder that can't be synced (EINVAL or
// ENOTSUP on some file systems) is left as it is.
func syncDir(dir string) {
	d, err := os.Open(dir) //nolint:gosec // the folder this install writes in
	if err != nil {
		return
	}

	_ = d.Sync() // the rename is done; a sync that fails can't undo it
	_ = d.Close()
}

// Cleanup removes what installs that stopped short left behind, and only that, by this
// app's own names: download folders in Downloads older than maxAge, and files beside a
// target in dirs (<binary>.<slug>-new and -old) older than maxAge. It reports nothing: a
// leftover that can't be removed is tried again next time.
func (in *Installer) Cleanup(dirs []string, now time.Time, maxAge time.Duration) {
	old := func(e os.DirEntry) bool {
		info, err := e.Info()

		return err == nil && now.Sub(info.ModTime()) > maxAge
	}

	ours := func(e os.DirEntry) bool { return e.IsDir() && strings.HasPrefix(e.Name(), in.Slug+"-") }
	removeEach(in.Downloads, func(e os.DirEntry) bool { return ours(e) && old(e) }, os.RemoveAll)

	leftover := func(e os.DirEntry) bool {
		name := e.Name()

		return e.Type().IsRegular() && (strings.HasSuffix(name, in.newSuffix()) || strings.HasSuffix(name, in.oldSuffix()))
	}

	for _, dir := range dirs {
		removeEach(dir, func(e os.DirEntry) bool { return leftover(e) && old(e) }, os.Remove)
	}
}

// removeEach removes, with remove, each entry of dir that match picks. A folder that can't
// be read, or an entry that can't be removed, is left as it is.
func removeEach(dir string, match func(os.DirEntry) bool, remove func(string) error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if match(e) {
			_ = remove(filepath.Join(dir, e.Name()))
		}
	}
}
