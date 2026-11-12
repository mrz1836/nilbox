package worktrees

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/gitread"
	"github.com/mrz1836/nilbox/internal/item"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// guardRig is a remoteFixture whose bare remote also has pull request 9, whose head names
// the filter driver evil for every text file in its .gitattributes, and a Maker whose
// fetches and checkouts run under gitread's guards, with git's environment holding the
// fake programs' entries.
type guardRig struct {
	t *testing.T
	e *env
	f remoteFixture

	// plants are the programs a test plants in a clone's config, and git's environment.
	plants *testutil.Planter

	// names are the programs the Maker's git ran.
	names recorded
}

// newGuardRig builds a guardRig, or skips the test without git.
func newGuardRig(t *testing.T) *guardRig {
	t.Helper()

	e := newEnv(t)
	r := &guardRig{t: t, e: e, f: newRemoteFixture(t, e), plants: testutil.NewPlanter(t, e.home)}

	work := filepath.Join(e.home, "pr-9")
	testutil.RunGit(t, e.home, "clone", "-q", r.f.bare, work)
	require.NoError(t, os.WriteFile(filepath.Join(work, ".gitattributes"), []byte("*.txt filter=evil\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(work, "a.txt"), []byte("a\n"), 0o600))
	testutil.RunGit(t, work, "add", ".gitattributes", "a.txt")
	testutil.RunGit(t, work, "commit", "-q", "-m", "attributes")
	testutil.RunGit(t, work, "push", "-q", "origin", "HEAD:refs/pull/9/head")

	return r
}

// maker returns a Maker whose guarded commands run under gitread's guards, recording the
// program every command runs, wired as core wires it: git found with gitread's own finder.
func (r *guardRig) maker() *Maker {
	return r.makerWith(gitread.New(gitread.Config{Env: r.plants.Environ}))
}

// guard is the guard core would give the Maker: gitread over the rig's environment.
func (r *guardRig) guard() *gitread.Git {
	return gitread.New(gitread.Config{Env: r.plants.Environ})
}

// makerWith returns maker's Maker over guard.
func (r *guardRig) makerWith(guard Guard) *Maker {
	git := namedRunner{run: execpath.Exec{Env: r.plants.Environ()}, names: &r.names}

	return NewMaker(NewService(Config{
		Settings: fakeSettings{root: cloneRoot}, Git: git, Finder: r.guard().GitFinder(), Guard: guard,
		GitWith: func(env []string) execpath.Runner {
			return namedRunner{run: execpath.Exec{Env: env}, names: &r.names}
		},
		Home: r.e.home, Clock: testutil.NewClock(listedAt()), Logger: slog.New(slog.DiscardHandler),
	}))
}

// namedRunner runs as run does, recording each program's name.
type namedRunner struct {
	run   execpath.Runner
	names *recorded
}

func (n namedRunner) Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, err error) {
	n.names.add(name)

	return n.run.Run(ctx, name, args...)
}

// recorded is a list of names, safe for concurrent use.
type recorded struct {
	mu    sync.Mutex
	names []string
}

func (r *recorded) add(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.names = append(r.names, name)
}

func (r *recorded) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.names...)
}

// refusedPlantings are every way a planted clone config could make a fetch or a checkout
// run its own program that no setup sets aside, each of which refuses worktree creation
// before git runs: a filter driver, git's upload-pack, an alternate refs command, a hook
// named in the config (which core.hooksPath doesn't reach), gc's recent objects hook,
// which the automatic maintenance a fetch starts runs, and Git LFS's own transfer agent.
func refusedPlantings() []testutil.Planting {
	return []testutil.Planting{
		{Name: "evil-smudge", Settings: [][2]string{
			{"filter.evil.smudge", "%s"}, {"filter.evil.clean", "%s"}, {"filter.evil.required", "true"},
		}},
		{Name: "evil-upload-pack", Settings: [][2]string{{"remote.origin.uploadpack", "%s"}}},
		{Name: testutil.AlternateRefs, Settings: [][2]string{{"core.alternateRefsCommand", "%s"}}},
		{Name: "evil-hook", Settings: [][2]string{{"hook.evil.command", "%s"}, {"hook.evil.event", "post-checkout"}}},
		{Name: "evil-gc", Settings: [][2]string{{"gc.recentObjectsHook", "%s"}, {"gc.auto", "1"}}},
		{Name: "evil-lfs-agent", Settings: [][2]string{
			{"lfs.standalonetransferagent", "evil"}, {"lfs.customtransfer.evil.path", "%s"},
		}},
	}
}

