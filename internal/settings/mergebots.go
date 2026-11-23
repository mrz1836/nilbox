package settings

import (
	"context"

	"github.com/mrz1836/nilbox/internal/prefs/prefval"
	"github.com/mrz1836/nilbox/internal/trust"
)

// KeyMergeBotsOn is whether a pull request's card says when one of the user's auto-merge
// bots approved it, so it merges by itself once its checks pass, and KeyMergeBots lists
// those bots (see trust.MergeBots). The inbox reads both; the switch starts off and the
// list empty.
const (
	KeyMergeBotsOn = trust.PrefMergeBotsOn
	KeyMergeBots   = trust.PrefMergeBots
)

// SetMergeBotsOn stores whether a pull request's card says when a listed auto-merge bot
// approved it, and returns the settings with it.
func (s *Service) SetMergeBotsOn(ctx context.Context, on bool) (Settings, error) {
	return s.save(ctx, setting(KeyMergeBotsOn, prefval.EncodeBool(on)))
}

// SetMergeBots stores the user's auto-merge bots, and returns the settings with them. Each
// login may be written as GitHub shows it ("acme-org-automerge", "acme-org-automerge[bot]",
// or "app/acme-org-automerge"); the list is stored normalized and sorted, and may be empty.
// A login that is not a bot's, or a list of more than 100, is refused, and nothing is
// stored.
func (s *Service) SetMergeBots(ctx context.Context, bots []string) (Settings, error) {
	values, err := checkMergeBots(bots)
	if err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setMany(values, ""))
}

// checkMergeBots refuses a list SetMergeBots would, and returns its write, normalized and
// sorted.
func checkMergeBots(bots []string) (map[string]string, error) {
	normalized, err := trust.NormalizeMergeBots(bots)
	if err != nil {
		return nil, err
	}

	return map[string]string{KeyMergeBots: prefval.EncodeList(normalized)}, nil
}
