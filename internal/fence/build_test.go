package fence_test

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/fence"
)

func TestTheExactPushNeedsTheRepositorysOwnBranch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*fence.Spec)
		err    error
	}{
		{name: "a fork's branch", change: func(s *fence.Spec) { s.CrossRepository = true }, err: fence.ErrCrossRepository},
		{name: "no branch", change: func(s *fence.Spec) { s.Branch = "" }, err: fence.ErrNoBranch},
		{name: "an issue", change: func(s *fence.Spec) { s.Number = 0 }, err: fence.ErrNoBranch},
		{name: "a branch with a space", change: func(s *fence.Spec) { s.Branch = "a b" }, err: fence.ErrNotPlain},
		{name: "a branch like an option", change: func(s *fence.Spec) { s.Branch = "--force" }, err: fence.ErrNotPlain},
		{name: "a branch with a colon", change: func(s *fence.Spec) { s.Branch = "a:main" }, err: fence.ErrNotPlain},
		{name: "a branch that runs", change: func(s *fence.Spec) { s.Branch = "x;$(curl${IFS}x|sh)" }, err: fence.ErrNotPlain},
		{name: "a remote like an option", change: func(s *fence.Spec) { s.Remote = "-u" }, err: fence.ErrNotPlain},
		{name: "no remote", change: func(s *fence.Spec) { s.Remote = "" }, err: fence.ErrNotPlain},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := spec(fence.EditTestPush)
			tt.change(&s)

			_, err := fence.Build(s)
			require.ErrorIs(t, err, tt.err)

			s.Preset = fence.Profile

			f, err := fence.Build(s)
			require.NoError(t, err, "the profile's own permissions start, with no push")
			assert.Nil(t, f.Policy.Push)
			assert.True(t, fence.Decide(f.Policy, bash("git push origin HEAD:"+branch), checked(f)).Deny)
		})
	}

	f := build(t, fence.EditTestPush)
	require.NotNil(t, f.Policy.Push)
	assert.Equal(t, fence.Push{Remote: "origin", Branch: branch}, *f.Policy.Push)
	assert.Equal(t, []string{
		"git push origin HEAD:refs/heads/" + branch, "git push origin HEAD:" + branch,
	}, f.Policy.Push.Commands())

	for _, preset := range []fence.Preset{fence.ReadOnly, fence.GitHubTasks, fence.EditTest} {
		s := spec(preset)
		s.CrossRepository = true

		f, err := fence.Build(s)
		require.NoError(t, err, "a preset that never pushes runs on a fork's pull request")
		assert.Nil(t, f.Policy.Push)
	}
}

