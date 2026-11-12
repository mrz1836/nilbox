package localrepos

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

func TestSurveyFolder(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	mkdir(t, filepath.Join(home, "projects"))
	work := filepath.Join(home, "work")
	fakeClone(t, filepath.Join(work, "api"))
	fakeClone(t, filepath.Join(work, "web"))
	fakeClone(t, filepath.Join(work, ".hidden"))

	roots := rootsAt(t, "~/projects", home,
		Location{Path: "~/work/web", Kind: KindClone}, Location{Path: "~/elsewhere/one", Kind: KindClone})

	got, err := Survey(t.Context(), work+"/", roots, home, nil)
	require.NoError(t, err)
	assert.Equal(t, Preview{
		Path: workLocation, Kind: KindFolder, Clones: 2, Inside: []Suggestion{}, Replaces: []string{"~/work/web"},
	}, got, "a folder counts its clones and replaces the clone locations directly inside it")
}

func TestSurveySuggestsTheFoldersInside(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	mkdir(t, filepath.Join(home, "projects"))
	src := filepath.Join(home, "src")

	for i := range 4 {
		fakeClone(t, filepath.Join(src, "acme-org", fmt.Sprintf("r%d", i)))
	}

	fakeClone(t, filepath.Join(src, "tools", "a"))
	fakeClone(t, filepath.Join(src, "tools", "b"))
	fakeClone(t, filepath.Join(src, ".hidden", "a"))
	fakeClone(t, filepath.Join(src, "listed", "a"))
	mkdir(t, filepath.Join(src, "empty"))

	roots := rootsAt(t, "~/projects", home, Location{Path: "~/src/listed", Kind: KindFolder})

	got, err := Survey(t.Context(), src, roots, home, nil)
	require.NoError(t, err)
	assert.Zero(t, got.Clones)
	assert.Equal(t, []Suggestion{{Path: "~/src/acme-org", Clones: 4}, {Path: "~/src/tools", Clones: 2}}, got.Inside,
		"the most clones first; hidden, empty, and listed folders aren't suggested")
	assert.False(t, got.MoreInside)
	assert.False(t, got.Partial)
}

func TestSurveyBoundsItsLookInside(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	mkdir(t, filepath.Join(home, "projects"))
	roots := rootsAt(t, "~/projects", home)

	many := filepath.Join(home, "many")
	for i := range insideKept + 2 {
		fakeClone(t, filepath.Join(many, fmt.Sprintf("owner-%02d", i), "repo"))
	}

	got, err := Survey(t.Context(), many, roots, home, nil)
	require.NoError(t, err)
	assert.Len(t, got.Inside, insideKept)
	assert.True(t, got.MoreInside)
	assert.False(t, got.Partial)
	assert.Equal(t, "~/many/owner-00", got.Inside[0].Path, "ties go by name")

	wide := filepath.Join(home, "wide")
	for i := range insideFolders + 1 {
		mkdir(t, filepath.Join(wide, fmt.Sprintf("d%03d", i)))
	}

	got, err = Survey(t.Context(), wide, roots, home, nil)
	require.NoError(t, err)
	assert.True(t, got.Partial, "no more than %d subfolders are looked into", insideFolders)
	assert.Empty(t, got.Inside)

	full := filepath.Join(home, "full", "owner")
	for i := range 3 {
		fakeClone(t, filepath.Join(full, fmt.Sprintf("r%d", i)))
	}

	count, read, complete := countClones(full, 2)
	assert.Equal(t, 2, read)
	assert.False(t, complete, "the budget ran out before the folder did")
	assert.LessOrEqual(t, count, 2)

	count, read, complete = countClones(full, 3)
	assert.Equal(t, 3, count)
	assert.Equal(t, 3, read)
	assert.True(t, complete)
}

func TestSurveyClone(t *testing.T) {
	t.Parallel()

	git := isolatedGit(t)

	home := t.TempDir()
	mkdir(t, filepath.Join(home, "projects"))
	exp := filepath.Join(home, "Desktop", "exp")
	initRepo(t, exp, "origin", exampleHTTPS)
	fakeClone(t, filepath.Join(home, "Desktop", "other"))
	fakeClone(t, filepath.Join(home, "Desktop", "third"))

	roots := rootsAt(t, "~/projects", home)

	got, err := Survey(t.Context(), exp, roots, home, git)
	require.NoError(t, err)
	assert.Equal(t, Preview{
		Path: "~/Desktop/exp", Kind: KindClone, Repo: "acme-org/example-repo", Inside: []Suggestion{}, Replaces: []string{},
		Parent: Suggestion{Path: "~/Desktop", Clones: 2},
	}, got, "a clone whose folder holds other clones suggests the folder")

	alone := fakeClone(t, filepath.Join(home, "solo", "only"))
	got, err = Survey(t.Context(), alone, roots, home, nil)
	require.NoError(t, err)
	assert.Equal(t, Suggestion{}, got.Parent, "a folder with no other clones isn't suggested")
	assert.Empty(t, got.Repo, "no git, no name")
}

