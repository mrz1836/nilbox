package prompts

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
)

// TestVariablesMatchData keeps the cheat sheet and Data in step: every field has exactly
// one variable of the matching type, in the same order.
func TestVariablesMatchData(t *testing.T) {
	typ := reflect.TypeFor[Data]()
	vars := Variables()
	require.Len(t, vars, typ.NumField())

	wantType := map[reflect.Kind]string{reflect.String: typeText, reflect.Int: typeNumber, reflect.Bool: typeYesNo}
	groups := map[string]bool{groupItem: true, groupRepo: true, groupPR: true, groupSession: true}

	for i, v := range vars {
		field := typ.Field(i)
		assert.Equal(t, field.Name, v.Name, "variable %d", i)
		assert.Equal(t, wantType[field.Type.Kind()], v.Type, v.Name)
		assert.True(t, groups[v.Group], v.Name)
		assert.NotEmpty(t, v.Description, v.Name)
	}
}

func TestSampleData(t *testing.T) {
	pr := Sample(item.KindPR)
	assert.True(t, pr.IsPR)
	assert.Equal(t, "pull request", pr.Kind)
	assert.Equal(t, "widget-cache", pr.Branch)
	assert.False(t, pr.InWorktree)

	issue := Sample(item.KindIssue)
	assert.False(t, issue.IsPR)
	assert.Equal(t, "issue", issue.Kind)
	assert.Empty(t, issue.Branch, "an issue has no pull request facts")
	assert.Zero(t, issue.Additions)
}

func TestWithSession(t *testing.T) {
	pr := Sample(item.KindPR)

	got := pr.WithSession(true, true)
	assert.True(t, got.InWorktree)
	assert.True(t, got.WorktreeOnPRHead)
	assert.Equal(t, "pr-7", got.Worktree)

	got = pr.WithSession(false, true)
	assert.False(t, got.WorktreeOnPRHead, "no worktree, so nothing is at the head")
	assert.Empty(t, got.Worktree)

	got = Sample(item.KindIssue).WithSession(true, true)
	assert.False(t, got.WorktreeOnPRHead, "an issue has no pull request head")
	assert.Equal(t, "issue-12", got.Worktree)
}

func TestValues(t *testing.T) {
	d := Sample(item.KindPR).WithSession(true, false)
	d.Title = "Fix\nthe widget"

	got := d.Values()
	assert.Len(t, got, len(Variables()))
	assert.Equal(t, "Fix the widget", got["Title"], "values are sanitized as a launch would")
	assert.Equal(t, "7", got["Number"])
	assert.Equal(t, "true", got["IsPR"])
	assert.Equal(t, "false", got["WorktreeOnPRHead"])
	assert.Equal(t, "pr-7", got["Worktree"])
}

func TestAppliesTo(t *testing.T) {
	assert.True(t, AppliesTo(AppliesToAny, item.KindPR))
	assert.True(t, AppliesTo(AppliesToAny, item.KindIssue))
	assert.True(t, AppliesTo(AppliesToPR, item.KindPR))
	assert.False(t, AppliesTo(AppliesToPR, item.KindIssue))
	assert.False(t, AppliesTo(AppliesToIssue, item.KindPR))
}
