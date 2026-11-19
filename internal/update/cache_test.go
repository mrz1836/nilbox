package update_test

import (
	"crypto/ed25519"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/update"
)

// putIn puts the release of version, signed with private, into cache, failing the test on
// an error.
func putIn(t *testing.T, cache *update.Cache, version string, private ed25519.PrivateKey) update.Entry {
	t.Helper()

	m := sign(t, version, zipOf(version), private)

	entry, err := cache.Put(m, func(w io.Writer) error {
		_, err := w.Write(zipOf(version))

		return err
	}, nil)
	require.NoError(t, err)

	return entry
}

// folders lists the folders in dir.
func folders(t *testing.T, dir string) []string {
	t.Helper()

	listed, err := os.ReadDir(dir)
	require.NoError(t, err)

	out := make([]string, 0, len(listed))
	for _, item := range listed {
		out = append(out, item.Name())
	}

	return out
}

func TestPutKeepsTheZipAndItsManifestTogether(t *testing.T) {
	t.Parallel()

	public, private := testKey(7)
	dir := t.TempDir()
	cache := update.NewCache(dir, public)

	var seen []int64

	m := sign(t, next, zipOf(next), private)

	entry, err := cache.Put(m, func(w io.Writer) error {
		for _, part := range [][]byte{zipOf(next)[:4], zipOf(next)[4:]} {
			if _, err := w.Write(part); err != nil {
				return err
			}
		}

		return nil
	}, func(written int64) { seen = append(seen, written) })
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(dir, next, m.File), entry.Zip)
	assert.Equal(t, m, entry.Manifest)
	assert.Equal(t, []int64{4, int64(len(zipOf(next)))}, seen, "progress counts what has arrived")
	assert.ElementsMatch(t, []string{m.File, update.ManifestName}, folders(t, filepath.Join(dir, next)),
		"no temporary file is left")

	info, err := os.Stat(entry.Zip)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestPutLeavesNothingOfAFailedDownload(t *testing.T) {
	t.Parallel()

	public, private := testKey(7)
	m := sign(t, next, zipOf(next), private)

	for name, fetch := range map[string]func(w io.Writer) error{
		"a fetch that fails partway": func(w io.Writer) error {
			_, _ = w.Write(zipOf(next)[:3])

			return errFakeNetwork
		},
		"a file that runs on": func(w io.Writer) error {
			_, err := w.Write(append(zipOf(next), '!'))

			return err
		},
		"a file another's digest": func(w io.Writer) error {
			body := zipOf(next)
			body[0] ^= 1
			_, err := w.Write(body)

			return err
		},
	} {
		dir := t.TempDir()

		_, err := update.NewCache(dir, public).Put(m, fetch, nil)
		require.Error(t, err, name)
		assert.Empty(t, folders(t, dir), "nothing of the version is left: %s", name)
	}
}

func TestLoadChecksEveryUpdateKeptAgain(t *testing.T) {
	t.Parallel()

	public, private := testKey(7)
	_, stranger := testKey(9)
	dir := t.TempDir()
	cache := update.NewCache(dir, public)

	putIn(t, cache, running, private)                                               // installed
	putIn(t, cache, "0.2.5", private)                                               // superseded by a newer one
	tampered := putIn(t, cache, "0.4.0", private)                                   // a byte changed after
	putIn(t, update.NewCache(dir, update.PublicKeyOf(stranger)), "0.5.0", stranger) // another key's
	good := putIn(t, cache, later, private)

	// A version folder holding another version's manifest.
	moved := putIn(t, update.NewCache(t.TempDir(), public), "0.6.0", private)
	require.NoError(t, os.Rename(filepath.Dir(moved.Zip), filepath.Join(dir, "0.7.0")))

	body := zipOf("0.4.0")
	body[0] ^= 1
	require.NoError(t, os.WriteFile(tampered.Zip, body, 0o600))

	// What isn't a version folder is left alone.
	require.NoError(t, os.Mkdir(filepath.Join(dir, "notes"), 0o700))

	entry, ok, err := cache.Load(running)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, good, entry, "the newest that checks out")
	assert.ElementsMatch(t, []string{later, "notes"}, folders(t, dir), "the rest is dropped")
}

func TestLoadOfAnEmptyOrMissingCacheFindsNothing(t *testing.T) {
	t.Parallel()

	public, _ := testKey(7)

	for _, dir := range []string{t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		_, ok, err := update.NewCache(dir, public).Load(running)
		require.NoError(t, err)
		assert.False(t, ok)
	}
}

