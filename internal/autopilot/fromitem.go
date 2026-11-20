package autopilot

import (
	"context"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/draft"
	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilot/watch"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/textutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// Make a routine from this…: the builder opens filled in from one stored item, so a
// routine for the items like it is a few clicks away. What it fills in comes from the
// item's own facts, as the inbox stored them, and asks GitHub nothing.

// ItemDraft is a new routine filled in from one item.
type ItemDraft struct {
	// Draft is the routine, unsaved, in the mode new routines start in.
	Draft Draft `json:"draft"`

	// Suggestions are conditions the item suggests, left for the user to tick: its labels,
	// and its dependency update's type.
	Suggestions []spec.Clause `json:"suggestions"`

	// Item is the item, and Match and Trace what the draft decides about it now, from what
	// is stored, which Test shows at the top until a test reads more.
	Item  ItemRef      `json:"item"`
	Match eval.Result  `json:"match"`
	Trace []eval.Trace `json:"trace"`
}

// FromItem returns a new routine filled in from the stored item itemID: its kind; its
// author, or its bot; its repository's owner; when its checks fail, when it conflicts,
// when changes are requested, else when it appears, as the item stands now; the failed
// checks' names as one condition, when its checks fail; and the prompt a click would
// launch on it, run where this build runs agents, the background first. Its labels and its
// dependency update's type come as suggestions, unticked. It saves nothing.
func (s *Service) FromItem(ctx context.Context, itemID int64) (ItemDraft, error) {
	out := ItemDraft{Suggestions: []spec.Clause{}, Trace: []eval.Trace{}}

	if s.items == nil {
		return out, uierr.New("The inbox can't be read, so no routine can be made from it.", ErrNoItems)
	}

	now := s.clock.Now()

	all, err := s.items.Facts(ctx, []int64{itemID}, now.UTC())
	if err != nil {
		return out, uierr.New("The item couldn't be read.", err)
	}

	if len(all) == 0 {
		return out, uierr.New("That item isn't stored on this Mac any more.", ErrNoItems)
	}

	f := all[0]
	viewer := s.viewer(ctx)
	sp := itemSpec(f, viewer, s.offers)

	rows, err := s.store.all(ctx)
	if err != nil {
		return out, err
	}

	out.Draft = Draft{Name: freeName(itemRoutineName(f, viewer), rows), Mode: s.defaultMode(ctx), Spec: sp}
	out.Suggestions = itemSuggestions(f)
	out.Item = ItemRef{
		ID: f.Item.ID, Kind: f.Item.Kind, Repo: f.Item.Repo, Number: f.Item.Number, Title: f.Item.Title,
		URL: f.Item.URL,
	}

	d := eval.Evaluate(sp, f, eval.History{}, now)
	out.Match, out.Trace = d.Match, nonNil(d.Trace)

	return out, nil
}

// itemSpec is the routine an item suggests.
func itemSpec(f facts.Facts, viewer string, offers *Offers) spec.Spec {
	search := watch.Search{Kind: f.Item.Kind, Where: []string{itemPlace(f, viewer)}}

	if a := itemAuthor(f, viewer); a != "" {
		search.Authors = []string{a}
	}

	sp := spec.Spec{
		Version:    spec.Version,
		Watch:      spec.Watch{Sources: []spec.Source{{Search: &search}}},
		Trigger:    spec.Trigger{Event: itemEvent(f)},
		Conditions: spec.Conditions{Match: spec.MatchAll, Of: []spec.Clause{}},
		Steps: []spec.Step{{Agent: &spec.AgentStep{
			Prompt: spec.PromptRef{ItemDefault: true}, Where: offers.PreferredPlace(),
		}}},
	}

	if sp.Trigger.Event == catalog.EventChecksFailed {
		if names := f.Value(catalog.FieldFailedNames); names.Known() && len(names.List) > 0 {
			sp.Conditions.Of = append(sp.Conditions.Of, spec.Clause{
				Field: catalog.FieldFailedNames, Op: catalog.OpAnyContains, Values: slices.Clone(names.List),
			})
		}
	}

	return spec.Normalize(sp)
}

// itemPlace is where the item's search looks: your account for an item of yours, else its
// repository's owner, as an org.
func itemPlace(f facts.Facts, viewer string) string {
	owner := f.Value(catalog.FieldRepoOwner).Text
	if owner == "" {
		owner, _, _ = strings.Cut(f.Item.Repo, "/")
	}

	if viewer != "" && strings.EqualFold(owner, viewer) {
		return "@me"
	}

	return "org:" + owner
}

// itemAuthor is who the item's search finds as its author: you, a bot as an app, or the
// person's login; "" when it isn't known.
func itemAuthor(f facts.Facts, viewer string) string {
	login := f.Value(catalog.FieldAuthor).Text

	switch {
	case login == "":
		return ""
	case viewer != "" && strings.EqualFold(login, viewer):
		return "@me"
	case f.Value(catalog.FieldAuthorIsBot).Bool:
		return "app/" + strings.TrimSuffix(strings.ToLower(login), "[bot]")
	default:
		return login
	}
}

// itemEvent is what the item suggests a routine fires on, as it stands now: its checks
// failing, its merge conflicts, changes requested, else its appearing.
func itemEvent(f facts.Facts) catalog.Event {
	if f.Item.Kind != item.KindPR {
		return catalog.EventAppears
	}

	switch state := f.Value(catalog.FieldChecksState); {
	case f.Value(catalog.FieldChecksFailed).Number > 0, state.Text == "failure", state.Text == "error":
		return catalog.EventChecksFailed
	case f.Value(catalog.FieldConflicts).Bool:
		return catalog.EventMergeConflicts
	case f.Value(catalog.FieldReviewDecision).Text == "changesRequested":
		return catalog.EventChangesRequested
	}

	return catalog.EventAppears
}

// itemSuggestions are the conditions an item suggests beyond what is filled in: its labels,
// and its dependency update's type.
func itemSuggestions(f facts.Facts) []spec.Clause {
	out := []spec.Clause{}

	if labels := f.Value(catalog.FieldLabels); labels.Known() && len(labels.List) > 0 {
		out = append(out, spec.Clause{Field: catalog.FieldLabels, Op: catalog.OpHasAny, Values: slices.Clone(labels.List)})
	}

	if t := f.Value(catalog.FieldUpdateType); t.Known() && t.Text != "" {
		out = append(out, spec.Clause{Field: catalog.FieldUpdateType, Op: catalog.OpOneOf, Values: []string{t.Text}})
	}

	return out
}

// itemRoutineName names a routine made from an item: "Dependabot's pull requests in
// acme-org", "Your issues", "octo-user's pull requests in acme-org".
func itemRoutineName(f facts.Facts, viewer string) string {
	kinds := f.Item.Kind.Plural()
	place := itemPlace(f, viewer)

	who := ""

	switch a := itemAuthor(f, viewer); {
	case a == "@me":
		who = "Your "
	case strings.HasPrefix(a, "app/"):
		who = botName(strings.TrimPrefix(a, "app/")) + "’s "
	case a != "":
		who = a + "’s "
	}

	name := who + kinds
	if who == "" {
		name = textutil.Capitalize(kinds)
	}

	if place != "@me" {
		name += " in " + strings.TrimPrefix(place, "org:")
	} else if who != "Your " {
		name += " in your account"
	}

	return draft.FitName(textutil.OneLine(name), "")
}

// botName names a bot as the sentence does: Dependabot, Renovate, GitHub Actions, or its
// login.
func botName(login string) string {
	switch login {
	case "dependabot":
		return "Dependabot"
	case "renovate":
		return "Renovate"
	case "github-actions":
		return "GitHub Actions"
	}

	return login
}
