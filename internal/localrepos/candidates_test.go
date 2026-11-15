package localrepos

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCandidatesFindClonesWhateverTheFolderIsCalled(t *testing.T) {
	t.Parallel()

	home := t.TempDir()

	// ~/code holds three clones; ~/src none directly but two owners' folders with some;
	// ~/Work Stuff, a name no list holds, one; ~/dev only folders without clones; and
	// GOPATH's and ghq's layouts an owner each.
	for _, name := range []string{"api", "web", "cli"} {
		fakeClone(t, filepath.Join(home, "code", name))
	}

	fakeClone(t, filepath.Join(home, "src", "acme-org", "example-repo"))
	fakeClone(t, filepath.Join(home, "src", "acme-org", "other-repo"))
	fakeClone(t, filepath.Join(home, "src", "octo-user", "dotfiles"))
	fakeClone(t, filepath.Join(home, "Work Stuff", "solo"))
	mkdir(t, filepath.Join(home, "dev", "notes"))
	fakeClone(t, filepath.Join(home, "go", "src", "github.com", "acme-org", "tool"))
	fakeClone(t, filepath.Join(home, "ghq", "github.com", "octo-org", "cli-tool"))

	got := Candidates(home, rootsAt(t, filepath.Join(home, "projects"), home))

	assert.Equal(t, []Suggestion{
		{Path: "~/code", Clones: 3},
		{Path: "~/src/acme-org", Clones: 2},
		{Path: "~/Work Stuff", Clones: 1},
		{Path: "~/ghq/github.com/octo-org", Clones: 1},
		{Path: "~/go/src/github.com/acme-org", Clones: 1},
		{Path: "~/src/octo-user", Clones: 1},
	}, got, "the most clones first, then by name")
}

func TestCandidatesNeverReadWhatTheyShouldNot(t *testing.T) {
	t.Parallel()

	home := t.TempDir()

	// A hidden folder, a link to a folder of clones elsewhere, and macOS's own folders,
	// which macOS guards or which never hold clones.
	fakeClone(t, filepath.Join(home, ".config", "tool"))
	fakeClone(t, filepath.Join(home, "Documents", "GitHub", "api"))
	fakeClone(t, filepath.Join(home, "Desktop", "web"))
	fakeClone(t, filepath.Join(home, "Library", "Caches", "cli"))

	elsewhere := t.TempDir()
	fakeClone(t, filepath.Join(elsewhere, "repos", "linked"))
	require.NoError(t, os.Symlink(filepath.Join(elsewhere, "repos"), filepath.Join(home, "linked")))

	// Disks mounted directly in home, and one level down in a folder that is looked in:
	// the mount table names one by macOS's data volume path.
	fakeClone(t, filepath.Join(home, "OrbStack", "machine"))
	fakeClone(t, filepath.Join(home, "mnt", "server", "remote"))
	fakeClone(t, filepath.Join(home, "mnt", "local", "kept"))

	mounts := []string{
		"/",
		dataVolume + filepath.Join(home, "OrbStack"),
		filepath.Join(home, "mnt", "server"),
	}

	got := candidatesIn(home, rootsAt(t, filepath.Join(home, "projects"), home), mounts, candidateEntries)

	assert.Equal(t, []Suggestion{{Path: "~/mnt/local", Clones: 1}}, got)
}

func TestCandidatesLookInTheCommonNamesFirst(t *testing.T) {
	t.Parallel()

	home := t.TempDir()

	// ~/aaa comes first by name, and its folders would spend the whole budget.
	for _, name := range []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k", "l"} {
		mkdir(t, filepath.Join(home, "aaa", name))
	}

	fakeClone(t, filepath.Join(home, "code", "api"))
	fakeClone(t, filepath.Join(home, "code", "web"))
	fakeClone(t, filepath.Join(home, "zzz", "late"))

	got := candidatesIn(home, rootsAt(t, filepath.Join(home, "projects"), home), nil, 10)

	assert.Equal(t, []Suggestion{{Path: "~/code", Clones: 2}}, got,
		"the common name is looked in before the budget runs out, and nothing past it is read")
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

	// ghq's layout is a link to ~/gh, which holds an owner's folder, and so is ~/code.
	home := t.TempDir()
	fakeClone(t, filepath.Join(home, "gh", "acme-org", "api"))
	mkdir(t, filepath.Join(home, "ghq"))
	require.NoError(t, os.Symlink(filepath.Join(home, "gh"), filepath.Join(home, "ghq", "github.com")))
	require.NoError(t, os.Symlink(filepath.Join(home, "gh"), filepath.Join(home, "code")))

	got := Candidates(home, rootsAt(t, filepath.Join(home, "projects"), home))
	assert.Equal(t, []Suggestion{{Path: "~/gh/acme-org", Clones: 1}}, got,
		"a folder is listed once, by its own name, however many links lead to it")
}

func TestCandidatesWithNothingToSuggest(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	mkdir(t, filepath.Join(home, "projects"))

	got := Candidates(home, rootsAt(t, filepath.Join(home, "projects"), home))
	assert.NotNil(t, got)
	assert.Empty(t, got)

	assert.Empty(t, Candidates("", Roots{}), "without a home there is nowhere to look")
	assert.Empty(t, Candidates(filepath.Join(home, "missing"), Roots{}), "nor in a home that isn't there")
}

func TestMountSetNamesEachMountAsTheUserSeesIt(t *testing.T) {
	t.Parallel()

	set := mountSet([]string{"/", dataVolume, dataVolume + "/Users/example/OrbStack", "/Volumes/Backup/"})

	for _, dir := range []string{
		"/", dataVolume, dataVolume + "/Users/example/OrbStack", "/Users/example/OrbStack",
		"/Volumes/Backup",
	} {
		assert.True(t, set[dir], dir)
	}

	assert.Len(t, set, 5)
}
