package pushwatch

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// originRepos are the bare repository and its clone originAndClone makes, built once for
// the test binary by buildOrigin and copied into each test that asks for them.
var originRepos = testutil.NewGitTemplate(buildOrigin) //nolint:gochecknoglobals // built once for the whole test binary

// buildOrigin builds origin.git, an empty bare repository on main, and example-repo, a
// clone of it.
func buildOrigin(b *testutil.GitBuild) struct{} {
	origin := filepath.Join(b.Root, "origin.git")

	b.Git(".", "-c", "init.defaultBranch=main", "init", "-q", "--bare", origin)
	b.Git(".", "clone", "-q", origin, filepath.Join(b.Root, "example-repo"))

	return struct{}{}
}

// originAndClone makes a bare repository standing in for GitHub, and a clone of it, as
// the clone's folder name, under a fresh folder, and returns the bare one's and the
// clone's paths.
func originAndClone(t *testing.T) (origin, clone string) {
	t.Helper()

	base := t.TempDir()
	originRepos.Copy(t, base)

	return filepath.Join(base, "origin.git"), filepath.Join(base, "example-repo")
}

// push commits a file named name on the clone's current branch and pushes the branch to
// origin, and returns the commit.
func push(t *testing.T, clone, branch, name string) string {
	t.Helper()

	commit := testutil.Commit(t, clone, name, name)
	testutil.RunGit(t, clone, "push", "-q", "origin", branch)

	return commit
}

func TestAPushMovesOriginsBranchInTheCloneAsANewFile(t *testing.T) {
	t.Parallel()

	_, clone := originAndClone(t)
	testutil.RunGit(t, clone, "checkout", "-q", "-b", "feat/watch")

	first := push(t, clone, "feat/watch", "one.txt")
	common := localrepos.CommonDir(clone)
	path := trackingPath(common, "feat/watch")

	assert.Equal(t, first, readLoose(path), "the push moved origin's branch in the clone")

	before := statFile(path)
	require.True(t, before.exists)

	second := push(t, clone, "feat/watch", "two.txt")
	assert.Equal(t, second, readLoose(path))
	assert.NotEqual(t, before, statFile(path), "git writes a moved ref as a new file")

	testutil.RunGit(t, clone, "pack-refs", "--all")
	assert.False(t, statFile(path).exists, "packing takes the loose ref away")
	assert.Empty(t, readLoose(path), "a ref packed away reads as unknown")

	third := push(t, clone, "feat/watch", "three.txt")
	assert.Equal(t, third, readLoose(path), "the next push writes the loose ref again")
}

func TestAReftableClonesBranchIsReadWithGit(t *testing.T) {
	t.Parallel()

	env := testutil.Git(t)
	base := t.TempDir()
	origin := filepath.Join(base, "origin.git")
	clone := filepath.Join(base, "example-repo")
	testutil.RunGit(t, base, "-c", "init.defaultBranch=main", "init", "-q", "--bare", origin)

	if _, _, err := (execpath.Exec{Env: env}).Run(t.Context(), "git", "clone", "-q", "--ref-format=reftable", origin,
		clone); err != nil {
		t.Skip("this git can't keep refs in reftable")
	}

	git := gitread.New(gitread.Config{Env: func() []string { return env }})
	common := localrepos.CommonDir(clone)
	testutil.RunGit(t, clone, "checkout", "-q", "-b", "feature")

	first := push(t, clone, "feature", "one.txt")
	require.True(t, statFile(tablesPath(common)).exists, "the clone keeps its refs in reftable")
	assert.False(t, statFile(trackingPath(common, "feature")).exists, "and has no file for the ref")
	assert.Equal(t, first, readTable(t.Context(), git, clone, "feature"))

	before := statFile(tablesPath(common))
	second := push(t, clone, "feature", "two.txt")

	assert.NotEqual(t, before, statFile(tablesPath(common)), "a push replaces the table list")
	assert.Equal(t, second, readTable(t.Context(), git, clone, "feature"))
	assert.Empty(t, readTable(t.Context(), git, clone, "no-such-branch"))
	assert.Empty(t, readTable(t.Context(), nil, clone, "feature"), "without git, nothing is read")
}

func TestCommitOfTakesAFullCommitIDAlone(t *testing.T) {
	t.Parallel()

	sha1, sha256 := strings.Repeat("a1", 20), strings.Repeat("b2", 32)

	for in, want := range map[string]string{
		sha1 + "\n":                     sha1,
		"  " + sha256 + "\r\n":          sha256,
		"ref: refs/remotes/origin/main": "",
		strings.Repeat("0", 40):         "",
		strings.ToUpper(sha1):           "",
		sha1[:39]:                       "",
		sha1 + " " + sha1:               "",
		"":                              "",
	} {
		assert.Equal(t, want, commitOf([]byte(in)), "%q", in)
	}
}

func TestStatFileSeesOnlyARegularFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	file := filepath.Join(dir, "ref")
	testutil.WriteFile(t, file, "content")

	link := filepath.Join(dir, "link")
	require.NoError(t, os.Symlink(file, link))

	got := statFile(file)
	assert.True(t, got.exists)
	assert.Equal(t, int64(len("content")), got.size)
	assert.Equal(t, fileSig{}, statFile(link), "a link is never followed")
	assert.Equal(t, fileSig{}, statFile(dir), "nor a folder read")
	assert.Equal(t, fileSig{}, statFile(filepath.Join(dir, "missing")))
	assert.Empty(t, readLoose(link))
}

func TestTrackingPathStaysInOriginsRefs(t *testing.T) {
	t.Parallel()

	assert.Equal(t, filepath.Join("/git", "refs", "remotes", "origin", "feat", "x"), trackingPath("/git", "feat/x"))
	assert.Equal(t, "refs/remotes/origin/feat/x", trackingRef("feat/x"))
	assert.Equal(t, filepath.Join("/git", "reftable", "tables.list"), tablesPath("/git"))
	assert.Equal(t, filepath.Join("/git", "config"), configPath("/git"))
}
