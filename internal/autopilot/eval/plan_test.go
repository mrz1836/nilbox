package eval_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

// TestPlanSharedAtOnce decides each of the benchmark's routines, and the lists of
// conditions the golden decides, from one plan in several evaluations at once: a plan
// never changes once prepared, so each decides as Evaluate does, and the race detector
// finds nothing.
func TestPlanSharedAtOnce(t *testing.T) {
	t.Parallel()

	const workers = 4

	for _, r := range append(routinesSection(t).routines, listsSection(t).routines...) {
		plan := eval.Prepare(r.spec)
		got := make([][]eval.Decision, workers)

		var wg sync.WaitGroup

		for w := range workers {
			wg.Go(func() {
				got[w] = decideEvery(func(f namedFacts, h namedHistory) eval.Decision {
					return plan.Evaluate(f.facts, h.history, now)
				})
			})
		}

		wg.Wait()

		want := decideEvery(func(f namedFacts, h namedHistory) eval.Decision {
			return eval.Evaluate(r.spec, f.facts, h.history, now)
		})

		for w := range workers {
			assert.Equal(t, want, got[w], "%s, evaluation %d", r.name, w)
		}
	}
}

// decideEvery returns decide's decision on every golden item with every golden history,
// in order.
func decideEvery(decide func(namedFacts, namedHistory) eval.Decision) []eval.Decision {
	items, histories := moreItems(), goldenHistories()
	out := make([]eval.Decision, 0, len(items)*len(histories))

	for _, f := range items {
		for _, h := range histories {
			out = append(out, decide(f, h))
		}
	}

	return out
}

// TestConditionsAreTheDecisionWhenTheTriggerHolds decides each golden routine's conditions
// alone on every golden item with every golden history: wherever the trigger holds, or
// can't be decided yet, Evaluate's decision is the trigger's and the conditions' together.
func TestConditionsAreTheDecisionWhenTheTriggerHolds(t *testing.T) {
	t.Parallel()

	for _, r := range append(routinesSection(t).routines, listsSection(t).routines...) {
		plan := eval.Prepare(r.spec)

		for _, f := range moreItems() {
			for _, h := range goldenHistories() {
				d := plan.Evaluate(f.facts, h.history, now)
				trigger := d.Trace[0].Result

				if trigger == eval.No {
					continue
				}

				assert.Equal(t, d.Match, eval.All(trigger, plan.Conditions(f.facts, h.history, now)),
					"%s on %s with %s", r.name, f.name, h.name)
			}
		}
	}
}

// TestConditionsDecideWhileTheTriggerDoesNot decides a routine whose trigger doesn't hold
// yet: its conditions still say which items it counts.
func TestConditionsDecideWhileTheTriggerDoesNot(t *testing.T) {
	t.Parallel()

	plan := eval.Prepare(routine(spec.Trigger{Event: catalog.EventChecksPassed}, spec.Conditions{Of: []spec.Clause{
		{Field: catalog.FieldTitle, Op: catalog.OpStartsWith, Value: "build(deps)"},
	}}))

	fits := build(prRow())
	other := prRow()
	other.Title = "Update the docs"

	assert.Equal(t, eval.No, plan.Evaluate(fits, eval.History{}, now).Match, "its checks failed, so it doesn't fire")
	assert.Equal(t, eval.Yes, plan.Conditions(fits, eval.History{}, now), "its title fits")
	assert.Equal(t, eval.No, plan.Conditions(build(other), eval.History{}, now), "its title doesn't")
}
