package prompts

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/textutil"
)

// Use is one place a launch default names a prompt: an org's (Target.Repo "") or a
// repository's default prompt for one kind of item, or for one situation a pull request can
// be in.
type Use struct {
	PromptID int64
	Kind     item.Kind
	Target   repo.Name

	// Slot is the default: DefaultSlotPR or DefaultSlotIssue for a kind's (or "", the
	// kind's), or a situation's slot, whose Kind is item.KindPR.
	Slot DefaultSlot
}

// UsageSource lists every place a launch default names a prompt, so a prompt's usage can
// say which orgs and repositories use it. The launch defaults implement it.
type UsageSource interface {
	PromptUses(ctx context.Context) ([]Use, error)
}

// loadUsage reads where every prompt is a default: the app's default prompts, the org and
// repository launch defaults, and the default replies.
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

	usage := defaultUsage(defaults)

	for _, use := range uses {
		addPlace(usage, use)
	}

	if err = s.addRoutineUses(ctx, usage); err != nil {
		return nil, err
	}

	return usage, nil
}

// defaultUsage is where the app's default prompts and default replies use each prompt.
func defaultUsage(defaults DefaultPrompts) map[int64]Usage {
	usage := map[int64]Usage{}

	addDefault := func(id int64, kind item.Kind) {
		if id != 0 {
			u := usage[id]
			u.Defaults = append(u.Defaults, kind)
			u.Kinds = appendUnique(u.Kinds, kind)
			usage[id] = u
		}
	}

	addDefault(defaults.PR, item.KindPR)
	addDefault(defaults.Issue, item.KindIssue)

	for _, slot := range situationSlots() {
		if id := defaults.Slot(slot); id != 0 {
			u := usage[id]
			u.Slots = append(u.Slots, slot)
			u.Kinds = appendUnique(u.Kinds, item.KindPR)
			usage[id] = u
		}
	}

	for _, slot := range ReplySlots() {
		if id := defaults.Replies.For(slot); id != 0 {
			u := usage[id]
			u.Replies = append(u.Replies, slot)
			usage[id] = u
		}
	}

	return usage
}

// addRoutineUses records in usage which routines name each prompt: by its starter's key,
// or its nickname, ignoring case.
func (s *Service) addRoutineUses(ctx context.Context, usage map[int64]Usage) error {
	if s.routines == nil {
		return nil
	}

	uses, err := s.routines.RoutineUses(ctx)
	if err != nil {
		return fmt.Errorf("read where the routines use the prompts: %w", err)
	}

	if len(uses) == 0 {
		return nil
	}

	all, err := s.store.All(ctx)
	if err != nil {
		return err
	}

	for _, use := range uses {
		addRoutineUse(usage, all, use)
	}

	return nil
}

// addRoutineUse records in usage that use's routine names the prompt of all it names.
func addRoutineUse(usage map[int64]Usage, all []Prompt, use RoutineUse) {
	for _, p := range all {
		if names(p, use) {
			u := usage[p.ID]
			u.Routines = appendUnique(u.Routines, use.Routine)
			usage[p.ID] = u
		}
	}
}

// names reports whether a routine's use names p: a starter's key names the user's copy of
// that starter, and a nickname the prompt that has it, ignoring case.
func names(p Prompt, use RoutineUse) bool {
	if use.Starter != "" {
		return p.StarterKey == use.Starter
	}

	return use.Nickname != "" && strings.EqualFold(p.Nickname, use.Nickname)
}

// refuseRoutineUse returns an InUseError for the prompt id when a routine runs it, saying
// which, and nil when none does or it can't be read: the store's own check still holds.
func (s *Service) refuseRoutineUse(ctx context.Context, id int64) error {
	if s.routines == nil {
		return nil
	}

	usage, err := s.usageOf(ctx, id)
	if err != nil {
		return err
	}

	if len(usage.Routines) == 0 {
		return nil
	}

	p, err := s.store.Lookup(ctx, id)
	if err != nil {
		return err
	}

	return &InUseError{Nickname: p.Nickname, Usage: normalizeUsage(usage)}
}

