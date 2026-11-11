package worktrees

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

	mu  sync.Mutex
	env []string

	// names are the programs the Maker's git ran.
	names recorded
}

// newGuardRig builds a guardRig, or skips the test without git.
func newGuardRig(t *testing.T) *guardRig {
	t.Helper()

	e := newEnv(t)
	r := &guardRig{t: t, e: e, f: newRemoteFixture(t, e), env: testutil.GitEnv(e.home)}

	work := filepath.Join(e.home, "pr-9")
	testutil.RunGit(t, e.home, "clone", "-q", r.f.bare, work)
	require.NoError(t, os.WriteFile(filepath.Join(work, ".gitattributes"), []byte("*.txt filter=evil\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(work, "a.txt"), []byte("a\n"), 0o600))
	testutil.RunGit(t, work, "add", ".gitattributes", "a.txt")
	testutil.RunGit(t, work, "commit", "-q", "-m", "attributes")
	testutil.RunGit(t, work, "push", "-q", "origin", "HEAD:refs/pull/9/head")

	return r
}

// environ is the user's environment, as git gets it.
func (r *guardRig) environ() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string(nil), r.env...)
}

// maker returns a Maker whose guarded commands run under gitread's guards, recording the
// program every command runs.
func (r *guardRig) maker() *Maker {
	git := namedRunner{run: execpath.Exec{Env: r.environ()}, names: &r.names}

	return NewMaker(NewService(Config{
		Settings: fakeSettings{root: cloneRoot}, Git: git, Finder: execpath.Search{},
		Guard: gitread.New(gitread.Config{Env: r.environ}),
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

// marker makes a fake program called name that writes ran/<name> when it runs, and
// returns its path and that file's.
func (r *guardRig) marker(name string) (program, marker string) {
	r.t.Helper()

	ran := filepath.Join(r.e.home, "ran")
	require.NoError(r.t, os.MkdirAll(ran, 0o700))

	marker = filepath.Join(ran, name)
	program, entry := testutil.FakeTool(r.t, name, testutil.FakeRun{Files: []testutil.FakeFile{{Path: marker, Content: "ran"}}})

	r.mu.Lock()
	r.env = append(r.env, entry)
	r.mu.Unlock()

	return program, marker
}

// setting is one setting a test sets in a clone's own config, %s in its value standing for
// a fake program's path.
type setting [2]string

// planting is one way a clone's own config could make worktree creation run a program: the
// fake's name and the settings that name it.
type planting struct {
	name     string
	settings []setting
}

// refusedPlantings are every way a planted clone config could make a fetch or a checkout
// run its own program, each of which refuses worktree creation before git runs: the ssh
// command, the password program, a credential helper, a filter driver, git's
// upload-pack, an alternate refs command, a hook named in the config (which
// core.hooksPath doesn't reach), the automatic maintenance a fetch starts, and Git LFS's
// own transfer agent.
func refusedPlantings() []planting {
	return []planting{
		{name: "evil-ssh", settings: []setting{{"core.sshCommand", "%s"}}},
		{name: "evil-askpass", settings: []setting{{"core.askPass", "%s"}}},
		{name: "evil-credential", settings: []setting{{"credential.helper", "!%s"}}},
		{name: "evil-smudge", settings: []setting{
			{"filter.evil.smudge", "%s"}, {"filter.evil.clean", "%s"}, {"filter.evil.required", "true"},
		}},
		{name: "evil-upload-pack", settings: []setting{{"remote.origin.uploadpack", "%s"}}},
		{name: "evil-alternate-refs", settings: []setting{{"core.alternateRefsCommand", "%s"}}},
		{name: "evil-hook", settings: []setting{{"hook.evil.command", "%s"}, {"hook.evil.event", "post-checkout"}}},
		{name: "evil-gc", settings: []setting{{"gc.auto", "1"}, {"gc.recentObjectsHook", "%s"}}},
		{name: "evil-lfs-agent", settings: []setting{
			{"lfs.standalonetransferagent", "evil"}, {"lfs.customtransfer.evil.path", "%s"},
		}},
	}
}

// keysOf returns the keys of settings as git lists them: the section and the name
// lowercased, a subsection as written.
func keysOf(settings []setting) []string {
	keys := make([]string, 0, len(settings))

	for _, s := range settings {
		first, last := strings.Index(s[0], "."), strings.LastIndex(s[0], ".")
		keys = append(keys, strings.ToLower(s[0][:first])+s[0][first:last]+strings.ToLower(s[0][last:]))
	}

	return keys
}

// plant sets p in the clone at dir's own config, pointing at a fake program that writes a
// marker file, and returns that file. An alternate refs command comes with an alternate
// object store, an empty repository's, for a fetch to ask it about.
func (r *guardRig) plant(dir string, p planting) string {
	r.t.Helper()

	program, marker := r.marker(p.name)

	for _, s := range p.settings {
		testutil.RunGit(r.t, dir, "config", s[0], strings.ReplaceAll(s[1], "%s", program))
	}

	if p.name == "evil-alternate-refs" {
		alt := filepath.Join(r.t.TempDir(), "alt.git")
		testutil.RunGit(r.t, r.e.home, "init", "-q", "--bare", alt)
		require.NoError(r.t, os.WriteFile(filepath.Join(dir, ".git", "objects", "info", "alternates"),
			[]byte(filepath.Join(alt, "objects")+"\n"), 0o600))
	}

	return marker
}

// plantHooks points the clone at dir's own core.hooksPath at a folder whose post-checkout
// hook is a fake that writes a marker file, and returns that file.
func (r *guardRig) plantHooks(dir string) string {
	r.t.Helper()

	marker := filepath.Join(r.e.home, "ran", "post-checkout")
	hook, entry := testutil.FakeTool(r.t, "post-checkout", testutil.FakeRun{
		Files: []testutil.FakeFile{{Path: marker, Content: "ran"}},
	})

	r.mu.Lock()
	r.env = append(r.env, entry)
	r.mu.Unlock()

	testutil.RunGit(r.t, dir, "config", "core.hooksPath", filepath.Dir(hook))

	return marker
}

// TestEnsureRefusesAClonesOwnProgramsBeforeGitRuns plants, in the clone's own config, each
// program a planted config could make worktree creation run, and asks for a pull
// request's worktree, whose head names the planted filter driver, and an issue's, whose
// default branch is asked of the remote: both are refused before git reaches the remote or
// writes a file, naming the settings, and no marker appears.
func TestEnsureRefusesAClonesOwnProgramsBeforeGitRuns(t *testing.T) {
	t.Parallel()

	for _, p := range refusedPlantings() {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()

			r := newGuardRig(t)
			marker := r.plant(r.f.clone, p)
			testutil.RunGit(t, r.f.clone, "remote", "set-head", "origin", "-d")
			m := r.maker()

			for _, req := range []EnsureRequest{r.f.request(item.KindPR, 9), r.f.request(item.KindIssue, 3)} {
				_, err := m.Ensure(t.Context(), req)
				require.ErrorIs(t, err, gitread.ErrCloneConfig, "%s #%d", req.Kind, req.Number)
				assert.Contains(t, uierr.Message(err), "This clone's own git config sets "+keysOf(p.settings)[0])
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
	testutil.RunGit(t, r.f.clone, "config", "core.sshCommand", "ssh -i ~/.ssh/example-key")

	_, err := r.maker().Ensure(t.Context(), r.f.request(item.KindPR, 7))
	require.ErrorIs(t, err, gitread.ErrCloneConfig)
	assert.Equal(t, "Couldn't fetch pull request #7 from origin. This clone's own git config sets core.sshcommand, "+
		"which isn't run with here: a clone's own config may hold only what git and Git LFS write there. Set it for "+
		"this folder in your global git config, with an includeIf, instead.", uierr.Message(err))
	assert.NotErrorIs(t, err, gitread.ErrRemoteHelper)
}

// plantWorktreeFilter adds to the clone at dir's own config an includeIf of every worktree
// git makes, whose file defines the filter driver evil, a fake that writes a marker file,
// which a pull request's .gitattributes names: no read in the clone itself sees it. It
// returns that file.
func (r *guardRig) plantWorktreeFilter(dir string) string {
	r.t.Helper()

	program, marker := r.marker("evil-worktree-filter")
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
		"hooks":     r.plantHooks(r.f.clone),
		"fsmonitor": r.plant(r.f.clone, planting{name: "evil-fsmonitor", settings: []setting{{"core.fsmonitor", "%s"}}}),
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

	bin, err := execpath.Search{}.Find("git")
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

	for _, kv := range []setting{
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
	require.NoError(t, os.WriteFile(filepath.Join(r.f.clone, "mine.txt"), []byte("mine\n"), 0o600))
	testutil.RunGit(t, r.f.clone, "add", "mine.txt")
	testutil.RunGit(t, r.f.clone, "commit", "-q", "-m", "mine")
	mine := strings.TrimSpace(testutil.RunGit(t, r.f.clone, "rev-parse", "HEAD"))

	work := filepath.Join(r.e.home, "pusher")
	testutil.RunGit(t, r.e.home, "clone", "-q", r.f.bare, work)
	require.NoError(t, os.WriteFile(filepath.Join(work, "theirs.txt"), []byte("theirs\n"), 0o600))
	testutil.RunGit(t, work, "add", "theirs.txt")
	testutil.RunGit(t, work, "commit", "-q", "-m", "theirs")
	testutil.RunGit(t, work, "push", "-q", "origin", "HEAD:main")
	theirs := strings.TrimSpace(testutil.RunGit(t, work, "rev-parse", "HEAD"))

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

	markers := map[string]string{"hooks": r.plantHooks(plain)}
	for _, p := range refusedPlantings() {
		markers[p.name] = r.plant(plain, p)
	}

	git := execpath.Exec{Env: r.environ(), Dir: plain}

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

	git := execpath.Exec{Env: r.environ(), Dir: plain}

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
