package eval_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/mrz1836/nilbox/internal/autopilot/catalog"
	"github.com/mrz1836/nilbox/internal/autopilot/eval"
	"github.com/mrz1836/nilbox/internal/autopilot/spec"
)

// TestPatternsCompileOnce checks that a regex and a path pattern are compiled once, not
// on every evaluation of every item, by what deciding the condition again allocates: its
// words, its reason, and the matching, never a compilation, which costs the regex below
// about 160 allocations and each path pattern a few.
//
//nolint:paralleltest // testing.AllocsPerRun sets GOMAXPROCS, process-wide state
func TestPatternsCompileOnce(t *testing.T) {
	f := build(prRow())

	for _, tt := range []struct {
		c   spec.Clause
		max float64
	}{
		{
			c: spec.Clause{
				Field: catalog.FieldTitle, Op: catalog.OpMatchesRegex,
				Value: `^build\(deps\): bump (\S+) from v?[0-9.]+ to v?[0-9.]+$`,
			},
			max: 20,
		},
		{
			c: spec.Clause{Field: catalog.FieldPaths, Op: catalog.OpAnyMatches, Values: []string{
				"deploy/**/*.tf", ".github/workflows/", "ci/**/build.yml", "docs/**/*.md", "tools/*/main.go", "go.*",
			}},
			max: 30,
		},
	} {
		decide(tt.c, f, eval.History{})

		allocs := testing.AllocsPerRun(50, func() { decide(tt.c, f, eval.History{}) })
		assert.LessOrEqual(t, allocs, tt.max, "%s %s allocates %v", tt.c.Field, tt.c.Op, allocs)
	}
}
