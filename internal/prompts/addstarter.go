package prompts

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/uierr"
)

var (
	// ErrUnknownStarter is returned for a starter key this build doesn't ship.
	ErrUnknownStarter = errors.New("no starter prompt has that key")

	// errStarterNotStored is a starter added that the library doesn't hold after.
	errStarterNotStored = errors.New("the starter prompt isn't stored")
)

// AddStarter adds the starter key names to the library again, as the build ships it,
// when the library holds no copy of it, and returns the copy: the one it added, or the
// one already there. Seed never adds back a starter the user deleted, so this is how an
// Autopilot template whose prompt was deleted brings it back, on the user's word. Its
// nickname is the starter's, or a free one beside it, and it becomes a default only for
// a slot with none, as Seed adds a starter.
func (s *Service) AddStarter(ctx context.Context, key string) (Prompt, error) {
	starter, ok := starterFor(key)
	if !ok {
		return Prompt{}, uierr.New("That prompt isn't one this version ships.", fmt.Errorf("%w: %q", ErrUnknownStarter, key))
	}

	var id int64

	err := s.store.inTx(ctx, func(st *Store) error {
		var err error

		id, err = addStarterIn(ctx, st, starter)

		return err
	})
	if err != nil {
		return Prompt{}, err
	}

	s.saved(ctx)

	return s.get(ctx, id)
}

// addStarterIn adds starter to the library st holds, inside its transaction, unless a copy
// is there, and records it as added; it returns the copy's ID.
func addStarterIn(ctx context.Context, st *Store, starter Starter) (int64, error) {
	if id, found, err := starterCopy(ctx, st, starter.Key); err != nil || found {
		return id, err
	}

	defaults, err := st.storedDefaults(ctx)
	if err != nil {
		return 0, err
	}

	if err = seedStarter(ctx, st, starter, defaults); err != nil {
		return 0, err
	}

	seeded, err := st.seeded(ctx)
	if err != nil {
		return 0, err
	}

	if err = st.setSeeded(ctx, appendUnique(seeded, starter.Key)); err != nil {
		return 0, err
	}

	id, found, err := starterCopy(ctx, st, starter.Key)
	if err == nil && !found {
		err = fmt.Errorf("add the %q starter prompt: %w", starter.Name, errStarterNotStored)
	}

	return id, err
}

// starterCopy returns the ID of the library's copy of the starter key, and whether it holds
// one.
func starterCopy(ctx context.Context, st *Store, key string) (int64, bool, error) {
	all, err := st.All(ctx)
	if err != nil {
		return 0, false, err
	}

	if i := slices.IndexFunc(all, func(p Prompt) bool { return p.StarterKey == key }); i >= 0 {
		return all[i].ID, true, nil
	}

	return 0, false, nil
}