func TestSurveyCovered(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	projects := mkdir(t, filepath.Join(home, "projects"))
	api := fakeClone(t, filepath.Join(projects, "api"))
	work := mkdir(t, filepath.Join(home, "work"))
	web := fakeClone(t, filepath.Join(work, "web"))
	group := mkdir(t, filepath.Join(projects, "group"))

	roots := rootsAt(t, "~/projects", home, Location{Path: workLocation, Kind: KindFolder})

	tests := []struct {
		dir, covered, by string
	}{
		{dir: projects, covered: "~/projects is your clone root.", by: "~/projects"},
		{dir: work, covered: "~/work is already one of your repository locations.", by: workLocation},
		{dir: api, covered: "~/projects/api is already in ~/projects.", by: "~/projects"},
		{dir: web, covered: "~/work/web is already in ~/work.", by: workLocation},
	}

	for _, tt := range tests {
		got, err := Survey(t.Context(), tt.dir, roots, home, nil)
		require.NoError(t, err)
		assert.Equal(t, tt.covered, got.Covered, "%s", tt.dir)
		assert.Equal(t, tt.by, got.CoveredBy, "%s", tt.dir)
	}

	got, err := Survey(t.Context(), group, roots, home, nil)
	require.NoError(t, err)
	assert.Empty(t, got.Covered, "a folder inside the clone root isn't covered: its clones are one level further down")
}

func TestSurveyRefuses(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	mkdir(t, filepath.Join(home, "projects"))
	roots := rootsAt(t, "~/projects", home)

	_, err := Survey(t.Context(), filepath.Join(home, "missing"), roots, home, nil)
	require.ErrorIs(t, err, ErrLocationNotFound)

	_, err = Survey(t.Context(), mkdir(t, filepath.Join(home, ".hidden")), roots, home, nil)
	require.ErrorIs(t, err, ErrLocationRefused)
	assert.Equal(t, "~/.hidden is hidden, and the app never looks in hidden folders.", uierr.Message(err))
}

func TestCheck(t *testing.T) {
	t.Parallel()

	git := isolatedGit(t)

	home := t.TempDir()
	projects := mkdir(t, filepath.Join(home, "projects"))
	fakeClone(t, filepath.Join(projects, "api"))
	fakeClone(t, filepath.Join(home, "work", "a"))
	fakeClone(t, filepath.Join(home, "work", "b"))
	initRepo(t, filepath.Join(home, "Desktop", "exp"), "origin", exampleHTTPS)
	mkdir(t, filepath.Join(home, "was-a-clone"))
	fakeClone(t, filepath.Join(home, "now-a-clone", "inner"))
	fakeClone(t, filepath.Join(home, "now-a-clone"))
	testutil.WriteFile(t, filepath.Join(home, "notes"), "x")

	got := Check(t.Context(), "~/projects", []Location{
		{Path: workLocation, Kind: KindFolder},
		{Path: "~/Desktop/exp", Kind: KindClone},
		{Path: "~/gone", Kind: KindFolder},
		{Path: "~/was-a-clone", Kind: KindClone},
		{Path: "~/now-a-clone", Kind: KindFolder},
		{Path: "~/projects/api", Kind: KindClone},
		{Path: "~/work/a", Kind: KindClone},
		{Path: workLocation, Kind: KindFolder},
		{Path: "~/notes", Kind: KindFolder},
	}, home, git)

	assert.Equal(t, []LocationState{
		{Path: "~/projects", Kind: KindFolder, Primary: true, State: StateOK, Clones: 1},
		{Path: workLocation, Kind: KindFolder, State: StateOK, Clones: 2},
		{Path: "~/Desktop/exp", Kind: KindClone, State: StateOK, Repo: "acme-org/example-repo"},
		{Path: "~/gone", Kind: KindFolder, State: StateMissing},
		{Path: "~/was-a-clone", Kind: KindClone, State: StateNotAClone},
		{Path: "~/now-a-clone", Kind: KindFolder, State: StateNowAClone, Clones: 1},
		{Path: "~/projects/api", Kind: KindClone, State: StateCovered, CoveredBy: "~/projects"},
		{Path: "~/work/a", Kind: KindClone, State: StateCovered, CoveredBy: workLocation},
		{Path: workLocation, Kind: KindFolder, State: StateCovered, CoveredBy: workLocation},
		{Path: "~/notes", Kind: KindFolder, State: StateNotFolder},
	}, got)

	got = Check(t.Context(), "~/projects", []Location{{Path: "relative", Kind: KindFolder}}, home, nil)
	assert.Equal(t, StateUnreadable, got[1].State, "a path that can't be read as one")
}
