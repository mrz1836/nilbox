package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/fence"
)

// codexShellTool is the name Codex's hook gives its shell.
const codexShellTool = "Bash"

// The Codex goldens' session, and the draft report schema the plan wrote, which Codex's
// strict mode refused (S6).
const (
	codexSession = "00000000-0000-4000-8000-000000000012"
	draftSchema  = `{"type":"object","required":["outcome","summary","pushed","needsPerson"],` +
		`"properties":{"outcome":{"enum":["fixed","nothing_to_do","partial","blocked","failed"]},` +
		`"summary":{"type":"string","maxLength":2000},"pushed":{"type":"boolean"},` +
		`"pushedCommit":{"type":"string","pattern":"^[0-9a-f]{40}$"},"needsPerson":{"type":"boolean"},` +
		`"reason":{"type":"string","maxLength":500}}}`
)

// codexPresets are the presets Codex keeps, each from the access its golden's profile
// has: a mode for the profile's own, full access for the fixed presets, which none keeps.
func codexPresets() map[fence.Preset]Access {
	return map[fence.Preset]Access{
		fence.ReadOnly: {Kind: AccessFull}, fence.EditTest: {Kind: AccessFull}, fence.EditTestPush: {Kind: AccessFull},
		fence.Profile: {Kind: AccessMode, Mode: "workspace-write"},
	}
}

// codexRun is a preset's whole Codex run: its argv and the files in its folder.
type codexRun struct {
	argv  []string
	files []fence.File
	built fence.Fence
}

// codexFenced builds a preset's background run for Codex, in the golden's folders or in
// worktree and runDir, as the launcher does: the fence worked out for the worktree, then
// the argv with it.
func codexFenced(t *testing.T, preset fence.Preset, base Access, worktree, runDir string) codexRun {
	t.Helper()

	a := FenceAccess(preset, base).InRun(runDir)

	prepared, err := PrepareFence(Codex, a, testSpec(preset, worktree, runDir), testProgram)
	require.NoError(t, err)

	argv, err := HeadlessArgs(Codex, HeadlessOptions{
		Model: "gpt-6-sol", Effort: "low", Access: a.Worked(prepared.Fence.Policy, testProgram), SessionID: codexSession,
		Schema: draftSchema, SystemNote: "You run unattended.\n\n" + prepared.Note,
	})
	require.NoError(t, err)

	files, err := HeadlessFiles(Codex, draftSchema)
	require.NoError(t, err)

	return codexRun{argv: argv, files: append(prepared.Files, files...), built: prepared.Fence}
}

func TestCodexFenceGoldens(t *testing.T) {
	t.Parallel()

	for preset, base := range codexPresets() {
		t.Run(string(preset), func(t *testing.T) {
			t.Parallel()

			run := codexFenced(t, preset, base, testWorktree, testRunDir)

			var b strings.Builder

			b.WriteString("# The argv and files a Codex background run gets under this tools preset. Rewrite with\n")
			b.WriteString("# go test ./internal/agent -run TestCodexFenceGoldens -update\n\nargv:\n")

			for _, a := range run.argv {
				fmt.Fprintf(&b, "  %q\n", a)
			}

			for _, file := range run.files {
				fmt.Fprintf(&b, "\n%s:\n%s", file.Name, file.Data)
			}

			path := filepath.Join("testdata", "fence", "codex-"+string(preset)+".golden")

			if *update {
				require.NoError(t, os.WriteFile(path, []byte(b.String()), 0o600))
			}

			want, err := os.ReadFile(path) //nolint:gosec // the test's own golden file
			require.NoError(t, err, "run with -update to write it")
			assert.Equal(t, string(want), b.String())
		})
	}
}

func TestCodexKeepsEveryPresetButGitHubTasks(t *testing.T) {
	t.Parallel()

	for _, keep := range Presets(Codex) {
		if keep.Preset == fence.GitHubTasks {
			assert.False(t, keep.Kept)
			assert.Contains(t, keep.Why, "Codex can't keep GitHub tasks only")

			continue
		}

		assert.True(t, keep.Kept, "%s: %s", keep.Preset, keep.Why)
		assert.Empty(t, keep.Why)
	}

	a := FenceAccess(fence.GitHubTasks, Access{}).InRun(testRunDir)
	_, err := HeadlessArgs(Codex, HeadlessOptions{SessionID: codexSession, Access: a})
	require.ErrorIs(t, err, ErrPresetNotKept, "a preset it can't keep never runs weaker")

	claude, err := Lookup(Claude)
	require.NoError(t, err)
	assert.Len(t, claude.Background.Presets, len(fence.Presets()))
	assert.True(t, claude.Background.Budget)

	codexInfo, err := Lookup(Codex)
	require.NoError(t, err)
	assert.Equal(t, Presets(Codex), codexInfo.Background.Presets, "the builder reads what it keeps from its info")
	assert.False(t, codexInfo.Background.Budget, "no budget of its own: its runs never outlive the app's quit")
	assert.False(t, codexInfo.Background.Cost, "it reports tokens, never a cost")
	assert.True(t, codexInfo.Background.Usage)
	assert.True(t, codexInfo.Background.Resume, "its runs can be taken over")
	assert.Equal(t, codexFloor, FenceFloor(Codex))
}

