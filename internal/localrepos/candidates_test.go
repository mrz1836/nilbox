package localrepos

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCandidatesFindTheFoldersWhereClonesLive(t *testing.T) {
	t.Parallel()

	home := t.TempDir()

	// ~/code holds three clones, ~/src none directly but two owners' folders with some,
	// ~/repos one, and ~/dev only folders without clones.
	for _, name := range []string{"api", "web", "cli"} {
		fakeClone(t, filepath.Join(home, "code", name))
	}

	fakeClone(t, filepath.Join(home, "src", "acme-org", "example-repo"))
	fakeClone(t, filepath.Join(home, "src", "acme-org", "other-repo"))
	fakeClone(t, filepath.Join(home, "src", "octo-user", "dotfiles"))
	fakeClone(t, filepath.Join(home, "repos", "solo"))
	mkdir(t, filepath.Join(home, "dev", "notes"))
	fakeClone(t, filepath.Join(home, "go", "src", "github.com", "acme-org", "tool"))

	// A folder elsewhere, which no common name leads to, is never looked in.
	fakeClone(t, filepath.Join(home, "elsewhere", "hidden-away"))

	got := Candidates(home, rootsAt(t, filepath.Join(home, "projects"), home))

	assert.Equal(t, []Suggestion{
		{Path: "~/code", Clones: 3},
		{Path: "~/src/acme-org", Clones: 2},
		{Path: "~/go/src/github.com/acme-org", Clones: 1},
		{Path: "~/repos", Clones: 1},
		{Path: "~/src/octo-user", Clones: 1},
	}, got, "the most clones first, then by name")
}

func TestCandidatesLeaveOutWhatTheLocationsHold(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	fakeClone(t, filepath.Join(home, "projects", "api"))
	fakeClone(t, filepath.Join(home, "code", "web"))
	fakeClone(t, filepath.Join(home, "repos", "cli"))

	roots := rootsAt(t, filepath.Join(home, "projects"), home, Location{Path: "~/code", Kind: KindFolder})

	assert.Equal(t, []Suggestion{{Path: "~/repos", Clones: 1}}, Candidates(home, roots),
		"the clone root and the other locations are no suggestion")
}

func TestCandidatesListAFolderOnce(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	fakeClone(t, filepath.Join(home, "workspace", "api"))
	require.NoError(t, os.Symlink(filepath.Join(home, "workspace"), filepath.Join(home, "code")))

	got := Candidates(home, rootsAt(t, filepath.Join(home, "projects"), home))
	assert.Equal(t, []Suggestion{{Path: "~/code", Clones: 1}}, got, "a link and its folder are one suggestion")
}

func TestCandidatesWithNothingToSuggest(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	mkdir(t, filepath.Join(home, "projects"))

	got := Candidates(home, rootsAt(t, filepath.Join(home, "projects"), home))
	assert.NotNil(t, got)
	assert.Empty(t, got)

	assert.Empty(t, Candidates("", Roots{}), "without a home there is nowhere to look")
}
