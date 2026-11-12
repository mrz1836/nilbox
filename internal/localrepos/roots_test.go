package localrepos

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// workLocation is a folder location, as stored.
const workLocation = "~/work"

// fakeClone makes a folder at dir that reads as a clone: it holds a .git folder.
func fakeClone(t *testing.T, dir string) string {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Join(dir, ".git"), 0o700))

	return dir
}

// mkdir makes the folder at dir and returns it.
func mkdir(t *testing.T, dir string) string {
	t.Helper()

	require.NoError(t, os.MkdirAll(dir, 0o700))

	return dir
}

func TestNewRootsDropsWhatItCantUse(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	projects := mkdir(t, filepath.Join(home, "projects"))
	work := mkdir(t, filepath.Join(home, "work"))
	require.NoError(t, os.Symlink(projects, filepath.Join(home, "projects-link")))

	roots := rootsAt(t, "~/projects", home,
		Location{Path: workLocation, Kind: KindFolder},
		Location{Path: "relative/dir", Kind: KindFolder},
		Location{Path: "~/odd", Kind: "bucket"},
		Location{Path: projects, Kind: KindFolder},
		Location{Path: "~/projects-link", Kind: KindFolder},
		Location{Path: work + "/", Kind: KindFolder},
		Location{Path: "~/Desktop/exp", Kind: KindClone},
	)

	assert.Equal(t, []Place{
		{Path: projects, Kind: KindFolder, Primary: true},
		{Path: work, Kind: KindFolder},
		{Path: filepath.Join(home, "Desktop", "exp"), Kind: KindClone},
	}, roots.Places(), "a bad path, an unknown kind, the clone root again, and a repeat are dropped")
	assert.True(t, roots.Several())
	assert.Equal(t, projects, roots.Primary())
	assert.Equal(t, []string{"~/projects", workLocation, "~/Desktop/exp"}, roots.Shown(home))
	assert.Equal(t, "~/projects or your other repository locations", roots.Where(home))
	assert.Equal(t, projects+"\nfolder:"+work+"\nclone:"+filepath.Join(home, "Desktop", "exp"), roots.Key())
	assert.True(t, roots.CloneAt(filepath.Join(home, "Desktop", "exp")+"/"))
	assert.False(t, roots.CloneAt(work))

	alone := rootsAt(t, "~/projects", home)
	assert.False(t, alone.Several())
	assert.Equal(t, projects, alone.Key(), "the clone root alone keys a cache as it always did")
	assert.Equal(t, "~/projects", alone.Where(home))
	assert.True(t, alone.Equal(rootsAt(t, projects, home)))
	assert.False(t, alone.Equal(roots))

	_, err := NewRoots("relative", nil, home)
	require.Error(t, err, "only the clone root can fail")

	var zero Roots
	assert.Empty(t, zero.Primary())
	assert.Empty(t, zero.Key())
	assert.NotNil(t, zero.Shown(home))
	assert.False(t, zero.Holds(filepath.Join(projects, "x")))
}

func TestRootsHolds(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	projects := mkdir(t, filepath.Join(home, "projects"))
	work := mkdir(t, filepath.Join(home, "work"))
	exp := fakeClone(t, filepath.Join(home, "Desktop", "exp"))
	require.NoError(t, os.Symlink(projects, filepath.Join(home, "projects-link")))
	require.NoError(t, os.Symlink(exp, filepath.Join(home, "exp-link")))

	roots := rootsAt(t, "~/projects", home,
		Location{Path: workLocation, Kind: KindFolder}, Location{Path: "~/Desktop/exp", Kind: KindClone})

	tests := []struct {
		dir  string
		want bool
	}{
		{dir: filepath.Join(projects, "api"), want: true},
		{dir: filepath.Join(projects, "api") + "/", want: true},
		{dir: filepath.Join(home, "projects-link", "api"), want: true},
		{dir: filepath.Join(work, "web"), want: true},
		{dir: exp, want: true},
		{dir: filepath.Join(home, "exp-link"), want: true},
		{dir: filepath.Join(projects, ".hidden")},
		{dir: filepath.Join(projects, "group", "nested")},
		{dir: projects},
		{dir: work},
		{dir: filepath.Join(exp, "inner")},
		{dir: filepath.Join(home, "Desktop", "other")},
		{dir: "projects/api"},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, roots.Holds(tt.dir), "%s", tt.dir)
	}
}

