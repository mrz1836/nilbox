package scripts

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/integrationprefs"
	"github.com/mrz1836/nilbox/internal/repo"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
)

// gitRig is a real clone under the rig's clone root, sdk, of a local bare origin reached
// over file://, every git isolated from the user's configuration; another clone of
// origin, outside the clone root, that pushes; and a Service whose branch status reads the
// clone with gitread, counting each time it asks origin, on a clock of its own. first is
// the commit origin and the clone start at.
type gitRig struct {
	*rig

	origin string
	dir    string
	other  string
	first  string

	clock  *testutil.Clock
	asked  *countingGit
	branch *Service

	// envMu guards genv, the user's environment every git here gets: an isolated git's,
	// with the fake programs' entries.
	envMu sync.Mutex
	genv  []string
}

// newGitRig builds a gitRig, or skips the test without git.
func newGitRig(t *testing.T) *gitRig {
	t.Helper()

	testutil.RequireGit(t)

	r := newRig(t)
	g := &gitRig{
		rig: r, origin: filepath.Join(r.home, "origin.git"), dir: filepath.Join(r.root, "sdk"),
		other: filepath.Join(r.home, "other"), clock: testutil.NewClock(time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)),
		genv: testutil.GitEnv(r.home),
	}

	testutil.RunGit(t, r.home, "-c", "init.defaultBranch=main", "init", "-q", "--bare", g.origin)
	testutil.RunGit(t, r.home, "clone", "-q", g.url(), g.other)
	g.first = g.commit(g.other, "one.txt")
	testutil.RunGit(t, g.other, "push", "-q", "origin", "HEAD:main")
	testutil.RunGit(t, r.root, "clone", "-q", g.url(), g.dir)

	// The clone's origin is on GitHub, as the clones list reads it, so its status asks
	// origin unasked (a local bare repository stands in for GitHub's).
	r.clones.setOrigin(g.dir, repo.Name{Owner: "acme-org", Repo: "sdk"})

	g.asked = &countingGit{BranchGit: gitread.New(gitread.Config{Env: g.environ})}
	g.branch = NewService(Config{
		Clones: r.clones, BranchGit: g.asked, Tools: r.tools, Settings: r.settings, Runs: r.engine, History: r.runs,
		DB: r.runner.DB, Env: func() ([]string, bool) { return g.environ(), true }, Home: r.home, Clock: g.clock,
	})
	r.engine.RegisterRerunner(scriptrun.KindPull, NewRerunner(g.branch))

	return g
}

// environ is the user's environment, as the login shell gives it.
func (g *gitRig) environ() []string {
	g.envMu.Lock()
	defer g.envMu.Unlock()

	return slices.Clone(g.genv)
}

// marker makes a fake program called name that writes the file ran/<name> when it runs,
// whatever it is run for, and returns its path and that file's.
func (g *gitRig) marker(name string) (program, marker string) {
	g.t.Helper()

	ran := filepath.Join(g.home, "ran")
	require.NoError(g.t, os.MkdirAll(ran, 0o700))

	marker = filepath.Join(ran, name)
	program, entry := testutil.FakeTool(g.t, name, testutil.FakeRun{Files: []testutil.FakeFile{{Path: marker, Content: "ran"}}})

	g.envMu.Lock()
	g.genv = append(g.genv, entry)
	g.envMu.Unlock()

	return program, marker
}

// url is origin's address.
func (g *gitRig) url() string {
	return "file://" + filepath.ToSlash(g.origin)
}

// commit writes name in dir and commits it, returning the commit.
func (g *gitRig) commit(dir, name string) string {
	g.t.Helper()

	require.NoError(g.t, os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o600))
	testutil.RunGit(g.t, dir, "add", name)
	testutil.RunGit(g.t, dir, "commit", "-q", "-m", name)

	return g.head(dir)
}

// head returns the commit dir's HEAD is at, read with the fake programs' entries in git's
// environment, as nothing the clone plants may run without them.
func (g *gitRig) head(dir string) string {
	g.t.Helper()

	oid := g.ref(dir, "HEAD")
	require.NotEmpty(g.t, oid)

	return oid
}

