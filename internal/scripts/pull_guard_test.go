package scripts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/execpath"
	"github.com/mrz1836/nilbox/internal/scriptrun"
	"github.com/mrz1836/nilbox/internal/testutil"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// planted is one way a clone's own config can make git run a program: the settings that
// name it, with %s for the fake program's path, and the fake's name.
type planted struct {
	name     string
	settings [][2]string
}

// The plantings that need more than their settings: an alternate refs command, which a
// fetch runs only in a clone with alternates, which plant gives the clone.
const alternateRefs = "evil-alternate-refs"

// refusedPlantings are every way a planted clone config could make a pull run its own
// program, each of which refuses the pull before git runs: the ssh command, the password
// program, a credential helper, a filter driver (the checkout runs its smudge), signature
// checks, git's upload-pack, an alternate refs command, a hook named in the config (which
// core.hooksPath doesn't reach), the automatic maintenance a fetch and a merge start
// (gc's recent objects hook), and Git LFS's own transfer agent.
func refusedPlantings() []planted {
	return []planted{
		{name: "evil-ssh", settings: [][2]string{{"core.sshCommand", "%s"}}},
		{name: "evil-askpass", settings: [][2]string{{"core.askPass", "%s"}}},
		{name: "evil-credential", settings: [][2]string{{"credential.helper", "!%s"}}},
		{name: "evil-smudge", settings: [][2]string{
			{"filter.evil.smudge", "%s"}, {"filter.evil.clean", "%s"}, {"filter.evil.required", "true"},
		}},
		{name: "evil-gpg", settings: [][2]string{
			{"merge.verifySignatures", "true"}, {"gpg.program", "%s"}, {"gpg.ssh.program", "%s"},
		}},
		{name: "evil-upload-pack", settings: [][2]string{{"remote.origin.uploadpack", "%s"}}},
		{name: alternateRefs, settings: [][2]string{{"core.alternateRefsCommand", "%s"}}},
		{name: "evil-hook", settings: [][2]string{{"hook.evil.command", "%s"}, {"hook.evil.event", "post-merge"}}},
		{name: "evil-gc", settings: [][2]string{
			{"maintenance.strategy", "gc"},
			{"gc.auto", "1"},
			{"gc.autoPackLimit", "1"},
			{"gc.autoDetach", "false"},
			{"maintenance.autoDetach", "false"},
			{"fetch.unpackLimit", "1"},
			{"gc.cruftPacks", "true"},
			{"gc.recentObjectsHook", "%s"},
		}},
		{name: "evil-lfs-agent", settings: [][2]string{
			{"lfs.standalonetransferagent", "evil"}, {"lfs.customtransfer.evil.path", "%s"},
		}},
	}
}

// keysOf returns the keys of settings as git lists them: the section and the name
// lowercased, a subsection as written.
func keysOf(settings [][2]string) []string {
	keys := make([]string, 0, len(settings))

	for _, s := range settings {
		first, last := strings.Index(s[0], "."), strings.LastIndex(s[0], ".")
		keys = append(keys, strings.ToLower(s[0][:first])+s[0][first:last]+strings.ToLower(s[0][last:]))
	}

	return keys
}

// plant sets p in the clone at dir's own config, pointing at a fake program that writes a
// marker file, and returns that file. An alternate refs command comes with an alternate
// object store, an empty repository's, for the fetch to ask it about.
func (g *gitRig) plant(dir string, p planted) string {
	g.t.Helper()

	program, marker := g.marker(p.name)

	for _, s := range p.settings {
		testutil.RunGit(g.t, dir, "config", s[0], strings.ReplaceAll(s[1], "%s", program))
	}

	if p.name == alternateRefs {
		alt := filepath.Join(g.t.TempDir(), "alt.git")
		testutil.RunGit(g.t, g.home, "init", "-q", "--bare", alt)
		require.NoError(g.t, os.WriteFile(filepath.Join(dir, ".git", "objects", "info", "alternates"),
			[]byte(filepath.Join(alt, "objects")+"\n"), 0o600))
	}

	return marker
}

// plantHooks points the clone at dir's own core.hooksPath at a folder whose post-merge hook
// is a fake that writes a marker file, and returns that file.
func (g *gitRig) plantHooks(dir string) string {
	g.t.Helper()

	marker := filepath.Join(g.home, "ran", "post-merge")
	require.NoError(g.t, os.MkdirAll(filepath.Dir(marker), 0o700))

	hook, entry := testutil.FakeTool(g.t, "post-merge", testutil.FakeRun{
		Files: []testutil.FakeFile{{Path: marker, Content: "ran"}},
	})

	g.envMu.Lock()
	g.genv = append(g.genv, entry)
	g.envMu.Unlock()

	testutil.RunGit(g.t, dir, "config", "core.hooksPath", filepath.Dir(hook))

	return marker
}

