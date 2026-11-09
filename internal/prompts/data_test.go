package prompts

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/item"
)

// TestVariablesMatchData keeps the cheat sheet and Data in step: every field has exactly
// one variable of the matching type, in the same order.
func TestVariablesMatchData(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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
	t.Parallel()

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

func TestAppliesToCovers(t *testing.T) {
	t.Parallel()

	assert.True(t, AppliesToAny.Covers(item.KindPR))
	assert.True(t, AppliesToAny.Covers(item.KindIssue))
	assert.True(t, AppliesToPR.Covers(item.KindPR))
	assert.False(t, AppliesToPR.Covers(item.KindIssue))
	assert.False(t, AppliesToIssue.Covers(item.KindPR))
	assert.False(t, AppliesTo("").Covers(item.KindPR))
}

func TestWithUpdateFillsInABotsDependencyUpdate(t *testing.T) {
	t.Parallel()

	bot := Sample(item.KindPR)
	bot.IsBot = true

	got := bot.WithUpdate("build(deps): bump golang.org/x/net from 0.30.0 to 0.31.0")
	assert.Equal(t, "golang.org/x/net", got.Dependency)
	assert.Equal(t, "0.30.0", got.FromVersion)
	assert.Equal(t, "0.31.0", got.ToVersion)
	assert.Equal(t, "minor", got.UpdateType)

	group := bot.WithUpdate("Bump the go-deps group with 3 updates")
	assert.Empty(t, group.Dependency, "a group names no one dependency")
	assert.Empty(t, group.UpdateType)

	assert.Equal(t, bot, bot.WithUpdate("Fix the login bug"), "a title that is not an update")

	person := Sample(item.KindPR)
	assert.Equal(t, person, person.WithUpdate("Bump lodash from 1.0.0 to 2.0.0"), "a person's pull request")

	issue := Sample(item.KindIssue)
	issue.IsBot = true
	assert.Equal(t, issue, issue.WithUpdate("Bump lodash from 1.0.0 to 2.0.0"), "an issue")
}

func TestSanitizedCleansTheDependencyFields(t *testing.T) {
	t.Parallel()

	d := Data{Dependency: "a\nb\u202e", FromVersion: strings.Repeat("1", 300), ToVersion: " 2 ", UpdateType: "major\t"}
	clean := d.sanitized()

	assert.Equal(t, "a b", clean.Dependency)
	assert.Equal(t, "2", clean.ToVersion)
	assert.Equal(t, "major", clean.UpdateType)
	assert.LessOrEqual(t, len([]rune(clean.FromVersion)), maxTitleRunes+1)
}
