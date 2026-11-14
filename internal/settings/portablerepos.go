package settings

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/backup"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/reporules"
	"github.com/mrz1836/nilbox/internal/store"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// PortableRepos is the user's repositories in portable form, as a backup holds them: the
// clone folder, the other repository locations, and the pinned and muted repositories and
// orgs. Each field is a pointer, so one a backup lacks is left as it is.
type PortableRepos struct {
	CloneRoot *string                `json:"cloneRoot,omitempty"`
	Locations *[]localrepos.Location `json:"locations,omitempty"`
	Pinned    *[]string              `json:"pinned,omitempty"`
	Muted     *[]string              `json:"muted,omitempty"`
}

// PortableRepos returns the user's repositories in portable form, every field set.
func (bp *BackupPart) PortableRepos(ctx context.Context) (PortableRepos, error) {
	s := bp.s

	set, err := s.Get(ctx)
	if err != nil {
		return PortableRepos{}, err
	}

	return PortableRepos{
		CloneRoot: &set.CloneRoot, Locations: &set.CloneLocations, Pinned: &set.RepoRules.Pinned,
		Muted: &set.RepoRules.Muted,
	}, nil
}

// PlanRepos works out what importing r, a backup's repositories, does under mode,
// changing nothing. The clone folder is set when it exists here. Merge adds the file's
// locations that exist here, and pins or mutes each of the file's targets, its rule
// winning over this Mac's; Replace makes the locations exactly the file's that exist here,
// and each list the file holds exactly the file's. A location or a target that can't be
// added is left out with a note, as one the setters would refuse.
func (bp *BackupPart) PlanRepos(ctx context.Context, r PortableRepos, mode backup.Mode) (PortablePlan, error) {
	s := bp.s

	keys := append([]string{KeyCloneRoot, KeyCloneLocations}, reporules.Keys()...)

	stored, err := s.prefs.GetMany(ctx, keys...)
	if err != nil {
		return PortablePlan{}, fmt.Errorf("read the repository settings: %w", err)
	}

	out := PortablePlan{writes: map[string]string{}, tells: map[announcement]bool{}}
	root := s.planCloneRoot(&out, storedCloneRoot(stored), r.CloneRoot)

	if r.Locations != nil {
		if err = s.planLocations(ctx, &out, root, decodeCloneLocations(stored[KeyCloneLocations]), *r.Locations, mode); err != nil {
			return PortablePlan{}, err
		}
	}

	if r.Pinned != nil || r.Muted != nil {
		planRepoRules(&out, reporules.Decode(stored), r, mode)
	}

	return out, nil
}

// planCloneRoot plans the clone folder file names, when it names one, and returns the
// clone folder the import leaves: file's when it exists here, else root, the one now.
func (s *Service) planCloneRoot(out *PortablePlan, root string, file *string) string {
	if file == nil {
		return root
	}

	writes, err := s.checkCloneRoot(*file)
	if err != nil {
		out.AddNote(fmt.Sprintf("The clone folder %s isn't a folder on this Mac, so it stays %s.", strings.TrimSpace(*file), root))

		return root
	}

	next := writes[KeyCloneRoot]
	if next == root {
		out.Add(backup.Item{Label: "Clone folder", Change: backup.ChangeSame, Detail: next})

		return root
	}

	out.Add(backup.Item{Label: "Clone folder", Change: backup.ChangeUpdate, Detail: next + " (now " + root + ")"})
	maps.Copy(out.writes, writes)

	return next
}

// planLocations plans the other repository locations: the file's, each added as Add
// Location adds one, to those here under Merge, or to none under Replace.
func (s *Service) planLocations(ctx context.Context, out *PortablePlan, root string, before, file []localrepos.Location,
	mode backup.Mode,
) error {
	list := slices.Clone(before)
	if mode == backup.ModeReplace {
		list = []localrepos.Location{}
	}

	for _, loc := range file {
		next, err := s.addPortable(ctx, root, list, loc)
		if err != nil {
			out.AddNote(fmt.Sprintf("The location %s was left out. %s", strings.TrimSpace(loc.Path), uierr.Message(err)))

			continue
		}

		list = next
	}

	if !listLocations(out, before, list) && slices.Equal(list, before) {
		return nil
	}

	encoded, err := store.EncodeJSON(list)
	if err != nil {
		return err
	}

	out.writes[KeyCloneLocations] = encoded

	return nil
}

