package prompts

import (
	"context"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/item"
)

// Seed adds each starter prompt this build ships that was never added before, and makes
// the review and work-on-issue starters the default prompts where no default is set yet.
// A starter that was added once is never added again, so one the user deleted stays
// deleted. A starter whose nickname is taken gets a free one. It adds all of them or,
// when a write fails, none.
func Seed(ctx context.Context, s *Store) error {
	return s.inTx(ctx, func(st *Store) error {
		seeded, err := st.seeded(ctx)
		if err != nil {
			return err
		}

		if seeded, err = seedMissing(ctx, st, seeded); err != nil {
			return err
		}

		return st.setSeeded(ctx, seeded)
	})
}

// seedMissing adds every starter not in seeded and returns seeded with their keys.
func seedMissing(ctx context.Context, st *Store, seeded []string) ([]string, error) {
	defaults, err := st.storedDefaults(ctx)
	if err != nil {
		return nil, err
	}

	for _, starter := range Starters() {
		if slices.Contains(seeded, starter.Key) {
			continue
		}

		if err = seedStarter(ctx, st, starter, defaults); err != nil {
			return nil, err
		}

		seeded = append(seeded, starter.Key)
	}

	return seeded, nil
}

// seedStarter adds one starter, and makes it the default for its kind when it is the
// review or work-on-issue starter and no default is set.
func seedStarter(ctx context.Context, st *Store, starter Starter, defaults DefaultPrompts) error {
	nickname, err := st.freeNickname(ctx, starter.Nickname)
	if err != nil {
		return err
	}

	in := starter.input()
	in.Nickname = nickname

	p := fromInput(in, agent.Checked{Models: starter.Models, Args: starter.ExtraArgs})
	p.StarterKey = starter.Key

	id, err := st.insert(ctx, p)
	if err != nil {
		return fmt.Errorf("add the %q starter prompt: %w", starter.Name, err)
	}

	switch {
	case starter.Key == StarterReviewPR && defaults.PR == 0:
		return st.setDefault(ctx, item.KindPR, id)
	case starter.Key == StarterWorkOnIssue && defaults.Issue == 0:
		return st.setDefault(ctx, item.KindIssue, id)
	default:
		return nil
	}
}
