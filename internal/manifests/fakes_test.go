package manifests

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/localrepos/localrepostest"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/reporules"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// owner is the org every fake clone's repository is in, and mainBranch the default branch
// most of them have.
const (
	owner      = "acme-org"
	mainBranch = "main"
)

var (
	errFake       = errors.New("fake: failed")
	errNotARepo   = errors.New("fake: not a git repository")
	errNoSuchBlob = errors.New("fake: no such object")
)

// The fake git's methods, as it counts its calls.
const (
	callOriginHead     = "OriginHead"
	callTrackingCommit = "TrackingCommit"
	callHead           = "Head"
	callTree           = "Tree"
	callLastChange     = "LastChange"
	callBlob           = "Blob"
	callShallow        = "Shallow"
	callFetchHead      = "FetchHead"
)

// fakeRepo is one clone as the fake git answers for it.
type fakeRepo struct {
	// originHead is the branch its refs/remotes/origin/HEAD names, tracking the commit
	// each of origin's branches names, and head the commit HEAD names.
	originHead string
	tracking   map[string]string
	head       string

	// trees are each commit's files, blobs each object's contents, and changes each
	// module's newest change, by changeKey.
	trees   map[string][]gitread.TreeEntry
	blobs   map[string]string
	changes map[string]gitread.Commit

	// shallow reports a shallow clone, and fetchHead where its FETCH_HEAD is.
	shallow   bool
	fetchHead string

	// failTree, failBlob, failChange, and failHead, when set, fail those reads.
	failTree   error
	failBlob   error
	failChange error
	failHead   error
}

// changeKey keys a module's newest change: its manifest and its lockfile.
func changeKey(manifest, lockfile string) string {
	return manifest + "\x00" + lockfile
}

// fakeGit is the read-only git Deps reads clones with, answering for each clone from its
// fakeRepo, and counting each call by method. It is safe for concurrent use.
type fakeGit struct {
	mu    sync.Mutex
	repos map[string]*fakeRepo
	calls map[string]int

	// treeHook, when set, is called as a tree is read, with the read's context.
	treeHook func(ctx context.Context)
}

// newFakeGit returns a fake git with no clones.
func newFakeGit() *fakeGit {
	return &fakeGit{repos: map[string]*fakeRepo{}, calls: map[string]int{}}
}

func (g *fakeGit) OriginHead(_ context.Context, dir string) (string, error) {
	r, ok := g.called(callOriginHead, dir)
	if !ok {
		return "", errNotARepo
	}

	return r.originHead, nil
}

func (g *fakeGit) TrackingCommit(_ context.Context, dir, branch string) (string, error) {
	r, ok := g.called(callTrackingCommit, dir)
	if !ok {
		return "", errNotARepo
	}

	if !gitread.ValidBranch(branch) {
		return "", gitread.ErrGitCommandNotAllowed
	}

	return r.tracking[branch], nil
}

func (g *fakeGit) Head(_ context.Context, dir string) (string, error) {
	r, ok := g.called(callHead, dir)
	if !ok {
		return "", errNotARepo
	}

	return r.head, r.failHead
}

