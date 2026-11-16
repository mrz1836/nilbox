package eval_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/facts"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

// BenchmarkEvaluateACycle decides every template routine, repeated to 20, on 500 items,
// as one engine cycle does; the target is under 20 ms a cycle.
func BenchmarkEvaluateACycle(b *testing.B) {
	entries, err := os.ReadDir(filepath.Join("..", "spec", "testdata", "templates"))
	require.NoError(b, err)

	var routines []spec.Spec

	for len(routines) < 20 {
		for _, e := range entries {
			data, err := os.ReadFile(filepath.Join("..", "spec", "testdata", "templates", e.Name()))
			require.NoError(b, err)

			s, err := spec.Load(data)
			require.NoError(b, err, strings.TrimSuffix(e.Name(), ".json"))

			routines = append(routines, s)
		}
	}

	items := make([]facts.Facts, 500)
	for i := range items {
		row := prRow()
		row.ID, row.Number = int64(i+1), i+1
		items[i] = build(row)
	}

	for b.Loop() {
		for _, s := range routines[:20] {
			for _, f := range items {
				eval.Evaluate(s, f, eval.History{}, now)
			}
		}
	}
}
