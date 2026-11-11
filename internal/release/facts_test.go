package release

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/clonelock"
	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/integrations"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// errOriginUnread is an origin the clone's config doesn't give.
var errOriginUnread = errors.New("config unreadable")

// fakeOrigins says every clone's origin is name. asking, when set, is called as each
// clone's origin is read.
type fakeOrigins struct {
	name     repo.Name
	onGitHub bool
	err      error
	asking   func()
}

func (f fakeOrigins) Origin(context.Context, string) (repo.Name, bool, error) {
	if f.asking != nil {
		f.asking()
	}

	return f.name, f.onGitHub, f.err
}

// fakeTools is magex as the integrations see it.
type fakeTools ToolFacts

func (f fakeTools) ReleaseTool(context.Context) ToolFacts { return ToolFacts(f) }

// errToolExit is ssh-add or ssh-keygen exiting non-zero.
var errToolExit = errors.New("exit status 1")

// sshFake stands in for ssh-add and ssh-keygen, recording each run.
type sshFake struct {
	// agent is what `ssh-add -L` lists; empty, it fails as an agent without keys does.
	agent string

	// opens reports `ssh-keygen -y` opens the key without a passphrase.
	opens bool

	mu    sync.Mutex
	calls [][]string
	envs  [][]string
}

// runner returns a Runner over the fake, as Gatherer's Runner config.
func (s *sshFake) runner(env []string) execpath.Runner {
	return sshRun{fake: s, env: env}
}

// ran returns the runs so far.
func (s *sshFake) ran() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return append([][]string{}, s.calls...)
}

// sshRun is one Runner over an sshFake, with the environment it was made with.
type sshRun struct {
	fake *sshFake
	env  []string
}

func (r sshRun) Run(_ context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	r.fake.mu.Lock()
	defer r.fake.mu.Unlock()

	r.fake.calls = append(r.fake.calls, append([]string{name}, args...))
	r.fake.envs = append(r.fake.envs, r.env)

	switch {
	case name == "ssh-add" && r.fake.agent != "":
		return []byte(r.fake.agent), nil, nil
	case name == "ssh-keygen" && r.fake.opens:
		return []byte("ssh-ed25519 AAAAopen\n"), nil, nil
	default:
		return nil, []byte("no\n"), errToolExit
	}
}

// gatherRig is a clone of a local bare origin, isolated from the user's git config, and
// the fakes a Gatherer reads GitHub, magex, the clone's origin, and ssh through. origin's
// master has first, tagged v1.0.0, and second; the clone is on master at second. root is
// also the home folder.
type gatherRig struct {
	root   string
	env    []string
	origin string
	clone  string
	first  string
	second string

	github    *fakeGitHub
	origins   fakeOrigins
	tool      ToolFacts
	locks     *clonelock.Set
	ssh       *sshFake
	shellRead bool
}

// realTempDir returns a fresh folder with its symbolic links resolved, as git prints it.
func realTempDir(t *testing.T) string {
	t.Helper()

	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)

	return dir
}

// newGatherRig builds a gatherRig in a fresh folder, or skips the test without git.
func newGatherRig(t *testing.T) *gatherRig {
	t.Helper()

	r := &gatherRig{env: testutil.Git(t), root: realTempDir(t), locks: clonelock.New(), ssh: &sshFake{}, shellRead: true}
	r.origin, r.clone = filepath.Join(r.root, "origin.git"), filepath.Join(r.root, "sdk")

	testutil.RunGit(t, r.root, "-c", "init.defaultBranch=master", "init", "-q", "--bare", r.origin)
	testutil.RunGit(t, r.root, "-c", "init.defaultBranch=master", "init", "-q", r.clone)
	r.first = commitIn(t, r.clone, "one.txt")
	testutil.RunGit(t, r.clone, "tag", "-a", "v1.0.0", "-m", "v1.0.0")
	r.second = commitIn(t, r.clone, "two.txt")
	testutil.RunGit(t, r.clone, "remote", "add", "origin", r.origin)
	testutil.RunGit(t, r.clone, "push", "-q", "-u", "origin", branchMaster, "--tags")

	r.github = &fakeGitHub{statuses: map[string]RepoStatus{name("sdk").Key(): {
		Permission: "WRITE", DefaultBranch: branchMaster, HeadOID: r.second, Checks: ChecksSuccess,
	}}}
	r.origins = fakeOrigins{name: name("sdk"), onGitHub: true}
	r.tool = ToolFacts{
		Path: "/opt/tools/magex", Version: "1.30.2", State: integrations.StateFound, Features: allFeatures(),
		MinVersion: "1.29.5", ExpectSince: "1.30.0",
	}

	return r
}

