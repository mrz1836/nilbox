package prompts

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
)

// Use is one place a launch default names a prompt: an org's (Target.Repo "") or a
// repository's default prompt for one kind of item.
type Use struct {
	PromptID int64
	Kind     item.Kind
	Target   repo.Name
}

// UsageSource lists every place a launch default names a prompt, so a prompt's usage can
// say which orgs and repositories use it. The launch defaults implement it.
type UsageSource interface {
	PromptUses(ctx context.Context) ([]Use, error)
}

// loadUsage reads where every prompt is a default: the app's default prompts, and the
// org and repository launch defaults.
func (s *Service) loadUsage(ctx context.Context) (map[int64]Usage, error) {
	defaults, err := s.store.storedDefaults(ctx)
	if err != nil {
		return nil, err
	}

	var uses []Use

	if s.usage != nil {
		if uses, err = s.usage.PromptUses(ctx); err != nil {
			return nil, fmt.Errorf("read where the prompts are used: %w", err)
		}
	}

	usage := map[int64]Usage{}

	addDefault := func(id int64, kind item.Kind) {
		if id != 0 {
			u := usage[id]
			u.Defaults = append(u.Defaults, string(kind))
			u.Kinds = appendUnique(u.Kinds, string(kind))
			usage[id] = u
		}
	}

	addDefault(defaults.PR, item.KindPR)
	addDefault(defaults.Issue, item.KindIssue)

	for _, use := range uses {
		addPlace(usage, use)
	}

	return usage, nil
}

// usageOf returns one prompt's usage, its lists in the order they were found.
func (s *Service) usageOf(ctx context.Context, id int64) (Usage, error) {
	usage, err := s.loadUsage(ctx)
	if err != nil {
		return Usage{}, err
	}

	return usage[id], nil
}

// addPlace records that a launch default uses a prompt.
func addPlace(usage map[int64]Usage, use Use) {
	u := usage[use.PromptID]
	if use.Target.Repo == "" {
		u.Orgs = appendUnique(u.Orgs, use.Target.Owner)
	} else {
		u.Repos = appendUnique(u.Repos, use.Target.String())
	}

	u.Kinds = appendUnique(u.Kinds, string(use.Kind))
	usage[use.PromptID] = u
}

// appendUnique appends s unless list already holds it.
func appendUnique(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}

	return append(list, s)
}

// normalizeUsage sorts a usage's lists and turns nil lists into empty ones.
func normalizeUsage(u Usage) Usage {
	for _, list := range []*[]string{&u.Defaults, &u.Orgs, &u.Repos, &u.Kinds} {
		if *list == nil {
			*list = []string{}
		}

		sort.Strings(*list)
	}

	return u
}

// describeUsage says where a prompt is a default, as in "the default for pull requests,
// and a default for acme-org and 2 repositories".
func describeUsage(u Usage) string {
	var parts []string

	for _, kind := range u.Defaults {
		parts = append(parts, "the default for "+item.Kind(kind).Plural())
	}

	var places []string

	if len(u.Orgs) > 0 {
		places = append(places, countedNames(u.Orgs, "orgs"))
	}

	if len(u.Repos) > 0 {
		places = append(places, countedNames(u.Repos, "repositories"))
	}

	if len(places) > 0 {
		parts = append(parts, "a default for "+strings.Join(places, " and "))
	}

	if len(parts) == 0 {
		return "not used anywhere"
	}

	return strings.Join(parts, ", and ")
}

// countedNames names one item, or counts several: "acme-org" or "3 orgs".
func countedNames(names []string, many string) string {
	if len(names) == 1 {
		return names[0]
	}

	return fmt.Sprintf("%d %s", len(names), many)
}