// TestEnsureRefusesAClonesOwnProgramsBeforeGitRuns plants, in the clone's own config, each
// program a planted config could make worktree creation run, and asks for a pull
// request's worktree, whose head names the planted filter driver, and an issue's, whose
// default branch is asked of the remote: both are refused before git reaches the remote or
// writes a file, naming the settings, and no marker appears.
func TestEnsureRefusesAClonesOwnProgramsBeforeGitRuns(t *testing.T) {
	t.Parallel()

	for _, p := range refusedPlantings() {
		t.Run(p.Name, func(t *testing.T) {
			t.Parallel()

			r := newGuardRig(t)
			marker := r.plants.Plant(r.f.clone, p)
			testutil.RunGit(t, r.f.clone, "remote", "set-head", "origin", "-d")
			m := r.maker()

			for _, req := range []EnsureRequest{r.f.request(item.KindPR, 9), r.f.request(item.KindIssue, 3)} {
				_, err := m.Ensure(t.Context(), req)
				require.ErrorIs(t, err, gitread.ErrCloneConfig, "%s #%d", req.Kind, req.Number)
				assert.Contains(t, uierr.Message(err), "This clone's own git config sets "+p.Keys()[0])
			}

			assert.NoFileExists(t, marker)
			assert.Len(t, worktreeList(t, r.f.clone), 1, "no worktree was made")
		})
	}
}

// worktreeList returns the worktrees git lists for the clone at dir.
func worktreeList(t *testing.T, dir string) []string {
	t.Helper()

	var out []string

	for line := range strings.Lines(testutil.RunGit(t, dir, "worktree", "list", "--porcelain")) {
		if path, ok := strings.CutPrefix(strings.TrimSpace(line), "worktree "); ok {
			out = append(out, path)
		}
	}

	return out
}

func TestEnsureNamesTheClonesOwnSettingsItRefuses(t *testing.T) {
	t.Parallel()

	r := newGuardRig(t)
	testutil.RunGit(t, r.f.clone, "config", "hook.evil.command", "touch /nonexistent/owned")

	_, err := r.maker().Ensure(t.Context(), r.f.request(item.KindPR, 7))
	require.ErrorIs(t, err, gitread.ErrCloneConfig)
	assert.Equal(t, "Couldn't fetch pull request #7 from origin. This clone's own git config sets hook.evil.command, "+
		"which isn't run with here: a clone's own config may hold only what git and Git LFS write there. Set it for "+
		"this folder in your global git config, with an includeIf, instead.", uierr.Message(err))
	require.NotErrorIs(t, err, gitread.ErrRemoteHelper)

	var refused *guardError
	require.ErrorAs(t, err, &refused, "the guard's refusal stays in the chain")
}

