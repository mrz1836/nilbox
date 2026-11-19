package fence_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/sensitive"
)

// group is a category's group holding paths.
func group(category string, paths ...string) sensitive.Group {
	g := sensitive.Group{Category: category, Files: []sensitive.Finding{}}
	for _, p := range paths {
		g.Files = append(g.Files, sensitive.Finding{Path: p})
	}

	return g
}

// groupPaths returns each group's category and its files' paths, in order.
func groupPaths(c sensitive.Check) map[string][]string {
	out := map[string][]string{}

	for _, g := range c.Groups {
		for _, f := range g.Files {
			out[g.Category] = append(out[g.Category], f.Path)
		}
	}

	return out
}

// categories returns the check's groups' categories, in order.
func categories(c sensitive.Check) []string {
	out := make([]string, 0, len(c.Groups))
	for _, g := range c.Groups {
		out = append(out, g.Category)
	}

	return out
}

func TestMergeChangedAddsARunsFilesToTheItemsCheck(t *testing.T) {
	t.Parallel()

	cfg := sensitive.DefaultConfig()

	tests := []struct {
		name string
		// check is the item's own file check.
		check sensitive.Check
		// changed are the files the run's commits changed.
		changed    []string
		wantStatus sensitive.Status
		wantOrder  []string
		wantFiles  map[string][]string
	}{
		{
			name:       "a clear item whose run changed a workflow",
			check:      sensitive.Check{Status: sensitive.StatusClear, Head: "abc", Complete: true, Groups: []sensitive.Group{}},
			changed:    []string{"main.go", ".github/workflows/ci.yml"},
			wantStatus: sensitive.StatusFlagged,
			wantOrder:  []string{sensitive.CategoryWorkflows},
			wantFiles:  map[string][]string{sensitive.CategoryWorkflows: {".github/workflows/ci.yml"}},
		},
		{
			name: "a run's agent file goes ahead of the item's own groups",
			check: sensitive.Check{
				Status: sensitive.StatusFlagged, Complete: true,
				Groups: []sensitive.Group{group(sensitive.CategoryDependencies, "go.sum"), group(sensitive.CategoryCustom, "secrets/x")},
			},
			changed:    []string{"CLAUDE.md"},
			wantStatus: sensitive.StatusFlagged,
			wantOrder:  []string{sensitive.CategoryAgent, sensitive.CategoryDependencies, sensitive.CategoryCustom},
			wantFiles: map[string][]string{
				sensitive.CategoryAgent: {"CLAUDE.md"}, sensitive.CategoryDependencies: {"go.sum"},
				sensitive.CategoryCustom: {"secrets/x"},
			},
		},
		{
			name: "a file the item already changed is listed once",
			check: sensitive.Check{
				Status: sensitive.StatusFlagged, Complete: true,
				Groups: []sensitive.Group{group(sensitive.CategoryTooling, "Makefile")},
			},
			changed:    []string{"Makefile", "scripts/build.sh"},
			wantStatus: sensitive.StatusFlagged,
			wantOrder:  []string{sensitive.CategoryTooling},
			wantFiles:  map[string][]string{sensitive.CategoryTooling: {"Makefile", "scripts/build.sh"}},
		},
		{
			name: "a run that changed only what the item did adds nothing",
			check: sensitive.Check{
				Status: sensitive.StatusPartial, Complete: false,
				Groups: []sensitive.Group{group(sensitive.CategoryTooling, "Makefile")},
			},
			changed:    []string{"Makefile"},
			wantStatus: sensitive.StatusPartial,
			wantOrder:  []string{sensitive.CategoryTooling},
			wantFiles:  map[string][]string{sensitive.CategoryTooling: {"Makefile"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			before := groupPaths(tt.check)
			check := tt.check

			got := fence.MergeChanged(&check, fence.CheckChanged("def", tt.changed, cfg))
			assert.Equal(t, tt.wantStatus, got.Status)
			assert.Equal(t, tt.wantOrder, categories(got), "groups in the order files are matched, the user's own last")
			assert.Equal(t, tt.wantFiles, groupPaths(got))
			assert.Equal(t, tt.check.Head, got.Head, "the item's own head")
			assert.Equal(t, tt.check.Complete, got.Complete, "a complete run's list leaves the check as complete as it was")
			assert.Equal(t, before, groupPaths(tt.check), "the item's own check is left as it was")
		})
	}
}

func TestMergeChangedWithoutAnItemCheckIsTheRunsAlone(t *testing.T) {
	t.Parallel()

	changed := fence.CheckChanged("def", []string{".claude/settings.json"}, sensitive.DefaultConfig())

	assert.Equal(t, changed, fence.MergeChanged(nil, changed), "a trusted item's run stands alone")
}

func TestMergeChangedIsIncompleteWhenTheRunsListIs(t *testing.T) {
	t.Parallel()

	many := make([]string, fence.MaxChanged+1)
	for i := range many {
		many[i] = "f.go"
	}

	changed := fence.CheckChanged("def", many, sensitive.DefaultConfig())
	require.False(t, changed.Complete)

	check := sensitive.Check{Status: sensitive.StatusClear, Complete: true, Groups: []sensitive.Group{}}

	got := fence.MergeChanged(&check, changed)
	assert.False(t, got.Complete, "a run's cut-off list makes the whole check incomplete")
	assert.Equal(t, sensitive.StatusClear, got.Status, "with nothing sensitive in it")
}