func TestACodexRunReadsNoUserConfigNoRulesAndThePromptOnStdin(t *testing.T) {
	t.Parallel()

	for preset, base := range codexPresets() {
		argv := codexFenced(t, preset, base, testWorktree, testRunDir).argv

		assert.Equal(t, []string{"exec", "--json", "--ignore-user-config", "--ignore-rules"}, argv[:4], preset)
		assert.Equal(t, []string{"--", "-"}, argv[len(argv)-2:], "%s: the prompt comes on stdin, never the command line", preset)
		assert.NotContains(t, argv, "--approve-for-me", preset)
		assert.NotContains(t, argv, "--ephemeral", "%s: the thread is kept, to be taken over", preset)
		assert.Contains(t, argv, `approval_policy="never"`, preset)
		assert.Contains(t, argv, `model_reasoning_effort="low"`, preset)
		assert.Contains(t, argv, `web_search="disabled"`, preset)
		assert.Contains(t, argv, codexBypassHookTrust, "%s: the hook runs only with the bypass", preset)
		assert.Equal(t, 1, strings.Count(strings.Join(argv, "\x00"), codexHooksKey+"="), preset)
		assert.Contains(t, strings.Join(argv, " "), testRunDir+"/"+ReportSchemaName)
	}
}

func TestCodexsSandboxPerPreset(t *testing.T) {
	t.Parallel()

	gitDir, common := testClone+"/.git/worktrees/pr-12", testClone+"/.git"
	proof := testRunDir + "/" + fence.ProofDir
	roots := `sandbox_workspace_write.writable_roots=["` + gitDir + `","` + common + `/objects","` + common + `/refs","` +
		common + `/logs","` + proof + `"]`

	readOnly := codexFenced(t, fence.ReadOnly, Access{}, testWorktree, testRunDir).argv
	assert.Contains(t, strings.Join(readOnly, " "), "--sandbox read-only")
	assert.NotContains(t, strings.Join(readOnly, " "), "writable_roots")

	editTest := codexFenced(t, fence.EditTest, Access{}, testWorktree, testRunDir).argv
	assert.Contains(t, strings.Join(editTest, " "), "--sandbox workspace-write")
	assert.Contains(t, editTest, roots, "the worktree's git folders, for a commit, and the sandbox check's proof")
	assert.NotContains(t, strings.Join(editTest, " "), "network_access", "no push, no network")
	assert.NotContains(t, strings.Join(editTest, " "), common+`/hooks`)
	assert.NotContains(t, strings.Join(editTest, " "), common+`"`, "never the clone's whole git folder")

	push := codexFenced(t, fence.EditTestPush, Access{}, testWorktree, testRunDir).argv
	assert.Contains(t, push, "sandbox_workspace_write.network_access=true", "the one push needs the network")

	full := codexFenced(t, fence.Profile, Access{Kind: AccessFull}, testWorktree, testRunDir).argv
	assert.Contains(t, full, codexBypass, "the profile's own permissions are a click's")
	assert.Contains(t, full, codexBypassHookTrust, "and its hard lines are the hook's")

	for _, base := range []Access{{Kind: AccessFull}, {Kind: AccessMode, Mode: "danger-full-access"}, {}} {
		for _, preset := range []fence.Preset{fence.ReadOnly, fence.EditTest, fence.EditTestPush} {
			argv := codexFenced(t, preset, base, testWorktree, testRunDir).argv
			assert.NotContains(t, argv, codexBypass, "%s with %+v: a fixed preset brings its own sandbox", preset, base)
			assert.NotContains(t, argv, "danger-full-access", preset)
			assert.Equal(t, 1, strings.Count(strings.Join(argv, " "), "--sandbox "), preset)
		}
	}
}

