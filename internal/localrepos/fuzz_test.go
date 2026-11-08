package localrepos

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/repo"
)

// FuzzParseRemoteURL checks that a remote URL either names no repository or names a
// valid one, which the canonical https and scp-like forms of the name, as a clone writes
// them, name again.
func FuzzParseRemoteURL(f *testing.F) {
	for _, seed := range []string{
		"",
		"https://github.com/acme-org/example-repo.git",
		"git@github.com:acme-org/example-repo.git",
		"ssh://git@ssh.github.com:443/acme-org/example-repo/",
		"  HTTPS://WWW.GitHub.com/Acme-Org/Example.Repo.GIT  ",
		"https://github.com/acme-org/example-repo%2Fextra",
		"https://gitlab.com/acme-org/example-repo.git",
		"git@github.com:acme-org",
		"file:///src/example-repo",
		"github.com:acme-org/example-repo.git.git",
	} {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		name, ok := parseRemoteURL(raw)
		if !ok {
			assert.Equal(t, repo.Name{}, name)

			return
		}

		require.True(t, name.Valid(), "parsed an invalid name %q from %q", name, raw)

		for _, canonical := range []string{
			"https://github.com/" + name.String() + ".git",
			"git@github.com:" + name.String() + ".git",
		} {
			again, ok := parseRemoteURL(canonical)
			require.True(t, ok, "%q names no repository", canonical)
			assert.Equal(t, name, again, "%q", canonical)
		}
	})
}
