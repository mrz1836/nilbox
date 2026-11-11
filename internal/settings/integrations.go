package settings

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/prefs"
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

// SetRunsIn stores where the runs go, in the app or in the terminal chosen for launches,
// and returns the settings with it. Nothing else hears of it: what starts a run (a
// release, a hooks action, checks, or a script) reads the setting as it starts one.
func (s *Service) SetRunsIn(ctx context.Context, place integrationprefs.RunsIn) (Settings, error) {
	stored, err := integrationprefs.EncodeRunsIn(place)
	if err != nil {
		return Settings{}, fmt.Errorf("%w: %q", err, place)
	}

	return s.save(ctx, setting(integrationprefs.KeyRunsIn, stored))
}

// SetOfferAfterMerge stores whether merging a pull request in the review sheet offers to
// release its repository, and returns the settings with it. Nothing else hears of it: the
// review sheet asks Release whether to offer as each merge lands.
func (s *Service) SetOfferAfterMerge(ctx context.Context, on bool) (Settings, error) {
	return s.save(ctx, setting(integrationprefs.KeyOfferAfterMerge, integrationprefs.EncodeOfferAfterMerge(on)))
}

// SetScriptSources stores which sources Repo scripts lists besides the user's own scripts,
// magex's targets and the commands in a repository's own files (t3.json and
// .vscode/tasks.json), and returns the settings with them. Nothing else hears of it: Repo
// scripts reads the setting as it lists a repository's scripts.
func (s *Service) SetScriptSources(ctx context.Context, magexTargets, repoFiles bool) (Settings, error) {
	return s.save(ctx, func(ctx context.Context, p *prefs.Store) error {
		if err := p.Set(ctx, integrationprefs.KeyMagexTargets, integrationprefs.EncodeOn(magexTargets)); err != nil {
			return err
		}

		return p.Set(ctx, integrationprefs.KeyRepoFiles, integrationprefs.EncodeOn(repoFiles))
	})
}

// SetIntegrationsAnnounced stores whether the window announced the Scripts tab, as it
// first appeared because a tool was found, and returns the settings with it, so the toast
// shows once rather than at every start. Nothing else hears of it.
func (s *Service) SetIntegrationsAnnounced(ctx context.Context, announced bool) (Settings, error) {
	return s.save(ctx, setting(integrationprefs.KeyAnnounced, strconv.FormatBool(announced)))
}

// SetScriptsLastPage stores the Scripts tab's page shown last, which the tab reopens on,
// and returns the settings with it. A page that isn't one of the tab's is refused.
func (s *Service) SetScriptsLastPage(ctx context.Context, page integrationprefs.ScriptsPage) (Settings, error) {
	stored, err := integrationprefs.EncodeLastPage(page)
	if err != nil {
		return Settings{}, fmt.Errorf("%w: %q", err, page)
	}

	return s.save(ctx, setting(integrationprefs.KeyLastPage, stored))
}

// SetScriptsLastRepo stores the clone Repo scripts showed last, as the window names it
// (its folder, with ~), which Repo scripts reopens on, and returns the settings with it;
// "" forgets it. A folder that isn't an absolute or ~ path on one line is refused. Repo
// scripts checks the folder again as it lists it, as it does any the window names.
func (s *Service) SetScriptsLastRepo(ctx context.Context, dir string) (Settings, error) {
	stored, err := integrationprefs.EncodeLastRepo(dir)
	if err != nil {
		return Settings{}, fmt.Errorf("%w: %q", err, dir)
	}

	return s.save(ctx, setting(integrationprefs.KeyLastRepo, stored))
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
