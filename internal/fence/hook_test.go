package fence_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/fence"
	"github.com/mrz1836/nilbox/internal/sensitive"
)

func mkdir(t *testing.T, dir string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o700))
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
}

// hookRun is a run's folder on disk with its policy, for the hook mode to read.
type hookRun struct {
	dir string
	wt  string
	f   fence.Fence
}

// newHookRun writes a run's folder for a preset, with its sandbox check already passed.
func newHookRun(t *testing.T, preset fence.Preset) hookRun {
	t.Helper()

	root := t.TempDir()
	r := hookRun{dir: filepath.Join(root, "run"), wt: filepath.Join(root, "wt")}
	mkdir(t, r.wt)

	s := spec(preset)
	s.Worktree, s.RunDir, s.GitDir, s.CommonDir = r.wt, r.dir, filepath.Join(root, "git"), filepath.Join(root, "git")

	f, err := fence.Build(s)
	require.NoError(t, err)

	r.f = f

	data, err := f.Policy.Encode()
	require.NoError(t, err)
	writeFile(t, filepath.Join(r.dir, fence.PolicyFile), string(data))

	if f.Policy.Canary != nil {
		writeFile(t, f.Policy.Canary.Proof, "")
	}

	return r
}

// call runs the hook mode on one tool call, as the agent would, and returns its status
// and what it wrote to stderr.
func (r hookRun) call(t *testing.T, args []string, tool string, input map[string]any) (int, string) {
	t.Helper()

	payload, err := json.Marshal(map[string]any{
		"session_id": "00000000-0000-4000-8000-000000000012", "hook_event_name": "PreToolUse",
		"tool_name": tool, "tool_input": input, "cwd": r.wt, "permission_mode": "dontAsk",
	})
	require.NoError(t, err)

	var stderr bytes.Buffer

	code, ok := fence.RunHook(args, bytes.NewReader(payload), &stderr)
	require.True(t, ok)

	return code, stderr.String()
}

func TestTheHookModeAnswersInTheProtocol(t *testing.T) {
	t.Parallel()

	r := newHookRun(t, fence.EditTestPush)
	args := []string{fence.HookArg(r.dir)}

	code, out := r.call(t, args, "Bash", map[string]any{"command": "go test ./..."})
	assert.Zero(t, code, out)
	assert.Empty(t, out, "a call let through says nothing")

	code, out = r.call(t, args, "Bash", map[string]any{"command": "git push --force origin HEAD:" + branch})
	assert.Equal(t, 2, code, "a refusal blocks the call")
	assert.Contains(t, out, "one push")

	code, _ = r.call(t, args, "Bash", map[string]any{"command": "git push origin HEAD:refs/heads/" + branch})
	assert.Zero(t, code)

	code, out = r.call(t, args, "Edit", map[string]any{"file_path": filepath.Join(r.wt, ".claude", "settings.json")})
	assert.Equal(t, 2, code)
	assert.Contains(t, out, "agent configuration")

	code, _ = r.call(t, args, "Write", map[string]any{"file_path": filepath.Join(r.wt, "main.go"), "content": "x"})
	assert.Zero(t, code)
}

