package draft

import (
	"context"
	"fmt"
	"slices"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/autopilotprefs"
)

// TriageSteps are the steps that change only how an item shows in the inbox.
func TriageSteps() []catalog.StepKind {
	return []catalog.StepKind{catalog.StepPin, catalog.StepSnooze, catalog.StepDismiss, catalog.StepRestore}
}

// warnings returns what may make sp, in mode, do less than meant or more than is wise,
// without asking GitHub: triage steps over a watch that is only searches, an agent in the
// terminal or with the profile's own permissions on Autopilot, and a background agent's
// fallbacks and budget its agent can't keep. A draft held in Dry run (dryRunOnly) runs
// nothing on its own, so the mode's warnings wait until it can.
func warnings(sp spec.Spec, mode autopilotprefs.Mode, dryRunOnly bool) []spec.Problem {
	out := append(triageWarnings(sp), fenceWarnings(sp)...)

	if mode != autopilotprefs.ModeAuto || dryRunOnly {
		return out
	}

	return append(out, autopilotWarnings(sp)...)
}

// triageWarnings warns of each triage step of a routine that watches only searches: it
// changes nothing the user sees of an item no inbox list holds.
func triageWarnings(sp spec.Spec) []spec.Problem {
	out := []spec.Problem{}

	if !OnlySearches(sp.Watch) {
		return out
	}

	for i, st := range sp.Steps {
		if slices.Contains(TriageSteps(), st.Kind()) {
			info, _ := catalog.StepByKind(st.Kind())
			out = append(out, spec.Problem{
				Part: spec.PartSteps, Field: fmt.Sprintf("[%d]", i),
				Message: info.Label + " changes how an item shows in your inbox, and this routine watches only " +
					"searches: items that aren't in your inbox won't look any different.",
			})
		}
	}

	return out
}

// autopilotWarnings warns, of a routine on Autopilot, of an agent step that runs in the
// terminal, or with the profile's own permissions, where nobody answers.
func autopilotWarnings(sp spec.Spec) []spec.Problem {
	var out []spec.Problem

	for i, st := range sp.Steps {
		switch {
		case st.Agent == nil:
		case st.Agent.Place() == catalog.WhereTerminal:
			out = append(out, spec.Problem{
				Part: spec.PartSteps, Field: fmt.Sprintf("[%d].agent.where", i),
				Message: "On Autopilot, a terminal run opens a terminal tab while you work: Ask first suits it better.",
			})
		case st.Agent.Tools() == catalog.PresetProfile:
			out = append(out, spec.Problem{
				Part: spec.PartSteps, Field: fmt.Sprintf("[%d].agent.fence.tools", i),
				Message: "On Autopilot with the profile's own permissions, the agent may do whatever a click's launch " +
					"may, with nobody there to answer.",
			})
		}
	}

	return out
}

// PastRulesOff is the warning of a Merge it that allows going past the rules while the
// user's switch for it is off.
const PastRulesOff = "Let routines merge your own pull requests past the rules is off in Autopilot's settings: " +
	"until you turn it on, this step merges only what GitHub allows."

// pastRulesWarnings warns of each Merge it that allows going past the rules while s says the
// user's switch for it is off, or can't tell: until it is on, the step merges only what
// GitHub allows. The switch is read only for a spec with such a step.
func pastRulesWarnings(ctx context.Context, sp spec.Spec, s Setup) []spec.Problem {
	out := []spec.Problem{}

	if !sp.PastRules() || s.PastRules(ctx) {
		return out
	}

	for i, st := range sp.Steps {
		if st.Merge != nil && (st.Merge.AllowBypass || st.Merge.AllowSensitive) {
			out = append(out, spec.Problem{
				Part: spec.PartSteps, Field: fmt.Sprintf("[%d].merge.allowBypass", i), Message: PastRulesOff,
			})
		}
	}

	return out
}

// OnlySearches reports a watch whose sources are all searches.
func OnlySearches(w spec.Watch) bool {
	if len(w.Sources) == 0 {
		return false
	}

	for _, src := range w.Sources {
		if src.Search == nil {
			return false
		}
	}

	return true
}
