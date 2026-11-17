package autopilot

import (
	"fmt"

	"github.com/mrz1836/nilbox/internal/agent"
	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
	"github.com/mrz1836/nilbox/internal/fence"
)

// What an agent step's own agent can do in the background, as its spec declares it
// (agent.Headless and agent.KeepsPreset): a routine never runs with a weaker fence than it
// shows, so a preset its agent can't keep is a problem, and the builder says why. An agent
// a row per org or a fallback names that can't keep it is passed over at the start, which
// the builder warns of; and a budget its agent can't keep is said to be so.

// fenceProblems returns a problem for each background agent step whose own agent, named
// rather than inherited, can't keep its tools preset. One that inherits its agent is
// checked as it starts, for the item's own repository.
func fenceProblems(sp spec.Spec) []spec.Problem {
	out := []spec.Problem{}

	for i, st := range sp.Steps {
		a := st.Agent
		if a == nil || a.Place() != catalog.WhereBackground || a.AgentID() == catalog.Inherit {
			continue
		}

		if kept, why := agent.KeepsPreset(agent.ID(a.AgentID()), fence.Preset(a.Tools())); !kept {
			out = append(out, spec.Problem{Part: spec.PartSteps, Field: fmt.Sprintf("[%d].agent.fence.tools", i), Message: why})
		}
	}

	return out
}

// fenceWarnings warns, of each background agent step, of a row per org or a fallback whose
// agent can't keep its tools preset, which is passed over as the run starts, and of a
// budget its own agent can't keep: dollars for one that keeps no budget of its own, tokens
// for one that reports none.
func fenceWarnings(sp spec.Spec) []spec.Problem {
	out := []spec.Problem{}

	for i, st := range sp.Steps {
		a := st.Agent
		if a == nil || a.Place() != catalog.WhereBackground {
			continue
		}

		out = append(out, chainWarnings(*a, i)...)

		if w, ok := budgetWarning(*a, i); ok {
			out = append(out, w)
		}
	}

	return out
}

// chainWarnings warns of the agents a step's rows and fallbacks name that can't keep its
// preset.
func chainWarnings(a spec.AgentStep, i int) []spec.Problem {
	var out []spec.Problem

	preset := fence.Preset(a.Tools())
	seen := map[string]bool{}

	warn := func(id, field, what string) {
		if id == "" || id == catalog.Inherit || seen[id+field] {
			return
		}

		seen[id+field] = true

		if kept, why := agent.KeepsPreset(agent.ID(id), preset); !kept {
			out = append(out, spec.Problem{
				Part: spec.PartSteps, Field: fmt.Sprintf("[%d].agent.%s", i, field),
				Message: why + " " + what + " is passed over.",
			})
		}
	}

	for _, row := range a.LaunchFor {
		warn(row.Agent, "launchFor", "A row that names it")
	}

	for _, fb := range a.Fallbacks {
		warn(fb.Agent, "fallbacks", "A fallback to it")
	}

	return out
}

// budgetWarning warns of a budget a step's own agent, named rather than inherited, can't
// keep.
func budgetWarning(a spec.AgentStep, i int) (spec.Problem, bool) {
	id := agent.ID(a.AgentID())
	if a.AgentID() == catalog.Inherit {
		return spec.Problem{}, false
	}

	info, err := agent.Lookup(id)
	if err != nil {
		return spec.Problem{}, false
	}

	inTokens := a.Fence != nil && a.Fence.Budget != nil && a.Fence.Budget.Tokens != nil

	words := budgetWords(info.Name, info.Background.Headless, inTokens)
	if words == "" {
		return spec.Problem{}, false
	}

	return spec.Problem{Part: spec.PartSteps, Field: fmt.Sprintf("[%d].agent.fence.budget", i), Message: words}, true
}

// budgetWords says why an agent named name, which can do h in the background, can't keep a
// budget in tokens or in dollars, or is "".
func budgetWords(name string, h agent.Headless, inTokens bool) string {
	switch {
	case !h.Background:
		return ""
	case !inTokens && !h.Budget:
		return "Budget: not available for " + name + " in dollars, which it keeps no budget of; the time limit still " +
			"applies, and its runs stop when the app quits."
	case inTokens && !h.Usage:
		return "Budget: " + name + " reports no tokens, so a budget in tokens can't stop it; the time limit still applies."
	case inTokens && !h.UsageLive:
		return "Budget: " + name + " reports its tokens only as a run ends, so a budget in tokens is checked then; the " +
			"time limit bounds the run."
	default:
		return ""
	}
}
