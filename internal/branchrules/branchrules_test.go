package branchrules_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/branchrules"
)

func TestDefaults(t *testing.T) {
	t.Parallel()

	assert.Equal(t, branchrules.Rules{Own: true, PullRequest: true, Merged: true, Protected: []string{"main", "master"}},
		branchrules.Defaults())
}

func TestNormalize(t *testing.T) {
	t.Parallel()

	got, err := branchrules.Normalize([]string{" main ", "", "release/*", "main", "v[0-9]*"})
	require.NoError(t, err)
	assert.Equal(t, []string{"main", "release/*", "v[0-9]*"}, got, "trimmed, without empty ones or repeats")

	empty, err := branchrules.Normalize(nil)
	require.NoError(t, err)
	assert.Empty(t, empty)
	assert.NotNil(t, empty)

	for _, bad := range []string{"has space", "tab\there", "[unclosed", strings.Repeat("x", 201)} {
		_, err = branchrules.Normalize([]string{bad})
		require.ErrorIs(t, err, branchrules.ErrInvalidPattern, bad)
	}

	many := make([]string, 101)
	for i := range many {
		many[i] = "b" + strings.Repeat("x", i)
	}

	_, err = branchrules.Normalize(many)
	require.ErrorIs(t, err, branchrules.ErrTooManyPatterns)
}

func TestMatches(t *testing.T) {
	t.Parallel()

	patterns := []string{"main", "master", "release/*", "v[0-9]*"}

	for branch, want := range map[string]bool{
		"main":              true,
		"master":            true,
		"release/1.2":       true,
		"release/1.2/fix":   false,
		"v1.0":              true,
		"main-fix":          false,
		"feature/main":      false,
		"worktree-pr-7":     false,
		"vnext":             false,
		"releases/1.2":      false,
		"release":           false,
		"maintenance/clean": false,
	} {
		assert.Equal(t, want, branchrules.Matches(branch, patterns), branch)
	}

	assert.False(t, branchrules.Matches("main", nil), "an empty list protects nothing")
	assert.False(t, branchrules.Matches("main", []string{"[bad"}), "a malformed pattern matches nothing")
}

func TestEncodeAndDecode(t *testing.T) {
	t.Parallel()

	rules := branchrules.Rules{Own: false, PullRequest: true, Merged: false, Protected: []string{" develop ", "main"}}

	stored, err := rules.Encode()
	require.NoError(t, err)
	assert.Equal(t, map[string]string{
		branchrules.PrefOwn: "false", branchrules.PrefPullRequest: "true", branchrules.PrefMerged: "false",
		branchrules.PrefProtected: `["develop","main"]`,
	}, stored)

	assert.Equal(t, branchrules.Rules{Own: false, PullRequest: true, Merged: false, Protected: []string{"develop", "main"}},
		branchrules.Decode(stored))

	_, err = branchrules.Rules{Protected: []string{"[bad"}}.Encode()
	require.ErrorIs(t, err, branchrules.ErrInvalidPattern)
	require.ErrorIs(t, branchrules.Rules{Protected: []string{"[bad"}}.Validate(), branchrules.ErrInvalidPattern)
	require.NoError(t, branchrules.Defaults().Validate())
}

func TestDecodeKeepsDefaultsForWhatIsMissingOrUnreadable(t *testing.T) {
	t.Parallel()

	assert.Equal(t, branchrules.Defaults(), branchrules.Decode(nil))
	assert.Equal(t, branchrules.Defaults(), branchrules.Decode(map[string]string{
		branchrules.PrefOwn: "maybe", branchrules.PrefProtected: "not json",
	}))
	assert.Equal(t, branchrules.Defaults(), branchrules.Decode(map[string]string{
		branchrules.PrefProtected: `["[bad"]`,
	}), "a list that no longer validates keeps the defaults")

	got := branchrules.Decode(map[string]string{branchrules.PrefProtected: `[]`})
	assert.Empty(t, got.Protected, "an emptied list stays empty")
	assert.NotNil(t, got.Protected)

	assert.ElementsMatch(t, []string{
		branchrules.PrefOwn, branchrules.PrefPullRequest, branchrules.PrefMerged, branchrules.PrefProtected,
	}, branchrules.Keys())
}
