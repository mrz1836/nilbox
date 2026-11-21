package manifests

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/localrepos/localrepostest"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// templateTime is when every commit a git template's build makes is dated, as Deps shows
// it.
const templateTime = "2026-01-02T03:04:05Z"

// The fixture's manifests: the Go module's before and after its bump, the tools module,
// and the pnpm workspace's root and package.
const (
	fixtureGoMod = "module github.com/acme-org/example-repo\n\ngo 1.24.0\n\nrequire github.com/acme-org/lib v1.2.3\n"
	bumpedGoMod  = "module github.com/acme-org/example-repo\n\ngo 1.25.0\n\ntoolchain go1.25.3\n\nrequire (\n" +
		"\tgithub.com/acme-org/lib v1.3.0\n\tgolang.org/x/text v0.20.0 // indirect\n)\n"
	fixtureTools  = "module github.com/acme-org/example-repo/tools\n\ngo 1.24\n\nrequire github.com/acme-org/tool v0.1.0\n"
	fixtureWeb    = `{"name": "web", "packageManager": "pnpm@9.1.0", "workspaces": ["packages/*"], "dependencies": {"a": "1"}, "devDependencies": {"b": "2", "c": "3"}}`
	fixtureUI     = `{"name": "@acme-org/ui", "dependencies": {"react": "^19.0.0"}}`
	initialCommit = "initial"
	bumpCommit    = "bump acme-org/lib"
)

// seedFiles are the files the fixture's first commit holds: two Go modules, a pnpm
// workspace of two packages, a go.work, magex's magefiles, and the vendored, installed,
// and test-fixture manifests Deps leaves out.
func seedFiles() map[string]string {
	return map[string]string{
		goModFile: fixtureGoMod, goSumFile: "github.com/acme-org/lib v1.2.3 h1:abc=\n", "tools/go.mod": fixtureTools,
		webPackage: fixtureWeb, webLock: "lockfileVersion: '9.0'\n", "web/pnpm-workspace.yaml": "packages:\n  - packages/*\n",
		"web/packages/ui/package.json": fixtureUI, goWorkFile: "go 1.25.0\n\nuse (\n\t.\n\t./tools\n)\n",
		"magefiles/magefile.go": "package main\n", "vendor/github.com/acme-org/lib/go.mod": "module github.com/acme-org/lib\n",
		"web/node_modules/left-pad/package.json": `{"name": "left-pad"}`, "internal/testdata/go.mod": "module fixture\n",
		"README.md": "example\n",
	}
}

// fixtureCommits are the commits the fixture's build makes on origin's main.
type fixtureCommits struct {
	initial string
	bump    string
}

// fixtureClones are the repositories a real rig starts from, built once for the test
// binary by buildClones and copied into each test that asks for them:
//
//   - origin.git, whose main holds seedFiles as "initial", then a bumped go.mod;
//   - seed, the repository that pushed them;
//   - projects/example, cloned before the bump, which fetched it since, so its origin/main
//     is ahead of its main;
//   - projects/upstream, cloned after the bump with its remote called upstream;
//   - projects/shallow, a clone of the bump alone;
//   - projects/docs, a repository of a README alone, with no remote.
var fixtureClones = testutil.NewGitTemplate(buildClones) //nolint:gochecknoglobals // built once for the whole test binary

// buildClones builds what fixtureClones describes.
func buildClones(b *testutil.GitBuild) fixtureCommits {
	origin := filepath.Join(b.Root, "origin.git")

	b.Git(".", "-c", "init.defaultBranch=main", "init", "-q", "--bare", origin)
	b.Git(".", "-c", "init.defaultBranch=main", "init", "-q", "seed")

	for name, content := range seedFiles() {
		b.WriteFile(filepath.Join("seed", name), content)
	}

	var c fixtureCommits

	b.Git("seed", "add", "-A")
	b.Git("seed", "commit", "-q", "-m", initialCommit)
	c.initial = b.Git("seed", "rev-parse", "HEAD")
	b.Git("seed", "remote", "add", "origin", origin)
	b.Git("seed", "push", "-q", "origin", mainBranch)

	b.Git(".", "clone", "-q", origin, filepath.Join("projects", "example"))

	b.WriteFile(filepath.Join("seed", goModFile), bumpedGoMod)
	b.Git("seed", "commit", "-q", "-a", "-m", bumpCommit)
	c.bump = b.Git("seed", "rev-parse", "HEAD")
	b.Git("seed", "push", "-q", "origin", mainBranch)

	b.Git(filepath.Join("projects", "example"), "fetch", "-q", "origin")
	b.Git(".", "clone", "-q", "-o", "upstream", origin, filepath.Join("projects", "upstream"))
	b.Git(".", "clone", "-q", "--depth", "1", "file://"+origin, filepath.Join("projects", "shallow"))
	b.Git(".", "-c", "init.defaultBranch=main", "init", "-q", filepath.Join("projects", "docs"))
	b.Commit(filepath.Join("projects", "docs"), "README.md", "docs\n")

	return c
}

