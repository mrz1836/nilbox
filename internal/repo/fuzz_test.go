package repo_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/repo"
)

// FuzzParse checks that Parse either fails with ErrInvalid and a zero Name, or returns a
// valid name that writes back as exactly the text it was read from, parses again to
// itself, agrees with New, and keys as that text in lower case.
func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"",
		"acme-org/example-repo",
		"Acme-Org/Example.Repo_2",
		"acme-org/example-repo/extra",
		"acme-org/..",
		"-flag/example-repo",
		"acme_org/-repo",
		"acme-org/café",
		strings.Repeat("a", repo.MaxOwnerLen) + "/" + strings.Repeat("b", repo.MaxRepoLen),
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, s string) {
		name, err := repo.Parse(s)
		if err != nil {
			require.ErrorIs(t, err, repo.ErrInvalid)
			assert.Equal(t, repo.Name{}, name)

			return
		}

		assert.True(t, name.Valid())
		assert.True(t, repo.ValidOwner(name.Owner), "owner %q", name.Owner)
		assert.True(t, repo.ValidRepo(name.Repo), "repo %q", name.Repo)
		assert.Equal(t, s, name.String())
		assert.Equal(t, strings.ToLower(s), name.Key())

		again, err := repo.Parse(name.String())
		require.NoError(t, err)
		assert.Equal(t, name, again)

		built, err := repo.New(name.Owner, name.Repo)
		require.NoError(t, err)
		assert.Equal(t, name, built)
	})
}
