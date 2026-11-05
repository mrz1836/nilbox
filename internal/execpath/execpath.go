// Package execpath finds the external programs the app runs (gh, git, claude) and runs them.
//
// An app started from the Finder or the Dock inherits launchd's minimal PATH, which lacks
// Homebrew, ~/.local/bin, and other common install locations. Finder.Find therefore tries
// the inherited PATH first and then a short list of well-known directories for the current
// OS. Runner runs a program as an argv, never through a shell.
package execpath

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNotFound is returned when a program is on neither the search path nor any of the
// well-known directories.
var ErrNotFound = errors.New("executable not found")

// Finder resolves a program name to the absolute path of an executable.
type Finder struct {
	// Home is the user's home directory. Home-relative well-known directories are skipped
	// when it is empty.
	Home string

	// Extra lists additional directories to search, before the well-known ones.
	Extra []string
}

// Find returns the absolute path of the named executable. It tries exec.LookPath(name)
// first, then each extra and well-known directory in order, and returns ErrNotFound
// naming the program and every directory searched when none has it.
func (f Finder) Find(name string) (string, error) {
	if path, err := exec.LookPath(name); err == nil {
		return absolute(path)
	}

	dirs := f.dirs()
	for _, dir := range dirs {
		if path, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
			return absolute(path)
		}
	}

	return "", fmt.Errorf("%w: %q is not on the search path or in %s", ErrNotFound, name, strings.Join(dirs, ", "))
}

// dirs returns the non-empty extra directories followed by the well-known ones.
func (f Finder) dirs() []string {
	known := wellKnownDirs(f.Home)
	dirs := make([]string, 0, len(f.Extra)+len(known))

	for _, dir := range f.Extra {
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}

	return append(dirs, known...)
}

// Runner runs programs as argv with exec.CommandContext. A name that is not an absolute
// path is resolved with Finder first.
type Runner struct {
	Finder Finder
}

// Run runs name with args and returns what it wrote to stdout and stderr. The error is the
// lookup or exec error, wrapped; a non-zero exit is an *exec.ExitError in the chain, so
// callers can read its exit code.
func (r Runner) Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	path := name
	if !filepath.IsAbs(path) {
		if path, err = r.Finder.Find(name); err != nil {
			return nil, nil, err
		}
	}

	var outBuf, errBuf bytes.Buffer

	cmd := exec.CommandContext(ctx, path, args...) //nolint:gosec // argv only, no shell
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf

	if err = cmd.Run(); err != nil {
		return outBuf.Bytes(), errBuf.Bytes(), fmt.Errorf("run %s: %w", filepath.Base(path), err)
	}

	return outBuf.Bytes(), errBuf.Bytes(), nil
}

// absolute makes a found path absolute; exec.LookPath already refuses relative results
// from the search path, but an explicit relative directory can still yield one.
func absolute(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", path, err)
	}

	return abs, nil
}