// gitread's Git is the read-only git core hands the Service.
var _ Git = (*gitread.Git)(nil)

// countingGit is the real read-only git, counting each call by the clone it reads and
// its method.
type countingGit struct {
	Git

	mu    sync.Mutex
	calls map[string]map[string]int
}

func (g *countingGit) OriginHead(ctx context.Context, dir string) (string, error) {
	g.count(dir, callOriginHead)

	return g.Git.OriginHead(ctx, dir)
}

func (g *countingGit) TrackingCommit(ctx context.Context, dir, branch string) (string, error) {
	g.count(dir, callTrackingCommit)

	return g.Git.TrackingCommit(ctx, dir, branch)
}

func (g *countingGit) Head(ctx context.Context, dir string) (string, error) {
	g.count(dir, callHead)

	return g.Git.Head(ctx, dir)
}

func (g *countingGit) Tree(ctx context.Context, dir, commit string, keep func(string) bool) ([]gitread.TreeEntry, error) {
	g.count(dir, callTree)

	return g.Git.Tree(ctx, dir, commit, keep)
}

func (g *countingGit) LastChange(ctx context.Context, dir, from, manifest, lockfile string) (gitread.Commit, error) {
	g.count(dir, callLastChange)

	return g.Git.LastChange(ctx, dir, from, manifest, lockfile)
}

func (g *countingGit) Blob(ctx context.Context, dir, oid string) ([]byte, error) {
	g.count(dir, callBlob)

	return g.Git.Blob(ctx, dir, oid)
}

func (g *countingGit) Shallow(ctx context.Context, dir string) (bool, error) {
	g.count(dir, callShallow)

	return g.Git.Shallow(ctx, dir)
}

func (g *countingGit) FetchHead(ctx context.Context, dir string) (string, error) {
	g.count(dir, callFetchHead)

	return g.Git.FetchHead(ctx, dir)
}

// count counts a call of method on the clone at dir.
func (g *countingGit) count(dir, method string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.calls[dir] == nil {
		g.calls[dir] = map[string]int{}
	}

	g.calls[dir][method]++
}

// counted returns the calls on the clone at dir since the last reset, and resets every
// count.
func (g *countingGit) counted(dir string) map[string]int {
	g.mu.Lock()
	defer g.mu.Unlock()

	out := g.calls[dir]
	g.calls = map[string]map[string]int{}

	return out
}

// realRig is a Service over a copy of fixtureClones, read with real git isolated from the
// user's config, counting its calls: example's and upstream's repository's default branch
// stored as main, the others' unknown.
type realRig struct {
	home    string
	root    string
	commits fixtureCommits
	git     *countingGit
	svc     *Service
}

// newRealRig returns a realRig, or skips the test without git.
func newRealRig(t *testing.T) realRig {
	t.Helper()

	env := testutil.Git(t)
	home := testutil.RealTempDir(t)
	r := realRig{home: home, root: filepath.Join(home, "projects"), commits: fixtureClones.Copy(t, home)}

	clones := localrepostest.New(r.root)
	for name, of := range map[string]string{
		"example": "example-repo", "upstream": "example-repo", "shallow": "shallow-repo", "docs": "docs-repo",
	} {
		clones.SetOrigin(filepath.Join(r.root, name), repo.Name{Owner: owner, Repo: of})
	}

	branches := &fakeBranches{branches: map[string]string{owner + "/example-repo": mainBranch}}
	r.git = &countingGit{Git: gitread.New(gitread.Config{Env: func() []string { return env }}), calls: map[string]map[string]int{}}
	r.svc = NewService(Config{Clones: clones, Git: r.git, Branches: branches, Home: home, Clock: testutil.NewClock(rigStart)})

	return r
}

// list returns the page, failing the test when List fails, its clones by folder.
func (r realRig) list(t *testing.T, refresh bool) (Page, map[string]Clone) {
	t.Helper()

	page, err := r.svc.List(t.Context(), refresh)
	require.NoError(t, err)

	byDir := map[string]Clone{}
	for _, c := range page.Clones {
		byDir[c.Dir] = c
	}

	return page, byDir
}

