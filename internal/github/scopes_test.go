package github

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckScopes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		header      string
		wantWarning ScopeWarning
		wantMissing []string
	}{
		{name: "all granted", header: "gist, read:org, repo, workflow"},
		{name: "no spaces", header: "repo,read:org"},
		{name: "write:org covers read:org", header: "repo, write:org"},
		{name: "admin:org covers read:org", header: "admin:org, repo"},
		{name: "missing both", header: "gist", wantMissing: []string{"repo", "read:org"}},
		{name: "missing read:org", header: "repo, workflow", wantMissing: []string{"read:org"}},
		{name: "public_repo is not repo", header: "public_repo, read:org", wantMissing: []string{"repo"}},
		{name: "no header", header: "", wantWarning: ScopesUnverifiable},
		{name: "blank header", header: "  ", wantWarning: ScopesUnverifiable},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			warning, err := CheckScopes(tc.header)
			assert.Equal(t, tc.wantWarning, warning)

			if tc.wantMissing == nil {
				require.NoError(t, err)

				return
			}

			require.ErrorIs(t, err, ErrMissingScopes)

			var missing *MissingScopesError
			require.ErrorAs(t, err, &missing)
			assert.Equal(t, tc.wantMissing, missing.Missing)
			assert.Contains(t, err.Error(), "gh auth refresh -s repo,read:org")

			for _, scope := range tc.wantMissing {
				assert.Contains(t, err.Error(), scope)
			}
		})
	}
}

func TestMissingScopesErrorIsOnlyMissingScopes(t *testing.T) {
	t.Parallel()

	err := &MissingScopesError{Missing: []string{scopeRepo}}

	require.ErrorIs(t, err, ErrMissingScopes)
	require.NotErrorIs(t, err, ErrNotLoggedIn)
}

func TestProjectsScope(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		header string
		want   ProjectScope
	}{
		{name: "read:project", header: "gist, read:org, read:project, repo", want: ProjectScopeGranted},
		{name: "project covers read:project", header: "project,repo,read:org", want: ProjectScopeGranted},
		{name: "spaces around it", header: " repo ,  read:project ", want: ProjectScopeGranted},
		{name: "neither", header: "repo, read:org, workflow", want: ProjectScopeMissing},
		{name: "a lookalike is not it", header: "repo, read:projects, write:project", want: ProjectScopeMissing},
		{name: "no header", header: "", want: ProjectScopeUnverified},
		{name: "blank header", header: "  ", want: ProjectScopeUnverified},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, ProjectsScope(tc.header))
		})
	}
}
