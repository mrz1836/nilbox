package localrepos

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/paths"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/uierr"
)

var (
	errTestInvalid = errors.New("invalid test request")
	errTestNoClone = errors.New("not a clone under the clone root")
)

// fakeRoot is a clone root of fixed folders, whose reads fail with rootErr and dirsErr,
// and whose clones' origins are origins (a clone missing there isn't on GitHub).
type fakeRoot struct {
	root      string
	dirs      []string
	origins   map[string]repo.Name
	rootErr   error
	dirsErr   error
	originErr error
	asked     []string
}

func (f *fakeRoot) Root(context.Context) (string, error) {
	return f.root, f.rootErr
}

func (f *fakeRoot) Dirs(_ context.Context, root string) ([]string, error) {
	if root != f.root {
		return nil, os.ErrNotExist
	}

	return f.dirs, f.dirsErr
}

func (f *fakeRoot) Origin(_ context.Context, dir string) (repo.Name, bool, error) {
	f.asked = append(f.asked, dir)
	if f.originErr != nil {
		return exampleRepo(), true, f.originErr
	}

	name, ok := f.origins[dir]

	return name, ok, nil
}

// byFolderRig is a home with a clone root, ~/projects, holding the clones api and web,
// web's origin on GitHub.
func byFolderRig(t *testing.T) (ByFolder, *fakeRoot) {
	t.Helper()

	home := t.TempDir()
	root := filepath.Join(home, "projects")
	api, web := filepath.Join(root, "api"), filepath.Join(root, "web")

	for _, dir := range []string{api, web} {
		require.NoError(t, os.MkdirAll(dir, 0o700))
	}

	f := &fakeRoot{root: root, dirs: []string{api, web}, origins: map[string]repo.Name{web: exampleRepo()}}

	return ByFolder{Clones: f, Home: home, Invalid: errTestInvalid, NoClone: errTestNoClone}, f
}

func TestByFolderList(t *testing.T) {
	t.Parallel()

	b, f := byFolderRig(t)

	l, err := b.List(t.Context())
	require.NoError(t, err)
	assert.Equal(t, Listing{Root: f.root, Dirs: f.dirs}, l)

	f.dirsErr = os.ErrPermission
	_, err = b.List(t.Context())
	require.ErrorIs(t, err, os.ErrPermission)
	require.EqualError(t, err, "list the clones: "+os.ErrPermission.Error())

	_, err = b.At(t.Context(), "~/projects/api")
	require.ErrorIs(t, err, os.ErrPermission, "At lists the clones first")

	f.rootErr = os.ErrDeadlineExceeded
	_, err = b.List(t.Context())
	require.ErrorIs(t, err, os.ErrDeadlineExceeded)
	require.EqualError(t, err, "read the clone root: "+os.ErrDeadlineExceeded.Error())
}

func TestByFolderPicksAListedClone(t *testing.T) {
	t.Parallel()

	b, f := byFolderRig(t)
	api, web := f.dirs[0], f.dirs[1]

	got, err := b.At(t.Context(), "~/projects/web")
	require.NoError(t, err)
	assert.Equal(t, Picked{Dir: web, Name: exampleRepo()}, got, "its origin's repository")

	got, err = b.At(t.Context(), " "+api+"/ ")
	require.NoError(t, err)
	assert.Equal(t, Picked{Dir: api}, got, "an absolute folder, cleaned; an origin off GitHub names nothing")

	link := filepath.Join(t.TempDir(), "linked")
	require.NoError(t, os.Symlink(web, link))

	got, err = b.At(t.Context(), link)
	require.NoError(t, err)
	assert.Equal(t, Picked{Dir: web, Name: exampleRepo()}, got, "the same folder through a link is the listed one")
}

func TestByFolderAsksKnownFirst(t *testing.T) {
	t.Parallel()

	b, f := byFolderRig(t)
	api, web := f.dirs[0], f.dirs[1]
	other := repo.Name{Owner: exampleOwner, Repo: "other"}

	b.Known = func(dir string) (repo.Name, bool) {
		switch dir {
		case api:
			return other, true
		case web:
			return repo.Name{}, true
		}

		return repo.Name{}, false
	}

	l, err := b.List(t.Context())
	require.NoError(t, err)

	got, err := b.Pick(t.Context(), "~/projects/api", l)
	require.NoError(t, err)
	assert.Equal(t, Picked{Dir: api, Name: other}, got)

	got, err = b.Pick(t.Context(), "~/projects/web", l)
	require.NoError(t, err)
	assert.Equal(t, Picked{Dir: web}, got, "what Known holds wins, an origin off GitHub included")
	assert.Empty(t, f.asked, "no origin is read for a clone Known holds")

	b.Known = func(string) (repo.Name, bool) { return other, false }

	got, err = b.Pick(t.Context(), "~/projects/web", l)
	require.NoError(t, err)
	assert.Equal(t, Picked{Dir: web, Name: exampleRepo()}, got, "a clone Known doesn't hold has its origin read")
	assert.Equal(t, []string{web}, f.asked)
}

func TestByFolderOriginThatCantBeRead(t *testing.T) {
	t.Parallel()

	b, f := byFolderRig(t)
	f.originErr = os.ErrPermission

	got, err := b.At(t.Context(), "~/projects/web")
	require.NoError(t, err, "an origin that can't be read doesn't refuse the clone")
	assert.Equal(t, Picked{Dir: f.dirs[1]}, got, "and names no repository, whatever it said")
}

func TestByFolderRefuses(t *testing.T) {
	t.Parallel()

	b, f := byFolderRig(t)

	_, err := b.At(t.Context(), "~/elsewhere")
	require.ErrorIs(t, err, errTestNoClone)
	assert.Equal(t, "~/elsewhere isn't a clone in ~/projects.", uierr.Message(err))
	require.EqualError(t, err, errTestNoClone.Error()+": "+filepath.Join(b.Home, "elsewhere"))

	_, err = b.At(t.Context(), "~/projects")
	require.ErrorIs(t, err, errTestNoClone, "the clone root itself isn't a clone")

	outside := t.TempDir()
	_, err = b.Pick(t.Context(), outside, Listing{Root: f.root, Dirs: f.dirs})
	require.ErrorIs(t, err, errTestNoClone)
	assert.Equal(t, outside+" isn't a clone in ~/projects.", uierr.Message(err), "a folder outside home keeps its path")

	for _, shown := range []string{"relative/dir", "", "~other/api"} {
		_, err = b.At(t.Context(), shown)
		require.ErrorIs(t, err, errTestInvalid, "%q", shown)
		require.NotErrorIs(t, err, errTestNoClone, "%q", shown)
	}

	_, err = b.At(t.Context(), "relative/dir")
	require.ErrorIs(t, err, paths.ErrRelative)
	require.EqualError(t, err, `invalid test request: the folder "relative/dir": `+paths.ErrRelative.Error()+": relative/dir")

	b.Home = ""
	_, err = b.At(t.Context(), "~/projects/api")
	require.ErrorIs(t, err, paths.ErrNoHome)
	require.ErrorIs(t, err, errTestInvalid)
}
