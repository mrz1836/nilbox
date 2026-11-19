//go:build mage

package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/update"
)

// keygenRepo makes a folder that stands in for the repository, and returns it with a home
// folder beside it.
func keygenRepo(t *testing.T) (string, string) {
	t.Helper()

	root := testutil.RealTempDir(t)
	repo := filepath.Join(root, "Repo")
	home := filepath.Join(root, "home")

	require.NoError(t, os.MkdirAll(repo, 0o750))
	require.NoError(t, os.MkdirAll(home, 0o750))

	return repo, home
}

// requireNoKey fails the test when keygen left anything in repo.
func requireNoKey(t *testing.T, repo string) {
	t.Helper()

	entries, err := os.ReadDir(repo)
	require.NoError(t, err)
	assert.Empty(t, entries, "nothing is written inside the repository")
}

func TestKeygen(t *testing.T) {
	t.Parallel()

	t.Run("writes the private key and prints the public key", func(t *testing.T) {
		t.Parallel()

		repo, home := keygenRepo(t)
		out := filepath.Join(t.TempDir(), "keys")

		var err error

		g := newRig(t)
		err = g.runner().keygen(out, repo, home)
		stdout := g.out.String()

		require.NoError(t, err)

		path := filepath.Join(out, signingKeyFile)
		info, err := os.Stat(path)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "readable by its owner alone")

		dirInfo, err := os.Stat(out)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o700), dirInfo.Mode().Perm())

		private, err := update.ParsePrivateKey([]byte(readFile(t, path)))
		require.NoError(t, err)

		lines := strings.Split(strings.TrimSpace(stdout), "\n")
		assert.Equal(t, update.EncodeKey(update.PublicKeyOf(private)), lines[len(lines)-1], "the public key, last, alone")
		assert.NotContains(t, stdout, "PRIVATE KEY")
		assert.NotContains(t, stdout, strings.Split(readFile(t, path), "\n")[1], "nothing of the private key")
	})

	t.Run("expands ~ from the home folder", func(t *testing.T) {
		t.Parallel()

		repo, home := keygenRepo(t)

		require.NoError(t, newRig(t).runner().keygen("~/keys", repo, home))

		assert.FileExists(t, filepath.Join(home, "keys", signingKeyFile))
	})

	t.Run("reads a relative folder from the working folder", func(t *testing.T) {
		t.Parallel()

		repo, _ := keygenRepo(t)

		require.ErrorIs(t, newRig(t).runner().keygen("keys", repo, ""), errInRepository)
		require.NoError(t, newRig(t).runner().keygen("../outside", repo, ""))

		assert.FileExists(t, filepath.Join(filepath.Dir(repo), "outside", signingKeyFile))
	})

	t.Run("never overwrites a key", func(t *testing.T) {
		t.Parallel()

		repo, home := keygenRepo(t)
		out := t.TempDir()
		path := filepath.Join(out, signingKeyFile)
		testutil.WriteFile(t, path, "the key kept")

		err := newRig(t).runner().keygen(out, repo, home)

		require.ErrorIs(t, err, fs.ErrExist)
		assert.Equal(t, "the key kept", readFile(t, path))
	})

	t.Run("refuses a folder inside the repository", func(t *testing.T) {
		t.Parallel()

		repo, home := keygenRepo(t)
		link := filepath.Join(t.TempDir(), "into-repo")
		require.NoError(t, os.Symlink(repo, link))

		for _, out := range []string{
			repo,
			filepath.Join(repo, "keys"),
			filepath.Join(repo, "not", "made", "yet"),
			filepath.Join(repo, "keys", "..", "other"),
			filepath.Join(link, "keys"),
		} {
			require.ErrorIs(t, newRig(t).runner().keygen(out, repo, home), errInRepository, out)
		}

		requireNoKey(t, repo)
	})

	t.Run("refuses the repository by another case of its name", func(t *testing.T) {
		t.Parallel()

		repo, home := keygenRepo(t)
		upper := filepath.Join(filepath.Dir(repo), strings.ToUpper(filepath.Base(repo)))

		if _, err := os.Stat(upper); err != nil {
			t.Skip("this file system tells names apart by case, so the other case is another folder")
		}

		require.ErrorIs(t, newRig(t).runner().keygen(filepath.Join(upper, "keys"), repo, home), errInRepository)
		requireNoKey(t, repo)
	})

	t.Run("refuses the repository through a firmlink", func(t *testing.T) {
		t.Parallel()

		if runtime.GOOS != "darwin" {
			t.Skip("firmlinks are macOS's")
		}

		repo, home := keygenRepo(t)
		data := filepath.Join("/System/Volumes/Data", paths.Resolved(repo))

		if _, err := os.Stat(data); err != nil {
			t.Skipf("the data volume doesn't show the repository at %s", data)
		}

		require.ErrorIs(t, newRig(t).runner().keygen(filepath.Join(data, "keys"), repo, home), errInRepository)
		requireNoKey(t, repo)
	})

	t.Run("refuses no folder", func(t *testing.T) {
		t.Parallel()

		repo, home := keygenRepo(t)

		require.ErrorIs(t, newRig(t).runner().keygen("", repo, home), errNoKeyFolder)
	})

	t.Run("refuses ~ without a home folder", func(t *testing.T) {
		t.Parallel()

		repo, _ := keygenRepo(t)

		require.ErrorIs(t, newRig(t).runner().keygen("~/keys", repo, ""), paths.ErrNoHome)
	})

	t.Run("a folder that can't be made", func(t *testing.T) {
		t.Parallel()

		repo, home := keygenRepo(t)
		file := filepath.Join(t.TempDir(), "file")
		testutil.WriteFile(t, file, "")

		require.ErrorIs(t, newRig(t).runner().keygen(filepath.Join(file, "keys"), repo, home), syscall.ENOTDIR)
	})

	t.Run("a repository that can't be read", func(t *testing.T) {
		t.Parallel()

		_, home := keygenRepo(t)

		err := newRig(t).runner().keygen(t.TempDir(), filepath.Join(t.TempDir(), "missing"), home)

		require.ErrorIs(t, err, fs.ErrNotExist)
	})
}

