package autopilot

import (
	"context"
	"slices"

	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// MatchStored counts what the draft d matches now among the items the inbox stored in the
// lists d watches, that its routine (d.ID, 0 for a new one) has no record of and doesn't
// leave alone, by kind: the count Describe it's card shows, "Matches 3 items in your inbox
// now". It decides from what the inbox stored alone and never asks GitHub, so an item
// whose decision waits on a read, such as its check names, counts as unknown. A search's
// items are Test's to read, so a draft that watches a search says its count may be short
// (Incomplete), as one does that watches more items than a test tries. A draft whose spec
// has problems is refused with them, as Save refuses it. It changes nothing, and works
// with Autopilot off.
func (s *Service) MatchStored(ctx context.Context, d Draft) (Matching, error) {
	_, sp, err := checkSpec(d.Spec)
	if err != nil {
		return Matching{}, err
	}

	if s.items == nil {
		return Matching{}, uierr.New("The inbox can't be read, so the routine's matches can't be counted.", ErrNoItems)
	}

	ids, err := s.items.WatchedItems(ctx, nil, listsOf(sp))
	if err != nil {
		return Matching{}, uierr.New("The inbox's lists couldn't be read.", err)
	}

	out := Matching{Incomplete: watchesSearches(sp) || len(ids) > maxTestItems}

	items, _, err := s.decide(ctx, sp, ids[:min(len(ids), maxTestItems)], s.clock.Now().UTC())
	if err != nil {
		return Matching{}, err
	}

	if err = s.markLeftAlone(ctx, d.ID, items); err != nil {
		return Matching{}, err
	}

	recorded, err := s.recorded(ctx, d.ID)
	if err != nil {
		return Matching{}, err
	}

	for _, it := range items {
		out.add(it, recorded)
	}

	return out, nil
}

// watchesSearches reports a spec with a search among its sources.
func watchesSearches(sp spec.Spec) bool {
	return slices.ContainsFunc(sp.Watch.Sources, func(src spec.Source) bool { return src.Search != nil })
}
