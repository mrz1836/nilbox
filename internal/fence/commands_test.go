package fence_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/fence"
)

// prefix is a command any arguments may follow, and exact one that takes none.
func prefix(words string) fence.Command { return fence.Command{Words: words, Prefix: true} }

func exact(words string) fence.Command { return fence.Command{Words: words} }

func TestAllowedCommandsFollowThePreset(t *testing.T) {
	t.Parallel()

	pushes := []fence.Command{
		exact("git push origin HEAD:refs/heads/" + branch), exact("git push origin HEAD:" + branch),
	}

	tests := []struct {
		preset fence.Preset
		has    []fence.Command
		hasNot []fence.Command
	}{
		{
			preset: fence.GitHubTasks,
			has: []fence.Command{
				prefix("gh pr view"), prefix("gh run list"), prefix("gh pr comment"), prefix("gh run rerun"),
				prefix("git status"), prefix("git log"),
			},
			hasNot: append([]fence.Command{
				prefix("git commit"), prefix("go"), prefix("ls"), exact("gh pr checkout 12"),
				exact("git fetch origin refs/pull/12/head"),
			}, pushes...),
		},
		{
			preset: fence.EditTest,
			has: []fence.Command{
				prefix("go"), prefix("magex"), prefix("ls"), prefix("rm"), prefix("git status"), prefix("git commit"),
				prefix("git rebase"), prefix("gh pr view"), exact("gh pr checkout 12"),
				exact("git fetch origin refs/pull/12/head"), prefix("make lint"),
			},
			hasNot: append([]fence.Command{prefix("gh pr comment"), prefix("git push"), prefix("curl")}, pushes...),
		},
		{
			preset: fence.EditTestPush,
			has: append([]fence.Command{
				prefix("go"), prefix("git commit"), exact("gh pr checkout 12"), exact("git fetch origin refs/pull/12/head"),
			}, pushes...),
			hasNot: []fence.Command{prefix("git push"), prefix("gh pr comment")},
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.preset), func(t *testing.T) {
			t.Parallel()

			s := spec(tt.preset)
			if tt.preset.Edits() {
				s.Allow = []string{"make lint"}
			}

			f, err := fence.Build(s)
			require.NoError(t, err)

			assertCommands(t, fence.AllowedCommands(f.Policy), tt.has, tt.hasNot)
		})
	}
}

// assertCommands checks that got holds every command of has and none of hasNot.
func assertCommands(t *testing.T, got, has, hasNot []fence.Command) {
	t.Helper()

	for _, c := range has {
		assert.Contains(t, got, c)
	}

	for _, c := range hasNot {
		assert.NotContains(t, got, c)
	}
}

func TestReadOnlyAndTheProfileAllowNoCommandOfTheirOwn(t *testing.T) {
	t.Parallel()

	for _, preset := range []fence.Preset{fence.ReadOnly, fence.Profile} {
		got := fence.AllowedCommands(build(t, preset).Policy)
		assert.NotNil(t, got, "%s: an empty list, never null", preset)
		assert.Empty(t, got, "%s: Read only runs no command, and the profile's permissions are its own", preset)
	}
}

func TestEditPresetsAllowNoCheckoutOrFetchWithoutAnItem(t *testing.T) {
	t.Parallel()

	s := spec(fence.EditTest)
	s.Number, s.Remote = 0, "-u"

	f, err := fence.Build(s)
	require.NoError(t, err)

	for _, c := range fence.AllowedCommands(f.Policy) {
		assert.True(t, c.Prefix, "an issue with no plain remote has no exact command: %q", c.Words)
	}
}