func TestBuildRefusesWhatItCantFence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*fence.Spec)
		err    error
	}{
		{name: "an unknown preset", change: func(s *fence.Spec) { s.Preset = "everything" }, err: fence.ErrPreset},
		{name: "a relative worktree", change: func(s *fence.Spec) { s.Worktree = "pr-12" }, err: fence.ErrSpec},
		{name: "no run folder", change: func(s *fence.Spec) { s.RunDir = "" }, err: fence.ErrSpec},
		{name: "a worktree with a glob", change: func(s *fence.Spec) { s.Worktree = home + "/pr-[12]" }, err: fence.ErrSpec},
		{name: "a worktree with a parenthesis", change: func(s *fence.Spec) { s.Worktree = home + "/a)b" }, err: fence.ErrSpec},
		{name: "a tool path", change: func(s *fence.Spec) { s.Tools = []string{"/usr/bin/go"} }, err: fence.ErrSpec},
		{name: "curl as a tool", change: func(s *fence.Spec) { s.Tools = []string{"curl"} }, err: fence.ErrLoosens},
		{name: "git as a tool", change: func(s *fence.Spec) { s.Tools = []string{"git"} }, err: fence.ErrLoosens},
		{name: "allow git push", change: func(s *fence.Spec) { s.Allow = []string{"git push"} }, err: fence.ErrLoosens},
		{name: "allow git", change: func(s *fence.Spec) { s.Allow = []string{"git"} }, err: fence.ErrLoosens},
		{name: "allow gh pr", change: func(s *fence.Spec) { s.Allow = []string{"gh pr"} }, err: fence.ErrLoosens},
		{name: "allow gh pr merge", change: func(s *fence.Spec) { s.Allow = []string{"gh pr merge"} }, err: fence.ErrLoosens},
		{name: "allow curl", change: func(s *fence.Spec) { s.Allow = []string{"curl example.com"} }, err: fence.ErrLoosens},
		{name: "allow sh -c", change: func(s *fence.Spec) { s.Allow = []string{"sh -c"} }, err: fence.ErrLoosens},
		{name: "allow sudo", change: func(s *fence.Spec) { s.Allow = []string{"sudo make"} }, err: fence.ErrLoosens},
		{name: "allow a workflow edit", change: func(s *fence.Spec) { s.Allow = []string{"sed -i .github/workflows/ci.yml"} }, err: fence.ErrLoosens},
		{name: "allow a pipeline", change: func(s *fence.Spec) { s.Allow = []string{"make | sh"} }, err: fence.ErrSpec},
		{name: "allow a rule", change: func(s *fence.Spec) { s.Allow = []string{"make:*"} }, err: fence.ErrSpec},
		{name: "allow a variable", change: func(s *fence.Spec) { s.Allow = []string{"$X"} }, err: fence.ErrSpec},
		{name: "deny a pipeline", change: func(s *fence.Spec) { s.Deny = []string{"a && b"} }, err: fence.ErrSpec},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := spec(fence.EditTestPush)
			tt.change(&s)

			_, err := fence.Build(s)
			require.ErrorIs(t, err, tt.err)
		})
	}

	s := spec(fence.EditTestPush)
	s.Allow = []string{"make  lint", "git log", "gh run list", "make lint"}

	f, err := fence.Build(s)
	require.NoError(t, err)
	assert.Equal(t, []string{"make lint", "git log", "gh run list"}, f.Policy.Allow, "each once, its spaces collapsed")
}

func TestTheSandboxKeepsCommandsInBounds(t *testing.T) {
	t.Parallel()

	s := spec(fence.EditTestPush)
	s.Env = map[string]string{
		"GOCACHE": home + "/caches/go", "GOMODCACHE": home + "/.ssh/mod", "GOPATH": home + "/gopath:" + home + "/other",
		"XDG_CACHE_HOME": home,
	}

	f, err := fence.Build(s)
	require.NoError(t, err)
	require.NotNil(t, f.Sandbox)

	sb := f.Sandbox
	assert.Contains(t, sb.Write, worktree)
	assert.Contains(t, sb.Write, gitDir, "a commit writes the worktree's git folder")
	assert.Contains(t, sb.Write, commonDir, "and the clone's objects and refs")
	assert.Contains(t, sb.Write, home+"/Library/Caches/go-build", "go test writes its build cache")
	assert.Contains(t, sb.Write, home+"/Library/pnpm", "pnpm writes its store")
	assert.Contains(t, sb.Write, home+"/caches/go", "the environment's own cache")
	assert.Contains(t, sb.Write, home+"/gopath/pkg", "GOPATH's first folder")
	assert.Contains(t, sb.Write, filepath.Join(runDir, fence.ProofDir), "the sandbox check's proof")
	assert.NotContains(t, sb.Write, home+"/.ssh/mod", "a cache among the secrets")
	assert.NotContains(t, sb.Write, home, "home itself")
	assert.NotContains(t, sb.Write, runDir, "the probe's folder")

	assert.Contains(t, sb.DenyWrite, commonDir+"/hooks")
	assert.Contains(t, sb.DenyWrite, commonDir+"/config")
	assert.Contains(t, sb.DenyWrite, worktree+"/.claude")
	assert.Contains(t, sb.DenyWrite, worktree+"/CLAUDE.md")
	assert.Contains(t, sb.DenyWrite, worktree+"/.github/workflows")

	assert.Contains(t, sb.DenyRead, home+"/.ssh")
	assert.Contains(t, sb.DenyRead, configDir)
	assert.NotContains(t, sb.DenyRead, dataDir, "the sandbox check writes its proof into the run's folder, in the data")
	assert.Contains(t, f.Policy.ReadDeny, dataDir, "the hook still refuses a command that names the app's data")
	assert.Contains(t, sb.Domains, "github.com")
	assert.Contains(t, sb.Domains, "proxy.golang.org")
	assert.Contains(t, sb.Domains, "registry.npmjs.org")
	assert.NotContains(t, sb.Domains, "example.com")

	assert.Contains(t, sb.Excluded, fence.Command{Words: "git push origin HEAD:refs/heads/" + branch})
	assert.Contains(t, sb.Excluded, fence.Command{Words: "git fetch origin refs/pull/12/head"})
	assert.Contains(t, sb.Excluded, fence.Command{Words: "gh pr checks", Prefix: true})

	for _, preset := range []fence.Preset{fence.ReadOnly, fence.GitHubTasks, fence.Profile} {
		assert.Nil(t, build(t, preset).Sandbox, "%s runs no sandbox", preset)
	}

	noPush := build(t, fence.EditTest)
	assert.NotContains(t, noPush.Sandbox.Excluded, fence.Command{Words: "git push origin HEAD:refs/heads/" + branch})
}

