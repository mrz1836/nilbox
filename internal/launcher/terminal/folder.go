package terminal

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// ErrInvalidFolder is returned for a folder a terminal can't be opened in safely: a path
// that is relative or holds a control character, or one that isn't a directory now.
var ErrInvalidFolder = errors.New("invalid folder")

// FolderOpener is a Driver that can also open a folder itself, for Open in terminal: a new
// tab or window whose own shell, started and set up by the terminal as any of its tabs is,
// begins in the folder, with nothing run there. A terminal whose driver can't is opened in
// a folder through a script that changes into it and starts the user's login shell, which
// in iTerm2 and Terminal.app is as good as a tab of their own. In Warp it isn't: Warp runs
// the script as one long command in its tab, so the shell nested inside gets none of
// Warp's input editor or blocks, while a folder Warp opens itself does.
type FolderOpener interface {
	Driver

	// OpenFolder opens a new tab or window of the terminal whose shell starts in dir, an
	// absolute path with no control characters, or it is ErrInvalidFolder and nothing runs.
	OpenFolder(ctx context.Context, dir string) error
}

// CheckFolder checks that dir is a folder a terminal can be opened in: an absolute path
// with no control characters, as a script's must be, to a directory that is there now.
// Anything else is ErrInvalidFolder, and a folder that isn't there is fs.ErrNotExist too.
func CheckFolder(dir string) error {
	if err := validateFolder(dir); err != nil {
		return err
	}

	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidFolder, err)
	}

	if !info.IsDir() {
		return fmt.Errorf("%w: %q is not a folder", ErrInvalidFolder, dir)
	}

	return nil
}

// validateFolder checks that dir is absolute and holds no control characters, the rules
// validateScriptPath keeps for a script, so handing it to a terminal can never open or run
// anything else.
func validateFolder(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("%w: %q is not absolute", ErrInvalidFolder, dir)
	}

	if strings.IndexFunc(dir, unicode.IsControl) >= 0 {
		return fmt.Errorf("%w: %q contains a control character", ErrInvalidFolder, dir)
	}

	return nil
}

// warpFolderArgs returns the `open` arguments that open a folder in Warp: the request a
// script's is (warpOpenArgs), since Warp tells a folder from a script itself. Warp opens a
// folder in a new tab of its frontmost window, or a new window when none is open, whose
// own shell starts there, set up as any Warp tab is, with its blocks. Before Warp's
// first-run welcome is finished, Warp drops the request while open still succeeds, as it
// does a script's. It is pure: running it is the darwin driver's job.
func warpFolderArgs(dir string) []string {
	return warpOpenArgs(dir)
}
