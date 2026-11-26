package launcher

import (
	"context"
	"errors"
	"io/fs"

	"github.com/mrz1836/nilbox/internal/launcher/terminal"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// CommandOpener opens a script already written into the launch folder, such as a run's
// from terminal.WriteScript, in the terminal chosen in Settings → Launching, as a launch
// opens its own, and opens that terminal in a folder where the terminal can do so itself.
// It is not bound: core hands it to the runs that go in the terminal, so the window has no
// way to open a file or a folder.
type CommandOpener struct {
	service *Service
}

// NewCommandOpener returns a CommandOpener over the launcher's settings, terminals, and
// launch folder.
func NewCommandOpener(s *Service) *CommandOpener {
	return &CommandOpener{service: s}
}

// Open opens the script at path. The same registry as a launch's picks the driver, and the
// driver's errors, macOS's Automation refusal (terminal.ErrAutomationDenied) among them,
// come back as they are. It opens nothing but a script the app wrote: path must be a
// regular file directly in the launch folder, named as terminal.ScriptWriter names
// scripts, or it is terminal.ErrInvalidScriptPath and nothing is opened.
func (o *CommandOpener) Open(ctx context.Context, path string) error {
	s := o.service

	if err := terminal.CheckScriptFile(s.paths.Launch, path); err != nil {
		return err
	}

	driver, err := o.driver(ctx)
	if err != nil {
		return err
	}

	return driver.Open(ctx, path)
}

// OpenFolder opens the terminal chosen in Settings → Launching in dir, for Open in
// terminal, when that terminal opens a folder itself (its driver is a
// terminal.FolderOpener, as Warp's is): a new tab or window whose own shell starts in dir,
// with nothing run there. It reports whether it opened it.
//
// When the chosen terminal can't (iTerm2, Terminal.app), it opens nothing and returns
// false with no error, and the caller opens the terminal there through a script that
// changes into dir instead, with Open.
//
// On an error it returns false, and the caller opens nothing more. Whichever terminal is
// chosen, dir must be an absolute path with no control characters to a folder that is
// there (terminal.CheckFolder), or it is terminal.ErrInvalidFolder, worded for the window
// when the folder is gone. The settings', the registry's (terminal.ErrUnsupportedOS where
// no terminal can be opened), and the driver's errors come back as they are.
func (o *CommandOpener) OpenFolder(ctx context.Context, dir string) (opened bool, err error) {
	if err = terminal.CheckFolder(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			err = uierr.New(paths.Tilde(dir, o.service.home)+" isn't there anymore.", err)
		}

		return false, err
	}

	driver, err := o.driver(ctx)
	if err != nil {
		return false, err
	}

	folders, ok := driver.(terminal.FolderOpener)
	if !ok {
		return false, nil
	}

	if err = folders.OpenFolder(ctx, dir); err != nil {
		return false, err
	}

	return true, nil
}

// driver returns the driver of the terminal chosen in Settings → Launching, as the
// settings are now, from the same registry as a launch's.
func (o *CommandOpener) driver(ctx context.Context) (terminal.Driver, error) {
	s := o.service

	prefs, err := s.settings.Get(ctx)
	if err != nil {
		return nil, err
	}

	return s.registry.Get(prefs.Terminal)
}