// gatherer returns a Gatherer over the rig's clone and fakes.
func (r *gatherRig) gatherer() *Gatherer {
	return NewGatherer(GatherConfig{
		Git:     gitread.New(gitread.Config{Env: func() []string { return r.env }}),
		GitHub:  r.github,
		Origins: r.origins,
		Tools:   fakeTools(r.tool),
		Locks:   r.locks,
		Env: func() ([]string, bool) {
			return []string{"SSH_AUTH_SOCK=/tmp/agent.sock", "PATH=/usr/bin"}, r.shellRead
		},
		Runner: r.ssh.runner,
		Home:   r.root,
	})
}

// gather gathers the rig's clone's facts for choice.
func (r *gatherRig) gather(t *testing.T, choice Choice) Facts {
	t.Helper()

	f, err := r.gatherer().Gather(t.Context(), GatherRequest{Repo: name("sdk"), Dir: r.clone, Choice: choice})
	require.NoError(t, err)

	return f
}

// pushElsewhere commits in a second clone of origin and pushes it, returning the commit.
func (r *gatherRig) pushElsewhere(t *testing.T) string {
	t.Helper()

	other := filepath.Join(r.root, "elsewhere")
	testutil.RunGit(t, r.root, "clone", "-q", r.origin, other)
	oid := commitIn(t, other, "elsewhere.txt")
	testutil.RunGit(t, other, "push", "-q", "origin", branchMaster)

	return oid
}

// writeIn writes text to name under dir with mode.
func writeIn(t *testing.T, dir, name, text string, mode os.FileMode) {
	t.Helper()

	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(text), mode))
	require.NoError(t, os.Chmod(path, mode))
}

func TestGatherACleanClone(t *testing.T) {
	t.Parallel()

	r := newGatherRig(t)
	f := r.gather(t, Choice{RunsIn: integrationprefs.RunsInApp})

	assert.Equal(t, r.tool, f.Tool)
	assert.Equal(t, RepoFacts{
		Name: name("sdk"), Found: true, Permission: "WRITE", DefaultBranch: branchMaster, HeadOID: r.second,
		Checks: ChecksSuccess, Branches: []string{branchMaster},
	}, f.Repo)

	c := f.Clone
	assert.Empty(t, c.Error)
	assert.True(t, c.OriginMatches)
	assert.Equal(t, gitread.OperationNone, c.InProgress)
	assert.Equal(t, []string{}, c.Changes)
	assert.Equal(t, branchMaster, c.Branch)
	assert.False(t, c.Detached)
	assert.Equal(t, []string{}, c.Elsewhere)
	assert.Equal(t, r.second, c.LocalHead)
	assert.Equal(t, r.second, c.TrackingHead)
	assert.Equal(t, gitread.Upstream{Remote: "origin", Ref: "refs/heads/master"}, c.Upstream)
	assert.Zero(t, c.Unpushed)
	assert.False(t, c.Behind)
	assert.True(t, c.Reached)
	assert.Empty(t, c.ReachError)
	assert.Equal(t, map[string]string{branchMaster: r.second}, c.RemoteHeads)
	require.Contains(t, c.RemoteTags, "v1.0.0")
	assert.Equal(t, r.first, c.RemoteTags["v1.0.0"].Commit, "the annotated tag, peeled")
	assert.Equal(t, []string{"v1.0.0"}, c.LocalTags)
	assert.Equal(t, SigningFacts{}, c.Signing)
	assert.Equal(t, []string{}, c.Hooks)
	assert.True(t, c.ShellRead)
	assert.False(t, c.Busy)
	assert.Empty(t, c.ModulePath)
	assert.Empty(t, r.ssh.ran(), "a clone that doesn't sign runs no ssh tool")

	o := Decide(f)
	assert.True(t, o.Ready, "%+v", o.Checks)
	assert.Equal(t, "v1.0.1", o.Version)
	assert.Equal(t, r.second, o.Head)
}

