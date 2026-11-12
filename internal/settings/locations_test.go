package settings

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// locationsRig is a Service over a home holding the clone root, ~/projects, with the
// clone api; a folder location to be, ~/work, with the clones web and cli; a single clone,
// ~/Desktop/exp; and a folder of owners, ~/src.
type locationsRig struct {
	svc  *Service
	db   *sql.DB
	home string
}

func newLocationsRig(t *testing.T) locationsRig {
	t.Helper()

	svc, db, home := newTestService(t)
	svc.reader = localrepos.NewReader(localrepos.DefaultReadLimit) // read as the app reads them

	for _, dir := range []string{
		"projects/api", "work/web", "work/cli", "Desktop/exp", "src/acme-org/one", "src/tools/two",
	} {
		require.NoError(t, os.MkdirAll(filepath.Join(home, dir, ".git"), 0o700))
	}

	return locationsRig{svc: svc, db: db, home: home}
}

// add adds dirs and returns the locations stored.
func (r locationsRig) add(t *testing.T, dirs ...string) []localrepos.Location {
	t.Helper()

	got, err := r.svc.AddCloneLocations(t.Context(), dirs)
	require.NoError(t, err)

	return got.CloneLocations
}

func TestAddCloneLocations(t *testing.T) {
	t.Parallel()

	r := newLocationsRig(t)

	got := r.add(t, " ~/Desktop/exp/ ", filepath.Join(r.home, "src", "acme-org"))
	assert.Equal(t, []localrepos.Location{
		{Path: "~/Desktop/exp", Kind: localrepos.KindClone},
		{Path: "~/src/acme-org", Kind: localrepos.KindFolder},
	}, got, "trimmed, stored with ~ however typed, and classified")

	got = r.add(t, "~/src/tools", "~/work")
	assert.Len(t, got, 4, "several at once")

	stored, err := r.svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, got, stored.CloneLocations)
}

func TestAddCloneLocationsRefuses(t *testing.T) {
	t.Parallel()

	r := newLocationsRig(t)
	r.add(t, "~/work")

	testutil.WriteFile(t, filepath.Join(r.home, "notes.txt"), "x")
	require.NoError(t, os.MkdirAll(filepath.Join(r.home, ".hidden"), 0o700))

	main := filepath.Join(r.home, "Desktop", "exp")
	admin := filepath.Join(main, ".git", "worktrees", "exp-wt")
	testutil.WriteFile(t, filepath.Join(admin, "commondir"), "../..\n")
	testutil.WriteFile(t, filepath.Join(r.home, "exp-wt", ".git"), "gitdir: "+admin+"\n")

	tests := []struct {
		dir, want string
	}{
		{dir: " ", want: "Choose a folder, or type its path."},
		{dir: "projects", want: "Type the folder's full path, or one starting with ~/."},
		{dir: "~/missing", want: "~/missing doesn't exist."},
		{dir: "~/notes.txt", want: "~/notes.txt is a file, not a folder."},
		{dir: "/", want: "/ is the whole disk; choose a folder inside it."},
		{dir: "~/.hidden", want: "~/.hidden is hidden, and the app never looks in hidden folders."},
		{dir: "~/exp-wt", want: "~/exp-wt is a linked worktree of ~/Desktop/exp; add ~/Desktop/exp instead."},
		{dir: "~/projects", want: "~/projects is your clone root."},
		{dir: "~/projects/api", want: "~/projects/api is already in ~/projects."},
		{dir: "~/work/web", want: "~/work/web is already in ~/work."},
	}

	for _, tt := range tests {
		_, err := r.svc.AddCloneLocations(t.Context(), []string{tt.dir})
		require.ErrorIs(t, err, ErrInvalidCloneLocation, "%q", tt.dir)
		assert.Equal(t, tt.want, uierr.Message(err), "%q", tt.dir)
	}

	_, err := r.svc.AddCloneLocations(t.Context(), nil)
	require.ErrorIs(t, err, ErrInvalidCloneLocation)

	_, err = r.svc.AddCloneLocations(t.Context(), []string{"~/Desktop/exp", "~/projects/api"})
	require.ErrorIs(t, err, ErrInvalidCloneLocation)

	stored, err := r.svc.Get(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []localrepos.Location{{Path: "~/work", Kind: localrepos.KindFolder}}, stored.CloneLocations,
		"all or none: the clone before the refused one isn't added")
}

func TestAddCloneLocationsClassifiesAListedOneAgain(t *testing.T) {
	t.Parallel()

	r := newLocationsRig(t)
	r.add(t, "~/Desktop/exp")

	require.NoError(t, os.RemoveAll(filepath.Join(r.home, "Desktop", "exp", ".git")))

	got := r.add(t, filepath.Join(r.home, "Desktop", "exp"))
	assert.Equal(t, []localrepos.Location{{Path: "~/Desktop/exp", Kind: localrepos.KindFolder}}, got,
		"Check again: a clone location that's no longer one is a folder now, listed once")
}

