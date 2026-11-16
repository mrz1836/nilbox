package fence

import (
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/sensitive"
)

// MaxChanged caps the paths a run's changed files are checked and kept for; a run that
// changed more is checked as far as that, and its check is incomplete.
const MaxChanged = 3000

// changeAgent is the change a run's file is reported with: its commits changed it.
const changeAgent = "changed"

// CheckChanged checks the files a run's commits changed, as git lists them from the head
// the run started at to its last commit, against the sensitive categories: the fence's
// denied ones always (agent, workflow, and tooling files), and the others as the user
// turned them on. The check is at head, the commit the files are in; one that lists more
// than MaxChanged files is incomplete. A run whose check holds an agent file holds the
// next launch on its item at that head, as an outside contributor's pull request would.
func CheckChanged(head string, files []string, cfg sensitive.Config) sensitive.Check {
	r := &sensitive.Result{Head: head, Total: len(files), Files: []sensitive.File{}, Complete: len(files) <= MaxChanged}

	for _, path := range files[:min(len(files), MaxChanged)] {
		if path = strings.TrimSpace(path); path != "" {
			r.Files = append(r.Files, sensitive.File{Path: path, Change: changeAgent})
		}
	}

	return sensitive.NewMatcher(changedConfig(cfg)).Evaluate(r, head, "")
}

// MergeChanged adds what agent runs changed on an item (CheckChanged) to its own file
// check, as a launch decides on both: check is nil for an item whose files aren't checked,
// a trusted bot's or a member's pull request, whose run's changes stand alone. Each of
// changed's files joins its category's group, so an agent file a run wrote is the item's
// agent file; a run's incomplete list makes the whole check incomplete.
func MergeChanged(check *sensitive.Check, changed sensitive.Check) sensitive.Check {
	if check == nil {
		return changed
	}

	out := *check
	out.Groups = slices.Clone(check.Groups)
	added := false

	for _, g := range changed.Groups {
		var more bool

		out.Groups, more = mergeGroup(out.Groups, g)
		added = added || more
	}

	order := categoryOrder()

	slices.SortStableFunc(out.Groups, func(a, b sensitive.Group) int { return order(a.Category) - order(b.Category) })

	if added {
		out.Status = sensitive.StatusFlagged
	}

	if !changed.Complete {
		out.Complete = false
	}

	return out
}

// mergeGroup adds a group's files to its category's among groups, or the group itself,
// and reports whether any file was new.
func mergeGroup(groups []sensitive.Group, g sensitive.Group) ([]sensitive.Group, bool) {
	i := slices.IndexFunc(groups, func(have sensitive.Group) bool { return have.Category == g.Category })
	if i < 0 {
		return append(groups, g), true
	}

	merged := groups[i]
	merged.Files = slices.Clone(merged.Files)
	added := false

	for _, f := range g.Files {
		if !slices.ContainsFunc(merged.Files, func(have sensitive.Finding) bool { return have.Path == f.Path }) {
			merged.Files = append(merged.Files, f)
			added = true
		}
	}

	groups[i] = merged

	return groups, added
}

// categoryOrder ranks a category by the order files are matched against them, the user's
// own last.
func categoryOrder() func(id string) int {
	cats := sensitive.Categories()

	ids := make([]string, 0, len(cats))
	for _, c := range cats {
		ids = append(ids, c.ID)
	}

	return func(id string) int {
		if i := slices.Index(ids, id); i >= 0 {
			return i
		}

		return len(ids)
	}
}

// changedConfig is cfg with the fence's denied categories always on.
func changedConfig(cfg sensitive.Config) sensitive.Config {
	out := cfg
	out.Off = slices.DeleteFunc(slices.Clone(cfg.Off), func(id string) bool {
		return slices.Contains(DeniedCategories(), id)
	})

	return out
}
