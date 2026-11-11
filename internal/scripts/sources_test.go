package scripts

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/mrz1836/nilbox/internal/brand"
	"github.com/mrz1836/nilbox/internal/uierr"
)

// fixture returns the text of testdata/name.
func fixture(t *testing.T, name string) string {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // a fixture the test names
	require.NoError(t, err)

	return string(data)
}

// folder makes a folder holding files, by path relative to it, and returns it.
func folder(t *testing.T, files map[string]string) string {
	t.Helper()

	dir := t.TempDir()
	for rel, text := range files {
		writeText(t, filepath.Join(dir, filepath.FromSlash(rel)), text)
	}

	return dir
}

// writeText writes text to path, making the folders it needs.
func writeText(t *testing.T, path, text string) {
	t.Helper()

	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(text), 0o600))
}

// byName returns the scripts by name; the first of a name wins.
func byName(list []script) map[string]script {
	out := map[string]script{}
	for _, sc := range list {
		if _, seen := out[sc.name]; !seen {
			out[sc.name] = sc
		}
	}

	return out
}

// names returns the scripts' names in order.
func names(list []script) []string {
	out := make([]string, 0, len(list))
	for _, sc := range list {
		out = append(out, sc.name)
	}

	return out
}

func TestReadT3ListsThisRepository(t *testing.T) {
	t.Parallel()

	got, found, err := readT3(folder(t, map[string]string{t3File: fixture(t, "t3.json")}))
	require.NoError(t, err)
	require.True(t, found)

	assert.Equal(t, []string{
		"Fix All", "Test", "Test (race)", "Lint (fix)", "Build (dev)", "Pre-commit: install", "Pre-commit: run",
		"Deps: tidy", "Deps: update all", "Version: bump patch", "Version: bump minor",
	}, names(got))

	all := byName(got)
	fix := all["Fix All"]
	assert.Equal(t, Entry{
		Ref: "t3:Fix All", Source: SourceT3, Name: "Fix All", Command: "magex format:fix && magex lint && magex test:race",
		Icon: "play", Runnable: true, Inputs: []Input{},
	}, fix.entry)
	assert.Len(t, fix.hash, 64, "a repository command needs trust")
	assert.Equal(t, []part{{kind: partShell, command: "magex format:fix && magex lint && magex test:race"}}, fix.parts)

	for _, name := range []string{"Version: bump patch", "Version: bump minor"} {
		assert.True(t, all[name].entry.Release, "%s points to Release", name)
	}

	assert.False(t, all["Test"].entry.Release)
	assert.NotEqual(t, all["Test"].hash, all["Test (race)"].hash)
}

func TestReadT3(t *testing.T) {
	t.Parallel()

	_, found, err := readT3(t.TempDir())
	require.NoError(t, err)
	assert.False(t, found, "no t3.json")

	_, found, err = readT3(folder(t, map[string]string{t3File: "{not json"}))
	assert.True(t, found)
	require.Error(t, err)
	assert.Contains(t, uierr.Message(err), "t3.json isn't valid JSON: invalid character")

	got, _, err := readT3(folder(t, map[string]string{t3File: `{"scripts": [
		{"name": "Same", "command": "one"}, {"name": "Same", "command": "two"},
		{"name": "Nothing", "command": "  "}, {"name": "", "command": "unnamed"}]}`}))
	require.NoError(t, err)
	require.Len(t, got, 3, "a script without a name is left out")
	assert.True(t, got[0].entry.Runnable)
	assert.Equal(t, "Another script in t3.json has this name.", got[1].entry.Why)
	assert.Equal(t, "Has no command to run.", got[2].entry.Why)

	_, _, err = readT3(folder(t, map[string]string{t3File: strings.Repeat(" ", maxFile+1)}))
	assert.Equal(t, "t3.json is over 1 MB, too large to read.", uierr.Message(err))
}

func TestT3HashIgnoresFormatting(t *testing.T) {
	t.Parallel()

	before, _, err := readT3(folder(t, map[string]string{t3File: fixture(t, "t3.json")}))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(fixture(t, "t3.json")), &doc))

	compact, err := json.Marshal(doc) // keys sorted, no space
	require.NoError(t, err)

	after, _, err := readT3(folder(t, map[string]string{t3File: "// reformatted\n" + string(compact)}))
	require.NoError(t, err)

	for i := range before {
		assert.Equal(t, before[i].hash, after[i].hash, before[i].name)
	}
}

