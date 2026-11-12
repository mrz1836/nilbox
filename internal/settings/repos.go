package settings

import (
	"context"
	"fmt"

	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/prefs"
	"github.com/mrz1836/nilbox/internal/reporules"
)

// SetRepoRule pins or mutes a repository ("owner/name") or an org ("owner"), or takes its
// rule off with reporules.RuleNone, and returns the settings with it. A target is in one
// list at most, so pinning a muted one unmutes it. The rules are read and written in one
// transaction, so two changes never lose either. Once they are stored, an
// event.RepoRulesChanged is emitted, so the inbox, the menu bar, and the Release page
// show the change at once.
func (s *Service) SetRepoRule(ctx context.Context, target string, rule reporules.Rule) (Settings, error) {
	if _, err := reporules.ParseTarget(target); err != nil {
		return Settings{}, err
	}

	if !rule.Valid() {
		return Settings{}, fmt.Errorf("%w %q", reporules.ErrInvalidRule, rule)
	}

	saved, err := s.save(ctx, func(ctx context.Context, p *prefs.Store) error {
		stored, err := p.GetMany(ctx, reporules.Keys()...)
		if err != nil {
			return fmt.Errorf("read the pinned and muted repositories: %w", err)
		}

		rules, err := reporules.Decode(stored).Set(target, rule)
		if err != nil {
			return err
		}

		if err = p.SetMany(ctx, rules.Encode()); err != nil {
			return fmt.Errorf("save the pinned and muted repositories: %w", err)
		}

		return nil
	})
	if err != nil {
		return saved, err
	}

	s.events.Emit(event.RepoRulesChanged{})

	return saved, nil
}