func TestDeniedCommandsHoldTheHardLines(t *testing.T) {
	t.Parallel()

	always := []fence.Command{
		prefix("curl"), prefix("ssh"), prefix("sudo"), prefix("osascript"), prefix("eval"), prefix("sh -c"),
		prefix("bash -c"), prefix("gh pr merge"), prefix("gh pr review"), prefix("gh api"), prefix("gh release"),
		prefix("git -C"), prefix("git -c"), prefix("git config --global"), prefix("git remote set-url"),
		prefix("git update-ref"), prefix("git submodule"), prefix("rm -rf"),
	}

	for _, preset := range fence.Presets() {
		t.Run(string(preset), func(t *testing.T) {
			t.Parallel()

			s := spec(preset)
			s.Deny = []string{"rm  -rf"}

			f, err := fence.Build(s)
			require.NoError(t, err)

			got := fence.DeniedCommands(f.Policy)

			for _, c := range always {
				assert.Contains(t, got, c)
			}

			if f.Policy.Push == nil {
				assert.Contains(t, got, prefix("git push"), "a preset with no push denies every push")
			} else {
				assert.NotContains(t, got, prefix("git push"), "a deny would beat the one push the preset allows")
			}
		})
	}
}

func TestDeniedPathsNameTheDeniedFilesAtTheirDepth(t *testing.T) {
	t.Parallel()

	f := build(t, fence.EditTest)
	got := fence.DeniedPaths(f.Policy)

	for _, want := range []string{
		worktree + "/**/.claude", worktree + "/**/.claude/**", // a folder at any depth, and all inside it
		worktree + "/**/CLAUDE.md",                                          // a file at any depth
		worktree + "/.github/workflows", worktree + "/.github/workflows/**", // a path from the root
		worktree + "/**/*.sh",             // a glob at any depth
		worktree + "/**/.git/**",          // every git folder in the worktree
		gitDir + "/**", commonDir + "/**", // and its git folders
	} {
		assert.Contains(t, got, want)
	}

	assert.NotContains(t, got, worktree+"/**/.github/workflows", "a pattern with a folder is rooted")

	seen := map[string]bool{}
	for _, p := range got {
		assert.False(t, seen[p], "each path once: %s", p)
		seen[p] = true
	}
}

func TestDeniedPathsNameTheWorktreeBothWays(t *testing.T) {
	t.Parallel()

	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "pr-12")
	require.NoError(t, os.Symlink(target, link))

	s := spec(fence.EditTest)
	s.Worktree = link

	f, err := fence.Build(s)
	require.NoError(t, err)
	require.NotEmpty(t, f.Policy.WorktreeAs, "the run names its worktree through a link")

	got := fence.DeniedPaths(f.Policy)
	assert.Contains(t, got, f.Policy.Worktree+"/**/CLAUDE.md")
	assert.Contains(t, got, link+"/**/CLAUDE.md", "the path the run names it by too")
}

// mode returns a file's permission bits.
func mode(t *testing.T, path string) os.FileMode {
	t.Helper()

	info, err := os.Lstat(path)
	require.NoError(t, err)

	return info.Mode().Perm()
}

func TestWriteRunKeepsTheRunsFilesPrivate(t *testing.T) {
	t.Parallel()

	for _, preset := range []fence.Preset{fence.ReadOnly, fence.EditTest} {
		t.Run(string(preset), func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(t.TempDir(), "run")
			require.NoError(t, os.MkdirAll(dir, 0o755)) //nolint:gosec // a folder looser than the run's, to tighten

			f := build(t, preset)
			require.NoError(t, fence.WriteRun(dir, f, fence.File{Name: "settings.json", Data: []byte(`{"a":1}`)}))

			assert.Equal(t, os.FileMode(0o700), mode(t, dir), "the folder is the user's alone")
			assert.Equal(t, os.FileMode(0o600), mode(t, filepath.Join(dir, fence.PolicyFile)))
			assert.Equal(t, os.FileMode(0o600), mode(t, filepath.Join(dir, "settings.json")))

			data, err := os.ReadFile(filepath.Join(dir, "settings.json")) //nolint:gosec // the test's own file
			require.NoError(t, err)
			assert.JSONEq(t, `{"a":1}`, string(data))

			policy, err := fence.ReadPolicy(dir)
			require.NoError(t, err)
			assert.Equal(t, f.Policy, policy, "the hook reads back the policy written")

			if preset.Sandboxed() {
				assert.Equal(t, os.FileMode(0o700), mode(t, filepath.Join(dir, fence.ProofDir)),
					"a sandboxed run gets the folder its check writes its proof into")
			} else {
				assert.NoFileExists(t, filepath.Join(dir, fence.ProofDir))
			}
		})
	}
}