func TestReadTasksListsThisRepository(t *testing.T) {
	t.Parallel()

	got, found, err := readTasks(folder(t, map[string]string{tasksFile: fixture(t, "tasks.json")}))
	require.NoError(t, err)
	require.True(t, found)
	require.Len(t, got, 38)

	for _, sc := range got {
		assert.True(t, sc.entry.Runnable, "%s: %s", sc.name, sc.entry.Why)
		assert.Len(t, sc.hash, 64, sc.name)
	}

	all := byName(got)

	lint := all["magex: lint"]
	assert.Equal(t, "magex lint", lint.entry.Command)
	assert.Equal(t, "Run comprehensive linting with golangci-lint (all enabled linters)", lint.entry.Detail)
	assert.Equal(t, []part{{kind: partShell, command: "magex lint", args: []string{}, env: [][2]string{}}}, lint.parts)

	bump := all["magex: version:bump"]
	assert.True(t, bump.entry.Release, "Release does this")
	assert.Equal(t, []Input{
		{
			ID: "targetBranch", Kind: InputPick, Description: "Target branch for version bump", Default: "master",
			Options: []Option{optionOf("master", "master"), optionOf("main", "main")},
		},
		{
			ID: "versionBump", Kind: InputPick, Description: "Select version bump type", Default: "patch",
			Options: []Option{optionOf("patch", "patch"), optionOf("minor", "minor")},
		},
	}, bump.entry.Inputs, "its pickString inputs become a form")

	major := all["magex: version:bump:major"]
	assert.Equal(t, []string{"majorConfirm", "targetBranch"}, inputIDs(major.entry.Inputs))

	assert.Equal(t,
		"magex format:fix && magex lint:fast && magex test:short && echo 'Running quick-check workflow...'",
		all["workflow: quick-check"].entry.Command)
	assert.Empty(t, all["workflow: quick-check"].entry.Note, "a sequence runs as VS Code runs it")

	assert.Equal(t, "magex format:fix && magex lint && magex test && magex test:coverrace && "+
		"echo 'Running full-check workflow...' && magex deps:audit && magex bench && "+
		"echo 'Running pre-release checks...'", all["workflow: pre-release"].entry.Command,
		"a dependency's own dependencies come first")

	assert.True(t, all["magex: docs:serve"].entry.Background, "runs until stopped")
}

func inputIDs(inputs []Input) []string {
	out := make([]string, 0, len(inputs))
	for _, in := range inputs {
		out = append(out, in.ID)
	}

	return out
}

