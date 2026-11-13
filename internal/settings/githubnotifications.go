package settings

import (
	"context"
	"errors"
	"fmt"

	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/prefs/prefval"
	"github.com/mrz1836/nilbox/internal/threadrules"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// SetGitHubNotificationSwitch turns one of the GitHub notification rules' switches on or
// off (see threadrules.Switches), and returns the settings with it. Nothing else hears of
// it: what reads and clears the notifications reads the rules as it runs.
func (s *Service) SetGitHubNotificationSwitch(ctx context.Context, name threadrules.Switch, on bool) (Settings, error) {
	if !name.Valid() {
		return Settings{}, uierr.New("That isn't a setting of GitHub notifications.",
			fmt.Errorf("%w: %q", threadrules.ErrUnknownSwitch, name))
	}

	return s.save(ctx, setting(name.Key(), prefval.EncodeBool(on)))
}

// SetGitHubNotificationTrigger turns one of the moments that clear a GitHub notification
// on or off (see threadrules.Triggers), and returns the settings with it. Clearing by hand
// has no switch and is refused, as is a trigger this build does not know. The triggers
// that are off are read and written in one transaction, so two changes never lose
// either.
func (s *Service) SetGitHubNotificationTrigger(ctx context.Context, trigger threadrules.Trigger, on bool,
) (Settings, error) {
	// SetTrigger refuses a trigger without a switch whatever the list, so it is checked
	// before anything is read.
	if _, err := threadrules.SetTrigger(nil, trigger, on); err != nil {
		return Settings{}, triggerError(trigger, err)
	}

	return s.save(ctx, func(ctx context.Context, p *prefs.Store) error {
		err := p.Update(ctx, threadrules.KeyTriggersOff, func(stored string, _ bool) (string, error) {
			off, err := threadrules.SetTrigger(threadrules.DecodeTriggersOff(stored), trigger, on)
			if err != nil {
				return "", err
			}

			return threadrules.EncodeTriggersOff(off), nil
		})
		if err != nil {
			return fmt.Errorf("save the GitHub notification triggers: %w", err)
		}

		return nil
	})
}

// SetGitHubNotificationAction stores what clearing a GitHub notification does, mark it
// done or mark it read, and returns the settings with it. An action this build does not
// know is refused.
func (s *Service) SetGitHubNotificationAction(ctx context.Context, action threadrules.Action) (Settings, error) {
	stored, err := threadrules.EncodeAction(action)
	if err != nil {
		return Settings{}, uierr.New("Choose to mark a cleared notification done or read.",
			fmt.Errorf("%w: %q", err, action))
	}

	return s.save(ctx, setting(threadrules.KeyAction, stored))
}

// SetGitHubCleanupCardDismissed stores whether the card that first offers to clean up the
// user's GitHub notifications is put away ("Not now"), and returns the settings with it.
func (s *Service) SetGitHubCleanupCardDismissed(ctx context.Context, dismissed bool) (Settings, error) {
	return s.save(ctx, setting(threadrules.KeyCleanupCardDismissed, prefval.EncodeBool(dismissed)))
}

// triggerError words SetTrigger's refusal of trigger for the window.
func triggerError(trigger threadrules.Trigger, err error) error {
	if errors.Is(err, threadrules.ErrNoSwitch) {
		return uierr.New("Clearing a notification by hand has no switch: it is always on.",
			fmt.Errorf("%w: %q", err, trigger))
	}

	return uierr.New("That isn't a moment that clears a GitHub notification.", fmt.Errorf("%w: %q", err, trigger))
}
