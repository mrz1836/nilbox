package settings

import (
	"context"

	"github.com/mrz1836/nilbox/internal/heldruns"
)

// KeyIgnoredWorkflows is the names of the workflows whose held runs never count toward
// where a pull request stands, on the inbox's cards and in the review sheet, as a JSON list
// (see the heldruns package). The inbox and the review sheet read it; a key never stored
// holds none.
const KeyIgnoredWorkflows = heldruns.KeyIgnoredWorkflows

// SetIgnoredWorkflows stores the names of the workflows whose held runs never count, in
// order, and returns the settings with them. Each is trimmed, and blank ones and exact
// repeats are left out; an empty list ignores none. A list that holds too many names, one
// too long, or one with a control character is refused, and nothing is stored.
func (s *Service) SetIgnoredWorkflows(ctx context.Context, names []string) (Settings, error) {
	values, err := checkIgnoredWorkflows(names)
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setMany(values, ""))
}

// checkIgnoredWorkflows refuses names SetIgnoredWorkflows would, and returns their write,
// cleaned.
func checkIgnoredWorkflows(names []string) (map[string]string, error) {
	encoded, err := heldruns.Encode(names)
	if err != nil {
		return nil, err
	}

	return map[string]string{KeyIgnoredWorkflows: encoded}, nil
}