func TestReadTasksCases(t *testing.T) {
	t.Parallel()

	got, _, err := readTasks(folder(t, map[string]string{tasksFile: fixture(t, "tasks-cases.jsonc")}))
	require.NoError(t, err)

	all := byName(got)
	runnable := func(label string) bool { return all[label].entry.Runnable }

	cant := brand.Name + " can't fill in"
	assert.Equal(t, map[string]string{
		"file":          "Uses ${file}, which " + cant + ".",
		"command var":   "Uses ${command:pickFile}, an editor command " + brand.Name + " can't run.",
		"config var":    "Uses ${config:go.gopath}, an editor setting " + brand.Name + " can't read.",
		"password":      "Asks for a password (${input:secret}), which " + brand.Name + " doesn't take.",
		"command input": `Uses ${input:picked}, an input of type "command", which ` + cant + ".",
		"missing input": "Uses ${input:nowhere}, which .vscode/tasks.json doesn't define.",
		"npm":           `A task of type "npm", which only VS Code runs.`,
		"no type":       "Has no type: shell or process.",
		"bad env":       `Sets a variable named "NOT-A-NAME", which a program can't take.`,
		"on greyed":     `Depends on "file": Uses ${file}, which ` + cant + ".",
		"on missing":    `Depends on "nowhere", which isn't in .vscode/tasks.json.`,
		"on background": `Depends on "serve", which runs in the background and doesn't end.`,
		"loop a":        `Depends on "loop b": Its dependencies loop back to it.`,
		"loop b":        "Its dependencies loop back to it.",
		"by type":       "Names a dependency in a way " + brand.Name + " can't follow.",
		"empty":         "Has no command to run.",
		"odd command":   "Has a command or an argument written in a way " + brand.Name + " can't read.",
		"unreadable":    "Is written in a way " + brand.Name + " can't read.",
	}, greyed(got), "every greyed task says why, in words")

	assert.NotContains(t, names(got), "", "a task without a label is left out")
	assert.True(t, runnable("plain"))
	assert.Equal(t, "Another task in .vscode/tasks.json has this label.", got[len(got)-3].entry.Why)

	assert.Equal(t, "echo 'two words' 'it'\\''s'", all["with args"].entry.Command, "arguments quoted for the shell")
	assert.Equal(t, "/bin/bash -l -c 'magex test'", all["own shell"].entry.Command)
	assert.Equal(t, &shellSpec{Executable: "/bin/bash", Args: []string{"-l", "-c"}}, all["own shell"].parts[0].shell)
	assert.Equal(t, "zsh -c 'magex test'", all["shell no args"].entry.Command, "a shell's own arguments default to -c")

	mac := all["on macOS"].parts[0]
	assert.Equal(t, "mac-only", mac.command, "the osx section is what runs")
	assert.Equal(t, [][2]string{{"OWN", "task"}, {"SHARED", "osx"}}, mac.env, "its options over the task's, by name")

	assert.Equal(t, part{
		kind: partProcess, command: "/usr/bin/true", args: []string{"--flag", "${workspaceFolder}/out"},
		env: [][2]string{},
	}, all["process"].parts[0])
	assert.Equal(t, "cd ${workspaceFolder}/sub && HOME_DIR=${userHome} PATH_TOO=${env:PATH}${pathSeparator}x "+
		"ROOT=${workspaceFolderBasename} make", all["in a folder"].entry.Command)

	bump := all["bump"]
	assert.True(t, bump.entry.Release)
	assert.Equal(t, []Input{
		{ID: "bump", Kind: InputPick, Default: "patch", Options: []Option{
			{Label: "patch", Value: "patch", Shown: "patch"}, {Label: "Minor", Value: "minor", Shown: "Minor (minor)"},
		}},
		{ID: "note", Kind: InputPrompt, Description: "A note", Default: "hello", Options: []Option{}},
	}, bump.entry.Inputs)

	assert.True(t, all["serve"].entry.Background)
	assert.Empty(t, all["sequence"].entry.Note)
	assert.Equal(t, "Runs its dependencies one after another, where VS Code runs them side by side.",
		all["parallel"].entry.Note)
	assert.Equal(t, "magex lint && echo 'two words' 'it'\\''s' && echo nested", all["nested"].entry.Command)
	assert.Equal(t, []string{"plain", "with args"}, labels(all["diamond"]), "a task runs once in a chain")
	assert.True(t, runnable("diamond"))
}

// greyed returns why each greyed task can't run, by label.
func greyed(list []script) map[string]string {
	out := map[string]string{}

	for _, sc := range list {
		if !sc.entry.Runnable && sc.entry.Why != "Another task in "+tasksFile+" has this label." {
			out[sc.name] = sc.entry.Why
		}
	}

	return out
}

// labels returns the commands a task's chain runs, by each part's own command.
func labels(sc script) []string {
	out := make([]string, 0, len(sc.parts))
	for _, p := range sc.parts {
		switch p.command {
		case "magex lint":
			out = append(out, "plain")
		case "echo":
			out = append(out, "with args")
		default:
			out = append(out, p.command)
		}
	}

	return out
}

func TestTasksAreBounded(t *testing.T) {
	t.Parallel()

	var b strings.Builder

	b.WriteString(`{"tasks": [{"label": "t0", "type": "shell", "command": "echo 0"}`)

	for i := 1; i < maxTasks+50; i++ {
		fmt.Fprintf(&b, `, {"label": "t%d", "type": "shell", "command": "echo %d", "dependsOn": "t%d"}`, i, i, i-1)
	}

	b.WriteString("]}")

	got, _, err := readTasks(folder(t, map[string]string{tasksFile: b.String()}))
	require.NoError(t, err)
	require.Len(t, got, maxTasks, "the rest are left out")

	all := byName(got)
	assert.True(t, all[fmt.Sprintf("t%d", maxChain-1)].entry.Runnable)
	assert.Equal(t, fmt.Sprintf("Its dependencies run more than %d commands.", maxChain),
		all[fmt.Sprintf("t%d", maxChain)].entry.Why)
	assert.LessOrEqual(t, len(all[fmt.Sprintf("t%d", maxTasks-1)].parts), maxChain+1)
}