// pushed commits name in the other clone and pushes it to origin's main, returning the
// commit.
func (g *gitRig) pushed(name string) string {
	g.t.Helper()

	oid := g.commit(g.other, name)
	testutil.RunGit(g.t, g.other, "push", "-q", "origin", "HEAD:main")

	return oid
}

// status returns the clone's branch status, asking origin again when refresh is set.
func (g *gitRig) status(refresh bool) BranchStatus {
	g.t.Helper()

	st, err := g.branch.BranchStatus(g.t.Context(), g.shown(g.dir), refresh)
	require.NoError(g.t, err)

	return st
}

// countingGit is a BranchGit that counts the times it asks origin, and runs then, once,
// after it next works out a pull's setup, as git is about to run.
type countingGit struct {
	BranchGit

	remote atomic.Int32

	mu   sync.Mutex
	then func()
}

func (c *countingGit) RemoteBranch(ctx context.Context, dir, branch string) (string, error) {
	c.remote.Add(1)

	return c.BranchGit.RemoteBranch(ctx, dir, branch)
}

func (c *countingGit) RemoteSetup(ctx context.Context, dir string) (gitread.Setup, error) {
	setup, err := c.BranchGit.RemoteSetup(ctx, dir)

	c.mu.Lock()
	then := c.then
	c.then = nil
	c.mu.Unlock()

	if then != nil {
		then()
	}

	return setup, err
}

// afterSetup runs then once the next pull's setup is worked out.
func (c *countingGit) afterSetup(then func()) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.then = then
}

func TestBranchStatusUpToDate(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	st := g.status(false)

	assert.Equal(t, BranchStatus{
		Dir: "~/projects/sdk", Branch: "main", Upstream: "main", State: BranchUpToDate, Local: g.first[:7],
		Origin: g.first[:7], Note: "main is up to date with origin/main.", CheckedAt: "2026-11-11T09:00:00Z",
	}, st)
	assert.EqualValues(t, 1, g.asked.remote.Load())
}

func TestBranchStatusBehind(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	g.status(false)

	newer := g.pushed("two.txt")

	st := g.status(true)
	assert.Equal(t, BranchBehind, st.State, "origin's newer commit isn't in the clone yet")
	assert.Equal(t, g.first[:7], st.Local)
	assert.Equal(t, newer[:7], st.Origin)
	assert.Equal(t, "origin/main has newer commits. Pull fast-forwards main to them.", st.Note)

	testutil.RunGit(t, g.dir, "fetch", "-q", "origin")
	assert.Equal(t, BranchBehind, g.status(false).State, "fetched, the branch is an ancestor of origin's")
}

func TestBranchStatusAhead(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	mine := g.commit(g.dir, "mine.txt")

	st := g.status(false)
	assert.Equal(t, BranchAhead, st.State)
	assert.Equal(t, mine[:7], st.Local)
	assert.Equal(t, "main has commits that aren't on origin/main yet, and nothing to pull.", st.Note)
}

func TestBranchStatusDiverged(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	g.commit(g.dir, "mine.txt")
	g.pushed("theirs.txt")

	assert.Equal(t, BranchBehind, g.status(false).State,
		"before a fetch only the pull can tell, and it refuses to do more than fast-forward")

	testutil.RunGit(t, g.dir, "fetch", "-q", "origin")

	st := g.status(false)
	assert.Equal(t, BranchDiverged, st.State)
	assert.Equal(t, "main and origin/main both have commits the other doesn't, so it can't be fast-forwarded.", st.Note)
}

func TestBranchStatusWithoutABranchOnOrigin(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	testutil.RunGit(t, g.dir, "checkout", "-q", "-b", "feature")

	st := g.status(false)
	assert.Equal(t, BranchNoUpstream, st.State)
	assert.Equal(t, "feature", st.Branch)
	assert.Empty(t, st.Upstream)
	assert.Equal(t, "feature doesn't track a branch on origin.", st.Note)
	assert.Zero(t, g.asked.remote.Load(), "nothing to ask origin about")

	testutil.RunGit(t, g.dir, "config", "branch.feature.remote", "upstream")
	testutil.RunGit(t, g.dir, "config", "branch.feature.merge", "refs/heads/main")
	assert.Equal(t, "feature tracks a branch on upstream, not origin.", g.status(false).Note)

	testutil.RunGit(t, g.dir, "config", "branch.feature.remote", ".")
	assert.Equal(t, "feature tracks another local branch, not one on origin.", g.status(false).Note)

	testutil.RunGit(t, g.dir, "config", "branch.feature.remote", "origin")
	testutil.RunGit(t, g.dir, "config", "branch.feature.merge", "refs/heads/gone")

	st = g.status(false)
	assert.Equal(t, BranchNoUpstream, st.State)
	assert.Equal(t, "gone", st.Upstream)
	assert.Equal(t, "origin has no branch gone.", st.Note)
}