func TestGatherFindsWhatMatters(t *testing.T) {
	t.Parallel()

	r := newGatherRig(t)
	other := filepath.Join(r.root, "other")

	testutil.RunGit(t, r.clone, "checkout", "-q", "-b", "feature-x")
	testutil.RunGit(t, r.clone, "worktree", "add", "-q", other, branchMaster)
	third := commitIn(t, other, "three.txt")
	testutil.RunGit(t, other, "tag", "v1.1.0")

	writeIn(t, r.clone, "one.txt", "changed\n", 0o600)
	writeIn(t, r.clone, "go.mod", "module github.com/acme-org/sdk // the SDK\n\ngo 1.26\n", 0o600)

	hooks := filepath.Join(r.clone, ".git", "hooks")
	writeIn(t, hooks, "pre-push", "#!/bin/sh\n", 0o700)
	writeIn(t, hooks, "post-checkout", "#!/bin/sh\n", 0o600) // not executable: git skips it

	f := r.gather(t, Choice{})
	c := f.Clone

	assert.Empty(t, c.Error)
	assert.Equal(t, "feature-x", c.Branch)
	assert.ElementsMatch(t, []string{"one.txt", "go.mod"}, c.Changes)
	assert.Equal(t, []string{"~/other"}, c.Elsewhere)
	assert.Equal(t, third, c.LocalHead)
	assert.Equal(t, 1, c.Unpushed)
	assert.False(t, c.Behind)
	assert.ElementsMatch(t, []string{"v1.0.0", "v1.1.0"}, c.LocalTags)
	assert.Equal(t, []string{hookPrePush}, c.Hooks)
	assert.Equal(t, "github.com/acme-org/sdk", c.ModulePath)

	o := Decide(f)
	assert.False(t, o.Ready)

	for _, id := range []CheckID{CheckDirty, CheckWhere, CheckUnpushed, CheckLocalOnly} {
		got, ok := checkOf(o, id)
		if assert.True(t, ok, "%s", id) {
			assert.Equal(t, CheckBlock, got.State, "%s", id)
		}
	}

	hook, ok := checkOf(o, CheckHooks)
	require.True(t, ok)
	assert.Equal(t, CheckInfo, hook.State)
}

func TestGatherSeesItIsBehind(t *testing.T) {
	t.Parallel()

	r := newGatherRig(t)
	ahead := r.pushElsewhere(t)

	f := r.gather(t, Choice{})
	assert.Equal(t, ahead, f.Clone.RemoteHeads[branchMaster])
	assert.True(t, f.Clone.Behind, "origin has a commit the clone hasn't fetched")
	assert.Zero(t, f.Clone.Unpushed)
	assert.Equal(t, ahead, Decide(f).Head)

	testutil.RunGit(t, r.clone, "fetch", "-q", "origin")

	f = r.gather(t, Choice{})
	assert.True(t, f.Clone.Behind, "fetched but not pulled")

	got, ok := checkOf(Decide(f), CheckBehind)
	require.True(t, ok)
	assert.Equal(t, CheckOK, got.State)
}

func TestGatherWithoutOriginsBranchInTheClone(t *testing.T) {
	t.Parallel()

	r := newGatherRig(t)
	testutil.RunGit(t, r.clone, "update-ref", "-d", "refs/remotes/origin/master")

	f := r.gather(t, Choice{})
	assert.Empty(t, f.Clone.TrackingHead)
	assert.Zero(t, f.Clone.Unpushed, "the clone's head is origin's")

	commitIn(t, r.clone, "mine.txt")

	f = r.gather(t, Choice{})
	assert.Equal(t, UnpushedSome, f.Clone.Unpushed, "a commit origin's head lacks, uncounted")

	r.pushElsewhere(t)

	f = r.gather(t, Choice{})
	assert.Equal(t, UnpushedUnknown, f.Clone.Unpushed, "the clone has neither ref to compare with")
	assert.True(t, f.Clone.Behind)
}