func TestTheHookModeFailsClosed(t *testing.T) {
	t.Parallel()

	r := newHookRun(t, fence.EditTest)

	tests := []struct {
		name  string
		args  []string
		stdin string
	}{
		{name: "no folder", args: []string{fence.HookFlag}, stdin: `{"tool_name":"Bash","tool_input":{"command":"ls"}}`},
		{name: "a relative folder", args: []string{fence.HookFlag + "=run"}, stdin: `{"tool_name":"Bash","tool_input":{"command":"ls"}}`},
		{name: "an unknown protocol", args: []string{"--fence-hook-v9=" + r.dir}, stdin: `{"tool_name":"Bash","tool_input":{"command":"ls"}}`},
		{name: "a folder with no policy", args: []string{fence.HookArg(t.TempDir())}, stdin: `{"tool_name":"Bash","tool_input":{"command":"ls"}}`},
		{name: "not JSON", args: []string{fence.HookArg(r.dir)}, stdin: `ls`},
		{name: "no tool", args: []string{fence.HookArg(r.dir)}, stdin: `{"tool_input":{}}`},
		{name: "an input it can't read", args: []string{fence.HookArg(r.dir)}, stdin: `{"tool_name":"Bash","tool_input":"ls"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer

			code, ok := fence.RunHook(tt.args, strings.NewReader(tt.stdin), &stderr)
			require.True(t, ok)
			assert.Equal(t, 2, code)
			assert.Contains(t, stderr.String(), "fence refused")
		})
	}

	broken := filepath.Join(t.TempDir(), "run")
	writeFile(t, filepath.Join(broken, fence.PolicyFile), `{"version":1,"preset":"everything"}`)

	var stderr bytes.Buffer

	code, _ := fence.RunHook([]string{fence.HookArg(broken)}, strings.NewReader(`{"tool_name":"Read","tool_input":{}}`), &stderr)
	assert.Equal(t, 2, code, "a policy it can't read refuses every call, a read's too")
}

func TestTheHookModeLetsOtherEventsBe(t *testing.T) {
	t.Parallel()

	r := newHookRun(t, fence.ReadOnly)

	var stderr bytes.Buffer

	code, ok := fence.RunHook([]string{fence.HookArg(r.dir)},
		strings.NewReader(`{"hook_event_name":"PostToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}`), &stderr)
	require.True(t, ok)
	assert.Zero(t, code)
}

func TestOtherArgumentsAreNoHookCall(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{nil, {"--debug"}, {"--data-dir=/x"}, {"fence-hook"}} {
		code, ok := fence.RunHook(args, strings.NewReader("{}"), &bytes.Buffer{})
		assert.False(t, ok, "%q", args)
		assert.Zero(t, code)
		assert.False(t, fence.IsHookCall(args))
	}

	assert.True(t, fence.IsHookCall([]string{"--fence-hook-v2=/x"}), "every version is the hook mode, refused or not")
}

func TestTheHookModeWritesNothingButItsRefusals(t *testing.T) {
	t.Parallel()

	r := newHookRun(t, fence.EditTestPush)
	before := listTree(t, filepath.Dir(r.dir))

	for _, c := range []struct {
		tool  string
		input map[string]any
	}{
		{"Bash", map[string]any{"command": "go test ./..."}},
		{"Edit", map[string]any{"file_path": filepath.Join(r.wt, "x.go")}},
		{"Read", map[string]any{"file_path": "/etc/hosts"}},
	} {
		r.call(t, []string{fence.HookArg(r.dir)}, c.tool, c.input)
	}

	assert.Equal(t, before, listTree(t, filepath.Dir(r.dir)), "a call let through writes nothing")

	count, last := fence.ReadDenials(r.dir)
	assert.Zero(t, count)
	assert.Empty(t, last)

	code, _ := r.call(t, []string{fence.HookArg(r.dir)}, "Bash", map[string]any{"command": "git push -f"})
	require.Equal(t, 2, code)

	code, _ = r.call(t, []string{fence.HookArg(r.dir)}, "apply_patch",
		map[string]any{"command": "*** Begin Patch\n*** Update File: AGENTS.md\n@@\n+x\n*** End Patch"})
	require.Equal(t, 2, code)

	after := listTree(t, filepath.Dir(r.dir))
	assert.Equal(t, sortedCopy(append(slices.Clone(before), filepath.Join(r.dir, fence.DenialsFile))), sortedCopy(after),
		"a refusal writes only its line in the run's own log, which an agent's stream may never show")

	count, last = fence.ReadDenials(r.dir)
	require.Equal(t, 2, count)
	assert.Equal(t, "Bash", last[0].Tool)
	assert.Equal(t, "git push -f", last[0].What)
	assert.Contains(t, last[0].Reason, "one push")
	assert.Equal(t, "apply_patch", last[1].Tool)
	assert.Contains(t, last[1].Reason, "AGENTS.md")

	info, err := os.Stat(filepath.Join(r.dir, fence.DenialsFile))
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm(), "the log is the user's alone")

	missing := filepath.Join(filepath.Dir(r.dir), "missing")
	code, _ = r.call(t, []string{fence.HookArg(missing)}, "Bash", map[string]any{"command": "ls"})
	assert.Equal(t, 2, code)
	assert.NoDirExists(t, missing, "a run's folder is never made by its hook")
}

// sortedCopy returns list sorted, as listTree's walk orders a folder's names.
func sortedCopy(list []string) []string {
	out := slices.Clone(list)
	slices.Sort(out)

	return out
}

func TestTheRefusalsLogIsBounded(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	for i := range fence.KeepDenials + 5 {
		fence.LogDenial(dir, fence.Call{Tool: "Bash", Command: "curl " + strconv.Itoa(i)}, "no network tools")
	}

	count, last := fence.ReadDenials(dir)
	assert.Equal(t, fence.KeepDenials+5, count)
	require.Len(t, last, fence.KeepDenials, "the last ones are kept")
	assert.Equal(t, "curl 5", last[0].What)

	link := filepath.Join(t.TempDir(), "run")
	require.NoError(t, os.MkdirAll(link, 0o700))
	target := filepath.Join(t.TempDir(), "elsewhere")
	require.NoError(t, os.Symlink(target, filepath.Join(link, fence.DenialsFile)))
	fence.LogDenial(link, fence.Call{Tool: "Bash", Command: "curl"}, "no")
	assert.NoFileExists(t, target, "never through a link")

	fence.LogDenial("relative", fence.Call{Tool: "Bash"}, "no")
	assert.NoFileExists(t, filepath.Join("relative", fence.DenialsFile))

	count, last = fence.ReadDenials(t.TempDir())
	assert.Zero(t, count)
	assert.NotNil(t, last)
}

// listTree lists every path under root.
func listTree(t *testing.T, root string) []string {
	t.Helper()

	var out []string

	require.NoError(t, filepath.WalkDir(root, func(path string, _ os.DirEntry, err error) error {
		out = append(out, path)

		return err
	}))

	return out
}

func TestCheckChangedFindsAgentFiles(t *testing.T) {
	t.Parallel()

	cfg := sensitive.DefaultConfig()
	cfg.Off = []string{sensitive.CategoryTooling, sensitive.CategoryDependencies}

	check := fence.CheckChanged("abc123", []string{"main.go", ".claude/settings.json", "Makefile", "go.sum"}, cfg)
	assert.Equal(t, sensitive.StatusFlagged, check.Status)
	assert.Equal(t, "abc123", check.Head)
	assert.True(t, check.Complete)

	agent, ok := check.Category(sensitive.CategoryAgent)
	require.True(t, ok, "a run that changed agent configuration says so")
	assert.Equal(t, ".claude/settings.json", agent.Files[0].Path)

	_, ok = check.Category(sensitive.CategoryTooling)
	assert.True(t, ok, "a denied category stays on whatever the user turned off")

	_, ok = check.Category(sensitive.CategoryDependencies)
	assert.False(t, ok, "another category follows the user's settings")

	clean := fence.CheckChanged("abc123", []string{"main.go"}, sensitive.DefaultConfig())
	assert.Equal(t, sensitive.StatusClear, clean.Status)

	many := make([]string, fence.MaxChanged+1)
	for i := range many {
		many[i] = "f.go"
	}

	assert.False(t, fence.CheckChanged("abc", many, sensitive.DefaultConfig()).Complete)
}
