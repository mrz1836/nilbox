package repo_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/repo"
)

func TestValidOwner(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		owner string
		want  bool
	}{
		{name: "org", owner: "acme-org", want: true},
		{name: "user", owner: "octo-user", want: true},
		{name: "one character", owner: "a", want: true},
		{name: "mixed case and digits", owner: "Acme-Org-2", want: true},
		{name: "39 characters", owner: strings.Repeat("a", repo.MaxOwnerLen), want: true},
		{name: "managed user", owner: "octo_acme", want: true},
		{name: "legacy trailing hyphen", owner: "octo-", want: true},
		{name: "legacy double hyphen", owner: "a--b", want: true},
		{name: "empty", owner: ""},
		{name: "40 characters", owner: strings.Repeat("a", repo.MaxOwnerLen+1)},
		{name: "leading hyphen", owner: "-octo"},
		{name: "leading underscore", owner: "_octo"},
		{name: "dot", owner: "octo.cat"},
		{name: "dot alone", owner: "."},
		{name: "dot dot", owner: ".."},
		{name: "slash", owner: "acme/org"},
		{name: "space", owner: "acme org"},
		{name: "shell character", owner: "acme$"},
		{name: "non-ASCII letter", owner: "acmé"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, repo.ValidOwner(tt.owner))
		})
	}
}

func TestValidRepo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		repo string
		want bool
	}{
		{name: "plain", repo: "example-repo", want: true},
		{name: "underscore", repo: "example_repo", want: true},
		{name: "dot", repo: "example.repo", want: true},
		{name: "leading dot", repo: ".github", want: true},
		{name: "trailing hyphen", repo: "repo-", want: true},
		{name: "one character", repo: "a", want: true},
		{name: "100 characters", repo: strings.Repeat("a", repo.MaxRepoLen), want: true},
		{name: "empty", repo: ""},
		{name: "101 characters", repo: strings.Repeat("a", repo.MaxRepoLen+1)},
		{name: "dot", repo: "."},
		{name: "dot dot", repo: ".."},
		{name: "leading hyphen", repo: "-repo"},
		{name: "flag", repo: "--help"},
		{name: "slash", repo: "a/b"},
		{name: "backslash", repo: `a\b`},
		{name: "space", repo: "a b"},
		{name: "tilde", repo: "repo~"},
		{name: "dollar", repo: "repo$"},
		{name: "non-ASCII letter", repo: "ré"},
	}

	for _, tt := range tests {
		t.Run(tt.name+" "+tt.repo, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, repo.ValidRepo(tt.repo))
		})
	}
}

func TestParse(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in   string
		want repo.Name
		ok   bool
	}{
		{in: "acme-org/example-repo", want: repo.Name{Owner: "acme-org", Repo: "example-repo"}, ok: true},
		{in: "Acme-Org/Example.Repo", want: repo.Name{Owner: "Acme-Org", Repo: "Example.Repo"}, ok: true},
		{in: "acme-org"},
		{in: "acme-org/"},
		{in: "/example-repo"},
		{in: "acme-org/example-repo/extra"},
		{in: "octo_acme/example-repo", want: repo.Name{Owner: "octo_acme", Repo: "example-repo"}, ok: true},
		{in: "_octo/example-repo"},
		{in: "acme-org/-flag"},
		{in: ""},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			t.Parallel()

			got, err := repo.Parse(tt.in)
			if !tt.ok {
				require.ErrorIs(t, err, repo.ErrInvalid)
				assert.Equal(t, repo.Name{}, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNew(t *testing.T) {
	t.Parallel()

	got, err := repo.New("acme-org", "example-repo")
	require.NoError(t, err)
	assert.Equal(t, repo.Name{Owner: "acme-org", Repo: "example-repo"}, got)

	_, err = repo.New("acme-org", "..")
	require.ErrorIs(t, err, repo.ErrInvalid)
	assert.EqualError(t, err, `not a valid GitHub repository name: "acme-org/.."`)
}

func TestNameFormats(t *testing.T) {
	t.Parallel()

	n := repo.Name{Owner: "Acme-Org", Repo: "Example-Repo"}

	assert.True(t, n.Valid())
	assert.False(t, repo.Name{Owner: "acme-org"}.Valid(), "an owner alone is not a repository")
	assert.Equal(t, "Acme-Org/Example-Repo", n.String())
	assert.Equal(t, "acme-org/example-repo", n.Key())
	assert.Equal(t, repo.Name{Owner: "acme-org", Repo: "example-repo"}.Key(), n.Key(), "keys ignore case")
	assert.Equal(t, "acme-org/", repo.Name{Owner: "ACME-ORG"}.Key())
	assert.Equal(t, "Acme-Org/Example-Repo#184", n.Ref(184))
}