func TestACodexFencesFlagsCantChangeWithoutTheFloorsReview(t *testing.T) {
	t.Parallel()

	used := map[string]bool{}

	for preset, base := range codexPresets() {
		bases := []Access{base}
		if preset == fence.Profile {
			bases = append(bases, Access{Kind: AccessFull}, Access{Kind: AccessPlan}, Access{})
		}

		for _, b := range bases {
			f := codexFenced(t, preset, b, testWorktree, testRunDir).built
			a := FenceAccess(preset, b).InRun(testRunDir).Worked(f.Policy, testProgram)

			recordFlags(used, codexFenceArgs(a.Fence))
			recordFlags(used, codexFenceArgs(a.TakenOver().Fence))
		}
	}

	got := slices.Collect(maps.Keys(used))
	sort.Strings(got)

	assert.Equal(t, slices.Sorted(slices.Values(codexFenceReviewed())), got, "a Codex fence's flags or config keys "+
		"changed: review each against codex-cli %s, raise codexFloor if one is newer, and list it in codexFenceReviewed",
		codexFloor)
}

// recordFlags records the flags args pass, and each config override's key.
func recordFlags(used map[string]bool, args []string) {
	for i, arg := range args {
		switch {
		case arg == codexConfig:
			key, _, _ := strings.Cut(args[i+1], "=")
			used[key], used[arg] = true, true
		case strings.HasPrefix(arg, "--"):
			used[arg] = true
		}
	}
}

func TestTheStrictSchemaIsWhatCodexTakes(t *testing.T) {
	t.Parallel()

	strict, err := StrictSchema(draftSchema)
	require.NoError(t, err)
	assert.NotContains(t, strict, "\n", "on one line")

	want, err := os.ReadFile(filepath.Join("testdata", "codex", "outcome-schema-strict.json"))
	require.NoError(t, err)

	assert.Equal(t, sortedRequired(t, want), sortedRequired(t, []byte(strict)),
		"the draft schema in S6's strict form: closed, every property required, the optional ones nullable")

	for _, bad := range []string{"", "[]", `{"type":"string"}`} {
		_, err = StrictSchema(bad)
		require.ErrorIs(t, err, ErrInvalidSchema, bad)
	}

	nested, err := StrictSchema(`{"type":"object","properties":{"a":{"type":"object","properties":{"b":{"type":"integer"}}},` +
		`"c":{"enum":["x"]},"d":{"description":"any"}}}`)
	require.NoError(t, err)
	assert.JSONEq(t, `{"type":"object","additionalProperties":false,"required":["a","c","d"],"properties":{`+
		`"a":{"type":["object","null"],"additionalProperties":false,"required":["b"],"properties":{"b":{"type":["integer","null"]}}},`+
		`"c":{"type":["string","null"],"enum":["x",null]},"d":{"anyOf":[{"description":"any"},{"type":"null"}]}}}`, nested)

	files, err := HeadlessFiles(Claude, draftSchema)
	require.NoError(t, err)
	assert.Empty(t, files, "Claude Code takes its schema on the command line")

	files, err = HeadlessFiles(Codex, "")
	require.NoError(t, err)
	assert.Empty(t, files)
}

// sortedRequired reads a schema with its required list sorted, as order means nothing there.
func sortedRequired(t *testing.T, data []byte) map[string]any {
	t.Helper()

	var v map[string]any
	require.NoError(t, json.Unmarshal(data, &v))

	if req, ok := v["required"].([]any); ok {
		slices.SortFunc(req, func(a, b any) int { return strings.Compare(a.(string), b.(string)) })
	}

	return v
}

func TestACodexRunDropsItsOwnVariablesButItsHomeAndCertificate(t *testing.T) {
	t.Parallel()

	env := []string{
		"PATH=/usr/bin", "CODEX_HOME=/srv/.codex_work", "CODEX_API_KEY=k", "CODEX_ACCESS_TOKEN=t", "CODEX_SANDBOX=seatbelt",
		"CODEX_SANDBOX_NETWORK_DISABLED=1", "CODEX_THREAD_ID=x", "CODEX_URL=https://example.com", "CODEX_SQLITE_HOME=/x",
		"CODEX_CA_CERTIFICATE=/etc/ca.pem", "OPENAI_API_KEY=o", "CLAUDECODE=1",
	}

	assert.Equal(t, []string{
		"PATH=/usr/bin", "CODEX_HOME=/srv/.codex_work", "CODEX_CA_CERTIFICATE=/etc/ca.pem", "OPENAI_API_KEY=o",
		"CLAUDECODE=1",
	}, HeadlessEnv(Codex, env), "the API key setting, not this, decides OPENAI_API_KEY (launcher.AgentEnv)")
}

