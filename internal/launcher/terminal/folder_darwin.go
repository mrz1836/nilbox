//go:build darwin

package terminal

import "context"

// Warp opens a folder itself; iTerm2 and Terminal.app open one through a script.
var _ FolderOpener = warpDriver{}

// OpenFolder opens a new tab of Warp's frontmost window, or a new window when none is open,
// whose own shell starts in dir, with `open -b <Warp's bundle ID> <dir>`, which needs no
// Automation permission.
func (d warpDriver) OpenFolder(ctx context.Context, dir string) error {
	if err := validateFolder(dir); err != nil {
		return err
	}

	return runTerminal(ctx, d.executor, appWarp, openBin, warpFolderArgs(dir)...)
}
