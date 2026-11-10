package launcher

import (
	"context"

	"github.com/mrz1836/nilbox/internal/launcher/terminal"
)

// CommandOpener opens a script already written into the launch folder, such as a run's
// from terminal.WriteScript, in the terminal chosen in Settings → Launching, as a launch
// opens its own. It is not bound: core hands it to the runs that go in the terminal, so
// the window has no way to open a file.
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

	prefs, err := s.settings.Get(ctx)
	if err != nil {
		return err
	}

	driver, err := s.registry.Get(prefs.Terminal)
	if err != nil {
		return err
	}

	return driver.Open(ctx, path)
}