// usageOf returns one prompt's usage, its lists in the order they were found.
func (s *Service) usageOf(ctx context.Context, id int64) (Usage, error) {
	usage, err := s.loadUsage(ctx)
	if err != nil {
		return Usage{}, err
	}

	return usage[id], nil
}

// addPlace records that a launch default uses a prompt: a kind's among the orgs or the
// repositories, a situation's among the slot places.
func addPlace(usage map[int64]Usage, use Use) {
	u := usage[use.PromptID]

	switch {
	case use.Slot.situation() && use.Target.Repo == "":
		u.SlotPlaces = appendUnique(u.SlotPlaces, SlotPlace{Org: use.Target.Owner, Slot: use.Slot})
	case use.Slot.situation():
		u.SlotPlaces = appendUnique(u.SlotPlaces, SlotPlace{Repo: use.Target.String(), Slot: use.Slot})
	case use.Target.Repo == "":
		u.Orgs = appendUnique(u.Orgs, use.Target.Owner)
	default:
		u.Repos = appendUnique(u.Repos, use.Target.String())
	}

	kind := use.Kind
	if use.Slot.situation() {
		kind = item.KindPR
	}

	u.Kinds = appendUnique(u.Kinds, kind)
	usage[use.PromptID] = u
}

// appendUnique appends s unless list already holds it.
func appendUnique[S comparable](list []S, s S) []S {
	if slices.Contains(list, s) {
		return list
	}

	return append(list, s)
}

// normalizeUsage sorts a usage's lists, keeping the situations in DefaultSlots order and
// the default replies in ReplySlots order, and turns nil lists into empty ones.
func normalizeUsage(u Usage) Usage {
	u.Defaults, u.Kinds = sortedList(u.Defaults), sortedList(u.Kinds)
	u.Slots = textutil.NonNil(u.Slots)
	slices.SortFunc(u.Slots, compareSlots)
	u.Orgs, u.Repos = sortedList(u.Orgs), sortedList(u.Repos)
	u.SlotPlaces = textutil.NonNil(u.SlotPlaces)
	slices.SortFunc(u.SlotPlaces, func(a, b SlotPlace) int {
		return cmp.Or(cmp.Compare(a.target(), b.target()), compareSlots(a.Slot, b.Slot))
	})
	u.Replies = textutil.NonNil(u.Replies)
	u.Routines = sortedList(u.Routines)

	return u
}

// compareSlots orders defaults as DefaultSlots lists them.
func compareSlots(a, b DefaultSlot) int {
	return cmp.Compare(slices.Index(DefaultSlots(), a), slices.Index(DefaultSlots(), b))
}

// sortedList sorts list in place and returns it, or an empty list for nil.
func sortedList[S ~string](list []S) []S {
	list = textutil.NonNil(list)
	slices.Sort(list)

	return list
}

// describeUsage says where a prompt is a default, as in "the default for pull requests,
// and a default for acme-org and 2 repositories".
func describeUsage(u Usage) string {
	var parts []string

	for _, kind := range u.Defaults {
		parts = append(parts, "the default for "+kind.Plural())
	}

	for _, slot := range u.Slots {
		parts = append(parts, slot.describe())
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

	if words := slotPlacesWords(u.SlotPlaces); words != "" {
		parts = append(parts, words)
	}

	switch len(u.Routines) {
	case 0:
	case 1:
		parts = append(parts, "used by the routine “"+u.Routines[0]+"”")
	default:
		parts = append(parts, fmt.Sprintf("used by %d routines", len(u.Routines)))
	}

	if len(parts) == 0 {
		return "not used anywhere"
	}

	return strings.Join(parts, ", and ")
}

// slotPlacesWords says where orgs and repositories make a prompt a situation's, as in
// "acme-org's prompt when checks fail" or "a situation's prompt in 3 places"; "" for none.
func slotPlacesWords(places []SlotPlace) string {
	switch len(places) {
	case 0:
		return ""
	case 1:
		return places[0].target() + "'s prompt " + places[0].Slot.When()
	default:
		return fmt.Sprintf("a situation's prompt in %d places", len(places))
	}
}

// countedNames names one item, or counts several: "acme-org" or "3 orgs".
func countedNames(names []string, many string) string {
	if len(names) == 1 {
		return names[0]
	}

	return fmt.Sprintf("%d %s", len(names), many)
}
