package localrepos

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Kind is what one of the user's other repository locations is: a folder whose clones
// sit directly inside it, as the clone root's do, or one clone on its own.
type Kind string

const (
	// KindFolder is a folder looked into one level deep, as the clone root is.
	KindFolder Kind = "folder"

	// KindClone is a clone that counts as that one repository.
	KindClone Kind = "clone"
)

// Valid reports whether k is one of the kinds.
func (k Kind) Valid() bool {
	return k == KindFolder || k == KindClone
}

// Location is one of the user's other repository locations as it is stored: its path,
// with ~ for home, and what it was when it was added. The kind is never worked out again
// on its own, so a folder someone later runs git init in is still looked into.
type Location struct {
	Path string `json:"path"`
	Kind Kind   `json:"kind"`
}

var (
	// ErrLocationNotFound is returned for a location that doesn't exist.
	ErrLocationNotFound = errors.New("the folder doesn't exist")

	// ErrNotAFolder is returned for a location that is a file.
	ErrNotAFolder = errors.New("not a folder")

	// ErrLocationRefused is returned for a folder that can't be a location: a hidden one,
	// one inside the app's worktrees folder or a git folder, or a linked worktree.
	ErrLocationRefused = errors.New("can't be a repository location")
)

// Classify works out what the folder at dir is, with stat alone: a clone, when it holds
// a .git entry, else a folder. A path that doesn't exist, or isn't a folder, is refused
// in words the window shows, with home written as ~.
func Classify(dir, home string) (Kind, error) {
	info, err := os.Stat(dir)

	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", uierr.New(paths.Tilde(dir, home)+" doesn't exist.", fmt.Errorf("%w: %s", ErrLocationNotFound, dir))
	case err != nil:
		return "", uierr.New("Couldn't read "+paths.Tilde(dir, home)+".", fmt.Errorf("read %s: %w", dir, err))
	case !info.IsDir():
		return "", uierr.New(paths.Tilde(dir, home)+" is a file, not a folder.", fmt.Errorf("%w: %s", ErrNotAFolder, dir))
	}

	if hasGit(dir) {
		return KindClone, nil
	}

	return KindFolder, nil
}

// CheckAddable refuses a folder that can't be a repository location, in words the window
// shows: a hidden one, which no listing looks in, one inside the app's worktrees folder
// (.<slug>) or a git folder, and a linked worktree, whose clone is the one to add.
func CheckAddable(dir, home string) error {
	shown := paths.Tilde(dir, home)
	clean := filepath.Clean(dir)

	if hidden(clean) {
		return refused(shown+" is hidden, and the app never looks in hidden folders.", clean)
	}

	for d := filepath.Dir(clean); filepath.Dir(d) != d; d = filepath.Dir(d) {
		switch filepath.Base(d) {
		case "." + brand.Slug:
			return refused(shown+" is inside the app's own worktrees folder.", clean)
		case ".git":
			return refused(shown+" is inside a git folder.", clean)
		}
	}

	if common, linked := linkedWorktree(clean); linked {
		if filepath.Base(common) == ".git" {
			main := paths.Tilde(filepath.Dir(common), home)

			return refused(shown+" is a linked worktree of "+main+"; add "+main+" instead.", clean)
		}

		return refused(shown+" is a linked worktree; add the clone it belongs to instead.", clean)
	}

	return nil
}

// refused is CheckAddable's refusal of dir, worded as sentence.
func refused(sentence, dir string) error {
	return uierr.New(sentence, fmt.Errorf("%w: %s", ErrLocationRefused, dir))
}

// hidden reports a folder whose name starts with a dot.
func hidden(dir string) bool {
	return strings.HasPrefix(filepath.Base(dir), ".")
}

// hasGit reports whether dir holds a .git entry, a folder or a file.
func hasGit(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))

	return err == nil
}

// linkedWorktree reports a linked worktree, whose .git file points at a git folder with a
// commondir, and returns that common git folder. A clone whose git folder only lives
// elsewhere, or a submodule, has none, and is a clone of its own.
func linkedWorktree(dir string) (string, bool) {
	dotGit := filepath.Join(dir, ".git")

	info, err := os.Stat(dotGit)
	if err != nil || info.IsDir() {
		return "", false
	}

	gitDir, ok := readGitPointer(dotGit, dir, "gitdir: ")
	if !ok {
		return "", false
	}

	common, ok := readGitPointer(filepath.Join(gitDir, "commondir"), gitDir, "")
	if !ok {
		return "", false
	}

	return filepath.Clean(common), true
}

// CommonDir returns the resolved common git folder of the clone at dir, or "" when dir
// has no readable .git. A .git folder is that folder. A .git file (a linked worktree, or
// a clone whose git folder lives elsewhere) points at the git folder, and a commondir file
// there points on to the common one.
func CommonDir(dir string) string {
	dotGit := filepath.Join(dir, ".git")

	info, err := os.Stat(dotGit)
	if err != nil {
		return ""
	}

	if info.IsDir() {
		return paths.Resolved(dotGit)
	}

	gitDir, ok := readGitPointer(dotGit, dir, "gitdir: ")
	if !ok {
		return ""
	}

	if common, found := readGitPointer(filepath.Join(gitDir, "commondir"), gitDir, ""); found {
		return paths.Resolved(common)
	}

	return paths.Resolved(gitDir)
}

// gitPointerLimit is the most of a one-line git file that is read, far more than the
// longest path a system takes.
const gitPointerLimit = 64 << 10

// readGitPointer reads the path a one-line git file holds after prefix. A relative path
// is taken from base, as git takes it. Only a regular file, or a link to one, is read
// (paths.ReadHead), so a named pipe put in its place never hangs a read, and reads as a
// file that isn't there.
func readGitPointer(file, base, prefix string) (string, bool) {
	data, err := paths.ReadHead(file, gitPointerLimit)
	if err != nil {
		return "", false
	}

	path, ok := strings.CutPrefix(strings.TrimRight(string(data), "\r\n"), prefix)
	if !ok || path == "" {
		return "", false
	}

	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}

	return path, true
}
