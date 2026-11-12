package reporules_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/reporules"
)

// acmeOrg is the org most cases name.
const acmeOrg = "acme-org"

func TestParseTarget(t *testing.T) {
	t.Parallel()

	for in, want := range map[string]repo.Name{
		acmeOrg:                    {Owner: acmeOrg},
		" Acme-Org/ ":              {Owner: "Acme-Org"},
		"acme-org/example-repo":    {Owner: acmeOrg, Repo: "example-repo"},
		"acme-org/example.repo/":   {Owner: acmeOrg, Repo: "example.repo"},
		"\tAcme-Org/Example-Repo ": {Owner: "Acme-Org", Repo: "Example-Repo"},
	} {
		got, err := reporules.ParseTarget(in)
		require.NoError(t, err, in)
		assert.Equal(t, want, got, in)
	}

	for _, bad := range []string{"", " ", "/", "-acme", "acme/..", "acme/b/c", "acme org", "/example-repo", "acme//x"} {
		_, err := reporules.ParseTarget(bad)
		require.ErrorIs(t, err, reporules.ErrInvalidTarget, bad)
	}
}

func TestSetKeepsATargetInOneListAtMost(t *testing.T) {
	t.Parallel()

	r, err := reporules.Rules{}.Set(acmeOrg, reporules.RuleMuted)
	require.NoError(t, err)
	assert.Equal(t, reporules.Rules{Pinned: []string{}, Muted: []string{acmeOrg}}, r)

	r, err = r.Set("Acme-Org", reporules.RulePinned)
	require.NoError(t, err)
	assert.Equal(t, reporules.Rules{Pinned: []string{"Acme-Org"}, Muted: []string{}}, r,
		"the target moves to the other list, matched ignoring case, in the casing given")

	r, err = r.Set("acme-org/example-repo", reporules.RuleMuted)
	require.NoError(t, err)

	r, err = r.Set("ACME-ORG/EXAMPLE-REPO", reporules.RuleMuted)
	require.NoError(t, err)
	assert.Equal(t, []string{"ACME-ORG/EXAMPLE-REPO"}, r.Muted, "a repeat replaces the target, never adds it twice")

	r, err = r.Set("acme-org/example-repo", reporules.RuleNone)
	require.NoError(t, err)
	assert.Equal(t, reporules.Rules{Pinned: []string{"Acme-Org"}, Muted: []string{}}, r, "RuleNone takes it out")

	r, err = r.Set("other-org", reporules.RuleNone)
	require.NoError(t, err)
	assert.Equal(t, reporules.Rules{Pinned: []string{"Acme-Org"}, Muted: []string{}}, r, "removing what isn't there changes nothing")
}

func TestSetRefusesWhatItCannotStore(t *testing.T) {
	t.Parallel()

	_, err := reporules.Rules{}.Set(acmeOrg, "hidden")
	require.ErrorIs(t, err, reporules.ErrInvalidRule)

	_, err = reporules.Rules{}.Set("not a repo", reporules.RulePinned)
	require.ErrorIs(t, err, reporules.ErrInvalidTarget)

	var full reporules.Rules

	for i := range reporules.MaxTargets {
		full, err = full.Set(fmt.Sprintf("org-%d", i), reporules.RuleMuted)
		require.NoError(t, err)
	}

	_, err = full.Set("one-more", reporules.RuleMuted)
	require.ErrorIs(t, err, reporules.ErrTooManyTargets)

	moved, err := full.Set("org-1", reporules.RuleMuted)
	require.NoError(t, err, "a target already in the full list is set again")
	assert.Len(t, moved.Muted, reporules.MaxTargets)

	pinned, err := full.Set("one-more", reporules.RulePinned)
	require.NoError(t, err, "each list has its own cap")
	assert.Equal(t, []string{"one-more"}, pinned.Pinned)
}