func TestReadsRealClones(t *testing.T) {
	t.Parallel()

	r := newRealRig(t)
	page, byDir := r.list(t, false)

	assert.Equal(t, []string{"~/projects/example", "~/projects/upstream", "~/projects/shallow"},
		[]string{page.Clones[0].Dir, page.Clones[1].Dir, page.Clones[2].Dir}, "equally old, so by repository and folder")
	assert.Equal(t, 1, page.WithoutModules, "docs")
	assert.Equal(t, 12, page.Modules, "four in each of the three")

	example := byDir["~/projects/example"]
	assert.Equal(t, "origin/main", example.Tip)
	assert.Equal(t, r.commits.bump, example.Commit, "origin's main as last fetched, ahead of the clone's own")
	assert.False(t, example.AtHead)
	assert.Equal(t, mainBranch, example.DefaultBranch)
	assert.NotEmpty(t, example.FetchedAt, "it fetched the bump")
	assert.False(t, example.Shallow)
	assert.Equal(t, templateTime, example.Oldest)
	assert.Equal(t, []Tool{ToolMagex, ToolPNPM}, example.Tools)
	assert.Equal(t, []string{goWorkFile, "web/pnpm-workspace.yaml"}, example.Workspaces)
	assert.Equal(t, []string{goModFile, "tools/go.mod", webPackage, "web/packages/ui/package.json"}, modulePaths(example),
		"no vendored, installed, or test-fixture manifest")

	assert.Equal(t, []Module{
		{
			Kind: KindGo, Path: goModFile, Lockfile: goSumFile, ChangedAt: templateTime, Subject: bumpCommit,
			GoModule: "github.com/acme-org/example-repo", GoVersion: "1.25.0", Toolchain: "go1.25.3", Direct: 1, Indirect: 1,
			Workspaces: []string{},
		},
		{
			Kind: KindGo, Path: "tools/go.mod", ChangedAt: templateTime, Subject: initialCommit,
			GoModule: "github.com/acme-org/example-repo/tools", GoVersion: "1.24", Direct: 1, Workspaces: []string{},
		},
		{
			Kind: KindJS, Path: webPackage, Lockfile: webLock, ChangedAt: templateTime, Subject: initialCommit, Name: "web",
			PackageManager: ToolPNPM, Workspaces: []string{"packages/*"}, Deps: 1, DevDeps: 2,
		},
		{
			Kind: KindJS, Path: "web/packages/ui/package.json", Lockfile: webLock, ChangedAt: templateTime,
			Subject: initialCommit, Name: "@acme-org/ui", PackageManager: ToolPNPM, Workspaces: []string{}, Deps: 1,
		},
	}, withoutTimes(example.Modules))

	upstream := byDir["~/projects/upstream"]
	assert.True(t, upstream.AtHead, "its remote isn't called origin")
	assert.Equal(t, headRef, upstream.Tip)
	assert.Equal(t, r.commits.bump, upstream.Commit)
	assert.Contains(t, upstream.AtHeadWhy, "This clone has no origin/main")
	assert.Empty(t, upstream.FetchedAt, "it never fetched")

	shallow := byDir["~/projects/shallow"]
	assert.True(t, shallow.Shallow)
	assert.Equal(t, "origin/main", shallow.Tip, "its origin/HEAD names main")
	assert.Equal(t, mainBranch, shallow.DefaultBranch)

	for _, m := range shallow.Modules {
		assert.Equal(t, bumpCommit, m.Subject, "%s: the history stops at the shallow commit", m.Path)
	}
}

func TestARealCloneReadAgainCostsOnlyItsCommitsRead(t *testing.T) {
	t.Parallel()

	r := newRealRig(t)
	example := filepath.Join(r.root, "example")

	r.list(t, false)
	assert.Equal(t, 1, r.git.counted(example)[callTree])

	r.list(t, true)
	assert.Equal(t, map[string]int{callTrackingCommit: 1}, r.git.counted(example), "an unchanged clone costs one rev-parse")

	seed := filepath.Join(r.home, "seed")
	testutil.WriteFile(t, filepath.Join(seed, "tools", "go.sum"), "github.com/acme-org/tool v0.1.0 h1:def=\n")
	testutil.RunGit(t, seed, "add", "tools/go.sum")
	testutil.RunGit(t, seed, "commit", "-q", "-m", "add the tools module's go.sum")
	testutil.RunGit(t, seed, "push", "-q", "origin", mainBranch)
	testutil.RunGit(t, example, "fetch", "-q", "origin")

	_, byDir := r.list(t, true)
	assert.Equal(t, 1, r.git.counted(example)[callTree], "a fetch that moved origin/main reads the clone again")

	modules := byDir["~/projects/example"].Modules
	require.Len(t, modules, 4)

	tools := modules[3]
	assert.Equal(t, "tools/go.mod", tools.Path, "now the freshest, so the last")
	assert.Equal(t, "tools/go.sum", tools.Lockfile)
	assert.Equal(t, "add the tools module's go.sum", tools.Subject, "its age is its lockfile's newest change")
	assert.Greater(t, tools.ChangedAt, templateTime)
}

// withoutTimes returns modules without the times they are sorted by, as a test writes
// them.
func withoutTimes(modules []Module) []Module {
	out := make([]Module, 0, len(modules))

	for _, m := range modules {
		m.changed = time.Time{}
		out = append(out, m)
	}

	return out
}