func TestGatherWithOnlyOriginsBranch(t *testing.T) {
	t.Parallel()

	r := newGatherRig(t)
	testutil.RunGit(t, r.clone, "checkout", "-q", "-b", "feature-x")
	testutil.RunGit(t, r.clone, "update-ref", "-d", "refs/heads/master")

	f := r.gather(t, Choice{})
	assert.Empty(t, f.Clone.LocalHead)
	assert.Equal(t, r.second, f.Clone.TrackingHead)
	assert.Zero(t, f.Clone.Unpushed)
	assert.False(t, f.Clone.Behind)

	o := Decide(f)
	_, blocked := checkOf(o, CheckBranch)
	assert.False(t, blocked, "magex makes master from origin/master")
	assert.True(t, o.Ready, "%+v", o.Checks)
	assert.Equal(t, "magex switches to master, pulls, tags the newest commit as v1.0.1, pushes the tag to origin, "+
		"and switches back to feature-x. It runs here in "+brand.Name+".", o.Sentence)
}

func TestGatherASetRightStaleCount(t *testing.T) {
	t.Parallel()

	r := newGatherRig(t)

	// The clone's master gains a commit that reaches origin while origin/master in the
	// clone stays where it was, as when another clone pushed the same commit.
	mine := commitIn(t, r.clone, "mine.txt")
	testutil.RunGit(t, r.clone, "push", "-q", "origin", mine+":refs/heads/master")
	testutil.RunGit(t, r.clone, "update-ref", "refs/remotes/origin/master", r.second)

	f := r.gather(t, Choice{})
	assert.Equal(t, r.second, f.Clone.TrackingHead)
	assert.Zero(t, f.Clone.Unpushed, "origin has the commit, though origin/master doesn't say so yet")
}

func TestGatherWhenOriginCantBeReached(t *testing.T) {
	t.Parallel()

	r := newGatherRig(t)
	testutil.RunGit(t, r.clone, "remote", "set-url", "origin", filepath.Join(r.root, "missing.git"))

	f := r.gather(t, Choice{})
	assert.False(t, f.Clone.Reached)
	assert.Contains(t, f.Clone.ReachError, "does not appear to be a git repository")
	assert.Equal(t, map[string]string{}, f.Clone.RemoteHeads)
	assert.Equal(t, []string{}, f.Repo.Branches)
	assert.Empty(t, f.Clone.Error, "the clone itself reads")

	o := Decide(f)
	got, ok := checkOf(o, CheckReach)
	require.True(t, ok)
	assert.Equal(t, CheckBlock, got.State)
	assert.Equal(t, r.second, o.Head, "GitHub's newest commit")
}

func TestGatherWhenGitHubFails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "down", err: errGitHubDown, want: "Couldn't read acme-org/sdk on GitHub."},
		{
			name: "signed out", err: fmt.Errorf("token: %w", ErrSignedOut),
			want: "Sign in to GitHub first, so Release can read acme-org/sdk.",
		},
		{name: "worded", err: uierr.New("GitHub is busy.", errGitHubDown), want: "GitHub is busy."},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := newGatherRig(t)
			r.github.err = tc.err

			f := r.gather(t, Choice{})
			assert.Equal(t, tc.want, f.Repo.Error)
			assert.Empty(t, f.Clone.LocalHead, "no branch to read without GitHub's default")
			assert.True(t, f.Clone.Reached)

			f = r.gather(t, Choice{Branch: branchMaster})
			assert.Equal(t, r.second, f.Clone.LocalHead, "a chosen branch is read all the same")
		})
	}

	t.Run("not found", func(t *testing.T) {
		t.Parallel()

		r := newGatherRig(t)
		r.github.statuses = map[string]RepoStatus{}

		f := r.gather(t, Choice{})
		assert.False(t, f.Repo.Found)
		assert.Empty(t, f.Repo.Error)
	})
}

func TestGatherSeesTheLock(t *testing.T) {
	t.Parallel()

	r := newGatherRig(t)
	holder := clonelock.Holder{Kind: clonelock.KindHooks, Repo: "acme-org/sdk", RunID: 3}

	release, _, ok := r.locks.TryLock(r.clone, holder)
	require.True(t, ok)
	t.Cleanup(release)

	f := r.gather(t, Choice{})
	assert.True(t, f.Clone.Busy)
	assert.Equal(t, holder, f.Clone.Holder)

	f, err := r.gatherer().Gather(t.Context(), GatherRequest{Repo: name("sdk"), Dir: r.clone, Locked: true})
	require.NoError(t, err)
	assert.False(t, f.Clone.Busy, "the caller's own lock isn't another job's")
}