// addPortable adds loc, one of a file's locations, to list, the locations beside the clone
// root root, as AddCloneLocations adds one, its kind worked out again.
func (s *Service) addPortable(ctx context.Context, root string, list []localrepos.Location, loc localrepos.Location,
) ([]localrepos.Location, error) {
	add, err := s.checkLocation(ctx, loc.Path)
	if err != nil {
		return nil, err
	}

	return s.addLocation(root, list, add)
}

// listLocations lists the locations an import leaves, list, against those here, before,
// and reports whether it adds or removes any.
func listLocations(out *PortablePlan, before, list []localrepos.Location) bool {
	changed := false

	for _, l := range list {
		change := backup.ChangeSame
		if !holdsPath(before, l.Path) {
			change, changed = backup.ChangeAdd, true
		}

		out.Add(backup.Item{Label: l.Path, Change: change, Detail: "repository location"})
	}

	for _, b := range before {
		if !holdsPath(list, b.Path) {
			out.Add(backup.Item{Label: b.Path, Change: backup.ChangeRemove, Detail: "repository location"})

			changed = true
		}
	}

	return changed
}

// holdsPath reports whether list holds a location stored as path.
func holdsPath(list []localrepos.Location, path string) bool {
	return slices.ContainsFunc(list, func(l localrepos.Location) bool { return l.Path == path })
}

// planRepoRules plans the pinned and muted repositories and orgs: each of the file's set
// over those here under Merge, so its rule wins for a target in both; under Replace, each
// list the file holds starts empty.
func planRepoRules(out *PortablePlan, before reporules.Rules, r PortableRepos, mode backup.Mode) {
	rules := reporules.Rules{Pinned: slices.Clone(before.Pinned), Muted: slices.Clone(before.Muted)}

	if mode == backup.ModeReplace && r.Pinned != nil {
		rules.Pinned = []string{}
	}

	if mode == backup.ModeReplace && r.Muted != nil {
		rules.Muted = []string{}
	}

	rules = setTargets(out, rules, r.Pinned, reporules.RulePinned)
	rules = setTargets(out, rules, r.Muted, reporules.RuleMuted)

	listRepoRules(out, before, rules)

	if !rules.Equal(before) {
		maps.Copy(out.writes, rules.Encode())
		out.tells[tellsRepoRules] = true
	}
}

// setTargets returns rules with rule set for each of targets, nil for none, noting each it
// refuses.
func setTargets(out *PortablePlan, rules reporules.Rules, targets *[]string, rule reporules.Rule) reporules.Rules {
	if targets == nil {
		return rules
	}

	for _, target := range *targets {
		next, err := rules.Set(target, rule)
		if err != nil {
			out.AddNote(fmt.Sprintf("%s was left out. %s", strings.TrimSpace(target), uierr.Message(err)))

			continue
		}

		rules = next
	}

	return rules
}

// listRepoRules lists each target the rules an import leaves, rules, hold, against those
// here, before, and each it takes away.
func listRepoRules(out *PortablePlan, before, rules reporules.Rules) {
	for _, target := range slices.Concat(rules.Pinned, rules.Muted) {
		now, was := ruleOf(rules, target), ruleOf(before, target)

		switch {
		case was == reporules.RuleNone:
			out.Add(backup.Item{Label: target, Change: backup.ChangeAdd, Detail: string(now)})
		case was != now:
			out.Add(backup.Item{Label: target, Change: backup.ChangeUpdate, Detail: string(now) + " (now " + string(was) + ")"})
		default:
			out.Add(backup.Item{Label: target, Change: backup.ChangeSame, Detail: string(now)})
		}
	}

	for _, target := range slices.Concat(before.Pinned, before.Muted) {
		if ruleOf(rules, target) == reporules.RuleNone {
			out.Add(backup.Item{Label: target, Change: backup.ChangeRemove, Detail: string(ruleOf(before, target))})
		}
	}
}

// ruleOf returns the rule rules give target itself, ignoring case.
func ruleOf(rules reporules.Rules, target string) reporules.Rule {
	same := func(t string) bool { return strings.EqualFold(t, target) }

	switch {
	case slices.ContainsFunc(rules.Pinned, same):
		return reporules.RulePinned
	case slices.ContainsFunc(rules.Muted, same):
		return reporules.RuleMuted
	default:
		return reporules.RuleNone
	}
}
