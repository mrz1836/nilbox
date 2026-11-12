package release

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/localrepos"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// fakeMergeSetting answers the integrations.offerAfterMerge setting.
type fakeMergeSetting struct {
	on  bool
	err error
}

func (f fakeMergeSetting) OfferAfterMerge(context.Context) (bool, error) { return f.on, f.err }

// fakeBranches answers the default branches the inbox stored, by repository key, counting
// the questions.
type fakeBranches struct {
	mu       sync.Mutex
	branches map[string]string
	err      error
	asked    int
}

func (f *fakeBranches) DefaultBranch(_ context.Context, name repo.Name) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.asked++

	return f.branches[name.Key()], f.err
}

// fixedTool is magex as the integrations know it.
type fixedTool struct {
	facts ToolFacts
}

func (f fixedTool) ReleaseTool(context.Context) ToolFacts { return f.facts }

// errNotACloneHere is a folder with no git repository in it.
var errNotACloneHere = errors.New("not a git repository")

// folderOrigins answers each clone folder's origin, by the folder's name, as one git
// command would, and records the folders it was asked about: each is a git run.
type folderOrigins struct {
	mu      sync.Mutex
	origins map[string]Clone
	asked   []string
}

func (f *folderOrigins) Origin(_ context.Context, dir string) (repo.Name, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.asked = append(f.asked, filepath.Base(dir))

	c, ok := f.origins[filepath.Base(dir)]
	if !ok {
		return repo.Name{}, false, errNotACloneHere
	}

	return c.Origin, c.OnHub, nil
}

// gitRuns returns the folders whose origin was read, in order.
func (f *folderOrigins) gitRuns() []string {
	f.mu.Lock()
	defer f.mu.Unlock()

	return slices.Clone(f.asked)
}

// sdkName is the repository most merges below land in.
const sdkName = "sdk"

// usableMagex is a copy of magex Release can run.
func usableMagex() ToolFacts {
	return ToolFacts{CopyFacts: integrations.CopyFacts{Path: "/opt/tools/magex", Version: "1.30.2", State: integrations.StateFound}}
}

// mergeRig is a Service that answers OfferAfterMerge over a real clone root, ~/projects,
// whose folders' origins a fake reads: sdk, web, and cli, each named as its repository is;
// fresh-app, a clone of fresh named otherwise; mirror, whose origin isn't on GitHub; and a
// notes folder that isn't a clone. The setting is on, magex usable, and the inbox stored
// default branches.
type mergeRig struct {
	home     string
	root     string
	github   *fakeGitHub
	clones   *fakeClones
	origins  *folderOrigins
	branches *fakeBranches
	setting  fakeMergeSetting
	tool     ToolFacts
}

// newMergeRig returns a rig in which a merge into sdk's master offers a release.
func newMergeRig(t *testing.T) *mergeRig {
	t.Helper()

	home := testutil.RealTempDir(t)
	root := filepath.Join(home, "projects")

	clones := map[string]Clone{
		"sdk": {Origin: name("sdk"), OnHub: true}, "web": {Origin: name("web"), OnHub: true},
		"cli": {Origin: name("cli"), OnHub: true}, "fresh-app": {Origin: name("fresh"), OnHub: true},
		"mirror": {Origin: name("mirror")},
	}

	listed := make([]Clone, 0, len(clones))

	for _, folder := range slices.Sorted(maps.Keys(clones)) {
		dir := filepath.Join(root, folder)
		require.NoError(t, os.MkdirAll(dir, 0o700))

		c := clones[folder]
		c.Dir = dir
		listed = append(listed, c)
	}

	require.NoError(t, os.MkdirAll(filepath.Join(root, "notes"), 0o700))
	testutil.WriteFile(t, filepath.Join(root, "SDK.md"), "not a folder")

	return &mergeRig{
		home: home, root: root, github: newRig().github, clones: &fakeClones{root: root, list: listed},
		origins: &folderOrigins{origins: clones}, setting: fakeMergeSetting{on: true}, tool: usableMagex(),
		branches: &fakeBranches{branches: map[string]string{
			name("sdk").Key(): branchMaster, name("web").Key(): branchMain, name("mirror").Key(): branchMain,
			name("fresh").Key(): branchMain,
		}},
	}
}

// service returns the Service over the rig as it is now.
func (m *mergeRig) service() *Service {
	return NewService(Config{
		GitHub: m.github, Clones: m.clones, Origins: m.origins, Tools: fixedTool{facts: m.tool},
		AfterMerge: m.setting, Branches: m.branches, Home: m.home,
	})
}