// TestEnsureWithTheClonesOwnTransportSettings makes worktrees in a clone whose own config
// names an ssh command, a password program, and a credential helper, each a fake that
// writes a marker file, rewrites origin's URL with insteadOf to the file:// remote it was
// cloned from, and sets an http setting: a pull request's worktree and an issue's, whose
// default branch is asked of the remote, are both made, and none of the clone's programs
// runs.
func TestEnsureWithTheClonesOwnTransportSettings(t *testing.T) {
	t.Parallel()

	r := newGuardRig(t)
	markers := map[string]string{}

	for _, p := range []testutil.Planting{
		{Name: "evil-ssh", Settings: [][2]string{{"core.sshCommand", "%s"}}},
		{Name: "evil-askpass", Settings: [][2]string{{"core.askPass", "%s"}}},
		{Name: "evil-credential", Settings: [][2]string{{"credential.helper", "!%s"}}},
	} {
		markers[p.Name] = r.plants.Plant(r.f.clone, p)
	}

	const url = "https://example.invalid/acme-org/example-repo.git"

	testutil.RunGit(t, r.f.clone, "config", "url."+r.f.bare+".insteadOf", url)
	testutil.RunGit(t, r.f.clone, "remote", "set-url", "origin", url)
	testutil.RunGit(t, r.f.clone, "config", "http.postBuffer", "524288000")
	testutil.RunGit(t, r.f.clone, "remote", "set-head", "origin", "-d")

	m := r.maker()

	pr, err := m.Ensure(t.Context(), r.f.request(item.KindPR, 9))
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(pr.Path, "a.txt"))

	issue, err := m.Ensure(t.Context(), r.f.request(item.KindIssue, 3))
	require.NoError(t, err, "its default branch is asked of the remote, then fetched")
	assert.Equal(t, r.f.main, issue.Head)

	for name, marker := range markers {
		assert.NoFileExists(t, marker, "worktree creation ran the clone's own %s", name)
	}
}

// TestEnsureSaysWhichOfTheClonesOwnSettingsItWentWithout fetches over ssh from a clone whose
// own config names its ssh command, from an environment whose own GIT_SSH is a fake that
// answers nothing: the user's ssh runs, never the clone's, and the failed fetch says the
// clone's own was set aside, which may be why.
func TestEnsureSaysWhichOfTheClonesOwnSettingsItWentWithout(t *testing.T) {
	t.Parallel()

	r := newGuardRig(t)
	evil := r.plants.Plant(r.f.clone, testutil.Planting{Name: "evil-ssh", Settings: [][2]string{{"core.sshCommand", "%s"}}})
	mine, ran := r.plants.Marker("my-ssh")

	r.plants.AddEnv("GIT_SSH=" + mine)

	testutil.RunGit(t, r.f.clone, "remote", "set-url", "origin", "ssh://git@example.invalid/acme-org/example-repo.git")

	_, err := r.maker().Ensure(t.Context(), r.f.request(item.KindPR, 7))
	require.Error(t, err)

	var aside *gitread.SetAsideError
	require.ErrorAs(t, err, &aside)
	assert.Equal(t, []string{gitread.SetAsideSSHCommand}, aside.Settings)
	assert.Equal(t, "Couldn't fetch pull request #7 from origin. This clone's own core.sshCommand isn't used to reach "+
		"origin, which may be why: set it for this folder in your global git config, with an includeIf.",
		uierr.Message(err))
	assert.FileExists(t, ran, "the user's own ssh ran")
	assert.NoFileExists(t, evil, "the clone's never did")
}

// failingGuard is a Guard whose setups fail with err, counting them, and whose reads run
// with the rig's isolated environment.
type failingGuard struct {
	env   func() []string
	err   error
	calls *atomic.Int32
}

func (f failingGuard) ReadEnv() []string { return f.env() }

func (f failingGuard) RemoteSetupFor(context.Context, string, string) (gitread.Setup, error) {
	f.calls.Add(1)

	return gitread.Setup{}, f.err
}

func (f failingGuard) CheckoutSetup(context.Context, string) (gitread.Setup, error) {
	f.calls.Add(1)

	return gitread.Setup{}, f.err
}

func (f failingGuard) LocalSetup(context.Context, string) (gitread.Local, error) {
	f.calls.Add(1)

	return gitread.Local{}, f.err
}

// errExit stands for git's exit status.
var errExit = errors.New("exit status 128")

