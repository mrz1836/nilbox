package manifests

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/clock"
	"github.com/mrz1836/nilbox/internal/event"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/reporules"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// repos returns the page's clones' repositories, in order.
func repos(page Page) []string {
	out := make([]string, 0, len(page.Clones))
	for _, c := range page.Clones {
		out = append(out, c.Repo)
	}

	return out
}

// modulePaths returns a clone's modules' paths, in order.
func modulePaths(c Clone) []string {
	out := make([]string, 0, len(c.Modules))
	for _, m := range c.Modules {
		out = append(out, m.Path)
	}

	return out
}

// treeRepo is a fake clone holding files at commit, each regular and 10 bytes long, its
// origin's branches naming the commits tracking does, and HEAD the commit.
func treeRepo(commit string, tracking map[string]string, files ...string) *fakeRepo {
	tree := make([]gitread.TreeEntry, 0, len(files))
	for _, file := range files {
		tree = append(tree, entry(file, 10))
	}

	return &fakeRepo{
		tracking: tracking, head: commit, trees: map[string][]gitread.TreeEntry{commit: tree},
		blobs: map[string]string{}, changes: map[string]gitread.Commit{},
	}
}

func TestListReadsEveryCloneStalestFirst(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("fresh", moduleRepo(commitID(1), daysAgo(2), daysAgo(1)))
	r.clone("stale", moduleRepo(commitID(2), daysAgo(400), daysAgo(30)))
	r.clone("middle", moduleRepo(commitID(3), daysAgo(90), daysAgo(60)))
	r.clone("docs", treeRepo(commitID(4), map[string]string{mainBranch: commitID(4)}, "README.md"))
	r.clone("hushed", moduleRepo(commitID(5), daysAgo(999), daysAgo(999)))
	r.folder("elsewhere")
	r.rules.set(t, owner+"/hushed", reporules.RuleMuted)
	r.rules.set(t, owner, reporules.RulePinned)
	r.rules.set(t, owner+"/fresh", reporules.RuleNone)
	r.rules.set(t, owner+"/stale", reporules.RuleMuted)
	r.rules.set(t, owner+"/stale", reporules.RuleNone)

	page := r.list(false)

	assert.Equal(t, []string{owner + "/stale", owner + "/middle", owner + "/fresh"}, repos(page))
	assert.Equal(t, 1, page.Muted, "hushed")
	assert.Equal(t, 1, page.WithoutGitHub, "elsewhere")
	assert.Equal(t, 1, page.WithoutModules, "docs")
	assert.Equal(t, 6, page.Modules)
	assert.Equal(t, clock.Format(rigStart), page.CheckedAt)
	assert.Empty(t, page.Error)

	for _, c := range page.Clones {
		assert.True(t, c.Pinned, "%s: its org is pinned", c.Repo)
	}

	assert.Equal(t, Clone{
		Repo: owner + "/stale", Dir: "~/projects/stale", Pinned: true, Oldest: clock.Format(daysAgo(400)),
		Tools: []Tool{ToolPNPM}, Workspaces: []string{}, Tip: "origin/main", Commit: commitID(2), DefaultBranch: mainBranch,
		Modules: []Module{
			{
				Kind: KindGo, Path: goModFile, Lockfile: goSumFile, ChangedAt: clock.Format(daysAgo(400)),
				Subject: "bump the go module", GoModule: "github.com/acme-org/example-repo", GoVersion: "1.25.0",
				Toolchain: "go1.25.3", Direct: 1, Indirect: 1, Workspaces: []string{}, changed: daysAgo(400),
			},
			{
				Kind: KindJS, Path: webPackage, Lockfile: webLock, ChangedAt: clock.Format(daysAgo(30)), Subject: "bump web",
				Name: "web", PackageManager: ToolPNPM, Workspaces: []string{"packages/*"}, Deps: 1, DevDeps: 2,
				changed: daysAgo(30),
			},
		},
		oldest: daysAgo(400),
	}, page.Clones[0])
}