func TestOfferAfterMerge(t *testing.T) {
	t.Parallel()

	offer := func(repo, branch string) AfterMerge {
		return AfterMerge{Offer: true, Repo: "acme-org/" + repo, Dir: "~/projects/" + repo, Branch: branch}
	}

	tests := []struct {
		name       string
		edit       func(m *mergeRig)
		repo, base string
		want       AfterMerge
		inboxAsked int
		gitRuns    []string
	}{
		{
			name: "into the default branch", repo: sdkName, base: branchMaster, want: offer(sdkName, branchMaster),
			gitRuns: []string{sdkName},
		},
		{
			name: "into another default branch", repo: "web", base: branchMain, want: offer("web", branchMain),
			gitRuns: []string{"web"},
		},
		{
			name: "into main beside a default of master", repo: sdkName, base: branchMain,
			want: offer(sdkName, branchMain), gitRuns: []string{sdkName},
		},
		{
			name: "into master with no default stored", repo: sdkName, base: branchMaster, want: offer(sdkName, branchMaster),
			edit: func(m *mergeRig) { m.branches.branches = nil }, gitRuns: []string{sdkName},
		},
		{name: "into a branch Release doesn't offer", repo: sdkName, base: "develop", inboxAsked: 1},
		{
			name: "into a branch with no default stored", repo: sdkName, base: "develop", inboxAsked: 1,
			edit: func(m *mergeRig) { m.branches.branches = nil },
		},
		{name: "the setting off", repo: sdkName, base: branchMaster, edit: func(m *mergeRig) { m.setting.on = false }},
		{name: "magex off", repo: sdkName, base: branchMaster, edit: func(m *mergeRig) { m.tool.Off = true }},
		{name: "magex missing", repo: sdkName, base: branchMaster, edit: func(m *mergeRig) { m.tool = ToolFacts{} }},
		{
			name: "magex too old", repo: sdkName, base: branchMaster,
			edit: func(m *mergeRig) { m.tool.State = integrations.StateTooOld },
		},
		{name: "no clone of it", repo: "elsewhere", base: branchMaster},
		{name: "a clone whose origin isn't on GitHub", repo: "mirror", base: branchMain, gitRuns: []string{"mirror"}},
		{name: "a folder that isn't a clone", repo: "notes", base: branchMaster, gitRuns: []string{"notes"}},
		{name: "a clone in a folder named otherwise", repo: "fresh", base: branchMain},
		{name: "no branch", repo: sdkName, base: " "},
		{
			name: "in another case", repo: "SDK", base: branchMaster,
			want:    AfterMerge{Offer: true, Repo: "acme-org/SDK", Dir: "~/projects/sdk", Branch: branchMaster},
			gitRuns: []string{sdkName},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			m := newMergeRig(t)
			if tt.edit != nil {
				tt.edit(m)
			}

			got, err := m.service().OfferAfterMerge(t.Context(), "acme-org", tt.repo, tt.base)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
			assert.Zero(t, m.github.calls(), "a merge's offer asks GitHub nothing")
			assert.Equal(t, tt.inboxAsked, m.branches.asked, "the inbox is asked only for a branch not main or master")
			assert.Equal(t, tt.gitRuns, m.origins.gitRuns(), "git runs only in the folders named as the repository is")
			assert.Zero(t, m.clones.scans(), "the clone root is never scanned")
		})
	}
}

func TestOfferAfterMergeReadsTheReleasePagesListFirst(t *testing.T) {
	t.Parallel()

	m := newMergeRig(t)
	cli := m.github.statuses[name("cli").Key()]
	cli.DefaultBranch = "trunk"
	m.github.statuses[name("cli").Key()] = cli
	m.branches.branches[name("cli").Key()] = "release" // stale: the page read GitHub since the inbox did
	svc := m.service()

	_, err := svc.Candidates(t.Context(), false)
	require.NoError(t, err)
	require.Equal(t, 1, m.github.calls())
	require.Equal(t, 1, m.clones.scans(), "the page lists the clone root")

	got, err := svc.OfferAfterMerge(t.Context(), "acme-org", "cli", "trunk")
	require.NoError(t, err)
	assert.Equal(t, AfterMerge{Offer: true, Repo: "acme-org/cli", Dir: "~/projects/cli", Branch: "trunk"}, got)

	got, err = svc.OfferAfterMerge(t.Context(), "acme-org", "cli", "release")
	require.NoError(t, err)
	assert.False(t, got.Offer, "the page's default, not the inbox's stale one")

	got, err = svc.OfferAfterMerge(t.Context(), "acme-org", "fresh", branchMain)
	require.NoError(t, err)
	assert.Equal(t, AfterMerge{Offer: true, Repo: "acme-org/fresh", Dir: "~/projects/fresh-app", Branch: branchMain},
		got, "the page's list finds a clone in a folder named otherwise")

	assert.Zero(t, m.branches.asked, "the page's list knew the default branches")
	assert.Empty(t, m.origins.gitRuns(), "and the clones")
	assert.Equal(t, 1, m.github.calls(), "and nothing more was asked of GitHub")
	assert.Equal(t, 1, m.clones.scans(), "nor the clone root scanned again")

	m.clones.moveTo(filepath.Join(m.home, "elsewhere"))
	require.NoError(t, os.MkdirAll(filepath.Join(m.home, "elsewhere"), 0o700))

	got, err = svc.OfferAfterMerge(t.Context(), "acme-org", "cli", "trunk")
	require.NoError(t, err)
	assert.False(t, got.Offer, "a list from another clone root doesn't serve")
}

