package worktrees

import (
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

// maker returns a Maker whose guarded commands run under gitread's guards.
func (r *guardRig) maker() *Maker {
	git := execpath.Exec{Env: r.environ()}

	return NewMaker(NewService(Config{
		Settings: fakeSettings{root: cloneRoot}, Git: git, Finder: execpath.Search{},
		Guard: gitread.New(gitread.Config{Env: r.environ}), Home: r.e.home, Clock: testutil.NewClock(listedAt()),
		Logger: slog.New(slog.DiscardHandler),
	}))
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

// plant sets, in the clone at dir's own config, every program a planted config could make
// a fetch or a checkout run, each a fake that writes a marker file, and returns the files.
func (r *guardRig) plant(dir string) map[string]string {
	r.t.Helper()

	markers := map[string]string{}
	set := func(name string, settings ...[2]string) {
		program, marker := r.marker(name)
		markers[name] = marker

		for _, s := range settings {
			testutil.RunGit(r.t, dir, "config", s[0], strings.ReplaceAll(s[1], "%s", program))
		}
	}

	set("evil-ssh", [2]string{"core.sshCommand", "%s"})
	set("evil-askpass", [2]string{"core.askPass", "%s"})
	set("evil-credential", [2]string{"credential.helper", "!%s"})
	set("evil-smudge", [2]string{"filter.evil.smudge", "%s"}, [2]string{"filter.evil.clean", "%s"},
		[2]string{"filter.evil.required", "true"})
	set("evil-upload-pack", [2]string{"remote.origin.uploadpack", "%s"})
	set("evil-fsmonitor", [2]string{"core.fsmonitor", "%s"})

	hookMarker := filepath.Join(r.e.home, "ran", "post-checkout")
	hook, entry := testutil.FakeTool(r.t, "post-checkout", testutil.FakeRun{
		Files: []testutil.FakeFile{{Path: hookMarker, Content: "ran"}},
	})

	r.mu.Lock()
	r.env = append(r.env, entry)
	r.mu.Unlock()

	testutil.RunGit(r.t, dir, "config", "core.hooksPath", filepath.Dir(hook))

	markers["hooks"] = hookMarker

	testutil.RunGit(r.t, dir, "config", "http.cookieFile", filepath.Join(r.e.home, "cookies"))
	testutil.RunGit(r.t, dir, "config", "http.saveCookies", "true")

	return markers
}

// TestEnsureRunsNoProgramTheClonesOwnConfigNames plants, in the clone's own config, every
// program a planted config could make worktree creation run, and makes a pull request's
// worktree, whose head names the planted filter driver, and an issue's, whose default
// branch is asked of the remote: both are made, the files are written as they are, and no
// marker appears.
func TestEnsureRunsNoProgramTheClonesOwnConfigNames(t *testing.T) {
	t.Parallel()

	r := newGuardRig(t)
	markers := r.plant(r.f.clone)
	testutil.RunGit(t, r.f.clone, "remote", "set-head", "origin", "-d")
	m := r.maker()

	pr, err := m.Ensure(t.Context(), r.f.request(item.KindPR, 9))
	require.NoError(t, err)

	got, err := os.ReadFile(filepath.Join(pr.Path, "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "a\n", string(got), "written as it is, the clone's filter driver turned off")

	issue, err := m.Ensure(t.Context(), r.f.request(item.KindIssue, 3))
	require.NoError(t, err, "its default branch is asked of the remote, then fetched")
	assert.Equal(t, r.f.main, issue.Head)

	for name, marker := range markers {
		assert.NoFileExists(t, marker, "worktree creation ran the clone's own %s", name)
	}

	assert.NoFileExists(t, filepath.Join(r.e.home, "cookies"))
}

// TestPlainGitWouldRunThePlantedPrograms shows the plantings are real: in a clone set up
// the same way, a plain fetch runs the planted upload-pack, and a plain `git worktree add`
// the planted filter driver and hook.
func TestPlainGitWouldRunThePlantedPrograms(t *testing.T) {
	t.Parallel()

	r := newGuardRig(t)
	plain := filepath.Join(r.e.home, "plain")
	testutil.RunGit(t, r.e.home, "clone", "-q", r.f.bare, plain)
	markers := r.plant(plain)
	git := execpath.Exec{Env: r.environ(), Dir: plain}

	_, _, err := git.Run(t.Context(), "git", "fetch", "-q", "origin", "refs/pull/9/head:refs/heads/plain-9")
	require.Error(t, err, "the planted upload-pack answers nothing")
	assert.FileExists(t, markers["evil-upload-pack"], "a plain fetch runs the clone's own upload-pack")

	testutil.RunGit(t, plain, "config", "--unset", "remote.origin.uploadpack")

	_, _, err = git.Run(t.Context(), "git", "fetch", "-q", "origin", "refs/pull/9/head:refs/heads/plain-9")
	require.NoError(t, err)

	_, _, err = git.Run(t.Context(), "git", "worktree", "add", "-q", filepath.Join(r.e.home, "plain-wt"), "plain-9")
	t.Logf("a plain worktree add: %v", err) // the planted driver may fail it, once it ran

	assert.FileExists(t, markers["evil-smudge"], "a plain worktree add runs the clone's own filter driver")
	assert.FileExists(t, markers["hooks"], "and its own post-checkout hook")
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

func TestEnsureSaysWhichOfTheClonesOwnSettingsItWentWithout(t *testing.T) {
	t.Parallel()

	r := newGuardRig(t)
	testutil.RunGit(t, r.f.clone, "config", "core.sshCommand", "ssh -i ~/.ssh/example-key")
	testutil.RunGit(t, r.f.clone, "remote", "set-url", "origin", "ssh://git@example.invalid/acme-org/example-repo.git")

	_, err := r.maker().Ensure(t.Context(), r.f.request(item.KindPR, 7))
	require.Error(t, err)

	var aside *gitread.SetAsideError
	require.ErrorAs(t, err, &aside)
	assert.Equal(t, []string{gitread.SetAsideSSHCommand}, aside.Settings)
	assert.Equal(t, "Couldn't fetch pull request #7 from origin. This clone's own core.sshCommand isn't used to reach "+
		"origin, which may be why: set it for this folder in your global git config, with an includeIf.",
		uierr.Message(err))
	assert.NotErrorIs(t, err, gitread.ErrRemoteHelper)
}