func TestATakenOverCodexRunResumesUnderItsSandboxAndIsToldSomeoneAnswers(t *testing.T) {
	t.Parallel()

	built := codexFenced(t, fence.EditTestPush, Access{}, testWorktree, testRunDir).built
	a := FenceAccess(fence.EditTestPush, Access{}).InRun(testRunDir).Worked(built.Policy, "").TakenOver()
	thread := "01a1e670-d359-7720-8a38-a2402fc62c90"

	argv, err := Args(Codex, Options{Model: "gpt-6-sol", Access: a, ResumeID: thread, TakeOver: true})
	require.NoError(t, err)

	assert.Equal(t, []string{"resume", thread}, argv[:2])
	assert.Equal(t, []string{"--", codexTakeOverNote}, argv[len(argv)-2:], "its first message: someone answers now")

	line := strings.Join(argv, " ")
	assert.Contains(t, line, "--sandbox workspace-write", "a resume keeps no sandbox of its own, so it's passed again")
	assert.Contains(t, line, "--ask-for-approval on-request", "whatever the sandbox refuses is asked of the user")
	assert.Contains(t, line, "writable_roots", "its commits still work")
	assert.NotContains(t, line, "network_access", "the network is off: a push asks first")
	assert.NotContains(t, line, codexBypassHookTrust, "no hook bypass, which would run a trusted branch's hooks too")
	assert.NotContains(t, line, `approval_policy="never"`)

	plain, err := Args(Codex, Options{ResumeID: thread})
	require.NoError(t, err)
	assert.Equal(t, []string{"resume", thread, "--"}, plain, "a session's own resume gets no message")

	readOnly := FenceAccess(fence.ReadOnly, Access{}).InRun(testRunDir).TakenOver()
	argv, err = Args(Codex, Options{Access: readOnly, ResumeID: thread, TakeOver: true})
	require.NoError(t, err)
	assert.Contains(t, strings.Join(argv, " "), "--sandbox read-only --ask-for-approval on-request")
}

// fakeCodex runs a Codex background run's argv the way codex-cli 0.163.0 did, as checked
// locally: the profile's config.toml, with the projects it trusts, loads unless
// --ignore-user-config; a branch's .codex/config.toml (its MCP servers) and .codex/hooks.json
// load only in a folder it trusts; a -c PreToolUse hook runs only with the hook bypass, and
// with it, every enabled hook runs, the branch's included.
type fakeCodex struct {
	ignoreUser bool
	bypass     bool
	sandbox    string
	hooks      []string
	configKeys []string
}

// newFakeCodex reads a run's argv.
func newFakeCodex(t *testing.T, argv []string) *fakeCodex {
	t.Helper()

	f := &fakeCodex{}

	for i := 0; i < len(argv); i++ {
		switch a := argv[i]; a {
		case codexIgnoreUserConfig:
			f.ignoreUser = true
		case codexBypassHookTrust:
			f.bypass = true
		case codexSandbox:
			i++
			f.sandbox = argv[i]
		case codexConfig:
			i++
			key, value, _ := strings.Cut(argv[i], "=")
			f.configKeys = append(f.configKeys, key)

			if key == codexHooksKey {
				f.hooks = append(f.hooks, hookCommand(t, value))
			}
		}
	}

	return f
}

// hookCommand reads the command out of a -c hooks.PreToolUse value.
func hookCommand(t *testing.T, value string) string {
	t.Helper()

	start := strings.Index(value, `command="`)
	require.GreaterOrEqual(t, start, 0, value)

	rest := value[start+len(`command=`):]

	var command string
	require.NoError(t, json.NewDecoder(strings.NewReader(rest)).Decode(&command), "a TOML basic string reads as JSON's")

	return command
}

// started returns the commands a run starts before its first tool call: the MCP servers
// of each config that loads, and each SessionStart hook that runs, in a worktree whose
// branch holds .codex/config.toml with an MCP server and .codex/hooks.json, in a profile
// that trusts every folder and has an MCP server and a notify program of its own.
func (f *fakeCodex) started() []string {
	var out []string

	if !f.ignoreUser {
		out = append(out, "the user's MCP server", "the user's notify program", "the branch's MCP server")

		if f.bypass {
			out = append(out, "the branch's hooks.json hook")
		}
	}

	return out
}