func TestAnUnchangedCloneCostsOneRevParse(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	api := r.clone("api", moduleRepo(commitID(1), daysAgo(10), daysAgo(5)))
	r.clone("web", moduleRepo(commitID(2), daysAgo(20), daysAgo(3)))

	r.list(false)

	first := r.git.counted()
	assert.Equal(t, 2, first[callTree], "each clone's tree is read once")
	assert.Equal(t, 4, first[callLastChange], "one log per module")
	assert.Equal(t, 4, first[callBlob], "one blob per manifest")

	r.list(true)
	assert.Equal(t, map[string]int{callTrackingCommit: 2}, r.git.counted(), "an unchanged clone costs one rev-parse")

	r.git.update(api, func(fr *fakeRepo) {
		moved := moduleRepo(commitID(9), daysAgo(1), daysAgo(1))
		fr.tracking, fr.trees, fr.changes = moved.tracking, moved.trees, moved.changes
	})

	page := r.list(true)
	calls := r.git.counted()
	assert.Equal(t, 2, calls[callTrackingCommit])
	assert.Equal(t, 1, calls[callTree], "only the clone whose commit moved is read again")
	assert.Equal(t, []string{owner + "/web", owner + "/api"}, repos(page))
	assert.Equal(t, commitID(9), page.Clones[1].Commit)
}

func TestWithoutAStoredDefaultAnUnchangedCloneAlsoReadsOriginsHead(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	fr := moduleRepo(commitID(1), daysAgo(10), daysAgo(5))
	fr.originHead = mainBranch
	r.clone("api", fr)
	r.branches.set("api", "")

	r.list(false)
	r.git.counted()

	r.list(true)
	assert.Equal(t, map[string]int{callOriginHead: 1, callTrackingCommit: 1}, r.git.counted())
}

func TestTheLastReadIsServedForFiveMinutes(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("api", moduleRepo(commitID(1), daysAgo(10), daysAgo(5)))

	r.list(false)
	r.git.counted()

	listed := r.clones.Reads().Dirs

	r.clock.Advance(FreshFor - time.Second)
	page := r.list(false)
	assert.Empty(t, r.git.counted(), "a read under five minutes old is served as it is")
	assert.Equal(t, listed, r.clones.Reads().Dirs, "the clones aren't listed again")
	assert.Equal(t, clock.Format(rigStart), page.CheckedAt)

	page = r.list(true)
	assert.Equal(t, map[string]int{callTrackingCommit: 1}, r.git.counted(), "refresh reads every clone again")
	assert.Equal(t, clock.Format(rigStart.Add(FreshFor-time.Second)), page.CheckedAt)

	r.clock.Advance(FreshFor)
	r.list(false)
	assert.Equal(t, map[string]int{callTrackingCommit: 1}, r.git.counted(), "a read five minutes old is read again")

	r.clock.Advance(-time.Hour)
	r.list(false)
	assert.Equal(t, map[string]int{callTrackingCommit: 1}, r.git.counted(), "and one from the future")
}

func TestRulesOrLocationsThatChangeReadAgain(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("api", moduleRepo(commitID(1), daysAgo(10), daysAgo(5)))

	page := r.list(false)
	assert.False(t, page.Clones[0].Pinned)
	r.git.counted()

	r.rules.set(t, owner+"/api", reporules.RulePinned)
	page = r.list(false)
	assert.True(t, page.Clones[0].Pinned)
	assert.Equal(t, map[string]int{callTrackingCommit: 1}, r.git.counted())

	r.rules.set(t, owner+"/api", reporules.RuleMuted)
	page = r.list(false)
	assert.Empty(t, page.Clones)
	assert.Equal(t, 1, page.Muted)
	assert.Empty(t, r.git.counted(), "a muted clone isn't read")

	r.rules.set(t, owner+"/api", reporules.RuleNone)
	page = r.list(false)
	require.Len(t, page.Clones, 1)
	assert.Equal(t, 1, r.git.counted()[callTree], "a clone muted for a read keeps nothing")

	r.rules.err = errFake
	r.list(false)
	assert.Empty(t, r.git.counted(), "rules that can't be read leave the last ones standing")
	assert.Contains(t, r.logs.String(), "couldn't read the pinned and muted repositories")

	other := testutil.RealTempDir(t)
	r.clones.SetExtra(localrepos.Location{Path: other, Kind: localrepos.KindFolder})
	r.list(false)
	assert.Equal(t, map[string]int{callTrackingCommit: 1}, r.git.counted(), "other locations read every clone again")
}

