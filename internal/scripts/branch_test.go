package scripts

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/gitread"
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
}

// newGitRig builds a gitRig, or skips the test without git.
func newGitRig(t *testing.T) *gitRig {
	t.Helper()

	testutil.RequireGit(t)

	r := newRig(t)
	g := &gitRig{
		rig: r, origin: filepath.Join(r.home, "origin.git"), dir: filepath.Join(r.root, "sdk"),
		other: filepath.Join(r.home, "other"), clock: testutil.NewClock(time.Date(2026, 11, 11, 9, 0, 0, 0, time.UTC)),
	}

	testutil.RunGit(t, r.home, "-c", "init.defaultBranch=main", "init", "-q", "--bare", g.origin)
	testutil.RunGit(t, r.home, "clone", "-q", g.url(), g.other)
	g.first = g.commit(g.other, "one.txt")
	testutil.RunGit(t, g.other, "push", "-q", "origin", "HEAD:main")
	testutil.RunGit(t, r.root, "clone", "-q", g.url(), g.dir)

	env := testutil.GitEnv(r.home)
	g.asked = &countingGit{BranchGit: gitread.New(gitread.Config{Env: func() []string { return env }})}
	g.branch = NewService(Config{
		Clones: r.clones, BranchGit: g.asked, Tools: r.tools, Settings: r.settings, Runs: r.engine, History: r.runs,
		DB: r.db, Env: func() ([]string, bool) { return env, true }, Home: r.home, Clock: g.clock,
	})
	r.engine.RegisterRerunner(scriptrun.KindPull, NewRerunner(g.branch))

	return g
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

// head returns the commit dir's HEAD is at.
func (g *gitRig) head(dir string) string {
	g.t.Helper()

	return strings.TrimSpace(testutil.RunGit(g.t, dir, "rev-parse", "HEAD"))
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

// countingGit is a BranchGit that counts the times it asks origin.
type countingGit struct {
	BranchGit

	remote atomic.Int32
}

func (c *countingGit) RemoteRefs(ctx context.Context, dir string) (gitread.RemoteRefs, error) {
	c.remote.Add(1)

	return c.BranchGit.RemoteRefs(ctx, dir)
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

	for _, tc := range []struct {
		up   gitread.Upstream
		want string
		why  string
	}{
		{up: gitread.Upstream{Remote: "origin", Ref: "refs/heads/main"}, want: "main"},
		{up: gitread.Upstream{Remote: "origin", Ref: "refs/heads/release/1.x"}, want: "release/1.x"},
		{up: gitread.Upstream{}, why: "dev doesn't track a branch on origin."},
		{up: gitread.Upstream{Remote: "origin"}, why: "dev doesn't track a branch on origin."},
		{up: gitread.Upstream{Remote: "origin", Ref: "refs/tags/v1"}, why: "dev doesn't track a branch on origin."},
		{up: gitread.Upstream{Remote: "origin", Ref: "refs/heads/-x"}, why: "dev doesn't track a branch on origin."},
		{up: gitread.Upstream{Remote: "origin", Ref: "refs/heads/a..b"}, why: "dev doesn't track a branch on origin."},
		{up: gitread.Upstream{Remote: "origin", Ref: "refs/heads/a b"}, why: "dev doesn't track a branch on origin."},
		{up: gitread.Upstream{Remote: "fork", Ref: "refs/heads/main"}, why: "dev tracks a branch on fork, not origin."},
	} {
		got, why := originBranch("dev", tc.up)
		assert.Equal(t, tc.want, got, "%+v", tc.up)
		assert.Equal(t, tc.why, why, "%+v", tc.up)
	}
}
