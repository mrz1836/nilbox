package manifests

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// The manifests of a clone whose modules don't all count.
const (
	emptyGoMod   = "module github.com/acme-org/tool\n\ngo 1.25.0\n"
	emptyPackage = `{"name": "root", "private": true, "workspaces": ["packages/*"]}`
)

// mixedRepo is a fake clone whose origin/main is commit, holding a go.mod that counts,
// last changed at goAt, and, each last changed 900 days ago, a go.mod that requires
// nothing, a package.json with no dependencies, and a go.mod under a fixtures and an
// examples folder that would count anywhere else.
func mixedRepo(commit string, goAt time.Time) *fakeRepo {
	fr := moduleRepo(commit, goAt, goAt)
	old := gitread.Commit{At: daysAgo(900), Subject: "add a sample"}

	for _, file := range []struct{ path, text string }{
		{toolsGoMod, emptyGoMod},
		{packageJSONFile, emptyPackage},
		{".github/ci-tester/fixtures/action-unpinned/go.mod", goModText},
		{"examples/with-config/go.mod", goModText},
		{"web/__fixtures__/app/package.json", packageText},
		{"docs/example/package.json", packageText},
	} {
		fr.trees[commit] = append(fr.trees[commit], entry(file.path, int64(len(file.text))))
		fr.blobs[objectFor(file.path)] = file.text
		fr.changes[changeKey(file.path, "")] = old
	}

	return fr
}

// uncounted returns why each of a clone's modules doesn't count, by its path.
func uncounted(c Clone) map[string]Uncounted {
	out := map[string]Uncounted{}
	for _, m := range c.Modules {
		out[m.Path] = m.Uncounted
	}

	return out
}

func TestModulesThatDontCountAreListedAfterTheRest(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("mixed", mixedRepo(commitID(1), daysAgo(10)))

	page := r.list(false)
	c := page.Clones[0]

	assert.Equal(t, map[string]Uncounted{
		goModFile:       UncountedNone,
		webPackage:      UncountedNone,
		toolsGoMod:      UncountedNoDeps,
		packageJSONFile: UncountedNoDeps,
		".github/ci-tester/fixtures/action-unpinned/go.mod": UncountedFixture,
		"examples/with-config/go.mod":                       UncountedExample,
		"web/__fixtures__/app/package.json":                 UncountedFixture,
		"docs/example/package.json":                         UncountedExample,
	}, uncounted(c))
	assert.Equal(t, []string{
		goModFile, webPackage, ".github/ci-tester/fixtures/action-unpinned/go.mod", "docs/example/package.json",
		"examples/with-config/go.mod", packageJSONFile, toolsGoMod, "web/__fixtures__/app/package.json",
	}, modulePaths(c), "those that count first, then the rest, each stalest first, then by path")
	assert.Equal(t, 2, c.Counted)
	assert.Equal(t, clock.Format(daysAgo(10)), c.Oldest, "the samples, 900 days old, don't make it stale")
	assert.Equal(t, c.Oldest, c.AgeFrom)
	assert.Equal(t, 8, page.Modules, "every module is still listed")
}

func TestACloneWithNothingCountedHasNoAge(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	commit := commitID(1)
	fr := treeRepo(commit, map[string]string{mainBranch: commit}, "examples/demo/go.mod")
	fr.trees[commit] = append(fr.trees[commit], entry(goModFile, int64(len(emptyGoMod))))
	fr.blobs = map[string]string{objectFor(goModFile): emptyGoMod, objectFor("examples/demo/go.mod"): goModText}
	fr.changes = map[string]gitread.Commit{
		changeKey(goModFile, ""):              {At: daysAgo(500), Subject: "start"},
		changeKey("examples/demo/go.mod", ""): {At: daysAgo(400), Subject: "add a demo"},
	}
	r.clone("small", fr)
	r.clone("api", moduleRepo(commitID(2), daysAgo(10), daysAgo(5)))

	page := r.list(false)
	require.Equal(t, []string{owner + "/api", owner + "/small"}, repos(page), "listed, after every clone with an age")
	assert.Zero(t, page.WithoutModules)

	c := page.Clones[1]
	assert.Zero(t, c.Counted)
	assert.Empty(t, c.Oldest)
	assert.Empty(t, c.AgeFrom)
	assert.Equal(t, []string{goModFile, "examples/demo/go.mod"}, modulePaths(c))
}

func TestAMarkAgesTheCloneFromThen(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	api := r.clone("api", moduleRepo(commitID(1), daysAgo(400), daysAgo(200)))
	r.clone("web", moduleRepo(commitID(2), daysAgo(100), daysAgo(50)))

	page := r.list(false)
	require.Equal(t, []string{owner + "/api", owner + "/web"}, repos(page))
	r.git.counted()

	page, err := r.svc.MarkChecked(t.Context(), owner+"/api")
	require.NoError(t, err)
	assert.Empty(t, r.git.counted(), "a mark reads no clone")
	assert.Equal(t, []string{owner + "/web", owner + "/api"}, repos(page), "the page it returns is sorted by it")

	c := page.Clones[1]
	assert.Equal(t, clock.Format(rigStart), c.CheckedAt)
	assert.Equal(t, clock.Format(rigStart), c.AgeFrom, "it ages from the mark")
	assert.Equal(t, clock.Format(daysAgo(400)), c.Oldest, "its manifests' age is still its own")
	assert.Equal(t, clock.Format(rigStart), r.list(false).Clones[1].CheckedAt, "List serves it too")

	r.clock.Advance(48 * time.Hour)
	page, err = r.svc.MarkChecked(t.Context(), owner+"/API")
	require.NoError(t, err)
	assert.Equal(t, clock.Format(rigStart.Add(48*time.Hour)), page.Clones[1].AgeFrom,
		"marking it again, in any case, moves the mark to now")

	r.clock.Advance(10 * 24 * time.Hour)

	later := rigStart.AddDate(0, 0, 5)

	r.git.update(api, func(fr *fakeRepo) {
		moved := moduleRepo(commitID(9), later, later.Add(time.Hour))
		fr.tracking, fr.trees, fr.changes = moved.tracking, moved.trees, moved.changes
	})

	c = r.list(true).Clones[1]
	assert.Equal(t, clock.Format(later), c.AgeFrom, "a manifest that changed since the mark wins")
	assert.Equal(t, clock.Format(rigStart.Add(48*time.Hour)), c.CheckedAt)

	page, err = r.svc.ClearChecked(t.Context(), owner+"/api")
	require.NoError(t, err)
	assert.Empty(t, page.Clones[1].CheckedAt)
	assert.Equal(t, clock.Format(later), page.Clones[1].AgeFrom)

	_, err = r.svc.ClearChecked(t.Context(), owner+"/api")
	require.NoError(t, err, "unmarking one that isn't marked leaves it as it is")
}