func (g *fakeGit) Tree(ctx context.Context, dir, commit string, keep func(path string) bool) ([]gitread.TreeEntry, error) {
	r, ok := g.called(callTree, dir)
	if !ok {
		return nil, errNotARepo
	}

	g.mu.Lock()
	hook := g.treeHook
	g.mu.Unlock()

	if hook != nil {
		hook(ctx)
	}

	if r.failTree != nil {
		return nil, r.failTree
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	out := []gitread.TreeEntry{}

	for _, e := range r.trees[commit] {
		if keep(e.Path) {
			out = append(out, e)
		}
	}

	return out, nil
}

func (g *fakeGit) LastChange(_ context.Context, dir, _, manifest, lockfile string) (gitread.Commit, error) {
	r, ok := g.called(callLastChange, dir)
	if !ok {
		return gitread.Commit{}, errNotARepo
	}

	if r.failChange != nil {
		return gitread.Commit{}, r.failChange
	}

	return r.changes[changeKey(manifest, lockfile)], nil
}

func (g *fakeGit) Blob(_ context.Context, dir, oid string) ([]byte, error) {
	r, ok := g.called(callBlob, dir)
	if !ok {
		return nil, errNotARepo
	}

	if r.failBlob != nil {
		return nil, r.failBlob
	}

	data, found := r.blobs[oid]
	if !found {
		return nil, errNoSuchBlob
	}

	return []byte(data), nil
}

func (g *fakeGit) Shallow(_ context.Context, dir string) (bool, error) {
	r, ok := g.called(callShallow, dir)
	if !ok {
		return false, errNotARepo
	}

	return r.shallow, nil
}

// FetchHead answers where FETCH_HEAD would be, as git does whether or not it is there:
// the repository's own fetchHead, else in the clone's .git folder.
func (g *fakeGit) FetchHead(_ context.Context, dir string) (string, error) {
	r, ok := g.called(callFetchHead, dir)
	if !ok {
		return "", errNotARepo
	}

	if r.fetchHead == "" {
		return filepath.Join(dir, ".git", "FETCH_HEAD"), nil
	}

	return r.fetchHead, nil
}

// set has the fake answer for the clone at dir from r.
func (g *fakeGit) set(dir string, r *fakeRepo) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.repos[dir] = r
}

// update changes the clone at dir's answers with change, under the fake's lock.
func (g *fakeGit) update(dir string, change func(r *fakeRepo)) {
	g.mu.Lock()
	defer g.mu.Unlock()

	change(g.repos[dir])
}

// called counts a call of method, and returns a copy of the clone at dir's answers, or
// false for a folder it has none for.
func (g *fakeGit) called(method, dir string) (fakeRepo, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.calls[method]++

	r, ok := g.repos[dir]
	if !ok {
		return fakeRepo{}, false
	}

	return *r, true
}

// counted returns how many times each method was called since the last reset, and
// resets the counts.
func (g *fakeGit) counted() map[string]int {
	g.mu.Lock()
	defer g.mu.Unlock()

	out := maps.Clone(g.calls)
	g.calls = map[string]int{}

	return out
}

// fakeRules are the pinned and muted repositories, or a failure to read them.
type fakeRules struct {
	mu    sync.Mutex
	rules reporules.Rules
	err   error
}

func (f *fakeRules) RepoRules(context.Context) (reporules.Rules, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.rules, f.err
}

// set applies rule to target.
func (f *fakeRules) set(t *testing.T, target string, rule reporules.Rule) {
	t.Helper()

	f.mu.Lock()
	defer f.mu.Unlock()

	rules, err := f.rules.Set(target, rule)
	require.NoError(t, err)

	f.rules = rules
}

// fakeBranches are the default branches the app stored, by repository.
type fakeBranches struct {
	mu       sync.Mutex
	branches map[string]string
}

func (f *fakeBranches) DefaultBranch(_ context.Context, name repo.Name) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.branches[name.String()], nil
}

// set stores branch as the default branch of the repository name.
func (f *fakeBranches) set(name, branch string) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.branches[owner+"/"+name] = branch
}

// rig is a Service over a clone root of folders that read as clones, under a home of its
// own, answered by the fake git, with a fake clock, the rules, the stored default
// branches, and a database of its own for the repositories marked up to date.
type rig struct {
	t        *testing.T
	home     string
	root     string
	clones   *localrepostest.Clones
	git      *fakeGit
	clock    *testutil.Clock
	rules    *fakeRules
	branches *fakeBranches
	db       *sql.DB
	events   *testutil.Events
	logs     *testutil.Logs
	svc      *Service
}

// rigStart is when every rig's clock starts.
var rigStart = time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC) //nolint:gochecknoglobals // a constant time