func TestTheCommitReadFollowsTheDefaultBranch(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("stored", treeRepo(commitID(1), map[string]string{mainBranch: commitID(1)}, goModFile))

	trunk := treeRepo(commitID(2), map[string]string{"trunk": commitID(2)}, goModFile)
	trunk.originHead = "trunk"
	r.clone("trunk", trunk)
	r.branches.set("trunk", "")

	r.clone("master", treeRepo(commitID(3), map[string]string{"master": commitID(3)}, goModFile))
	r.branches.set("master", "")

	r.clone("upstream", treeRepo(commitID(4), map[string]string{}, goModFile))

	r.clone("nameless", treeRepo(commitID(5), map[string]string{}, goModFile))
	r.branches.set("nameless", "")

	byRepo := map[string]Clone{}
	for _, c := range r.list(false).Clones {
		byRepo[c.Repo] = c
	}

	for name, want := range map[string]struct{ tip, branch, commit string }{
		"stored": {"origin/main", mainBranch, commitID(1)}, "trunk": {"origin/trunk", "trunk", commitID(2)},
		"master": {"origin/master", "master", commitID(3)}, "upstream": {headRef, mainBranch, commitID(4)},
		"nameless": {headRef, "", commitID(5)},
	} {
		c := byRepo[owner+"/"+name]
		assert.Equal(t, want.tip, c.Tip, name)
		assert.Equal(t, want.branch, c.DefaultBranch, name)
		assert.Equal(t, want.commit, c.Commit, name)
		assert.Equal(t, want.tip == headRef, c.AtHead, name)
		assert.Equal(t, want.tip == headRef, c.AtHeadWhy != "", name)
	}

	assert.Equal(t, "This clone has no origin/main, so its HEAD was read: its GitHub remote may not be named origin, or "+
		"it hasn't fetched main yet.", byRepo[owner+"/upstream"].AtHeadWhy)
	assert.Contains(t, byRepo[owner+"/nameless"].AtHeadWhy, "No default branch is known")
}

func TestAGuessedDefaultBranchIsReadOnce(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("master", treeRepo(commitID(3), map[string]string{"master": commitID(3)}, goModFile))
	r.branches.set("master", "")

	r.list(false)

	calls := r.git.counted()
	assert.Equal(t, 2, calls[callTrackingCommit], "origin/main, then origin/master, whose commit is the one read")
	assert.Equal(t, 1, calls[callOriginHead])
	assert.Zero(t, calls[callHead])
}

func TestAShallowCloneAndWhenItFetched(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	fetchHead := filepath.Join(t.TempDir(), "FETCH_HEAD")
	testutil.WriteFile(t, fetchHead, "fetched\n")
	require.NoError(t, os.Chtimes(fetchHead, daysAgo(3), daysAgo(3)))

	fr := moduleRepo(commitID(1), daysAgo(10), daysAgo(5))
	fr.shallow, fr.fetchHead = true, fetchHead
	api := r.clone("api", fr)

	c := r.list(false).Clones[0]
	assert.True(t, c.Shallow)
	assert.Equal(t, clock.Format(daysAgo(3)), c.FetchedAt)
	r.git.counted()

	require.NoError(t, os.Chtimes(fetchHead, daysAgo(1), daysAgo(1)))
	r.git.update(api, func(fr *fakeRepo) { fr.shallow = false })

	c = r.list(true).Clones[0]
	assert.False(t, c.Shallow, "a fetch that moved nothing has the clone read again for whether it is shallow")
	assert.Equal(t, clock.Format(daysAgo(1)), c.FetchedAt)
	assert.Equal(t, map[string]int{callTrackingCommit: 1, callShallow: 1}, r.git.counted())

	require.NoError(t, os.Remove(fetchHead))

	c = r.list(true).Clones[0]
	assert.Empty(t, c.FetchedAt, "a FETCH_HEAD that is gone is no fetch")
}