func TestOfferAfterMergeSaysWhatItCantRead(t *testing.T) {
	t.Parallel()

	ctx := t.Context()

	for _, bad := range [][2]string{{"", sdkName}, {"acme-org", ""}, {"acme org", sdkName}} {
		_, err := newMergeRig(t).service().OfferAfterMerge(ctx, bad[0], bad[1], branchMaster)
		require.ErrorIs(t, err, ErrInvalidRequest, "%q", bad)
	}

	m := newMergeRig(t)
	m.setting.err = errGitHubDown
	_, err := m.service().OfferAfterMerge(ctx, "acme-org", sdkName, branchMaster)
	require.ErrorIs(t, err, errGitHubDown, "the setting couldn't be read")

	m = newMergeRig(t)
	m.branches.err = errGitHubDown
	_, err = m.service().OfferAfterMerge(ctx, "acme-org", sdkName, "develop")
	require.ErrorIs(t, err, errGitHubDown, "the stored default branch couldn't be read")

	got, err := m.service().OfferAfterMerge(ctx, "acme-org", sdkName, branchMaster)
	require.NoError(t, err, "master needs no default branch")
	assert.True(t, got.Offer)

	m = newMergeRig(t)
	m.clones.moveTo(filepath.Join(m.home, "gone"))
	_, err = m.service().OfferAfterMerge(ctx, "acme-org", sdkName, branchMaster)
	require.ErrorContains(t, err, "read the clone root", "a clone root that isn't there")

	m = newMergeRig(t)
	m.clones.rootErr = errGitHubDown
	_, err = m.service().OfferAfterMerge(ctx, "acme-org", sdkName, branchMaster)
	require.ErrorIs(t, err, errGitHubDown, "the clone root couldn't be read from the settings")

	m = newMergeRig(t)
	got, err = NewService(Config{GitHub: m.github, Clones: m.clones}).OfferAfterMerge(ctx, "acme-org", sdkName,
		branchMaster)
	require.NoError(t, err)
	assert.Equal(t, AfterMerge{}, got, "without the setting, a merge offers nothing")

	got, err = NewService(Config{GitHub: m.github, Clones: m.clones, AfterMerge: m.setting}).
		OfferAfterMerge(ctx, "acme-org", sdkName, branchMaster)
	require.NoError(t, err)
	assert.Equal(t, AfterMerge{}, got, "without magex's facts, a merge offers nothing")

	got, err = NewService(Config{
		GitHub: m.github, Clones: m.clones, AfterMerge: m.setting, Tools: fixedTool{facts: m.tool},
	}).OfferAfterMerge(ctx, "acme-org", sdkName, branchMaster)
	require.NoError(t, err)
	assert.Equal(t, AfterMerge{}, got, "without a way to read an origin, only the page's list serves")
	assert.Zero(t, m.clones.scans())
}

func TestOfferAfterMergeLooksInEveryLocation(t *testing.T) {
	t.Parallel()

	m := newMergeRig(t)

	work := filepath.Join(m.home, "work")
	exp := filepath.Join(m.home, "Desktop", "exp-app")

	for _, dir := range []string{filepath.Join(work, "api"), exp} {
		require.NoError(t, os.MkdirAll(dir, 0o700))
	}

	m.clones.extra = []localrepos.Location{
		{Path: filepath.Join(m.home, "gone"), Kind: localrepos.KindFolder},
		{Path: work, Kind: localrepos.KindFolder},
		{Path: exp, Kind: localrepos.KindClone},
	}
	m.origins.origins["api"] = Clone{Origin: name("api"), OnHub: true}
	m.origins.origins["exp-app"] = Clone{Origin: name("exp"), OnHub: true}
	m.branches.branches[name("api").Key()] = branchMain
	m.branches.branches[name("exp").Key()] = branchMain

	got, err := m.service().OfferAfterMerge(t.Context(), "acme-org", "api", branchMain)
	require.NoError(t, err)
	assert.Equal(t, AfterMerge{Offer: true, Repo: "acme-org/api", Dir: "~/work/api", Branch: branchMain}, got,
		"a folder location's clone named as the repository is; a missing location is passed over")
	assert.Equal(t, []string{"api"}, m.origins.gitRuns(), "only the folders named as the repository is")

	got, err = m.service().OfferAfterMerge(t.Context(), "acme-org", "exp", branchMain)
	require.NoError(t, err)
	assert.Equal(t, AfterMerge{Offer: true, Repo: "acme-org/exp", Dir: "~/Desktop/exp-app", Branch: branchMain}, got,
		"a clone location, whatever its name")
	assert.Equal(t, []string{"api", "exp-app"}, m.origins.gitRuns(), "the clone locations come last")
	assert.Zero(t, m.clones.scans(), "no location is scanned")
}