// pushSigned pushes to origin's main a commit that names the filter driver evil for its
// text files in its .gitattributes, and carries a signature, which a signature check hands
// to gpg.program.
func (g *gitRig) pushSigned() {
	g.t.Helper()

	require.NoError(g.t, os.WriteFile(filepath.Join(g.other, ".gitattributes"), []byte("*.txt filter=evil\n"), 0o600))
	require.NoError(g.t, os.WriteFile(filepath.Join(g.other, "a.txt"), []byte("a\n"), 0o600))
	testutil.RunGit(g.t, g.other, "add", ".gitattributes", "a.txt")
	testutil.RunGit(g.t, g.other, "commit", "-q", "-m", "attributes")

	tree := strings.TrimSpace(testutil.RunGit(g.t, g.other, "rev-parse", "HEAD^{tree}"))
	parent := g.head(g.other)
	body := "tree " + tree + "\nparent " + parent + "\n" +
		"author " + testutil.GitName + " <" + testutil.GitEmail + "> 1700000000 +0000\n" +
		"committer " + testutil.GitName + " <" + testutil.GitEmail + "> 1700000000 +0000\n" +
		"gpgsig -----BEGIN PGP SIGNATURE-----\n \n iQEzBAABCAAdFiEEexample\n -----END PGP SIGNATURE-----\n\nsigned\n"

	file := filepath.Join(g.t.TempDir(), "commit")
	require.NoError(g.t, os.WriteFile(file, []byte(body), 0o600))

	oid := strings.TrimSpace(testutil.RunGit(g.t, g.other, "hash-object", "-t", "commit", "-w", file))
	testutil.RunGit(g.t, g.other, "push", "-q", "origin", oid+":refs/heads/main")
}

// TestPullRefusesAClonesOwnProgramsBeforeGitRuns plants, in the clone's own config, each
// program a planted config could make a pull run, a fake that writes a marker file, and
// pulls a signed commit whose .gitattributes names the planted filter driver: the pull is
// refused before git runs, naming the settings, and no marker appears.
func TestPullRefusesAClonesOwnProgramsBeforeGitRuns(t *testing.T) {
	t.Parallel()

	for _, p := range refusedPlantings() {
		t.Run(p.name, func(t *testing.T) {
			t.Parallel()

			g := newGitRig(t)
			g.pushSigned()
			require.Equal(t, BranchBehind, g.status(false).State)

			marker := g.plant(g.dir, p)

			_, err := g.branch.Pull(t.Context(), g.shown(g.dir))
			require.ErrorIs(t, err, ErrCantPull)
			assert.Contains(t, uierr.Message(err), "This clone's own git config sets "+keysOf(p.settings)[0])
			assert.Zero(t, g.recorded(), "refused before anything ran")
			assert.Equal(t, g.first, g.head(g.dir))
			assert.NoFileExists(t, marker)
		})
	}
}

func TestPullNamesEveryRefusedSetting(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	g.status(false)

	testutil.RunGit(t, g.dir, "config", "hook.evil.command", "touch /nonexistent/owned")
	testutil.RunGit(t, g.dir, "config", "hook.evil.event", "post-merge")

	_, err := g.branch.Pull(t.Context(), g.shown(g.dir))
	require.ErrorIs(t, err, ErrCantPull)
	assert.Equal(t, "Couldn't pull main. This clone's own git config sets hook.evil.command and hook.evil.event, "+
		"which aren't run with here: a clone's own config may hold only what git and Git LFS write there. Set them "+
		"for this folder in your global git config, with an includeIf, instead.", uierr.Message(err))
}

// TestPullNeverWritesOutsideTheClone plants a core.worktree that sends git's checkout to a
// folder outside the clone, straight in the clone's config and through an includeIf of the
// branch it is on: the pull is refused, and nothing is written there.
func TestPullNeverWritesOutsideTheClone(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	g.pushed("two.txt")
	g.status(false)

	elsewhere := filepath.Join(g.home, "elsewhere")
	require.NoError(t, os.Mkdir(elsewhere, 0o700))
	testutil.RunGit(t, g.dir, "config", "core.worktree", elsewhere)

	_, err := g.branch.Pull(t.Context(), g.shown(g.dir))
	require.ErrorIs(t, err, ErrCantPull)
	assert.Contains(t, uierr.Message(err), "sets core.worktree,")

	testutil.RunGit(t, g.dir, "config", "--unset", "core.worktree")

	included := filepath.Join(g.home, "evil.gitconfig")
	require.NoError(t, os.WriteFile(included, []byte("[core]\n\tworktree = "+elsewhere+"\n"), 0o600))
	testutil.RunGit(t, g.dir, "config", "includeIf.onbranch:main.path", included)

	_, err = g.branch.Pull(t.Context(), g.shown(g.dir))
	require.ErrorIs(t, err, ErrCantPull)
	assert.Contains(t, uierr.Message(err), "sets includeif.onbranch:main.path and core.worktree,")

	assert.Zero(t, g.recorded())
	assert.NoFileExists(t, filepath.Join(elsewhere, "two.txt"))
	assert.Equal(t, g.first, g.head(g.dir))
}