func TestBranchStatusDetached(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	testutil.RunGit(t, g.dir, "checkout", "-q", "--detach")

	st := g.status(false)
	assert.Equal(t, BranchDetached, st.State)
	assert.Empty(t, st.Branch)
	assert.Equal(t, "HEAD is detached, so there's no branch to pull.", st.Note)
	assert.Zero(t, g.asked.remote.Load())
}

func TestBranchStatusInTheMiddleOfAMerge(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	require.NoError(t, os.WriteFile(filepath.Join(g.dir, ".git", "MERGE_HEAD"), []byte(g.first+"\n"), 0o600))

	st := g.status(false)
	assert.Equal(t, BranchInProgress, st.State)
	assert.Equal(t, "git is merging here. Finish or abort that before pulling.", st.Note)
}

func TestBranchStatusKeepsOriginsAnswerFiveMinutes(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	require.Equal(t, BranchUpToDate, g.status(false).State)

	newer := g.pushed("two.txt")

	g.clock.Advance(OriginKept - time.Second)

	st := g.status(false)
	assert.Equal(t, BranchUpToDate, st.State, "origin's answer is kept")
	assert.Equal(t, "2026-11-11T09:00:00Z", st.CheckedAt)
	assert.EqualValues(t, 1, g.asked.remote.Load(), "and origin isn't asked again")

	st = g.status(true)
	assert.Equal(t, BranchBehind, st.State, "a refresh asks again")
	assert.Equal(t, newer[:7], st.Origin)
	assert.EqualValues(t, 2, g.asked.remote.Load())

	g.commit(g.dir, "mine.txt")
	assert.Equal(t, BranchBehind, g.status(false).State, "the clone is read again every time, origin isn't")
	assert.EqualValues(t, 2, g.asked.remote.Load())

	g.clock.Advance(OriginKept)
	g.status(false)
	assert.EqualValues(t, 3, g.asked.remote.Load(), "an answer five minutes old is asked again")
}

func TestBranchStatusAsksAnOriginOffGitHubOnlyWhenTheUserDoes(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	g.clones.mu.Lock()
	delete(g.clones.origins, g.dir)
	g.clones.mu.Unlock()

	st := g.status(false)
	assert.Equal(t, BranchNotAsked, st.State, "an origin off GitHub isn't asked unasked")
	assert.Equal(t, "origin isn't on GitHub, so it's asked only when you check.", st.Note)
	assert.Equal(t, "main", st.Branch)
	assert.Empty(t, st.CheckedAt)
	assert.Zero(t, g.asked.remote.Load(), "nothing leaves the Mac")

	st = g.status(true)
	assert.Equal(t, BranchUpToDate, st.State, "the user's check asks it")
	assert.EqualValues(t, 1, g.asked.remote.Load())

	assert.Equal(t, BranchUpToDate, g.status(false).State, "and its answer is served while it is kept")
	assert.EqualValues(t, 1, g.asked.remote.Load())

	g.clock.Advance(OriginKept)
	assert.Equal(t, BranchNotAsked, g.status(false).State, "once it is old, the next read asks nothing again")
	assert.EqualValues(t, 1, g.asked.remote.Load())
}

func TestBranchStatusSaysWhyOriginCantBeAsked(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	testutil.RunGit(t, g.dir, "remote", "set-url", "origin", "file://"+filepath.ToSlash(filepath.Join(g.home, "gone.git")))

	st := g.status(false)
	assert.Equal(t, BranchUnknown, st.State)
	assert.Equal(t, "main", st.Branch)
	assert.Contains(t, st.Note, "Couldn't ask origin about main: ")
	assert.Contains(t, st.Note, "gone.git")
	assert.Empty(t, st.CheckedAt)

	testutil.RunGit(t, g.dir, "remote", "set-url", "origin", g.url())
	assert.Equal(t, BranchUpToDate, g.status(false).State, "a failure isn't kept")
}

