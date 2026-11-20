package draft

import (
	"fmt"
	"slices"
	"strings"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
	"github.com/mrz1836/nilbox/internal/brand"
)

// Offers names the kinds of step, and the GitHub actions, a build runs: by default the
// catalog's own (Step.Offered, GitHubAction.Offered), which each part's phase turns on
// once its dispatcher is built. A test hands in its own, with fakes for the dispatchers,
// never by changing the catalog. A nil *Offers is the catalog's.
type Offers struct {
	Steps   []catalog.StepKind
	Actions []catalog.GitHubActionKind

	// Places are where this build runs an agent step (catalog.PlaceInfo.Offered). A step
	// set to run anywhere else is a problem the builder shows (placeProblems), so it is
	// never saved; and one a routine holds from before keeps its runs from starting, as
	// this Mac isn't ready for it (the autopilot package's readiness).
	Places []catalog.Where
}

// CatalogOffers returns the catalog's own offers: each kind of step, GitHub action, and
// place it marks offered.
func CatalogOffers() *Offers {
	out := &Offers{}

	for _, info := range catalog.Steps() {
		if info.Offered {
			out.Steps = append(out.Steps, info.Kind)
		}
	}

	for _, a := range catalog.GitHubActions() {
		if a.Offered {
			out.Actions = append(out.Actions, a.Kind)
		}
	}

	for _, p := range catalog.Places() {
		if p.Offered {
			out.Places = append(out.Places, p.ID)
		}
	}

	return out
}

// OrCatalog returns o, or the catalog's offers for nil.
func (o *Offers) OrCatalog() *Offers {
	if o == nil {
		return CatalogOffers()
	}

	return o
}

// RunsAt reports whether this build runs an agent step where place says.
func (o *Offers) RunsAt(place catalog.Where) bool {
	return slices.Contains(o.OrCatalog().Places, place)
}

// PlaceWords names the places this build runs an agent in: "In my terminal".
func (o *Offers) PlaceWords() string {
	var words []string

	for _, p := range catalog.Places() {
		if o.RunsAt(p.ID) {
			words = append(words, p.Label)
		}
	}

	return Series(words)
}

// NotOffered returns the kinds of step s has that this build can't run yet, in the
// catalog's order: a step whose kind isn't offered, and a GitHub step with an action
// that isn't. The list is never nil.
func (o *Offers) NotOffered(s spec.Spec) []catalog.StepKind {
	out := []catalog.StepKind{}

	for _, info := range catalog.Steps() {
		for _, st := range s.Steps {
			if st.Kind() == info.Kind && !o.runs(st) {
				out = append(out, info.Kind)

				break
			}
		}
	}

	return out
}

// ModeFor returns mode, or Dry run for a spec with a step this build can't run yet.
func (o *Offers) ModeFor(mode autopilotprefs.Mode, s spec.Spec) autopilotprefs.Mode {
	if len(o.NotOffered(s)) > 0 {
		return autopilotprefs.ModeDryRun
	}

	return mode
}

// Needs names each part of s this build can't run yet, in plain words, as a template
// shows them: an agent's place, then each kind of step and GitHub action it doesn't offer,
// in the catalog's order. The list is never nil.
func (o *Offers) Needs(s spec.Spec) []string {
	out := []string{}

	for _, words := range append(o.placeNeeds(s), o.stepNeeds(s)...) {
		if !slices.Contains(out, words) {
			out = append(out, words)
		}
	}

	return out
}

// PreferredPlace is where a routine made from an item runs its agent: in the background
// once this build runs agents there, else the first place it does.
func (o *Offers) PreferredPlace() catalog.Where {
	if o.RunsAt(catalog.WhereBackground) {
		return catalog.WhereBackground
	}

	for _, p := range catalog.Places() {
		if o.RunsAt(p.ID) {
			return p.ID
		}
	}

	return catalog.DefaultWhere
}

// placeProblems returns a problem for each agent step of s set to run where this build
// can't run an agent, at its where field: plain words in the builder, which keep the
// routine from being saved as it is.
func (o *Offers) placeProblems(s spec.Spec) []spec.Problem {
	var out []spec.Problem

	for i, st := range s.Steps {
		if st.Agent == nil || o.RunsAt(st.Agent.Place()) {
			continue
		}

		info, _ := catalog.PlaceByID(st.Agent.Place())
		out = append(out, spec.Problem{
			Part: spec.PartSteps, Field: fmt.Sprintf("[%d].agent.where", i),
			Message: fmt.Sprintf("this version of %s can't run an agent %s yet: choose %s", brand.Name,
				strings.ToLower(info.Label), o.PlaceWords()),
		})
	}

	return out
}

// runs reports whether this build runs st.
func (o *Offers) runs(st spec.Step) bool {
	off := o.OrCatalog()

	if !slices.Contains(off.Steps, st.Kind()) {
		return false
	}

	for _, a := range st.GitHub {
		if !slices.Contains(off.Actions, a.Kind()) {
			return false
		}
	}

	return true
}

// placeNeeds names each place s runs an agent that this build doesn't.
func (o *Offers) placeNeeds(s spec.Spec) []string {
	var out []string

	for _, st := range s.Steps {
		if st.Agent != nil && !o.RunsAt(st.Agent.Place()) {
			info, _ := catalog.PlaceByID(st.Agent.Place())
			out = append(out, "agents "+strings.ToLower(info.Label))
		}
	}

	return out
}

// stepNeeds names each kind of step, and each GitHub action, of s this build doesn't run,
// in the catalog's order.
func (o *Offers) stepNeeds(s spec.Spec) []string {
	off := o.OrCatalog()

	var out []string

	for _, info := range catalog.Steps() {
		for _, st := range s.Steps {
			switch {
			case st.Kind() != info.Kind:
			case !slices.Contains(off.Steps, info.Kind):
				out = append(out, "“"+info.Label+"”")
			default:
				out = append(out, off.actionNeeds(st)...)
			}
		}
	}

	return out
}

// actionNeeds names each GitHub action of st this build doesn't send.
func (o *Offers) actionNeeds(st spec.Step) []string {
	var out []string

	for _, a := range st.GitHub {
		if !slices.Contains(o.Actions, a.Kind()) {
			out = append(out, "“"+actionLabel(a.Kind())+"”")
		}
	}

	return out
}

// actionLabel names a GitHub action as the builder does, by the catalog's label when it
// lists the action, else as the spec spells it.
func actionLabel(kind catalog.GitHubActionKind) string {
	for _, a := range catalog.GitHubActions() {
		if a.Kind == kind {
			return a.Label
		}
	}

	return map[catalog.GitHubActionKind]string{
		catalog.GitHubRebase: "Ask Dependabot to rebase", catalog.GitHubUpdateBranch: "Update branch",
		catalog.GitHubReply: "Comment a reply", catalog.GitHubLabels: "Labels",
	}[kind]
}

// NeedsWords says what a template needs (Needs) as a sentence, "" for nothing.
func NeedsWords(needs []string) string {
	if len(needs) == 0 {
		return ""
	}

	return fmt.Sprintf("It needs %s, which this version of %s can't run yet.", Series(needs), brand.Name)
}