func TestEnsureWordsASetupThatCouldntBeWorkedOut(t *testing.T) {
	t.Parallel()

	r := newGuardRig(t)

	for setupErr, want := range map[error]string{
		fmt.Errorf("git config: bad config line 3 in file .git/config: %w", errExit): "Bad config line 3 in file .git/config.",
		fmt.Errorf("%w: %w", gitread.ErrNoGit, errExit): "Git can't be found on your login shell's PATH or in the usual tool " +
			"folders.",
		fmt.Errorf("git ls-remote: No such remote 'upstream': %w", errExit): "No such remote 'upstream'.",
	} {
		guard := failingGuard{env: r.plants.Environ, err: setupErr, calls: &atomic.Int32{}}

		_, err := r.makerWith(guard).Ensure(t.Context(), r.f.request(item.KindPR, 7))
		require.Error(t, err)
		assert.Equal(t, "Couldn't fetch pull request #7 from origin. "+want, uierr.Message(err))
		require.ErrorIs(t, err, setupErr, "gitread's error stays in the chain")
		assert.Equal(t, int32(1), guard.calls.Load())
	}
}

// countingGuard is a Guard that counts the setups it works out.
type countingGuard struct {
	Guard

	remote, checkout atomic.Int32
}

func (c *countingGuard) RemoteSetupFor(ctx context.Context, dir, remote string) (gitread.Setup, error) {
	c.remote.Add(1)

	return c.Guard.RemoteSetupFor(ctx, dir, remote)
}

func (c *countingGuard) CheckoutSetup(ctx context.Context, dir string) (gitread.Setup, error) {
	c.checkout.Add(1)

	return c.Guard.CheckoutSetup(ctx, dir)
}

// TestEnsureWorksOutOneSetup makes an issue's worktree, which asks the remote for its
// default branch, fetches it, and checks it out, and a pull request's, which fetches and
// checks out: each works out the clone's setup once, for all three commands.
func TestEnsureWorksOutOneSetup(t *testing.T) {
	t.Parallel()

	r := newGuardRig(t)
	testutil.RunGit(t, r.f.clone, "remote", "set-head", "origin", "-d")

	guard := &countingGuard{Guard: r.guard()}
	m := r.makerWith(guard)

	_, err := m.Ensure(t.Context(), r.f.request(item.KindIssue, 3))
	require.NoError(t, err)
	assert.Equal(t, int32(1), guard.remote.Load(), "one setup for the ls-remote, the fetch, and the checkout")
	assert.Zero(t, guard.checkout.Load())

	_, err = m.Ensure(t.Context(), r.f.request(item.KindPR, 9))
	require.NoError(t, err)
	assert.Equal(t, int32(2), guard.remote.Load(), "one more for the next launch")
	assert.Zero(t, guard.checkout.Load())
}

func TestGuardedCommandsRunTheSetupsGit(t *testing.T) {
	t.Parallel()

	var names recorded

	g := git{bin: "/srv/tools/other-git", with: func([]string) execpath.Runner {
		return namedRunner{run: okRunner{}, names: &names}
	}}

	_, _, err := g.guarded(t.Context(), "/srv/projects/example-repo", gitread.Setup{Git: "/srv/tools/git"},
		worktreeAddArgs("/srv/worktrees/pr-7", "worktree-pr-7"))
	require.NoError(t, err)
	assert.Equal(t, []string{"/srv/tools/git"}, names.all(), "the git the setup was worked out for")
}

// okRunner runs nothing and succeeds.
type okRunner struct{}

func (okRunner) Run(context.Context, string, ...string) (stdout, stderr []byte, err error) {
	return nil, nil, nil
}

// plantWorktreeFilter adds to the clone at dir's own config an includeIf of every worktree
// git makes, whose file defines the filter driver evil, a fake that writes a marker file,
// which a pull request's .gitattributes names: no read in the clone itself sees it. It
// returns that file.
func (r *guardRig) plantWorktreeFilter(dir string) string {
	r.t.Helper()

	program, marker := r.plants.Marker("evil-worktree-filter")
	included := filepath.Join(r.t.TempDir(), "evil.gitconfig")
	require.NoError(r.t, os.WriteFile(included, []byte("[filter \"evil\"]\n\tsmudge = "+program+"\n\trequired = true\n"), 0o600))
	testutil.RunGit(r.t, dir, "config", "includeIf.gitdir:**/.git/worktrees/**.path", included)

	return marker
}