// newRig returns a rig with no clones.
func newRig(t *testing.T) *rig {
	t.Helper()

	home := testutil.RealTempDir(t)
	r := &rig{
		t: t, home: home, root: filepath.Join(home, "projects"), git: newFakeGit(), clock: testutil.NewClock(rigStart),
		rules: &fakeRules{}, branches: &fakeBranches{branches: map[string]string{}}, db: testutil.DB(t),
		events: &testutil.Events{}, logs: &testutil.Logs{},
	}
	require.NoError(t, os.MkdirAll(r.root, 0o700))

	r.clones = localrepostest.New(r.root)
	r.svc = NewService(Config{
		Clones: r.clones, Git: r.git, Rules: r.rules, Branches: r.branches, DB: r.db, Home: home, Clock: r.clock,
		Events: r.events, Logger: r.logs.Logger(),
	})

	return r
}

// folder makes a folder called name in the root that reads as a clone, with no origin,
// and returns it.
func (r *rig) folder(name string) string {
	r.t.Helper()

	dir := filepath.Join(r.root, name)
	require.NoError(r.t, os.MkdirAll(filepath.Join(dir, ".git"), 0o700))

	return dir
}

// clone makes a clone called name of the repository acme-org/<name> on GitHub, whose
// default branch the app stored as main, answered by the fake git from fr, and returns
// its folder.
func (r *rig) clone(name string, fr *fakeRepo) string {
	r.t.Helper()

	dir := r.folder(name)
	r.clones.SetOrigin(dir, repo.Name{Owner: owner, Repo: name})
	r.branches.set(name, mainBranch)
	r.git.set(dir, fr)

	return dir
}

// list returns the page, failing the test when List fails.
func (r *rig) list(refresh bool) Page {
	r.t.Helper()

	page, err := r.svc.List(r.t.Context(), refresh)
	require.NoError(r.t, err)

	return page
}

// The manifests the fake clones hold.
const (
	goModText = "module github.com/acme-org/example-repo\n\ngo 1.25.0\n\ntoolchain go1.25.3\n\n" +
		"require (\n\tgithub.com/acme-org/lib v1.2.3\n\tgolang.org/x/text v0.20.0 // indirect\n)\n"
	packageText = `{"name": "web", "packageManager": "pnpm@9.1.0", "workspaces": ["packages/*"],
		"dependencies": {"a": "1"}, "devDependencies": {"b": "2", "c": "3"}}`
	webPackage = "web/package.json"
	webLock    = "web/pnpm-lock.yaml"
	toolsGoMod = "tools/go.mod"
)

// moduleRepo is a fake clone whose origin/main is commit, holding a go.mod, with its
// go.sum, last changed at goAt, and web/package.json, with a pnpm lockfile, last changed
// at jsAt.
func moduleRepo(commit string, goAt, jsAt time.Time) *fakeRepo {
	return &fakeRepo{
		tracking: map[string]string{mainBranch: commit},
		head:     commit,
		trees: map[string][]gitread.TreeEntry{commit: {
			entry(goModFile, int64(len(goModText))), entry(goSumFile, 10), entry(webPackage, int64(len(packageText))),
			entry(webLock, 10), entry("README.md", 10),
		}},
		blobs: map[string]string{objectFor(goModFile): goModText, objectFor(webPackage): packageText},
		changes: map[string]gitread.Commit{
			changeKey(goModFile, goSumFile): {At: goAt, Subject: "bump the go module"},
			changeKey(webPackage, webLock):  {At: jsAt, Subject: "bump web"},
		},
	}
}

// commitID is a fake commit ID, the same in every run for n.
func commitID(n int) string {
	return fmt.Sprintf("%040x", n+1)
}

// daysAgo is the rig's start, n days before.
func daysAgo(n int) time.Time {
	return rigStart.AddDate(0, 0, -n)
}
