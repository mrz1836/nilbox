package scripts

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/repo"
)

func TestSumPathKinds(t *testing.T) {
	t.Parallel()

	dir := folder(t, map[string]string{"magefiles/main.go": magefile, "magefiles/sub/x.txt": "x"})
	root := filepath.Join(dir, "magefiles")
	require.NoError(t, os.Symlink(filepath.Join(root, "gone"), filepath.Join(root, "dangling")))
	require.NoError(t, os.Symlink(filepath.Join(root, "sub"), filepath.Join(root, "folder")))

	budget := int64(maxMagefileBytes)

	got, err := sumPath(root, filepath.Join(root, "dangling"), &budget)
	require.NoError(t, err)
	assert.Equal(t, fileSum{Path: "dangling", Kind: "link", Link: filepath.Join(root, "gone")}, got,
		"a link to nothing is summed by where it points")

	got, err = sumPath(root, filepath.Join(root, "folder"), &budget)
	require.NoError(t, err)
	assert.Equal(t, fileSum{Path: "folder", Kind: "link", Link: filepath.Join(root, "sub")}, got)

	_, err = sumPath(root, filepath.Join(root, "missing"), &budget)
	require.Error(t, err)

	assert.Equal(t, "Couldn't read the repository's magefiles: no such file.",
		magefilesMessage(errors.New("no such file"))) //nolint:err113 // a reason to word
}

func TestTrustStoreWithoutADatabase(t *testing.T) {
	t.Parallel()

	store := newTrustStore(nil, nil)
	name := repo.Name{Owner: "acme-org", Repo: "sdk"}
	key := trustKey{source: SourceT3, name: "Test"}

	got, err := store.forRepo(t.Context(), name)
	require.NoError(t, err)
	assert.Empty(t, got)

	require.NoError(t, store.set(t.Context(), name, key, trustRecord{sha256: "x"}), "nothing is kept")

	_, ok, err := store.get(t.Context(), name, key)
	require.NoError(t, err)
	assert.False(t, ok, "so every repository command asks")
}

func TestServiceDefaults(t *testing.T) {
	t.Parallel()

	s := NewService(Config{Clones: &fakeClones{}})

	cfg, err := s.config(t.Context())
	require.NoError(t, err)
	assert.Equal(t, integrationprefs.Decode(nil), cfg, "every source on")
	assert.Equal(t, ToolFacts{}, s.tool(t.Context()), "no magex")
	assert.True(t, filepath.IsAbs(s.shell()))

	env, read := s.env()
	assert.True(t, read)
	assert.NotEmpty(t, env, "the app's own")
}
