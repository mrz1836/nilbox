package heldruns_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/heldruns"
)

// preview is a fork's own workflow, whose held runs the user can approve.
const preview = "Deploy Preview"

func TestIgnored(t *testing.T) {
	t.Parallel()

	trigger := heldruns.Run{Workflow: "Auto-merge Trigger", OnGitHub: true}
	held := heldruns.Run{Workflow: preview}
	named := []string{preview}

	for _, c := range []struct {
		name       string
		run        heldruns.Run
		mergeState string
		ignored    []string
		want       bool
	}{
		{"a run held on GitHub never counts", trigger, "UNSTABLE", nil, true},
		{"nor while GitHub works the merge out", trigger, "", nil, true},
		{"until GitHub reports the merge blocked", trigger, "BLOCKED", nil, false},
		{"a run the user can approve counts", held, "UNSTABLE", nil, false},
		{"unless its workflow is named", held, "UNSTABLE", named, true},
		{"even with the merge blocked", held, "BLOCKED", named, true},
		{"a named run held on GitHub never counts", trigger, "BLOCKED", []string{"Auto-merge Trigger"}, true},
		{"names match case and all", held, "UNSTABLE", []string{"deploy preview", "Deploy"}, false},
		{"a run of no name matches no name", heldruns.Run{}, "CLEAN", []string{""}, false},
	} {
		assert.Equal(t, c.want, heldruns.Ignored(c.run, c.mergeState, c.ignored), c.name)
	}
}

func TestCountComposesTheKindsWithTheList(t *testing.T) {
	t.Parallel()

	runs := []heldruns.Run{
		{Workflow: "Auto-merge Trigger", OnGitHub: true},
		{Workflow: preview},
		{Workflow: "Lint"},
	}

	approvable, onGitHub := heldruns.Count(runs, "UNSTABLE", nil)
	assert.Equal(t, 2, approvable, "the runs the user can approve count")
	assert.Zero(t, onGitHub, "a run held on GitHub doesn't")

	approvable, onGitHub = heldruns.Count(runs, "UNSTABLE", []string{preview})
	assert.Equal(t, 1, approvable, "an approvable run of a named workflow doesn't count")
	assert.Zero(t, onGitHub)

	approvable, onGitHub = heldruns.Count(runs, "BLOCKED", []string{preview})
	assert.Equal(t, 1, approvable)
	assert.Equal(t, 1, onGitHub, "a blocked merge counts the run held on GitHub as its likely blocker")

	approvable, onGitHub = heldruns.Count(runs, "BLOCKED", []string{preview, "Auto-merge Trigger", "Lint"})
	assert.Zero(t, approvable, "the user's names win over the blocked merge")
	assert.Zero(t, onGitHub)

	approvable, onGitHub = heldruns.Count(nil, "BLOCKED", nil)
	assert.Zero(t, approvable)
	assert.Zero(t, onGitHub)
}

func TestUnnamedFillsTheRunsDetailsStoredBeforeTheNames(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []heldruns.Run{}, heldruns.Unnamed(nil, 0, 0))
	assert.Equal(t, []heldruns.Run{{}, {}, {OnGitHub: true}}, heldruns.Unnamed(nil, 2, 1))

	named := []heldruns.Run{{Workflow: "Lint"}, {Workflow: "Auto-merge Trigger", OnGitHub: true}}
	assert.Equal(t, named, heldruns.Unnamed(named, 1, 1), "every run counted is listed")
	assert.Equal(t, slices.Concat(named, []heldruns.Run{{}}), heldruns.Unnamed(named, 2, 0),
		"a run counted but not listed is added, and a run listed but not counted kept")

	// An unnamed run held on GitHub never counts until the merge is blocked; an unnamed
	// approvable one always counts.
	approvable, onGitHub := heldruns.Count(heldruns.Unnamed(nil, 1, 1), "UNSTABLE", []string{"Lint"})
	assert.Equal(t, 1, approvable)
	assert.Zero(t, onGitHub)
}

func TestClean(t *testing.T) {
	t.Parallel()

	got, err := heldruns.Clean([]string{" Auto-merge Trigger ", "", preview, "Auto-merge Trigger", "deploy preview"})
	require.NoError(t, err)
	assert.Equal(t, []string{"Auto-merge Trigger", preview, "deploy preview"}, got,
		"trimmed, blanks and exact repeats left out, case kept")

	got, err = heldruns.Clean(nil)
	require.NoError(t, err)
	assert.Equal(t, []string{}, got)

	many := make([]string, heldruns.MaxIgnored+1)
	for i := range many {
		many[i] = strings.Repeat("w", i+1)
	}

	_, err = heldruns.Clean(many)
	require.ErrorIs(t, err, heldruns.ErrTooMany)

	_, err = heldruns.Clean([]string{strings.Repeat("x", heldruns.MaxNameLength+1)})
	require.ErrorIs(t, err, heldruns.ErrNameTooLong)

	_, err = heldruns.Clean([]string{"CI\nDeploy"})
	require.ErrorIs(t, err, heldruns.ErrInvalidName)

	got, err = heldruns.Clean([]string{strings.Repeat("é", heldruns.MaxNameLength)})
	require.NoError(t, err, "the bound counts characters, not bytes")
	assert.Len(t, got, 1)
}

func TestDecodeAndEncode(t *testing.T) {
	t.Parallel()

	for _, stored := range []string{"", "null", "{}", "not json", `["CI\nDeploy"]`} {
		assert.Equal(t, []string{}, heldruns.Decode(stored), "%q reads as none", stored)
	}

	assert.Equal(t, []string{"Auto-merge Trigger"}, heldruns.Decode(`[" Auto-merge Trigger", ""]`))

	encoded, err := heldruns.Encode([]string{preview, " Deploy Preview "})
	require.NoError(t, err)
	assert.JSONEq(t, `["Deploy Preview"]`, encoded)
	assert.Equal(t, []string{preview}, heldruns.Decode(encoded))

	encoded, err = heldruns.Encode(nil)
	require.NoError(t, err)
	assert.Equal(t, "[]", encoded)

	_, err = heldruns.Encode([]string{"a\tb"})
	require.ErrorIs(t, err, heldruns.ErrInvalidName)
}