func TestRootsClones(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	projects := filepath.Join(home, "projects")
	api, zed := fakeClone(t, filepath.Join(projects, "api")), fakeClone(t, filepath.Join(projects, "zed"))
	web := fakeClone(t, filepath.Join(home, "work", "web"))
	fakeClone(t, filepath.Join(home, "work", ".hidden"))
	exp := fakeClone(t, filepath.Join(home, "Desktop", "exp"))
	tools := filepath.Join(home, "src", "tools")
	cli := fakeClone(t, filepath.Join(tools, "cli"))
	fakeClone(t, tools) // a folder location someone ran git init in: still looked into
	mkdir(t, filepath.Join(home, "was-a-clone"))
	require.NoError(t, os.Symlink(api, filepath.Join(home, "work", "api-link")))

	roots := rootsAt(t, "~/projects", home,
		Location{Path: "~/Desktop/exp", Kind: KindClone},
		Location{Path: workLocation, Kind: KindFolder},
		Location{Path: "~/gone", Kind: KindFolder},
		Location{Path: "~/was-a-clone", Kind: KindClone},
		Location{Path: "~/src/tools", Kind: KindFolder},
		Location{Path: "~/projects/api", Kind: KindClone},
	)

	found, err := roots.Clones(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []string{api, zed, exp, web, cli}, found.Dirs,
		"the clone root's by name, then each location in order; a clone reached twice once, where first reached")
	assert.Equal(t, []Unread{
		{Dir: filepath.Join(home, "gone"), State: StateMissing},
		{Dir: filepath.Join(home, "was-a-clone"), State: StateNotAClone},
	}, found.Skipped)
	assert.Equal(t, "~/gone wasn't found", found.Skipped[0].Words(home))
	assert.Equal(t, "~/was-a-clone is no longer a clone", found.Skipped[1].Words(home))
	assert.Equal(t, "~/x couldn't be read", Unread{Dir: filepath.Join(home, "x"), State: StateUnreadable}.Words(home))
	assert.Equal(t, "~/x isn't a folder", Unread{Dir: filepath.Join(home, "x"), State: StateNotFolder}.Words(home))

	alone, err := rootsAt(t, "~/projects", home).Clones(t.Context())
	require.NoError(t, err)
	assert.Equal(t, Found{Dirs: []string{api, zed}, Skipped: []Unread{}}, alone)

	_, err = rootsAt(t, "~/missing", home, Location{Path: workLocation, Kind: KindFolder}).Clones(t.Context())
	require.ErrorIs(t, err, fs.ErrNotExist, "a clone root that can't be read is an error")

	empty, err := rootsAt(t, mkdir(t, filepath.Join(home, "empty")), home).Clones(t.Context())
	require.NoError(t, err)
	assert.NotNil(t, empty.Dirs)
}

func TestRootsClonesNamesAFileLocation(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	mkdir(t, filepath.Join(home, "projects"))
	testutil.WriteFile(t, filepath.Join(home, "notes"), "x")
	testutil.WriteFile(t, filepath.Join(home, "one"), "x")

	found, err := rootsAt(t, "~/projects", home,
		Location{Path: "~/notes", Kind: KindFolder}, Location{Path: "~/one", Kind: KindClone}).Clones(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []Unread{
		{Dir: filepath.Join(home, "notes"), State: StateNotFolder},
		{Dir: filepath.Join(home, "one"), State: StateNotFolder},
	}, found.Skipped)
}

func TestResolveAcrossLocations(t *testing.T) {
	t.Parallel()

	git := isolatedGit(t)

	home := t.TempDir()
	mkdir(t, filepath.Join(home, "projects"))
	initRepo(t, filepath.Join(home, "projects", "fork"), "origin", "git@github.com:octo-user/example-repo.git",
		"upstream", exampleHTTPS)
	initRepo(t, filepath.Join(home, "work", "example-repo"), "origin", exampleHTTPS)
	initRepo(t, filepath.Join(home, "Desktop", "other"), "origin", "https://github.com/acme-org/other-repo")

	r := Resolver{Roots: rootsAt(t, "~/projects", home,
		Location{Path: workLocation, Kind: KindFolder},
		Location{Path: "~/Desktop/other", Kind: KindClone},
		Location{Path: "~/gone", Kind: KindFolder},
	), Home: home, Git: git}

	got, err := r.Resolve(t.Context(), exampleRepo())
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "work", "example-repo"), got.Repo.Path,
		"a clone whose origin is the repository wins over the clone root's fork")

	got, err = r.Resolve(t.Context(), repo.Name{Owner: exampleOwner, Repo: "other-repo"})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "Desktop", "other"), got.Repo.Path, "a clone location resolves")

	_, err = r.Resolve(t.Context(), repo.Name{Owner: exampleOwner, Repo: "missing-repo"})
	require.ErrorIs(t, err, ErrCloneNotFound)
	assert.Equal(t, "no local clone of acme-org/missing-repo in ~/projects or your other repository locations; "+
		"~/gone wasn't found", err.Error())
}

func TestResolveTiesGoToTheCloneRoot(t *testing.T) {
	t.Parallel()

	git := isolatedGit(t)

	home := t.TempDir()
	initRepo(t, filepath.Join(home, "work", "a-copy"), "origin", exampleHTTPS)
	initRepo(t, filepath.Join(home, "projects", "z-copy"), "origin", exampleHTTPS)

	r := Resolver{Roots: rootsAt(t, "~/projects", home, Location{Path: workLocation, Kind: KindFolder}), Home: home, Git: git}

	got, err := r.Resolve(t.Context(), exampleRepo())
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, "projects", "z-copy"), got.Repo.Path)
}