func TestAddCloneLocationsDropsTheClonesAFolderCovers(t *testing.T) {
	t.Parallel()

	r := newLocationsRig(t)
	r.add(t, "~/work/web", "~/Desktop/exp")

	got := r.add(t, "~/work")
	assert.Equal(t, []localrepos.Location{
		{Path: "~/Desktop/exp", Kind: localrepos.KindClone},
		{Path: "~/work", Kind: localrepos.KindFolder},
	}, got, "~/work/web is inside ~/work now")
}

func TestRemoveCloneLocation(t *testing.T) {
	t.Parallel()

	r := newLocationsRig(t)
	r.add(t, "~/work", "~/Desktop/exp")

	got, err := r.svc.RemoveCloneLocation(t.Context(), filepath.Join(r.home, "work")+"/")
	require.NoError(t, err)
	assert.Equal(t, []localrepos.Location{{Path: "~/Desktop/exp", Kind: localrepos.KindClone}}, got.CloneLocations,
		"matched as the same folder")

	got, err = r.svc.RemoveCloneLocation(t.Context(), "~/work")
	require.NoError(t, err, "removing one that isn't listed is no error")
	assert.Len(t, got.CloneLocations, 1)

	setPref(t, r.db, KeyCloneLocations, `[{"path":"~/gone","kind":"folder"},{"path":"~/Desktop/exp","kind":"clone"}]`)

	got, err = r.svc.RemoveCloneLocation(t.Context(), "~/gone")
	require.NoError(t, err)
	assert.Equal(t, []localrepos.Location{{Path: "~/Desktop/exp", Kind: localrepos.KindClone}}, got.CloneLocations,
		"a location that's gone is removed by its text")
}

func TestPreviewAndCheckCloneLocations(t *testing.T) {
	t.Parallel()

	r := newLocationsRig(t)
	r.add(t, "~/work/web")
	r.svc.git = &testutil.Runner{Stdout: "remote.origin.url https://github.com/acme-org/example-repo.git\n"}

	preview, err := r.svc.PreviewCloneLocation(t.Context(), "~/work")
	require.NoError(t, err)
	assert.Equal(t, localrepos.Preview{
		Path: "~/work", Kind: localrepos.KindFolder, Clones: 2, Inside: []localrepos.Suggestion{},
		Replaces: []string{"~/work/web"},
	}, preview)

	preview, err = r.svc.PreviewCloneLocation(t.Context(), "~/src")
	require.NoError(t, err)
	assert.Equal(t, []localrepos.Suggestion{{Path: "~/src/acme-org", Clones: 1}, {Path: "~/src/tools", Clones: 1}},
		preview.Inside)

	_, err = r.svc.PreviewCloneLocation(t.Context(), "relative")
	require.ErrorIs(t, err, ErrInvalidCloneLocation)

	_, err = r.svc.PreviewCloneLocation(t.Context(), "/")
	require.ErrorIs(t, err, localrepos.ErrLocationRefused)
	assert.Equal(t, "/ is the whole disk; choose a folder inside it.", uierr.Message(err), "the preview refuses / too")

	states, err := r.svc.CheckCloneLocations(t.Context())
	require.NoError(t, err)
	assert.Equal(t, []localrepos.LocationState{
		{Path: "~/projects", Kind: localrepos.KindFolder, Primary: true, State: localrepos.StateOK, Clones: 1},
		{Path: "~/work/web", Kind: localrepos.KindClone, State: localrepos.StateOK, Repo: "acme-org/example-repo"},
	}, states)
}

func TestWithGitReadsAClonesOrigin(t *testing.T) {
	t.Parallel()

	svc, db, home := newTestService(t)
	require.NoError(t, os.MkdirAll(filepath.Join(home, "projects"), 0o700))
	require.NoError(t, os.MkdirAll(filepath.Join(home, "exp", ".git"), 0o700))

	git := &testutil.Runner{Stdout: "remote.origin.url git@github.com:acme-org/example-repo.git\n"}
	withGit := serviceOver(t, db, home, WithGit(git))

	preview, err := withGit.PreviewCloneLocation(t.Context(), "~/exp")
	require.NoError(t, err)
	assert.Equal(t, "acme-org/example-repo", preview.Repo)

	preview, err = svc.PreviewCloneLocation(t.Context(), "~/exp")
	require.NoError(t, err)
	assert.Empty(t, preview.Repo, "no git, no name")
}

func TestDecodeCloneLocationsIsLenient(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw  string
		want []localrepos.Location
	}{
		{raw: "", want: []localrepos.Location{}},
		{raw: "not json", want: []localrepos.Location{}},
		{raw: `{"path":"~/work"}`, want: []localrepos.Location{}},
		{
			raw: `[{"path":" ~/work/ ","kind":"folder"},{"path":"relative","kind":"folder"},` +
				`{"path":"~/odd","kind":"bucket"},{"path":"/srv/exp","kind":"clone"},{"path":"~","kind":"folder"}]`,
			want: []localrepos.Location{
				{Path: "~/work", Kind: localrepos.KindFolder},
				{Path: "/srv/exp", Kind: localrepos.KindClone},
				{Path: "~", Kind: localrepos.KindFolder},
			},
		},
	}

	for _, tt := range tests {
		assert.Equal(t, tt.want, decodeCloneLocations(tt.raw), "%q", tt.raw)
	}
}