// call runs one tool call through the run's PreToolUse hooks, in their hook mode, and
// reports whether it runs.
func (f *fakeCodex) call(t *testing.T, tool string, input map[string]any, cwd string) bool {
	t.Helper()

	if !f.bypass {
		return true // the hook silently doesn't run (S6)
	}

	for _, h := range f.hooks {
		payload, err := json.Marshal(map[string]any{
			"hook_event_name": "PreToolUse", "tool_name": tool, "tool_input": input, "cwd": cwd,
			"session_id": "01a1e670-d359-7720-8a38-a2402fc62c90", "permission_mode": "bypassPermissions",
			"transcript_path": nil, "model": "gpt-6-sol",
		})
		require.NoError(t, err)

		words := shellWords(t, h)
		require.Len(t, words, 2, "the hook is the app's executable and its flag")
		require.Equal(t, testProgram, words[0])

		code, ok := fence.RunHook(words[1:], bytes.NewReader(payload), &bytes.Buffer{})
		require.True(t, ok, "the hook runs in its hook mode")

		if code == 2 {
			return false
		}
	}

	return true
}

func TestABranchsCodexConfigCantRunACommandInABackgroundRun(t *testing.T) {
	t.Parallel()

	for preset, base := range codexPresets() {
		argv := codexFenced(t, preset, base, testWorktree, testRunDir).argv
		f := newFakeCodex(t, argv)

		assert.Empty(t, f.started(), "%s: no MCP server, notify program, or branch hook starts", preset)
		assert.NotContains(t, f.configKeys, "mcp_servers", preset)
		assert.NotContains(t, f.configKeys, "notify", preset)
	}

	without := newFakeCodex(t, slices.DeleteFunc(slices.Clone(codexFenced(t, fence.EditTest, Access{}, testWorktree,
		testRunDir).argv), func(a string) bool { return a == codexIgnoreUserConfig }))
	assert.Contains(t, without.started(), "the branch's hooks.json hook",
		"the check is real: without it, the bypass would run a trusted branch's hook unreviewed")
}

func TestTheCodexFenceHoldsWhole(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	wt := filepath.Join(root, "worktree")
	runDir := filepath.Join(root, "run")

	require.NoError(t, os.MkdirAll(wt, 0o700))

	run := codexFenced(t, fence.EditTestPush, Access{}, wt, runDir)
	require.NoError(t, fence.WriteRun(runDir, run.built, run.files...))
	require.NoError(t, os.WriteFile(run.built.Policy.Canary.Proof, nil, 0o600))

	f := newFakeCodex(t, run.argv)
	require.Equal(t, "workspace-write", f.sandbox)

	bashCall := func(cmd string) (string, map[string]any) { return codexShellTool, map[string]any{"command": cmd} }
	patchCall := func(file string) (string, map[string]any) {
		return "apply_patch", map[string]any{"command": "*** Begin Patch\n*** Update File: " + file + "\n@@\n+x\n*** End Patch"}
	}

	for _, tt := range []struct {
		name string
		call func() (string, map[string]any)
		runs bool
	}{
		{"git add", func() (string, map[string]any) { return bashCall("git add add.go") }, true},
		{"git commit", func() (string, map[string]any) { return bashCall(`git commit -m "Fix Add"`) }, true},
		{"the exact push", func() (string, map[string]any) { return bashCall("git push origin HEAD:" + testBranch) }, true},
		{"another branch", func() (string, map[string]any) { return bashCall("git push origin HEAD:other") }, false},
		{"--force", func() (string, map[string]any) { return bashCall("git push --force origin HEAD:" + testBranch) }, false},
		{"sh -c", func() (string, map[string]any) { return bashCall("sh -c 'git push origin HEAD:other'") }, false},
		{"git -C", func() (string, map[string]any) { return bashCall("git -C . push origin HEAD:other") }, false},
		{"curl", func() (string, map[string]any) { return bashCall("curl -sS https://example.com") }, false},
		{"the AGENTS.md patch", func() (string, map[string]any) { return patchCall(wt + "/AGENTS.md") }, false},
		{"a source patch", func() (string, map[string]any) { return patchCall(wt + "/add.go") }, true},
		{"a key, viewed", func() (string, map[string]any) {
			return "view_image", map[string]any{"path": testHome + "/.ssh/id_ed25519"}
		}, false},
	} {
		tool, input := tt.call()
		assert.Equal(t, tt.runs, f.call(t, tool, input, wt), tt.name)
	}

	count, _ := fence.ReadDenials(runDir)
	assert.Equal(t, 7, count, "each refusal is in the run's log, which Codex's stream never shows")
}