func TestGatherAFolderThatIsntAClone(t *testing.T) {
	t.Parallel()

	r := newGatherRig(t)

	f, err := r.gatherer().Gather(t.Context(), GatherRequest{Repo: name("sdk"), Dir: realTempDir(t)})
	require.NoError(t, err, "what git can't read is a fact, not a failure")
	assert.NotEmpty(t, f.Clone.Error)
	assert.False(t, f.Clone.Reached)
	assert.Equal(t, []string{}, f.Clone.Changes)

	got, ok := checkOf(Decide(f), CheckOrigin)
	require.True(t, ok)
	assert.Equal(t, CheckBlock, got.State)
}

func TestGatherDetachedInTheMiddleOfAMerge(t *testing.T) {
	t.Parallel()

	r := newGatherRig(t)
	testutil.RunGit(t, r.clone, "checkout", "-q", "--detach", r.first)
	writeIn(t, filepath.Join(r.clone, ".git"), "MERGE_HEAD", r.second+"\n", 0o600)

	f := r.gather(t, Choice{})
	assert.True(t, f.Clone.Detached)
	assert.Empty(t, f.Clone.Branch)
	assert.Equal(t, gitread.OperationMerging, f.Clone.InProgress)
}

func TestGatherTheOrigin(t *testing.T) {
	t.Parallel()

	t.Run("another repository", func(t *testing.T) {
		t.Parallel()

		r := newGatherRig(t)
		r.origins = fakeOrigins{name: name("other"), onGitHub: true}

		f := r.gather(t, Choice{})
		assert.False(t, f.Clone.OriginMatches)
		assert.Empty(t, f.Clone.Error)
	})

	t.Run("not on GitHub", func(t *testing.T) {
		t.Parallel()

		r := newGatherRig(t)
		r.origins = fakeOrigins{name: name("sdk")}

		assert.False(t, r.gather(t, Choice{}).Clone.OriginMatches)
	})

	t.Run("unreadable", func(t *testing.T) {
		t.Parallel()

		r := newGatherRig(t)
		r.origins = fakeOrigins{err: errOriginUnread}

		f := r.gather(t, Choice{})
		assert.Equal(t, errOriginUnread.Error(), f.Clone.Error)
	})
}

func TestGatherWithoutMagexOrTheShell(t *testing.T) {
	t.Parallel()

	r := newGatherRig(t)
	r.shellRead = false

	g := r.gatherer()
	g.tools = nil

	f, err := g.Gather(t.Context(), GatherRequest{Repo: name("sdk"), Dir: r.clone})
	require.NoError(t, err)
	assert.Equal(t, ToolFacts{Features: []integrations.Feature{}}, f.Tool)
	assert.False(t, f.Clone.ShellRead)
}

func TestGatherMarksWhatGitCantRead(t *testing.T) {
	t.Parallel()

	r := newGatherRig(t)

	for _, step := range []string{"worktree", "refs/remotes/", "branch.master.merge", "rev-list", "^{commit}", "merge-base"} {
		t.Run(step, func(t *testing.T) {
			t.Parallel()

			g := r.gatherer()
			g.git = gitread.New(gitread.Config{
				Env: func() []string { return r.env },
				Runner: func(e []string) execpath.Runner {
					return failingRunner{real: execpath.Exec{Env: e}, failOn: step}
				},
			})

			f, err := g.Gather(t.Context(), GatherRequest{Repo: name("sdk"), Dir: r.clone})
			require.NoError(t, err)
			assert.Equal(t, step, f.Clone.Error, "what git said when %s failed", step)
		})
	}
}