func TestBranchStatusReadsNothingWhileScriptsAreOff(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	g.settings.scripts(integrationprefs.SwitchOff)

	assert.Equal(t, BranchStatus{
		Dir: "~/projects/sdk", State: BranchOff, Note: "Scripts are turned off in Settings → Integrations.",
	}, g.status(true))
	assert.Zero(t, g.asked.remote.Load(), "origin isn't asked")

	_, err := g.branch.Pull(t.Context(), g.shown(g.dir))
	require.ErrorIs(t, err, ErrCantRun)
}

// TestBranchStatusOffersNoPullItWouldRefuse reads a clone behind origin whose own config
// holds a hook, which a pull's Before refuses: the status says so, from the clone alone,
// and offers no pull; once the setting is gone, it does.
func TestBranchStatusOffersNoPullItWouldRefuse(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	g.pushed("two.txt")
	testutil.RunGit(t, g.dir, "config", "hook.evil.command", "touch /nonexistent/owned")

	st := g.status(true)
	assert.Equal(t, BranchRefused, st.State)
	assert.Equal(t, "main", st.Branch)
	assert.Equal(t, "This clone's own git config sets hook.evil.command, which isn't run with here: a clone's own "+
		"config may hold only what git and Git LFS write there. Set it for this folder in your global git config, "+
		"with an includeIf, instead.", st.Note)
	assert.Zero(t, g.asked.remote.Load(), "origin isn't asked")

	_, err := g.branch.Pull(t.Context(), g.shown(g.dir))
	require.ErrorIs(t, err, ErrCantPull, "the pull refuses it all the same")

	testutil.RunGit(t, g.dir, "config", "core.sshCommand", "my-ssh")
	testutil.RunGit(t, g.dir, "config", "--unset", "hook.evil.command")
	assert.Equal(t, BranchBehind, g.status(false).State, "a setting the pull sets aside offers it")
}

func TestBranchStatusOnlyInAClone(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)

	_, err := g.branch.BranchStatus(t.Context(), "~/other", false)
	require.ErrorIs(t, err, ErrNoClone)

	_, err = NewService(Config{Clones: g.clones}).BranchStatus(t.Context(), g.shown(g.dir), false)
	require.ErrorIs(t, err, ErrNotSetUp)
}

func TestOriginBranch(t *testing.T) {
	t.Parallel()

	const notTracking = "dev doesn't track a branch on origin."

	for _, tc := range []struct {
		up   gitread.Upstream
		want string
		why  string
	}{
		{up: gitread.Upstream{Remote: originRemote, Ref: "refs/heads/main"}, want: "main"},
		{up: gitread.Upstream{Remote: originRemote, Ref: "refs/heads/release/1.x"}, want: "release/1.x"},
		{up: gitread.Upstream{}, why: notTracking},
		{up: gitread.Upstream{Remote: originRemote}, why: notTracking},
		{up: gitread.Upstream{Remote: originRemote, Ref: "refs/tags/v1"}, why: notTracking},
		{up: gitread.Upstream{Remote: originRemote, Ref: "refs/heads/-x"}, why: notTracking},
		{up: gitread.Upstream{Remote: originRemote, Ref: "refs/heads/a..b"}, why: notTracking},
		{up: gitread.Upstream{Remote: originRemote, Ref: "refs/heads/a b"}, why: notTracking},
		{up: gitread.Upstream{Remote: originRemote, Ref: "refs/heads/a:b"}, why: notTracking},
		{up: gitread.Upstream{Remote: originRemote, Ref: "refs/heads/a*"}, why: notTracking},
		{up: gitread.Upstream{Remote: originRemote, Ref: "refs/heads/a^"}, why: notTracking},
		{up: gitread.Upstream{Remote: originRemote, Ref: "refs/heads/x.lock"}, why: notTracking},
		{up: gitread.Upstream{Remote: "fork", Ref: "refs/heads/main"}, why: "dev tracks a branch on fork, not origin."},
	} {
		got, why := originBranch("dev", tc.up)
		assert.Equal(t, tc.want, got, "%+v", tc.up)
		assert.Equal(t, tc.why, why, "%+v", tc.up)
	}
}
