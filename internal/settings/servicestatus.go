package settings

import (
	"context"
	"fmt"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/statusprefs"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Each setter of what the service status check watches (a provider, a part) emits an
// event.StatusRulesChanged once the change is stored, so the check follows it at once: a
// provider turned off is forgotten, and one turned on is checked. A part ticked or
// unticked needs no request: the last readings are judged again.

// SetStatusProvider turns a provider's status page on or off (see statusprefs.Catalog),
// and returns the settings with it. A provider this build doesn't know is refused. The
// providers off are read and written in one update, so two quick changes never lose
// either.
func (s *Service) SetStatusProvider(ctx context.Context, id statusprefs.ProviderID, on bool) (Settings, error) {
	if _, ok := statusprefs.Lookup(id); !ok {
		return Settings{}, uierr.New("That isn't a service whose status the app checks.",
			fmt.Errorf("%w %q", statusprefs.ErrUnknownProvider, id))
	}

	return s.saveStatusRules(ctx, statusprefs.KeyProvidersOff, func(cfg statusprefs.Config) (statusprefs.Config, error) {
		return cfg.SetProvider(id, on)
	})
}

// SetStatusPart ticks or unticks a part of a provider to watch (see statusprefs.Catalog),
// and returns the settings with it. A part this build doesn't know is refused. The list
// it is stored in is read and written in one update, so two quick changes never lose
// either.
func (s *Service) SetStatusPart(ctx context.Context, id statusprefs.PartID, on bool) (Settings, error) {
	key, err := statusprefs.PartKey(id)
	if err != nil {
		return Settings{}, uierr.New("That isn't a part of a service whose status the app checks.", err)
	}

	return s.saveStatusRules(ctx, key, func(cfg statusprefs.Config) (statusprefs.Config, error) {
		return cfg.SetPart(id, on)
	})
}

// saveStatusRules changes the choice stored under key with change, read and written in
// one update, and emits an event.StatusRulesChanged once it is stored; a change that fails
// emits nothing.
func (s *Service) saveStatusRules(ctx context.Context, key string,
	change func(statusprefs.Config) (statusprefs.Config, error),
) (Settings, error) {
	saved, err := s.save(ctx, func(ctx context.Context, p *prefs.Store) error {
		err := p.Update(ctx, key, func(stored string, _ bool) (string, error) {
			cfg, err := change(statusprefs.Decode(map[string]string{key: stored}))
			if err != nil {
				return "", err
			}

			return cfg.Encode()[key], nil
		})
		if err != nil {
			return fmt.Errorf("save what the service status check watches: %w", err)
		}

		return nil
	})
	if err != nil {
		return saved, err
	}

	s.events.Emit(event.StatusRulesChanged{})

	return saved, nil
}

// checkStatusOff returns the write of a backup's providers turned off, each id this build
// doesn't know left out and named in a note.
func checkStatusOff(off []statusprefs.ProviderID) (map[string]string, []string) {
	cfg, unknown := statusprefs.Choose(off, nil)

	return map[string]string{statusprefs.KeyProvidersOff: cfg.Encode()[statusprefs.KeyProvidersOff]}, unknownNotes(unknown)
}

// checkStatusParts returns the writes of a backup's parts watched, each id this build
// doesn't know left out and named in a note.
func checkStatusParts(parts []statusprefs.PartID) (map[string]string, []string) {
	cfg, unknown := statusprefs.Choose(nil, parts)
	stored := cfg.Encode()

	return map[string]string{
		statusprefs.KeyPartsOff: stored[statusprefs.KeyPartsOff], statusprefs.KeyPartsOn: stored[statusprefs.KeyPartsOn],
	}, unknownNotes(unknown)
}

// unknownNotes words the ids of status pages and parts a backup names that this build
// doesn't know.
func unknownNotes(unknown []string) []string {
	notes := make([]string, 0, len(unknown))
	for _, id := range unknown {
		notes = append(notes, fmt.Sprintf("Service status: %q isn't a status page or part this version checks, "+
			"so it was left out.", id))
	}

	return notes
}