// TestEnsureNeverWritesOutsideTheClone plants a core.worktree that sends git's checkout to
// a folder outside the clone, and an includeIf that adds a filter driver only inside the
// worktrees git makes, which no read in the clone itself sees: worktree creation is
// refused, and nothing is written or run.
func TestEnsureNeverWritesOutsideTheClone(t *testing.T) {
	t.Parallel()

	r := newGuardRig(t)
	elsewhere := filepath.Join(r.e.home, "elsewhere")
	require.NoError(t, os.Mkdir(elsewhere, 0o700))
	testutil.RunGit(t, r.f.clone, "config", "core.worktree", elsewhere)

	_, err := r.maker().Ensure(t.Context(), r.f.request(item.KindPR, 9))
	require.ErrorIs(t, err, gitread.ErrCloneConfig)
	assert.Contains(t, uierr.Message(err), "sets core.worktree,")

	testutil.RunGit(t, r.f.clone, "config", "--unset", "core.worktree")

	marker := r.plantWorktreeFilter(r.f.clone)

	_, err = r.maker().Ensure(t.Context(), r.f.request(item.KindPR, 9))
	require.ErrorIs(t, err, gitread.ErrCloneConfig)
	assert.Contains(t, uierr.Message(err), "sets includeif.gitdir:**/.git/worktrees/**.path,")

	assert.NoFileExists(t, marker)
	assert.Empty(t, mustReadDir(t, elsewhere))
	assert.Len(t, worktreeList(t, r.f.clone), 1)
}

// mustReadDir lists dir.
func mustReadDir(t *testing.T, dir string) []os.DirEntry {
	t.Helper()

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)

	return entries
}