func TestListsAreSortedIgnoringCaseWithAnOrgBeforeItsRepositories(t *testing.T) {
	t.Parallel()

	var (
		r   reporules.Rules
		err error
	)

	for _, target := range []string{"zeta/b", "Beta", "zeta", "alpha/x", "Zeta/a"} {
		r, err = r.Set(target, reporules.RuleMuted)
		require.NoError(t, err)
	}

	assert.Equal(t, []string{"alpha/x", "Beta", "zeta", "Zeta/a", "zeta/b"}, r.Muted)
}

func TestForPrefersTheRepositorysOwnRule(t *testing.T) {
	t.Parallel()

	r := reporules.Rules{Pinned: []string{"acme-org/example-repo", "solo"}, Muted: []string{"Acme-Org", "solo/quiet"}}

	for name, want := range map[repo.Name]struct {
		rule   reporules.Rule
		target string
	}{
		{Owner: acmeOrg, Repo: "example-repo"}:    {reporules.RulePinned, "acme-org/example-repo"},
		{Owner: "ACME-ORG", Repo: "Example-Repo"}: {reporules.RulePinned, "acme-org/example-repo"},
		{Owner: acmeOrg, Repo: "other"}:           {reporules.RuleMuted, "Acme-Org"},
		{Owner: "solo", Repo: "quiet"}:            {reporules.RuleMuted, "solo/quiet"},
		{Owner: "solo", Repo: "loud"}:             {reporules.RulePinned, "solo"},
		{Owner: "someone", Repo: "else"}:          {reporules.RuleNone, ""},
		{Owner: acmeOrg}:                          {reporules.RuleMuted, "Acme-Org"},
	} {
		rule, target := r.For(name)
		assert.Equal(t, want.rule, rule, name.String())
		assert.Equal(t, want.target, target, name.String())
		assert.Equal(t, want.rule == reporules.RuleMuted, r.IsMuted(name), name.String())
		assert.Equal(t, want.rule == reporules.RulePinned, r.IsPinned(name), name.String())
	}
}

func TestEncodeAndDecode(t *testing.T) {
	t.Parallel()

	r := reporules.Rules{Pinned: []string{"acme-org/example-repo"}, Muted: []string{"other-org", acmeOrg}}

	stored := r.Encode()
	assert.Equal(t, map[string]string{
		reporules.PrefPinned: `["acme-org/example-repo"]`, reporules.PrefMuted: `["acme-org","other-org"]`,
	}, stored)

	got := reporules.Decode(stored)
	assert.Equal(t, reporules.Rules{Pinned: []string{"acme-org/example-repo"}, Muted: []string{acmeOrg, "other-org"}}, got)
	assert.True(t, got.Equal(reporules.Decode(stored)))
	assert.False(t, got.Equal(reporules.Rules{}))

	assert.Equal(t, `[]`, reporules.Rules{}.Encode()[reporules.PrefPinned], "an empty list is stored as one, never null")
	assert.ElementsMatch(t, []string{reporules.PrefPinned, reporules.PrefMuted}, reporules.Keys())
}

func TestDecodeDropsWhatSetWouldNotStore(t *testing.T) {
	t.Parallel()

	empty := reporules.Rules{Pinned: []string{}, Muted: []string{}}

	assert.Equal(t, empty, reporules.Decode(nil))
	assert.Equal(t, empty, reporules.Decode(map[string]string{reporules.PrefPinned: "{broken", reporules.PrefMuted: "null"}))

	got := reporules.Decode(map[string]string{
		reporules.PrefPinned: `["acme-org", "bad target", "ACME-ORG", "x/y"]`,
		reporules.PrefMuted:  `["Acme-Org", "z", "x/y/"]`,
	})
	assert.Equal(t, reporules.Rules{Pinned: []string{acmeOrg, "x/y"}, Muted: []string{"z"}}, got,
		"an invalid target, a repeat, and one the pinned list holds are dropped")
}

func TestRuleValid(t *testing.T) {
	t.Parallel()

	for _, r := range []reporules.Rule{reporules.RuleNone, reporules.RulePinned, reporules.RuleMuted} {
		assert.True(t, r.Valid(), r)
	}

	assert.False(t, reporules.Rule("Pinned").Valid())
}
