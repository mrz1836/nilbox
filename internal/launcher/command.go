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
// It brings a followed run's tab forward again too, in the terminal the run opened in. It
// is not bound: core hands it to the runs that go in the terminal, so the window has no
// way to open a file or a folder.
type CommandOpener struct {
	service *Service
}

// NewCommandOpener returns a CommandOpener over the launcher's settings, terminals, and
// launch folder.
func NewCommandOpener(s *Service) *CommandOpener {
	return &CommandOpener{service: s}
}

// Open opens the script at path, and returns the ID of the terminal it opened it in: the
// one chosen in Settings → Launching as it read them, which a run records, so the window
// names the terminal the run went to and Focus brings its tab forward there, whatever is
// chosen later. The same registry as a launch's picks the driver, and the driver's errors,
// macOS's Automation refusal (terminal.ErrAutomationDenied) among them, come back as they
// are, with no ID. It opens nothing but a script the app wrote: path must be a regular
// file directly in the launch folder, named as terminal.ScriptWriter names scripts, or it
// is terminal.ErrInvalidScriptPath and nothing is opened.
func (o *CommandOpener) Open(ctx context.Context, path string) (terminal.ID, error) {
	s := o.service

	if err := terminal.CheckScriptFile(s.paths.Launch, path); err != nil {
		return "", err
	}

	driver, err := o.driver(ctx)
	if err != nil {
		return "", err
	}

	if err = driver.Open(ctx, path); err != nil {
		return "", err
	}

	return driver.ID(), nil
}

// Focus brings forward the tab that tab tells in the terminal id, the one a followed run
// recorded it opened in (Open's ID), through the same registry as a launch's, for Show in
// terminal. An empty id, from a run recorded before runs recorded their terminal, is the
// terminal chosen in Settings → Launching now. The registry's and the driver's errors come
// back as they are: terminal.ErrUnknownTerminal for an id it doesn't know,
// terminal.ErrNoFocus for a terminal that can't bring a tab forward, terminal.ErrNoSuchTab
// when no tab runs on the tab's tty, terminal.ErrNoFocusLink when Warp has no link to it,
// and terminal.ErrUnsupportedOS where no terminal can be opened.
func (o *CommandOpener) Focus(ctx context.Context, id terminal.ID, tab terminal.Tab) error {
	s := o.service

	if id == "" {
		prefs, err := s.settings.Get(ctx)
		if err != nil {
			return err
		}

		id = prefs.Terminal
	}

	return s.registry.Focus(ctx, id, tab)
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