func TestAppKeygen(t *testing.T) {
	t.Parallel()

	g := newRig(t)
	home := t.TempDir()
	r := g.runner()
	homeDir := func() (string, error) { return home, nil }

	g.setenv(mageArgsJSON, `["out=`+filepath.Join(g.dir, "keys")+`"]`)
	require.ErrorIs(t, r.keygenFromArgs(homeDir), errInRepository, "the checkout is the repository")

	g.setenv(mageArgsJSON, `["out=~/keys"]`)
	require.NoError(t, r.keygenFromArgs(homeDir))
	assert.FileExists(t, filepath.Join(home, "keys", signingKeyFile))
}

func TestAppKeygenNeedsAHomeFolder(t *testing.T) {
	t.Parallel()

	g := newRig(t)
	g.setenv(mageArgsJSON, `["out=`+t.TempDir()+`"]`)

	err := g.runner().keygenFromArgs(func() (string, error) { return "", paths.ErrNoHome })

	require.ErrorIs(t, err, paths.ErrNoHome)
	assert.Contains(t, err.Error(), "find the home folder")
	assert.Empty(t, g.out.String(), "no key is made")
}

func TestInsideFolder(t *testing.T) {
	t.Parallel()

	root := testutil.RealTempDir(t)
	sibling := root + "-other"
	require.NoError(t, os.MkdirAll(sibling, 0o750))

	for path, want := range map[string]bool{
		root:                                  true,
		filepath.Join(root, "a"):              true,
		filepath.Join(root, "a", "b"):         true,
		filepath.Join(root, "..a-not-parent"): true,
		filepath.Dir(root):                    false,
		sibling:                               false,
		filepath.Join(sibling, "keys"):        false,
	} {
		got, err := insideFolder(path, root)

		require.NoError(t, err)
		assert.Equal(t, want, got, path)
	}
}