func TestTasksFileOptions(t *testing.T) {
	t.Parallel()

	got, _, err := readTasks(folder(t, map[string]string{tasksFile: `{
		"options": {"cwd": "build", "env": {"A": "file", "B": "file"}, "shell": {"executable": "bash"}},
		"tasks": [
			{"label": "inherits", "type": "shell", "command": "make"},
			{"label": "overrides", "type": "shell", "command": "make", "options": {"cwd": "other", "env": {"B": "task"}}},
			{"label": "process", "type": "process", "command": "make"}]}`}))
	require.NoError(t, err)

	all := byName(got)
	assert.Equal(t, part{
		kind: partShell, command: "make", args: []string{}, cwd: "build",
		env: [][2]string{{"A", "file"}, {"B", "file"}}, shell: &shellSpec{Executable: "bash"},
	}, all["inherits"].parts[0],
		"the file's options are every task's")
	assert.Equal(t, part{
		kind: partShell, command: "make", args: []string{}, cwd: "other",
		env: [][2]string{{"A", "file"}, {"B", "task"}}, shell: &shellSpec{Executable: "bash"},
	}, all["overrides"].parts[0])
	assert.Nil(t, all["process"].parts[0].shell, "a process runs with no shell")
}

func TestTaskHashes(t *testing.T) {
	t.Parallel()

	read := func(text string) map[string]script {
		got, _, err := readTasks(folder(t, map[string]string{tasksFile: text}))
		require.NoError(t, err)

		return byName(got)
	}

	base := read(`{"tasks": [
		{"label": "a", "type": "shell", "command": "magex lint", "options": {"env": {"X": "1", "Y": "2"}}},
		{"label": "b", "type": "shell", "command": "magex test"},
		{"label": "all", "dependsOn": ["a", "b"], "dependsOrder": "sequence"}]}`)

	reformatted := read(`// the same tasks, written another way
	{ "tasks" : [
		{ "command" : "magex lint", "type": "shell", "label": "a", "options": {"env": {"Y": "2", "X": "1",},},
		  "presentation": {"reveal": "always"}, "problemMatcher": ["$go"] },
		{"label": "b", /* unchanged */ "type": "shell", "command": "magex test", "group": "test"},
		{"dependsOrder": "sequence", "dependsOn": ["a", "b"], "label": "all"},
	]}`)

	for _, label := range []string{"a", "b", "all"} {
		assert.Equal(t, base[label].hash, reformatted[label].hash, "%s: formatting, order, and ignored fields", label)
	}

	changed := read(`{"tasks": [
		{"label": "a", "type": "shell", "command": "magex lint", "options": {"env": {"X": "1", "Y": "2"}}},
		{"label": "b", "type": "shell", "command": "magex test:race"},
		{"label": "all", "dependsOn": ["a", "b"], "dependsOrder": "sequence"}]}`)

	assert.Equal(t, base["a"].hash, changed["a"].hash)
	assert.NotEqual(t, base["b"].hash, changed["b"].hash)
	assert.NotEqual(t, base["all"].hash, changed["all"].hash, "a dependency's command changed")

	for name, text := range map[string]string{
		"an env value": `{"tasks": [{"label": "a", "type": "shell", "command": "magex lint", "options": {"env": {"X": "1", "Y": "3"}}}]}`,
		"the folder":   `{"tasks": [{"label": "a", "type": "shell", "command": "magex lint", "options": {"cwd": "sub", "env": {"X": "1", "Y": "2"}}}]}`,
		"the shell": `{"tasks": [{"label": "a", "type": "shell", "command": "magex lint",
			"options": {"shell": {"executable": "bash"}, "env": {"X": "1", "Y": "2"}}}]}`,
		"the type": `{"tasks": [{"label": "a", "type": "process", "command": "magex lint", "options": {"env": {"X": "1", "Y": "2"}}}]}`,
	} {
		assert.NotEqual(t, base["a"].hash, read(text)["a"].hash, name)
	}

	withInput := `{"inputs": [{"id": "b", "type": "pickString", "options": ["patch", "minor"]}],
		"tasks": [{"label": "a", "type": "shell", "command": "magex bump b=${input:b}"}]}`
	moreChoices := strings.Replace(withInput, `"minor"]`, `"minor", "major"]`, 1)
	assert.NotEqual(t, read(withInput)["a"].hash, read(moreChoices)["a"].hash, "an input's definition")
}