// TestPullRunsNoHookOrMonitorOfTheClonesOwn plants the clone's own hooks folder and file
// system monitor, which husky and git itself write and every pull sets again: the pull
// runs, and runs neither.
func TestPullRunsNoHookOrMonitorOfTheClonesOwn(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	two := g.pushed("two.txt")

	markers := map[string]string{
		"hooks":     g.plantHooks(g.dir),
		"fsmonitor": g.plant(g.dir, planted{name: "evil-fsmonitor", settings: [][2]string{{"core.fsmonitor", "%s"}}}),
	}

	require.Equal(t, BranchBehind, g.status(false).State)

	run := g.pull()
	require.Equal(t, scriptrun.StatusSucceeded, run.Status, run.Summary)
	assert.Equal(t, two, g.head(g.dir))

	for name, marker := range markers {
		assert.NoFileExists(t, marker, "the pull ran the clone's own %s", name)
	}
}

// TestPullOfACloneWithWhatGitAndItsToolsWrite pulls into a clone whose own config holds
// what git clone, a fork's upstream, Git LFS, commit signing, an editor's branch notes,
// git clone --recurse-submodules, and husky write: none of it refuses the pull.
func TestPullOfACloneWithWhatGitAndItsToolsWrite(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	two := g.pushed("two.txt")

	for _, kv := range [][2]string{
		{"remote.upstream.url", "https://example.invalid/acme-org/example-repo.git"},
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
		{"core.hooksPath", ".husky/_"},
		{"pull.rebase", "true"},
	} {
		testutil.RunGit(t, g.dir, "config", kv[0], kv[1])
	}

	require.Equal(t, BranchBehind, g.status(false).State)

	run := g.pull()
	require.Equal(t, scriptrun.StatusSucceeded, run.Status, run.Summary)
	assert.Equal(t, two, g.head(g.dir))
}

// TestAPlainPullWouldRunThePlantedPrograms shows the plantings above are real: in a clone
// of the same origin set up the same way, a plain `git pull --ff-only` runs each of the
// planted programs a file:// origin reaches, the hooks of the clone's own folder and
// config, and writes outside the clone when its config says so.
func TestAPlainPullWouldRunThePlantedPrograms(t *testing.T) {
	t.Parallel()

	reached := map[string]bool{
		"evil-smudge": true, "evil-gpg": true, "evil-upload-pack": true, alternateRefs: true, "evil-hook": true,
	}

	for _, p := range refusedPlantings() {
		if !reached[p.name] {
			continue
		}

		t.Run(p.name, func(t *testing.T) {
			t.Parallel()

			g := newGitRig(t)
			plain := filepath.Join(g.home, "plain")
			testutil.RunGit(t, g.home, "clone", "-q", g.url(), plain)
			g.pushSigned()

			marker := g.plant(plain, p)

			_, _, err := execpath.Exec{Env: g.environ(), Dir: plain}.Run(t.Context(), "git", "pull", "--ff-only", "-q")
			t.Logf("a plain pull: %v", err) // it may fail once the planted program ran, as a planted upload-pack does
			assert.FileExists(t, marker, "a plain pull runs the clone's own %s", p.name)
		})
	}

	t.Run("hooks", func(t *testing.T) {
		t.Parallel()

		g := newGitRig(t)
		plain := filepath.Join(g.home, "plain")
		testutil.RunGit(t, g.home, "clone", "-q", g.url(), plain)
		g.pushed("two.txt")

		marker := g.plantHooks(plain)

		_, _, err := execpath.Exec{Env: g.environ(), Dir: plain}.Run(t.Context(), "git", "pull", "--ff-only", "-q")
		require.NoError(t, err)
		assert.FileExists(t, marker, "a plain pull runs the clone's own post-merge hook")
	})

	t.Run("core.worktree", func(t *testing.T) {
		t.Parallel()

		g := newGitRig(t)
		plain := filepath.Join(g.home, "plain")
		testutil.RunGit(t, g.home, "clone", "-q", g.url(), plain)
		g.pushed("two.txt")

		elsewhere := filepath.Join(g.home, "elsewhere")
		require.NoError(t, os.Mkdir(elsewhere, 0o700))
		testutil.RunGit(t, plain, "config", "core.worktree", elsewhere)

		_, _, err := execpath.Exec{Env: g.environ(), Dir: plain}.Run(t.Context(), "git", "pull", "--ff-only", "-q")
		t.Logf("a plain pull: %v", err)
		assert.FileExists(t, filepath.Join(elsewhere, "two.txt"), "a plain pull writes where core.worktree says")
	})
}
