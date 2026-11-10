package settings

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// ErrInvalidToolPath is returned for a chosen copy of a tool that is not an executable file.
var ErrInvalidToolPath = errors.New("invalid tool path")

// SetIntegrationEnabled stores how the user set an integration's switch, automatic, on,
// or off, and returns the settings with it. Once it is stored, an
// event.IntegrationsChanged is emitted, so the window hears that the tools' list changed.
func (s *Service) SetIntegrationEnabled(ctx context.Context, id integrationprefs.ID, state integrationprefs.Switch,
) (Settings, error) {
	if !id.Valid() {
		return Settings{}, fmt.Errorf("%w: %q", integrationprefs.ErrUnknownIntegration, id)
	}

	stored, err := integrationprefs.EncodeSwitch(state)
	if err != nil {
		return Settings{}, fmt.Errorf("%w: %q", err, state)
	}

	return s.saveIntegrations(ctx, integrationprefs.KeyEnabled(id), stored, event.IntegrationsChanged{})
}

// SetIntegrationPath stores the copy of a tool the user chose, and returns the settings
// with it: an absolute or ~-relative path to an executable file, stored cleaned and
// absolute, or "" to use the copy the user's shell finds. Once it is stored, an
// event.IntegrationsChanged is emitted, so the integrations detect the tools again.
func (s *Service) SetIntegrationPath(ctx context.Context, id integrationprefs.ID, path string) (Settings, error) {
	switch {
	case !id.Valid():
		return Settings{}, fmt.Errorf("%w: %q", integrationprefs.ErrUnknownIntegration, id)
	case !id.Tool():
		return Settings{}, fmt.Errorf("%w: %q", integrationprefs.ErrNotATool, id)
	}

	path = strings.TrimSpace(path)
	if path != "" {
		full, err := s.toolPath(path)
		if err != nil {
			return Settings{}, err
		}

		path = full
	}

	stored, err := integrationprefs.EncodePath(path)
	if err != nil {
		return Settings{}, fmt.Errorf("%w: %w", ErrInvalidToolPath, err)
	}

	return s.saveIntegrations(ctx, integrationprefs.KeyPath(id), stored, event.IntegrationsChanged{CopyChosen: true})
}

// toolPath expands a ~ in path and checks that it names an executable file, worded for
// the user when it doesn't.
func (s *Service) toolPath(path string) (string, error) {
	full, err := paths.ExpandHome(path, s.home)
	if err != nil {
		return "", uierr.New("Enter the program's full path, such as ~/bin/magex.",
			fmt.Errorf("%w: %w", ErrInvalidToolPath, err))
	}

	shown := paths.Tilde(full, s.home)

	info, err := os.Stat(full)

	switch {
	case err != nil:
		return "", uierr.New("There's nothing at "+shown+".", fmt.Errorf("%w: %w", ErrInvalidToolPath, err))
	case !info.Mode().IsRegular():
		return "", uierr.New(shown+" isn't a program.", fmt.Errorf("%w: %s is not a file", ErrInvalidToolPath, shown))
	case !execpath.Executable(info.Mode()):
		return "", uierr.New(shown+" can't be run: it isn't executable.",
			fmt.Errorf("%w: %s is not executable", ErrInvalidToolPath, shown))
	}

	return full, nil
}

// saveIntegrations stores value under key, and, once it is stored, emits changed.
func (s *Service) saveIntegrations(ctx context.Context, key, value string, changed event.IntegrationsChanged,
) (Settings, error) {
	saved, err := s.save(ctx, setting(key, value))
	if err != nil {
		return saved, err
	}

	s.events.Emit(changed)

	return saved, nil
}
