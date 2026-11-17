package autopilot

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/item"
)

// Why didn't a routine run?: beside what a routine decides about an item now (Explain's
// trace), what of the routine itself holds it back, why the item isn't in what it
// watches, and every record its ledger keeps of the two. Each is read from what is stored,
// and asks GitHub nothing.

// maxExplainRecords caps the records Explain lists.
const maxExplainRecords = 20

// routineLines says what of r itself holds it back on every item: switched off, Autopilot
// paused or off, Dry run, another account's, and what this Mac lacks for it.
func (s *Service) routineLines(ctx context.Context, r Routine) []string {
	out := []string{}
	settings := s.settings(ctx)

	switch {
	case !settings.Enabled:
		out = append(out, "Autopilot is off, so no routine runs. Turn it on in Settings → Labs.")
	case settings.Paused:
		out = append(out, "Autopilot is paused, so nothing new starts until you resume it.")
	}

	for _, line := range []struct {
		on    bool
		words string
	}{
		{!r.Enabled, "It's switched off, so it runs on nothing."},
		{r.OtherAccount, fmt.Sprintf("It belongs to @%s, so it doesn't run while another account is signed in.", r.Account)},
		{r.Mode == autopilotprefs.ModeDryRun, "It's in Dry run: it records what it would do, and does nothing else."},
	} {
		if line.on {
			out = append(out, line.words)
		}
	}

	return append(out, s.notReadyLines(r)...)
}

// notReadyLines say what this Mac lacks for r, an enabled routine that loads, as its
// readiness checks last found it.
func (s *Service) notReadyLines(r Routine) []string {
	if !r.Enabled || r.BrokenReason != "" {
		return nil
	}

	rr, ok := s.ready.snapshot().routineReady(r.ID, r.Name, r.Spec, false)
	if !ok || rr.Ready {
		return nil
	}

	out := make([]string, 0, len(rr.Problems))
	for _, c := range rr.Problems {
		out = append(out, "This Mac isn't ready for it: "+c.Words())
	}

	return out
}

// watchMisses says, a sentence a source, why the item f is in none of sp's sources, from
// the facts stored: the inbox list it isn't in, or the first field of a search it doesn't
// fit. A search whose fields it fits, as far as the facts tell, says its rows come with
// the next refresh, since a search's rows are what the inbox last read.
func watchMisses(sp spec.Spec, f facts.Facts, viewer string) []string {
	out := []string{}

	for _, src := range sp.Watch.Sources {
		switch {
		case len(src.Lists) > 0:
			out = append(out, listMiss(src.Lists, f))
		case src.Search != nil:
			if miss := searchMiss(*src.Search, f, viewer); miss != "" {
				out = append(out, "Not in its search: "+miss)
			} else {
				out = append(out, "Its search should find it, but the last refresh didn't bring it: "+
					"it shows once a refresh reads the search again.")
			}
		}
	}

	return out
}

// listMiss says which of lists the item isn't in.
func listMiss(lists []catalog.ListSource, f facts.Facts) string {
	label := func(l catalog.ListSource) string {
		if info, ok := catalog.ListSourceByID(l); ok {
			return info.Label
		}

		return string(l)
	}

	labels := make([]string, 0, len(lists))
	for _, l := range lists {
		labels = append(labels, label(l))
	}

	listed := f.Value(catalog.FieldLists).List
	in := make([]string, 0, len(listed))

	for _, l := range listed {
		in = append(in, label(catalog.ListSource(l)))
	}

	if len(in) == 0 {
		return fmt.Sprintf("It isn't in your %s %s.", series(labels), plural(len(labels), "list", "lists"))
	}

	return fmt.Sprintf("It isn't in your %s %s: it's in your %s %s.", series(labels), plural(len(labels), "list", "lists"),
		series(in), plural(len(in), "list", "lists"))
}

// plural picks one or many by n.
func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}

	return many
}

// searchMiss says the first field of s the item f doesn't fit, as far as its facts tell,
// "" when it fits every one they tell of.
func searchMiss(s watch.Search, f facts.Facts, viewer string) string {
	checks := []func() string{
		func() string { return kindMiss(s, f) },
		func() string { return authorMiss(s, f, viewer) },
		func() string { return whereMiss(s, f, viewer) },
		func() string { return labelsMiss(s, f) },
		func() string { return branchMiss(s, f) },
		func() string { return stateMiss(s, f) },
	}

	for _, check := range checks {
		if miss := check(); miss != "" {
			return miss
		}
	}

	return ""
}

// kindMiss: an issue in a search of pull requests, or the other way about.
func kindMiss(s watch.Search, f facts.Facts) string {
	if s.Kind == "" || s.Kind == f.Item.Kind {
		return ""
	}

	return fmt.Sprintf("it's %s, and the search finds %s.", articled(f.Item.Kind), s.Kind.Plural())
}

// articled names a kind with its article.
func articled(k item.Kind) string {
	if k == item.KindIssue {
		return "an issue"
	}

	return "a pull request"
}

