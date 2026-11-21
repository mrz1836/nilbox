package eval_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/autopilot/eval"
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