func TestModulesThatCantBeReadSaySo(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	commit := commitID(1)
	fr := treeRepo(commit, map[string]string{mainBranch: commit}, "broken/package.json", "nomod/go.mod", "gone/go.mod")
	fr.trees[commit] = append(fr.trees[commit], entry("big/package.json", maxManifest+1))
	fr.blobs = map[string]string{objectFor("broken/package.json"): "{nope", objectFor("nomod/go.mod"): "go 1.22\n"}
	fr.changes = map[string]gitread.Commit{changeKey("nomod/go.mod", ""): {At: daysAgo(7), Subject: "old"}}
	r.clone("odd", fr)

	c := r.list(false).Clones[0]
	assert.Equal(t, []string{"nomod/go.mod", "big/package.json", "broken/package.json", "gone/go.mod"}, modulePaths(c),
		"the one whose age is known first, the rest by path")

	problems := map[string]string{}
	for _, m := range c.Modules {
		problems[m.Path] = m.Problem
	}

	assert.Equal(t, map[string]string{
		"big/package.json":    "This package.json is larger than 1 MiB, so it wasn't read.",
		"broken/package.json": "This package.json isn't a JSON object, so it wasn't read.",
		"nomod/go.mod":        "This go.mod names no module.",
		"gone/go.mod":         "Couldn't read this go.mod: fake: no such object.",
	}, problems)
	assert.Equal(t, 3, r.git.counted()[callBlob], "a manifest over a mebibyte isn't read")

	r.list(true)
	assert.Equal(t, 1, r.git.counted()[callTree], "a read that may go otherwise next time isn't kept")
}

func TestAManifestAPartialCloneLacksKeepsItsAge(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	commit := commitID(1)
	fr := treeRepo(commit, map[string]string{mainBranch: commit})
	missing := entry(goModFile, 0)
	missing.Size, missing.Missing = -1, true
	fr.trees[commit] = append(fr.trees[commit], missing)
	fr.changes = map[string]gitread.Commit{changeKey(goModFile, ""): {At: daysAgo(40), Subject: "bump the modules"}}
	r.clone("partial", fr)

	c := r.list(false).Clones[0]
	require.Len(t, c.Modules, 1, "a manifest the clone lacks still counts")
	assert.Equal(t, "This clone doesn't hold this go.mod's contents, as a partial clone may not, so only when it "+
		"last changed was read.", c.Modules[0].Problem)
	assert.Equal(t, "bump the modules", c.Modules[0].Subject, "its history is read")
	assert.NotEmpty(t, c.Oldest)
	assert.Zero(t, r.git.counted()[callBlob], "and its contents aren't asked for, which would fetch them")
}

func TestModulesTheClonesTimeRanOutBeforeSaySo(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	g := newFakeGit()
	r := &moduleReader{git: g, dir: "/nowhere", commit: commitID(1)}

	modules := r.readAll(ctx, []gitread.TreeEntry{entry(goModFile, 10), entry(webPackage, 10)})
	assert.Equal(t, []Module{
		{
			Kind: KindGo, Path: goModFile, Workspaces: []string{},
			Problem: "This go.mod wasn't read: reading this clone took too long.",
		},
		{
			Kind: KindJS, Path: webPackage, Workspaces: []string{},
			Problem: "This package.json wasn't read: reading this clone took too long.",
		},
	}, modules, "each in its place, so the modules read before are still shown")
	assert.True(t, r.unsettled.Load(), "and the next scan reads them all again")
	assert.Empty(t, g.counted(), "no git is run once the time is out")
}

