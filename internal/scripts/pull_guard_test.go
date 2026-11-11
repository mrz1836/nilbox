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
)

// planted is one way a clone's own config can make git run a program: the settings that
// name it, with %s for the fake program's path, and the fake's name.
type planted struct {
	name     string
	settings [][2]string
}

// plantings are every way a planted clone config could make a pull run its own program:
// the ssh command, the password program, a credential helper, a cookie file, a filter
// driver (the checkout runs its smudge), signature checks, git's upload-pack, the file
// system monitor, and a hooks folder.
func plantings() []planted {
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
		{name: "evil-fsmonitor", settings: [][2]string{{"core.fsmonitor", "%s"}}},
	}
}

// plant sets p in the clone at dir's own config, pointing at a fake program that writes a
// marker file, and returns that file.
func (g *gitRig) plant(dir string, p planted) string {
	g.t.Helper()

	program, marker := g.marker(p.name)

	for _, s := range p.settings {
		testutil.RunGit(g.t, dir, "config", s[0], strings.ReplaceAll(s[1], "%s", program))
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
// to gpg.program, and returns it.
func (g *gitRig) pushSigned() string {
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

	return oid
}

// TestPullRunsNoProgramTheClonesOwnConfigNames plants, in the clone's own config, every
// program a planted config could make a pull run, each a fake that writes a marker file,
// and pulls a signed commit whose .gitattributes names the planted filter driver: the
// pull fast-forwards, the file is written as it is, and no marker appears.
func TestPullRunsNoProgramTheClonesOwnConfigNames(t *testing.T) {
	t.Parallel()

	g := newGitRig(t)
	signed := g.pushSigned()

	markers := map[string]string{"hooks": g.plantHooks(g.dir)}
	for _, p := range plantings() {
		markers[p.name] = g.plant(g.dir, p)
	}

	testutil.RunGit(t, g.dir, "config", "http.cookieFile", filepath.Join(g.home, "cookies"))
	testutil.RunGit(t, g.dir, "config", "http.saveCookies", "true")

	require.Equal(t, BranchBehind, g.status(false).State)

	run := g.pull()
	require.Equal(t, scriptrun.StatusSucceeded, run.Status, run.Summary)
	assert.Equal(t, signed, g.head(g.dir))

	for name, marker := range markers {
		assert.NoFileExists(t, marker, "the pull ran the clone's own %s", name)
	}

	got, err := os.ReadFile(filepath.Join(g.dir, "a.txt"))
	require.NoError(t, err)
	assert.Equal(t, "a\n", string(got), "written as it is, the clone's filter driver turned off")

	argv := strings.Join(run.Argv, " ")
	for _, want := range []string{
		"-c core.askpass=", "-c credential.helper=", "-c http.savecookies=false", "-c http.cookiefile=",
		"-c filter.evil.clean= -c filter.evil.smudge= -c filter.evil.process= -c filter.evil.required=false",
		"--no-verify-signatures", "--upload-pack=git-upload-pack", "-c core.fsmonitor=false",
	} {
		assert.Contains(t, argv, want)
	}

	assert.NoFileExists(t, filepath.Join(g.home, "cookies"))
}

// TestAPlainPullWouldRunThePlantedPrograms shows the plantings above are real: in a clone
// of the same origin set up the same way, a plain `git pull --ff-only` runs each of the
// planted programs a file:// origin reaches.
func TestAPlainPullWouldRunThePlantedPrograms(t *testing.T) {
	t.Parallel()

	reached := map[string]bool{"evil-smudge": true, "evil-gpg": true, "evil-upload-pack": true}

	for _, p := range plantings() {
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
}
