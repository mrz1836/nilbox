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

// refusedPlantings are every way a planted clone config could make a pull run its own
// program that no setup sets aside, each of which refuses the pull before git runs: a
// filter driver (the checkout runs its smudge), signature checks, git's upload-pack, an
// alternate refs command, a hook named in the config (which core.hooksPath doesn't
// reach), gc's recent objects hook, which the automatic maintenance a fetch and a merge
// start runs, and Git LFS's own transfer agent.
func refusedPlantings() []testutil.Planting {
	return []testutil.Planting{
		{Name: "evil-smudge", Settings: [][2]string{
			{"filter.evil.smudge", "%s"}, {"filter.evil.clean", "%s"}, {"filter.evil.required", "true"},
		}},
		{Name: "evil-gpg", Settings: [][2]string{
			{"merge.verifySignatures", "true"}, {"gpg.program", "%s"}, {"gpg.ssh.program", "%s"},
		}},
		{Name: "evil-upload-pack", Settings: [][2]string{{"remote.origin.uploadpack", "%s"}}},
		{Name: testutil.AlternateRefs, Settings: [][2]string{{"core.alternateRefsCommand", "%s"}}},
		{Name: "evil-hook", Settings: [][2]string{{"hook.evil.command", "%s"}, {"hook.evil.event", "post-merge"}}},
		{Name: "evil-gc", Settings: [][2]string{
			{"gc.recentObjectsHook", "%s"},
			{"gc.cruftPacks", "true"},
			{"gc.autoPackLimit", "1"},
			{"gc.autoDetach", "false"},
			{"fetch.unpackLimit", "1"},
			{"maintenance.strategy", "gc"},
			{"maintenance.autoDetach", "false"},
			{"gc.auto", "1"},
		}},
		{Name: "evil-lfs-agent", Settings: [][2]string{
			{"lfs.standalonetransferagent", "evil"}, {"lfs.customtransfer.evil.path", "%s"},
		}},
	}
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
		t.Run(p.Name, func(t *testing.T) {
			t.Parallel()

			g := newGitRig(t)
			g.pushSigned()
			require.Equal(t, BranchBehind, g.status(false).State)

			marker := g.plants.Plant(g.dir, p)

			_, err := g.branch.Pull(t.Context(), g.shown(g.dir))
			require.ErrorIs(t, err, ErrCantPull)
			assert.Contains(t, uierr.Message(err), "This clone's own git config sets "+p.Keys()[0])
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

// TestPullWithTheClonesOwnTransportSettings pulls into a clone whose own config names an
// ssh command, a password program, and a credential helper, each a fake that writes a
// marker file, and rewrites origin's URL with insteadOf to the file:// origin it was
// cloned from: the pull runs, and none of the clone's programs does.
func TestPullWithTheClonesOwnTransportSettings(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	two := g.pushed("two.txt")
	markers := map[string]string{}

	for _, p := range []testutil.Planting{
		{Name: "evil-ssh", Settings: [][2]string{{"core.sshCommand", "%s"}}},
		{Name: "evil-askpass", Settings: [][2]string{{"core.askPass", "%s"}}},
		{Name: "evil-credential", Settings: [][2]string{{"credential.helper", "!%s"}}},
	} {
		markers[p.Name] = g.plants.Plant(g.dir, p)
	}

	const url = "https://example.invalid/acme-org/example-repo.git"

	testutil.RunGit(t, g.dir, "config", "url."+g.url()+".insteadOf", url)
	testutil.RunGit(t, g.dir, "remote", "set-url", "origin", url)
	testutil.RunGit(t, g.dir, "config", "http.postBuffer", "524288000")
	require.Equal(t, BranchBehind, g.status(false).State)

	run := g.pull()
	require.Equal(t, scriptrun.StatusSucceeded, run.Status, run.Summary)
	assert.Equal(t, two, g.head(g.dir))

	for name, marker := range markers {
		assert.NoFileExists(t, marker, "the pull ran the clone's own %s", name)
	}
}

// TestPullSaysWhichOfTheClonesOwnSettingsItWentWithout pulls over ssh in a clone whose own
// config names its ssh command, from an environment whose own GIT_SSH is a fake that
// answers nothing: the user's ssh runs, never the clone's, and the failed pull says the
// clone's own was set aside, which may be why.
func TestPullSaysWhichOfTheClonesOwnSettingsItWentWithout(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	g.status(false)

	evil := g.plants.Plant(g.dir, testutil.Planting{Name: "evil-ssh", Settings: [][2]string{{"core.sshCommand", "%s"}}})
	mine, ran := g.plants.Marker("my-ssh")
	g.plants.AddEnv("GIT_SSH=" + mine)

	testutil.RunGit(t, g.dir, "remote", "set-url", "origin", "ssh://git@example.invalid/acme-org/example-repo.git")

	run := g.pull()
	assert.Equal(t, scriptrun.StatusFailed, run.Status)
	assert.Equal(t, "Couldn't pull main: Could not read from remote repository. This clone's own core.sshCommand "+
		"isn't used to reach origin, which may be why: set it for this folder in your global git config, with an "+
		"includeIf.", run.Summary)
	assert.FileExists(t, ran, "the user's own ssh ran")
	assert.NoFileExists(t, evil, "the clone's never did")
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
		"hooks":     g.plants.PlantHooks(g.dir, "post-merge"),
		"fsmonitor": g.plants.Plant(g.dir, testutil.Planting{Name: "evil-fsmonitor", Settings: [][2]string{{"core.fsmonitor", "%s"}}}),
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
		"evil-smudge": true, "evil-gpg": true, "evil-upload-pack": true, testutil.AlternateRefs: true, "evil-hook": true,
	}

	for _, p := range refusedPlantings() {
		if !reached[p.Name] {
			continue
		}

		t.Run(p.Name, func(t *testing.T) {
			t.Parallel()

			g := newGitRig(t)
			plain := filepath.Join(g.home, "plain")
			testutil.RunGit(t, g.home, "clone", "-q", g.url(), plain)
			g.pushSigned()

			marker := g.plants.Plant(plain, p)

			_, _, err := execpath.Exec{Env: g.plants.Environ(), Dir: plain}.Run(t.Context(), "git", "pull", "--ff-only", "-q")
			t.Logf("a plain pull: %v", err) // it may fail once the planted program ran, as a planted upload-pack does
			assert.FileExists(t, marker, "a plain pull runs the clone's own %s", p.Name)
		})
	}

	t.Run("hooks", func(t *testing.T) {
		t.Parallel()

		g := newGitRig(t)
		plain := filepath.Join(g.home, "plain")
		testutil.RunGit(t, g.home, "clone", "-q", g.url(), plain)
		g.pushed("two.txt")

		marker := g.plants.PlantHooks(plain, "post-merge")

		_, _, err := execpath.Exec{Env: g.plants.Environ(), Dir: plain}.Run(t.Context(), "git", "pull", "--ff-only", "-q")
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

		_, _, err := execpath.Exec{Env: g.plants.Environ(), Dir: plain}.Run(t.Context(), "git", "pull", "--ff-only", "-q")
		t.Logf("a plain pull: %v", err)
		assert.FileExists(t, filepath.Join(elsewhere, "two.txt"), "a plain pull writes where core.worktree says")
	})
}