func TestAManifestGitPrintsTooMuchOfIsKept(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	fr := moduleRepo(commitID(1), daysAgo(10), daysAgo(5))
	fr.failBlob = fmt.Errorf("run git: %w", execpath.ErrOutputLimit)
	r.clone("api", fr)

	c := r.list(false).Clones[0]
	assert.Equal(t, "Couldn't read this go.mod: git printed more than can be read here.", c.Modules[0].Problem)
	r.git.counted()

	r.list(true)
	assert.Equal(t, map[string]int{callTrackingCommit: 1}, r.git.counted(), "it would again at the same commit")
}

func TestAModuleWhoseHistoryCantBeReadIsListedLast(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	broken := moduleRepo(commitID(1), daysAgo(10), daysAgo(5))
	broken.failChange = errFake
	r.clone("broken", broken)
	r.clone("fine", moduleRepo(commitID(2), daysAgo(1), daysAgo(1)))

	page := r.list(false)
	assert.Equal(t, []string{owner + "/fine", owner + "/broken"}, repos(page))

	c := page.Clones[1]
	assert.Empty(t, c.Oldest)
	assert.Empty(t, c.Modules[0].ChangedAt)
	assert.Equal(t, "Couldn't read when this go.mod last changed: fake: failed.", c.Modules[0].Problem)
}

func TestACloneThatCantBeReadSaysSo(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	tree := moduleRepo(commitID(1), daysAgo(10), daysAgo(5))
	tree.failTree = errFake
	r.clone("tree", tree)

	head := treeRepo(commitID(2), map[string]string{}, goModFile)
	head.failHead = errFake
	r.clone("head", head)

	page := r.list(false)
	require.Len(t, page.Clones, 2, "listed, not counted as holding no module")
	assert.Zero(t, page.WithoutModules)

	errs := map[string]string{}
	for _, c := range page.Clones {
		errs[c.Repo] = c.Error
		assert.NotNil(t, c.Modules)
	}

	assert.Equal(t, map[string]string{
		owner + "/tree": "Couldn't read this clone's files at origin/main: fake: failed.",
		owner + "/head": "Couldn't work out which commit of this clone to read: fake: failed.",
	}, errs)
	assert.Contains(t, r.logs.String(), "couldn't read a clone's manifests")
}

func TestACloneWithManyModulesIsTruncated(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	files := make([]string, 0, maxModules+5)

	for i := range maxModules + 5 {
		files = append(files, fmt.Sprintf("m%03d/go.mod", i))
	}

	r.clone("mono", treeRepo(commitID(1), map[string]string{mainBranch: commitID(1)}, files...))

	page := r.list(false)
	require.Len(t, page.Clones, 1)
	assert.True(t, page.Clones[0].Truncated)
	assert.Len(t, page.Clones[0].Modules, maxModules)
	assert.Equal(t, maxModules, page.Modules)
	assert.Equal(t, maxModules, r.git.counted()[callLastChange], "only the modules listed are read")
}

func TestToolsAndWorkspaces(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	commit := commitID(1)
	fr := treeRepo(commit, map[string]string{mainBranch: commit}, "magefiles/magefile.go", goWorkFile, "apps/yarn.lock",
		"apps/site/package.json", "pnpm-workspace.yaml", "bun/package.json", "bun/bun.lockb", goModFile)
	fr.blobs = map[string]string{objectFor("bun/package.json"): `{"packageManager": "bun@1.1.0"}`}
	r.clone("tools", fr)

	c := r.list(false).Clones[0]
	assert.Equal(t, []Tool{ToolMagex, ToolPNPM, ToolYarn, ToolBun}, c.Tools)
	assert.Equal(t, []string{goWorkFile, "pnpm-workspace.yaml"}, c.Workspaces)

	managers := map[string]Tool{}
	for _, m := range c.Modules {
		managers[m.Path] = m.PackageManager
	}

	assert.Equal(t, map[string]Tool{"apps/site/package.json": ToolYarn, "bun/package.json": ToolBun, goModFile: ToolNone},
		managers, "a package's manager falls back to its lockfile's")
}