func TestGatherStopsWithItsContext(t *testing.T) {
	t.Parallel()

	r := newGatherRig(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := r.gatherer().Gather(ctx, GatherRequest{Repo: name("sdk"), Dir: r.clone})
	require.ErrorIs(t, err, context.Canceled)
}

func TestNewGathererDefaults(t *testing.T) {
	t.Parallel()

	g := NewGatherer(GatherConfig{Home: "/work"})

	env, read := g.env()
	assert.True(t, read)
	assert.Equal(t, os.Environ(), env)
	assert.IsType(t, execpath.Exec{}, g.runner([]string{"PATH=/usr/bin", "HOME=/work"}))
}

func TestToolFactsOf(t *testing.T) {
	t.Parallel()

	def := integrations.MageX()
	tool := integrations.Tool{
		State: integrations.StateFound, On: true,
		Copy: integrations.Copy{Path: "/opt/tools/magex", Version: integrations.Version{Raw: "1.30.1"}},
	}

	got := ToolFactsOf(def, tool)
	assert.Equal(t, ToolFacts{
		Path: "/opt/tools/magex", Version: "1.30.1", State: integrations.StateFound,
		Features: []integrations.Feature{
			integrations.FeatureBumpJSON, integrations.FeatureExpect, integrations.FeaturePushFails,
			integrations.FeatureSelfUpdate,
		},
		MinVersion: def.MinVersion, ExpectSince: def.Features[integrations.FeatureExpect],
	}, got)
	assert.True(t, got.Usable())
	assert.True(t, got.Has(integrations.FeatureExpect))
	assert.False(t, got.Has(integrations.FeatureFastForwardPull))

	tool.Switch, tool.On = integrationprefs.SwitchOff, false
	got = ToolFactsOf(def, tool)
	assert.True(t, got.Off)
	assert.False(t, got.Usable())
	assert.False(t, got.Has(integrations.FeatureExpect), "a magex that is off has nothing")

	// Left automatic, a copy too old to use isn't on, but the user didn't turn it off: the
	// sheet says it is too old, and how to update it.
	old := integrations.Tool{
		Switch: integrationprefs.SwitchAuto, State: integrations.StateTooOld, On: false,
		Copy: integrations.Copy{Path: "/opt/tools/magex", Version: integrations.Version{Raw: "1.27.0"}},
	}
	got = ToolFactsOf(def, old)
	assert.False(t, got.Off)
	assert.False(t, got.Usable())

	check, blocks := decider{f: Facts{Tool: got}}.toolCheck()
	require.True(t, blocks)
	assert.Equal(t, []Fix{FixUpdate}, check.Fixes)
	assert.Contains(t, check.Text, "magex 1.27.0 is older than")

	// Turned on, a missing copy isn't off either.
	got = ToolFactsOf(def, integrations.Tool{Switch: integrationprefs.SwitchOn, State: integrations.StateMissing, On: true})
	assert.False(t, got.Off)
	assert.False(t, got.Usable())

	got = ToolFactsOf(def, integrations.Tool{State: integrations.StateMissing, On: true})
	assert.Equal(t, []integrations.Feature{}, got.Features)
	assert.False(t, got.Usable())
}

func TestParseModulePath(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"module github.com/acme-org/sdk\n\ngo 1.26\n":   "github.com/acme-org/sdk",
		"// the SDK\nmodule github.com/acme-org/sdk/v2": "github.com/acme-org/sdk/v2",
		"module \"example.com/quoted\" // why not\n":    "example.com/quoted",
		"module\texample.com/tabbed\n":                  "example.com/tabbed",
		"modules example.com/not\n":                     "",
		"go 1.26\n":                                     "",
		"":                                              "",
	}

	for in, want := range tests {
		assert.Equal(t, want, parseModulePath([]byte(in)), "%q", in)
	}
}

func TestGitMessage(t *testing.T) {
	t.Parallel()

	inner := fmt.Errorf("run git: %w", errToolExit)

	tests := []struct {
		err  error
		want string
	}{
		{err: fmt.Errorf("git ls-remote: Permission denied (publickey).: %w", inner), want: "Permission denied (publickey)."},
		{err: fmt.Errorf("git status: %w", inner), want: "git status"},
		{
			err:  fmt.Errorf("git ls-remote: %w", fmt.Errorf("%w: %w", context.DeadlineExceeded, inner)),
			want: "origin didn't answer within " + gitread.RemoteTimeout.String(),
		},
		{err: errOriginUnread, want: errOriginUnread.Error()},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.want, gitMessage(tc.err), "%v", tc.err)
	}
}