// TestEnsureRunsNoHookOrMonitorOfTheClonesOwn plants the clone's own hooks folder and file
// system monitor, which husky and git itself write and every command here sets again, and
// makes a pull request's worktree and an issue's: both are made, neither runs, and every
// command on the clone runs one git, through the configured runner.
func TestEnsureRunsNoHookOrMonitorOfTheClonesOwn(t *testing.T) {
	t.Parallel()

	r := newGuardRig(t)
	markers := map[string]string{
		"hooks":     r.plants.PlantHooks(r.f.clone, "post-checkout"),
		"fsmonitor": r.plants.Plant(r.f.clone, testutil.Planting{Name: "evil-fsmonitor", Settings: [][2]string{{"core.fsmonitor", "%s"}}}),
	}

	testutil.RunGit(t, r.f.clone, "remote", "set-head", "origin", "-d")
	m := r.maker()

	pr, err := m.Ensure(t.Context(), r.f.request(item.KindPR, 9))
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(pr.Path, "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "a\n", string(got))

	issue, err := m.Ensure(t.Context(), r.f.request(item.KindIssue, 3))
	require.NoError(t, err, "its default branch is asked of the remote, then fetched")
	assert.Equal(t, r.f.main, issue.Head)

	for name, marker := range markers {
		assert.NoFileExists(t, marker, "worktree creation ran the clone's own %s", name)
	}

	bin, err := r.guard().GitFinder().Find("git")
	require.NoError(t, err)

	names := r.names.all()
	require.NotEmpty(t, names)

	for _, name := range names {
		assert.Equal(t, bin, name, "every command on the clone runs one git, through the configured runner")
	}
}

// TestEnsureInACloneWithWhatGitAndItsToolsWrite makes worktrees in a clone whose own config
// holds what git clone, a fork's upstream, Git LFS, commit signing, an editor's branch
// notes, and git clone --recurse-submodules write: none of it refuses them, and a pull
// request is fetched from the fork's upstream.
func TestEnsureInACloneWithWhatGitAndItsToolsWrite(t *testing.T) {
	t.Parallel()

	r := newGuardRig(t)

	for _, kv := range [][2]string{
		{"remote.upstream.url", r.f.bare},
		{"remote.upstream.fetch", "+refs/heads/*:refs/remotes/upstream/*"},
		{"remote.upstream.gh-resolved", "base"},
		{"filter.lfs.clean", "git-lfs clean -- %f"},
		{"filter.lfs.smudge", "git-lfs smudge -- %f"},
		{"filter.lfs.process", "git-lfs filter-process"},
		{"filter.lfs.required", "true"},
		{"lfs.repositoryformatversion", "0"},
		{"user.signingkey", "~/.ssh/id_ed25519.pub"},
		{"gpg.format", "ssh"},
		{"commit.gpgsign", "true"},
		{"tag.gpgsign", "true"},
		{"branch.main.vscode-merge-base", "origin/main"},
		{"submodule.active", "."},
	} {
		testutil.RunGit(t, r.f.clone, "config", kv[0], kv[1])
	}

	m := r.maker()

	req := r.f.request(item.KindPR, 9)
	req.Remote = "upstream"

	pr, err := m.Ensure(t.Context(), req)
	require.NoError(t, err, "a fork's pull request, from its upstream")
	assert.FileExists(t, filepath.Join(pr.Path, "a.txt"))

	issue, err := m.Ensure(t.Context(), r.f.request(item.KindIssue, 3))
	require.NoError(t, err)
	assert.Equal(t, r.f.main, issue.Head)
}

// TestEnsureNeverMovesARefThroughTheClonesOwnRefspecs reproduces a clone whose own
// remote.origin.fetch maps origin's branches onto its own, forced: an issue's fetch of the
// default branch must move only origin/main, never the clone's own main, which holds a
// commit of its own.
func TestEnsureNeverMovesARefThroughTheClonesOwnRefspecs(t *testing.T) {
	t.Parallel()

	r := newGuardRig(t)
	testutil.RunGit(t, r.f.clone, "config", "remote.origin.fetch", "+refs/heads/*:refs/heads/*")
	mine := testutil.Commit(t, r.f.clone, "mine.txt", "mine\n")

	work := filepath.Join(r.e.home, "pusher")
	testutil.RunGit(t, r.e.home, "clone", "-q", r.f.bare, work)
	theirs := testutil.Commit(t, work, "theirs.txt", "theirs\n")
	testutil.RunGit(t, work, "push", "-q", "origin", "HEAD:main")

	req := r.f.request(item.KindIssue, 4)
	req.DefaultBranch = mainBranch

	for name, m := range map[string]*Maker{"guarded": r.maker(), "unguarded": NewMaker(r.e.service(r.e.git))} {
		issue, err := m.Ensure(t.Context(), req)
		require.NoError(t, err, name)
		assert.Equal(t, theirs, issue.Head, "%s: the issue starts from origin's main", name)
		assert.Equal(t, mine, strings.TrimSpace(testutil.RunGit(t, r.f.clone, "rev-parse", "refs/heads/main")),
			"%s: the clone's own main keeps its commit", name)

		testutil.RunGit(t, r.f.clone, "worktree", "remove", "--force", issue.Path)
		testutil.RunGit(t, r.f.clone, "branch", "-D", issue.Branch)
	}
}

// TestEnsureInABloblessClone makes worktrees in a partial clone that has none of its
// files' contents: the checkout fetches those it writes from origin, the clone's promisor
// remote, under the same guards.
func TestEnsureInABloblessClone(t *testing.T) {
	t.Parallel()

	r := newGuardRig(t)
	testutil.RunGit(t, r.f.bare, "config", "uploadpack.allowFilter", "true")

	clone := filepath.Join(r.e.root, "blobless")
	testutil.RunGit(t, r.e.home, "clone", "-q", "--filter=blob:none", "file://"+filepath.ToSlash(r.f.bare), clone)

	m := r.maker()

	req := r.f.request(item.KindPR, 7)
	req.Clone = clone

	pr, err := m.Ensure(t.Context(), req)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(pr.Path, "fix.txt"))

	req = r.f.request(item.KindIssue, 3)
	req.Clone = clone

	issue, err := m.Ensure(t.Context(), req)
	require.NoError(t, err)
	assert.FileExists(t, filepath.Join(issue.Path, "README.md"))
}

