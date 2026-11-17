package fence_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/fence"
)

// Codex names its tools its own way, as its hook hands them over (S6, codex-cli 0.163.0):
// its shell is Bash, its edits are apply_patch with the patch as tool_input.command, and it
// reads an image with view_image. It has no file tools for reading, so under Read only it
// reads through its shell (Policy.ShellReads).

// patch is an apply_patch call of a patch whose body is files.
func patch(files string) fence.Call {
	return fence.Call{Tool: "apply_patch", Command: "*** Begin Patch\n" + files + "*** End Patch\n", Dir: worktree}
}

func TestPatchFilesNamesEveryFileAPatchWrites(t *testing.T) {
	t.Parallel()

	got := fence.PatchFiles("*** Begin Patch\n*** Update File: a.go\n@@\n-x\n+y\n*** Add File: b/c.go\n+z\n" +
		"*** Delete File: old.go\n*** Update File: d.go\n*** Move to: e.go\r\n*** Update File: a.go\n*** End Patch\n")
	assert.Equal(t, []string{"a.go", "b/c.go", "old.go", "d.go", "e.go"}, got, "each once, a move's target too")
	assert.Empty(t, fence.PatchFiles("not a patch"))
}

func TestACodexPatchIsDecidedAsTheWritesItMakes(t *testing.T) {
	t.Parallel()

	runCases(t, fence.EditTestPush, []verdictCase{
		{name: "a source file", call: patch("*** Update File: main.go\n@@\n+x\n")},
		{name: "a new file, absolute", call: patch("*** Add File: " + worktree + "/internal/fix.go\n+x\n")},
		{name: "AGENTS.md", call: patch("*** Update File: " + worktree + "/AGENTS.md\n@@\n+x\n"), deny: true},
		{name: "AGENTS.md among others", call: patch("*** Update File: main.go\n@@\n+x\n*** Add File: AGENTS.md\n+y\n"), deny: true},
		{name: "a workflow", call: patch("*** Update File: .github/workflows/ci.yml\n@@\n+x\n"), deny: true},
		{name: ".codex", call: patch("*** Add File: .codex/config.toml\n+x\n"), deny: true},
		{name: "a move into .claude", call: patch("*** Update File: a.md\n*** Move to: .claude/a.md\n"), deny: true},
		{name: "outside the worktree", call: patch("*** Add File: " + home + "/x.go\n+x\n"), deny: true},
		{name: "git's own folder", call: patch("*** Add File: " + gitDir + "/hooks/pre-commit\n+x\n"), deny: true},
		{name: "a patch naming nothing", call: patch("+x\n"), deny: true},
		{name: "apply_patch on the shell", call: bash("apply_patch '*** Begin Patch\n*** Add File: AGENTS.md\n+x\n*** End Patch'"), deny: true},
		{name: "apply_patch on the shell, a source file", call: bash("apply_patch '*** Begin Patch\n*** Add File: x.go\n+x\n*** End Patch'")},
		{name: "a here-document hides it", call: bash("apply_patch <<'EOF'\n*** Begin Patch\n*** Add File: x.go\n+x\n*** End Patch\nEOF"), deny: true},
	})

	runCases(t, fence.ReadOnly, []verdictCase{
		{name: "read only patches nothing", call: patch("*** Update File: main.go\n@@\n+x\n"), deny: true},
	})
}

func TestCodexsOtherToolsAreDecided(t *testing.T) {
	t.Parallel()

	runCases(t, fence.EditTest, []verdictCase{
		{name: "view an image in the worktree", call: tool("view_image", worktree+"/docs/a.png")},
		{name: "view a key", call: tool("view_image", home+"/.ssh/id_ed25519"), deny: true},
		{name: "web search", call: fence.Call{Tool: "web_search"}, deny: true},
		{name: "an MCP tool", call: fence.Call{Tool: "mcp__browser__open"}, deny: true},
		{name: "code mode, whose nested calls are hooked one by one", call: fence.Call{Tool: "exec", Command: "await tools.exec_command({cmd: 'ls'})"}},
	})
}

// shellReads builds the test run's fence under Read only for an agent that reads through
// its shell.
func shellReads(t *testing.T) fence.Fence {
	t.Helper()

	s := spec(fence.ReadOnly)
	s.ShellReads = true

	f, err := fence.Build(s)
	require.NoError(t, err)
	require.True(t, f.Policy.ShellReads)
	require.Nil(t, f.Sandbox, "Read only is held by the agent's own read-only sandbox")

	return f
}

func TestReadOnlyLetsAShellReaderReadAndNothingElse(t *testing.T) {
	t.Parallel()

	f := shellReads(t)
	fs := checked(f)

	for cmd, deny := range map[string]bool{
		"git diff origin/main...HEAD":       false,
		"git log --oneline -5":              false,
		"rg -n 'func Add' --glob '*.go'":    false,
		"cat add.go && sed -n 1,20p go.mod": false,
		"ls -la | head":                     false,
		"gh pr view 12 --repo acme-org/api": false,
		"find . -name '*.go'":               false,
		"git status 2>/dev/null":            false,
		"touch s6-ro.txt":                   true,
		"echo x > notes.txt":                true,
		"sed -i '' s/a/b/ add.go":           true,
		"find . -delete":                    true,
		"go test ./...":                     true,
		"git commit -am x":                  true,
		"git push origin HEAD:" + branch:    true,
		"gh pr comment 12 --body hi":        true,
		"curl https://example.com":          true,
		"cat " + home + "/.ssh/id_ed25519":  true,
		"sh -c 'cat add.go'":                true,
		"python3 -c 'print(1)'":             true,
	} {
		v := fence.Decide(f.Policy, bash(cmd), fs)
		assert.Equal(t, deny, v.Deny, "%s: %q", cmd, v.Reason)
	}

	plain := build(t, fence.ReadOnly)
	assert.True(t, fence.Decide(plain.Policy, bash("git diff"), checked(plain)).Deny,
		"an agent with file tools of its own runs no command under Read only")

	data, err := plain.Policy.Encode()
	require.NoError(t, err)
	assert.NotContains(t, string(data), "shellReads", "a policy without it reads as an older build wrote it")
}
