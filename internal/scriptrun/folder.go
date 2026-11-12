package scriptrun

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/mrz1836/nilbox/internal/uierr"
)

// FolderScript names the script that opens the terminal in a clone and runs nothing
// there, for a page's Open in terminal: no run owns it, so it takes run 0's name, which
// the launch folder's pruning knows (see ScriptName).
const FolderScript = "run-0"

// ErrNoFolderTerminal is returned by OpenFolder where the user's terminal can't be opened.
var ErrNoFolderTerminal = errors.New("the terminal can't be opened here")

// ScriptName names the script that opens run id in the terminal, or the terminal in its
// folder: run-<id>, a prefix the launch folder's pruning knows, so the scripts are
// removed once they are old.
func ScriptName(id int64) string {
	return "run-" + strconv.FormatInt(id, 10)
}

// FolderOpener opens the user's terminal in a folder, running nothing there, under the
// script name it is given. Core adapts the terminal the runs open in.
type FolderOpener interface {
	OpenFolder(ctx context.Context, name, dir string) error
}

// OpenFolder opens the user's terminal in the clone at dir through opener, as
// FolderScript, for a page's Open in terminal: the user looks at the clone or fixes it
// there, and the app runs nothing in it. A nil opener, where no terminal can be opened,
// is ErrNoFolderTerminal, in words the window shows.
func OpenFolder(ctx context.Context, opener FolderOpener, dir string) error {
	if opener == nil {
		return uierr.New("Your terminal can't be opened here.", ErrNoFolderTerminal)
	}

	if err := opener.OpenFolder(ctx, FolderScript, dir); err != nil {
		return fmt.Errorf("open the terminal in the clone: %w", err)
	}

	return nil
}
