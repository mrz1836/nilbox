// Package execpath finds the external programs the app runs (gh, git, claude) and runs them.
//
// An app started from the Finder or the Dock inherits launchd's minimal PATH, which lacks
// Homebrew, ~/.local/bin, and other common install locations. Finder.Find therefore tries
// the inherited PATH first and then a short list of well-known directories for the current
// OS. Exec runs a program as an argv, never through a shell, and every service that runs
// a program takes it as a Runner, so tests can hand it a fake. ExitCode and GitMessage
// read a failed program's outcome.
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

// Runner runs a program with arguments and returns what it wrote. Implementations run the
// argv directly, never through a shell; Exec is the app's.
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error)
}

// Exec runs programs as argv with exec.CommandContext. A name that is not an absolute
// path is resolved with Finder first.
type Exec struct {
	Finder Finder
}

// Run runs name with args and returns what it wrote to stdout and stderr. The error is the
// lookup or exec error, wrapped; a non-zero exit is an *exec.ExitError in the chain, so
// callers can read its exit code (see ExitCode).
func (r Exec) Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
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

// ExitCode returns the status a program exited with when err holds its exit (a non-zero
// exit status is an *exec.ExitError in Exec's error chain), or -1 when err holds none:
// the program could not be run, was killed by a signal, or err is nil.
func ExitCode(err error) int {
	var exit interface{ ExitCode() int }
	if err == nil || !errors.As(err, &exit) {
		return -1
	}

	return exit.ExitCode()
}

// FirstLine returns the first non-blank line of a program's output, trimmed, or "".
func FirstLine(out []byte) string {
	for line := range strings.SplitSeq(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}

	return ""
}

// GitMessage returns the first non-blank line of git's error output without its
// "fatal: " or "error: " prefix: the part of a git failure worth showing.
func GitMessage(stderr []byte) string {
	line := FirstLine(stderr)

	for _, prefix := range []string{"fatal: ", "error: "} {
		line = strings.TrimPrefix(line, prefix)
	}

	return line
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