func TestAMarkIsTheRepositorysAndOutlivesTheService(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("api", moduleRepo(commitID(1), daysAgo(400), daysAgo(200)))
	other := r.folder("api-copy")
	r.clones.SetOrigin(other, repo.Name{Owner: owner, Repo: "api"})
	r.git.set(other, moduleRepo(commitID(1), daysAgo(400), daysAgo(200)))

	before := r.list(false)
	require.Len(t, before.Clones, 2)

	_, err := r.svc.MarkChecked(t.Context(), owner+"/api")
	require.NoError(t, err)

	again := NewService(Config{
		Clones: r.clones, Git: r.git, Branches: r.branches, DB: r.db, Home: r.home, Clock: r.clock,
	})
	page, err := again.List(t.Context(), false)
	require.NoError(t, err)
	require.Len(t, page.Clones, 2)

	for _, c := range page.Clones {
		assert.Equal(t, clock.Format(rigStart), c.CheckedAt, "%s: every clone of the repository shares it", c.Dir)
	}
}

func TestAMarkLeavesACloneThatCantBeReadUnaged(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	fr := moduleRepo(commitID(1), daysAgo(10), daysAgo(5))
	fr.failTree = errFake
	r.clone("broken", fr)
	r.list(false)

	page, err := r.svc.MarkChecked(t.Context(), owner+"/broken")
	require.NoError(t, err)

	c := page.Clones[0]
	assert.NotEmpty(t, c.Error)
	assert.Equal(t, clock.Format(rigStart), c.CheckedAt, "the mark is kept")
	assert.Empty(t, c.AgeFrom, "but a read that failed is never taken for a fresh one")
}

func TestAMarkNamesARepository(t *testing.T) {
	t.Parallel()

	r := newRig(t)

	for _, name := range []string{"", "acme-org", "acme-org/", "../etc/passwd", "acme-org/a/b"} {
		_, err := r.svc.MarkChecked(t.Context(), name)
		require.ErrorIs(t, err, ErrInvalidRequest, "%q", name)
		assert.Equal(t, "That isn't a repository.", uierr.Message(err))

		_, err = r.svc.ClearChecked(t.Context(), name)
		require.ErrorIs(t, err, ErrInvalidRequest, "%q", name)
	}
}

func TestMarksThatCantBeReadStand(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("api", moduleRepo(commitID(1), daysAgo(400), daysAgo(200)))
	r.list(false)

	_, err := r.svc.MarkChecked(t.Context(), owner+"/api")
	require.NoError(t, err)

	testutil.Exec(t, r.db, "DROP TABLE deps_checks")

	page := r.list(false)
	assert.Equal(t, clock.Format(rigStart), page.Clones[0].CheckedAt, "the last marks read stand")
	assert.Contains(t, r.logs.String(), "using the last ones read")

	_, err = r.svc.MarkChecked(t.Context(), owner+"/api")
	require.Error(t, err)
	assert.Equal(t, "Couldn't mark acme-org/api up to date.", uierr.Message(err))

	_, err = r.svc.ClearChecked(t.Context(), owner+"/api")
	require.Error(t, err)
	assert.Equal(t, "Couldn't unmark acme-org/api.", uierr.Message(err))
}

func TestWithoutADatabaseNothingIsMarked(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("api", moduleRepo(commitID(1), daysAgo(400), daysAgo(200)))
	svc := NewService(Config{Clones: r.clones, Git: r.git, Branches: r.branches, Home: r.home, Clock: r.clock})

	_, err := svc.List(t.Context(), false)
	require.NoError(t, err)

	page, err := svc.MarkChecked(t.Context(), owner+"/api")
	require.NoError(t, err)
	assert.Empty(t, page.Clones[0].CheckedAt)

	_, err = svc.ClearChecked(t.Context(), owner+"/api")
	require.NoError(t, err)
}

func TestAgeFrom(t *testing.T) {
	t.Parallel()

	old, recent := daysAgo(100), daysAgo(1)

	for name, tc := range map[string]struct {
		oldest, checked time.Time
		failed          bool
		want            time.Time
	}{
		"unmarked":                    {oldest: old, want: old},
		"marked since":                {oldest: old, checked: recent, want: recent},
		"changed since the mark":      {oldest: recent, checked: old, want: recent},
		"marked, its age unknown":     {checked: recent, want: recent},
		"neither known":               {},
		"a read that failed":          {oldest: old, failed: true},
		"a read that failed, marked":  {checked: recent, failed: true},
		"marked as it last changed":   {oldest: old, checked: old, want: old},
		"marked, nothing else to say": {checked: old, want: old},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, ageFrom(tc.oldest, tc.checked, tc.failed))
		})
	}
}
