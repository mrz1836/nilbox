package eval_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

// patternRoutines are routines whose conditions match a regex and path patterns on every
// pull request they see, so a cycle that holds them shows what matching costs.
func patternRoutines() []spec.Spec {
	return []spec.Spec{
		routine(spec.Trigger{Event: catalog.EventAppears}, spec.Conditions{Of: []spec.Clause{{
			Field: catalog.FieldTitle, Op: catalog.OpMatchesRegex,
			Value: `^build\(deps(-dev)?\): bump (\S+) from v?[0-9.]+ to v?[0-9.]+( in \S+)?$`,
		}}}),
		routine(spec.Trigger{Event: catalog.EventAppears}, spec.Conditions{Of: []spec.Clause{{
			Field: catalog.FieldPaths, Op: catalog.OpAllMatch,
			Values: []string{"go.mod", "go.sum", "**/package.json", "**/pnpm-lock.yaml", "vendor/"},
		}}}),
	}
}

// templates loads every template fixture of the spec package.
func templates(b *testing.B) []spec.Spec {
	b.Helper()

	entries, err := os.ReadDir(filepath.Join("..", "spec", "testdata", "templates"))
	require.NoError(b, err)

	out := make([]spec.Spec, 0, len(entries))

	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "templates", e.Name()))
		require.NoError(b, err)

		s, err := spec.Load(data)
		require.NoError(b, err, strings.TrimSuffix(e.Name(), ".json"))

		out = append(out, s)
	}

	return out
}

// BenchmarkEvaluateACycle decides 20 routines on 500 pull requests, as one engine cycle
// does: the two pattern routines, then every template routine, repeated as needed, each
// prepared once and decided on every item. The target is under 20 ms a cycle.
func BenchmarkEvaluateACycle(b *testing.B) {
	routines := patternRoutines()
	for _, s := range routines {
		require.NoError(b, spec.Validate(spec.Normalize(s)))
	}

	for len(routines) < 20 {
		routines = append(routines, templates(b)...)
	}

	items := make([]facts.Facts, 500)
	for i := range items {
		row := prRow()
		row.ID, row.Number = int64(i+1), i+1
		items[i] = build(row)
	}

	for b.Loop() {
		for _, s := range routines[:20] {
			plan := eval.Prepare(s)

			for _, f := range items {
				plan.Evaluate(f, eval.History{}, now)
			}
		}
	}
}