// authorMiss: an author the search doesn't name.
func authorMiss(s watch.Search, f facts.Facts, viewer string) string {
	login := f.Value(catalog.FieldAuthor).Text
	if len(s.Authors) == 0 || login == "" {
		return ""
	}

	for _, a := range s.Authors {
		if sameAuthor(a, login, viewer) {
			return ""
		}
	}

	names := make([]string, 0, len(s.Authors))
	for _, a := range s.Authors {
		names = append(names, authorWords(a))
	}

	return fmt.Sprintf("its author is %s, not %s.", login, orSeries(names))
}

// sameAuthor reports an author a search names: you, a bot as its app, or a login.
func sameAuthor(a, login, viewer string) bool {
	bare := strings.TrimSuffix(strings.ToLower(login), "[bot]")

	switch {
	case a == "@me":
		return viewer != "" && strings.EqualFold(login, viewer)
	case strings.HasPrefix(a, "app/"):
		return strings.EqualFold(strings.TrimPrefix(a, "app/"), bare)
	default:
		return strings.EqualFold(a, login)
	}
}

// authorWords names a search's author: you, a bot by its name, or a login.
func authorWords(a string) string {
	switch {
	case a == "@me":
		return "you"
	case strings.HasPrefix(a, "app/"):
		return botName(strings.TrimPrefix(a, "app/"))
	default:
		return a
	}
}

// whereMiss: a repository in none of the places the search looks.
func whereMiss(s watch.Search, f facts.Facts, viewer string) string {
	if len(s.Where) == 0 || f.Item.Repo == "" {
		return ""
	}

	owner, _, _ := strings.Cut(f.Item.Repo, "/")

	for _, w := range s.Where {
		if inPlace(w, owner, f.Item.Repo, viewer) {
			return ""
		}
	}

	places := make([]string, 0, len(s.Where))
	for _, w := range s.Where {
		places = append(places, placeWords(w))
	}

	return fmt.Sprintf("it's in %s, not %s.", f.Item.Repo, orSeries(places))
}

// inPlace reports a repository of owner, named repoName, that a search's place holds.
func inPlace(where, owner, repoName, viewer string) bool {
	kind, value, _ := strings.Cut(where, ":")

	switch {
	case where == "@me":
		return viewer != "" && strings.EqualFold(owner, viewer)
	case kind == "repo":
		return strings.EqualFold(value, repoName)
	default:
		return strings.EqualFold(value, owner)
	}
}

// placeWords names a search's place: your account, an owner, or a repository.
func placeWords(where string) string {
	if where == "@me" {
		return "your account"
	}

	_, value, ok := strings.Cut(where, ":")
	if !ok {
		return where
	}

	return value
}

// labelsMiss: none of the labels the search asks for, or one it leaves out.
func labelsMiss(s watch.Search, f facts.Facts) string {
	v := f.Value(catalog.FieldLabels)
	if !v.Known() {
		return ""
	}

	has := func(label string) bool {
		return slices.ContainsFunc(v.List, func(l string) bool { return strings.EqualFold(l, label) })
	}

	if len(s.Labels) > 0 && !slices.ContainsFunc(s.Labels, has) {
		return fmt.Sprintf("it isn't labeled %s.", orSeries(s.Labels))
	}

	for _, l := range s.NotLabels {
		if has(l) {
			return fmt.Sprintf("it's labeled %s, which the search leaves out.", l)
		}
	}

	return ""
}

// branchMiss: a pull request into another base, or from a branch of another prefix.
func branchMiss(s watch.Search, f facts.Facts) string {
	if base := f.Value(catalog.FieldBase); s.Base != "" && base.Known() && !strings.EqualFold(base.Text, s.Base) {
		return fmt.Sprintf("it merges into %s, not %s.", base.Text, s.Base)
	}

	if head := f.Value(catalog.FieldHead); s.HeadPrefix != "" && head.Known() && !strings.HasPrefix(head.Text, s.HeadPrefix) {
		return fmt.Sprintf("its branch %s doesn't start with %s.", head.Text, s.HeadPrefix)
	}

	return ""
}

// stateMiss: a draft in a search of pull requests out of draft, or the other way about.
func stateMiss(s watch.Search, f facts.Facts) string {
	draft := f.Value(catalog.FieldDraft)
	if !draft.Known() {
		return ""
	}

	switch {
	case slices.Contains(s.State, watch.StateReady) && draft.Bool:
		return "it's a draft, and the search finds pull requests out of draft."
	case slices.Contains(s.State, watch.StateDraft) && !draft.Bool:
		return "it isn't a draft, and the search finds drafts."
	}

	return ""
}

// orSeries joins words as a sentence offers them: "a", "a or b", "a, b, or c".
func orSeries(words []string) string {
	switch len(words) {
	case 0:
		return ""
	case 1:
		return words[0]
	case 2:
		return words[0] + " or " + words[1]
	default:
		return strings.Join(words[:len(words)-1], ", ") + ", or " + words[len(words)-1]
	}
}