func TestWriteRunRefusesWhatItCantWritePrivately(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// setup returns the run's folder, and what the run's extra file is named.
		setup func(t *testing.T) (dir, file string)
		err   error
	}{
		{
			name:  "a relative folder",
			setup: func(*testing.T) (string, string) { return "agent-runs/run", "settings.json" },
			err:   fence.ErrSpec,
		},
		{
			name:  "a file with no name",
			setup: func(t *testing.T) (string, string) { return t.TempDir(), "" },
			err:   fence.ErrSpec,
		},
		{
			name:  "a file in another folder",
			setup: func(t *testing.T) (string, string) { return t.TempDir(), "../settings.json" },
			err:   fence.ErrSpec,
		},
		{
			name:  "a hidden file",
			setup: func(t *testing.T) (string, string) { return t.TempDir(), ".settings.json" },
			err:   fence.ErrSpec,
		},
		{
			name: "a link in the folder's place",
			setup: func(t *testing.T) (string, string) {
				link := filepath.Join(t.TempDir(), "run")
				require.NoError(t, os.Symlink(t.TempDir(), link))

				return link, "settings.json"
			},
			err: fence.ErrSpec,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir, file := tt.setup(t)
			err := fence.WriteRun(dir, build(t, fence.EditTest), fence.File{Name: file, Data: []byte("{}")})
			require.ErrorIs(t, err, tt.err)
		})
	}
}

func TestWriteRunNeverWritesThroughALink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	elsewhere := filepath.Join(t.TempDir(), "elsewhere.json")
	require.NoError(t, os.WriteFile(elsewhere, []byte("kept"), 0o600))
	require.NoError(t, os.Symlink(elsewhere, filepath.Join(dir, "settings.json")))

	err := fence.WriteRun(dir, build(t, fence.EditTest), fence.File{Name: "settings.json", Data: []byte("{}")})
	require.Error(t, err)

	data, err := os.ReadFile(elsewhere) //nolint:gosec // the test's own file
	require.NoError(t, err)
	assert.Equal(t, "kept", string(data), "the file the link points at is left alone")
}

func TestWriteRunFailsWhereNoFolderCanBeMade(t *testing.T) {
	t.Parallel()

	file := filepath.Join(t.TempDir(), "run")
	require.NoError(t, os.WriteFile(file, nil, 0o600))

	require.Error(t, fence.WriteRun(file, build(t, fence.ReadOnly)), "a file in the folder's place")
	require.Error(t, fence.WriteRun(filepath.Join(file, "sub"), build(t, fence.ReadOnly)), "a file in its parent's")

	data, err := os.ReadFile(file) //nolint:gosec // the test's own file
	require.NoError(t, err)
	assert.Empty(t, data, "the file is left as it was")
}

func TestReadPolicyRefusesWhatTheHookCantTrust(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(t *testing.T) string
		says  string
	}{
		{
			name:  "a relative folder",
			setup: func(*testing.T) string { return "agent-runs/run" },
			says:  "isn't an absolute path",
		},
		{
			name:  "no policy",
			setup: func(t *testing.T) string { return t.TempDir() },
			says:  "no such file",
		},
		{
			name: "a policy too large",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, fence.PolicyFile), make([]byte, 1<<20+1), 0o600))

				return dir
			},
			says: "too large",
		},
		{
			name: "a sandbox check with a relative file",
			setup: func(t *testing.T) string {
				p := build(t, fence.EditTest).Policy
				p.Canary = &fence.Canary{Proof: "sandbox/tried", Probe: p.Canary.Probe}

				data, err := p.Encode()
				require.NoError(t, err)

				dir := t.TempDir()
				require.NoError(t, os.WriteFile(filepath.Join(dir, fence.PolicyFile), data, 0o600))

				return dir
			},
			says: "sandbox check's files",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := fence.ReadPolicy(tt.setup(t))
			require.ErrorIs(t, err, fence.ErrPolicy)
			assert.Contains(t, err.Error(), tt.says)
		})
	}
}

