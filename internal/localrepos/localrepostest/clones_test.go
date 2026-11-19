package localrepostest_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/localrepos/localrepostest"
	"github.com/mrz1836/nilbox/internal/repo"
)

var errFake = errors.New("fake: failed")

// cloneIn makes a folder called name under root that reads as a clone, and returns it.
func cloneIn(t *testing.T, root, name string) string {
	t.Helper()

	dir := filepath.Join(root, name)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o700))

	return dir
}

func TestClonesListsTheClonesInItsRootsAndCountsEachRead(t *testing.T) {
	t.Parallel()

	root, work := t.TempDir(), t.TempDir()
	api, web := cloneIn(t, root, "api"), cloneIn(t, work, "web")

	c := localrepostest.New(root)
	roots, err := c.Roots(t.Context())
	require.NoError(t, err)
	assert.Equal(t, root, roots.Primary())

	dirs, err := c.Dirs(t.Context(), roots)
	require.NoError(t, err)
	assert.Equal(t, []string{api}, dirs)

	c.SetExtra(localrepos.Location{Path: work, Kind: localrepos.KindFolder})
	roots, err = c.Roots(t.Context())
	require.NoError(t, err)

	dirs, err = c.Dirs(t.Context(), roots)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{api, web}, dirs, "the other locations' clones too")

	c.SetRoot(work)
	roots, err = c.Roots(t.Context())
	require.NoError(t, err)
	assert.Equal(t, work, roots.Primary(), "the clone root moved")

	assert.Equal(t, localrepostest.Reads{Roots: 3, Dirs: 2}, c.Reads())
}

func TestClonesFailsWhereATestSays(t *testing.T) {
	t.Parallel()

	c := localrepostest.New(t.TempDir())
	roots, err := c.Roots(t.Context())
	require.NoError(t, err)

	c.FailRoots(errFake)
	_, err = c.Roots(t.Context())
	require.ErrorIs(t, err, errFake)

	c.FailDirs(errFake)
	_, err = c.Dirs(t.Context(), roots)
	require.ErrorIs(t, err, errFake)

	c.FailRoots(nil)
	c.FailDirs(nil)
	_, err = c.Roots(t.Context())
	require.NoError(t, err, "nil reads the roots again")

	dirs, err := c.Dirs(t.Context(), roots)
	require.NoError(t, err, "and lists the clones again")
	assert.Empty(t, dirs)

	assert.Equal(t, localrepostest.Reads{Roots: 3, Dirs: 2}, c.Reads(), "a failed read counts too")
}

func TestClonesOriginsAndHooks(t *testing.T) {
	t.Parallel()

	var c localrepostest.Clones // the zero value has no origin

	name, ok, err := c.Origin(t.Context(), "/projects/sdk")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Zero(t, name)

	sdk := repo.Name{Owner: "acme-org", Repo: "sdk"}
	c.SetOrigin("/projects/sdk", sdk)

	var listed, asked int

	c.Hook(func() { listed++ }, func() { asked++ })

	name, ok, err = c.Origin(t.Context(), "/projects/sdk")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, sdk, name)

	roots, err := localrepos.NewRoots(t.TempDir(), nil, "")
	require.NoError(t, err)

	_, err = c.Dirs(t.Context(), roots)
	require.NoError(t, err)

	c.ForgetOrigin("/projects/sdk")
	c.Hook(nil, nil)

	_, ok, err = c.Origin(t.Context(), "/projects/sdk")
	require.NoError(t, err)
	assert.False(t, ok, "a forgotten origin is off GitHub")

	assert.Equal(t, 1, listed, "the listing's hook ran once, while it was set")
	assert.Equal(t, 1, asked, "and the origin's")
	assert.Equal(t, localrepostest.Reads{Dirs: 1, Origins: 3}, c.Reads())
}