func TestEnsureNamesTheRemoteItRefuses(t *testing.T) {
	t.Parallel()

	r := newGuardRig(t)
	testutil.RunGit(t, r.f.clone, "remote", "add", "upstream", "ext::sh -c touch% "+filepath.Join(r.e.home, "pwned"))

	req := r.f.request(item.KindPR, 7)
	req.Remote = "upstream"

	_, err := r.maker().Ensure(t.Context(), req)
	require.ErrorIs(t, err, gitread.ErrRemoteHelper)
	assert.Equal(t, "Couldn't fetch pull request #7 from upstream. This clone reaches upstream through a remote "+
		"helper (a URL that starts with a name and ::, or remote.upstream.vcs), which isn't run here, as the clone's "+
		"own config could make it run anything.", uierr.Message(err))
	assert.NoFileExists(t, filepath.Join(r.e.home, "pwned"))
}

// TestPlainGitWouldRunThePlantedPrograms shows the plantings are real: in a clone set up
// the same way, a plain fetch runs the planted upload-pack and alternate refs command, and
// a plain `git worktree add` the planted filter driver and the hooks of the clone's own
// folder and config.
func TestPlainGitWouldRunThePlantedPrograms(t *testing.T) {
	t.Parallel()

	r := newGuardRig(t)
	plain := filepath.Join(r.e.home, "plain")
	testutil.RunGit(t, r.e.home, "clone", "-q", r.f.bare, plain)

	markers := map[string]string{"hooks": r.plants.PlantHooks(plain, "post-checkout")}
	for _, p := range refusedPlantings() {
		markers[p.Name] = r.plants.Plant(plain, p)
	}

	git := execpath.Exec{Env: r.plants.Environ(), Dir: plain}

	_, _, err := git.Run(t.Context(), "git", "fetch", "-q", "origin", "refs/pull/9/head:refs/heads/plain-9")
	require.Error(t, err, "the planted upload-pack answers nothing")
	assert.FileExists(t, markers["evil-upload-pack"], "a plain fetch runs the clone's own upload-pack")

	testutil.RunGit(t, plain, "config", "--unset", "remote.origin.uploadpack")

	_, _, err = git.Run(t.Context(), "git", "fetch", "-q", "origin", "refs/pull/9/head:refs/heads/plain-9")
	require.NoError(t, err)
	assert.FileExists(t, markers["evil-alternate-refs"], "a plain fetch runs the clone's own alternate refs command")

	_, _, err = git.Run(t.Context(), "git", "worktree", "add", "-q", filepath.Join(r.e.home, "plain-wt"), "plain-9")
	t.Logf("a plain worktree add: %v", err) // the planted driver may fail it, once it ran

	assert.FileExists(t, markers["evil-smudge"], "a plain worktree add runs the clone's own filter driver")
	assert.FileExists(t, markers["hooks"], "its own post-checkout hook")
	assert.FileExists(t, markers["evil-hook"], "and a hook its own config names")
}

// TestAPlainWorktreeAddRunsWhatOnlyTheWorktreeIncludes shows an includeIf of the worktrees
// git makes is real: a filter driver it defines shows in no read of the clone itself, and
// a plain `git worktree add` runs it.
func TestAPlainWorktreeAddRunsWhatOnlyTheWorktreeIncludes(t *testing.T) {
	t.Parallel()

	r := newGuardRig(t)
	plain := filepath.Join(r.e.home, "plain")
	testutil.RunGit(t, r.e.home, "clone", "-q", r.f.bare, plain)

	marker := r.plantWorktreeFilter(plain)
	assert.NotContains(t, testutil.RunGit(t, plain, "config", "--list"), "filter.evil", "the clone itself never sees it")

	git := execpath.Exec{Env: r.plants.Environ(), Dir: plain}

	_, _, err := git.Run(t.Context(), "git", "fetch", "-q", "origin", "refs/pull/9/head:refs/heads/plain-9")
	require.NoError(t, err)

	_, _, err = git.Run(t.Context(), "git", "worktree", "add", "-q", filepath.Join(r.e.home, "plain-wt"), "plain-9")
	t.Logf("a plain worktree add: %v", err)
	assert.FileExists(t, marker, "a plain worktree add runs a filter driver only the new worktree's config includes")
}