func TestSandboxedReadsWhatTheCheckLeft(t *testing.T) {
	t.Parallel()

	sandboxed := build(t, fence.EditTest)
	require.NotNil(t, sandboxed.Policy.Canary)

	tests := []struct {
		name   string
		policy fence.Policy
		exists []string
		want   fence.SandboxState
	}{
		{name: "no sandbox", policy: build(t, fence.GitHubTasks).Policy, want: fence.SandboxNone},
		{name: "the check never ran", policy: sandboxed.Policy, want: fence.SandboxUnchecked},
		{
			name: "the check was refused its second file", policy: sandboxed.Policy,
			exists: []string{sandboxed.Policy.Canary.Proof}, want: fence.SandboxHeld,
		},
		{
			name: "the check wrote its second file", policy: sandboxed.Policy,
			exists: []string{sandboxed.Policy.Canary.Proof, sandboxed.Policy.Canary.Probe}, want: fence.SandboxLeaked,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fs := checked(fence.Fence{})
			for _, p := range tt.exists {
				fs.exists[p] = true
			}

			assert.Equal(t, tt.want, fence.Sandboxed(tt.policy, fs))
		})
	}

	assert.Equal(t, fence.SandboxUnchecked, fence.Sandboxed(sandboxed.Policy, nil),
		"no file system reads the real one, where the run's folder isn't")
}

func TestAgentNoteSaysWhatTheFenceAllows(t *testing.T) {
	t.Parallel()

	pushed := build(t, fence.EditTestPush)
	check := pushed.Policy.Canary.Command()

	tests := []struct {
		preset fence.Preset
		want   string
	}{
		{preset: fence.ReadOnly, want: "Your tools are fenced: Read only."},
		{preset: fence.GitHubTasks, want: "Your tools are fenced: GitHub tasks only."},
		{
			preset: fence.EditTest,
			want: "Your tools are fenced: Edit and test, no push. Push nothing. " + fence.SandboxWords +
				" Before any other command, run `" + check + "` on its own: it checks the sandbox, and its second " +
				"file is meant to fail with \"Operation not permitted\".",
		},
		{
			preset: fence.EditTestPush,
			want: "Your tools are fenced: Edit, test, and push to the PR's branch. The one push allowed is `git push " +
				"origin HEAD:refs/heads/" + branch + "`, exactly as written. " + fence.SandboxWords +
				" Before any other command, run `" + check + "` on its own: it checks the sandbox, and its second " +
				"file is meant to fail with \"Operation not permitted\".",
		},
	}

	for _, tt := range tests {
		t.Run(string(tt.preset), func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, build(t, tt.preset).AgentNote())
		})
	}

	note := pushed.NoteWith("Commands run in the agent's own sandbox.")
	assert.Contains(t, note, " Commands run in the agent's own sandbox. Before any other command")
	assert.NotContains(t, note, fence.SandboxWords, "another agent's sandbox says its own words")

	s := spec(fence.Profile)
	s.CrossRepository = true

	f, err := fence.Build(s)
	require.NoError(t, err)
	assert.Equal(t, "Your tools are fenced: The profile's own permissions. Push nothing.", f.AgentNote(),
		"the profile's own permissions edit, with no push on a fork's pull request")
	assert.NotContains(t, f.AgentNote(), "sandbox", "and run no sandbox check")
}
