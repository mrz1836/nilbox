// Package execpath finds the external programs the app runs (gh, git, claude) and runs them.
//
// An app started from the Finder or the Dock inherits launchd's minimal PATH, which lacks
// Homebrew, ~/.local/bin, and other common install locations. Search.Find therefore tries
// a search path (the user's login shell's, when Search.Path is set, else the inherited
// one) and then a short list of well-known directories for the current OS. Exec runs a
// program as an argv, never through a shell, and Start runs a long-lived one in its own
// process group (Proc). Every service that finds a program takes a Finder, and every
// service that runs one takes a Runner, so tests can hand it a fake. ExitCode and
// GitMessage read a failed program's outcome.
package execpath

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// runWaitDelay bounds how long Exec.Run waits, once its program has exited, for a
// descendant still holding the program's output, such as a daemon it started.
const runWaitDelay = 2 * time.Second

// ErrNotFound is returned when a program is on neither the search path nor any of the
// well-known directories.
var ErrNotFound = errors.New("executable not found")

// Finder resolves a program name to the absolute path of an executable. Search is the
// app's.
type Finder interface {
	Find(name string) (string, error)
}

// Search finds programs on a search path and in the well-known directories where
// command-line tools are installed. Its zero value searches the inherited PATH and those
// directories without the home-relative ones.
type Search struct {
	// Path, when set, is the search path Find tries first, in place of the inherited
	// PATH: the user's login shell's PATH (internal/shellenv), so the app finds the same
	// git, magex, and go-pre-commit as the user's terminal. FallbackPath builds one for
	// when the shell's can't be read.
	Path string

	// Home is the user's home directory. Home-relative well-known directories are skipped
	// when it is empty.
	Home string

	// Extra lists additional directories to search, before the well-known ones.
	Extra []string
}

// Find returns the absolute path of the named executable. It tries the search path first
// (Path, or the inherited PATH through exec.LookPath), then each extra and well-known
// directory in order, and returns ErrNotFound naming the program and every directory
// searched when none has it.
func (f Search) Find(name string) (string, error) {
	if path, ok := f.onPath(name); ok {
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

// onPath looks name up on Path when it is set, else on the inherited PATH. Path's empty
// and relative entries are skipped: "." would hand the name back to the inherited PATH,
// and any other relative folder would resolve against the app's own working directory.
func (f Search) onPath(name string) (string, bool) {
	if f.Path == "" || strings.ContainsRune(name, filepath.Separator) {
		path, err := exec.LookPath(name)

		return path, err == nil
	}

	for _, dir := range filepath.SplitList(f.Path) {
		if !filepath.IsAbs(dir) {
			continue
		}

		if path, err := exec.LookPath(filepath.Join(dir, name)); err == nil {
			return path, true
		}
	}

	return "", false
}

// FallbackPath returns the search path to use when the user's login shell's PATH can't be
// read: the well-known directories for home first, then inherited, without repeats. On
// macOS a GUI app inherits launchd's /usr/bin:/bin:/usr/sbin:/sbin, where Apple's git
// asks in a keychain window for credentials another git stored and so stalls a run with
// no terminal; the well-known directories hold the git and tools the user's terminal runs.
func FallbackPath(home, inherited string) string {
	dirs := wellKnownDirs(home)

	for _, dir := range filepath.SplitList(inherited) {
		if dir != "" && !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}

	return strings.Join(dirs, string(filepath.ListSeparator))
}

// dirs returns the non-empty extra directories followed by the well-known ones.
func (f Search) dirs() []string {
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
// path is resolved with Search first.
type Exec struct {
	Search Search

	// Env, when non-nil, is the whole environment each program runs with, as in
	// exec.Cmd.Env; nil runs it with this process's environment. It lets a caller, such
	// as a test isolating git from the user's configuration, set a program's environment
	// without changing the process's, which every other goroutine shares. Search looks a
	// program up on its own Path, or this process's search path, never on a PATH in Env.
	Env []string
}

// Run runs name with args and returns what it wrote to stdout and stderr. The error is the
// lookup or exec error, wrapped; a non-zero exit is an *exec.ExitError in the chain, so
// callers can read its exit code (see ExitCode). A program that exits but leaves a
// descendant holding its output is waited for runWaitDelay more, then its output so far
// is returned and its exit status counts: such a descendant can't hang Run past the
// program.
func (r Exec) Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	path := name
	if !filepath.IsAbs(path) {
		if path, err = r.Search.Find(name); err != nil {
			return nil, nil, err
		}
	}

	var outBuf, errBuf bytes.Buffer

	cmd := exec.CommandContext(ctx, path, args...) //nolint:gosec // argv only, no shell
	cmd.Env = r.Env
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	cmd.WaitDelay = runWaitDelay

	if err = cmd.Run(); err != nil && !errors.Is(err, exec.ErrWaitDelay) {
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

// Executable reports whether a file with mode m is a program someone may run: a regular
// file with an execute bit.
func Executable(m fs.FileMode) bool {
	return m.IsRegular() && m.Perm()&0o111 != 0
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