func TestEnsureRefusesARemoteTheGuardsRefuse(t *testing.T) {
	t.Parallel()

	r := newGuardRig(t)
	testutil.RunGit(t, r.f.clone, "remote", "set-url", "origin", "ext::sh -c touch% "+filepath.Join(r.e.home, "pwned"))

	_, err := r.maker().Ensure(t.Context(), r.f.request(item.KindPR, 7))
	require.ErrorIs(t, err, gitread.ErrRemoteHelper)
	assert.Equal(t, "Couldn't fetch pull request #7 from origin. This clone reaches origin through a remote helper (a "+
		"URL that starts with a name and ::, or remote.origin.vcs), which isn't run here, as the clone's own config "+
		"could make it run anything.", uierr.Message(err))
	assert.NoFileExists(t, filepath.Join(r.e.home, "pwned"))
}

// TestEnsureChecksOutWhatTheCloneHasFromARemoteItRefuses refuses origin, reached through a
// remote helper: an issue's worktree still starts from the last fetched copy of origin's
// default branch, saying why, and one on a branch the clone has is attached, since a
// checkout asks the remote nothing.
func TestEnsureChecksOutWhatTheCloneHasFromARemoteItRefuses(t *testing.T) {
	t.Parallel()

	r := newGuardRig(t)
	pwned := filepath.Join(r.e.home, "pwned")
	testutil.RunGit(t, r.f.clone, "remote", "set-url", "origin", "ext::sh -c touch% "+pwned)
	testutil.RunGit(t, r.f.clone, "update-ref", "refs/heads/worktree-issue-5", r.f.main)

	req := r.f.request(item.KindIssue, 3)
	req.DefaultBranch = mainBranch

	issue, err := r.maker().Ensure(t.Context(), req)
	require.NoError(t, err, "from the last fetched copy")
	assert.Equal(t, r.f.main, issue.Head)
	assert.Contains(t, issue.Notice, "Couldn't fetch origin/main, so the worktree starts from the last fetched copy: "+
		"This clone reaches origin through a remote helper")

	attached, err := r.maker().Ensure(t.Context(), r.f.request(item.KindIssue, 5))
	require.NoError(t, err, "a branch the clone has")
	assert.Equal(t, ActionAttach, attached.Action)
	assert.NoFileExists(t, pwned)
}

// TestEnsureRefusesACheckoutGitLFSWouldReroute sets, in a clone with Git LFS's own filter,
// a proxy for every http request: the checkout of a branch the clone has, which Git LFS
// would download its files for through the proxy, with the user's credentials, is refused.
// Without Git LFS, the same proxy refuses nothing, as no checkout reaches a host.
func TestEnsureRefusesACheckoutGitLFSWouldReroute(t *testing.T) {
	t.Parallel()

	r := newGuardRig(t)
	testutil.RunGit(t, r.f.clone, "config", "filter.lfs.process", "git-lfs filter-process")
	testutil.RunGit(t, r.f.clone, "config", "http.proxy", "http://203.0.113.9:8080")
	testutil.RunGit(t, r.f.clone, "update-ref", "refs/heads/worktree-issue-5", r.f.main)

	_, err := r.maker().Ensure(t.Context(), r.f.request(item.KindIssue, 5))
	require.ErrorIs(t, err, gitread.ErrCloneRoutes)

	testutil.RunGit(t, r.f.clone, "config", "--unset", "filter.lfs.process")

	attached, err := r.maker().Ensure(t.Context(), r.f.request(item.KindIssue, 5))
	require.NoError(t, err)
	assert.Equal(t, ActionAttach, attached.Action)
}