func TestPruneKeepsOneVersionOrNone(t *testing.T) {
	t.Parallel()

	public, private := testKey(7)
	dir := t.TempDir()
	cache := update.NewCache(dir, public)

	putIn(t, cache, next, private)
	putIn(t, cache, later, private)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "notes"), 0o700))

	require.NoError(t, cache.Prune(later))
	assert.ElementsMatch(t, []string{later, "notes"}, folders(t, dir))

	require.NoError(t, cache.Prune(""))
	assert.Equal(t, []string{"notes"}, folders(t, dir))

	require.NoError(t, update.NewCache(filepath.Join(dir, "missing"), public).Prune(""), "a missing cache holds nothing")
}

func TestPutRefusesAManifestNamingNoReleasesFile(t *testing.T) {
	t.Parallel()

	public, private := testKey(7)
	m := sign(t, next, zipOf(next), private)
	m.File = "../escape.zip"

	_, err := update.NewCache(t.TempDir(), public).Put(m, func(io.Writer) error { return nil }, nil)
	require.ErrorIs(t, err, update.ErrInvalid)
	assert.NotErrorIs(t, err, update.ErrMismatch)
}

func TestLoadKeepsTheNewestByVersionNotByName(t *testing.T) {
	t.Parallel()

	public, private := testKey(7)
	dir := t.TempDir()
	cache := update.NewCache(dir, public)

	// "0.10.0" lists before "0.9.0", and is the newer.
	newest := putIn(t, cache, "0.10.0", private)
	putIn(t, cache, "0.9.0", private)

	entry, ok, err := cache.Load(running)
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, newest, entry)
	assert.Equal(t, []string{"0.10.0"}, folders(t, dir), "the older is dropped")
}

func TestLoadDropsAnUpdateWhoseManifestCantBeRead(t *testing.T) {
	t.Parallel()

	public, private := testKey(7)
	dir := t.TempDir()
	cache := update.NewCache(dir, public)

	entry := putIn(t, cache, next, private)
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(entry.Zip), update.ManifestName), []byte("{"), 0o600))

	_, ok, err := cache.Load(running)
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, folders(t, dir))
}

func TestACacheThatIsntAFolderCantBeLoadedOrPruned(t *testing.T) {
	t.Parallel()

	public, _ := testKey(7)
	file := filepath.Join(t.TempDir(), "updates")
	require.NoError(t, os.WriteFile(file, []byte("a file"), 0o600))

	cache := update.NewCache(file, public)

	_, ok, err := cache.Load(running)
	require.Error(t, err)
	assert.False(t, ok)
	require.Error(t, cache.Prune(""))
}

func TestPutLeavesNothingWhenSomethingTakesTheZipsOrManifestsPlace(t *testing.T) {
	t.Parallel()

	public, private := testKey(7)
	m := sign(t, next, zipOf(next), private)

	for _, name := range []string{m.File, update.ManifestName} {
		dir := t.TempDir()

		_, err := update.NewCache(dir, public).Put(m, func(w io.Writer) error {
			// A folder, not empty, where the file goes.
			if err := os.MkdirAll(filepath.Join(dir, next, name, "inside"), 0o700); err != nil {
				return err
			}

			_, err := w.Write(zipOf(next))

			return err
		}, nil)
		require.Error(t, err, name)
		assert.Empty(t, folders(t, dir), "nothing of the version is left: %s", name)
	}
}

func TestPutSaysWhatItCouldntSaveInAFolderItCantWrite(t *testing.T) {
	t.Parallel()
	testutil.SkipAsRoot(t)

	public, private := testKey(7)
	m := sign(t, next, zipOf(next), private)
	fetch := func(w io.Writer) error {
		_, err := w.Write(zipOf(next))

		return err
	}

	root := t.TempDir()
	kept := filepath.Join(root, "kept")
	putIn(t, update.NewCache(kept, public), next, private)

	require.NoError(t, os.Chmod(root, 0o500))       //nolint:gosec // the test's own folder, made read-only
	require.NoError(t, os.Chmod(kept, 0o500))       //nolint:gosec // the test's own folder, made read-only
	t.Cleanup(func() { _ = os.Chmod(root, 0o700) }) //nolint:gosec // put back for t.TempDir's removal
	t.Cleanup(func() { _ = os.Chmod(kept, 0o700) }) //nolint:gosec // put back for t.TempDir's removal

	_, err := update.NewCache(filepath.Join(root, "missing"), public).Put(m, fetch, nil)
	require.ErrorContains(t, err, "make the update's folder")

	_, err = update.NewCache(kept, public).Put(m, fetch, nil)
	require.ErrorContains(t, err, "remove "+next+" from the updates kept", "the copy kept before can't be cleared")
	assert.Equal(t, []string{next}, folders(t, kept))
}