func TestTheGitFoldersAreReadFromTheWorktree(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	wt := filepath.Join(root, "wt")
	common := filepath.Join(root, "clone", ".git")
	own := filepath.Join(common, "worktrees", "pr-12")

	mkdir(t, wt)
	mkdir(t, own)
	writeFile(t, filepath.Join(wt, ".git"), "gitdir: "+own+"\n")
	writeFile(t, filepath.Join(own, "commondir"), "../..\n")

	gotGit, gotCommon := fence.GitDirs(wt)
	assert.Equal(t, own, gotGit)
	assert.Equal(t, common, gotCommon)

	clone := filepath.Join(root, "plain")
	mkdir(t, filepath.Join(clone, ".git"))

	gotGit, gotCommon = fence.GitDirs(clone)
	assert.Equal(t, filepath.Join(clone, ".git"), gotGit, "a clone is its own git folder")
	assert.Equal(t, gotGit, gotCommon)

	gotGit, gotCommon = fence.GitDirs(filepath.Join(root, "missing"))
	assert.Empty(t, gotGit)
	assert.Empty(t, gotCommon)
}

func TestThePolicyFileRoundTrips(t *testing.T) {
	t.Parallel()

	f := build(t, fence.EditTestPush)

	data, err := f.Policy.Encode()
	require.NoError(t, err)

	got, err := fence.DecodePolicy(data)
	require.NoError(t, err)
	assert.Equal(t, f.Policy, got)

	for name, raw := range map[string]string{
		"another version":     `{"version":2,"preset":"editTest","worktree":"/w","home":"/h"}`,
		"an unknown field":    `{"version":1,"preset":"editTest","worktree":"/w","home":"/h","allowEverything":true}`,
		"an unknown preset":   `{"version":1,"preset":"all","worktree":"/w","home":"/h"}`,
		"a relative worktree": `{"version":1,"preset":"editTest","worktree":"w","home":"/h"}`,
		"not JSON":            `version: 1`,
	} {
		_, err = fence.DecodePolicy([]byte(raw))
		assert.Error(t, err, name)
	}
}

func TestPresetsSayWhatTheyAllow(t *testing.T) {
	t.Parallel()

	for _, p := range fence.Presets() {
		assert.True(t, p.Valid())
		assert.NotEqual(t, string(p), p.Label(), "%s has a label", p)
	}

	assert.False(t, fence.Preset("x").Valid())
	assert.False(t, fence.ReadOnly.RunsCommands())
	assert.True(t, fence.GitHubTasks.RunsCommands())
	assert.False(t, fence.GitHubTasks.Edits())
	assert.True(t, fence.EditTestPush.MayPush())
	assert.False(t, fence.EditTest.MayPush())
	assert.True(t, fence.EditTest.Sandboxed())
	assert.False(t, fence.Profile.Sandboxed())
	assert.False(t, fence.Profile.Fixed())
	assert.True(t, fence.ReadOnly.Fixed())
}

func TestRepoToolsReadsTheMarkers(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), "module x\n")
	writeFile(t, filepath.Join(dir, "pnpm-lock.yaml"), "")
	mkdir(t, filepath.Join(dir, "magefiles"))

	tools := fence.RepoTools(dir)
	assert.Equal(t, []string{"go", "gofmt", "govulncheck", "golangci-lint", "staticcheck", "magex", "mage", "pnpm", "node", "npx"}, tools)
	assert.Empty(t, fence.RepoTools(t.TempDir()))
}