func TestAReadThatFailsKeepsTheLastPage(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("api", moduleRepo(commitID(1), daysAgo(10), daysAgo(5)))
	r.list(false)

	r.clones.FailDirs(errFake)
	page := r.list(true)
	assert.Equal(t, "Couldn't list the clones in ~/projects.", page.Error)
	assert.Equal(t, []string{owner + "/api"}, repos(page), "the last read's clones stay")
	assert.Contains(t, r.logs.String(), "showing the last read")

	r.clones.FailDirs(nil)
	page = r.list(true)
	assert.Empty(t, page.Error)

	first := newRig(t)
	first.clones.FailDirs(errFake)

	_, err := first.svc.List(t.Context(), false)
	require.ErrorIs(t, err, errFake, "with no page to keep, the read's failure is the call's")
	assert.Equal(t, "Couldn't list the clones in ~/projects.", uierr.Message(err))

	first.clones.FailRoots(errFake)
	_, err = first.svc.List(t.Context(), false)
	require.ErrorIs(t, err, errFake)
}

func TestListTellsTheWindowHowFarItHasGot(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("api", moduleRepo(commitID(1), daysAgo(10), daysAgo(5)))
	r.list(false)

	var told []event.ManifestsProgress

	for _, e := range r.events.All() {
		if p, ok := e.(event.ManifestsProgress); ok {
			told = append(told, p)
		}
	}

	require.NotEmpty(t, told)
	assert.Equal(t, event.ManifestsProgress{Phase: event.ManifestsFindingClones}, told[0], "finding the clones, at once")

	for _, p := range told {
		assert.LessOrEqual(t, p.Done, p.Total)
	}
}

func TestThePageIsTheCallersOwn(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("api", moduleRepo(commitID(1), daysAgo(10), daysAgo(5)))

	page := r.list(false)
	page.Clones[0].Modules[1].Workspaces[0] = "changed"
	page.Clones[0].Tools[0] = ToolBun
	page.Clones[0].Modules[0].Path = "changed"

	again := r.list(false)
	assert.Equal(t, []string{"packages/*"}, again.Clones[0].Modules[1].Workspaces)
	assert.Equal(t, []Tool{ToolPNPM}, again.Clones[0].Tools)
	assert.Equal(t, goModFile, again.Clones[0].Modules[0].Path)
}

func TestNoListIsNil(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	empty := r.list(false)
	assert.NotNil(t, empty.Clones)

	r.clone("api", moduleRepo(commitID(1), daysAgo(10), daysAgo(5)))

	for _, c := range r.list(true).Clones {
		assert.NotNil(t, c.Tools)
		assert.NotNil(t, c.Workspaces)

		for _, m := range c.Modules {
			assert.NotNil(t, m.Workspaces, m.Path)
		}
	}

	assert.NotNil(t, NewService(Config{}).lastPage().Clones, "before any read")
}

func TestACallerThatStopsWaitingLeavesTheReadGoing(t *testing.T) {
	t.Parallel()

	r := newRig(t)
	r.clone("api", moduleRepo(commitID(1), daysAgo(10), daysAgo(5)))

	entered, release := make(chan struct{}), make(chan struct{})

	r.git.treeHook = func(context.Context) {
		close(entered)
		<-release
	}

	ctx, cancel := context.WithCancel(t.Context())
	waited := make(chan error, 1)

	go func() {
		_, err := r.svc.List(ctx, false)
		waited <- err
	}()

	<-entered
	cancel()
	require.ErrorIs(t, <-waited, context.Canceled, "the caller stops waiting")

	r.git.mu.Lock()
	r.git.treeHook = nil
	r.git.mu.Unlock()
	close(release)

	page := r.list(false)
	assert.Equal(t, []string{owner + "/api"}, repos(page), "the read went on, and lands for the next caller")
	assert.Equal(t, 1, r.git.counted()[callTree], "it was read once")
}
